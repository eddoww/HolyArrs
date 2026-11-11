package migrate

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

func TestQualityWords(t *testing.T) {
	for _, c := range []struct {
		f    TitleFile
		want string
	}{
		// The API's parts.
		{TitleFile{Quality: "WEBDL-2160p", Source: "web", Resolution: 2160}, "2160p WEB-DL"},
		{TitleFile{Quality: "Remux-2160p", Source: "bluray", Resolution: 2160, Modifier: "remux"}, "2160p BluRay REMUX"},
		{TitleFile{Quality: "Bluray-1080p Remux", Source: "blurayRaw", Resolution: 1080}, "1080p BluRay REMUX"},
		{TitleFile{Quality: "HDTV-2160p", Source: "tv", Resolution: 2160}, "2160p HDTV"},
		{TitleFile{Quality: "SDTV", Source: "television", Resolution: 480}, "480p HDTV"},
		{TitleFile{Quality: "WEBRip-720p", Source: "webRip", Resolution: 720}, "720p WEBRip"},
		{TitleFile{Quality: "BR-DISK", Source: "bluray", Resolution: 1080, Modifier: "brdisk"}, "1080p BDMV"},
		{TitleFile{Quality: "Unknown", Source: "unknown"}, ""},
		// A backup's name only.
		{TitleFile{Quality: "WEBDL-1080p"}, "1080p WEB-DL"},
		{TitleFile{Quality: "Bluray-720p"}, "720p BluRay"},
		{TitleFile{Quality: "Remux-2160p"}, "2160p BluRay REMUX"},
		{TitleFile{Quality: "SDTV"}, "480p HDTV"},
		{TitleFile{Quality: "DVD"}, "480p DVD"},
	} {
		if got := qualityWords(c.f); got != c.want {
			t.Errorf("%+v: %q, want %q", c.f, got, c.want)
		}
	}
}

// A planned title carries the files the old app recorded, inside its own
// folder, with their quality, release names, episodes and the episodes it
// did not watch;
// one in a 4K Library folder is planned as its 4K Version.
func TestPlanCarriesRecordedFiles(t *testing.T) {
	sonarr := &Snapshot{Kind: Sonarr, Folders: []Folder{{Path: "/media/TV"}, {Path: "/media/TV-4K"}},
		QualityProfiles: []Profile{{ID: 5, Name: "Ultra-HD", Cutoff: "WEBDL-2160p"}},
		Titles: []Title{{SourceID: 1, Kind: "show", Name: "Agatha", Year: 2024, IDs: map[string]string{"tvdb": "1"}, Path: "/media/TV-4K/Agatha (2024)",
			Monitored: true, ProfileID: 5, Profile: "Ultra-HD",
			Seasons: []Season{{Number: 0, Monitored: false}, {Number: 1, Monitored: true}},
			Files: []TitleFile{
				{ID: 7, Path: "/media/TV-4K/Agatha (2024)/Season 01/Agatha - S01E01.mkv", Quality: "WEBDL-2160p", Source: "web", Resolution: 2160,
					ReleaseName: "Agatha.All.Along.S01E01.2160p.DSNP.WEB-DL.DDP5.1.H.265-GRP"},
				{ID: 8, Path: "/elsewhere/stray.mkv"},
			},
			Episodes: []Episode{{Season: 1, Number: 1, Monitored: true, FileID: 7}, {Season: 1, Number: 2}, {Season: 0, Number: 1}}}}}
	p := BuildPlan([]Input{{ID: 1, Snap: sonarr}}, Choices{})
	g := p.Group(GroupTitles)
	if len(g.Items) != 1 {
		t.Fatalf("titles %+v / %+v", g.Items, p.NotMoved)
	}
	tp := g.Items[0].Title
	want := []FilePlan{{Path: "Agatha (2024)/Season 01/Agatha - S01E01.mkv", Quality: "2160p WEB-DL", Episodes: [][2]int{{1, 1}},
		ReleaseName: "Agatha.All.Along.S01E01.2160p.DSNP.WEB-DL.DDP5.1.H.265-GRP"}}
	if !reflect.DeepEqual(tp.Files, want) {
		t.Fatalf("files %+v", tp.Files)
	}
	// Season 0 is off as a whole; only S01E02 is an episode left off.
	if !reflect.DeepEqual(tp.EpisodesOff, [][2]int{{1, 2}}) {
		t.Fatalf("episodes off %+v", tp.EpisodesOff)
	}
	if tp.Profile != "Ultra-HD" || tp.Preset != "4K" || tp.VersionLabel != "4K" {
		t.Fatalf("rules %q / %q, version %q", tp.Profile, tp.Preset, tp.VersionLabel)
	}
	stray := false
	for _, n := range p.NotMoved {
		stray = stray || n.From == "sonarr titles[0].files"
	}
	if !stray {
		t.Fatalf("the stray file is not listed: %+v", p.NotMoved)
	}
}

