package mediafiles

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dlclark/regexp2"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
)

// TraitCondition is one test of a trait that matches by conditions, the
// way a Sonarr or Radarr custom format does. Conditions of one kind pass
// when at least one of them passes and every required one passes; the
// trait matches when each kind passes. Negate turns one condition's answer
// around before that.
type TraitCondition struct {
	Name string `json:"name"`
	// Kind is release_title, release_group, edition (a pattern in Value),
	// resolution (480, 576, 720, 1080 or 2160 in Value), source, modifier
	// (OldAppQuality words in Value), language (a code, or "original" for
	// the title's own language) or size (MinGB to MaxGB).
	Kind     string  `json:"kind"`
	Value    string  `json:"value,omitempty"`
	MinGB    float64 `json:"min_gb,omitempty"`
	MaxGB    float64 `json:"max_gb,omitempty"`
	Except   bool    `json:"except,omitempty"` // language: a language other than Value is in the Release
	Negate   bool    `json:"negate,omitempty"`
	Required bool    `json:"required,omitempty"`
}

// TraitConditionKinds are the kinds a condition can have.
var TraitConditionKinds = []string{"release_title", "release_group", "edition", "resolution", "source", "modifier", "language", "size"}

var (
	conditionSources     = []string{"unknown", "cam", "telesync", "telecine", "workprint", "dvd", "tv", "web_dl", "web_rip", "bluray", "tv_raw", "bluray_raw"}
	conditionModifiers   = []string{"none", "regional", "screener", "raw_hd", "br_disk", "remux"}
	conditionResolutions = []string{"0", "360", "480", "540", "576", "720", "1080", "2160"}
)

// reVideoExt is a video file's extension at the end of a name: Radarr
// reads release titles without it.
var reVideoExt = regexp.MustCompile(`(?i)\.(mkv|mp4|avi|m4v|wmv|mov|ts|m2ts)$`)

// patternTimeout bounds one pattern match: a pattern written to backtrack
// without end must not stall a search.
const patternTimeout = 100 * time.Millisecond

// compilePattern reads a pattern the way Sonarr and Radarr do (.NET
// syntax, case ignored), so their custom formats keep their meaning:
// look-arounds such as \bHDR\b(?!ip) and Unicode word edges work here,
// where Go's own patterns have neither.
func compilePattern(p string) (*regexp2.Regexp, error) {
	re, err := regexp2.Compile(p, regexp2.IgnoreCase)
	if err != nil {
		return nil, err
	}
	re.MatchTimeout = patternTimeout
	return re, nil
}

// patternProblem says in plain words why a pattern cannot be used.
func patternProblem(p string) string {
	_, err := compilePattern(p)
	if err == nil {
		return ""
	}
	msg := err.Error()
	if i := strings.LastIndex(msg, " - "); i >= 0 {
		msg = msg[i+3:]
	}
	return msg
}

// checkConditions checks and cleans the conditions of a trait that matches
// by conditions.
func checkConditions(cs []TraitCondition) ([]TraitCondition, error) {
	if len(cs) == 0 || len(cs) > 50 {
		return nil, fieldErr("conditions", "List 1 to 50 conditions.")
	}
	out := make([]TraitCondition, 0, len(cs))
	for i, c := range cs {
		c.Name = strings.TrimSpace(c.Name)
		c.Value = strings.TrimSpace(c.Value)
		if c.Name == "" {
			c.Name = fmt.Sprintf("Condition %d", i+1)
		}
		if !utf8.ValidString(c.Name) || utf8.RuneCountInString(c.Name) > 100 {
			return nil, fieldErr("conditions", "Condition names hold at most 100 characters.")
		}
		field := fmt.Sprintf("conditions.%d", i)
		switch c.Kind {
		case "release_title", "release_group", "edition":
			if c.Value == "" || utf8.RuneCountInString(c.Value) > 500 {
				return nil, fieldErr(field, "%s: write a pattern of 1 to 500 characters.", c.Name)
			}
			if why := patternProblem(c.Value); why != "" {
				return nil, fieldErr(field, "%s: the pattern cannot be read (%s).", c.Name, why)
			}
		case "resolution":
			if !slices.Contains(conditionResolutions, c.Value) {
				return nil, fieldErr(field, "%s: the resolution must be one of %s.", c.Name, strings.Join(conditionResolutions, ", "))
			}
		case "source":
			if !slices.Contains(conditionSources, c.Value) {
				return nil, fieldErr(field, "%s: unknown source %q.", c.Name, c.Value)
			}
		case "modifier":
			if !slices.Contains(conditionModifiers, c.Value) {
				return nil, fieldErr(field, "%s: unknown kind of copy %q.", c.Name, c.Value)
			}
		case "language":
			if _, ok := releasename.LanguageNames[c.Value]; !ok && c.Value != "original" {
				return nil, fieldErr(field, "%s: unknown language %q.", c.Name, c.Value)
			}
		case "size":
			if c.MinGB < 0 || c.MaxGB <= c.MinGB || c.MaxGB > 1<<20 || math.IsNaN(c.MinGB) || math.IsNaN(c.MaxGB) {
				return nil, fieldErr(field, "%s: the size needs 0 <= smallest < largest (in GB).", c.Name)
			}
		default:
			return nil, fieldErr(field, "Unknown condition kind %q.", c.Kind)
		}
		if c.Kind != "size" {
			c.MinGB, c.MaxGB = 0, 0
		}
		if c.Kind != "language" {
			c.Except = false
		}
		out = append(out, c)
	}
	return out, nil
}

