package migrate

import (
	"fmt"
	"path"
	"strings"
	"unicode"

	"github.com/HolyArrs/holyarrs/app/backend/internal/library"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
	"github.com/HolyArrs/holyarrs/app/backend/internal/sources"
)

// recorded puts the files the old app recorded for a title on its plan,
// with their place in the Library folder here, their quality and their
// episodes. It says how
// many recorded files lie outside the title's folder (they are left out).
func (b *builder) recorded(tp *TitlePlan, t Title) int {
	dir := oldPath(t.Path)
	// A title already here takes the recorded quality by file name.
	for _, f := range t.Files {
		if f.Quality == "" || f.Path == "" {
			continue
		}
		if tp.Qualities == nil {
			tp.Qualities = map[string]string{}
		}
		tp.Qualities[path.Base(strings.ReplaceAll(f.Path, `\`, "/"))] = f.Quality
	}
	tp.OldEpisodes = oldEpisodes(t)
	lost := 0
	for _, f := range t.Files {
		op := oldPath(f.Path)
		if dir == "" || op == "" || !under(op, dir) || op == dir {
			lost++
			continue
		}
		fp := FilePlan{Path: path.Join(tp.Folder, strings.TrimPrefix(op, dir+"/")), Quality: clip(f.Quality, 40), ReleaseName: f.ReleaseName}
		for _, e := range t.Episodes {
			if f.ID != 0 && e.FileID == f.ID && len(fp.Episodes) < 50 {
				fp.Episodes = append(fp.Episodes, [2]int{e.Season, e.Number})
			}
		}
		tp.Files = append(tp.Files, fp)
	}
	// A snapshot a fork build read before upstream's EpisodesOff holds the
	// switched-off episodes in Episodes only.
	if len(tp.EpisodesOff) == 0 {
		off := map[int]bool{}
		for _, n := range tp.SeasonsOff {
			off[n] = true
		}
		for _, e := range t.Episodes {
			if !e.Monitored && !off[e.Season] && e.Season >= 0 && e.Season < 10000 && e.Number >= 0 {
				tp.EpisodesOff = append(tp.EpisodesOff, sources.EpisodeNumber{Season: e.Season, Episode: e.Number})
			}
		}
	}
	return lost
}

// OldEpisode is one episode as the old app lists it.
type OldEpisode = library.OldEpisode

// oldEpisodes is the old app's episodes in each recorded file, by the
// file's base name (nil when there are none), for a show whose seasons
// differ here. A base name two files share gets none: which file the
// episodes belong to is unknown.
func oldEpisodes(t Title) map[string][]OldEpisode {
	seen := map[string]int{}
	for _, f := range t.Files {
		seen[fileBase(f.Path)]++
	}
	var out map[string][]OldEpisode
	for _, f := range t.Files {
		base := fileBase(f.Path)
		if f.ID == 0 || base == "" || seen[base] != 1 {
			continue
		}
		for _, e := range t.Episodes {
			if e.FileID != f.ID {
				continue
			}
			if out == nil {
				out = map[string][]OldEpisode{}
			}
			out[base] = append(out[base], OldEpisode{Season: e.Season, Number: e.Number, AirDate: e.AirDate, Title: e.Title})
		}
	}
	return out
}

func fileBase(p string) string {
	if p == "" {
		return ""
	}
	return path.Base(strings.ReplaceAll(p, `\`, "/"))
}

// recordedFiles puts the old app's recorded files on the plan (recorded),
// lists those outside the title's folder as not moved, and says how many
// came, for the title's line.
func (b *builder) recordedFiles(s *Snapshot, tp *TitlePlan, t Title, from, name string) string {
	if lost := b.recorded(tp, t); lost > 0 {
		b.not(GroupTitles, from+".files", fmt.Sprintf("%s: %d recorded files", name, lost), "they are not inside the title's folder, so they are left to the folder scan")
	}
	if len(tp.Files) == 0 {
		return ""
	}
	return fmt.Sprintf(" with the %d files %s recorded", len(tp.Files), kindName(s.Kind))
}

// namePreset notes the Pick rules a quality profile became, by title kind
// and profile name, for a request app that names the profile.
func (b *builder) namePreset(k Kind, profile, rules string) {
	if lk := libraryKind(k); lk != "" {
		b.namedPresets[lk+":"+strings.ToLower(profile)] = rules
	}
}

// fourKFolder says a Library folder holds 4K copies: a request app's 4K
// server uses it, or its name says 4K, UHD or 2160p ("Movies-4k").
func (b *builder) fourKFolder(p string) bool {
	if b.fourK[p] {
		return true
	}
	for _, w := range strings.FieldsFunc(strings.ToLower(path.Base(p)), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if w == "4k" || w == "uhd" || w == "2160p" {
			return true
		}
	}
	return false
}

// fourKVersions marks the main Version of every movie and show placed in
// a 4K Library folder as its 4K copy (a 4K ask finds it there) once every
// app is read.
func (b *builder) fourKVersions() {
	for i := range b.items[GroupTitles].Items {
		tp := b.items[GroupTitles].Items[i].Title
		if tp == nil || tp.Kind == "artist" || tp.Library == "" || !b.fourKFolder(tp.Library) {
			continue
		}
		tp.FourK = true
		b.items[GroupTitles].Items[i].Detail += "; its copy here is its " + mediafiles.FourKName + " copy"
	}
}

// profileName is the name of a title's quality profile ("" when unknown).
func profileName(s *Snapshot, t Title) string {
	if t.Profile != "" {
		return t.Profile
	}
	for _, p := range s.QualityProfiles {
		if p.ID == t.ProfileID {
			return p.Name
		}
	}
	return ""
}