// Moving a Radarr title keeps the file Radarr recorded with Radarr's
// quality and the release it came from (custom formats test that name in
// Radarr), uses the Pick rules named after its profile when they are here,
// and names the main Version 4K in a 4K Library folder.
func TestMoveKeepsRecordedFileQualityAndRules(t *testing.T) {
	var movies4k string
	l := newMoveLabWith(t, func(radarr, _, _ *Snapshot) {
		radarr.Folders = []Folder{{Path: "/movies"}, {Path: "/movies-4k"}}
		radarr.Titles[0].Path = "/movies-4k/The Matrix (1999)"
		radarr.Titles[0].Files = []TitleFile{{ID: 3, Path: "/movies-4k/The Matrix (1999)/The.Matrix.1999.1080p.mkv", Quality: "Remux-2160p", Source: "bluray", Resolution: 2160, Modifier: "remux",
			ReleaseName: "The.Matrix.1999.2160p.UHD.BluRay.REMUX.HDR.HEVC.Atmos-NORDiC"}}
	})
	movies4k = filepath.Join(filepath.Dir(l.movies), "films-4k")
	if err := os.MkdirAll(filepath.Join(movies4k, "The Matrix (1999)"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(l.file, filepath.Join(movies4k, "The Matrix (1999)", "The.Matrix.1999.1080p.mkv")); err != nil {
		t.Fatal(err)
	}
	l.rules = []PathRule{{From: "/movies", To: filepath.ToSlash(l.movies)}, {From: "/movies-4k", To: filepath.ToSlash(movies4k)}}
	// Pick rules named after the Radarr profile, as the custom format move makes them.
	ctx := context.Background()
	var ultra int64
	err := l.st.Write(ctx, func(tx *sql.Tx) error {
		var body string
		if err := tx.QueryRow("SELECT body FROM pick_rules WHERE name = '4K'").Scan(&body); err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO pick_rules (created_at, created_by, updated_at, updated_by, name, kinds, revision, preset, body) VALUES ('2026-01-01T00:00:00Z', '{}', '2026-01-01T00:00:00Z', '{}', 'Ultra-HD', '["movie"]', 1, 0, ?)`, body)
		if err != nil {
			return err
		}
		ultra, err = res.LastInsertId()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	mv := l.move(t, Choices{})
	if mv.State != "done" {
		t.Fatalf("move %+v", mv.Report)
	}
	line := lineOf(t, mv.Report, "title:movie:tmdb:603")
	if line.Result != ResAdopted {
		t.Fatalf("title line %+v", line)
	}
	files, _ := database.Select[mediafiles.File](ctx, l.st.Read(), "files", "1=1")
	if len(files) != 1 || files[0].Quality.Resolution != 2160 || files[0].Quality.Source != "remux" {
		t.Fatalf("files %+v", files)
	}
	if r := files[0].ReleaseName; r == nil || *r != "The.Matrix.1999.2160p.UHD.BluRay.REMUX.HDR.HEVC.Atmos-NORDiC" {
		t.Fatalf("release name %v", r)
	}
	vs, _ := database.Select[mediafiles.Version](ctx, l.st.Read(), "versions", "1=1")
	if len(vs) != 1 || vs[0].PickRulesID != ultra || vs[0].Label != mediafiles.FourKName {
		t.Fatalf("version %+v (Ultra-HD is %d)", vs, ultra)
	}
}

// A moved title is adopted under the Pick rules made from its old profile,
// so its file is judged good enough (or not) under them: a Bluray-1080p
// file under Ultra-HD (good enough at 2160p) looks for a better copy, as
// Radarr's cutoff unmet did, though the folder's default rules (Standard)
// would count it good enough.
func TestMoveJudgesAdoptedFileUnderItsOwnRules(t *testing.T) {
	l := newMoveLabWith(t, func(radarr, _, _ *Snapshot) {
		radarr.Titles[0].Monitored = true
		radarr.Titles[0].Files = []TitleFile{{ID: 3, Path: "/movies/The Matrix (1999)/The.Matrix.1999.1080p.mkv", Quality: "Bluray-1080p", Source: "bluray", Resolution: 1080}}
	})
	ctx := context.Background()
	var ultra int64
	err := l.st.Write(ctx, func(tx *sql.Tx) error {
		var body string
		if err := tx.QueryRow("SELECT body FROM pick_rules WHERE name = '4K'").Scan(&body); err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO pick_rules (created_at, created_by, updated_at, updated_by, name, kinds, revision, preset, body) VALUES ('2026-01-01T00:00:00Z', '{}', '2026-01-01T00:00:00Z', '{}', 'Ultra-HD', '["movie"]', 1, 0, ?)`, body)
		if err != nil {
			return err
		}
		ultra, err = res.LastInsertId()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if mv := l.move(t, Choices{}); mv.State != "done" {
		t.Fatalf("move %+v", mv.Report)
	}
	vs, _ := database.Select[mediafiles.Version](ctx, l.st.Read(), "versions", "1=1")
	slots, _ := database.Select[mediafiles.Slot](ctx, l.st.Read(), "slots", "1=1")
	if len(vs) != 1 || vs[0].PickRulesID != ultra || len(slots) != 1 {
		t.Fatalf("versions %+v, slots %+v (Ultra-HD is %d)", vs, slots, ultra)
	}
	if sl := slots[0]; sl.State != mediafiles.SlotBetterWanted || sl.GoodEnough || sl.LastReason == nil || !strings.Contains(*sl.LastReason, "below good enough") {
		t.Fatalf("slot %s, good enough %v, reason %v; want better_wanted under Ultra-HD", sl.State, sl.GoodEnough, *sl.LastReason)
	}
}

// A title the old app had no file for, whose folder is not on disk, comes
// over as missing in its Library folder with its old folder name; one the
// old app had files for is not found (a path rule is missing).
func TestMoveTitleWithNoFolderYet(t *testing.T) {
	l := newMoveLabWith(t, func(radarr, _, _ *Snapshot) {
		radarr.Titles[0].Path = "/movies/Toy Story (1995)"
		radarr.Titles[0].IDs = map[string]string{"tmdb": "603"}
	})
	p := l.plan(t)
	if it := p.Group(GroupTitles).Items[0]; it.Title.Folder != "Toy Story (1995)" {
		t.Fatalf("title %+v", it.Title)
	}
	mv := l.move(t, Choices{})
	line := lineOf(t, mv.Report, "title:movie:tmdb:603")
	if line.Result != ResCreated || !strings.Contains(line.Detail, "added as missing") {
		t.Fatalf("line %+v", line)
	}
	ctx := context.Background()
	vs, _ := database.Select[mediafiles.Version](ctx, l.st.Read(), "versions", "1=1")
	if len(vs) != 1 || vs[0].FolderName != "Toy Story (1995)" {
		t.Fatalf("versions %+v", vs)
	}
	if _, err := os.Stat(filepath.Join(l.movies, "Toy Story (1995)")); !os.IsNotExist(err) {
		t.Fatalf("the move made the folder: %v", err)
	}

	l2 := newMoveLabWith(t, func(radarr, _, _ *Snapshot) {
		radarr.Titles[0].Path = "/movies/Gone (2001)"
		radarr.Titles[0].Files = []TitleFile{{ID: 1, Path: "/movies/Gone (2001)/gone.mkv", Quality: "WEBDL-1080p"}}
	})
	mv = l2.move(t, Choices{})
	if line := lineOf(t, mv.Report, "title:movie:tmdb:603"); line.Result != ResNotFound {
		t.Fatalf("recorded files with no folder: %+v", line)
	}
}
