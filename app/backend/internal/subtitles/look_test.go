package subtitles

import (
	"context"
	"testing"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/jobs"
	"github.com/HolyArrs/holyarrs/app/backend/internal/subtitles/container"
)

// Setting default languages lists what each File misses without asking any
// site. The look Job reads never-looked-at Files in batches.
func TestLookJob_ListsMissingWithoutSites(t *testing.T) {
	f := newFix(t)
	ctx := context.Background()
	s := &Service{Store: f.st}
	q := jobs.New(f.st, jobs.Options{Workers: 1, PollInterval: 5 * time.Millisecond})
	s.Register(q)
	if err := q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(q.Stop)
	old := lookBatch
	lookBatch = 1 // one File per Job so the rest spill into the next Job
	t.Cleanup(func() { lookBatch = old })

	wishAll(t, f, Want{Language: "es"})
	if err := s.QueueLook(ctx); err != nil {
		t.Fatal(err)
	}
	var total int
	for range 400 {
		_, total, _ = MissingList(ctx, f.st.Read(), MissingFilter{Language: "es"}, 50, 0)
		var open int
		_ = f.st.Read().QueryRow("SELECT count(*) FROM jobs WHERE kind = ? AND state IN ('queued', 'running')", KindLook).Scan(&open)
		if total >= 2 && open == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if total < 2 {
		t.Fatalf("missing Spanish rows %d", total)
	}
	var n int
	if err := f.st.Read().QueryRow("SELECT count(*) FROM jobs WHERE kind = ? AND state = 'done'", KindLook).Scan(&n); err != nil || n < 2 {
		t.Fatalf("look Jobs done %d %v", n, err)
	}
	if err := f.st.Read().QueryRow("SELECT count(*) FROM jobs WHERE kind = ?", KindSearch).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a look queued %d searches", n)
	}
	var sites int
	_ = f.st.Read().QueryRow("SELECT count(*) FROM subtitle_sites").Scan(&sites)
	if sites != 0 {
		t.Fatal("no site should exist")
	}
}

// Built-in tracks that name no language count only when a language is set for
// them, and picture tracks drop out when asked.
func TestTrackHaves_UnknownLanguage(t *testing.T) {
	tracks := []container.Track{
		{Index: 2, Codec: "tx3g", Language: "und"},
		{Index: 3, Codec: "S_HDMV/PGS", Language: "nor", Picture: true, Name: "Norsk SDH"},
	}
	if hs := trackHaves(tracks, false, ""); len(hs) != 1 || hs[0].Language != "no" || !hs[0].HearingImpaired || !hs[0].Picture {
		t.Fatalf("no fallback: %+v", hs)
	}
	if hs := trackHaves(tracks, false, "en"); len(hs) != 2 || hs[0].Language != "en" {
		t.Fatalf("fallback en: %+v", hs)
	}
	if hs := trackHaves(tracks, true, "en"); len(hs) != 1 || hs[0].Track != 2 {
		t.Fatalf("no pictures: %+v", hs)
	}
}
