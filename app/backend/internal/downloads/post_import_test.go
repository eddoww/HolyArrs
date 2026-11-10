package downloads

import (
	"context"
	"strings"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// finishedGrab sends the world's Release and syncs it to finished.
func finishedGrab(t *testing.T, w *world, m *Manager) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := m.Grab(ctx, w.slotID, w.relID, domain.System)
	if err != nil {
		t.Fatal(err)
	}
	w.fake.set(hash, map[string]any{"name": "Dune.Part.Two.2024.1080p.WEB.H264-HOLYARRS.mkv", "category": "holyarrs", "progress": 1.0,
		"state": "stalledUP", "save_path": "/data/downloads/complete"})
	w.fake.files[hash] = []map[string]any{{"name": "Dune.Part.Two.2024.1080p.WEB.H264-HOLYARRS.mkv", "size": 29588180, "progress": 1.0}}
	if _, err := m.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if d := w.download(t, id); d.State != "finished" {
		t.Fatalf("download %s", d.State)
	}
	return id
}

func setDownloader(t *testing.T, w *world, q string, args ...any) {
	t.Helper()
	exec41(t, w.s, "UPDATE downloaders SET "+q, args...)
}

// After a full import the torrent moves to the after-import category, like
// the Post-Import Category of the older apps, and the Download is marked
// imported. The files are never touched.
func TestAfterImportMovesTorrentToItsCategory(t *testing.T) {
	w, m := setup(t)
	setDownloader(t, w, "writable = 1, post_import_category = 'holyarrs-imported'")
	id := finishedGrab(t, w, m)
	m.AfterImport(context.Background(), id)
	if len(w.fake.moved) != 1 || w.fake.moved[0] != hash+"|holyarrs-imported" {
		t.Fatalf("setCategory calls %v", w.fake.moved)
	}
	if !strings.Contains(strings.Join(w.fake.cats, ","), "holyarrs-imported") {
		t.Fatalf("the category was not created first: %v", w.fake.cats)
	}
	if len(w.fake.deleted) != 0 {
		t.Fatalf("a torrent was removed: %v", w.fake.deleted)
	}
	if d := w.download(t, id); d.ImportedAt == nil {
		t.Fatal("the Download is not marked imported")
	}
	es, _, err := w.s.Events(context.Background(), database.EventFilter{Kinds: []string{"download_changed"}})
	if err != nil || len(es) != 1 || !strings.Contains(es[0].Summary, "holyarrs-imported") {
		t.Fatalf("events %+v %v", es, err)
	}
}

// No after-import category, or a Downloader HolyArrs may not change: the
// torrent stays where it is, and the Download is still marked imported.
func TestAfterImportLeavesTorrentAlone(t *testing.T) {
	for name, q := range map[string]string{
		"no category": "writable = 1, post_import_category = ''",
		"read only":   "writable = 0, post_import_category = 'holyarrs-imported'",
	} {
		t.Run(name, func(t *testing.T) {
			w, m := setup(t)
			setDownloader(t, w, q)
			id := finishedGrab(t, w, m)
			m.AfterImport(context.Background(), id)
			if len(w.fake.moved) != 0 {
				t.Fatalf("setCategory calls %v", w.fake.moved)
			}
			if d := w.download(t, id); d.ImportedAt == nil {
				t.Fatal("the Download is not marked imported")
			}
		})
	}
}

// The seeding clean-up removes only torrents HolyArrs imported, that the
// client stopped, and that reached the ratio or the seeding time; it always
// keeps their files, so the Library's hardlinks stay.
func TestSeedCleanupRemovesOnlyImportedStoppedTorrents(t *testing.T) {
	w, m := setup(t)
	setDownloader(t, w, `writable = 1, post_import_category = 'holyarrs-imported', seed_goal = '{"ratio":2,"minutes":23040}'`)
	id := finishedGrab(t, w, m)
	ctx := context.Background()
	stopped := map[string]any{"category": "holyarrs-imported", "progress": 1.0, "state": "stoppedUP", "ratio": 1.2, "seeding_time": 3600}
	w.fake.set(hash, stopped)
	const other = "2222222222222222222222222222222222222222"
	w.fake.set(other, map[string]any{"name": "Other", "category": "holyarrs-imported", "progress": 1.0, "state": "stoppedUP", "ratio": 5.0})
	if n, err := m.SeedCleanup(ctx); err != nil || n != 0 || len(w.fake.deleted) != 0 {
		t.Fatalf("not imported yet: %d %v %v", n, err, w.fake.deleted)
	}
	m.AfterImport(ctx, id)
	if n, err := m.SeedCleanup(ctx); err != nil || n != 0 || len(w.fake.deleted) != 0 {
		t.Fatalf("below the goal: %d %v %v", n, err, w.fake.deleted)
	}
	w.fake.set(hash, map[string]any{"state": "uploading", "ratio": 2.5})
	if n, err := m.SeedCleanup(ctx); err != nil || n != 0 || len(w.fake.deleted) != 0 {
		t.Fatalf("still seeding: %d %v %v", n, err, w.fake.deleted)
	}
	w.fake.set(hash, map[string]any{"state": "stoppedUP", "ratio": 0.4, "seeding_time": 16 * 24 * 3600})
	n, err := m.SeedCleanup(ctx)
	if err != nil || n != 1 || len(w.fake.deleted) != 1 || w.fake.deleted[0] != hash+"|false" {
		t.Fatalf("seeding time reached: %d %v %v", n, err, w.fake.deleted)
	}
	es, _, err := w.s.Events(ctx, database.EventFilter{Kinds: []string{"download_removed"}})
	if err != nil || len(es) != 1 || !strings.Contains(es[0].Summary, "its files stay") {
		t.Fatalf("events %+v %v", es, err)
	}
}

// The ratio alone is enough, and pausedUP (qBittorrent before 5) counts as
// stopped. With no goal the clean-up is off.
func TestSeedCleanupRatioAndOff(t *testing.T) {
	w, m := setup(t)
	setDownloader(t, w, `writable = 1, seed_goal = '{"ratio":2}'`)
	id := finishedGrab(t, w, m)
	ctx := context.Background()
	m.AfterImport(ctx, id)
	w.fake.set(hash, map[string]any{"state": "pausedUP", "ratio": 2.0})
	setDownloader(t, w, `seed_goal = '{}'`)
	if n, err := m.SeedCleanup(ctx); err != nil || n != 0 {
		t.Fatalf("off: %d %v", n, err)
	}
	setDownloader(t, w, `seed_goal = '{"ratio":2}'`)
	if n, err := m.SeedCleanup(ctx); err != nil || n != 1 || w.fake.deleted[0] != hash+"|false" {
		t.Fatalf("ratio reached: %d %v %v", n, err, w.fake.deleted)
	}
}
