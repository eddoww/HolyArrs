package migrate

import (
	"context"
	"slices"
	"strings"
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

// A qBittorrent Radarr and Sonarr both used becomes one Downloader on
// HolyArrs' own categories, managing only those, so it never touches the
// old apps' torrents while they still run.
func TestMoveSharedDownloaderUsesOwnCategories(t *testing.T) {
	l := newMoveLabWith(t, func(radarr, _, _ *Snapshot) {
		radarr.DownloadClients[0].Settings["movieCategory"] = "radarr"
		radarr.DownloadClients[0].Settings["movieImportedCategory"] = "radarr-imported"
	})
	sonarr := &Snapshot{Kind: Sonarr, Version: "4.0.20", Via: ViaAPI, DownloadClients: []Provider{{Name: "qBit", Kind: "QBittorrent", Enabled: true, Priority: 1,
		Settings: map[string]any{"host": "qbit", "port": int64(8080), "username": "admin", "tvCategory": "sonarr", "tvImportedCategory": "sonarr-imported"}}}}
	sonarr.finish()
	saved, err := Save(context.Background(), l.st, 1, sonarr)
	if err != nil {
		t.Fatal(err)
	}
	l.snaps = append(l.snaps, saved.ID)
	mv := l.move(t, Choices{})
	if mv.State != "done" {
		t.Fatalf("move %+v", mv)
	}
	if n := l.count(t, `SELECT count(*) FROM downloaders`); n != 1 {
		t.Fatalf("%d Downloaders, want the shared client once", n)
	}
	if n := l.count(t, `SELECT count(*) FROM downloaders WHERE category = 'holyarrs' AND post_import_category = 'holyarrs-imported'
		AND own_categories_only = 1 AND state = 'disabled'`); n != 1 {
		t.Fatal("the shared Downloader is not on HolyArrs' own categories")
	}
	if !strings.Contains(lineOf(t, mv.Report, "downloader:http://qbit:8080").Detail, "downloads into the holyarrs category, moves imported torrents to holyarrs-imported") {
		t.Fatalf("line %+v", lineOf(t, mv.Report, "downloader:http://qbit:8080"))
	}
}
