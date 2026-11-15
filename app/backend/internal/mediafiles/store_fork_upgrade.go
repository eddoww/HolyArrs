package mediafiles

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// Fork builds before 1.1.4 moved Sonarr's and Radarr's custom formats and
// quality profiles their own way: traits that match by conditions (match
// "conditions") and Pick rules that judge like those apps (like_old_apps,
// on the fork's longer quality ladder). Upstream 1.1.4 imports the same
// things as traits with tests (specs) and Pick rules marked as imported
// (Synced). UpgradeForkRules turns the first into the second once per
// install, before any judging, so the traits and Pick rules an owner moved
// keep working and keep their ids (Pick rules name traits as own:<id>).

// forkRulesDone marks the upgrade done in the this_app settings.
const forkRulesDone = "fork_rules_upgraded"

// forkCondition is one condition of a fork trait.
type forkCondition struct {
	Name     string  `json:"name"`
	Kind     string  `json:"kind"`
	Value    string  `json:"value"`
	Negate   bool    `json:"negate"`
	Required bool    `json:"required"`
	Except   bool    `json:"except"`
	MinGB    float64 `json:"min_gb"`
	MaxGB    float64 `json:"max_gb"`
}

// forkStepMap maps the fork's quality ladder steps that upstream's ladder
// lacks onto upstream's steps (br_disk has none).
var forkStepMap = map[string]string{
	"workprint": "cam", "telecine": "telesync",
	"regional": "sd_dvd", "dvdscr": "sd_dvd", "dvd_r": "sd_dvd", "sd_bluray": "sd_dvd",
	"sd_web": "sd_tv", "2160p_tv": "2160p_web", "raw_hd": "1080p_tv", "br_disk": "",
}

// forkStep is the upstream step of a fork step ("" for none).
func forkStep(q string) string {
	if to, ok := forkStepMap[q]; ok {
		return to
	}
	if slices.Contains(Ladder, q) {
		return q
	}
	return ""
}

// forkConditionSpecs turns a fork trait's conditions into upstream's trait
// tests; ok is false when a condition has no test here.
func forkConditionSpecs(conds []forkCondition) ([]OwnSpec, bool) {
	out := make([]OwnSpec, 0, len(conds))
	for _, c := range conds {
		s := OwnSpec{Name: c.Name, Negate: c.Negate, Required: c.Required, Value: strings.TrimSpace(c.Value)}
		if s.Name == "" {
			s.Name = c.Kind
		}
		switch c.Kind {
		case "release_title":
			s.Kind = "name"
		case "release_group":
			s.Kind = "group"
		case "edition":
			s.Kind = "edition"
		case "resolution":
			s.Kind = "resolution"
		case "language":
			s.Kind = "language"
			if c.Except {
				s.Value = "!" + s.Value
			}
		case "source", "modifier":
			s.Kind = c.Kind
		case "size":
			s.Kind, s.Value = "size", SizeRangeValue(c.MinGB, c.MaxGB)
		default:
			return nil, false
		}
		out = append(out, s)
	}
	return out, len(out) > 0 && SpecsCompile(out)
}

// forkRules is a fork Pick rules row being upgraded.
type forkRules struct {
	id    int64
	name  string
	kinds []string
	raw   map[string]json.RawMessage
}

// appOf names the old app Pick rules for these kinds came from.
func appOf(kinds []string) string {
	if slices.Contains(kinds, "show") && !slices.Contains(kinds, "movie") {
		return "sonarr"
	}
	return "radarr"
}

