package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

// The parity fixture (testdata/arr_parity) is read, without changing
// anything, from a real Radarr 6 and Sonarr 4: their custom formats and
// quality profiles as the API gives them, and releases with what those
// apps decided about each: interactive search results (with the file the
// title had) and grab and import history. Download links and keys are not
// in it.
type parityRow struct {
	App       string         `json:"app"`
	Kind      string         `json:"kind"`
	Title     string         `json:"title"`
	Year      int            `json:"year"`
	Original  string         `json:"original_language"`
	Profile   string         `json:"profile"`
	Release   string         `json:"release"`
	Size      int64          `json:"size"`
	Languages []string       `json:"languages"`
	Quality   string         `json:"quality"`
	Revision  int            `json:"revision"`
	Formats   []string       `json:"formats"`
	Score     int            `json:"score"`
	Approved  *bool          `json:"approved"`
	Rejects   []string       `json:"rejections"`
	Current   *parityCurrent `json:"current"`
}

type parityCurrent struct {
	Quality  string `json:"quality"`
	Revision int    `json:"revision"`
	Score    int    `json:"score"`
}

func parityJSON(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "arr_parity", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// paritySnapshot reads an app's formats and profiles the way a live read
// does.
func paritySnapshot(t *testing.T, k Kind) *Snapshot {
	t.Helper()
	var formats, profiles []map[string]any
	parityJSON(t, string(k)+"_formats.json", &formats)
	parityJSON(t, string(k)+"_profiles.json", &profiles)
	sets := map[string][]map[string]any{}
	for _, f := range formats {
		sets["formats"] = append(sets["formats"], lower(f))
	}
	for _, p := range profiles {
		sets["profiles"] = append(sets["profiles"], lower(p))
	}
	s := arrReader{kind: k}.build(&arrRows{sets: sets})
	s.Kind = k
	return s
}

// parityRules plans the move of both apps and sets up what it would make:
// the traits (with ids) and the Pick rules, by app and profile name.
func parityRules(t *testing.T) (*Plan, map[string]mediafiles.PickRulesBody, map[string]string) {
	t.Helper()
	return parityRulesWith(t, nil)
}

// parityRulesWith is parityRules with edit changing each trait first.
func parityRulesWith(t *testing.T, edit func(*mediafiles.OwnTrait)) (*Plan, map[string]mediafiles.PickRulesBody, map[string]string) {
	t.Helper()
	p := BuildPlan([]Input{{1, paritySnapshot(t, Radarr)}, {2, paritySnapshot(t, Sonarr)}}, Choices{})
	var traits []mediafiles.OwnTrait
	ownKey := map[string]string{}
	names := map[string]string{}
	rules := map[string]mediafiles.PickRulesBody{}
	for _, g := range p.Groups {
		if g.Name != GroupRules {
			continue
		}
		for _, it := range g.Items {
			if it.Trait != nil {
				id := int64(len(traits) + 1)
				tr := mediafiles.OwnTrait{Meta: database.Meta{ID: id}, Name: it.Trait.Name, Match: "conditions", Conditions: it.Trait.Conditions}
				if edit != nil {
					edit(&tr)
				}
				if err := mediafiles.CheckOwnTrait(&tr); err != nil {
					t.Fatalf("trait %s: %v", it.Name, err)
				}
				traits = append(traits, tr)
				ownKey[it.Key] = mediafiles.OwnTraitKey(id)
				names[mediafiles.OwnTraitKey(id)] = it.Trait.Name
			}
		}
		for _, it := range g.Items {
			if it.Rules == nil || it.Rules.Make == nil {
				continue
			}
			body := it.Rules.Make.Body
			for _, s := range it.Rules.Make.Scores {
				body.Traits = append(body.Traits, mediafiles.TraitPoints{Trait: ownKey[s.Trait], Points: s.Points})
			}
			if err := mediafiles.CheckPickRules(it.Name, it.Rules.Make.Kinds, body); err != nil {
				t.Fatalf("Pick rules %s: %v", it.Name, err)
			}
			rules[it.Rules.App+":"+it.Rules.Profile] = body
		}
	}
	mediafiles.SetOwnTraits(traits)
	t.Cleanup(func() { mediafiles.SetOwnTraits(nil) })
	return p, rules, names
}

// TestArrParityPlan: all 17 formats of each app move once (both apps hold
// the same ones), every profile becomes Pick rules, and no format or
// score is left on the not-moved list.
func TestArrParityPlan(t *testing.T) {
	p, rules, names := parityRules(t)
	if len(names) != 17 {
		t.Fatalf("%d traits, want 17: %v", len(names), names)
	}
	for _, n := range p.NotMoved {
		if strings.Contains(n.From, "custom_formats") || strings.Contains(n.From, "quality_profiles") {
			t.Errorf("not moved: %+v", n)
		}
	}
	for _, k := range []string{"radarr:Nordic Multi-Audio", "radarr:Ultra-HD", "radarr:HD-1080p", "sonarr:Ultra-HD", "sonarr:HD-1080p"} {
		if _, ok := rules[k]; !ok {
			t.Errorf("no Pick rules for %s", k)
		}
	}
	nordic := rules["radarr:Nordic Multi-Audio"]
	if nordic.MinPoints != 10000 || *nordic.GoodEnoughPoints != 10000 || nordic.UpgradeMinPoints != 10000 || nordic.Language.Mode != "any" ||
		nordic.GoodEnoughAt != "1080p_bluray" || !nordic.LikeOldApps || nordic.NoUpgrades {
		t.Errorf("Nordic Multi-Audio %+v", nordic)
	}
	uhd := rules["radarr:Ultra-HD"]
	if uhd.Language.Mode != "original" || uhd.GoodEnoughAt != "2160p_remux" || *uhd.GoodEnoughPoints != 4200 || len(uhd.Traits) != 13 {
		t.Errorf("Ultra-HD %+v", uhd)
	}
	allowed := func(b mediafiles.PickRulesBody) []string {
		var out []string
		for _, s := range b.Steps {
			if s.Allowed {
				out = append(out, s.Quality)
			}
		}
		return out
	}
	if a := allowed(uhd); !slices.Equal(a, []string{"2160p_tv", "2160p_web", "2160p_bluray", "2160p_remux"}) {
		t.Errorf("Ultra-HD steps %v", a)
	}
	if a := allowed(rules["sonarr:HD-1080p"]); !slices.Equal(a, []string{"1080p_tv", "1080p_web", "1080p_bluray"}) {
		t.Errorf("Sonarr HD-1080p steps %v", a)
	}
}

// explained says why HolyArrs reads a release unlike the old app on
// purpose ("" when it should not differ). Radarr reads no language from
// NORDiC or NOR, so a Nordic release counts for it only as the original
// language; HolyArrs reads the Nordic dubs these names announce. Radarr
// also takes ESub and MSubs for burnt-in subtitles; HolyArrs reads them as
// subtitle tracks.
func explained(r parityRow, got []string) string {
	var extra, missing []string
	for _, f := range got {
		if !slices.Contains(r.Formats, f) {
			extra = append(extra, f)
		}
	}
	for _, f := range r.Formats {
		if !slices.Contains(got, f) {
			missing = append(missing, f)
		}
	}
	nordic := func(f string) bool { return f == "NO: Norwegian audio" || f == "Preferred: Original language" }
	words := strings.FieldsFunc(r.Release, func(c rune) bool { return !unicode.IsLetter(c) })
	switch {
	case len(missing) == 0 && len(extra) > 0 && !slices.ContainsFunc(extra, func(f string) bool { return !nordic(f) }) &&
		slices.ContainsFunc(words, func(w string) bool { return strings.EqualFold(w, "nordic") || w == "NOR" }):
		return whyNordic
	case len(extra) == 0 && len(missing) == 0 && slices.ContainsFunc(r.Rejects, func(x string) bool { return strings.HasPrefix(x, "Hardcode subs") }):
		return whyESub
	}
	return ""
}

// The readings that differ on purpose. Reading NORDiC and NOR as Norwegian
// audio adds the points of a Norwegian audio format (10000 in the Nordic
// Multi-Audio profile), so a NORDiC release can rank above a NORWEGiAN one
// that Radarr scores the same; it never changes whether a release is
// taken. Reading ESub as a subtitle track takes a release Radarr refuses
// for hardcoded subtitles.
const (
	whyNordic = "NORDiC and NOR are read as Norwegian audio"
	whyESub   = "ESub and MSubs are read as subtitle tracks, not burnt-in subtitles"
)

// modelled says whether a rejection is one Pick rules make (quality,
// format score, language, upgrades, hardcoded subtitles); title matching,
// seeders and the like are judged elsewhere.
func modelled(rejection string) bool {
	for _, p := range []string{"Unknown Movie", "Unknown Series", "Unable to parse", "Not enough seeders", "Hardcode subs", "Release is", "Has been", "Episode", "Movie", "Already"} {
		if strings.HasPrefix(rejection, p) {
			return false
		}
	}
	return true
}

func isExisting(rejection string) bool { return strings.HasPrefix(rejection, "Existing file") }

// TestArrParity judges every fixture release with the moved formats and
// Pick rules and compares the format matches, the score and the decision
// with what Radarr or Sonarr gave.
func TestArrParity(t *testing.T) {
	_, rules, names := parityRules(t)
	var rows []parityRow
	parityJSON(t, "releases.json", &rows)
	code := map[string]string{}
	for c, n := range releasename.LanguageNames {
		code[n] = c
	}
	var traitKeys []string
	for k := range names {
		traitKeys = append(traitKeys, k)
	}
	sort.Strings(traitKeys)
	type tally struct{ n, formats, score, plain, upgrade, plainN, upgradeN, explained, nordic int }
	var all tally
	var diffs []string
	for _, r := range rows {
		if slices.ContainsFunc(r.Rejects, func(x string) bool {
			return !modelled(x) && !strings.HasPrefix(x, "Hardcode") && !strings.HasPrefix(x, "Not enough")
		}) {
			continue // matched no title there: not a Pick rules question
		}
		body, ok := rules[r.App+":"+r.Profile]
		if !ok {
			t.Fatalf("no rules for %s %s", r.App, r.Profile)
		}
		title := &mediafiles.TitleRef{ID: 1, Name: r.Title, Year: r.Year, OriginalLanguage: code[r.Original]}
		if r.App == "sonarr" {
			title.Kind = "show"
		}
		p := releasename.Parse(r.Release, releasename.Hints{Show: r.App == "sonarr"})
		if r.Kind == "search" && mediafiles.MatchTitle(p, []mediafiles.TitleRef{*title}).Title == nil {
			continue // a release for another title, which the old app scored without one
		}
		rel := mediafiles.ReleaseFacts{Name: r.Release, Parse: p, Size: r.Size, Protocol: "torrent"}
		set := mediafiles.SlotSet{Match: mediafiles.Match{Title: title, Conf: releasename.Sure}, SlotIDs: []int64{1}, Out: true}
		var got []string
		facts := mediafiles.FileFacts{Size: r.Size, OriginalLanguage: title.OriginalLanguage}
		if r.Kind == "history:downloadFolderImported" {
			// An imported file's formats count its inspected audio
			// languages there; HolyArrs inspects the file too.
			for _, l := range r.Languages {
				facts.Languages = append(facts.Languages, code[l])
			}
		}
		for _, k := range traitKeys {
			if ok, _ := mediafiles.TraitMatches(k, p, facts); ok {
				got = append(got, names[k])
			}
		}
		sort.Strings(got)
		v := mediafiles.Judge(rel, set, body)
		if r.Kind != "search" {
			v.Points = mediafiles.CurrentOfFile(r.Release, facts, time.Time{}, body).Points
		}
		all.n++
		fOK := slices.Equal(got, r.Formats)
		sOK := v.Points == r.Score
		if fOK {
			all.formats++
		}
		if sOK {
			all.score++
		}
		why := explained(r, got)
		if why != "" {
			all.explained++
		}
		if why == whyNordic {
			all.nordic++
		}
		line := func(what string) {
			if why == "" {
				diffs = append(diffs, fmt.Sprintf("%s %s %q: %s", r.App, r.Profile, r.Release, what))
			}
		}
		// Reading NORDiC as Norwegian audio may change formats and score,
		// never whether the release is taken.
		decision := func(what string) {
			if why != whyESub {
				diffs = append(diffs, fmt.Sprintf("%s %s %q: %s", r.App, r.Profile, r.Release, what))
			}
		}
		if !fOK {
			line(fmt.Sprintf("formats %v, want %v", got, r.Formats))
		} else if !sOK {
			line(fmt.Sprintf("score %d, want %d", v.Points, r.Score))
		}
		if r.Kind != "search" {
			continue
		}
		// Without the file the title has: refused when the app gave a
		// rejection other than about that file.
		wantRefused := slices.ContainsFunc(r.Rejects, func(x string) bool { return !isExisting(x) && !strings.HasPrefix(x, "Not enough") })
		all.plainN++
		if gotRefused := !v.Passed; gotRefused == wantRefused {
			all.plain++
		} else {
			decision(fmt.Sprintf("refused %v, want %v (%v; %s)", gotRefused, wantRefused, r.Rejects, reasons(v)))
		}
		// With the file the title had: chosen only as an upgrade.
		if r.Current == nil || wantRefused {
			continue
		}
		set.Current = &mediafiles.Current{Step: mediafiles.OldAppStep(r.Current.Quality), Points: r.Current.Score, Fix: r.Current.Revision - 1}
		u := mediafiles.Judge(rel, set, body)
		wantUp := !slices.ContainsFunc(r.Rejects, isExisting)
		all.upgradeN++
		if u.Passed == wantUp {
			all.upgrade++
		} else {
			decision(fmt.Sprintf("upgrade %v, want %v (%v; %s)", u.Passed, wantUp, r.Rejects, reasons(u)))
		}
	}
	t.Logf("%d releases: formats agree %d (%.1f%%), score %d (%.1f%%); decisions without a file %d of %d, as an upgrade %d of %d; %d differ on purpose, %d of them as NORDiC or NOR is read as Norwegian audio",
		all.n, all.formats, pct(all.formats, all.n), all.score, pct(all.score, all.n), all.plain, all.plainN, all.upgrade, all.upgradeN, all.explained, all.nordic)
	for _, d := range diffs {
		t.Error(d)
	}
}

func pct(a, b int) float64 { return 100 * float64(a) / float64(max(b, 1)) }

func reasons(v mediafiles.Verdict) string {
	var out []string
	for _, r := range v.Reasons {
		if r.Effect == "fail" {
			out = append(out, r.Code()+": "+r.Detail)
		}
	}
	return strings.Join(out, "; ")
}

// TestArrParityBigBuckBunny: the known case. Radarr refuses this release
// for HD-1080p: Bluray-720p is not allowed and x264 takes 1500 points,
// leaving -1400 under the least of 0.
func TestArrParityBigBuckBunny(t *testing.T) {
	_, rules, _ := parityRules(t)
	name := "Big Buck Bunny (2008) 720p Bluray nHD x264-NhaNc3"
	title := &mediafiles.TitleRef{ID: 1, Name: "Big Buck Bunny", Year: 2008, OriginalLanguage: "en"}
	rel := mediafiles.ReleaseFacts{Name: name, Parse: releasename.Parse(name, releasename.Hints{}), Size: 219538056, Protocol: "torrent"}
	v := mediafiles.Judge(rel, mediafiles.SlotSet{Match: mediafiles.Match{Title: title, Conf: releasename.Sure}, SlotIDs: []int64{1}, Out: true}, rules["radarr:HD-1080p"])
	fails := strings.Join(failCodes(v), " ")
	if v.Passed || v.Points != -1400 || !strings.Contains(fails, "step.not_allowed") || !strings.Contains(fails, "points.below_min") {
		t.Fatalf("passed %v, %d points, fails %s", v.Passed, v.Points, reasons(v))
	}
	sub := false
	for _, r := range v.Reasons {
		sub = sub || (r.Effect == "subtract" && r.Points == -1500 && strings.HasPrefix(r.Detail, "Reject: x264 H264 AVC"))
	}
	if !sub {
		t.Fatalf("no x264 line: %+v", v.Reasons)
	}
}

func failCodes(v mediafiles.Verdict) []string {
	var out []string
	for _, r := range v.Reasons {
		if r.Effect == "fail" {
			out = append(out, r.Code())
		}
	}
	return out
}

// TestArrParityNordicReadAsNorwegianAudio pins the one reading that ranks
// unlike Radarr in the Nordic Multi-Audio profile. Radarr reads no
// language from NORDiC, so it gives a NORDiC release the Nordic multi
// release and original language points (10900 here) and a NORWEGiAN one
// the Norwegian title tag and audio points (20800). HolyArrs reads the
// Norwegian dub NORDiC announces, so the NORDiC release also gets the
// Norwegian audio points and ranks 100 above the NORWEGiAN one. Both pass
// in either app. The condition pick-rules.md gives to rank as Radarr does
// brings the NORDiC release back to Radarr's points.
func TestArrParityNordicReadAsNorwegianAudio(t *testing.T) {
	_, rules, _ := parityRules(t)
	body := rules["radarr:Nordic Multi-Audio"]
	title := &mediafiles.TitleRef{ID: 1, Name: "Paw Patrol The Movie", Year: 2021, OriginalLanguage: "en"}
	points := func(name string) int {
		t.Helper()
		rel := mediafiles.ReleaseFacts{Name: name, Parse: releasename.Parse(name, releasename.Hints{}), Size: 4 << 30, Protocol: "torrent"}
		v := mediafiles.Judge(rel, mediafiles.SlotSet{Match: mediafiles.Match{Title: title, Conf: releasename.Sure}, SlotIDs: []int64{1}, Out: true}, body)
		if !v.Passed {
			t.Fatalf("%s refused: %s", name, reasons(v))
		}
		return v.Points
	}
	nordic := points("Paw Patrol The Movie 2021 NORDiC 1080p WEB-DL H 264-NoTrace")
	norwegian := points("Paw Patrol The Movie 2021 NORWEGiAN 1080p WEB-DL H 264-NoTrace")
	if nordic != 20900 || norwegian != 20800 {
		t.Fatalf("NORDiC %d, NORWEGiAN %d; want 20900 and 20800 (Radarr: 10900 and 20800)", nordic, norwegian)
	}

	_, rules, _ = parityRulesWith(t, func(tr *mediafiles.OwnTrait) {
		if tr.Name == "NO: Norwegian audio" {
			tr.Conditions = append(tr.Conditions, mediafiles.TraitCondition{Name: "not NORDiC", Kind: "release_title", Value: `\bnordi[ck]\b|\bNOR\b`, Negate: true, Required: true})
		}
	})
	body = rules["radarr:Nordic Multi-Audio"]
	nordic = points("Paw Patrol The Movie 2021 NORDiC 1080p WEB-DL H 264-NoTrace")
	norwegian = points("Paw Patrol The Movie 2021 NORWEGiAN 1080p WEB-DL H 264-NoTrace")
	if nordic != 10900 || norwegian != 20800 {
		t.Fatalf("with the condition: NORDiC %d, NORWEGiAN %d; want Radarr's 10900 and 20800", nordic, norwegian)
	}
}
