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
	"unicode"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
	"github.com/HolyArrs/holyarrs/app/backend/internal/library"
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
	// The apps' /language lists, which a live read takes too: their ids
	// differ between the two apps.
	ids := parityRadarrLanguages
	if k == Sonarr {
		ids = paritySonarrLanguages
	}
	sets["languages"] = append(sets["languages"], map[string]any{"id": int64(-2), "name": "Original"}, map[string]any{"id": int64(-1), "name": "Any"})
	for id, code := range ids {
		sets["languages"] = append(sets["languages"], map[string]any{"id": id, "name": releasename.LanguageNames[code]})
	}
	s := arrReader{kind: k}.build(&arrRows{sets: sets})
	s.Kind = k
	return s
}

// parityRadarrLanguages and paritySonarrLanguages are each app's language
// ids, as their /language lists give them.
var (
	parityRadarrLanguages = map[int64]string{1: "en", 2: "fr", 3: "es", 4: "de", 5: "it", 6: "da", 7: "nl", 8: "ja", 9: "is", 10: "zh",
		11: "ru", 12: "pl", 13: "vi", 14: "sv", 15: "no", 16: "fi", 17: "tr", 18: "pt", 19: "nl-be", 20: "el", 21: "ko", 22: "hu",
		23: "he", 24: "lt", 25: "cs", 26: "hi", 27: "ro", 28: "th", 29: "bg", 30: "pt-br", 31: "ar", 32: "uk", 33: "fa", 34: "bn",
		35: "sk", 36: "lv", 37: "es-419", 38: "ca", 39: "hr", 40: "sr", 41: "bs", 42: "et", 43: "ta", 44: "id", 45: "te", 46: "mk",
		47: "sl", 48: "ml", 49: "kn", 50: "sq", 51: "af", 52: "mr", 53: "tl", 54: "ur", 55: "rm", 56: "mn", 57: "ka", 58: "ga"}
	paritySonarrLanguages = map[int64]string{1: "en", 2: "fr", 3: "es", 4: "de", 5: "it", 6: "da", 7: "nl", 8: "ja", 9: "is", 10: "zh",
		11: "ru", 12: "pl", 13: "vi", 14: "sv", 15: "no", 16: "fi", 17: "tr", 18: "pt", 19: "nl-be", 20: "el", 21: "ko", 22: "hu",
		23: "he", 24: "lt", 25: "cs", 26: "ar", 27: "hi", 28: "bg", 29: "ml", 30: "uk", 31: "sk", 32: "th", 33: "pt-br", 34: "es-419",
		35: "ro", 36: "lv", 37: "fa", 38: "ca", 39: "hr", 40: "sr", 41: "bs", 42: "et", 43: "ta", 44: "id", 45: "mk", 46: "sl"}
)

// parityRules plans the move of both apps and sets up what it would make:
// the traits (with ids) and the Pick rules, by app and profile name.
func parityRules(t *testing.T) (*Plan, map[string]mediafiles.PickRulesBody, map[string]string) {
	t.Helper()
	return parityRulesWith(t, nil)
}

