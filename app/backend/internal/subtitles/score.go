package subtitles

import (
	"slices"
	"sort"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
	"github.com/HolyArrs/holyarrs/app/backend/internal/subtitles/sites"
)

// Video holds what a candidate is scored against.
type Video struct {
	Kind        string // movie or episode
	Title       string
	Aliases     []string
	Year        int
	Season      int
	Episode     int
	ReleaseName string // the File's release name, or its file name when it has none
	FPS         float64
}

// Part is one line of a score: what was compared and how many points it gave.
type Part struct {
	Name    string `json:"name"`
	Points  int    `json:"points"`
	Matched bool   `json:"matched"`
}

// Score is a candidate's points out of 100 and the parts behind them. Refused
// says why a candidate can never be picked automatically, such as a wrong
// title, a wrong episode or an unwanted forced track. A refused Total is 0.
type Score struct {
	Total   int    `json:"total"`
	Parts   []Part `json:"parts"`
	Refused string `json:"refused,omitempty"`
}

// points holds Bazarr's match points for each scored part as {episode,
// movie}. A score is the matched points as a share of the most a subtitle can
// get without a file hash match, 360 for an episode and 180 for a movie, so
// the pass marks mean what they mean in Bazarr. A valid file hash match is
// worth one point less than everything else together.
var points = map[string][2]int{
	"hash":       {359, 179},
	"name":       {160, 60},
	"year":       {90, 40},
	"season":     {30, 0},
	"episode":    {30, 0},
	"source":     {25, 30},
	"edition":    {0, 30},
	"group":      {20, 15},
	"audio":      {1, 1},
	"resolution": {1, 1},
	"codec":      {1, 1},
	"as_wished":  {1, 1},
	"service":    {1, 1},
}

// maxPoints is the most a subtitle can get without a hash match, {episode, movie}.
var maxPoints = [2]int{360, 180}

var partOrder = []string{"hash", "name", "year", "season", "episode", "group", "source", "resolution", "codec", "audio", "edition", "service", "as_wished"}

func column(kind string) int {
	if kind == "episode" {
		return 0
	}
	return 1
}

// Limits on how much work one site answer may cause while scoring.
const (
	MaxReleaseNames      = 50  // release names scored for each candidate
	MaxCandidatesPerSite = 500 // candidates scored for each site in one search
)

// ScoreCandidate scores one candidate for one wanted language. It reads only
// the first MaxReleaseNames release names so a large answer stays cheap.
func ScoreCandidate(v Video, w Want, c sites.Candidate) Score {
	matched := map[string]bool{}
	refuse := func(why string) Score {
		return Score{Refused: why, Parts: []Part{}}
	}
	if c.Forced != w.Forced {
		return refuse("forced does not match the wish")
	}
	show := v.Kind == "episode"
	vr := releasename.Parse(v.ReleaseName, releasename.Hints{Show: show})

	// Keep the candidate's release name that matches the most release parts.
	var best releasename.Result
	bestN, exact := -1, false
	for _, name := range c.ReleaseNames[:min(len(c.ReleaseNames), MaxReleaseNames)] {
		if len(name) > 512 {
			continue
		}
		if foldName(name) == foldName(v.ReleaseName) && foldName(name) != "" {
			exact = true
		}
		r := releasename.Parse(name, releasename.Hints{Show: show})
		n := len(releaseMatches(vr, r))
		if n > bestN {
			best, bestN = r, n
		}
	}

	// A site that found the subtitle by the title's id or the file's hash has
	// matched the title and its year.
	byID := c.MatchedByID || c.HashMatched
	// Title name.
	name := c.Title
	if name == "" && bestN >= 0 {
		name = best.Title.Value
	}
	switch {
	case byID:
		matched["name"] = true
	case name != "":
		if !sameTitle(name, v.Title, v.Aliases) {
			return refuse("wrong title")
		}
		matched["name"] = true
	}
	// Year.
	year := c.Year
	if year == 0 && bestN >= 0 && best.Year.Conf != releasename.Unknown {
		year = best.Year.Value
	}
	switch {
	case year != 0 && v.Year != 0 && year != v.Year && !byID:
		return refuse("wrong year")
	case byID, year != 0 && year == v.Year:
		matched["year"] = true
	case show && year == 0 && matched["name"]:
		// A show is named without its year, so a subtitle for it carries none;
		// Bazarr counts the year then too.
		matched["year"] = true
	}
	// Season and episode.
	if show {
		se, ep := c.Season, c.Episode
		if se == 0 && bestN >= 0 && best.Show != nil && len(best.Show.Seasons) == 1 {
			se = best.Show.Seasons[0]
		}
		if ep == 0 && bestN >= 0 && best.Show != nil && len(best.Show.Episodes) > 0 {
			ep = best.Show.Episodes[0]
			if slices.Contains(best.Show.Episodes, v.Episode) {
				ep = v.Episode
			}
		}
		if (se != 0 && se != v.Season) || (ep != 0 && ep != v.Episode) {
			return refuse("wrong episode")
		}
		// A hash match without numbers was found for this very file.
		matched["season"], matched["episode"] = se != 0 || c.HashMatched, ep != 0 || c.HashMatched
	}
	// Release parts.
	if exact {
		for _, p := range []string{"group", "source", "resolution", "codec", "audio", "edition"} {
			matched[p] = true
		}
		matched["service"] = service(v.ReleaseName) != ""
	} else if bestN >= 0 {
		for _, p := range releaseMatches(vr, best) {
			matched[p] = true
		}
	}
	// Like Bazarr, a video and a subtitle that both name no edition match on it.
	if len(vr.Edition) == 0 && (bestN < 0 || len(best.Edition) == 0) {
		matched["edition"] = true
	}
	// Hearing impaired as wanted. Forced was already checked above.
	switch w.HearingImpaired {
	case HIOnly:
		matched["as_wished"] = c.HearingImpaired
	case HIEither:
		matched["as_wished"] = true
	default:
		matched["as_wished"] = !c.HearingImpaired
	}
	// A file hash match counts only when the subtitle's own details back it up,
	// as in Bazarr: the right episode and source for an episode, and the right
	// source and codec for a movie. It then stands for every part but the
	// hearing-impaired choice.
	if c.HashMatched && hashBacked(show, matched) {
		matched = map[string]bool{"hash": true, "as_wished": matched["as_wished"]}
	}
	return tally(v.Kind, matched)
}

