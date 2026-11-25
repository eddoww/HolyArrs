package downloads

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

// TestGrabLinkSendsToYtdlpWithNoRelease: a pasted link goes to the yt-dlp
// Downloader, the Download has no Release, and the Slot is downloading.
func TestGrabLinkSendsToYtdlpWithNoRelease(t *testing.T) {
	w, m := setup(t)
	ctx := context.Background()
	w.exec(t, "UPDATE slots SET state = 'wanted', chosen_release_id = NULL WHERE id = ?", w.slotID)
	if _, _, err := m.GrabLink(ctx, w.slotID, "https://video.example/watch?v=abc", domain.System); !errors.Is(err, ErrNoYtdlp) {
		t.Fatalf("with no yt-dlp runner: %v, want ErrNoYtdlp", err)
	}
	y, _ := newTestYtdlp(t)
	m.Ytdlp = y
	if _, _, err := m.GrabLink(ctx, w.slotID, "https://video.example/watch?v=abc", domain.System); !errors.Is(err, ErrNoYtdlp) {
		t.Fatalf("with no yt-dlp Downloader: %v, want ErrNoYtdlp", err)
	}
	w.exec(t, `INSERT INTO downloaders(created_at, created_by, updated_at, updated_by, name, adapter, protocol, url, category)
		VALUES ('2026-10-12T10:00:00Z', '{}', '2026-10-12T10:00:00Z', '{}', 'yt-dlp', 'ytdlp', 'web', ?, 'holyarrs')`, YtdlpURL)

	if _, _, err := m.GrabLink(ctx, w.slotID, "https://video.example/fail", domain.System); !errors.Is(err, ErrLinkRefused) {
		t.Fatalf("an unsupported link: %v, want ErrLinkRefused", err)
	}
	id, info, err := m.GrabLink(ctx, w.slotID, "https://video.example/watch?v=abc", domain.System)
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "A video" {
		t.Errorf("title %q", info.Title)
	}
	var adapter string
	var rel sql.NullInt64
	var ext string
	if err := w.s.Read().QueryRow(`SELECT dr.adapter, d.release_id, d.external_id FROM downloads d JOIN downloaders dr ON dr.id = d.downloader_id WHERE d.id = ?`, id).Scan(&adapter, &rel, &ext); err != nil {
		t.Fatal(err)
	}
	if adapter != "ytdlp" || rel.Valid {
		t.Fatalf("download went to %q with release %v", adapter, rel)
	}
	if _, err := YtdlpID(ext); err != nil {
		t.Fatalf("external id %q", ext)
	}
	var state string
	var active sql.NullInt64
	if err := w.s.Read().QueryRow("SELECT state, active_download_id FROM slots WHERE id = ?", w.slotID).Scan(&state, &active); err != nil {
		t.Fatal(err)
	}
	if state != string(mediafiles.SlotDownloading) || active.Int64 != id {
		t.Fatalf("slot is %s with download %v, want downloading with %d", state, active, id)
	}
	if _, _, err := m.GrabLink(ctx, w.slotID, "https://video.example/watch?v=other", domain.System); !errors.Is(err, ErrLinkBusy) {
		t.Fatalf("a second link while downloading: %v, want ErrLinkBusy", err)
	}
}
