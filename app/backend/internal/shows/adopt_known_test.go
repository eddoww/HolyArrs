package shows

import (
	"context"
	"reflect"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/library"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// A show adopted with the files an old app recorded takes their episodes
// and quality from the old app, not from the names: a file named for
// S01E02 that the old app recorded as S01E01 fills S01E01, and the episode
// the old app did not watch starts not kept up to date.
func TestAdoptShowWithRecordedFiles(t *testing.T) {
	svc, s, _, _ := addShowService(t)
	ctx := context.Background()
	lf := addShowFolder(t, s)
	showTree(t, lf.Path)
	before := snapshot(t, lf.Path)
	known := []library.KnownFile{
		{Path: "Lab Show (2025)/Season 1/Lab Show - S01E02 - Second.mkv", Quality: "1080p WEB-DL", Episodes: [][2]int{{1, 1}}},
		{Path: "Lab Show (2025)/Season 2/Lab.Show.S02E01E02.720p.HDTV.x264-GRP.mkv", Quality: "720p HDTV", Episodes: [][2]int{{2, 1}, {2, 2}}},
	}
	res, err := svc.Adopt(ctx, lf.ID, []library.AdoptChoice{{Folder: "Lab Show (2025)", ID: "tmdb:100", Known: known, EpisodesOff: [][2]int{{2, 3}}}}, domain.System)
	if err != nil || res[0].Error != "" {
		t.Fatalf("adopt %+v %v", res, err)
	}
	if !reflect.DeepEqual(snapshot(t, lf.Path), before) {
		t.Fatal("adopting moved, renamed or wrote a file")
	}
	id := res[0].TitleID
	eps := episodes(t, s, id)
	files, _ := database.Select[mediafiles.File](ctx, s.Read(), "files", "slot_ids LIKE ? ", "%")
	byPath := map[string]mediafiles.File{}
	for _, f := range files {
		byPath[f.RelativePath] = f
	}
	second := byPath["Lab Show (2025)/Season 1/Lab Show - S01E02 - Second.mkv"]
	if len(second.SlotIDs) != 1 || second.SlotIDs[0] != eps["S01E01"].SlotID || second.Quality.Resolution != 1080 || second.Quality.Source != "web_dl" {
		t.Fatalf("recorded file %+v", second)
	}
	// The bigger S01E01 file the old app did not record holds no episode now.
	if f, ok := byPath["Lab Show (2025)/Season 1/Lab.Show.S01E01.1080p.WEB-DL.H264-GRP.mkv"]; ok && len(f.SlotIDs) > 0 {
		t.Fatalf("an unrecorded file took a recorded episode: %+v", f)
	}
	if eps["S02E03"].Keep || !eps["S02E01"].Keep {
		t.Fatalf("watched flags: S02E03 %v, S02E01 %v", eps["S02E03"].Keep, eps["S02E01"].Keep)
	}
}
