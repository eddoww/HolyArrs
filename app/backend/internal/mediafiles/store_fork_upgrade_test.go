package mediafiles

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
)

// liveForkStore is a database as a fork build 1.0.19 left the live one
// (upstream's 0001-0106 and the fork's 9001-9004), with its moved traits and
// Pick rules, then started once with this build's migrations.
func liveForkStore(t *testing.T) *database.Store {
	t.Helper()
	ms, err := database.EmbeddedMigrations("migrations/main")
	if err != nil {
		t.Fatal(err)
	}
	old := fstest.MapFS{}
	for _, m := range ms {
		if m.Version <= 106 || m.Version == 9003 || m.Version == 9004 {
			old[m.Name] = &fstest.MapFile{Data: []byte(m.SQL)}
		}
	}
	for _, n := range []string{"9001_download_handling.sql", "9002_site_sources.sql"} {
		b, err := os.ReadFile("../core/database/testdata/homelab_1019/" + n)
		if err != nil {
			t.Fatal(err)
		}
		old[n] = &fstest.MapFile{Data: b}
	}
	dir := t.TempDir()
	s, err := database.Open(dir, database.Options{MainMigrations: old})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Write(context.Background(), func(tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO own_traits (id, created_at, created_by, updated_at, updated_by, name, match, terms, conditions) VALUES
			 (3, '2026-10-01T00:00:00Z', '{}', '2026-10-01T00:00:00Z', '{}', 'NO: Nordic multi release', 'conditions', '[]', '[{"name":"NORDiC","kind":"release_title","value":"\\b(nordi[ck])\\b","required":true}]'),
			 (5, '2026-10-01T00:00:00Z', '{}', '2026-10-01T00:00:00Z', '{}', 'Not English', 'conditions', '[]', '[{"name":"English","kind":"language","value":"en","except":true,"required":true},{"name":"Not 2160p","kind":"resolution","value":"2160","negate":true}]'),
			 (6, '2026-10-01T00:00:00Z', '{}', '2026-10-01T00:00:00Z', '{}', 'Remux words', 'name', '["remux"]', '[]')`,
			`INSERT INTO pick_rules (id, created_at, created_by, updated_at, updated_by, name, kinds, revision, preset, body) VALUES
			 (8, '2026-10-01T00:00:00Z', '{}', '2026-10-01T00:00:00Z', '{}', 'Nordic Multi-Audio', '["movie"]', 1, 0, '{"steps":[{"quality":"workprint","allowed":false},{"quality":"dvdscr","allowed":true},{"quality":"1080p_web","allowed":true},{"quality":"1080p_bluray","allowed":true},{"quality":"br_disk","allowed":true}],"good_enough_at":"1080p_bluray","traits":[{"trait":"own:3","points":10000}],"must_have":[],"never":[],"min_points":0,"size_limits":{"dvd_r":{"min":1,"max":5,"per_minute":false},"regional":{"min":2,"max":9,"per_minute":false}},"protocol":{"prefer":"either"},"language":{"wanted":["no"],"original_ok":false,"mode":"wanted"},"order":"quality_then_points","upgrade_min_points":1,"proper_policy":{"mode":"take_any"},"like_old_apps":true}'),
			 (12, '2026-10-01T00:00:00Z', '{}', '2026-10-01T00:00:00Z', '{}', 'HD-1080p (Sonarr)', '["show"]', 1, 0, '{"steps":[{"quality":"sd_web","allowed":true},{"quality":"720p_tv","allowed":true},{"quality":"raw_hd","allowed":true},{"quality":"1080p_web","allowed":true}],"good_enough_at":"1080p_web","traits":[{"trait":"own:5","points":-100}],"must_have":[],"never":[],"min_points":-5,"size_limits":{},"protocol":{"prefer":"either"},"language":{"wanted":[],"original_ok":true,"mode":"any"},"order":"quality_then_points","upgrade_min_points":1,"proper_policy":{"mode":"take_any"},"like_old_apps":true,"no_upgrades":true}'),
			 (13, '2026-10-01T00:00:00Z', '{}', '2026-10-01T00:00:00Z', '{}', 'Original only', '["movie"]', 1, 0, '{"steps":[{"quality":"2160p_tv","allowed":true}],"good_enough_at":"2160p_tv","traits":[],"must_have":[],"never":[],"min_points":0,"size_limits":{},"protocol":{"prefer":"either"},"language":{"wanted":[],"original_ok":true,"mode":"original"},"order":"quality_then_points","upgrade_min_points":1,"proper_policy":{"mode":"take_any"},"like_old_apps":true}')`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = database.Open(dir, database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// The live database's moved traits and Pick rules take upstream's imported
// form once: same ids, tests mapped from the conditions, a pass with
// upstream's checks, and the same matches as before.
func TestUpgradeForkRulesFromLive(t *testing.T) {
	s := liveForkStore(t)
	ctx := context.Background()
	traits, rules, err := UpgradeForkRules(ctx, s)
	if err != nil || traits != 2 || rules != 3 {
		t.Fatalf("upgraded %d traits, %d rules: %v", traits, rules, err)
	}
	ts, err := database.Select[OwnTrait](ctx, s.Read(), "own_traits", "1=1 ORDER BY id")
	if err != nil || len(ts) != 3 {
		t.Fatalf("traits %+v %v", ts, err)
	}
	want := map[int64]struct {
		sourceID string
		specs    []OwnSpec
	}{
		3: {"radarr:homelab:3", []OwnSpec{{Name: "NORDiC", Kind: "name", Value: `\b(nordi[ck])\b`, Required: true}}},
		5: {"sonarr:homelab:5", []OwnSpec{{Name: "English", Kind: "language", Value: "!en", Required: true}, {Name: "Not 2160p", Kind: "resolution", Value: "2160", Negate: true}}},
	}
	for _, tr := range ts {
		w, ok := want[tr.ID]
		if !ok {
			if tr.Match != "name" || tr.Source != "" || !slices.Equal(tr.Terms, []string{"remux"}) {
				t.Fatalf("a trait a person made changed: %+v", tr)
			}
			continue
		}
		if tr.Match != "name" || len(tr.Terms) != 0 || tr.Source != "import" || tr.SourceID != w.sourceID || !slices.Equal(tr.Specs, w.specs) || !tr.OldApp() {
			t.Fatalf("trait %d: %+v", tr.ID, tr)
		}
		if !SpecsCompile(tr.Specs) {
			t.Fatalf("trait %d does not compile", tr.ID)
		}
	}
	var conds string
	if err := s.Read().QueryRow("SELECT conditions FROM own_traits WHERE id = 3").Scan(&conds); err != nil || conds == "[]" {
		t.Fatalf("the conditions column must stay: %q %v", conds, err)
	}
	// The judge matches them as the fork did.
	SetOwnTraits(ts)
	t.Cleanup(func() { SetOwnTraits(nil) })
	for _, c := range []struct {
		trait, name string
		lang        string
		want        bool
	}{
		{"own:3", "Heat.1995.NORDiC.1080p.BluRay.x264-GRP", "", true},
		{"own:3", "Heat.1995.1080p.BluRay.x264-GRP", "", false},
		{"own:5", "Les.Choses.2019.FRENCH.1080p.WEB.H264-GRP", "", true},
		{"own:5", "Les.Choses.2019.FRENCH.2160p.WEB.H264-GRP", "", false},
		{"own:5", "Heat.1995.1080p.BluRay.x264-GRP", "en", false},
	} {
		p := releasename.Parse(c.name, releasename.Hints{})
		p.OriginalLanguage = c.lang
		if got, why := traitMatches(c.trait, p); got != c.want {
			t.Errorf("%s on %s: %v (%s), want %v", c.trait, c.name, got, why, c.want)
		}
	}
	prs, err := database.Select[PickRules](ctx, s.Read(), "pick_rules", "id IN (8, 12, 13) ORDER BY id")
	if err != nil || len(prs) != 3 {
		t.Fatalf("pick rules %+v %v", prs, err)
	}
	allowed := func(b PickRulesBody) []string {
		var out []string
		for _, st := range b.Steps {
			if st.Allowed {
				out = append(out, st.Quality)
			}
		}
		return out
	}
	for _, pr := range prs {
		if err := CheckPickRules(pr.Name, pr.Kinds, pr.Body); err != nil {
			t.Fatalf("%s: %v", pr.Name, err)
		}
		var raw map[string]json.RawMessage
		var body string
		_ = s.Read().QueryRow("SELECT body FROM pick_rules WHERE id = ?", pr.ID).Scan(&body)
		_ = json.Unmarshal([]byte(body), &raw)
		var lang map[string]json.RawMessage
		_ = json.Unmarshal(raw["language"], &lang)
		if _, ok := raw["like_old_apps"]; ok || raw["no_upgrades"] != nil || lang["mode"] != nil {
			t.Fatalf("%s keeps the fork's keys: %s", pr.Name, body)
		}
		if pr.Body.Synced == nil || pr.Body.Synced.From != "import" || pr.Body.Synced.Name != pr.Name || !pr.Body.Synced.OldApp() {
			t.Fatalf("%s synced %+v", pr.Name, pr.Body.Synced)
		}
		if pr.Revision != 2 {
			t.Fatalf("%s revision %d", pr.Name, pr.Revision)
		}
	}
	movie, show, orig := prs[0].Body, prs[1].Body, prs[2].Body
	if got := allowed(movie); !slices.Equal(got, []string{"sd_dvd", "1080p_web", "1080p_bluray"}) || movie.GoodEnoughAt != "1080p_bluray" ||
		movie.Synced.App != "radarr" || !slices.Equal(movie.Language.Wanted, []string{"no"}) || movie.Language.OriginalOK ||
		movie.Traits[0] != (TraitPoints{Trait: "own:3", Points: 10000}) {
		t.Fatalf("movie rules %v %+v", got, movie)
	}
	if l := movie.SizeLimits["sd_dvd"]; len(movie.SizeLimits) != 1 || l.Min != 1 || l.Max != 9 {
		t.Fatalf("size limits %+v", movie.SizeLimits)
	}
	if got := allowed(show); !slices.Equal(got, []string{"sd_tv", "720p_tv", "1080p_tv", "1080p_web"}) || show.Synced.App != "sonarr" ||
		show.GoodEnoughAt != "sd_tv" || show.GoodEnoughPoints == nil || *show.GoodEnoughPoints != -5 ||
		!slices.Equal(show.Language.Wanted, PickLanguages()) || !show.Language.OriginalOK {
		t.Fatalf("show rules %v %+v", got, show)
	}
	if got := allowed(orig); !slices.Equal(got, []string{"2160p_web"}) || orig.GoodEnoughAt != "2160p_web" || len(orig.Language.Wanted) != 0 || !orig.Language.OriginalOK {
		t.Fatalf("original-only rules %v %+v", got, orig)
	}
	// Once per install.
	if database.SettingOr(ctx, s.Read(), "this_app", forkRulesDone, "") == "" {
		t.Fatal("the upgrade is not marked done")
	}
	if tr, ru, err := UpgradeForkRules(ctx, s); err != nil || tr != 0 || ru != 0 {
		t.Fatalf("a second run upgraded %d, %d: %v", tr, ru, err)
	}
}
