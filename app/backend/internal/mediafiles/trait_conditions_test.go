package mediafiles

import (
	"slices"
	"strings"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
)

func condInput(name string, size int64, original string) *traitInput {
	return newTraitInput(releasename.Parse(name, releasename.Hints{}), size, original)
}

// TestTraitConditionsPatterns: release title patterns read as Sonarr and
// Radarr read them: case ignored, look-arounds and word edges kept.
func TestTraitConditionsPatterns(t *testing.T) {
	withOwnTraits(t,
		OwnTrait{Meta: database.Meta{ID: 1}, Name: "HDR10", Match: "conditions", Conditions: []TraitCondition{
			{Name: "HDR10", Kind: "release_title", Value: `(?i)(\bHDR10\b|\bHDR\b(?!ip))`}}},
		OwnTrait{Meta: database.Meta{ID: 2}, Name: "x264", Match: "conditions", Conditions: []TraitCondition{
			{Name: "x264", Kind: "release_title", Value: `(\bx264\b|\bH\.?264\b|\bAVC\b)`}}},
		OwnTrait{Meta: database.Meta{ID: 3}, Name: "Norsk tag", Match: "conditions", Conditions: []TraitCondition{
			{Name: "tag", Kind: "release_title", Value: `\b(norsk|norwegian)\b`, Required: true}}},
	)
	for _, c := range []struct {
		name  string
		trait string
		want  bool
	}{
		{"Dune.Part.Two.2024.2160p.WEB-DL.DV.HDR.H265-GRP", "own:1", true},
		{"Dune.Part.Two.2024.1080p.HDRip.x264-GRP", "own:1", false}, // HDRip is not HDR
		{"Dune.Part.Two.2024.2160p.WEB-DL.hdr10.H265-GRP", "own:1", true},
		{"Big Buck Bunny (2008) 720p Bluray nHD x264-NhaNc3", "own:2", true},
		{"Big Buck Bunny (2008) 720p Bluray nHD X265-NhaNc3", "own:2", false},
		{"Troll 2022 NORWEGIAN 1080p WEBRip x264-WORLD", "own:3", true},
		// The movie's own title is out of reach, as in Radarr.
		{"Norwegian Wood 2010 1080p BluRay x264-GRP", "own:3", false},
	} {
		ok, why := traitMatches(c.trait, condInput(c.name, 0, "en"))
		if ok != c.want {
			t.Errorf("%s %s: %v (%s), want %v", c.trait, c.name, ok, why, c.want)
		}
	}
}

// TestTraitConditionsGroups: conditions of one kind pass when any passes
// and every required one passes; kinds must all pass; negate turns one
// around first.
func TestTraitConditionsGroups(t *testing.T) {
	cs := compileConditions([]TraitCondition{
		{Name: "2160p", Kind: "resolution", Value: "2160"},
		{Name: "1080p", Kind: "resolution", Value: "1080"},
		{Name: "not x264", Kind: "release_title", Value: `\bx264\b`, Negate: true},
		{Name: "web", Kind: "release_title", Value: `\bWEB`},
	})
	for _, c := range []struct {
		name string
		want bool
	}{
		{"Movie 2020 1080p WEB-DL H265-GRP", true},
		{"Movie 2020 2160p WEBRip H265-GRP", true},
		{"Movie 2020 720p WEB-DL H265-GRP", false},  // no resolution passes
		{"Movie 2020 1080p BluRay x264-GRP", false}, // no title condition passes
		{"Movie 2020 1080p WEB-DL x264-GRP", true},  // one title condition passes
	} {
		if ok, why := matchConditions("t", cs, condInput(c.name, 0, "")); ok != c.want {
			t.Errorf("%s: %v (%s), want %v", c.name, ok, why, c.want)
		}
	}
	req := compileConditions([]TraitCondition{
		{Name: "nordic", Kind: "release_title", Value: `\bnordic\b`, Required: true},
		{Name: "eng", Kind: "release_title", Value: `\beng\b`},
	})
	if ok, _ := matchConditions("t", req, condInput("Movie 2020 ENG 1080p WEB-DL-GRP", 0, "")); ok {
		t.Error("a failed required condition must fail its kind")
	}
	if ok, _ := matchConditions("t", req, condInput("Movie 2020 NORDiC 1080p WEB-DL-GRP", 0, "")); !ok {
		t.Error("the required condition alone passes")
	}
}

