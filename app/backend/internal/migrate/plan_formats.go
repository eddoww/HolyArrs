package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

// radarrLanguages and sonarrLanguages map each app's language ids to the
// codes HolyArrs uses; the two apps number them differently.
var (
	radarrLanguages = map[int64]string{1: "en", 2: "fr", 3: "es", 4: "de", 5: "it", 6: "da", 7: "nl", 8: "ja", 9: "is", 10: "zh",
		11: "ru", 12: "pl", 13: "vi", 14: "sv", 15: "no", 16: "fi", 17: "tr", 18: "pt", 19: "nl-be", 20: "el", 21: "ko", 22: "hu",
		23: "he", 24: "lt", 25: "cs", 26: "hi", 27: "ro", 28: "th", 29: "bg", 30: "pt-br", 31: "ar", 32: "uk", 33: "fa", 34: "bn",
		35: "sk", 36: "lv", 37: "es-419", 38: "ca", 39: "hr", 40: "sr", 41: "bs", 42: "et", 43: "ta", 44: "id", 45: "te", 46: "mk",
		47: "sl", 48: "ml", 49: "kn", 50: "sq", 51: "af", 52: "mr", 53: "tl", 54: "ur", 55: "rm", 56: "mn", 57: "ka", 58: "ga"}
	sonarrLanguages = map[int64]string{1: "en", 2: "fr", 3: "es", 4: "de", 5: "it", 6: "da", 7: "nl", 8: "ja", 9: "is", 10: "zh",
		11: "ru", 12: "pl", 13: "vi", 14: "sv", 15: "no", 16: "fi", 17: "tr", 18: "pt", 19: "nl-be", 20: "el", 21: "ko", 22: "hu",
		23: "he", 24: "lt", 25: "cs", 26: "ar", 27: "hi", 28: "bg", 29: "ml", 30: "uk", 31: "sk", 32: "th", 33: "pt-br", 34: "es-419",
		35: "ro", 36: "lv", 37: "fa", 38: "ca", 39: "hr", 40: "sr", 41: "bs", 42: "et", 43: "ta", 44: "id", 45: "mk", 46: "sl"}
)

// languageCode is an app's language id as a code: "original" for the
// title's own (-2), "" for any (-1) or an id it does not know.
func languageCode(k Kind, id int64) string {
	if id == -2 {
		return "original"
	}
	if k == Sonarr {
		return sonarrLanguages[id]
	}
	return radarrLanguages[id]
}

// Source and modifier condition values by app; Sonarr counts remuxes and
// raw HD as sources of their own.
var (
	radarrSources   = map[int64]string{0: "unknown", 1: "cam", 2: "telesync", 3: "telecine", 4: "workprint", 5: "dvd", 6: "tv", 7: "web_dl", 8: "web_rip", 9: "bluray"}
	sonarrSources   = map[int64]string{0: "unknown", 1: "tv", 2: "tv_raw", 3: "web_dl", 4: "web_rip", 5: "dvd", 6: "bluray", 7: "bluray_raw"}
	radarrModifiers = map[int64]string{0: "none", 1: "regional", 2: "screener", 3: "raw_hd", 4: "br_disk", 5: "remux"}
)

// specKinds are the condition kinds HolyArrs can test, by the apps' names.
var specKinds = map[string]string{
	"releasetitlespecification": "release_title", "releasegroupspecification": "release_group",
	"editionspecification": "edition", "resolutionspecification": "resolution", "sourcespecification": "source",
	"qualitymodifierspecification": "modifier", "languagespecification": "language", "sizespecification": "size",
}

