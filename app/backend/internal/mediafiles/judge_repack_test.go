package mediafiles

import (
	"slices"
	"testing"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
)

// TestRepackFromAnotherGroupLikeOldApps follows Sonarr's and Radarr's
// RepackSpecification on a real Sonarr search: Fallout S01E01 held as a
// WEBDL-2160p file from LAZYCUNTS, where Sonarr refused the NTb REPACK as
// "a repack for a different release group".
func TestRepackFromAnotherGroupLikeOldApps(t *testing.T) {
	rules := oldAppRules()
	for i := range rules.Steps {
		rules.Steps[i].Allowed = rules.Steps[i].Quality == "2160p_web" || rules.Steps[i].Quality == "2160p_remux"
	}
	rules.GoodEnoughAt, rules.GoodEnoughPoints = "2160p_remux", nil
	title := &TitleRef{ID: 1, Name: "Fallout", Year: 2024, Kind: "show", OriginalLanguage: "en"}
	const file = "Fallout (2024) - S01E01 - The End [WEBDL-2160p][EAC3 Atmos 5.1][h265]-LAZYCUNTS"
	judge := func(name, fileName string, r PickRulesBody) Verdict {
		p := releasename.Parse(name, releasename.Hints{Show: true})
		cur := CurrentOf(fileName, time.Time{}, r)
		return Judge(ReleaseFacts{Name: name, Parse: p, Size: 4 << 30, Protocol: "torrent"},
			SlotSet{Match: Match{Title: title, Conf: releasename.Sure}, SlotIDs: []int64{1}, Out: true, Current: &cur}, r)
	}
	for _, c := range []struct {
		why, name, file string
		refused         bool
	}{
		{"a repack from another group", "Fallout S01E01 The End 2160p REPACK AMZN WEB-DL DDP5 1 HDR H 265-NTb", file, true},
		{"a repack from the file's group", "Fallout S01E01 The End 2160p REPACK AMZN WEB-DL DDP5 1 HDR H 265-LAZYCUNTS", file, false},
		{"group in another case", "Fallout S01E01 The End 2160p REPACK AMZN WEB-DL DDP5 1 HDR H 265-lazycunts", file, false},
		{"the file's group is not known", "Fallout S01E01 The End 2160p REPACK AMZN WEB-DL DDP5 1 HDR H 265-NTb", "Fallout (2024) - S01E01 - The End [WEBDL-2160p][EAC3 Atmos 5.1][h265]", true},
		{"the repack's group is not known", "Fallout S01E01 The End 2160p REPACK AMZN WEB-DL DDP5 1 HDR H 265", file, true},
		{"a rerip from another group", "Fallout S01E01 The End 2160p RERIP AMZN WEB-DL DDP5 1 HDR H 265-NTb", file, true},
		{"a proper from another group", "Fallout S01E01 The End 2160p PROPER AMZN WEB-DL DDP5 1 HDR H 265-NTb", file, false},
		{"a repack of another quality", "Fallout S01E01 The End 2160p REPACK AMZN WEBRip DDP5 1 HDR H 265-NTb", file, false},
		{"a repack of a higher step", "Fallout S01E01 The End 2160p REPACK UHD BluRay REMUX DV HDR HEVC TrueHD 7 1-NTb", file, false},
		{"no more fixed than the file", "Fallout S01E01 The End 2160p REPACK AMZN WEB-DL DDP5 1 HDR H 265-NTb", "Fallout (2024) - S01E01 - The End [WEBDL-2160p Proper][EAC3 Atmos 5.1][h265]-LAZYCUNTS", false},
	} {
		v := judge(c.name, c.file, rules)
		if got := slices.Contains(codes(v, "fail"), "better.repack_group"); got != c.refused {
			t.Errorf("%s: refused as a repack %v, want %v\n%s", c.why, got, c.refused, dump(v))
		}
	}

	if v := judge("Fallout S01E01 The End 2160p REPACK AMZN WEB-DL DDP5 1 HDR H 265-LAZYCUNTS", file, rules); !v.Passed || !slices.Contains(codes(v, "pass"), "better.fix") {
		t.Errorf("the file's own group's repack replaces it: %s", dump(v))
	}
	other := "Fallout S01E01 The End 2160p REPACK AMZN WEB-DL DDP5 1 HDR H 265-NTb"
	off := rules
	off.ProperPolicy.Mode = "prefer_only"
	if v := judge("Fallout S01E01 The End 2160p REPACK AMZN WEB-DL DDP5 1 HDR H 265-LAZYCUNTS", file, off); !slices.Contains(codes(v, "fail"), "better.repack_group") {
		t.Errorf("repacks that never replace the file, as Sonarr's do not upgrade: %s", dump(v))
	}
	off.ProperPolicy.Mode = "ignore"
	if v := judge(other, file, off); slices.Contains(codes(v, "fail"), "better.repack_group") {
		t.Errorf("repacks not preferred skip the check, as in Sonarr: %s", dump(v))
	}
	own := rules
	own.LikeOldApps = false
	if v := judge(other, file, own); !slices.Contains(codes(v, "pass"), "better.fix") {
		t.Errorf("HolyArrs's own Pick rules take a fix from any group under take_any: %s", dump(v))
	}
}