// TestTraitConditionsLanguageSizeQuality: languages fall back to the
// title's own, "original" stands for it, and source, modifier, size and
// group read the old apps' way.
func TestTraitConditionsLanguageSizeQuality(t *testing.T) {
	test := func(c TraitCondition, name string, size int64, original string) bool {
		ok, _ := compileConditions([]TraitCondition{c})[0].test(condInput(name, size, original))
		return ok
	}
	original := TraitCondition{Kind: "language", Value: "original"}
	norwegian := TraitCondition{Kind: "language", Value: "no"}
	if !test(original, "Coco.2017.1080p.BluRay.x264-GRP", 0, "en") {
		t.Error("a name without a language has the original")
	}
	if test(original, "Oppenheimer.2023.MULTi.FRENCH.1080p.H264-NBDY", 0, "en") {
		t.Error("a French name is not the original English")
	}
	if !test(norwegian, "Coco.2017.NORDiC.1080p.WEB-DL.H264-NoTrace", 0, "en") || !test(original, "Coco.2017.NORDiC.1080p.WEB-DL.H264-NoTrace", 0, "en") {
		t.Error("NORDiC carries Norwegian and the original")
	}
	if !test(TraitCondition{Kind: "language", Value: "en", Except: true}, "Coco.2017.NORDiC.ENG.1080p.WEB-DL-GRP", 0, "en") {
		t.Error("except: a language other than English is there")
	}
	if !test(TraitCondition{Kind: "modifier", Value: "br_disk"}, "Coco 3D 2017 MULTi COMPLETE BLURAY-SharpHD", 0, "") {
		t.Error("a complete Blu-ray is a disc")
	}
	if !test(TraitCondition{Kind: "source", Value: "bluray"}, "Coco 2017 1080p BluRay REMUX AVC-FGT", 0, "") ||
		!test(TraitCondition{Kind: "source", Value: "bluray_raw"}, "Coco 2017 1080p BluRay REMUX AVC-FGT", 0, "") {
		t.Error("a remux is a Blu-ray source, and Sonarr's raw Blu-ray")
	}
	if !test(TraitCondition{Kind: "resolution", Value: "1080"}, "Coco 3D 2017 COMPLETE BLURAY-SharpHD", 0, "") {
		t.Error("a disc counts as 1080p, as in Radarr")
	}
	size := TraitCondition{Kind: "size", MinGB: 1, MaxGB: 10}
	if !test(size, "Movie 2020 1080p WEB-DL-GRP", 10<<30, "") || test(size, "Movie 2020 1080p WEB-DL-GRP", 1<<30, "") {
		t.Error("size is over the least and up to the most")
	}
	if !test(TraitCondition{Kind: "release_group", Value: `^(FLUX|NTb)$`}, "Movie 2020 1080p WEB-DL-NTb", 0, "") {
		t.Error("release group pattern")
	}
}

// TestCheckOwnTraitConditions: patterns HolyArrs cannot read are refused
// when saved, with what is wrong.
func TestCheckOwnTraitConditions(t *testing.T) {
	ok := OwnTrait{Name: "HDR", Match: "conditions", Conditions: []TraitCondition{{Kind: "release_title", Value: `\bHDR\b(?!ip)`}}}
	if err := CheckOwnTrait(&ok); err != nil || ok.Conditions[0].Name != "Condition 1" || len(ok.Terms) != 0 {
		t.Fatalf("lookahead refused: %v %+v", err, ok)
	}
	for _, c := range []TraitCondition{
		{Kind: "release_title", Value: `(unclosed`},
		{Kind: "release_title", Value: ``},
		{Kind: "resolution", Value: "1440"},
		{Kind: "language", Value: "xx"},
		{Kind: "size", MinGB: 5, MaxGB: 1},
		{Kind: "year", Value: "2020"},
	} {
		bad := OwnTrait{Name: "x", Match: "conditions", Conditions: []TraitCondition{c}}
		err := CheckOwnTrait(&bad)
		var fe *FieldError
		if err == nil || !strings.HasPrefix(fieldOf(err, &fe), "conditions") {
			t.Errorf("%+v: %v", c, err)
		}
	}
	if err := CheckOwnTrait(&OwnTrait{Name: "x", Match: "conditions"}); err == nil {
		t.Error("no conditions accepted")
	}
}

func fieldOf(err error, fe **FieldError) string {
	if e, ok := err.(*FieldError); ok {
		*fe = e
		return e.Field
	}
	return ""
}

// TestPatternTimeout: a pattern that backtracks without end stops at the
// match time limit and does not match.
func TestPatternTimeout(t *testing.T) {
	re, err := compilePattern(`^(a+)+$`)
	if err != nil {
		t.Fatal(err)
	}
	ok, why := patternMatch(re, strings.Repeat("a", 40)+"b")
	if ok || why != "took too long" {
		t.Fatalf("%v %q", ok, why)
	}
}

// TestPresetStepsUnchanged: the steps a name takes under the ready-made
// Pick rules are HolyArrs's own reading, whatever the old apps would say,
// and their ladders hold none of the steps only old-app rules use.
func TestPresetStepsUnchanged(t *testing.T) {
	names := []string{
		"Coco 2017 2160p UHD Blu-ray HEVC TrueHD 7 1-WhiteRhino", "Coco 3D 2017 MULTi COMPLETE BLURAY-SharpHD",
		"From S01E01 1080p HEVC x265-MeGusta", "The.Last.of.Us.S01E01.WEB.x264-TORRENTGALAXY", "Coco.2017.BRRip.XViD-ETRG",
		"Dune Part Two 2024 576p WEBRip x265 AAC-SSN", "Oppenheimer 2023 2160p HDTV x265-GRP", "Coco (2017) DVD9",
		"Movie.2020.1080p.WEB-DL.DDP5.1.H.265-GRP", "Movie.2020.2160p.BluRay.REMUX.HEVC-GRP",
	}
	presets := map[string]PickRulesBody{GoodEverydayName: GoodEveryday(), SaveSpaceName: SaveSpace(), BestName: Best(), FourKName: FourK()}
	for pn, rules := range presets {
		for _, s := range rules.Steps {
			if !slices.Contains(Ladder, s.Quality) {
				t.Errorf("%s has step %s", pn, s.Quality)
			}
		}
		for _, n := range names {
			p := releasename.Parse(n, releasename.Hints{})
			step, idx := ruleStep(rules, p)
			if want := StepFor(p); step != want || (want != "" && idx != stepIndex(rules, want)) {
				t.Errorf("%s %s: %s, want %s", pn, n, step, want)
			}
		}
		if err := CheckPickRules(pn, []string{"movie"}, withStep(rules, "br_disk")); err == nil {
			t.Errorf("%s: an old-app step is refused without like_old_apps", pn)
		}
	}
}

func withStep(b PickRulesBody, step string) PickRulesBody {
	b.Steps = append(slices.Clone(b.Steps), Step{Quality: step})
	return b
}
