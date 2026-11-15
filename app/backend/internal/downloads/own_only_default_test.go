package downloads

import (
	"context"
	"testing"

	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// A new torrent Downloader starts on its own items only, unless the person
// turns that off; a Usenet one has no other app's items to hide.
func TestNewTorrentDownloaderStartsOnItsOwnItems(t *testing.T) {
	ctx := context.Background()
	s, v := openSettingsWorld(t)
	q, err := Add(ctx, s, v, Input{Name: "q", Adapter: "transmission", URL: "http://10.0.0.5:9091"}, domain.System)
	if err != nil || !q.OwnOnly {
		t.Fatalf("torrent Downloader own_only %v (%v), want on", q.OwnOnly, err)
	}
	off := false
	o, err := Add(ctx, s, v, Input{Name: "o", Adapter: "transmission", URL: "http://10.0.0.6:9091", OwnOnly: &off}, domain.System)
	if err != nil || o.OwnOnly {
		t.Fatalf("turned off: own_only %v (%v)", o.OwnOnly, err)
	}
	n, err := Add(ctx, s, v, Input{Name: "n", Adapter: "nzbget", URL: "http://10.0.0.7:6789"}, domain.System)
	if err != nil || n.OwnOnly {
		t.Fatalf("Usenet Downloader own_only %v (%v), want off", n.OwnOnly, err)
	}
	// A change that does not send it keeps it.
	if c, err := Change(ctx, s, v, q.ID, Input{Name: "q2", Adapter: "transmission", URL: q.URL}, domain.System); err != nil || !c.OwnOnly {
		t.Fatalf("change kept own_only %v (%v)", c.OwnOnly, err)
	}
}
