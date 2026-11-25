package migrate

import (
	"slices"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
	"github.com/HolyArrs/holyarrs/app/backend/internal/sources"
)

// Sonarr's episode list keeps rows with a file and rows the user switched
// off, so an off episode with no file (watched, then deleted) is not lost.
func TestKeepEpisodeKeepsOffEpisodes(t *testing.T) {
	cases := []struct {
		row  map[string]any
		kept bool
	}{
		{map[string]any{"episodefileid": 7.0, "monitored": true}, true},
		{map[string]any{"episodefileid": 0.0, "monitored": false}, true},
		{map[string]any{"episodefileid": 0.0, "monitored": true}, false},
		{map[string]any{"episodefileid": 0.0}, false},
	}
	for _, c := range cases {
		if got := keepEpisode(c.row) != nil; got != c.kept {
			t.Errorf("%v kept = %v", c.row, got)
		}
	}
}

// Off episodes are read from either way in and grouped by show.
func TestEpisodesOffByTitle(t *testing.T) {
	rows := []any{
		map[string]any{"seriesid": 1.0, "seasonnumber": 1.0, "episodenumber": 2.0, "monitored": false},
		map[string]any{"seriesid": 1.0, "seasonnumber": 1.0, "episodenumber": 3.0, "monitored": true},
		map[string]any{"seriesid": 2.0, "seasonnumber": 0.0, "episodenumber": 1.0, "monitored": 0.0},
		map[string]any{"seriesid": 2.0, "seasonnumber": 1.0, "episodenumber": 1.0, "monitored": 1.0},
		map[string]any{"seriesid": 2.0, "seasonnumber": 1.0, "episodenumber": 4.0},
	}
	got := episodesOffByTitle(episodesOffRows(rows, 0))
	if !slices.Equal(got[1], []sources.EpisodeNumber{{Season: 1, Episode: 2}}) || !slices.Equal(got[2], []sources.EpisodeNumber{{Season: 0, Episode: 1}}) || len(got) != 2 {
		t.Fatalf("%v", got)
	}
	// The API's per-show list has no show id: the caller's is used.
	api := episodesOffByTitle(episodesOffRows([]any{map[string]any{"seasonnumber": 1.0, "episodenumber": 5.0, "monitored": false}}, 9))
	if !slices.Equal(api[9], []sources.EpisodeNumber{{Season: 1, Episode: 5}}) {
		t.Fatalf("%v", api)
	}
}

// A season kept up to date in Sonarr whose episodes were switched off one
// by one comes over with those episodes off; the others stay wanted.
func TestMoveKeepsEpisodesOff(t *testing.T) {
	sh := cunk(cunkSonarr)
	for e := 1; e <= 3; e++ {
		sh.Episodes = append(sh.Episodes, metadata.Episode{Season: 1, Number: e, Name: "Part"})
	}
	saved := cunkEpisodesOff
	cunkEpisodesOff = []sources.EpisodeNumber{{Season: 1, Episode: 1}, {Season: 1, Episode: 2}}
	t.Cleanup(func() { cunkEpisodesOff = saved })
	l, _ := cunkSetup(t, cunkSonarr, []metadata.Show{sh})
	l.move(t, Choices{})
	if n := l.count(t, `SELECT count(*) FROM items WHERE kind = 'episode' AND keep_up_to_date = 0`); n != 2 {
		t.Fatalf("%d episodes off", n)
	}
	if n := l.count(t, `SELECT count(*) FROM items WHERE kind = 'episode' AND keep_up_to_date = 1 AND json_extract(numbers, '$.episode') = 3`); n != 1 {
		t.Fatal("episode 3 not kept")
	}
	if n := l.count(t, `SELECT count(*) FROM parts WHERE kind = 'season' AND number = 1 AND keep_up_to_date = 1`); n != 1 {
		t.Fatal("season 1 not kept")
	}
}

// A backup's off episodes are read from its Episodes table with the
// fork's recorded episodes (attachFiles), whether or not it has an
// EpisodeFiles table.
func TestEpisodesOffFromBackup(t *testing.T) {
	in := &arrRows{sets: map[string][]map[string]any{"episodes": {
		{"id": int64(1), "seriesid": int64(4), "seasonnumber": int64(1), "episodenumber": int64(1), "episodefileid": int64(0), "monitored": int64(0)},
		{"id": int64(2), "seriesid": int64(4), "seasonnumber": int64(1), "episodenumber": int64(2), "episodefileid": int64(0), "monitored": int64(1)},
	}}}
	s := &Snapshot{Titles: []Title{{SourceID: 4, Kind: "show"}}}
	arrReader{kind: Sonarr}.attachFiles(s, in, nil)
	if got := s.Titles[0].EpisodesOff; !slices.Equal(got, []sources.EpisodeNumber{{Season: 1, Episode: 1}}) {
		t.Fatalf("%v", got)
	}
}
