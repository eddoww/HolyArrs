package mediafiles

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
)

// The fork's reading of a Release for traits and Pick rules brought from
// Sonarr and Radarr (an imported trait, OwnTrait.OldApp; imported Pick
// rules, Synced.OldApp), so they keep making those apps' choices:
//
//   - source, modifier and resolution tests read the quality as those apps
//     do (releasename.ReadOldAppQuality), with their telecine, workprint,
//     raw HD, regional and screener values;
//   - a name test reads the name as Radarr does: odd characters and the
//     video extension out, and a movie's own title put out of reach;
//   - a repack from another group than the current file's is refused, and
//     Pick rules with upgrades off keep the current file.
//
// Language tests of every trait count Bokmål and Nynorsk as Norwegian and
// add the title's own language when the name says it keeps it (ORIGINAL,
// NORDiC).

// sameLanguage is the code names and the old apps use for a language:
// files and TMDB tag Norwegian by its written forms (nb, nn), they have one.
func sameLanguage(code string) string {
	switch strings.ToLower(code) {
	case "nb", "nn", "nob", "nno":
		return "no"
	}
	return code
}

// oldAppSourceWords are the source test values a quality as the old apps
// read it fits: its source, and its modifier where that is a source here.
func oldAppSourceWords(q releasename.OldAppQuality) []string {
	out := []string{q.Source}
	switch q.Modifier {
	case "remux":
		out = append(out, "remux")
	case "br_disk":
		out = append(out, "disc_image")
	case "raw_hd":
		out = append(out, "tv_raw")
	}
	return out
}

// oldAppModifier is the modifier test value of a quality as the old apps
// read it ("" for none).
func oldAppModifier(q releasename.OldAppQuality) string {
	switch q.Modifier {
	case "br_disk":
		return "disc_image"
	case "none":
		return ""
	}
	return q.Modifier
}

// oldAppTest answers a source, modifier or resolution test of an imported
// trait; ok is false for any other kind.
func (s ownSpec) oldAppTest(p releasename.Result) (hit, ok bool) {
	switch s.Kind {
	case "source":
		q := releasename.ReadOldAppQuality(p.Original)
		for _, w := range oldAppSourceWords(q) {
			if slices.Contains(s.sources, w) {
				return true, true
			}
		}
		return false, true
	case "modifier":
		m := oldAppModifier(releasename.ReadOldAppQuality(p.Original))
		return m != "" && slices.Contains(s.sources, m), true
	case "resolution":
		q := releasename.ReadOldAppQuality(p.Original)
		return q.Resolution > 0 && q.Resolution == s.res, true
	}
	return false, false
}

var reVideoExt = regexp.MustCompile(`(?i)\.(mkv|mp4|avi|m4v|wmv|mov|ts|m2ts)$`)

// nameTested is the name an imported trait's name test reads, as Radarr
// reads it: odd characters and the video extension out and, for a movie,
// its own title's words put out of reach ("A Movie"), so a pattern for a
// tag never matches a word of the title.
func nameTested(p releasename.Result) string {
	s := strings.Map(func(r rune) rune {
		if strings.ContainsRune("<>?*|", r) {
			return -1
		}
		return r
	}, p.Original)
	if m := reVideoExt.FindStringIndex(s); m != nil {
		s = s[:m[0]]
	}
	if p.IsShow() || p.Music != nil {
		return s
	}
	words := strings.Fields(p.Title.Value)
	// Words in brackets before the title ("[Norwegian] Troll") are tags,
	// not the title.
	if strings.HasPrefix(s, "[") {
		if end := strings.IndexByte(s, ']'); end > 0 {
			for _, w := range strings.FieldsFunc(s[1:end], func(r rune) bool { return r == ' ' || r == '.' || r == '_' }) {
				if len(words) > 1 && strings.EqualFold(words[0], w) {
					words = words[1:]
				}
			}
		}
	}
	if len(words) == 0 {
		return s
	}
	q := make([]string, len(words))
	for i, w := range words {
		q[i] = regexp.QuoteMeta(w)
	}
	if re, err := regexp.Compile(`(?i)` + strings.Join(q, `[\W_]+`)); err == nil {
		if loc := re.FindStringIndex(s); loc != nil {
			s = s[:loc[0]] + "A Movie" + s[loc[1]:]
		}
	}
	return s
}

// repackRefused is why Sonarr and Radarr refuse a repack of the current
// file's quality ("" when they take it): only the group that made the file
// may fix it, so a repack from another group, or with either group
// unknown, is refused. A repack of another quality, or one that does not
// fix more than the file, is judged as any copy. A web copy counts as
// another quality than a web rip.
func repackRefused(p releasename.Result, cur *Current, pol ProperPolicy, sameStep bool) string {
	web := func(s string) bool { return s == "web_dl" || s == "web_rip" }
	curSource := ""
	if cur.parse != nil {
		curSource = cur.parse.Source.Value
	}
	otherWeb := web(p.Source.Value) && web(curSource) && p.Source.Value != curSource
	if !p.Repack || pol.Mode == "ignore" || !sameStep || otherWeb || p.Fix <= cur.Fix {
		return ""
	}
	switch {
	case pol.Mode == "prefer_only":
		return "a repack never replaces the current file under these Pick rules"
	case cur.Group == "":
		return "a repack, but the current file's group is not known"
	case p.Group.Value == "":
		return "a repack, but its group is not known"
	case !strings.EqualFold(p.Group.Value, cur.Group):
		return fmt.Sprintf("a repack from %s, but the current file is from %s", p.Group.Value, cur.Group)
	}
	return ""
}

