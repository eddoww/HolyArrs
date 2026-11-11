package mediafiles

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
)

// Rank orders the passed Verdicts (section 3.2). The first becomes chosen
// (unless it is held) and each loser gets a reason naming the key it lost on.
// rels and vs are parallel.
func Rank(rels []ReleaseFacts, vs []Verdict, rules PickRulesBody) { rank(rels, vs, rules, false) }

// RankPacksFirst ranks like Rank, but a passing season pack comes before
// any Release that is not one (setting season_pack_prefer = always).
func RankPacksFirst(rels []ReleaseFacts, vs []Verdict, rules PickRulesBody) {
	rank(rels, vs, rules, true)
}

func rank(rels []ReleaseFacts, vs []Verdict, rules PickRulesBody, packsFirst bool) {
	var idx []int
	for i := range vs {
		if vs[i].Passed {
			idx = append(idx, i)
		}
	}
	type key struct {
		name   string
		a, b   func(i int) int
		higher bool
	}
	step := func(i int) int { return vs[i].Step.Index }
	pts := func(i int) int { return vs[i].Points }
	keys := []key{{"quality", step, step, true}, {"points", pts, pts, true}}
	if rules.Order == "points_then_quality" {
		keys[0], keys[1] = keys[1], keys[0]
	}
	if packsFirst {
		pack := func(i int) int {
			if isPack(rels[i].Parse) {
				return 1
			}
			return 0
		}
		keys = append([]key{{"season pack", pack, pack, true}}, keys...)
	}
	middle := func(i int) int { return -int(vs[i].sizeOff >> 20) }
	cmp := func(a, b int) (int, string) {
		for _, k := range keys {
			if x, y := k.a(a), k.a(b); x != y {
				if x > y {
					return -1, fmt.Sprintf("on %s (%d vs %d)", k.name, y, x)
				}
				return 1, fmt.Sprintf("on %s (%d vs %d)", k.name, x, y)
			}
		}
		pa, pb := rels[a].Parse, rels[b].Parse
		// A fix (PROPER, REPACK) ranks above the same step and points: from any
		// group under take_any and prefer_only, as the older apps' "prefer"
		// does, and only from the same group otherwise.
		anyGroup := rules.ProperPolicy.Mode == "take_any" || rules.ProperPolicy.Mode == "prefer_only"
		if (anyGroup || pa.Group.Value != "" && strings.EqualFold(pa.Group.Value, pb.Group.Value)) && pa.Fix != pb.Fix {
			if pa.Fix > pb.Fix {
				return -1, "on fix number"
			}
			return 1, "on fix number"
		}
		if pref := rules.Protocol.Prefer; pref != "either" && rels[a].Protocol != rels[b].Protocol {
			if rels[a].Protocol == pref {
				return -1, "on preferred protocol"
			}
			return 1, "on preferred protocol"
		}
		if c, why := episodeOrder(pa, pb); c != 0 {
			return c, why
		}
		if x, y := len(vs[a].SlotIDs), len(vs[b].SlotIDs); x != y && !pa.IsShow() {
			if x > y {
				return -1, "on Slots covered"
			}
			return 1, "on Slots covered"
		}
		if x, y := rels[a].SourcePriority, rels[b].SourcePriority; x != y {
			if x < y {
				return -1, "on Source priority"
			}
			return 1, "on Source priority"
		}
		if x, y := seederBand(rels[a].Seeders), seederBand(rels[b].Seeders); x != y {
			if x > y {
				return -1, "on seeders"
			}
			return 1, "on seeders"
		}
		if x, y := middle(a), middle(b); x != y {
			if x > y {
				return -1, "on size"
			}
			return 1, "on size"
		}
		if ta, tb := rels[a].PublishedAt, rels[b].PublishedAt; !ta.Equal(tb) {
			if ta.Before(tb) {
				return -1, "on published time"
			}
			return 1, "on published time"
		}
		return 0, ""
	}
	sort.SliceStable(idx, func(x, y int) bool { c, _ := cmp(idx[x], idx[y]); return c < 0 })
	for pos, i := range idx {
		vs[i].Rank = pos + 1
		if pos == 0 {
			vs[i].add("rank", "first", "rank", fmt.Sprintf("ranked 1 of %d", len(idx)), 0)
			continue
		}
		w := idx[0]
		_, why := cmp(w, i)
		if why == "" {
			why = "on order found"
		}
		vs[i].Outcome = "passed_over"
		vs[i].add("rank", "lost", "rank", fmt.Sprintf("lost to %s %s", rels[w].Name, why), 0)
	}
}

// ReachesGoodEnough reports whether a file with this release name meets the
// rules' Good enough step (and points, when set). It is used when the Pick
// rules of a Version change.
func ReachesGoodEnough(releaseName string, rules PickRulesBody) bool {
	return reachesGoodEnough(releasename.Parse(releaseName, hintsFor(rules)), FileFacts{}, rules)
}

func reachesGoodEnough(p releasename.Result, f FileFacts, rules PickRulesBody) bool {
	_, idx := ruleStep(rules, p)
	if idx < 0 || idx < stepIndex(rules, rules.GoodEnoughAt) {
		return false
	}
	return rules.GoodEnoughPoints == nil || sumPoints(f.input(p), rules) >= *rules.GoodEnoughPoints
}

// FileFacts are what is known about a File besides its name, for traits
// that test languages and size. The zero value knows nothing.
type FileFacts struct {
	Size int64
	// Languages are the file's audio languages as inspected (codes); they
	// win over the languages its name gives.
	Languages []string
	// OriginalLanguage is the title's original language code.
	OriginalLanguage string
}

func (f FileFacts) input(p releasename.Result) *traitInput {
	in := newTraitInput(p, f.Size, f.OriginalLanguage)
	for _, l := range f.Languages {
		if l = sameLanguage(l); !slices.Contains(in.fileLang, l) {
			in.fileLang = append(in.fileLang, l)
		}
	}
	return in
}

// TraitMatches says whether a trait matches a parsed name, with what else
// is known about the Release or file, and what it saw.
func TraitMatches(trait string, p releasename.Result, f FileFacts) (bool, string) {
	return traitMatches(trait, f.input(p))
}

// CurrentOf describes a File already in the Library from its release name,
// for the "better than what we have" step (pick-rules.md section 4). size
// is the File's own size in bytes (0: not known), so size traits score it
// the way they score a new Release.
func CurrentOf(releaseName string, size int64, importedAt time.Time, rules PickRulesBody) Current {
	return CurrentOfFile(releaseName, FileFacts{Size: size}, importedAt, rules)
}

// CurrentOfFile is CurrentOf with what else is known about the file, so
// traits on languages and size count for it as they did for the Release.
func CurrentOfFile(releaseName string, f FileFacts, importedAt time.Time, rules PickRulesBody) Current {
	p := releasename.Parse(releaseName, hintsFor(rules))
	step, _ := ruleStep(rules, p)
	return Current{Step: step, Points: sumPoints(f.input(p), rules), Group: p.Group.Value, Fix: p.Fix, Source: p.Source.Value, ImportedAt: importedAt}
}

// StepOf is the ladder step a name parses to and its place in the rules'
// ladder (-1 when the step is unknown or not on the ladder).
func StepOf(name string, rules PickRulesBody) (string, int) {
	return ruleStep(rules, releasename.Parse(name, hintsFor(rules)))
}
