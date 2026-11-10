package library

import (
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
)

// KnownFile is a video file an old app recorded in a folder being adopted:
// where it is, the quality the old app judged it in words the release name
// reader knows ("2160p WEB-DL"), and for a show the episodes it holds. The
// move fills these, so an adopted title keeps the files and quality the old
// app had instead of guessing them from the file names again.
type KnownFile struct {
	// Path is relative to the Library folder, in slash form.
	Path    string
	Quality string
	// Episodes are season and episode numbers (shows only).
	Episodes [][2]int
}

// NameWithQuality is a file's name that also says the quality an old app
// recorded for it: the name as it is when it already reads as that
// resolution and source, else the name with the quality words after it in
// brackets. It is kept as the File's release name, so every later check
// (good enough, renaming, reading the quality again) reads the same quality.
func NameWithQuality(name, quality string) string {
	quality = strings.TrimSpace(quality)
	if quality == "" {
		return name
	}
	// The reader takes a name's first word for its title: one stands in.
	want := releasename.Parse("x "+quality, releasename.Hints{})
	got := releasename.Parse(name, releasename.Hints{})
	if (want.Resolution.Value == 0 || got.Resolution.Value == want.Resolution.Value) && (want.Source.Value == "" || got.Source.Value == want.Source.Value) {
		return name
	}
	return name + " [" + quality + "]"
}

// knownMain picks the main file of a movie folder from the files the old
// app recorded there: the biggest one that is among the folder's videos.
func knownMain(known []KnownFile, vids []AdoptVideo) (AdoptVideo, KnownFile, bool) {
	var best AdoptVideo
	var bestKnown KnownFile
	found := false
	for _, k := range known {
		for _, v := range vids {
			if v.Path == k.Path && (!found || v.Size > best.Size) {
				best, bestKnown, found = v, k, true
			}
		}
	}
	return best, bestKnown, found
}