// compiledCondition is a condition ready to test, its pattern read once.
type compiledCondition struct {
	TraitCondition
	re *regexp2.Regexp
}

func compileConditions(cs []TraitCondition) []compiledCondition {
	out := make([]compiledCondition, 0, len(cs))
	for _, c := range cs {
		cc := compiledCondition{TraitCondition: c}
		if c.Kind == "release_title" || c.Kind == "release_group" || c.Kind == "edition" {
			// A stored pattern always compiled when it was saved; one that
			// no longer does simply never matches.
			cc.re, _ = compilePattern(c.Value)
		}
		out = append(out, cc)
	}
	return out
}

// traitInput is what trait tests read about one Release or one owned file.
type traitInput struct {
	p        releasename.Result
	size     int64
	original string   // the title's original language code, "" when not known
	fileLang []string // an owned file's inspected audio languages, if read
	quality  *releasename.OldAppQuality
	langs    []string
	titled   *string
}

func newTraitInput(p releasename.Result, size int64, original string) *traitInput {
	if size > 0 {
		p.Size = size // size traits read it from the parse
	}
	return &traitInput{p: p, size: size, original: sameLanguage(original)}
}

// sameLanguage is the code names and the old apps use for a language:
// files and TMDB tag Norwegian by its written forms (nb, nn), they have one.
func sameLanguage(code string) string {
	if code == "nb" || code == "nn" {
		return "no"
	}
	return code
}

// oldQuality is the quality Sonarr and Radarr would read from the name.
func (in *traitInput) oldQuality() releasename.OldAppQuality {
	if in.quality == nil {
		q := releasename.ReadOldAppQuality(in.p.Original)
		in.quality = &q
	}
	return *in.quality
}

// languages are the Release's audio languages as the old apps settle them:
// those the name gives, the title's own language when the name gives none,
// and the title's own added when the name says it keeps it (ORIGINAL,
// NORDiC). An owned file's inspected languages win over its name.
func (in *traitInput) languages() []string {
	if in.langs != nil {
		return in.langs
	}
	langs := slices.Clone(in.p.Languages)
	if len(in.fileLang) > 0 {
		langs = slices.Clone(in.fileLang)
	}
	if in.original != "" && (len(langs) == 0 || in.p.Has("original_audio")) && !slices.Contains(langs, in.original) {
		langs = append(langs, in.original)
	}
	if langs == nil {
		langs = []string{}
	}
	in.langs = langs
	return langs
}

// titleTested is the name release title patterns read. For a movie the
// title's own words are put out of reach ("A Movie"), as Radarr does, so a
// pattern for a tag never matches a word of the title.
func (in *traitInput) titleTested() string {
	if in.titled != nil {
		return *in.titled
	}
	s := strings.Map(func(r rune) rune {
		if strings.ContainsRune("<>?*|", r) {
			return -1
		}
		return r
	}, in.p.Original)
	if m := reVideoExt.FindStringIndex(s); m != nil {
		s = s[:m[0]]
	}
	if !in.p.IsShow() && in.p.Music == nil {
		words := strings.Fields(in.p.Title.Value)
		// Words in brackets before the title ("[Norwegian] Troll") are
		// tags, not the title.
		if strings.HasPrefix(s, "[") {
			if end := strings.IndexByte(s, ']'); end > 0 {
				for _, w := range strings.FieldsFunc(s[1:end], func(r rune) bool { return r == ' ' || r == '.' || r == '_' }) {
					if len(words) > 1 && strings.EqualFold(words[0], w) {
						words = words[1:]
					}
				}
			}
		}
		if len(words) > 0 {
			q := make([]string, len(words))
			for i, w := range words {
				q[i] = regexp.QuoteMeta(w)
			}
			if re, err := regexp.Compile(`(?i)` + strings.Join(q, `[\W_]+`)); err == nil {
				if loc := re.FindStringIndex(s); loc != nil {
					s = s[:loc[0]] + "A Movie" + s[loc[1]:]
				}
			}
		}
	}
	in.titled = &s
	return s
}

