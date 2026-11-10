package migrate

import (
	"net/http"
	"path/filepath"
	"testing"
)

// Radarr's API gives each movie its recorded file with the quality it
// judged; the read keeps the path, size and every part of the quality.
func TestRadarrRecordedFile(t *testing.T) {
	u, key := radarrAPIWith(t, map[string]any{"/api/v3/movie": []any{
		map[string]any{"id": 1, "tmdbId": 603, "title": "The Matrix", "year": 1999, "path": "/movies/The Matrix (1999)", "monitored": true,
			"movieFile": map[string]any{"id": 4, "movieId": 1, "relativePath": "The Matrix (1999) WEBDL-1080p.mkv",
				"path": "/movies/The Matrix (1999)/The Matrix (1999) WEBDL-1080p.mkv", "size": 1234,
				"quality": map[string]any{"quality": map[string]any{"id": 16, "name": "Remux-2160p", "source": "bluray", "resolution": 2160, "modifier": "remux"}}}},
		map[string]any{"id": 2, "tmdbId": 604, "title": "Unwatched", "year": 2003, "path": "/movies/Unwatched (2003)"},
	}})
	s := read(t, api(Radarr, u, key))
	m := findTitle(t, s, "The Matrix")
	if len(m.Files) != 1 {
		t.Fatalf("files %+v", m.Files)
	}
	f := m.Files[0]
	if f.ID != 4 || f.Path != "/movies/The Matrix (1999)/The Matrix (1999) WEBDL-1080p.mkv" || f.Size != 1234 ||
		f.Quality != "Remux-2160p" || f.Source != "bluray" || f.Resolution != 2160 || f.Modifier != "remux" {
		t.Fatalf("file %+v", f)
	}
	if u := findTitle(t, s, "Unwatched"); len(u.Files) != 0 {
		t.Fatalf("a movie with no file has %+v", u.Files)
	}
	if s.Counts()["files"] != 1 {
		t.Fatalf("counts %+v", s.Counts())
	}
}

// Sonarr's API gives the episode files and episodes per show; the read
// keeps every file and the episodes that are not watched or have a file.
func TestSonarrRecordedEpisodes(t *testing.T) {
	asked := map[string]bool{}
	u, key := sonarrAPIWith(t, false, map[string]any{
		"/api/v3/episodefile": func(r *http.Request) any {
			asked["files "+r.URL.Query().Get("seriesId")] = true
			return []any{map[string]any{"id": 31, "seriesId": 5, "seasonNumber": 1, "relativePath": "Season 01/Breaking Bad - S01E01.mkv",
				"path": "/tv/Breaking Bad/Season 01/Breaking Bad - S01E01.mkv", "size": 99,
				"quality": map[string]any{"quality": map[string]any{"id": 3, "name": "WEBDL-1080p", "source": "web", "resolution": 1080}}}}
		},
		"/api/v3/episode": func(r *http.Request) any {
			asked["episodes "+r.URL.Query().Get("seriesId")] = true
			return []any{
				map[string]any{"seriesId": 5, "seasonNumber": 1, "episodeNumber": 1, "episodeFileId": 31, "hasFile": true, "monitored": true},
				map[string]any{"seriesId": 5, "seasonNumber": 1, "episodeNumber": 2, "episodeFileId": 0, "hasFile": false, "monitored": false},
				map[string]any{"seriesId": 5, "seasonNumber": 1, "episodeNumber": 3, "episodeFileId": 0, "hasFile": false, "monitored": true},
			}
		},
	})
	s := read(t, api(Sonarr, u, key))
	if !asked["files 5"] || !asked["episodes 5"] {
		t.Fatalf("asked %v", asked)
	}
	bb := findTitle(t, s, "Breaking Bad")
	if len(bb.Files) != 1 || bb.Files[0].ID != 31 || bb.Files[0].Quality != "WEBDL-1080p" || bb.Files[0].Source != "web" || bb.Files[0].Resolution != 1080 {
		t.Fatalf("files %+v", bb.Files)
	}
	want := []Episode{{Season: 1, Number: 1, Monitored: true, FileID: 31}, {Season: 1, Number: 2}}
	if len(bb.Episodes) != 2 || bb.Episodes[0] != want[0] || bb.Episodes[1] != want[1] {
		t.Fatalf("episodes %+v", bb.Episodes)
	}
	// A Sonarr that cannot list episode files still reads, with a note.
	u, key = sonarrAPI(t, false)
	if s := read(t, api(Sonarr, u, key)); !hasNote(s, "episode files could not be read") || len(findTitle(t, s, "Breaking Bad").Files) != 0 {
		t.Fatalf("notes %v", s.Notes)
	}
}

// A backup holds the files in MovieFiles, EpisodeFiles and Episodes, with
// the quality as an id named through QualityDefinitions and the path inside
// the title's folder.
func TestRecordedFilesFromBackups(t *testing.T) {
	rp := filepath.Join(t.TempDir(), "radarr.db")
	makeDB(t, rp, append(radarrDB(),
		`CREATE TABLE MovieFiles (Id INTEGER PRIMARY KEY, MovieId INTEGER, RelativePath TEXT, Size INTEGER, Quality TEXT)`,
		`INSERT INTO MovieFiles VALUES (8, 1, 'The Matrix.mkv', 77, '{"quality":3,"revision":{"version":1}}')`)...)
	s := read(t, file(Radarr, rp))
	m := findTitle(t, s, "The Matrix")
	if len(m.Files) != 1 || m.Files[0].Path != m.Path+"/The Matrix.mkv" || m.Files[0].Quality != "WEBDL-1080p" || m.Files[0].Size != 77 {
		t.Fatalf("movie file %+v (path %q)", m.Files, m.Path)
	}
	sp := filepath.Join(t.TempDir(), "sonarr.db")
	makeDB(t, sp, append(sonarrDB(false),
		`CREATE TABLE EpisodeFiles (Id INTEGER PRIMARY KEY, SeriesId INTEGER, SeasonNumber INTEGER, RelativePath TEXT, Size INTEGER, Quality TEXT)`,
		`INSERT INTO EpisodeFiles VALUES (31, 5, 1, 'Season 01\Breaking Bad - S01E01.mkv', 99, '{"quality":3}')`,
		`CREATE TABLE Episodes (Id INTEGER PRIMARY KEY, SeriesId INTEGER, SeasonNumber INTEGER, EpisodeNumber INTEGER, EpisodeFileId INTEGER, Monitored INTEGER)`,
		`INSERT INTO Episodes VALUES (1, 5, 1, 1, 31, 1), (2, 5, 1, 2, 0, 0), (3, 5, 1, 3, 0, 1)`)...)
	s = read(t, file(Sonarr, sp))
	bb := findTitle(t, s, "Breaking Bad")
	if len(bb.Files) != 1 || bb.Files[0].Path != "/tv/Breaking Bad/Season 01/Breaking Bad - S01E01.mkv" || bb.Files[0].Quality != "WEBDL-1080p" {
		t.Fatalf("episode files %+v", bb.Files)
	}
	if len(bb.Episodes) != 2 || bb.Episodes[0].FileID != 31 || bb.Episodes[1].Monitored {
		t.Fatalf("episodes %+v", bb.Episodes)
	}
}
