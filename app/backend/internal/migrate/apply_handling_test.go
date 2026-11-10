package migrate

import (
	"context"
	"slices"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/importer"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/jobs"
)

// A move sets Where files go and the feed check from Radarr's download
// handling, and makes the qBittorrent Downloader keep Radarr's categories,
// managing only those; undo puts the settings back.
func TestMoveDownloadHandlingAndCategories(t *testing.T) {
	l := newMoveLabWith(t, func(radarr, _, _ *Snapshot) {
		radarr.DownloadClients[0].Settings["movieCategory"] = "radarr"
		radarr.DownloadClients[0].Settings["movieImportedCategory"] = "radarr-imported"
		radarr.DownloadHandling = map[string]any{"minimumfreespacewhenimporting": int64(250), "importextrafiles": true,
			"extrafileextensions": "srt,ass", "rsssyncinterval": int64(30)}
	})
	ctx := context.Background()
	mv := l.move(t, Choices{})
	if mv.State != "done" {
		t.Fatalf("move %+v", mv)
	}
	if got := lineOf(t, mv.Report, "download_handling"); got.Result != ResChanged {
		t.Fatalf("line %+v", got)
	}
	r, _ := importer.ReadFileRules(ctx, l.st.Read())
	if r.MinFreeMB != 250 || !r.ImportExtraFiles || !slices.Equal(r.ExtraFileExtensions, []string{"srt", "ass"}) {
		t.Fatalf("file rules %+v", r)
	}
	if every, on, _ := jobs.ScheduleEvery(ctx, l.st.Read(), jobs.KindFeedCheck); every != 30*60 || !on {
		t.Fatalf("feed check every %d on %v", every, on)
	}
	if n := l.count(t, `SELECT count(*) FROM downloaders WHERE category = 'radarr' AND post_import_category = 'radarr-imported'
		AND own_categories_only = 1 AND writable = 1 AND state = 'disabled'`); n != 1 {
		t.Fatal("the Downloader did not keep Radarr's categories")
	}
	l.undo(t, mv.ID)
	r, _ = importer.ReadFileRules(ctx, l.st.Read())
	if r.MinFreeMB != 100 || !slices.Equal(r.ExtraFileExtensions, importer.DefaultExtraExtensions) {
		t.Fatalf("file rules not back %+v", r)
	}
	if every, on, _ := jobs.ScheduleEvery(ctx, l.st.Read(), jobs.KindFeedCheck); every != 15*60 || !on {
		t.Fatalf("feed check not back: every %d on %v", every, on)
	}
}
