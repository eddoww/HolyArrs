package subtitles

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
	"github.com/HolyArrs/holyarrs/app/backend/internal/subtitles/sites/fakesite"
)

// cues makes SRT text of the lines.
func cues(lines ...string) string {
	var b strings.Builder
	for i, l := range lines {
		b.WriteString(itoa(int64(i+1)) + "\n00:00:01,000 --> 00:00:02,000\n" + l + "\n\n")
	}
	return b.String()
}

// A forced subtitle never meets a want for a full one, as in Bazarr, where
// a profile language with forced "False" is not met by a .forced file.
func TestMissing_ForcedOnlyIsNotFull(t *testing.T) {
	wants := []Want{{Language: "en"}, {Language: "nl"}, {Language: "no"}}
	forced := []Have{{Language: "en", Forced: true, Place: "beside"}}
	if got := Missing(wants, forced, DoneAny); len(got) != 3 {
		t.Fatalf("a forced English subtitle met %d of 3 wants", 3-len(got))
	}
	if got := Missing([]Want{{Language: "en", Forced: true}}, forced, DoneAll); len(got) != 0 {
		t.Fatalf("a forced want is met by a forced subtitle: %+v", got)
	}
}

// A subtitle file beside the video that names no language counts, as in
// Bazarr, by the language its text clearly shows: the case of The First
// Omen, whose "<video>.srt" holds YTS lines and the forced English lines,
// next to "<video>.en.forced.srt". A file whose language is not clear, or
// in a language not wanted, counts as nothing, and no file is changed.
func TestLook_LanguageGuessedBeside(t *testing.T) {
	f := newFix(t)
	s, _ := svc(t, f, &fakesite.Site{})
	ctx := context.Background()
	wishAll(t, f, Want{Language: "en"}, Want{Language: "nl"}, Want{Language: "no"})
	set := ReadSettings(ctx, f.st.Read())
	set.DoneWhen = DoneAny
	if err := SaveSettings(ctx, f.st, set, domain.System); err != nil {
		t.Fatal(err)
	}
	const base = "The Lantern (2010)/The Lantern (2010) 1080p BluRay"
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(f.abs(base+name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	look := func() Status {
		t.Helper()
		st, err := s.Look(ctx, f.movieFile, domain.System)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	forcedLines := []string{"I am a butterfly...", "...And I must fly!", "A butterfly? Well then,\nI must not keep you from flying!", "It's a boy.", "There are two.", "A girl..."}
	write(".en.forced.srt", cues(forcedLines...))
	if st := look(); len(st.Missing) != 3 {
		t.Fatalf("only a forced subtitle: missing %+v", st.Missing)
	}

	write(".srt", cues("Hvad vil du mig?", "Jeg ved ikke noget om det nu.", "Måske kan nogen hjælpe dig efter skolen.", "Nej, det blev for meget.", "Hvad laver du? Det er noget helt andet, måske."))
	if st := look(); len(st.Missing) != 3 {
		t.Fatalf("a Danish file is not wanted: missing %+v, haves %+v", st.Missing, st.Haves)
	}
	if err := os.Remove(f.abs(base + ".srt")); err != nil {
		t.Fatal(err)
	}

	yts := cues(append([]string{"Downloaded from\nYTS.MX", "Official YIFY movies site:\nYTS.MX"}, forcedLines...)...)
	write(".srt", yts)
	st := look()
	i := slices.IndexFunc(st.Haves, func(h Have) bool { return h.LanguageGuessed })
	if len(st.Missing) != 0 || i < 0 || st.Haves[i].Language != "en" || st.Haves[i].Forced {
		t.Fatalf("a language-less English file: missing %+v, haves %+v", st.Missing, st.Haves)
	}
	var lang string
	var guessed bool
	if err := f.st.Read().QueryRow("SELECT language, language_guessed FROM subtitles WHERE file_id = ? AND place = 'beside' AND state = 'present' AND relative_path LIKE '%BluRay.srt'",
		f.movieFile).Scan(&lang, &guessed); err != nil || lang != "en" || !guessed {
		t.Fatalf("recorded %q guessed %v (%v)", lang, guessed, err)
	}
	if b, _ := os.ReadFile(f.abs(base + ".srt")); string(b) != yts {
		t.Fatal("the file was changed")
	}

	if h, ok := guessedBeside(f.abs("The Lantern (2010)"), "The Lantern (2010) 1080p BluRay", "The Lantern (2010) 1080p BluRay.forced.srt"); ok {
		t.Fatalf("a missing file: %+v", h)
	}
	write(".forced.srt", yts)
	if h, ok := guessedBeside(f.abs("The Lantern (2010)"), "The Lantern (2010) 1080p BluRay", "The Lantern (2010) 1080p BluRay.forced.srt"); !ok || !h.Forced || h.Language != "en" {
		t.Fatalf("a language-less forced file: %+v %v", h, ok)
	}
	write(".srt", "1\n00:00:01,000 --> 00:00:02,000\n"+strings.Repeat("You know what I want. ", 60000)+"\n")
	if h, ok := guessedBeside(f.abs("The Lantern (2010)"), "The Lantern (2010) 1080p BluRay", "The Lantern (2010) 1080p BluRay.srt"); ok {
		t.Fatalf("a file of 1 MB or more is not read: %+v", h)
	}
}
