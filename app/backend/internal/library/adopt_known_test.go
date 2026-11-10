package library

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

func TestNameWithQuality(t *testing.T) {
	for _, c := range []struct{ name, quality, want string }{
		{"Heat (1995) Bluray-720p", "", "Heat (1995) Bluray-720p"},
		// The name already says the recorded quality: kept as it is.
		{"Heat (1995) Bluray-720p", "720p BluRay", "Heat (1995) Bluray-720p"},
		// The name says another quality, or none: the recorded one is added.
		{"Heat (1995) Bluray-720p", "1080p BluRay", "Heat (1995) Bluray-720p [1080p BluRay]"},
		{"bbb_sunflower_native_60fps_normal", "2160p HDTV", "bbb_sunflower_native_60fps_normal [2160p HDTV]"},
		{"Show - S01E02 - Title", "1080p WEB-DL", "Show - S01E02 - Title [1080p WEB-DL]"},
	} {
		if got := NameWithQuality(c.name, c.quality); got != c.want {
			t.Errorf("%q + %q = %q, want %q", c.name, c.quality, got, c.want)
		}
	}
}

// A movie folder adopted with the file the old app recorded keeps that
// file as its main one, even when another video is bigger, and the quality
// the old app judged it: no file is touched.
func TestAdoptKeepsTheRecordedFileAndQuality(t *testing.T) {
	s := open(t)
	lf := addFolder(t, s)
	writeFile(t, filepath.Join(lf.Path, "Heat (1995)", "Heat (1995) Bluray-720p.mkv"), strings.Repeat("h", 100))
	writeFile(t, filepath.Join(lf.Path, "Heat (1995)", "extras", "Behind the scenes.mkv"), strings.Repeat("x", 300))
	before := snapshot(t, lf.Path)
	svc := &Service{Store: s, Metadata: adoptTMDB, Now: func() time.Time { return now }}
	ctx := context.Background()
	res, err := svc.Adopt(ctx, lf.ID, []AdoptChoice{{Folder: "Heat (1995)", TMDB: "949",
		Known: []KnownFile{{Path: "Heat (1995)/Heat (1995) Bluray-720p.mkv", Quality: "1080p BluRay"}}}}, domain.System)
	if err != nil || res[0].Error != "" {
		t.Fatalf("adopt %+v %v", res, err)
	}
	if fmt.Sprint(snapshot(t, lf.Path)) != fmt.Sprint(before) {
		t.Fatal("adopting changed files on disk")
	}
	files, _ := database.Select[mediafiles.File](ctx, s.Read(), "files", "1=1 ORDER BY id")
	if len(files) != 1 || files[0].RelativePath != "Heat (1995)/Heat (1995) Bluray-720p.mkv" {
		t.Fatalf("files %+v", files)
	}
	f := files[0]
	if f.Quality.Resolution != 1080 || f.Quality.Source != "bluray" || f.ReleaseName == nil || *f.ReleaseName != "Heat (1995) Bluray-720p [1080p BluRay]" {
		t.Fatalf("quality %+v release %v", f.Quality, *f.ReleaseName)
	}
	// Reading the quality again later reads the recorded one.
	if n, err := svc.RejudgeAdopted(ctx); err != nil || n != 0 {
		t.Fatalf("rejudge changed %d %v", n, err)
	}
}
