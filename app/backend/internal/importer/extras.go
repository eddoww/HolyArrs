package importer

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
)

// Extra files, as Sonarr and Radarr's "import extra files": the subtitle
// files of a download (by default srt, sub, idx, ass and ssa) are placed
// beside the video they belong to, named after it, so the subtitles part
// finds them as subtitles beside the video. An extra belongs to a video
// when it sits beside it and starts with its name (Movie.en.srt beside
// Movie.mkv), when it sits in a folder named after the video (Subs/Movie/
// 2_English.srt), or, when the import placed one video, anywhere in the
// download (Subs/English.srt). It is hardlinked like the video, and an
// extra never replaces a file already there.

// DefaultExtraExtensions are the extra file types imported unless changed.
var DefaultExtraExtensions = []string{"srt", "sub", "idx", "ass", "ssa"}

// ExtraPlaced is one extra file an import placed.
type ExtraPlaced struct {
	From     string `json:"from"`
	To       string `json:"to"`
	PlacedBy string `json:"placed_by"`
}

// extraFor is one extra file matched to a placed video, with the part of
// its new name that follows the video's name (".en.srt").
type extraFor struct {
	source, suffix string
}

// placeExtras places the extra files of cands beside the videos the plan
// placed. language names a subtitle language word ("English", "nor") by its
// code, "" when unknown; nil keeps only names that follow the video's own.
func placeExtras(cands []Candidate, plan []mediafiles.PlanStep, results []mediafiles.PlaceResult, rules FileRules, ops Ops, language func(string) string) []ExtraPlaced {
	if !rules.ImportExtraFiles || len(rules.ExtraFileExtensions) == 0 {
		return nil
	}
	exts := map[string]bool{}
	for _, e := range rules.ExtraFileExtensions {
		exts["."+e] = true
	}
	var videos []int
	for i, st := range plan {
		if i < len(results) && st.Method == "hardlink" && results[i].Outcome == "placed" && videoExt[strings.ToLower(filepath.Ext(st.SourceFile))] {
			videos = append(videos, i)
		}
	}
	if len(videos) == 0 {
		return nil
	}
	if ops.Link == nil {
		ops.Link = os.Link
	}
	if ops.Copy == nil {
		ops.Copy = copyFile
	}
	var extras []Candidate
	for _, c := range cands {
		if exts[strings.ToLower(filepath.Ext(c.Path))] && !isSample(c.Path, releasename.Parse(filepath.Base(c.Path), releasename.Hints{})) {
			extras = append(extras, c)
		}
	}
	taken := map[string]bool{}
	byVideo := map[int][]extraFor{}
	// Beside the video and named after it.
	for _, vi := range videos {
		stem, dir := stemOf(plan[vi].SourceFile), filepath.Dir(plan[vi].SourceFile)
		for _, c := range extras {
			base := filepath.Base(c.Path)
			if taken[c.Path] || filepath.Dir(c.Path) != dir || len(base) <= len(stem)+1 || !strings.EqualFold(base[:len(stem)+1], stem+".") {
				continue
			}
			taken[c.Path] = true
			suffix := base[len(stem):]
			byVideo[vi] = append(byVideo[vi], extraFor{c.Path, strings.TrimSuffix(suffix, filepath.Ext(suffix)) + strings.ToLower(filepath.Ext(suffix))})
		}
	}
	// In a folder named after the video, then anywhere when there is one video.
	for _, vi := range videos {
		stem := stemOf(plan[vi].SourceFile)
		for _, c := range extras {
			if !taken[c.Path] && (strings.EqualFold(filepath.Base(filepath.Dir(c.Path)), stem) || len(videos) == 1) {
				taken[c.Path] = true
				byVideo[vi] = append(byVideo[vi], extraFor{c.Path, extraSuffix(c.Path, language)})
			}
		}
	}
	var out []ExtraPlaced
	for _, vi := range videos {
		target := plan[vi].TargetPath
		targetStem := strings.TrimSuffix(target, filepath.Ext(target))
		used := map[string]bool{}
		for _, x := range byVideo[vi] {
			to := freeExtraName(targetStem, x.suffix, used)
			used[strings.ToLower(to)] = true
			r := place(mediafiles.PlanStep{SourceFile: x.source, TargetPath: to, Method: "hardlink"}, ops, time.Time{})
			if r.Outcome == "placed" {
				out = append(out, ExtraPlaced{From: x.source, To: to, PlacedBy: r.PlacedBy})
			}
		}
	}
	return out
}

// stemOf is a file's name without its folder and extension.
func stemOf(p string) string {
	return strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
}

// extraSuffix builds the end of an extra's new name from its own name: the
// language, then forced and hearing impaired marks, then the extension
// (".en.forced.srt"). An unknown language is left out.
func extraSuffix(p string, language func(string) string) string {
	words := strings.FieldsFunc(strings.ToLower(stemOf(p)), func(r rune) bool { return !unicode.IsLetter(r) })
	out := ""
	// Only the last two words can name the language, so a word of a
	// release name ("It") is not taken for one; marks are never languages.
	for i, seen := len(words)-1, 0; i >= 0 && seen < 2 && out == "" && language != nil; i-- {
		switch words[i] {
		case "forced", "foreign", "sdh", "hi", "cc":
			continue
		}
		seen++
		if code := language(words[i]); code != "" {
			out = "." + code
		}
	}
	for _, w := range words {
		if w == "forced" || w == "foreign" {
			out += ".forced"
			break
		}
	}
	for _, w := range words {
		if w == "sdh" || w == "hi" || w == "cc" {
			out += ".hi"
			break
		}
	}
	return out + strings.ToLower(filepath.Ext(p))
}

// freeExtraName is stem+suffix, or with a number before the extension when
// another extra of this import already took that name.
func freeExtraName(stem, suffix string, used map[string]bool) string {
	ext := filepath.Ext(suffix)
	mid := strings.TrimSuffix(suffix, ext)
	to := stem + suffix
	for n := 2; used[strings.ToLower(to)] && n < 100; n++ {
		to = stem + mid + "." + strconv.Itoa(n) + ext
	}
	return to
}
