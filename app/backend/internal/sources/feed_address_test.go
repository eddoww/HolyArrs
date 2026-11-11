package sources

import "testing"

// An address with no path calls /api, and so does Prowlarr's address for one
// of its indexers, which Sonarr and Radarr keep without the /api they add to
// it; anything else is left alone.
func TestFeedAddress(t *testing.T) {
	for in, want := range map[string]string{
		"https://indexer.example":                                  "https://indexer.example/api",
		"https://indexer.example/":                                 "https://indexer.example/api",
		"https://indexer.example/api":                              "https://indexer.example/api",
		"http://prowlarr:9696/1/api":                               "http://prowlarr:9696/1/api",
		"http://prowlarr.arr-stack.svc.cluster.local/4/":           "http://prowlarr.arr-stack.svc.cluster.local/4/api",
		"http://prowlarr:9696/4":                                   "http://prowlarr:9696/4/api",
		"https://example.org/prowlarr/12/":                         "https://example.org/prowlarr/12/api",
		"https://indexer.example/v1":                               "https://indexer.example/v1",
		"http://jackett:9117/api/v2.0/x/":                          "http://jackett:9117/api/v2.0/x",
		"https://indexer.example/feed?t=x":                         "https://indexer.example/feed?t=x",
		"http://192.168.1.36:9696/5":                               "http://192.168.1.36:9696/5/api",
		"http://192.168.1.36:9696/5/":                              "http://192.168.1.36:9696/5/api",
		"http://192.168.1.36:9696/5/api":                           "http://192.168.1.36:9696/5/api",
		"http://jackett:9117/api/v2.0/indexers/x/results/torznab":  "http://jackett:9117/api/v2.0/indexers/x/results/torznab/api",
		"http://jackett:9117/api/v2.0/indexers/x/results/torznab/": "http://jackett:9117/api/v2.0/indexers/x/results/torznab/api",
	} {
		if got := feedAddress(in); got != want {
			t.Errorf("feedAddress(%q) = %q, want %q", in, got, want)
		}
	}
}
