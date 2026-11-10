package migrate

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The download handling settings are read from Radarr's and Sonarr's three
// settings pages, or from a backup's Config table.
func TestReadDownloadHandling(t *testing.T) {
	u, key := radarrAPIWith(t, map[string]any{
		"/api/v3/config/indexer": map[string]any{"id": 1, "availabilityDelay": 0, "rssSyncInterval": 30},
		"/api/v3/config/mediamanagement": map[string]any{"id": 1, "minimumFreeSpaceWhenImporting": 100, "copyUsingHardlinks": true,
			"importExtraFiles": true, "extraFileExtensions": "srt,sub,idx,ass,ssa", "downloadPropersAndRepacks": "preferAndUpgrade", "recycleBin": ""},
		"/api/v3/config/downloadclient": map[string]any{"id": 1, "enableCompletedDownloadHandling": true, "autoRedownloadFailed": true,
			"autoRedownloadFailedFromInteractiveSearch": true, "checkForFinishedDownloadInterval": 1},
	})
	s := read(t, api(Radarr, u, key))
	h := s.DownloadHandling
	if num(h["rsssyncinterval"]) != 30 || num(h["minimumfreespacewhenimporting"]) != 100 || !boolean(h["importextrafiles"]) ||
		str(h["extrafileextensions"]) != "srt,sub,idx,ass,ssa" || !boolean(h["autoredownloadfailedfrominteractivesearch"]) || h["recyclebin"] != nil || len(h) != 9 {
		t.Fatalf("api %+v", h)
	}
	p := filepath.Join(t.TempDir(), "radarr.db")
	makeDB(t, p, append(radarrDB(), `CREATE TABLE Config (Id INTEGER PRIMARY KEY, Key TEXT, Value TEXT)`,
		`INSERT INTO Config VALUES (1, 'apikey', 'S3CR3T_IN_CONFIG'), (2, 'rsssyncinterval', '15'), (3, 'importextrafiles', 'True'), (4, 'extrafileextensions', 'srt')`)...)
	s = read(t, file(Radarr, p))
	if h := s.DownloadHandling; num(h["rsssyncinterval"]) != 15 || !boolean(h["importextrafiles"]) || str(h["extrafileextensions"]) != "srt" || len(h) != 3 {
		t.Fatalf("backup %+v", h)
	}
	if s.Settings != nil {
		t.Fatalf("backup settings %+v", s.Settings)
	}
}

// Radarr and Sonarr each give their settings; the plan holds one Download
// handling item where the stricter one wins, and lists what HolyArrs always
// does its own way.
func TestPlanDownloadHandling(t *testing.T) {
	radarr := &Snapshot{Kind: Radarr, DownloadHandling: map[string]any{"rsssyncinterval": int64(30), "minimumfreespacewhenimporting": int64(100),
		"importextrafiles": true, "extrafileextensions": "srt", "downloadpropersandrepacks": "doNotPrefer", "autoredownloadfailedfrominteractivesearch": false}}
	sonarr := &Snapshot{Kind: Sonarr, DownloadHandling: map[string]any{"rsssyncinterval": int64(15), "minimumfreespacewhenimporting": int64(50),
		"importextrafiles": true, "extrafileextensions": "srt,sub,idx,ass,ssa,mkv", "downloadpropersandrepacks": "preferAndUpgrade", "copyusinghardlinks": true}}
	p := BuildPlan([]Input{{1, radarr}, {2, sonarr}}, Choices{})
	it := item(t, p, GroupDownloaders, "download_handling")
	h := it.DownloadHandling
	if h == nil || *h.FeedMinutes != 15 || *h.MinFreeMB != 100 || !*h.ImportExtraFiles || !slices.Equal(h.ExtraFileExtensions, []string{"srt", "sub", "idx", "ass", "ssa"}) {
		t.Fatalf("item %+v %+v", it, h)
	}
	if !strings.Contains(it.Detail, "check the feeds every 15 minutes") || !strings.Contains(it.Detail, "keep 100 MB free") {
		t.Fatalf("detail %q", it.Detail)
	}
	if n, ok := notMoved(p, "radarr download_handling.downloadpropersandrepacks"); !ok || !strings.Contains(n.Why, "Pick rules") {
		t.Fatalf("propers %+v", n)
	}
	if _, ok := notMoved(p, "radarr download_handling.autoredownloadfailedfrominteractivesearch"); !ok {
		t.Fatal("redownload from a manual choice off is not listed")
	}
	if _, ok := notMoved(p, "sonarr download_handling.downloadpropersandrepacks"); ok {
		t.Fatal("prefer and upgrade is what HolyArrs does: no line")
	}
	if !slices.Contains(p.Mapped, "sonarr download_handling.rsssyncinterval") {
		t.Fatalf("mapped %v", p.Mapped)
	}
}

// A qBittorrent download client keeps its category and its after-import
// category; another kind keeps the category only.
func TestPlanDownloaderCategories(t *testing.T) {
	prov := func(name, kind string, set map[string]any) Provider {
		return Provider{ID: 1, Name: name, Kind: kind, Enabled: true, Priority: 10, Settings: set}
	}
	radarr := &Snapshot{Kind: Radarr, DownloadClients: []Provider{
		prov("qBit", "QBittorrent", map[string]any{"host": "qbit", "port": int64(80), "movieCategory": "radarr", "movieImportedCategory": "radarr-imported"}),
		prov("Tr", "Transmission", map[string]any{"host": "tr", "port": int64(9091), "movieCategory": "radarr", "movieImportedCategory": "done"}),
	}}
	p := BuildPlan([]Input{{1, radarr}}, Choices{})
	q := item(t, p, GroupDownloaders, "downloader:http://qbit:80").Downloader
	if q.Category != "radarr" || q.PostImportCategory != "radarr-imported" {
		t.Fatalf("qbit %+v", q)
	}
	tr := item(t, p, GroupDownloaders, "downloader:http://tr:9091").Downloader
	if tr.Category != "radarr" || tr.PostImportCategory != "" {
		t.Fatalf("transmission %+v", tr)
	}
	if _, ok := notMoved(p, "radarr download_clients[1].post_import_category"); !ok {
		t.Fatal("the transmission after-import category is not listed")
	}
}