// UpgradeForkRules upgrades the fork's moved traits and Pick rules (see
// above) once per install and says how many of each it changed. Anything it
// cannot upgrade is left as it was and logged, and the pass runs again on
// the next start.
func UpgradeForkRules(ctx context.Context, s *database.Store) (traits, rules int, err error) {
	if database.SettingOr(ctx, s.Read(), "this_app", forkRulesDone, "") != "" {
		return 0, 0, nil
	}
	failed := false
	err = s.Write(ctx, func(tx *sql.Tx) error {
		traits, rules, failed = 0, 0, false
		var all []forkRules
		rows, err := tx.QueryContext(ctx, "SELECT id, name, kinds, body FROM pick_rules ORDER BY id")
		if err != nil {
			return err
		}
		for rows.Next() {
			var r forkRules
			var kinds, body string
			if err := rows.Scan(&r.id, &r.name, &kinds, &body); err != nil {
				rows.Close()
				return err
			}
			_ = json.Unmarshal([]byte(kinds), &r.kinds)
			if json.Unmarshal([]byte(body), &r.raw) != nil {
				continue
			}
			all = append(all, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		// Each trait's old app is the app of the Pick rules that give it points.
		apps := map[string]map[string]int{}
		for _, r := range all {
			var tps []TraitPoints
			_ = json.Unmarshal(r.raw["traits"], &tps)
			for _, tp := range tps {
				if apps[tp.Trait] == nil {
					apps[tp.Trait] = map[string]int{}
				}
				apps[tp.Trait][appOf(r.kinds)]++
			}
		}
		now := database.FormatTime(time.Now())
		who := actorJSON(domain.System)
		trows, err := tx.QueryContext(ctx, "SELECT id, name, conditions FROM own_traits WHERE match = 'conditions' ORDER BY id")
		if err != nil {
			return err
		}
		type ftrait struct {
			id    int64
			name  string
			conds []forkCondition
		}
		var fts []ftrait
		for trows.Next() {
			var t ftrait
			var conds string
			if err := trows.Scan(&t.id, &t.name, &conds); err != nil {
				trows.Close()
				return err
			}
			_ = json.Unmarshal([]byte(conds), &t.conds)
			fts = append(fts, t)
		}
		trows.Close()
		if err := trows.Err(); err != nil {
			return err
		}
		for _, t := range fts {
			specs, ok := forkConditionSpecs(t.conds)
			if !ok {
				log.Printf("library: the moved trait %q (%d) has a condition upstream traits cannot test; it is left as it was", t.name, t.id)
				failed = true
				continue
			}
			app, n := "radarr", 0
			for a, c := range apps[OwnTraitKey(t.id)] {
				if c > n || c == n && a == "radarr" {
					app, n = a, c
				}
			}
			b, _ := json.Marshal(specs)
			if _, err := tx.ExecContext(ctx, `UPDATE own_traits SET match = 'name', terms = '[]', source = 'import', source_id = ?, specs = ?,
				updated_at = ?, updated_by = ? WHERE id = ?`, app+":homelab:"+strconv.FormatInt(t.id, 10), string(b), now, who, t.id); err != nil {
				return err
			}
			traits++
		}
		for _, r := range all {
			var like bool
			_ = json.Unmarshal(r.raw["like_old_apps"], &like)
			if !like {
				continue
			}
			body, err := forkRulesBody(r)
			if err == nil {
				err = CheckPickRules(r.name, r.kinds, body)
			}
			if err != nil {
				log.Printf("library: the moved Pick rules %q (%d) could not be upgraded: %v; they are left as they were", r.name, r.id, err)
				failed = true
				continue
			}
			b, _ := json.Marshal(body)
			if _, err := tx.ExecContext(ctx, `UPDATE pick_rules SET body = ?, revision = revision + 1, updated_at = ?, updated_by = ? WHERE id = ?`,
				string(b), now, who, r.id); err != nil {
				return err
			}
			rules++
		}
		if failed {
			return nil
		}
		return database.PutSetting(tx, "this_app", forkRulesDone, now, domain.System, time.Now())
	})
	if err == nil && (traits > 0 || rules > 0) {
		log.Printf("library: upgraded %d moved traits and %d moved Pick rules to the imported form", traits, rules)
	}
	return traits, rules, err
}

// forkRulesBody is the upstream body of fork Pick rules that judged like
// Sonarr and Radarr (see UpgradeForkRules).
func forkRulesBody(r forkRules) (PickRulesBody, error) {
	var body PickRulesBody
	b, _ := json.Marshal(r.raw)
	if err := json.Unmarshal(b, &body); err != nil {
		return body, err
	}
	var noUpgrades bool
	_ = json.Unmarshal(r.raw["no_upgrades"], &noUpgrades)
	var lang struct {
		Wanted []string `json:"wanted"`
		Mode   string   `json:"mode"`
	}
	_ = json.Unmarshal(r.raw["language"], &lang)
	// A step is allowed when any step mapped onto it was.
	allowed := map[string]bool{}
	for _, s := range body.Steps {
		if to := forkStep(s.Quality); to != "" && s.Allowed {
			allowed[to] = true
		}
	}
	steps := make([]Step, len(Ladder))
	first, best := "", ""
	for i, q := range Ladder {
		steps[i] = Step{Quality: q, Allowed: allowed[q]}
		if allowed[q] {
			if first == "" {
				first = q
			}
			best = q
		}
	}
	if first == "" {
		return body, fmt.Errorf("no allowed quality step has a match on the ladder")
	}
	body.Steps = steps
	good := forkStep(body.GoodEnoughAt)
	if !allowed[good] {
		good = best
	}
	body.GoodEnoughAt = good
	if noUpgrades {
		body.GoodEnoughAt = first
		min := body.MinPoints
		body.GoodEnoughPoints = &min
	}
	if len(body.SizeLimits) > 0 {
		sizes := map[string]SizeLimit{}
		for k, l := range body.SizeLimits {
			to := k
			if k != "*" {
				if to = forkStep(k); to == "" {
					continue
				}
			}
			if have, ok := sizes[to]; ok {
				l.Min, l.Max = min(l.Min, have.Min), max(l.Max, have.Max)
			}
			sizes[to] = l
		}
		body.SizeLimits = sizes
	}
	switch lang.Mode {
	case "any":
		body.Language = LanguageRule{Wanted: PickLanguages(), OriginalOK: true}
	case "original":
		body.Language = LanguageRule{Wanted: []string{}, OriginalOK: true}
	case "wanted":
		var want []string
		for _, l := range lang.Wanted {
			if slices.Contains(PickLanguages(), l) {
				want = append(want, l)
			}
		}
		if len(want) == 0 {
			// A language Pick rules cannot name: any language, as the
			// import does for a language with no match here.
			body.Language = LanguageRule{Wanted: PickLanguages(), OriginalOK: true}
		} else {
			body.Language = LanguageRule{Wanted: want}
		}
	default:
		if body.Language.Wanted == nil {
			body.Language.Wanted = []string{}
		}
	}
	app := appOf(r.kinds)
	body.Synced = &SyncedProfile{From: "import", App: app, ProfileID: app + ":homelab:rules:" + strconv.FormatInt(r.id, 10),
		Name: r.name, At: time.Now().UTC().Format(time.RFC3339)}
	return body, nil
}
