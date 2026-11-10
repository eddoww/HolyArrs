package subtitles

import (
	"context"
	"os"
	"slices"
	"testing"

	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
	"github.com/HolyArrs/holyarrs/app/backend/internal/subtitles/sites/fakesite"
)

// Missing honours when a File is done: every wished language, any one of
// them, or one named language (every language when the wish lacks it).
func TestMissing_DoneWhen(t *testing.T) {
	wants := []Want{{Language: "en"}, {Language: "nl"}, {Language: "no"}}
	en := []Have{{Language: "en", Place: "beside"}}
	langs := func(ws []Want) []string {
		out := []string{}
		for _, w := range ws {
			out = append(out, w.Language)
		}
		return out
	}
	for _, c := range []struct {
		done  string
		haves []Have
		want  []string
	}{
		{DoneAll, en, []string{"nl", "no"}},
		{DoneAny, en, []string{}},
		{DoneAny, nil, []string{"en", "nl", "no"}},
		{"en", en, []string{}},
		{"nl", en, []string{"nl", "no"}},
		{"de", en, []string{"nl", "no"}},
	} {
		if got := langs(Missing(wants, c.haves, c.done)); !slices.Equal(got, c.want) {
			t.Errorf("done %q: missing %v, want %v", c.done, got, c.want)
		}
	}
}

// done_when is stored, read back and checked; a Norwegian variant counts as
// Norwegian.
func TestSettings_DoneWhen(t *testing.T) {
	f := newFix(t)
	ctx := context.Background()
	if got := ReadSettings(ctx, f.st.Read()).DoneWhen; got != DoneAll {
		t.Fatalf("default %q", got)
	}
	set := ReadSettings(ctx, f.st.Read())
	set.DoneWhen = "nb"
	if err := SaveSettings(ctx, f.st, set, domain.System); err != nil {
		t.Fatal(err)
	}
	if got := ReadSettings(ctx, f.st.Read()).DoneWhen; got != "no" {
		t.Fatalf("read back %q, want no", got)
	}
	set.DoneWhen = "not a language"
	if err := SaveSettings(ctx, f.st, set, domain.System); err == nil {
		t.Fatal("a bad done_when was saved")
	}
}

// A File looked at under one rule is looked at again when the rule changes,
// and its wish rows follow: with any one language done, an English subtitle
// beside the video leaves nothing wanted.
func TestService_DoneWhenChangeLooksAgain(t *testing.T) {
	f := newFix(t)
	s, _ := svc(t, f, &fakesite.Site{})
	ctx := context.Background()
	wishAll(t, f, Want{Language: "en"}, Want{Language: "nl"}, Want{Language: "no"})
	if err := os.WriteFile(f.abs("The Lantern (2010)/The Lantern (2010) 1080p BluRay.en.srt"), []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wanted := func() []string {
		t.Helper()
		st, err := s.Look(ctx, f.movieFile, domain.System)
		if err != nil {
			t.Fatal(err)
		}
		var rows []string
		r, err := f.st.Read().Query("SELECT language FROM subtitles WHERE file_id = ? AND place = 'wish' ORDER BY language", f.movieFile)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		for r.Next() {
			var l string
			_ = r.Scan(&l)
			rows = append(rows, l)
		}
		if len(rows) != len(st.Missing) {
			t.Fatalf("wish rows %v, missing %+v", rows, st.Missing)
		}
		return rows
	}
	if got := wanted(); !slices.Equal(got, []string{"nl", "no"}) {
		t.Fatalf("every language: wanted %v", got)
	}
	stale := func() bool {
		t.Helper()
		ids, err := s.staleFiles(ctx, 100, nil, map[int64]bool{})
		if err != nil {
			t.Fatal(err)
		}
		return slices.Contains(ids, f.movieFile)
	}
	if stale() {
		t.Fatal("stale right after a look")
	}
	set := ReadSettings(ctx, f.st.Read())
	set.DoneWhen = DoneAny
	if err := SaveSettings(ctx, f.st, set, domain.System); err != nil {
		t.Fatal(err)
	}
	if !stale() {
		t.Fatal("a new done rule did not make the File stale")
	}
	if got := wanted(); len(got) != 0 {
		t.Fatalf("any one language: wanted %v", got)
	}
	set.DoneWhen = "nl"
	if err := SaveSettings(ctx, f.st, set, domain.System); err != nil {
		t.Fatal(err)
	}
	if got := wanted(); !slices.Equal(got, []string{"nl", "no"}) {
		t.Fatalf("done at Dutch: wanted %v", got)
	}
}