// formatConditions reads a custom format's conditions; why says what
// HolyArrs cannot test when it cannot take the format.
func formatConditions(k Kind, f Named) ([]mediafiles.TraitCondition, string) {
	if len(f.Items) == 0 {
		return nil, "it has no conditions"
	}
	var out []mediafiles.TraitCondition
	for _, it := range f.Items {
		impl := str(it["kind"])
		kind := specKinds[strings.ToLower(impl)]
		if kind == "" {
			return nil, "a condition on " + specWords(impl) + " is not something HolyArrs can test"
		}
		c := mediafiles.TraitCondition{Name: clip(str(it["name"]), 100), Kind: kind, Negate: boolean(it["negate"]), Required: boolean(it["required"])}
		v := it["value.value"]
		switch kind {
		case "release_title", "release_group", "edition":
			c.Value = str(v)
		case "resolution":
			c.Value = strconv.FormatInt(num(v), 10)
		case "source":
			src := radarrSources[num(v)]
			if k == Sonarr {
				src = sonarrSources[num(v)]
			}
			if src == "" {
				return nil, fmt.Sprintf("its source condition has a value (%d) HolyArrs does not know", num(v))
			}
			c.Value = src
		case "modifier":
			if c.Value = radarrModifiers[num(v)]; c.Value == "" {
				return nil, fmt.Sprintf("its quality modifier condition has a value (%d) HolyArrs does not know", num(v))
			}
		case "language":
			if c.Value = languageCode(k, num(v)); c.Value == "" {
				return nil, fmt.Sprintf("its language condition names a language (%d) HolyArrs does not know", num(v))
			}
			c.Except = boolean(it["value.exceptLanguage"])
		case "size":
			c.MinGB, c.MaxGB = fnum(it["value.min"]), fnum(it["value.max"])
		}
		out = append(out, c)
	}
	t := mediafiles.OwnTrait{Name: "x", Match: "conditions", Conditions: out}
	if err := mediafiles.CheckOwnTrait(&t); err != nil {
		return nil, err.Error()
	}
	return t.Conditions, ""
}

// specWords names an app condition kind in words ("IndexerFlagSpecification"
// is "indexer flag").
func specWords(impl string) string {
	impl = strings.TrimSuffix(impl, "Specification")
	var b strings.Builder
	for i, r := range impl {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "an unknown kind"
	}
	return strings.ToLower(b.String())
}

func fnum(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f
	}
	return float64(num(v))
}

// contentKey names a value by its content, so the same custom format or
// profile from two apps becomes one thing here, and a second move adds
// nothing twice.
func contentKey(prefix string, v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return prefix + hex.EncodeToString(sum[:8])
}

// conditionContent is what makes two formats the same: their conditions
// without the names they gave them.
func conditionContent(cs []mediafiles.TraitCondition) []mediafiles.TraitCondition {
	out := slices.Clone(cs)
	for i := range out {
		out[i].Name = ""
	}
	return out
}

// formats plans an app's custom formats as traits. Formats with the same
// conditions in two apps become one trait; a name already taken by another
// format gets the app's name after it.
func (b *builder) formats(s *Snapshot) {
	for i, f := range s.CustomFormats {
		from := b.from("custom_formats", i)
		name := clip(f.Name, 60)
		if name == "" {
			name = "Format " + strconv.FormatInt(f.ID, 10)
		}
		cs, why := formatConditions(s.Kind, f)
		if why != "" {
			b.not(GroupRules, from, name, "not moved: "+why+"; profiles that score it lose those points")
			continue
		}
		key := contentKey("trait:", conditionContent(cs))
		b.formatKeys[b.formatKey(s.Kind, f.Name)] = key
		if b.keys[key] {
			b.p.Mapped = append(b.p.Mapped, from)
			continue
		}
		if b.traitNames[strings.ToLower(name)] {
			name = clip(name, 50) + " (" + appName(s.Kind) + ")"
		}
		b.traitNames[strings.ToLower(name)] = true
		b.add(GroupRules, Item{Key: key, Name: name, From: from,
			Detail: fmt.Sprintf("one of your own traits, matching by %d condition%s", len(cs), plural(len(cs))),
			Trait:  &TraitPlan{Name: name, Conditions: cs}})
	}
}

// formatKey keys an app's custom format to the trait it became; format
// names repeat across two copies of one app, so the read is part of the key.
func (b *builder) formatKey(k Kind, name string) string {
	return fmt.Sprintf("%d:%s:%s", b.snapID, k, name)
}

