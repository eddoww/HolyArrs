package mediafiles

import (
	"slices"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
)

// oldAppRules is a profile like Radarr's HD-1080p: 1080p steps allowed,
// good enough at Blu-ray 1080p and 2600 points, upgrades of 100 points.
func oldAppRules() PickRulesBody {
	allowed := map[string]bool{"1080p_tv": true, "1080p_web": true, "1080p_bluray": true, "1080p_remux": true}
	steps := make([]Step, len(OldAppLadder))
	for i, q := range OldAppLadder {
		steps[i] = Step{Quality: q, Allowed: allowed[q]}
	}
	ge := 2600
	return PickRulesBody{Steps: steps, GoodEnoughAt: "1080p_bluray", GoodEnoughPoints: &ge, Traits: []TraitPoints{}, MustHave: []string{},
		Never: []string{"sample"}, SizeLimits: map[string]SizeLimit{}, Protocol: ProtocolRule{Prefer: "either"},
		Language: LanguageRule{Wanted: []string{}, Mode: "original"}, Order: "quality_then_points", UpgradeMinPoints: 100,
		ProperPolicy: ProperPolicy{Mode: "take_any"}, LikeOldApps: true}
}

func TestOldAppRulesCheckAndSteps(t *testing.T) {
	rules := oldAppRules()
	if err := CheckPickRules("HD-1080p", []string{"movie"}, rules); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"From S01E01 1080p HEVC x265-MeGusta":                    "1080p_tv",
		"Coco 3D 2017 MULTi COMPLETE BLURAY-SharpHD":             "br_disk",
		"Dune Part Two 2024 576p WEBRip x265 AAC-SSN":            "sd_web",
		"Coco 2017 2160p UHD Blu-ray HEVC TrueHD 7 1-WhiteRhino": "br_disk",
	} {
		if step, _ := StepOf(name, rules); step != want {
			t.Errorf("%s: %s, want %s", name, step, want)
		}
	}
	music := LosslessFirst()
	music.LikeOldApps = true
	if err := CheckPickRules("m", []string{"music"}, music); err == nil {
		t.Error("music Pick rules cannot judge like the old apps")
	}
	bad := rules
	bad.Language = LanguageRule{Mode: "wanted"}
	if err := CheckPickRules("x", []string{"movie"}, bad); err == nil {
		t.Error("a wanted language mode needs a language")
	}
}

func TestOldAppLanguageModes(t *testing.T) {
	rules := oldAppRules()
	title := &TitleRef{ID: 1, Name: "Oppenheimer", Year: 2023, OriginalLanguage: "en"}
	judge := func(name string, r PickRulesBody) Verdict {
		rel := ReleaseFacts{Name: name, Parse: releasename.Parse(name, releasename.Hints{}), Size: 8 << 30, Protocol: "torrent"}
		return Judge(rel, SlotSet{Match: Match{Title: title, Conf: releasename.Sure}, SlotIDs: []int64{1}, Out: true}, r)
	}
	if v := judge("Oppenheimer.2023.MULTi.FRENCH.1080p.WEB-DL.H264-NBDY", rules); !slices.Contains(codes(v, "fail"), "hard.language") {
		t.Errorf("original wanted, French found: %s", dump(v))
	}
	if v := judge("Oppenheimer.2023.1080p.WEB-DL.H264-NBDY", rules); slices.Contains(codes(v, "fail"), "hard.language") {
		t.Errorf("no language means the original: %s", dump(v))
	}
	rules.Language.Mode = "any"
	if v := judge("Oppenheimer.2023.MULTi.FRENCH.1080p.WEB-DL.H264-NBDY", rules); slices.Contains(codes(v, "fail"), "hard.language") {
		t.Errorf("any language: %s", dump(v))
	}
	rules.Language = LanguageRule{Wanted: []string{"no"}, Mode: "wanted"}
	if v := judge("Oppenheimer.2023.NORDiC.1080p.WEB-DL.H264-NoTrace", rules); slices.Contains(codes(v, "fail"), "hard.language") {
		t.Errorf("Norwegian wanted, NORDiC has it: %s", dump(v))
	}
}

// TestBetterLikeOldApps: Radarr's upgrade rules.
func TestBetterLikeOldApps(t *testing.T) {
	rules := oldAppRules()
	idx := func(s string) int { return stepIndex(rules, s) }
	for _, c := range []struct {
		why        string
		cur        Current
		step       string
		points     int
		fix        bool
		noUpgrades bool
		want       string
	}{
		{"both cut-offs met", Current{Step: "1080p_bluray", Points: 3000}, "1080p_remux", 5000, false, false, "better.good_enough_already"},
		{"higher step below the cut-off", Current{Step: "1080p_web", Points: 3000}, "1080p_bluray", 0, false, false, "better.higher"},
		{"lower step", Current{Step: "1080p_web", Points: 0}, "1080p_tv", 5000, false, false, "better.lower_step"},
		{"higher step, step met, points not", Current{Step: "1080p_bluray", Points: 100}, "1080p_remux", 100, false, false, "better.good_enough_already"},
		{"same step, more points", Current{Step: "1080p_bluray", Points: 100}, "1080p_bluray", 300, false, false, "better.points"},
		{"same step, too few more", Current{Step: "1080p_bluray", Points: 100}, "1080p_bluray", 150, false, false, "better.not_better"},
		{"same step, fewer", Current{Step: "1080p_bluray", Points: 100}, "1080p_bluray", 50, false, false, "better.not_better"},
		{"a fix", Current{Step: "1080p_bluray", Points: 3000}, "1080p_bluray", 3000, true, false, "better.fix"},
		{"upgrades off", Current{Step: "1080p_tv", Points: 0}, "1080p_tv", 5000, false, true, "better.good_enough_already"},
	} {
		r := rules
		r.NoUpgrades = c.noUpgrades
		var v Verdict
		fix := 0
		if c.fix {
			fix = 1
		}
		v.betterLikeOldApps(r, &c.cur, idx(c.step), idx(c.cur.Step), c.points, c.fix && fix > 0)
		if got := v.Reasons[0].Code(); got != c.want {
			t.Errorf("%s: %s (%s), want %s", c.why, got, v.Reasons[0].Detail, c.want)
		}
	}
}