func patternMatch(re *regexp2.Regexp, s string) (bool, string) {
	if re == nil || s == "" {
		return false, ""
	}
	m, err := re.FindStringMatch(s)
	if err != nil {
		return false, "took too long"
	}
	if m == nil {
		return false, ""
	}
	return true, m.String()
}

// test answers one condition, before Negate, with what it saw.
func (c compiledCondition) test(in *traitInput) (bool, string) {
	switch c.Kind {
	case "release_title":
		ok, hit := patternMatch(c.re, in.titleTested())
		if ok {
			return true, "the name has " + hit
		}
		if hit != "" {
			return false, "the pattern " + hit
		}
		return false, "the name does not fit"
	case "release_group":
		g := in.p.Group.Value
		if g == "" {
			return false, "no release group"
		}
		ok, _ := patternMatch(c.re, g)
		return ok, "release group " + g
	case "edition":
		e := strings.Join(in.p.Edition, " ")
		ok, _ := patternMatch(c.re, e)
		return ok, "edition " + e
	case "resolution":
		q := in.oldQuality()
		return strconv.Itoa(q.Resolution) == c.Value, fmt.Sprintf("%dp", q.Resolution)
	case "source":
		q := in.oldQuality()
		switch c.Value {
		case "tv_raw":
			return q.Modifier == "raw_hd", q.Name
		case "bluray_raw":
			return q.Modifier == "remux" && q.Source == "bluray", q.Name
		}
		return q.Source == c.Value, q.Name
	case "modifier":
		q := in.oldQuality()
		return q.Modifier == c.Value, q.Name
	case "language":
		want := c.Value
		if want == "original" {
			if in.original == "" {
				return false, "the title's language is not known"
			}
			want = in.original
		}
		langs := in.languages()
		words := make([]string, len(langs))
		for i, l := range langs {
			words[i] = releasename.LanguageName(l)
		}
		said := "audio " + strings.Join(words, ", ")
		if len(words) == 0 {
			said = "no audio language known"
		}
		if c.Except {
			return slices.ContainsFunc(langs, func(l string) bool { return l != want }), said
		}
		return slices.Contains(langs, want), said
	case "size":
		gb := float64(in.size) / float64(1<<30)
		lo, hi := int64(c.MinGB*float64(1<<30)), int64(c.MaxGB*float64(1<<30))
		return in.size > lo && in.size <= hi, fmt.Sprintf("%.2f GB", gb)
	}
	return false, "unknown condition"
}

// matchConditions answers a trait that matches by conditions, with the
// names of the conditions that passed.
func matchConditions(name string, cs []compiledCondition, in *traitInput) (bool, string) {
	type group struct {
		any, requiredFailed bool
	}
	groups := map[string]*group{}
	var order []string
	var passed, failed []string
	for _, c := range cs {
		g := groups[c.Kind]
		if g == nil {
			g = &group{}
			groups[c.Kind] = g
			order = append(order, c.Kind)
		}
		ok, saw := c.test(in)
		if c.Negate {
			ok = !ok
		}
		if ok {
			g.any = true
			passed = append(passed, c.Name)
		} else {
			if c.Required {
				g.requiredFailed = true
			}
			failed = append(failed, c.Name+" ("+saw+")")
		}
	}
	for _, k := range order {
		if g := groups[k]; !g.any || g.requiredFailed {
			return false, name + ": not met: " + strings.Join(failed, "; ")
		}
	}
	// A format whose one condition carries its own name says just the name.
	passed = slices.DeleteFunc(passed, func(n string) bool { return n == name })
	if len(passed) == 0 {
		return true, name
	}
	return true, name + ": " + strings.Join(passed, "; ")
}
