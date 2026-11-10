package mediafiles

import "testing"

// Propers and repacks, "prefer and upgrade" as Sonarr and Radarr run it:
// with the default take_any policy a fix ranks above the same step and
// points from any group, and (judge_test T23, T24) replaces a copy already
// in the Library, however old.
func TestRankPrefersAFixFromAnyGroup(t *testing.T) {
	rules := GoodEveryday()
	if rules.ProperPolicy.Mode != "take_any" {
		t.Fatalf("default proper policy %q", rules.ProperPolicy.Mode)
	}
	plain := release(row{Name: "Saltwater.Choir.2022.1080p.WEB-DL.H.265-QUILL"})
	fix := release(row{Name: "Saltwater.Choir.2022.REPACK.1080p.WEB-DL.H.265-TINCAN"})
	rank := func(rules PickRulesBody) []Verdict {
		vs := []Verdict{Judge(plain, setFor(t, plain, nil), rules), Judge(fix, setFor(t, fix, nil), rules)}
		if vs[0].Points != vs[1].Points || vs[0].Step.Index != vs[1].Step.Index {
			t.Fatalf("not the same step and points:\n%s\n%s", dump(vs[0]), dump(vs[1]))
		}
		Rank([]ReleaseFacts{plain, fix}, vs, rules)
		return vs
	}
	if vs := rank(rules); vs[1].Outcome != "chosen" || vs[0].Reasons[len(vs[0].Reasons)-1].Detail != "lost to "+fix.Name+" on fix number" {
		t.Fatalf("take_any: the repack should win on its fix number\n%s", dump(vs[0]))
	}
	rules.ProperPolicy.Mode = "same_group_only"
	if vs := rank(rules); vs[0].Reasons[len(vs[0].Reasons)-1].Detail == "lost to "+fix.Name+" on fix number" {
		t.Fatal("same_group_only: another group's fix must not decide")
	}
}
