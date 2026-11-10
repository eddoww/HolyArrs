package importer

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// langWords stands in for the subtitles part's language names.
func langWords(w string) string {
	return map[string]string{"english": "en", "eng": "en", "norwegian": "no", "nor": "no", "dutch": "nl"}[w]
}

// placedNames runs a movie plan over dl and returns the extras' new names.
func placedNames(t *testing.T, dl, lib string, rules FileRules) []string {
	t.Helper()
	cands, err := Scan(dl)
	if err != nil {
		t.Fatal(err)
	}
	plan := Plan(cands, target(lib))
	res := Execute(plan, Ops{})
	var out []string
	for _, x := range placeExtras(cands, plan, res, rules, Ops{}, langWords) {
		a, _ := os.Stat(x.From)
		b, err := os.Stat(x.To)
		if err != nil || !os.SameFile(a, b) || x.PlacedBy != "hardlink" {
			t.Fatalf("%s is not a hardlink of %s: %v", x.To, x.From, err)
		}
		out = append(out, filepath.Base(x.To))
	}
	sort.Strings(out)
	return out
}

const placedStem = "Dune - Part Two (2024) 1080p WEB"

// Extras named after the video keep what follows its name; the others take
// the language from their own name.
func TestPlaceExtrasBesideTheVideo(t *testing.T) {
	dl, lib := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dl, relName+".mkv"), 1000)
	write(t, filepath.Join(dl, relName+".en.srt"), 10)
	write(t, filepath.Join(dl, relName+".nl.forced.ass"), 10)
	write(t, filepath.Join(dl, relName+".nfo"), 10)
	write(t, filepath.Join(dl, "Subs", "2_English.srt"), 10)
	write(t, filepath.Join(dl, "Subs", "3_Norwegian.SDH.srt"), 10)
	write(t, filepath.Join(dl, "Subs", "Track 4.srt"), 10)
	write(t, filepath.Join(dl, "Subs", "Dutch.idx"), 10)
	write(t, filepath.Join(dl, "Subs", "Dutch.sub"), 10)
	write(t, filepath.Join(dl, "Sample", "sample.srt"), 10)
	got := placedNames(t, dl, lib, DefaultFileRules())
	want := []string{placedStem + ".en.2.srt", placedStem + ".en.srt", placedStem + ".nl.forced.ass", placedStem + ".nl.idx", placedStem + ".nl.sub",
		placedStem + ".no.hi.srt", placedStem + ".srt"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("placed %q\nwant   %q", got, want)
	}
}

// Off, or a type not in the list, places nothing; a file already at the
// target is never replaced.
func TestPlaceExtrasOffAndNeverReplaces(t *testing.T) {
	dl, lib := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dl, relName+".mkv"), 1000)
	write(t, filepath.Join(dl, relName+".en.srt"), 10)
	write(t, filepath.Join(dl, relName+".en.vtt"), 10)
	off := DefaultFileRules()
	off.ImportExtraFiles = false
	if got := placedNames(t, dl, lib, off); len(got) != 0 {
		t.Fatalf("off placed %q", got)
	}
	lib2 := t.TempDir()
	mine := filepath.Join(lib2, "Dune - Part Two (2024)", placedStem+".en.srt")
	write(t, mine, 3)
	if got := placedNames(t, dl, lib2, DefaultFileRules()); len(got) != 0 {
		t.Fatalf("replaced a file: %q", got)
	}
	if fi, _ := os.Stat(mine); fi.Size() != 3 {
		t.Fatal("the file already there changed")
	}
}

// In a season pack each episode takes the extras beside it or in its own
// folder, never another episode's.
func TestPlaceExtrasPerEpisode(t *testing.T) {
	dl, lib := t.TempDir(), t.TempDir()
	plan, res := []mediafiles.PlanStep{}, []mediafiles.PlaceResult{}
	for _, ep := range []string{"Show.S01E01.1080p.WEB-GRP", "Show.S01E02.1080p.WEB-GRP"} {
		src := filepath.Join(dl, ep+".mkv")
		write(t, src, 1000)
		write(t, filepath.Join(dl, "Subs", ep, "2_English.srt"), 10)
		plan = append(plan, mediafiles.PlanStep{SourceFile: src, TargetPath: filepath.Join(lib, "Show", ep+" placed.mkv"), Method: "hardlink"})
		res = append(res, mediafiles.PlaceResult{SourceFile: src, Outcome: "placed"})
	}
	write(t, filepath.Join(dl, "Subs", "Loose.srt"), 10)
	cands, _ := Scan(dl)
	got := placeExtras(cands, plan, res, DefaultFileRules(), Ops{}, langWords)
	if len(got) != 2 {
		t.Fatalf("placed %+v", got)
	}
	for _, x := range got {
		ep := filepath.Base(filepath.Dir(x.From))
		if filepath.Base(x.To) != ep+" placed.en.srt" {
			t.Fatalf("%s went to %s", x.From, x.To)
		}
	}
}

// A movie import hardlinks its subtitle files beside the video and lists
// them in History.
func TestImportPlacesExtraFiles(t *testing.T) {
	w := setup(t)
	write(t, filepath.Join(w.dl, relName+".en.srt"), 64)
	if err := w.s.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE downloads SET files_seen = json_insert(files_seen, '$[#]', json(?))`, `{"name":"`+relName+`.en.srt","size":64}`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	im := &Importer{Store: w.s, SubtitleLanguage: langWords}
	ctx := context.Background()
	imp, err := im.Import(ctx, w.dlID, false, domain.System)
	if err != nil || imp.State != "done" {
		t.Fatalf("import %+v %v", imp, err)
	}
	sub := filepath.Join(w.lib, "Dune - Part Two (2024)", placedStem+".en.srt")
	a, _ := os.Stat(filepath.Join(w.dl, relName+".en.srt"))
	b, err := os.Stat(sub)
	if err != nil || !os.SameFile(a, b) {
		t.Fatalf("subtitle not hardlinked beside the video: %v", err)
	}
	files, _ := database.Select[mediafiles.File](ctx, w.s.Read(), "files", "")
	if len(files) != 1 {
		t.Fatalf("an extra became a Library file: %+v", files)
	}
	es, _, _ := w.s.Events(ctx, database.EventFilter{Kinds: []string{"import_done"}})
	if len(es) != 1 || es[0].Data["extras"] == nil {
		t.Fatalf("import event %+v", es)
	}
}
