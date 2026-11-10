package migrate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/metadata"
)

// nameOnlyShows knows one show by its TMDB id only, as TMDB often does for
// a new show Sonarr knows by its TVDB id.
type nameOnlyShows struct{ show metadata.Show }

func (f nameOnlyShows) SearchShows(_ context.Context, q string) ([]metadata.Show, error) {
	other := metadata.Show{Source: "tmdb", Name: "Kaos", Year: 1984, IDs: map[string]string{"tmdb": "314330"}}
	return []metadata.Show{f.show, other}, nil
}

func (f nameOnlyShows) Show(_ context.Context, id string) (metadata.Show, error) {
	if id == "tmdb:102621" {
		return f.show, nil
	}
	return metadata.Show{}, metadata.ErrShowNotFound
}

// A show whose Sonarr id the details source does not know is found by its
// name and year (one year either side), when only one show fits.
func TestMoveFindsAShowByNameWhenItsIDIsUnknown(t *testing.T) {
	l := newMoveLabWith(t, func(radarr, _, _ *Snapshot) {
		radarr.Kind = Sonarr
		radarr.Folders = []Folder{{Path: "/tv"}}
		radarr.Titles = []Title{{SourceID: 5, Kind: "show", Name: "Kaos", Year: 2025, IDs: map[string]string{"tvdb": "461289"}, Path: "/tv/Kaos (2025)", Monitored: true}}
		radarr.QualityProfiles, radarr.Exclusions, radarr.Indexers, radarr.DownloadClients, radarr.Notifications, radarr.Naming = nil, nil, nil, nil, nil, nil
	})
	tv := filepath.Join(filepath.Dir(l.movies), "tv")
	if err := os.MkdirAll(filepath.Join(tv, "Kaos (2025)"), 0o755); err != nil {
		t.Fatal(err)
	}
	l.rules = []PathRule{{From: "/tv", To: filepath.ToSlash(tv)}}
	l.mv.Library.Shows = metadata.ShowSources{"tmdb": nameOnlyShows{show: metadata.Show{Source: "tmdb", Name: "KAOS", Year: 2024, Status: "running", IDs: map[string]string{"tmdb": "102621"}}}}
	mv := l.move(t, Choices{})
	line := lineOf(t, mv.Report, "title:show:tvdb:461289")
	if line.Result != ResAdopted || !strings.Contains(line.Detail, "found as tmdb:102621 by its name and year") {
		t.Fatalf("line %+v", line)
	}
}