func appName(k Kind) string {
	switch k {
	case Radarr:
		return "Radarr"
	case Sonarr:
		return "Sonarr"
	}
	return string(k)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// pickPointsLimit is the most points Pick rules give or take.
const pickPointsLimit = 10000

func clampPoints(n int) (int, bool) {
	switch {
	case n > pickPointsLimit:
		return pickPointsLimit, true
	case n < -pickPointsLimit:
		return -pickPointsLimit, true
	}
	return n, false
}

// rulesFromProfile turns a Sonarr or Radarr quality profile into the Pick
// rules it becomes, judged like that app; notes say what changed on the way.
func (b *builder) rulesFromProfile(k Kind, p Profile) (*MakeRulesPlan, []string, string) {
	var notes []string
	body := mediafiles.PickRulesBody{
		LikeOldApps: true, NoUpgrades: !p.UpgradeAllowed,
		Traits: []mediafiles.TraitPoints{}, MustHave: []string{},
		Never:      []string{"sample", "executable", "password_protected", "hardcoded_subs"},
		SizeLimits: map[string]mediafiles.SizeLimit{},
		Protocol:   mediafiles.ProtocolRule{Prefer: "either"},
		Language:   mediafiles.LanguageRule{Wanted: []string{}, OriginalOK: true, Mode: "any"},
		Order:      "quality_then_points", UpgradeMinPoints: max(p.MinUpgradeScore, 1),
		ProperPolicy: mediafiles.ProperPolicy{Mode: "take_any"},
	}
	at := map[string]int{}
	for _, st := range p.Steps {
		step := mediafiles.OldAppStep(st.Quality)
		if step == "" {
			if !strings.EqualFold(st.Quality, "Unknown") {
				notes = append(notes, "quality "+clip(st.Quality, 40)+" is not known here")
			}
			continue
		}
		if i, ok := at[step]; ok {
			body.Steps[i].Allowed = body.Steps[i].Allowed || st.Allowed
			continue
		}
		at[step] = len(body.Steps)
		body.Steps = append(body.Steps, mediafiles.Step{Quality: step, Allowed: st.Allowed})
	}
	if len(body.Steps) == 0 {
		return nil, nil, "it lists no quality HolyArrs knows"
	}
	// Qualities the app does not list (Sonarr has no CAM or BR-DISK) go
	// first, off, so they never count as better than a listed one.
	var missing []mediafiles.Step
	for _, q := range mediafiles.OldAppLadder {
		if _, ok := at[q]; !ok {
			missing = append(missing, mediafiles.Step{Quality: q})
		}
	}
	body.Steps = append(missing, body.Steps...)
	allowed := slices.IndexFunc(body.Steps, func(s mediafiles.Step) bool { return s.Allowed })
	if allowed < 0 {
		return nil, nil, "it allows no quality"
	}
	cut := ""
	for _, st := range p.Steps {
		if strings.EqualFold(st.Quality, p.Cutoff) || strings.EqualFold(st.Group, p.Cutoff) {
			cut = mediafiles.OldAppStep(st.Quality)
			break
		}
	}
	if i := slices.IndexFunc(body.Steps, func(s mediafiles.Step) bool { return s.Quality == cut }); i < 0 || !body.Steps[i].Allowed {
		for i := len(body.Steps) - 1; i >= 0; i-- {
			if body.Steps[i].Allowed {
				cut = body.Steps[i].Quality
				break
			}
		}
		notes = append(notes, "its upgrade-until quality is not allowed, so Good enough is the top allowed step")
	}
	body.GoodEnoughAt = cut
	var cl bool
	if body.MinPoints, cl = clampPoints(p.MinScore); cl {
		notes = append(notes, fmt.Sprintf("its least format score %d is cut to %d", p.MinScore, body.MinPoints))
	}
	ge, cl := clampPoints(p.CutoffScore)
	if cl {
		notes = append(notes, fmt.Sprintf("its upgrade-until score %d is cut to %d", p.CutoffScore, ge))
	}
	body.GoodEnoughPoints = &ge
	if body.UpgradeMinPoints, cl = clampPoints(body.UpgradeMinPoints); cl {
		notes = append(notes, "its upgrade step is cut to 10000")
	}
	if p.Language != nil {
		switch code := languageCode(k, *p.Language); {
		case *p.Language == -1:
		case code == "original":
			body.Language.Mode = "original"
		case code != "":
			body.Language = mediafiles.LanguageRule{Wanted: []string{code}, OriginalOK: false, Mode: "wanted"}
		default:
			notes = append(notes, fmt.Sprintf("its language (%d) is not known here, so any language passes", *p.Language))
		}
	}
	plan := &MakeRulesPlan{Kinds: []string{libraryKind(k)}, Body: body, Scores: []TraitScore{}}
	names := make([]string, 0, len(p.Scores))
	for n := range p.Scores {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		key := b.formatKeys[b.formatKey(k, n)]
		if key == "" {
			notes = append(notes, "the score for "+clip(n, 60)+" is left out, as that format did not move")
			continue
		}
		pts, cl := clampPoints(p.Scores[n])
		if cl {
			notes = append(notes, fmt.Sprintf("the score for %s is cut to %d", clip(n, 60), pts))
		}
		if i := slices.IndexFunc(plan.Scores, func(s TraitScore) bool { return s.Trait == key }); i >= 0 {
			// Two formats there are one trait here: their scores add up.
			plan.Scores[i].Points, _ = clampPoints(plan.Scores[i].Points + pts)
			continue
		}
		plan.Scores = append(plan.Scores, TraitScore{Trait: key, Name: clip(n, 60), Points: pts})
	}
	if len(plan.Scores) > 64 {
		notes = append(notes, fmt.Sprintf("only 64 of its %d format scores are kept", len(plan.Scores)))
		plan.Scores = plan.Scores[:64]
	}
	return plan, notes, ""
}

// makeRules plans the Pick rules one Sonarr or Radarr profile becomes. The
// same rules from two apps become one set for both kinds of title; the
// same name with other rules gets the app's name after it.
func (b *builder) makeRules(s *Snapshot, i int, p Profile) {
	from := b.from("quality_profiles", i)
	name := clip(b.named(p.Name), 150)
	if name == "" {
		name = "Profile " + strconv.FormatInt(p.ID, 10)
	}
	plan, notes, why := b.rulesFromProfile(s.Kind, p)
	if why != "" {
		b.not(GroupRules, from, name, "not moved: "+why+"; its titles use Standard")
		b.presets[b.presetKey(s.Kind, p.ID)] = mediafiles.GoodEverydayName
		return
	}
	content := struct {
		Body   mediafiles.PickRulesBody
		Scores []TraitScore
		Name   string
	}{plan.Body, slices.Clone(plan.Scores), strings.ToLower(name)}
	for j := range content.Scores {
		content.Scores[j].Name = ""
	}
	key := contentKey("pick_rules_made:", content)
	if it := b.itemByKey(GroupRules, key); it != nil {
		// The other app has the same profile: one set serves both kinds.
		if kind := libraryKind(s.Kind); !slices.Contains(it.Rules.Make.Kinds, kind) {
			it.Rules.Make.Kinds = append(it.Rules.Make.Kinds, kind)
			it.Detail += "; also for " + kind + "s"
		}
		b.presets[b.presetKey(s.Kind, p.ID)] = it.Rules.Preset
		b.nameMadeRules(s.Kind, p.Name, it.Rules.Preset)
		b.p.Mapped = append(b.p.Mapped, from)
		return
	}
	if b.rulesNames[strings.ToLower(name)] {
		name = clip(name, 140) + " (" + appName(s.Kind) + ")"
	}
	b.rulesNames[strings.ToLower(name)] = true
	b.presets[b.presetKey(s.Kind, p.ID)] = name
	b.nameMadeRules(s.Kind, p.Name, name)
	detail := fmt.Sprintf("new Pick rules judged like %s: %d allowed steps, good enough at %s and %d points, %d trait scores",
		appName(s.Kind), countAllowed(plan.Body.Steps), plan.Body.GoodEnoughAt, *plan.Body.GoodEnoughPoints, len(plan.Scores))
	why = "the same quality steps, upgrade rules, language and format scores as the profile"
	if len(notes) > 0 {
		why += "; " + strings.Join(notes, "; ")
	}
	b.add(GroupRules, Item{Key: key, Name: name, From: from, Detail: detail, Why: why,
		Rules: &RulesPlan{App: string(s.Kind), Profile: clip(p.Name, maxName), Preset: name, Make: plan}})
}

// nameMadeRules lets a request app that names this profile use the Pick
// rules made from it: a Seerr Radarr server's "HD-1080p" is Radarr's
// "HD-1080p (Radarr)" here, never Sonarr's "HD-1080p". With two apps of
// one kind, the first read wins.
func (b *builder) nameMadeRules(app Kind, profile, made string) {
	k := libraryKind(app) + ":" + strings.ToLower(profile)
	if _, ok := b.namedPresets[k]; !ok {
		b.namedPresets[k] = made
	}
}

func countAllowed(steps []mediafiles.Step) int {
	n := 0
	for _, s := range steps {
		if s.Allowed {
			n++
		}
	}
	return n
}

// itemByKey finds a planned item by its key.
func (b *builder) itemByKey(group, key string) *Item {
	g := b.items[group]
	for i := range g.Items {
		if g.Items[i].Key == key {
			return &g.Items[i]
		}
	}
	return nil
}
