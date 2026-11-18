package migrate

import (
	"fmt"
	"testing"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
)

// cunkFive is a show TMDB finds only by the old app's TMDB id, whose
// season 1 has five episodes aired a week apart.
func cunkFive() (metadata.Show, []string, []TitleFile, []Episode) {
	sh := metadata.Show{Source: "tmdb", Name: "Cunk on...", Year: 2022, IDs: map[string]string{"tmdb": "79063"},
		OtherNames: []string{"Cunk on Earth"}, Seasons: []metadata.Season{{Number: 1}}}
	names := []string{"In the Beginnings", "Faith/Off", "Renaissance Woman", "Industrial Revolutions", "A Brave New World"}
	var files []string
	var old []TitleFile
	var eps []Episode
	for i, n := range names {
		aired := time.Date(2022, 9, 20+i, 21, 0, 0, 0, time.UTC)
		day := aired.Format("2006-01-02")
		sh.Episodes = append(sh.Episodes, metadata.Episode{Season: 1, Number: i + 1, Name: n, AirDate: &aired, AirDay: day})
		name := fmt.Sprintf("Cunk on Earth - S01E%02d - x WEBDL-1080p.mkv", i+1)
		files = append(files, "Season 01/"+name)
		old = append(old, TitleFile{ID: int64(i + 1), Path: "/tv/Cunk on Earth/Season 01/" + name, Quality: "WEBDL-1080p"})
		eps = append(eps, Episode{Season: 1, Number: i + 1, Monitored: true, FileID: int64(i + 1), AirDate: day, Title: n})
	}
	return sh, files, old, eps
}

// The old app's episodes air on the same days under the same names as
// TMDB's: every file is placed at import.
func TestSeasonsDifferPlacedWhenDateAndNameAgree(t *testing.T) {
	sh, files, old, eps := cunkFive()
	// Names agree once case and punctuation are set aside.
	eps[1].Title = "faith off"
	l, _ := cunkSetupOld(t, cunkSonarr, []metadata.Show{sh}, old, eps, files...)
	l.move(t, Choices{})
	if n := l.count(t, showFilled); n != 5 {
		t.Fatalf("%d placed", n)
	}
	if n := l.count(t, `SELECT count(*) FROM files WHERE leftover_reason = 'seasons_differ' AND slot_ids = '[]'`); n != 0 {
		t.Fatalf("%d wait", n)
	}
}

// A file whose old episode has the same day but another name, or the same
// name on another day, keeps waiting; the others are placed.
func TestSeasonsDifferWaitsUnlessBothAgree(t *testing.T) {
	sh, files, old, eps := cunkFive()
	eps[3].Title = "Something Else"
	eps[4].AirDate = "2022-10-30"
	l, _ := cunkSetupOld(t, cunkSonarr, []metadata.Show{sh}, old, eps, files...)
	l.move(t, Choices{})
	if n := l.count(t, showFilled); n != 3 {
		t.Fatalf("%d placed", n)
	}
	if n := l.count(t, `SELECT count(*) FROM files WHERE leftover_reason = 'seasons_differ' AND slot_ids = '[]'`); n != 2 {
		t.Fatalf("%d wait", n)
	}
}

// A base name two recorded files share gets no old episodes: which file
// they belong to is unknown.
func TestOldEpisodesSkipsRepeatedBaseNames(t *testing.T) {
	ti := Title{Files: []TitleFile{{ID: 1, Path: "/tv/X/Season 01/Episode 01.mkv"}, {ID: 2, Path: "/tv/X/Season 02/Episode 01.mkv"}, {ID: 3, Path: `\tv\X\x.mkv`}},
		Episodes: []Episode{{Season: 1, Number: 1, FileID: 1}, {Season: 2, Number: 1, FileID: 2}, {Season: 1, Number: 2, FileID: 3, AirDate: "2022-09-27", Title: "Gods"}}}
	eps := oldEpisodes(ti)
	if _, ok := eps["Episode 01.mkv"]; ok || len(eps["x.mkv"]) != 1 || eps["x.mkv"][0] != (OldEpisode{Season: 1, Number: 2, AirDate: "2022-09-27", Title: "Gods"}) {
		t.Fatalf("eps = %v", eps)
	}
}
