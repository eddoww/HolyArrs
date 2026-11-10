package migrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/library"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
	"github.com/HolyArrs/holyarrs/app/backend/internal/requests"
)

type settingValue struct {
	Exists bool   `json:"exists"`
	Value  string `json:"value"`
}

type askBaseline struct {
	UserID          int64  `json:"user_id"`
	AskedAt         string `json:"asked_at"`
	UserFingerprint string `json:"user_fingerprint"`
}

type askPersonMark struct {
	AskID           int64  `json:"ask_id"`
	UserID          int64  `json:"user_id"`
	AskedAt         string `json:"asked_at"`
	AskFingerprint  string `json:"ask_fingerprint"`
	UserFingerprint string `json:"user_fingerprint"`
}

func askIdentity(ctx context.Context, q rowQuerier, id int64) (string, error) {
	var fp string
	err := q.QueryRowContext(ctx, "SELECT created_at || char(31) || kind || char(31) || ref FROM asks WHERE id = ?", id).Scan(&fp)
	return fp, err
}

func userIdentity(ctx context.Context, q rowQuerier, id int64) (string, error) {
	var fp string
	err := q.QueryRowContext(ctx, "SELECT created_at FROM users WHERE id = ?", id).Scan(&fp)
	return fp, err
}

func (r *run) expectedUserIdentity(key string, id int64) (string, error) {
	if key != "" {
		var row int64
		var fp string
		err := r.m.Store.Read().QueryRowContext(r.ctx, `SELECT m.row_id,m.fingerprint FROM move_made m JOIN moves v ON v.id=m.move_id
			WHERE m.key=? AND m.what='user' AND v.state!='undone' ORDER BY m.rowid DESC LIMIT 1`, key).Scan(&row, &fp)
		if err == nil && row == id {
			if at := strings.IndexByte(fp, 31); at > 0 {
				return fp[:at], nil
			}
			return "", errUndoReplaced
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	return userIdentity(r.ctx, r.m.Store.Read(), id)
}

func (r *run) askMarkThisMove(key string) (bool, bool) {
	tx, err := r.m.Store.Read().BeginTx(r.ctx, nil)
	if err != nil {
		return false, false
	}
	defer tx.Rollback()
	var x madeRow
	err = tx.QueryRowContext(r.ctx, "SELECT key,what,row_id,before,after,fingerprint FROM move_made WHERE move_id=? AND key=?", r.move, key).
		Scan(&x.key, &x.what, &x.row, &x.before, &x.after, &x.fp)
	if err != nil {
		return false, false
	}
	switch x.what {
	case "ask":
		var b askBaseline
		if json.Unmarshal([]byte(x.after), &b) != nil || b.UserFingerprint == "" || fingerprint(r.ctx, tx, "ask", x.row) != x.fp {
			return true, false
		}
		userFP, err := userIdentity(r.ctx, tx, b.UserID)
		if err != nil || userFP != b.UserFingerprint {
			return true, false
		}
		var one int
		err = tx.QueryRowContext(r.ctx, "SELECT 1 FROM ask_people WHERE ask_id=? AND user_id=? AND asked_at=? AND counted=0", x.row, b.UserID, b.AskedAt).Scan(&one)
		return true, err == nil
	case "ask_person", "ask_claim":
		var b askPersonMark
		if json.Unmarshal([]byte(x.before), &b) != nil || b.AskFingerprint == "" || b.UserFingerprint == "" {
			return true, false
		}
		askFP, askErr := askIdentity(r.ctx, tx, b.AskID)
		userFP, userErr := userIdentity(r.ctx, tx, b.UserID)
		if askErr != nil || userErr != nil || askFP != b.AskFingerprint || userFP != b.UserFingerprint {
			return true, false
		}
		var one int
		err = tx.QueryRowContext(r.ctx, "SELECT 1 FROM ask_people WHERE ask_id=? AND user_id=? AND asked_at=? AND counted=0", b.AskID, b.UserID, b.AskedAt).Scan(&one)
		return true, err == nil
	default:
		return true, false
	}
}

type askSettingsMark map[string]settingValue

func readSettingValue(ctx context.Context, q rowQuerier, key string) (settingValue, error) {
	var raw string
	if err := q.QueryRowContext(ctx, "SELECT value FROM settings WHERE grp = 'this_app' AND key = ?", key).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
		return settingValue{}, nil
	} else if err != nil {
		return settingValue{}, err
	}
	return settingValue{Exists: true, Value: raw}, nil
}

func (r *run) askSettings(it Item) (string, string) {
	as := it.AskSettings
	// wanted holds each request rule this item sets, as it is stored.
	wanted := map[string]string{}
	var words, missing []string
	if as.MoviesPerWeek != nil {
		wanted["ask_movies_per_week"] = strconv.Itoa(*as.MoviesPerWeek)
	}
	if as.ShowsPerWeek != nil {
		wanted["ask_shows_per_week"] = strconv.Itoa(*as.ShowsPerWeek)
	}
	if as.MoviesPerWeek != nil || as.ShowsPerWeek != nil {
		words = append(words, "default movie and show weekly limits")
	}
	for _, t := range []struct {
		target             *AskTarget
		kind, folder, rule string
		word               string
	}{{as.Movie, "movie", "ask_library_folder_id", "ask_pick_rules_id", "movies"}, {as.Show, "show", "ask_show_library_folder_id", "ask_show_pick_rules_id", "shows"},
		{as.FourK, "movie", "ask_4k_library_folder_id", "ask_4k_pick_rules_id", "4K movies"},
		{as.ShowFourK, "show", "ask_show_4k_library_folder_id", "ask_show_4k_pick_rules_id", "4K shows"}} {
		if t.target == nil {
			continue
		}
		lf := r.id("SELECT id FROM library_folders WHERE path = ? AND kind = ?", filepath.Clean(filepath.FromSlash(t.target.Folder)), t.kind)
		if lf == 0 && r.dry && isDir(filepath.FromSlash(t.target.Folder)) {
			// The move adds the folder before it gets here.
			words = append(words, t.word+" to "+t.target.Folder+" once it is a Library folder")
			continue
		}
		if lf == 0 {
			missing = append(missing, t.word+" ("+t.target.Folder+" is not a "+t.kind+" Library folder here)")
			continue
		}
		wanted[t.folder] = strconv.FormatInt(lf, 10)
		w := t.word + " to " + t.target.Folder
		if pr := r.rulesFor(t.target.Profile, t.target.Preset, t.kind); pr != 0 {
			wanted[t.rule] = strconv.FormatInt(pr, 10)
			w += " on " + r.rulesNameByID(pr)
		}
		words = append(words, w)
	}
	what := strings.Join(words, "; ")
	if len(missing) > 0 {
		what = strings.TrimPrefix(what+"; not set for "+strings.Join(missing, ", "), "; ")
	}
	if len(wanted) == 0 {
		if r.dry && len(words) > 0 {
			return ResChange, what
		}
		return ResNotFound, what
	}
	same := true
	for k, v := range wanted {
		cur, err := readSettingValue(r.ctx, r.m.Store.Read(), k)
		if err != nil {
			return ResFailed, err.Error()
		}
		same = same && cur.Exists && cur.Value == v
	}
	if same {
		return ResAlready, "these request rules are already set"
	}
	if r.dry {
		return ResChange, what
	}
	who, _ := json.Marshal(r.by)
	err := r.m.Store.Write(r.ctx, func(tx *sql.Tx) error {
		beforeState, afterState := askSettingsMark{}, askSettingsMark{}
		for key, value := range wanted {
			state, err := readSettingValue(r.ctx, tx, key)
			if err != nil {
				return err
			}
			beforeState[key] = state
			afterState[key] = settingValue{Exists: true, Value: value}
		}
		now := database.FormatTime(r.m.now())
		for k, state := range afterState {
			if _, err := tx.Exec(`INSERT INTO settings (grp, key, value, created_at, created_by, updated_at, updated_by) VALUES ('this_app', ?, ?, ?, ?, ?, ?)
				ON CONFLICT (grp, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
				k, state.Value, now, string(who), now, string(who)); err != nil {
				return err
			}
		}
		before, _ := json.Marshal(beforeState)
		after, _ := json.Marshal(afterState)
		return r.markTx(tx, it.Key, "ask_settings", 0, string(before), string(after))
	})
	return result(err, ResChanged, what)
}

// signIn keeps the address of the media server people signed in with in
// the request app as HolyArrs' own sign-in with it, left off until the
// owner turns it on. An address already set here stays.
func (r *run) signIn(it Item) (string, string) {
	si := it.SignIn
	key := "sign_in_" + si.Kind + "_url"
	cur, err := readSettingValue(r.ctx, r.m.Store.Read(), key)
	if err != nil {
		return ResFailed, err.Error()
	}
	switch {
	case cur.Exists && cur.Value == si.URL:
		return ResAlready, "this sign-in address is already set"
	case cur.Exists && cur.Value != "":
		return ResKept, "another sign-in address is set here, so it stays"
	case r.dry:
		return ResChange, si.URL + "; off until you turn it on"
	}
	who, _ := json.Marshal(r.by)
	err = r.m.Store.Write(r.ctx, func(tx *sql.Tx) error {
		before, err := readSettingValue(r.ctx, tx, key)
		if err != nil {
			return err
		}
		now := database.FormatTime(r.m.now())
		if _, err := tx.Exec(`INSERT INTO settings (grp, key, value, created_at, created_by, updated_at, updated_by) VALUES ('this_app', ?, ?, ?, ?, ?, ?)
			ON CONFLICT (grp, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
			key, si.URL, now, string(who), now, string(who)); err != nil {
			return err
		}
		b, _ := json.Marshal(askSettingsMark{key: before})
		a, _ := json.Marshal(askSettingsMark{key: {Exists: true, Value: si.URL}})
		return r.markTx(tx, it.Key, "ask_settings", 0, string(b), string(a))
	})
	return result(err, ResChanged, si.URL+"; off until you turn it on under Settings > Sign-in")
}

func (r *run) id(q string, args ...any) int64 {
	var id int64
	_ = r.m.Store.Read().QueryRowContext(r.ctx, q, args...).Scan(&id)
	return id
}

// result is a made row's line: failed with the reason, or res. The row and
// its mark are made in one transaction, so a failed one left nothing.
func result(err error, res, detail string) (string, string) {
	if err != nil {
		return ResFailed, err.Error()
	}
	return res, detail
}

func (r *run) folder(it Item) (string, string) {
	f := it.Folder
	p := filepath.FromSlash(f.Path)
	if r.id("SELECT id FROM library_folders WHERE path = ?", filepath.Clean(p)) != 0 {
		return ResAlready, "already a Library folder"
	}
	if !filepath.IsAbs(p) || !isDir(p) {
		return ResNotFound, "no folder at " + f.Path + " on this server; add a path rule"
	}
	if r.dry {
		return ResCreate, "a " + f.Kind + " Library folder at " + f.Path
	}
	paths, _ := library.DownloadPaths(r.ctx, r.m.Store.Read(), r.m.Library.SavePath)
	in := library.FolderInput{Path: p, Kind: f.Kind, Label: f.Label}
	what := "a " + f.Kind + " Library folder at " + f.Path
	if pr := r.rulesFor(f.Profile, f.Preset, f.Kind); pr != 0 {
		in.DefaultPickRules = &pr
		what += "; new titles take " + r.rulesNameByID(pr)
	}
	_, err := library.AddFolder(r.ctx, r.m.Store, in, paths, r.by, r.markWith(it.Key, "folder"))
	return result(err, ResCreated, what)
}

func (r *run) presetID(name, titleKind string) int64 {
	if name == "" {
		return 0
	}
	// Pick rules made for shows only never serve a movie: Sonarr's
	// "HD-1080p" and Radarr's "HD-1080p (Radarr)" are both here.
	kind := titleKind
	if kind == "artist" {
		kind = "music"
	}
	return r.id(`SELECT id FROM pick_rules WHERE name = ? AND kinds LIKE ? ORDER BY id LIMIT 1`, r.rulesName(name), `%"`+kind+`"%`)
}

// rulesNamed is the id of the Pick rules with this name, case ignored (0
// when none has it).
func (r *run) rulesNamed(name string) int64 {
	return r.id("SELECT id FROM pick_rules WHERE lower(name) = lower(?)", name)
}

// rulesNameOf is the name of the Pick rules with this id ("" when gone).
func (r *run) rulesNameOf(id int64) string {
	var name string
	_ = r.m.Store.Read().QueryRowContext(r.ctx, "SELECT name FROM pick_rules WHERE id = ?", id).Scan(&name)
	return name
}

func (r *run) rules(it Item) (string, string) {
	kind := "movie"
	if it.Rules.App == string(Lidarr) {
		kind = "artist"
	}
	if r.presetID(it.Rules.Preset, kind) == 0 {
		return ResNotFound, "the Pick rules " + it.Rules.Preset + " are not here any more; titles keep the default"
	}
	return ResMatched, "titles from it use " + it.Rules.Preset
}

// seededWait is the protocol block the ready-made Pick rules come with.
var seededWait = mediafiles.ProtocolRule{Prefer: "either"}

// untouchedWait says a protocol block is still as it came.
func untouchedWait(p mediafiles.ProtocolRule) bool {
	if p.Prefer == "" {
		p.Prefer = "either"
	}
	return p == seededWait
}

// protocol sets an old app's default wait on ready-made Pick rules, only
// while their wait is still as it came; the old block is kept for undo.
func (r *run) protocol(it Item) (string, string) {
	pp := it.Protocol
	id := r.id("SELECT id FROM pick_rules WHERE name = ? AND preset = 1", pp.Preset)
	if made, ok := r.madeRules[pp.Preset]; ok {
		// Pick rules this move made from a profile take the wait too.
		id = r.id("SELECT id FROM pick_rules WHERE name = ?", made)
	}
	if id == 0 {
		return ResNotFound, "the Pick rules " + pp.Preset + " are not here any more"
	}
	var cur mediafiles.PickRules
	if err := database.Get(r.ctx, r.m.Store.Read(), "pick_rules", id, &cur); err != nil {
		return ResFailed, err.Error()
	}
	want := mediafiles.ProtocolRule{Prefer: pp.Prefer, WaitMinutesUsenet: pp.WaitUsenet, WaitMinutesTorrent: pp.WaitTorrent, SkipWaitAtGoodEnough: pp.SkipAtGoodEnough}
	switch {
	case cur.Body.Protocol == want:
		return ResAlready, pp.Preset + " already waits this way"
	case !untouchedWait(cur.Body.Protocol):
		return ResKept, "this server's Pick rules already set a wait, so it stays"
	}
	what := pp.Preset + ": " + waitWords(*pp)
	if r.dry {
		u, err := library.PickRulesUse(r.ctx, r.m.Store, id)
		if err != nil {
			return ResFailed, err.Error()
		}
		return ResChange, fmt.Sprintf("%s; used by %d versions here now", what, u.Versions)
	}
	before, _ := json.Marshal(cur.Body.Protocol)
	after, _ := json.Marshal(want)
	body := cur.Body
	body.Protocol = want
	_, err := library.UpdatePickRules(r.ctx, r.m.Store, id, library.PickRulesInput{Name: cur.Name, Kinds: cur.Kinds, Body: body}, r.by, func(tx *sql.Tx) error {
		// Changed by someone else since it was read: leave it to the next move.
		var rev int
		if err := tx.QueryRow("SELECT revision FROM pick_rules WHERE id = ?", id).Scan(&rev); err != nil {
			return err
		}
		if rev != cur.Revision+1 {
			return errors.New("these Pick rules changed during the move; move again")
		}
		return r.markTx(tx, it.Key, "pick_rules_protocol", id, string(before), string(after))
	})
	return result(err, ResChanged, what)
}

// outsideList adds a Radarr list as a paused Outside list.
func (r *run) outsideList(it Item) (string, string) {
	o := it.OutsideList
	if r.id("SELECT id FROM outside_lists WHERE kind = ? AND ref = ? COLLATE NOCASE AND title_kind = 'movie'", o.Kind, o.Ref) != 0 {
		return ResAlready, "an Outside list for this list is here"
	}
	lib := filepath.Clean(filepath.FromSlash(o.Library))
	what := map[string]string{requests.ListTMDB: "TMDB list ", requests.ListTrakt: "Trakt list "}[o.Kind] + o.Ref + " into " + o.Library + ", paused until you check it and turn it on"
	if r.dry {
		if r.id("SELECT id FROM library_folders WHERE path = ?", lib) == 0 && !isDir(lib) {
			return ResNotFound, "no folder at " + o.Library + " on this server; add a path rule"
		}
		return ResCreate, what
	}
	lfID := r.id("SELECT id FROM library_folders WHERE path = ? AND kind = 'movie'", lib)
	if lfID == 0 {
		return ResNotFound, "its Library folder " + o.Library + " is not here (not moved, or left out)"
	}
	name := freeListName(it.Name, func(n string) bool {
		return r.id("SELECT id FROM outside_lists WHERE name = ? COLLATE NOCASE", n) != 0
	})
	off, keep := false, o.KeepUpToDate
	in := requests.OutsideListInput{Name: name, Kind: o.Kind, Ref: o.Ref, LibraryFolderID: lfID, PickRulesID: r.presetID(o.Preset, "movie"), KeepUpToDate: &keep,
		SearchOnAdd: o.SearchOnAdd, ReleaseGate: o.ReleaseGate, Enabled: &off, SyncEveryHours: o.SyncEveryHours}
	_, err := requests.AddOutsideList(r.ctx, r.m.Store, in, r.by, r.markWith(it.Key, "outside_list"))
	return result(err, ResCreated, what)
}
