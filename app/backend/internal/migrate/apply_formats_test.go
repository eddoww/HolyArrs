package migrate

import (
	"context"
	"database/sql"
	"strconv"
	"testing"

	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
	"github.com/HolyArrs/holyarrs/app/backend/internal/library"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

// formatsLab is the move lab with a Radarr profile read in full and two
// custom formats, one with a look-ahead pattern.
func formatsLab(t *testing.T) *moveLab {
	return newMoveLabWith(t, func(radarr, _, _ *Snapshot) {
		radarr.CustomFormats = []Named{
			{ID: 1, Name: "HDR10", Items: []map[string]any{{"name": "HDR10", "kind": "ReleaseTitleSpecification", "value.value": `(?i)(\bHDR10\b|\bHDR\b(?!ip))`}}},
			{ID: 2, Name: "x264", Items: []map[string]any{{"name": "x264", "kind": "ReleaseTitleSpecification", "value.value": `(?i)\bx264\b`}}},
		}
		lang := int64(-2)
		radarr.QualityProfiles = []Profile{{ID: 2, Name: "Ultra-HD", Cutoff: "Remux-2160p", UpgradeAllowed: true, CutoffScore: 400, MinUpgradeScore: 100,
			Language: &lang, Scores: map[string]int{"HDR10": 500, "x264": -1500},
			Steps: []ProfileStep{{Quality: "HDTV-1080p"}, {Quality: "WEBDL-2160p", Group: "WEB 2160p", Allowed: true},
				{Quality: "WEBRip-2160p", Group: "WEB 2160p", Allowed: true}, {Quality: "Bluray-2160p", Allowed: true}, {Quality: "Remux-2160p", Allowed: true}}}}
	})
}

// TestMoveFormatsAndProfile: the move makes the traits and the Pick rules,
// the title uses them, a second move adds nothing, and undo takes them out.
func TestMoveFormatsAndProfile(t *testing.T) {
	l := formatsLab(t)
	mv := l.move(t, Choices{})
	if mv.State != "done" {
		t.Fatalf("move %+v", mv)
	}
	if n := l.count(t, "SELECT count(*) FROM own_traits WHERE match = 'conditions'"); n != 2 {
		t.Fatalf("traits %d", n)
	}
	var hdrID int64
	if err := l.st.Read().QueryRow("SELECT id FROM own_traits WHERE name = 'HDR10'").Scan(&hdrID); err != nil {
		t.Fatal(err)
	}
	var rules string
	if err := l.st.Read().QueryRow(`SELECT p.name FROM titles t JOIN versions v ON v.title_id = t.id JOIN pick_rules p ON p.id = v.pick_rules_id
		WHERE json_extract(t.source_ids, '$.tmdb') = '603'`).Scan(&rules); err != nil || rules != "Ultra-HD" {
		t.Fatalf("title rules %q %v", rules, err)
	}
	if n := l.count(t, `SELECT count(*) FROM pick_rules WHERE name = 'Ultra-HD' AND json_extract(body, '$.like_old_apps') = 1
		AND json_extract(body, '$.language.mode') = 'original' AND json_extract(body, '$.good_enough_at') = '2160p_remux'
		AND json_extract(body, '$.good_enough_points') = 400 AND body LIKE ?`, `%"own:`+strconv.FormatInt(hdrID, 10)+`","points":500%`); n != 1 {
		t.Fatal("Pick rules not made from the profile")
	}
	again := l.move(t, Choices{})
	if again.Report.Counts[ResCreated] != 0 {
		t.Fatalf("second move made things: %+v", again.Report.Counts)
	}
	if n := l.count(t, "SELECT count(*) FROM own_traits"); n != 2 {
		t.Fatalf("traits after a second move %d", n)
	}
	l.undo(t, again.ID)
	u := l.undo(t, mv.ID)
	if u.State != "undone" {
		t.Fatalf("undo %+v", u)
	}
	if n := l.count(t, "SELECT count(*) FROM own_traits"); n != 0 {
		t.Fatalf("traits after undo %d", n)
	}
}

// TestMoveFormatsKeepNames: a trait or Pick rules of yours with the same
// name keeps it; the moved one is named "(moved)". A trait of yours with
// the same conditions is used instead of a second one.
func TestMoveFormatsKeepNames(t *testing.T) {
	l := formatsLab(t)
	mustExec(t, l, `INSERT INTO own_traits (created_at, created_by, updated_at, updated_by, name, match, terms, conditions)
		VALUES ('2026-10-01T00:00:00Z', '{}', '2026-10-01T00:00:00Z', '{}', 'HDR10', 'name', '["HDR"]', '[]'),
		('2026-10-01T00:00:00Z', '{}', '2026-10-01T00:00:00Z', '{}', 'Mine x264', 'conditions', '[]', '[{"name":"x","kind":"release_title","value":"(?i)\\bx264\\b"}]')`)
	mustExec(t, l, `INSERT INTO pick_rules (created_at, created_by, updated_at, updated_by, name, kinds, revision, preset, body)
		SELECT created_at, created_by, updated_at, updated_by, 'Ultra-HD', kinds, 1, 0, body FROM pick_rules WHERE name = 'Standard'`)
	mv := l.move(t, Choices{})
	if mv.State != "done" {
		t.Fatalf("move %+v", mv)
	}
	if n := l.count(t, "SELECT count(*) FROM own_traits WHERE name = 'HDR10 (moved)' AND match = 'conditions'"); n != 1 {
		t.Fatalf("the moved HDR10 trait is not named (moved): %+v", mv.Report.Lines)
	}
	if n := l.count(t, "SELECT count(*) FROM own_traits"); n != 3 {
		t.Fatalf("the x264 format must use your trait: %d traits", n)
	}
	var rules string
	if err := l.st.Read().QueryRow(`SELECT p.name FROM titles t JOIN versions v ON v.title_id = t.id JOIN pick_rules p ON p.id = v.pick_rules_id
		WHERE json_extract(t.source_ids, '$.tmdb') = '603'`).Scan(&rules); err != nil || rules != "Ultra-HD (moved)" {
		t.Fatalf("title rules %q %v", rules, err)
	}
}

func mustExec(t *testing.T, l *moveLab, q string, args ...any) {
	t.Helper()
	if err := l.st.Write(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec(q, args...); return err }); err != nil {
		t.Fatal(err)
	}
}

