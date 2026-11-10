package migrate

import (
	"slices"
	"strings"
	"testing"
)

// prowlarrCopy is an indexer Prowlarr put in Sonarr or Radarr for one of its
// own: "<site> (Prowlarr)" at Prowlarr's address for that site.
func prowlarrCopy(site string, id int, cats ...any) Provider {
	return Provider{Name: site + " (Prowlarr)", Kind: "Torznab", Enabled: true, Priority: 25, NeedsReentry: []string{"apiKey"},
		Settings: map[string]any{"baseUrl": "http://prowlarr.arr-stack.svc.cluster.local/" + string(rune('0'+id)) + "/", "apiPath": "/api", "categories": cats}}
}

// Sonarr and Radarr keep Prowlarr's address without the /api they add to
// it, so the moved Source gets it too; a base with the path already, or with
// no path, does not get it twice.
func TestMovedTorznabAddressGetsTheAPIPath(t *testing.T) {
	for base, want := range map[string]string{
		"http://prowlarr.arr-stack.svc.cluster.local/4/":           "http://prowlarr.arr-stack.svc.cluster.local/4/api",
		"http://prowlarr:9696/4/api":                               "http://prowlarr:9696/4/api",
		"https://indexer.example":                                  "https://indexer.example",
		"http://jackett:9117/api/v2.0/indexers/x/results/torznab/": "http://jackett:9117/api/v2.0/indexers/x/results/torznab/api",
	} {
		s := &Snapshot{Kind: Sonarr, Indexers: []Provider{{Name: "Feed", Kind: "Torznab", Settings: map[string]any{"baseUrl": base, "apiPath": "/api"}}}}
		if got := item(t, BuildPlan([]Input{{1, s}}, Choices{}), GroupSources, "source:feed").Source.URL; got != want {
			t.Errorf("%s moved as %s, want %s", base, got, want)
		}
	}
	// An app too old to have the setting always added /api; an empty one adds nothing.
	for set, want := range map[string]string{"": "http://prowlarr:9696/4/", "missing": "http://prowlarr:9696/4/api", "/feed": "http://prowlarr:9696/4/feed"} {
		settings := map[string]any{"baseUrl": "http://prowlarr:9696/4/", "apiPath": set}
		if set == "missing" {
			delete(settings, "apiPath")
		}
		s := &Snapshot{Kind: Radarr, Indexers: []Provider{{Name: "Feed", Kind: "Torznab", Settings: settings}}}
		if got := item(t, BuildPlan([]Input{{1, s}}, Choices{}), GroupSources, "source:feed").Source.URL; got != want {
			t.Errorf("apiPath %q moved as %s, want %s", set, got, want)
		}
	}
}

// The same indexer read from Sonarr and Radarr becomes one Source that
// serves shows and movies, with both apps' categories.
func TestSameIndexerFromSonarrAndRadarrMerges(t *testing.T) {
	sonarr := &Snapshot{Kind: Sonarr, Indexers: []Provider{prowlarrCopy("Knaben", 2, int64(5000), int64(5040))}}
	radarr := &Snapshot{Kind: Radarr, Indexers: []Provider{prowlarrCopy("Knaben", 2, int64(2000), int64(2040))}}
	radarr.Indexers[0].Settings["removeYear"] = true
	p := BuildPlan([]Input{{1, sonarr}, {2, radarr}}, Choices{})
	if _, ok := notMoved(p, "radarr indexers[0].removeYear"); !ok {
		t.Fatal("a setting of Radarr's copy with no Source setting is not listed")
	}
	if n := len(p.Group(GroupSources).Items); n != 1 {
		t.Fatalf("%d Sources, want 1: %+v", n, p.Group(GroupSources).Items)
	}
	it := item(t, p, GroupSources, "source:knaben (prowlarr)")
	sp := it.Source
	if !slices.Equal(sp.Kinds, []string{"movie", "tv"}) || !slices.Equal(sp.Categories["tv"], []int{5000, 5040}) || !slices.Equal(sp.Categories["movie"], []int{2000, 2040}) {
		t.Fatalf("merged %+v", sp)
	}
	if sp.URL != "http://prowlarr.arr-stack.svc.cluster.local/2/api" || !strings.Contains(it.Detail, "also serves movie titles, as in radarr") {
		t.Fatalf("merged %s %q", sp.URL, it.Detail)
	}
	if !slices.Contains(p.Mapped, "radarr indexers[0]") {
		t.Fatalf("Radarr's copy is not accounted for: %v", p.Mapped)
	}
	if _, ok := notMoved(p, "radarr indexers[0]"); ok {
		t.Fatal("Radarr's copy was listed as not moved")
	}
	// The same name at another address is still two things; the first wins.
	radarr.Indexers[0].Settings["baseUrl"] = "http://other.example/2/"
	p = BuildPlan([]Input{{1, sonarr}, {2, radarr}}, Choices{})
	if sp := item(t, p, GroupSources, "source:knaben (prowlarr)").Source; !slices.Equal(sp.Kinds, []string{"tv"}) {
		t.Fatalf("another address merged: %+v", sp)
	}
	if _, ok := notMoved(p, "radarr indexers[0]"); !ok {
		t.Fatal("another address must be listed as not moved")
	}
}