// hashBacked reports whether the other matches back a file hash match.
func hashBacked(show bool, matched map[string]bool) bool {
	need := []string{"source", "codec"}
	if show {
		need = []string{"name", "season", "episode", "source"}
	}
	for _, p := range need {
		if !matched[p] {
			return false
		}
	}
	return true
}

// tally turns matched parts into a score out of 100, rounded down so a pass
// mark is met only by the full share. Each part shows its share; the shares
// add up to the total.
func tally(kind string, matched map[string]bool) Score {
	col := column(kind)
	raw := 0
	for _, p := range partOrder {
		if matched[p] {
			raw += points[p][col]
		}
	}
	total := min(raw*100/maxPoints[col], 100)
	s := Score{Total: total, Parts: []Part{}}
	type share struct {
		i    int
		rest int
	}
	var rests []share
	given := 0
	for _, p := range partOrder {
		pts := points[p][col]
		if pts == 0 || (p == "hash" && !matched["hash"]) {
			continue
		}
		part := Part{Name: p, Matched: matched[p]}
		if part.Matched {
			part.Points = pts * 100 / maxPoints[col]
			given += part.Points
			rests = append(rests, share{len(s.Parts), pts * 100 % maxPoints[col]})
		}
		s.Parts = append(s.Parts, part)
	}
	// Hand the points lost to rounding to the parts with the largest remainders.
	sort.SliceStable(rests, func(i, j int) bool { return rests[i].rest > rests[j].rest })
	for i := 0; given < total && i < len(rests); i++ {
		s.Parts[rests[i].i].Points++
		given++
	}
	return s
}

// releaseMatches lists the release parts two parsed names have in common.
// Release groups count with their known equivalents, sources by family (a
// remux is a Blu-ray, a web rip is web), as the old apps' name reader does.
func releaseMatches(v, c releasename.Result) []string {
	var out []string
	if v.Group.Value != "" && sameGroup(v.Group.Value, c.Group.Value) {
		out = append(out, "group")
	}
	if v.Source.Value != "" && sourceFamily(v.Source.Value) == sourceFamily(c.Source.Value) {
		out = append(out, "source")
	}
	if v.Resolution.Value != 0 && v.Resolution.Value == c.Resolution.Value {
		out = append(out, "resolution")
	}
	if v.Codec.Value != "" && v.Codec.Value == c.Codec.Value {
		out = append(out, "codec")
	}
	if v.Audio.Format != "" && v.Audio.Format == c.Audio.Format {
		out = append(out, "audio")
	}
	for _, e := range v.Edition {
		if slices.Contains(c.Edition, e) {
			out = append(out, "edition")
			break
		}
	}
	if sv := service(v.Original); sv != "" && sv == service(c.Original) {
		out = append(out, "service")
	}
	return out
}

// equalGroups are release groups that put out the same releases.
var equalGroups = [][]string{{"framestor", "w4nk3r", "bhdstudio"}, {"lol", "dimension"}, {"asap", "immerse", "fleet"}, {"avs", "sva"}}

func sameGroup(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if a == b {
		return true
	}
	for _, g := range equalGroups {
		if slices.Contains(g, a) && slices.Contains(g, b) {
			return true
		}
	}
	return false
}

func sourceFamily(s string) string {
	switch s {
	case "bluray", "remux", "disc_image":
		return "bluray"
	case "web_dl", "web_rip":
		return "web"
	}
	return s
}

// services are the streaming service marks in release names.
var services = []string{"amzn", "nf", "dsnp", "hmax", "atvp", "hulu", "pcok", "pmtp", "stan", "crav", "hbo"}

// service returns the streaming service a release name carries, or "".
func service(name string) string {
	for _, t := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') }) {
		if slices.Contains(services, t) {
			return t
		}
	}
	return ""
}

func foldName(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127 {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func sameTitle(name, title string, aliases []string) bool {
	n := foldName(name)
	if n == "" {
		return false
	}
	for _, t := range append([]string{title}, aliases...) {
		if foldName(t) == n {
			return true
		}
	}
	return false
}

// Scored is a candidate paired with its score.
type Scored struct {
	sites.Candidate
	Score    Score `json:"score"`
	Priority int   `json:"-"` // site priority, lower goes first
	Order    int   `json:"-"` // position of the site in the list
}

// Rank sorts candidates best first by score, then trust, site priority, most
// downloads and site order. Refused candidates always sort last.
func Rank(cs []Scored) {
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if (a.Score.Refused == "") != (b.Score.Refused == "") {
			return a.Score.Refused == ""
		}
		if a.Score.Total != b.Score.Total {
			return a.Score.Total > b.Score.Total
		}
		if a.Trusted != b.Trusted {
			return a.Trusted
		}
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if a.Downloads != b.Downloads {
			return a.Downloads > b.Downloads
		}
		return a.Order < b.Order
	})
}
