package library

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/importer"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

// KnownFile is a video file an old app recorded in a folder being adopted:
// where it is, the quality the old app recorded for it ("WEBDL-2160p"), the
// release it came from, and for a show
// the episodes it holds. The move fills these, so an adopted title keeps the
// files, quality and release names the old app had instead of guessing them
// from the file names again.
type KnownFile struct {
	// Path is relative to the Library folder, in slash form.
	Path    string
	Quality string
	// ReleaseName is the release the file came from ("" when not known).
	ReleaseName string
	// Episodes are season and episode numbers (shows only).
	Episodes [][2]int
}

// KnownRelease is the release name an adopted file keeps: the release the
// old app recorded it came from, else the file's own name (without its
// extension). Radarr and Sonarr test their custom formats on that release
// name too. The quality the old app recorded goes into the File's media
// info (RecordedQuality), where every later reading finds it.
func KnownRelease(fileName, releaseName string) string {
	if releaseName == "" {
		releaseName = strings.TrimSuffix(fileName, path.Ext(fileName))
	}
	return releaseName
}

// RecordedQuality is info with the quality an old app recorded for the file
// ("WEBDL-1080p") under mediafiles.RecordedQualityKey, as SetRecordedQualities
// keeps it; info itself is not changed. No quality leaves info as it is.
func RecordedQuality(info map[string]any, quality string) map[string]any {
	if quality = strings.TrimSpace(quality); quality == "" {
		return info
	}
	out := make(map[string]any, len(info)+1)
	for k, v := range info {
		out[k] = v
	}
	out[mediafiles.RecordedQualityKey] = quality
	return out
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

// AdoptFolderThere checks a chosen folder is on disk, inside the Library
// folder and not through a link.
func AdoptFolderThere(lf mediafiles.LibraryFolder, c AdoptChoice) error {
	if fi, err := os.Lstat(filepath.Join(lf.Path, c.Folder)); err != nil || !fi.IsDir() || !importer.InsideRealFile(lf.Path, filepath.Join(lf.Path, c.Folder)) {
		return errors.New("no such folder in the Library folder")
	}
	return nil
}

// withKnown makes a file the old app recorded the File, with the quality
// and the release it recorded (the fork's Known files). fileOnly says the
// choice is one video file, not a folder.
func (a *Adoption) withKnown(root string, c AdoptChoice, fileOnly bool) {
	var vids []AdoptVideo
	if a.File != "" {
		vids = append(vids, AdoptVideo{Path: a.File, Size: a.Size})
	}
	if !fileOnly {
		vids = append(vids, listVideos(root, c.Folder)...)
	}
	if v, k, ok := knownMain(c.Known, vids); ok {
		a.File, a.Size, a.Quality, a.ReleaseName = v.Path, v.Size, k.Quality, k.ReleaseName
	}
}
