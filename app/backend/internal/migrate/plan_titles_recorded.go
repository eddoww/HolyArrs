package migrate

import (
	"path"
	"strings"
	"unicode"

	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

// recorded puts the files the old app recorded for a title on its plan,
// with their place in the Library folder here, their quality and their
// episodes, and a show's episodes the old app did not watch. It says how
// many recorded files lie outside the title's folder (they are left out).
func (b *builder) recorded(tp *TitlePlan, t Title) int {
	dir := oldPath(t.Path)
	off := map[int]bool{}
	for _, n := range tp.SeasonsOff {
		off[n] = true
	}
	lost := 0
	for _, f := range t.Files {
		op := oldPath(f.Path)
		if dir == "" || op == "" || !under(op, dir) || op == dir {
			lost++
			continue
		}
		fp := FilePlan{Path: path.Join(tp.Folder, strings.TrimPrefix(op, dir+"/")), Quality: qualityWords(f), ReleaseName: f.ReleaseName}
		for _, e := range t.Episodes {
			if f.ID != 0 && e.FileID == f.ID && len(fp.Episodes) < 50 {
				fp.Episodes = append(fp.Episodes, [2]int{e.Season, e.Number})
			}
		}
		tp.Files = append(tp.Files, fp)
	}
	for _, e := range t.Episodes {
		if !e.Monitored && !off[e.Season] && e.Season >= 0 && e.Season < 10000 && e.Number >= 0 {
			tp.EpisodesOff = append(tp.EpisodesOff, [2]int{e.Season, e.Number})
		}
	}
	return lost
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

// fourKVersions names the main Version of every movie and show placed in a
// 4K Library folder "4K", so it counts as the title's 4K copy (a 4K ask
// finds it there) once every app is read.
func (b *builder) fourKVersions() {
	for i := range b.items[GroupTitles].Items {
		tp := b.items[GroupTitles].Items[i].Title
		if tp == nil || tp.Kind == "artist" || tp.Library == "" || !b.fourKFolder(tp.Library) {
			continue
		}
		tp.VersionLabel = mediafiles.FourKName
		b.items[GroupTitles].Items[i].Detail += "; its copy here is its " + mediafiles.FourKName + " Version"
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
