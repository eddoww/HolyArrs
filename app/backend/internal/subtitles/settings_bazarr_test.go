package subtitles

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/jobs"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
	"github.com/HolyArrs/holyarrs/app/backend/internal/subtitles/sites/fakesite"
)

// Adaptive searching waits the set number of days between tries once the
// first try is older than the set delay.
func TestService_AdaptiveDelayAndDelta(t *testing.T) {
	f := newFix(t)
	s, _ := svc(t, f, &fakesite.Site{})
	ctx := context.Background()
	wishAll(t, f, Want{Language: "en"})
	set := ReadSettings(ctx, f.st.Read())
	set.AdaptiveAfterDays, set.AdaptiveEveryDays = 2, 3
	if err := SaveSettings(ctx, f.st, set, domain.System); err != nil {
		t.Fatal(err)
	}
	nextTry := func() time.Duration {
		t.Helper()
		var next string
		if err := f.st.Read().QueryRow("SELECT next_try_at FROM subtitles WHERE place = 'wish' AND language = 'en'").Scan(&next); err != nil {
			t.Fatal(err)
		}
		at, _ := time.Parse(database.TimeLayout, next)
		return time.Until(at)
	}
	if _, err := s.Search(ctx, f.movieFile, SearchOptions{}, domain.System); err != nil {
		t.Fatal(err)
	}
	if d := nextTry(); d > 7*time.Hour { // still the usual 6 hours
		t.Fatalf("first next try in %v", d)
	}
	old := database.FormatTime(time.Now().UTC().AddDate(0, 0, -5))
	if err := f.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE subtitles SET first_tried_at = ? WHERE place = 'wish'", old)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Search(ctx, f.movieFile, SearchOptions{}, domain.System); err != nil {
		t.Fatal(err)
	}
	if d := nextTry(); d < 71*time.Hour || d > 73*time.Hour {
		t.Fatalf("after the delay the next try is in %v, want 3 days", d)
	}
}

// A subtitle chosen by hand is searched again for a better one only when
// better_by_hand is on.
func TestService_BetterByHand(t *testing.T) {
	f := newFix(t)
	site := &fakesite.Site{Subs: []fakesite.Sub{
		{ID: "ok", Kind: "movie", Language: "en", Title: "The Lantern", Year: 2010, Release: "The.Lantern.2010.1080p.BluRay.x264-OTHER", Format: "srt", Text: fakesite.Cue("first", 1000)},
	}}
	s, q := svc(t, f, site)
	ctx := context.Background()
	wishAll(t, f, Want{Language: "en"})
	if _, err := s.Choose(ctx, f.movieFile, "Lab subtitle site", "ok", domain.System); err != nil {
		t.Fatal(err)
	}
	site.Subs = append(site.Subs, fakesite.Sub{ID: "best", Kind: "movie", Language: "en", Title: "The Lantern", Year: 2010,
		Release: "The.Lantern.2010.1080p.BluRay.x264-GRP", Format: "srt", Text: fakesite.Cue("second", 1000)})
	if err := s.handleBetter(ctx, &jobs.Run{}); err != nil {
		t.Fatal(err)
	}
	if len(*q) != 0 {
		t.Fatalf("a hand choice was queued with better_by_hand off: %+v", *q)
	}
	if res, _ := s.Search(ctx, f.movieFile, SearchOptions{Better: true, BetterDays: 7}, domain.System); len(res.Outcomes) != 0 {
		t.Fatalf("hand choice searched again: %+v", res.Outcomes)
	}
	set := ReadSettings(ctx, f.st.Read())
	set.BetterByHand = true
	if err := SaveSettings(ctx, f.st, set, domain.System); err != nil {
		t.Fatal(err)
	}
	if err := s.handleBetter(ctx, &jobs.Run{}); err != nil {
		t.Fatal(err)
	}
	if len(*q) != 1 || (*q)[0].params["by_hand"] != true {
		t.Fatalf("queued %+v", *q)
	}
	res, _ := s.Search(ctx, f.movieFile, SearchOptions{Better: true, BetterDays: 7, ByHand: true}, domain.System)
	if len(res.Outcomes) != 1 || res.Outcomes[0].Result != "upgraded" {
		t.Fatalf("%+v", res.Outcomes)
	}
}

func TestSettings_BazarrFields(t *testing.T) {
	set := DefaultSettings()
	if set.AdaptiveAfterDays != 21 || set.AdaptiveEveryDays != 7 || set.BetterByHand || set.BuiltInUnknown != "" {
		t.Fatalf("defaults %+v", set)
	}
	set.BuiltInUnknown = "eng"
	if err := set.Validate(); err != nil || set.BuiltInUnknown != "en" {
		t.Fatalf("eng: %v %q", err, set.BuiltInUnknown)
	}
	set.BuiltInUnknown = "klingon"
	if err := set.Validate(); err == nil {
		t.Fatal("unknown language taken")
	}
	set.BuiltInUnknown, set.AdaptiveEveryDays = "", 0
	if err := set.Validate(); err == nil {
		t.Fatal("zero days taken")
	}
}