// parityRulesWith is parityRules with edit changing each trait first. The
// plan imports each profile as upstream's Pick rules, its scored formats as
// imported traits (tests in specs); they are set up here as the import
// stores them (library.ImportPickRules), without a database.
func parityRulesWith(t *testing.T, edit func(*mediafiles.OwnTrait)) (*Plan, map[string]mediafiles.PickRulesBody, map[string]string) {
	t.Helper()
	p := BuildPlan([]Input{{1, paritySnapshot(t, Radarr)}, {2, paritySnapshot(t, Sonarr)}}, Choices{})
	var traits []mediafiles.OwnTrait
	ids := map[string]int64{}
	names := map[string]string{}
	rules := map[string]mediafiles.PickRulesBody{}
	for _, g := range p.Groups {
		if g.Name != GroupRules {
			continue
		}
		for _, it := range g.Items {
			rp := it.Rules
			if rp == nil || rp.Body == nil {
				continue
			}
			body := *rp.Body
			body.Traits = nil
			for _, it := range rp.Traits {
				id, ok := ids[it.SourceID]
				if !ok {
					id = int64(len(traits) + 1)
					ids[it.SourceID] = id
					tr := mediafiles.OwnTrait{Meta: database.Meta{ID: id}, Name: it.Name, Match: "name", Terms: []string{}, Source: "import", SourceID: it.SourceID, Specs: it.Specs}
					if edit != nil {
						edit(&tr)
					}
					if !mediafiles.SpecsCompile(tr.Specs) {
						t.Fatalf("trait %s does not compile: %+v", tr.Name, tr.Specs)
					}
					traits = append(traits, tr)
					names[mediafiles.OwnTraitKey(id)] = it.Name
				}
				body.Traits = append(body.Traits, mediafiles.TraitPoints{Trait: mediafiles.OwnTraitKey(id), Points: it.Points})
			}
			body.Synced = &mediafiles.SyncedProfile{From: "import", App: rp.App, Name: rp.Name}
			if err := mediafiles.CheckPickRules(rp.Name, rp.Kinds, body); err != nil {
				t.Fatalf("Pick rules %s: %v", rp.Name, err)
			}
			rules[rp.App+":"+rp.Profile] = body
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
	distinct := map[string]bool{}
	for _, n := range names {
		distinct[n] = true
	}
	if len(distinct) != 17 {
		t.Fatalf("%d traits, want 17: %v", len(distinct), names)
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
	if nordic.MinPoints != 10000 || *nordic.GoodEnoughPoints != 10000 || nordic.UpgradeMinPoints != 10000 || !nordic.Language.OriginalOK ||
		!slices.Equal(nordic.Language.Wanted, mediafiles.PickLanguages()) || nordic.GoodEnoughAt != "1080p_bluray" || !nordic.Synced.OldApp() {
		t.Errorf("Nordic Multi-Audio %+v", nordic)
	}
	uhd := rules["radarr:Ultra-HD"]
	if len(uhd.Language.Wanted) != 0 || !uhd.Language.OriginalOK || uhd.GoodEnoughAt != "2160p_remux" || *uhd.GoodEnoughPoints != 4200 || len(uhd.Traits) != 13 {
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
	if a := allowed(uhd); !slices.Equal(a, []string{"2160p_web", "2160p_bluray", "2160p_remux"}) {
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
	// Upstream's ladder has one step for qualities the old apps rank apart
	// (HDTV-2160p and WEBDL-2160p are both 2160p web), so an upgrade from one
	// to the other on the quality alone is not one here.
	whyLadder = "the ladder here joins qualities the old app ranks apart"
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
	type tally struct{ n, formats, score, plain, upgrade, plainN, upgradeN, explained, nordic, ladder int }
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
		fp := p
		fp.Size, fp.OriginalLanguage = r.Size, title.OriginalLanguage
		if r.Kind == "history:downloadFolderImported" {
			// An imported file's formats count its inspected audio
			// languages there; HolyArrs inspects the file too.
			for _, l := range r.Languages {
				fp.AudioLanguages = append(fp.AudioLanguages, code[l])
			}
		}
		for _, k := range traitKeys {
			if ok, _ := mediafiles.TraitMatches(k, fp); ok && !slices.Contains(got, names[k]) {
				got = append(got, names[k])
			}
		}
		sort.Strings(got)
		v := mediafiles.Judge(rel, set, body)
		// The score is the format points, as the app's is, whether or not
		// the judge went on to count them.
		v.Points = mediafiles.PointsOf(fp, body)
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
		set.Current = &mediafiles.Current{Step: library.QualityStep(r.Current.Quality), Points: r.Current.Score, Fix: r.Current.Revision - 1}
		u := mediafiles.Judge(rel, set, body)
		wantUp := !slices.ContainsFunc(r.Rejects, isExisting)
		all.upgradeN++
		if u.Passed == wantUp {
			all.upgrade++
		} else if q := library.QualityStep(r.Quality); q != "" && q == library.QualityStep(r.Current.Quality) && r.Quality != r.Current.Quality {
			all.ladder++
		} else {
			decision(fmt.Sprintf("upgrade %v, want %v (%v; %s)", u.Passed, wantUp, r.Rejects, reasons(u)))
		}
	}
	t.Logf("%d releases: formats agree %d (%.1f%%), score %d (%.1f%%); decisions without a file %d of %d, as an upgrade %d of %d; %d differ on purpose, %d of them as NORDiC or NOR is read as Norwegian audio; %d upgrades between two qualities on one ladder step here (%s)",
		all.n, all.formats, pct(all.formats, all.n), all.score, pct(all.score, all.n), all.plain, all.plainN, all.upgrade, all.upgradeN, all.explained, all.nordic, all.ladder, whyLadder)
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
	p := rel.Parse
	p.OriginalLanguage = "en"
	points := mediafiles.PointsOf(p, rules["radarr:HD-1080p"])
	if v.Passed || points != -1400 || points >= rules["radarr:HD-1080p"].MinPoints || !strings.Contains(fails, "step.not_allowed") {
		t.Fatalf("passed %v, %d points, fails %s", v.Passed, points, reasons(v))
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
			tr.Specs = append(tr.Specs, mediafiles.OwnSpec{Name: "not NORDiC", Kind: "name", Value: `\bnordi[ck]\b|\bNOR\b`, Negate: true, Required: true})
		}
	})
	body = rules["radarr:Nordic Multi-Audio"]
	nordic = points("Paw Patrol The Movie 2021 NORDiC 1080p WEB-DL H 264-NoTrace")
	norwegian = points("Paw Patrol The Movie 2021 NORWEGiAN 1080p WEB-DL H 264-NoTrace")
	if nordic != 10900 || norwegian != 20800 {
		t.Fatalf("with the condition: NORDiC %d, NORWEGiAN %d; want Radarr's 10900 and 20800", nordic, norwegian)
	}
}

// TestArrParityLibraryFileByInspectedAudio: a file in the Library is judged
// by the audio languages its inspection read, as Radarr judges a movie file
// by its media info languages. The moved Nordic Multi-Audio rules are good
// enough at Bluray-1080p (Radarr's cutoff) with 10000 points, which come
// only from a NORDiC or Norwegian name or from Norwegian audio. The names
// are those of real adopted files on the live install.
func TestArrParityLibraryFileByInspectedAudio(t *testing.T) {
	_, rules, _ := parityRules(t)
	body := rules["radarr:Nordic Multi-Audio"]
	info := map[string]any{"resolution": "1080p", "inspection_status": "complete"}
	facts := func(audio ...string) mediafiles.FileFacts {
		return mediafiles.FileFacts{Size: 4 << 30, Audio: audio, OriginalLanguage: "en"}
	}
	owned := func(name string, audio ...string) bool {
		return mediafiles.OwnedReachesGoodEnoughFile(name, info, facts(audio...), body)
	}
	points := func(name string, f mediafiles.FileFacts) int {
		p := releasename.Parse(name, releasename.Hints{})
		p.Size, p.AudioLanguages, p.OriginalLanguage = f.Size, f.Audio, f.OriginalLanguage
		return mediafiles.PointsOf(p, body)
	}
	nordicAudio := []string{"cs", "da", "en", "es", "fi", "fr", "nl", "no", "pl", "sv"}

	// A web copy with Norwegian audio gets the Norwegian points now, and
	// stays below good enough on its step: Radarr's cutoff is Bluray-1080p.
	beauty := "Beauty and the Beast (1991) WEBDL-1080p"
	if n := points(beauty, facts(nordicAudio...)); n < 10000 {
		t.Errorf("%s with Norwegian audio counts %d points, want at least 10000", beauty, n)
	}
	if n := points(beauty, facts()); n >= 10000 {
		t.Errorf("%s with no inspection counts %d points; its name gives no Norwegian", beauty, n)
	}
	if owned(beauty, nordicAudio...) {
		t.Errorf("%s reached good enough below the Bluray-1080p step", beauty)
	}

	bluray := "Beauty and the Beast (1991) Bluray-1080p"
	if !owned(bluray, nordicAudio...) {
		t.Errorf("%s with Norwegian audio does not reach good enough", bluray)
	}
	if owned(bluray) {
		t.Errorf("%s with no inspection reached good enough; its name gives no Norwegian", bluray)
	}
	if owned("Bolt (2008) Bluray-1080p", "da", "en", "sv") {
		t.Error("a Bluray-1080p file without Norwegian audio reached good enough")
	}
	if !owned("Paw Patrol The Movie 2021 NORDiC 1080p BluRay x264-NoTrace", "en") {
		t.Error("a NORDiC Bluray-1080p file does not reach good enough")
	}
}
