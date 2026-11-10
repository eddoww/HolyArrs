package migrate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/library"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

// traitID is the id of the trait a planned format became in this run or
// an earlier move (0 when it did not).
func (r *run) traitID(key string) int64 {
	if id := r.traitIDs[key]; id != 0 {
		return id
	}
	if id, ok := r.made(key); ok {
		return id
	}
	return 0
}

func (r *run) setTraitID(key string, id int64) {
	if r.traitIDs == nil {
		r.traitIDs = map[string]int64{}
	}
	r.traitIDs[key] = id
}

// rulesName is the name the Pick rules planned as name got here (another
// set may have had it already); name itself when none were made.
func (r *run) rulesName(name string) string {
	if n := r.madeRules[name]; n != "" {
		return n
	}
	return name
}

func (r *run) setRulesName(planned, got string) {
	if r.madeRules == nil {
		r.madeRules = map[string]string{}
	}
	r.madeRules[planned] = got
}

// freeName is name, or name with " (moved)" and a number after it, whichever
// taken does not hold first.
func freeName(name string, limit int, taken func(string) bool) string {
	if !taken(name) {
		return name
	}
	for i := 1; i < 100; i++ {
		suffix := " (moved)"
		if i > 1 {
			suffix = fmt.Sprintf(" (moved %d)", i)
		}
		n := clip(name, limit-len(suffix)) + suffix
		if !taken(n) {
			return n
		}
	}
	return name
}

func sameConditions(a, b []mediafiles.TraitCondition) bool {
	x, _ := json.Marshal(conditionContent(a))
	y, _ := json.Marshal(conditionContent(b))
	return string(x) == string(y)
}

// trait adds a moved custom format as one of your own traits. A trait of
// yours with the same conditions is used instead of a second one.
func (r *run) trait(it Item) (string, string) {
	t := it.Trait
	if id, ok := r.made(it.Key); ok {
		r.setTraitID(it.Key, id)
		return ResAlready, "moved before"
	}
	have, err := library.OwnTraits(r.ctx, r.m.Store)
	if err != nil {
		return ResFailed, err.Error()
	}
	names := map[string]bool{}
	for _, x := range have {
		if x.Match == "conditions" && sameConditions(x.Conditions, t.Conditions) {
			r.setTraitID(it.Key, x.ID)
			return ResMatched, "your trait " + x.Name + " has the same conditions"
		}
		names[strings.ToLower(x.Name)] = true
	}
	name := freeName(t.Name, 60, func(n string) bool { return names[strings.ToLower(n)] })
	what := fmt.Sprintf("your own trait %s, matching by %d condition%s", name, len(t.Conditions), plural(len(t.Conditions)))
	if r.dry {
		return ResCreate, what
	}
	made, err := library.CreateOwnTrait(r.ctx, r.m.Store, library.OwnTraitInput{Name: name, Match: "conditions", Conditions: t.Conditions}, r.by, r.markWith(it.Key, "trait"))
	if err == nil {
		r.setTraitID(it.Key, made.ID)
	}
	return result(err, ResCreated, what)
}

// makeRules adds the Pick rules a Sonarr or Radarr profile became, with
// the points of the traits its formats became.
func (r *run) makeRules(it Item) (string, string) {
	mk := it.Rules.Make
	if id, ok := r.made(it.Key); ok {
		if name := r.rulesNameOf(id); name != "" {
			r.setRulesName(it.Rules.Preset, name)
		}
		return ResAlready, "moved before"
	}
	body := mk.Body
	body.Traits = []mediafiles.TraitPoints{}
	var missing []string
	for _, s := range mk.Scores {
		id := r.traitID(s.Trait)
		if id == 0 {
			if !r.dry {
				missing = append(missing, s.Name)
			}
			continue
		}
		body.Traits = append(body.Traits, mediafiles.TraitPoints{Trait: mediafiles.OwnTraitKey(id), Points: s.Points})
	}
	name := freeName(it.Rules.Preset, 200, func(n string) bool { return r.rulesNamed(n) != 0 })
	what := fmt.Sprintf("Pick rules %s for %s, judged like %s, with %d trait scores", name, strings.Join(mk.Kinds, " and "), appName(Kind(it.Rules.App)), len(mk.Scores))
	if len(missing) > 0 {
		what += "; without the scores of " + strings.Join(missing, ", ") + ", as those traits are not here"
	}
	r.setRulesName(it.Rules.Preset, name)
	if r.dry {
		return ResCreate, what
	}
	_, err := library.CreatePickRules(r.ctx, r.m.Store, library.PickRulesInput{Name: name, Kinds: mk.Kinds, Body: body}, r.by, r.markWith(it.Key, "pick_rules"))
	return result(err, ResCreated, what)
}