// upgradesOff says imported Pick rules came from a profile with upgrades
// off: Good enough at the first allowed step, with the least points.
func upgradesOff(rules PickRulesBody) bool {
	if rules.GoodEnoughPoints == nil || *rules.GoodEnoughPoints != rules.MinPoints {
		return false
	}
	for _, s := range rules.Steps {
		if s.Allowed {
			return s.Quality == rules.GoodEnoughAt
		}
	}
	return false
}

// FileFacts are what is known about a File in the Library besides its
// name, for traits that test languages and size: its size, the audio
// languages its inspection read (none when it read none, and then the
// name's count) and the title's original language.
type FileFacts struct {
	Size             int64
	Audio            []string
	OriginalLanguage string
}

// OwnedFileFacts are a Library File's facts: its size, its audio track
// languages (else its Version's, FileLanguages) and its title's original
// language.
func OwnedFileFacts(t Title, f File, v Version) FileFacts {
	facts := FileFacts{Size: f.Size, Audio: FileLanguages(f.MediaInfo, v.AudioLanguages)}
	if t.OriginalLanguage != nil {
		facts.OriginalLanguage = *t.OriginalLanguage
	}
	return facts
}

// OwnedReachesGoodEnoughFile is OwnedReachesGoodEnough with what else is
// known about the file (OwnedFileFacts), as Radarr and Sonarr judge a
// recorded file by the languages its media info gives: the points of
// language and size traits count for it as they would for a Release.
func OwnedReachesGoodEnoughFile(name string, mediaInfo map[string]any, f FileFacts, rules PickRulesBody) bool {
	judge := JudgeName(name, mediaInfo)
	p := releasename.Parse(judge, hintsFor(rules))
	p.Size, p.AudioLanguages, p.OriginalLanguage = f.Size, f.Audio, f.OriginalLanguage
	if idx := stepIndex(rules, stepFor(rules, p)); idx >= 0 && idx >= stepIndex(rules, rules.GoodEnoughAt) &&
		(rules.GoodEnoughPoints == nil || sumPoints(p, rules) >= *rules.GoodEnoughPoints) {
		return true
	}
	if p.Music != nil || p.Source.Value != "" || p.Resolution.Value == 0 {
		return false
	}
	need := stepResolution(rules.GoodEnoughAt)
	return need > 0 && p.Resolution.Value >= need
}

// TraitMatches says whether a trait matches a read name (with its size,
// audio and original language set), and what it saw. The parity test with
// the old apps uses it.
func TraitMatches(trait string, p releasename.Result) (bool, string) { return traitMatches(trait, p) }

// PointsOf is the points Pick rules give a read name.
func PointsOf(p releasename.Result, rules PickRulesBody) int { return sumPoints(p, rules) }

// oldAppSteps maps Sonarr's and Radarr's qualities onto the ladder, as an
// import maps a profile's qualities (a full Blu-ray disc has no step).
var oldAppSteps = map[string]string{
	"WORKPRINT": "cam", "CAM": "cam", "TELESYNC": "telesync", "TELECINE": "telesync",
	"REGIONAL": "sd_dvd", "DVDSCR": "sd_dvd", "DVD": "sd_dvd", "DVD-R": "sd_dvd", "Bluray-480p": "sd_dvd", "Bluray-576p": "sd_dvd",
	"SDTV": "sd_tv", "WEBDL-480p": "sd_tv", "WEBRip-480p": "sd_tv",
	"HDTV-720p": "720p_tv", "WEBDL-720p": "720p_web", "WEBRip-720p": "720p_web", "Bluray-720p": "720p_bluray",
	"HDTV-1080p": "1080p_tv", "Raw-HD": "1080p_tv", "WEBDL-1080p": "1080p_web", "WEBRip-1080p": "1080p_web",
	"Bluray-1080p": "1080p_bluray", "Remux-1080p": "1080p_remux",
	"HDTV-2160p": "2160p_web", "WEBDL-2160p": "2160p_web", "WEBRip-2160p": "2160p_web", "Bluray-2160p": "2160p_bluray", "Remux-2160p": "2160p_remux",
}

// stepFor is the ladder step of a read name under these Pick rules: for
// rules imported from Sonarr or Radarr, the step of the quality those apps
// read (ReadOldAppQuality), so they take and refuse as there; else StepFor.
func stepFor(rules PickRulesBody, p releasename.Result) string {
	if !rules.Synced.OldApp() || p.Music != nil || p.Original == "" {
		return StepFor(p)
	}
	return oldAppSteps[releasename.ReadOldAppQuality(p.Original).Name]
}

// oldAppLanguages are a Release's languages as Sonarr and Radarr settle
// them: those the name gives (Bokmål and Nynorsk as Norwegian), with the
// title's own language when the name gives none or says it keeps it
// (ORIGINAL, NORDiC).
func oldAppLanguages(p releasename.Result, original string) []string {
	var out []string
	for _, l := range p.Languages {
		if l = sameLanguage(l); !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	if original = sameLanguage(original); original != "" && (len(out) == 0 || p.Has("original_audio")) && !slices.Contains(out, original) {
		out = append(out, original)
	}
	return out
}
