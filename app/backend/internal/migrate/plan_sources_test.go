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
// it, so the imported Source gets it too; a base with the path already does
// not get it twice.
func TestMovedTorznabAddressGetsTheAPIPath(t *testing.T) {
	for base, want := range map[string]string{
		"http://prowlarr.arr-stack.svc.cluster.local/4/":           "http://prowlarr.arr-stack.svc.cluster.local/4/api",
		"http://prowlarr:9696/4/api":                               "http://prowlarr:9696/4/api",
		"https://indexer.example":                                  "https://indexer.example/api",
		"http://jackett:9117/api/v2.0/indexers/x/results/torznab/": "http://jackett:9117/api/v2.0/indexers/x/results/torznab/api",
	} {
		s := &Snapshot{Kind: Sonarr, Indexers: []Provider{{Name: "Feed", Kind: "Torznab", Settings: map[string]any{"baseUrl": base, "apiPath": "/api"}}}}
		if got := item(t, BuildPlan([]Input{{1, s}}, Choices{}), GroupSources, "source:feed").Source.URL; got != want {
			t.Errorf("%s moved as %s, want %s", base, got, want)
		}
	}
	// An app too old to have the setting, or an empty one, means /api.
	for set, want := range map[string]string{"": "http://prowlarr:9696/4/api", "missing": "http://prowlarr:9696/4/api", "/feed": "http://prowlarr:9696/4/feed"} {
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
	if sp.URL != "http://prowlarr.arr-stack.svc.cluster.local/2/api" || !strings.Contains(it.Detail, "through Prowlarr") || !strings.Contains(it.Detail, "also serves movie titles, as in radarr") {
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

// Prowlarr's own IPTorrents, Knaben, SubsPlease and TorrentsCSV become site
// Sources, and the copies Prowlarr put in Sonarr and Radarr are not moved
// again. A copy of a Prowlarr indexer the move cannot bring over stays a
// Torznab Source through Prowlarr.
func TestProwlarrSitesMoveAsSiteSources(t *testing.T) {
	prowlarr := &Snapshot{Kind: Prowlarr, Indexers: []Provider{
		{ID: 4, Name: "IPTorrents", Kind: "IPTorrents", Enabled: true, Priority: 25, NeedsReentry: []string{"cookie"},
			Settings: map[string]any{"baseUrl": "https://iptorrents.com/", "userAgent": "Mozilla/5.0 Firefox", "freeLeechOnly": true, "searchSortBy": int64(0),
				"torrentBaseSettings.appMinimumSeeders": int64(1)}},
		{ID: 2, Name: "Knaben", Kind: "Knaben", Enabled: true, Priority: 25, Settings: map[string]any{"baseUrl": "https://knaben.org/"}},
		{ID: 3, Name: "SubsPlease", Kind: "SubsPlease", Enabled: true, Priority: 20, Settings: map[string]any{"baseUrl": "https://subsplease.org/"}},
		{ID: 1, Name: "TorrentsCSV", Kind: "TorrentsCSV", Enabled: false, Priority: 25, Settings: map[string]any{"baseUrl": "https://torrents-csv.com/"}},
		{ID: 9, Name: "1337x", Kind: "Cardigann", Enabled: true, Settings: map[string]any{"baseUrl": "https://1337x.to/"}},
	}}
	sonarr := &Snapshot{Kind: Sonarr, Indexers: []Provider{prowlarrCopy("IPTorrents", 4, int64(5000)), prowlarrCopy("Knaben", 2, int64(5000)),
		prowlarrCopy("SubsPlease", 3, int64(5000)), prowlarrCopy("1337x", 9, int64(5000))}}
	radarr := &Snapshot{Kind: Radarr, Indexers: []Provider{prowlarrCopy("IPTorrents", 4, int64(2000)), prowlarrCopy("1337x", 9, int64(2000))}}
	p := BuildPlan([]Input{{1, sonarr}, {2, radarr}, {3, prowlarr}}, Choices{})

	ipt := item(t, p, GroupSources, "source:iptorrents")
	if s := ipt.Source; s.Format != "iptorrents" || s.URL != "https://iptorrents.com/" || s.UserAgent != "Mozilla/5.0 Firefox" || !s.FreeleechOnly ||
		!s.NeedsKey || *s.MinSeeders != 1 || !slices.Equal(s.Kinds, []string{"movie", "tv", "music"}) || ipt.Needs != "your cookie" {
		t.Fatalf("IPTorrents %+v %q", s, ipt.Needs)
	}
	if s := item(t, p, GroupSources, "source:knaben").Source; s.Format != "knaben" || s.URL != "https://api.knaben.org/" || s.NeedsKey || s.UserAgent != "" {
		t.Fatalf("Knaben %+v", s)
	}
	if s := item(t, p, GroupSources, "source:subsplease").Source; s.Format != "subsplease" || s.Priority != 20 || !slices.Equal(s.Kinds, []string{"movie", "tv"}) {
		t.Fatalf("SubsPlease %+v", s)
	}
	if s := item(t, p, GroupSources, "source:torrentscsv").Source; s.Format != "torrentscsv" || s.On {
		t.Fatalf("TorrentsCSV %+v", s)
	}
	for _, from := range []string{"sonarr indexers[0]", "sonarr indexers[1]", "sonarr indexers[2]", "radarr indexers[0]"} {
		if n, ok := notMoved(p, from); !ok || !strings.Contains(n.Why, "reached through Prowlarr") {
			t.Errorf("%s: %+v", from, n)
		}
	}
	x := item(t, p, GroupSources, "source:1337x (prowlarr)")
	if x.Source.Format != "torznab" || !slices.Equal(x.Source.Kinds, []string{"movie", "tv"}) || x.Source.URL != "http://prowlarr.arr-stack.svc.cluster.local/9/api" {
		t.Fatalf("a Prowlarr copy with no site Source %+v", x.Source)
	}
	if n, ok := notMoved(p, "prowlarr indexers[4]"); !ok || !strings.Contains(n.Why, "Sources bridge") {
		t.Fatalf("Cardigann %+v", n)
	}
	if n, ok := notMoved(p, "prowlarr indexers[0].searchSortBy"); !ok {
		t.Fatalf("an unmatched site setting %+v", n)
	}
	for _, used := range []string{"baseUrl", "userAgent", "freeLeechOnly", "torrentBaseSettings.appMinimumSeeders"} {
		if _, ok := notMoved(p, "prowlarr indexers[0]."+used); ok {
			t.Errorf("%s was moved but listed as not moved", used)
		}
	}
}

// A moved site Source is made paused, with its user agent and freeleech
// choice and without the cookie, which the owner pastes again.
func TestProwlarrSiteSourceIsMadePaused(t *testing.T) {
	l := newMoveLabWith(t, func(radarr, bazarr, _ *Snapshot) {
		radarr.Indexers = []Provider{prowlarrCopy("IPTorrents", 4, int64(2000))}
		*bazarr = Snapshot{Kind: Prowlarr, Indexers: []Provider{
			{ID: 4, Name: "IPTorrents", Kind: "IPTorrents", Enabled: true, Priority: 25, NeedsReentry: []string{"cookie"},
				Settings: map[string]any{"baseUrl": "https://iptorrents.com/", "userAgent": "Mozilla/5.0 Firefox", "freeLeechOnly": true}},
			{ID: 2, Name: "Knaben", Kind: "Knaben", Enabled: true, Priority: 25, Settings: map[string]any{"baseUrl": "https://knaben.org/"}},
		}}
	})
	mv := l.move(t, Choices{})
	if n := l.count(t, `SELECT count(*) FROM sources WHERE name = 'IPTorrents' AND format = 'iptorrents' AND state = 'disabled' AND secret_ref IS NULL
		AND user_agent = 'Mozilla/5.0 Firefox' AND freeleech_only = 1 AND url = 'https://iptorrents.com/' AND protocol = 'torrent'`); n != 1 {
		t.Fatalf("IPTorrents not made as a paused site Source: %+v", lineOf(t, mv.Report, "source:iptorrents"))
	}
	if n := l.count(t, `SELECT count(*) FROM sources WHERE name = 'Knaben' AND format = 'knaben' AND url = 'https://api.knaben.org/'
		AND json_extract(use_for, '$.feed') = 1 AND kinds = '["movie","tv","music"]'`); n != 1 {
		t.Fatalf("Knaben not made: %+v", lineOf(t, mv.Report, "source:knaben"))
	}
	if n := l.count(t, "SELECT count(*) FROM sources WHERE name LIKE '%(Prowlarr)%'"); n != 0 {
		t.Fatal("Radarr's copy of a Prowlarr site was moved too")
	}
}
