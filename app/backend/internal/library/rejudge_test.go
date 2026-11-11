package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
)

// nordicRules makes Pick rules like "Nordic Multi-Audio" moved over from
// Radarr: good enough at Bluray-1080p with 10000 points, which come from a
// NORDiC name or from Norwegian audio.
func nordicRules(t *testing.T, s *database.Store) mediafiles.PickRules {
	t.Helper()
	ctx := context.Background()
	t.Cleanup(func() { mediafiles.SetOwnTraits(nil) })
	nordic, err := CreateOwnTrait(ctx, s, OwnTraitInput{Name: "NO: Nordic multi release", Match: "conditions",
		Conditions: []mediafiles.TraitCondition{{Name: "NORDiC", Kind: "release_title", Value: `\b(nordi[ck])\b`, Required: true}}}, domain.System)
	if err != nil {
		t.Fatal(err)
	}
	audio, err := CreateOwnTrait(ctx, s, OwnTraitInput{Name: "NO: Norwegian audio", Match: "conditions",
		Conditions: []mediafiles.TraitCondition{{Name: "Norwegian", Kind: "language", Value: "no", Required: true}}}, domain.System)
	if err != nil {
		t.Fatal(err)
	}
	body := mediafiles.GoodEveryday()
	at := 10000
	body.GoodEnoughAt, body.GoodEnoughPoints = "1080p_bluray", &at
	body.Traits = append(body.Traits, mediafiles.TraitPoints{Trait: mediafiles.OwnTraitKey(nordic.ID), Points: 10000},
		mediafiles.TraitPoints{Trait: mediafiles.OwnTraitKey(audio.ID), Points: 10000})
	pr, err := CreatePickRules(ctx, s, PickRulesInput{Name: "Nordic Multi-Audio", Kinds: []string{"movie"}, Body: body}, domain.System)
	if err != nil {
		t.Fatal(err)
	}
	return pr
}

// TestRejudgeAdoptedCountsInspectedAudio is the live case: movies moved over
// from Radarr under Nordic Multi-Audio were judged by the languages their
// names give, so a file with Norwegian audio but no Norwegian in its name
// stayed "better copy wanted". The pass runs once more on an install that
// ran the first one, counts the audio languages the inspection read, and
// leaves Keep up to date as it is.
func TestRejudgeAdoptedCountsInspectedAudio(t *testing.T) {
	s := open(t)
	lf := addFolder(t, s)
	pr := nordicRules(t, s)
	ctx := context.Background()
	// The first pass already ran on this install.
	if err := s.Write(ctx, func(tx *sql.Tx) error {
		return database.PutSetting(tx, "this_app", "adopted_quality_rejudged", "2026-11-01T00:00:00Z", domain.System, now)
	}); err != nil {
		t.Fatal(err)
	}
	type movie struct {
		name, file string
		audio      []string // what the inspection read
		keep       bool
	}
	nordicAudio := []string{"cs", "da", "en", "fi", "no", "sv"}
	movies := []movie{
		{"Beauty and the Beast", "Beauty and the Beast (1991) Bluray-1080p.mkv", nordicAudio, true},
		{"Bolt", "Bolt (2008) Bluray-1080p.mkv", []string{"da", "en", "sv"}, true},
		{"Paw Patrol The Movie", "Paw Patrol The Movie 2021 NORDiC 1080p BluRay x264-NoTrace.mkv", []string{"en"}, true},
		// Keep up to date turned off by hand stays off.
		{"Brave", "Brave (2012) Bluray-1080p.mkv", nordicAudio, false},
	}
	ids := map[string]int64{}
	for i, m := range movies {
		folder := strings.TrimSuffix(m.file, filepath.Ext(m.file))
		writeFile(t, filepath.Join(lf.Path, folder, m.file), "video")
		keep := m.keep
		id, err := AddMovie(ctx, s, metadata.Movie{Name: m.name, Year: 2000 + i, OriginalLanguage: "en", IDs: map[string]string{"tmdb": strconv.Itoa(7000 + i)}},
			AddOptions{LibraryFolderID: lf.ID, PickRulesID: pr.ID, KeepUpToDate: &keep, Adopt: &Adoption{FolderName: folder, File: folder + "/" + m.file, Size: 5}},
			nil, now, domain.System)
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		ids[m.name] = id
		// The inspection read the audio languages after the adoption.
		langs, _ := json.Marshal(m.audio)
		if err := s.Write(ctx, func(tx *sql.Tx) error {
			_, err := tx.Exec(`UPDATE files SET languages = json(?), media_info = json_set(media_info, '$.audio_languages', json(?),
				'$.inspection_status', 'complete') WHERE version_id IN (SELECT id FROM versions WHERE title_id = ?)`, string(langs), string(langs), id)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	state := func(name string) mediafiles.Slot {
		t.Helper()
		sl := slotsOf(t, s, ids[name])
		if len(sl) != 1 {
			t.Fatalf("%s: slots %+v", name, sl)
		}
		return sl[0]
	}
	// Adopted before the inspection read anything: only the NORDiC name counts.
	for name, want := range map[string]mediafiles.SlotState{"Beauty and the Beast": mediafiles.SlotBetterWanted, "Bolt": mediafiles.SlotBetterWanted,
		"Paw Patrol The Movie": mediafiles.SlotInLibrary, "Brave": mediafiles.SlotInLibrary} {
		if got := state(name).State; got != want {
			t.Fatalf("%s adopted as %s, want %s", name, got, want)
		}
	}

	svc := &Service{Store: s, Now: func() time.Time { return now }}
	if n, err := svc.RejudgeAdopted(ctx); err != nil || n != 2 {
		t.Fatalf("rejudge changed %d slots, %v; want Beauty and the Beast and Brave", n, err)
	}
	for name, want := range map[string]struct {
		state mediafiles.SlotState
		good  bool
	}{
		"Beauty and the Beast": {mediafiles.SlotInLibrary, true},
		"Bolt":                 {mediafiles.SlotBetterWanted, false},
		"Paw Patrol The Movie": {mediafiles.SlotInLibrary, true},
		"Brave":                {mediafiles.SlotInLibrary, true},
	} {
		if sl := state(name); sl.State != want.state || sl.GoodEnough != want.good {
			t.Errorf("%s: %s, good enough %v; want %s, %v", name, sl.State, sl.GoodEnough, want.state, want.good)
		}
	}
	var brave mediafiles.Title
	if err := database.Get(ctx, s.Read(), "titles", ids["Brave"], &brave); err != nil || brave.KeepUpToDate {
		t.Fatalf("Brave keep up to date %v, %v; want it left off", brave.KeepUpToDate, err)
	}
	if n, err := svc.RejudgeAdopted(ctx); err != nil || n != 0 {
		t.Fatalf("second run changed %d slots, %v; it runs once", n, err)
	}
}