// TestMoveAgainAfterUndo: undo keeps the Pick rules and the Library folder
// that the removed titles' History still uses; the next move takes them
// back, with the trait scores of the traits it makes again, instead of
// making "(moved)" sets. Rules you changed since the undo stay yours: the
// move then makes its own and points the kept folder at them.
func TestMoveAgainAfterUndo(t *testing.T) {
	l := newMoveLabWith(t, func(radarr, _, seerr *Snapshot) {
		radarr.CustomFormats = []Named{
			{ID: 1, Name: "HDR10", Items: []map[string]any{{"name": "HDR10", "kind": "ReleaseTitleSpecification", "value.value": `(?i)(\bHDR10\b|\bHDR\b(?!ip))`}}},
			{ID: 2, Name: "x264", Items: []map[string]any{{"name": "x264", "kind": "ReleaseTitleSpecification", "value.value": `(?i)\bx264\b`}}},
		}
		radarr.QualityProfiles = []Profile{{ID: 2, Name: "Ultra-HD", Cutoff: "Remux-2160p", UpgradeAllowed: true, CutoffScore: 400, MinUpgradeScore: 100,
			Scores: map[string]int{"HDR10": 500, "x264": -1500},
			Steps:  []ProfileStep{{Quality: "WEBDL-2160p", Allowed: true}, {Quality: "Remux-2160p", Allowed: true}}}}
		seerr.RequestServers = []RequestServer{{Kind: "movie", Name: "Radarr", Default: true, Profile: "Ultra-HD", Folder: "/movies"}}
	})
	folderRules := func() int64 {
		var id int64
		if err := l.st.Read().QueryRow("SELECT ifnull(default_pick_rules_id, 0) FROM library_folders").Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	titleRules := func() int64 {
		var id int64
		if err := l.st.Read().QueryRow(`SELECT v.pick_rules_id FROM titles t JOIN versions v ON v.title_id = t.id
			WHERE t.state != 'removed' AND json_extract(t.source_ids, '$.tmdb') = '603'`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	// scored says the Pick rules give HDR10 its 500 points with the trait
	// that is here now.
	scored := func(rules int64) bool {
		var hdr int64
		if err := l.st.Read().QueryRow("SELECT id FROM own_traits WHERE name = 'HDR10'").Scan(&hdr); err != nil {
			return false
		}
		return l.count(t, "SELECT count(*) FROM pick_rules WHERE id = ? AND body LIKE ?", rules, `%"own:`+strconv.FormatInt(hdr, 10)+`","points":500%`) == 1
	}
	mine := "SELECT count(*) FROM pick_rules WHERE preset = 0"

	first := l.move(t, Choices{})
	rules := titleRules()
	if first.State != "done" || folderRules() != rules || !scored(rules) {
		t.Fatalf("first move %s: folder rules %d, title rules %d", first.State, folderRules(), rules)
	}
	if u := l.undo(t, first.ID); u.State != "undone" || l.count(t, mine) != 1 || l.count(t, "SELECT count(*) FROM own_traits") != 0 {
		t.Fatalf("undo %s, %d own Pick rules, %d traits", u.State, l.count(t, mine), l.count(t, "SELECT count(*) FROM own_traits"))
	}
	for round := 2; round <= 3; round++ {
		mv := l.move(t, Choices{})
		if mv.State != "done" || l.count(t, mine) != 1 || l.count(t, "SELECT count(*) FROM pick_rules WHERE name LIKE '%(moved%'") != 0 {
			t.Fatalf("move %d %s: %d own Pick rules", round, mv.State, l.count(t, mine))
		}
		if titleRules() != rules || folderRules() != rules || !scored(rules) {
			t.Fatalf("move %d: title rules %d, folder rules %d, want %d with scores", round, titleRules(), folderRules(), rules)
		}
		if res := lineOf(t, mv.Report, it(t, l, GroupRules, func(i Item) bool { return i.Rules != nil && i.Rules.Make != nil }).Key).Result; res != ResChanged {
			t.Fatalf("move %d: the kept Pick rules line is %s", round, res)
		}
		if u := l.undo(t, mv.ID); u.State != "undone" || l.count(t, "SELECT count(*) FROM own_traits") != 0 {
			t.Fatalf("undo %d %s", round, u.State)
		}
	}

	if _, err := library.UpdatePickRules(context.Background(), l.st, rules, library.PickRulesInput{Name: "Ultra-HD", Kinds: []string{"movie"}, Body: mediafiles.GoodEveryday()}, domain.System); err != nil {
		t.Fatal(err)
	}
	mv := l.move(t, Choices{})
	made := titleRules()
	if mv.State != "done" || made == rules || !scored(made) || folderRules() != made || l.count(t, "SELECT count(*) FROM pick_rules WHERE id = ? AND name = 'Ultra-HD (moved)'", made) != 1 {
		t.Fatalf("after your change: title rules %d (kept %d), folder rules %d", made, rules, folderRules())
	}
}

// it is the first planned item of a group that ok accepts.
func it(t *testing.T, l *moveLab, group string, ok func(Item) bool) Item {
	t.Helper()
	for _, g := range l.plan(t).Groups {
		if g.Name != group {
			continue
		}
		for _, i := range g.Items {
			if ok(i) {
				return i
			}
		}
	}
	t.Fatalf("no %s item", group)
	return Item{}
}
