package downloads

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// Hashes in a client shared with Radarr: one in HolyArrs' category, one in
// its after-import category, one of Radarr's.
const (
	scopeOwn      = "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
	scopeImported = "b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2"
	scopeRadarr   = "c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3"
)

// sharedClient is a qBittorrent other apps share: torrents/info honours the
// category and hashes filters, and every write is recorded.
type sharedClient struct {
	mu     sync.Mutex
	writes []string
}

func (f *sharedClient) saw() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.writes, " ")
}

func (f *sharedClient) server(t *testing.T) *httptest.Server {
	t.Helper()
	all := []map[string]any{
		{"hash": scopeOwn, "name": "Own", "category": "holyarrs", "progress": 0.5, "state": "downloading"},
		{"hash": scopeImported, "name": "Imported", "category": "holyarrs-imported", "progress": 1, "state": "uploading"},
		{"hash": scopeRadarr, "name": "Radarr.Movie", "category": "radarr", "progress": 1, "state": "uploading"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		_ = r.ParseMultipartForm(1 << 20)
		path := strings.TrimPrefix(r.URL.Path, "/api/v2/")
		switch path {
		case "app/webapiVersion":
			_, _ = w.Write([]byte("2.15.1"))
		case "app/version":
			_, _ = w.Write([]byte("v5.1.0"))
		case "app/preferences":
			_, _ = w.Write([]byte(`{"save_path":"/data/downloads"}`))
		case "torrents/categories":
			_, _ = w.Write([]byte(`{"holyarrs":{"name":"holyarrs","savePath":""},"radarr":{"name":"radarr","savePath":""}}`))
		case "torrents/info":
			out := []map[string]any{}
			for _, t := range all {
				if c := r.Form.Get("category"); c != "" && t["category"] != c {
					continue
				}
				if hs := r.Form.Get("hashes"); hs != "" && !strings.Contains(hs, t["hash"].(string)) {
					continue
				}
				out = append(out, t)
			}
			_ = json.NewEncoder(w).Encode(out)
		case "torrents/properties":
			_, _ = w.Write([]byte(`{"total_size":5000,"save_path":"/data/downloads"}`))
		default:
			f.mu.Lock()
			f.writes = append(f.writes, path+"?"+r.Form.Encode())
			f.mu.Unlock()
			if path == "torrents/add" {
				_, _ = w.Write([]byte("Ok."))
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// guarded is the Downloads API on a Downloader that manages only its own
// categories, on a client shared with Radarr.
func guarded(t *testing.T) (*dl41, *sharedClient, string) {
	t.Helper()
	t.Setenv(ReadOnlyEnv, "")
	x := seed41(t, mediafiles.SlotDownloading, "downloading")
	f := &sharedClient{}
	exec41(t, x.s, "UPDATE downloaders SET url = ?, allow_local_network = 1, writable = 1, own_categories_only = 1, post_import_category = 'holyarrs-imported'", f.server(t).URL)
	var did int64
	_ = x.s.Read().QueryRow("SELECT id FROM downloaders").Scan(&did)
	return x, f, x.srv.URL + "/api/v1/downloaders/" + itoa(did)
}

// Actions on a torrent in another app's category are refused before they
// reach the client; the Downloader's own and after-import categories work.
func TestOwnCategoriesRefuseOutsideItems(t *testing.T) {
	x, f, base := guarded(t)
	do(t, "POST", base+"/items/"+scopeOwn+"/pause", "", http.StatusOK)
	do(t, "POST", base+"/items/"+scopeImported+"/pause", "", http.StatusOK)
	do(t, "POST", base+"/items/"+scopeRadarr+"/pause", "", http.StatusForbidden)
	do(t, "POST", base+"/items/"+scopeRadarr+"/remove", `{"delete_files":true}`, http.StatusForbidden)
	do(t, "GET", base+"/items/"+scopeRadarr+"/detail", "", http.StatusForbidden)
	do(t, "POST", base+"/bulk", `{"action":"pause","hashes":["`+scopeOwn+`","`+scopeRadarr+`"]}`, http.StatusForbidden)
	do(t, "POST", base+"/share-limits", `{"hashes":["`+scopeRadarr+`"],"ratio":1,"seeding_minutes":-2,"inactive_minutes":-2,"action":"stop"}`, http.StatusForbidden)
	do(t, "POST", base+"/speed-limits", `{"hashes":["`+scopeRadarr+`"],"down":1,"up":1}`, http.StatusForbidden)
	if saw := f.saw(); strings.Contains(saw, scopeRadarr) || strings.Contains(saw, "torrents/delete") {
		t.Fatalf("an action on Radarr's torrent reached the client: %s", saw)
	}
	do(t, "POST", base+"/bulk", `{"action":"pause","hashes":["`+scopeOwn+`","`+scopeImported+`"]}`, http.StatusOK)
	// Off, the Downloader steers the whole client again.
	exec41(t, x.s, "UPDATE downloaders SET own_categories_only = 0")
	do(t, "POST", base+"/items/"+scopeRadarr+"/pause", "", http.StatusOK)
}

// A hand add goes into the Downloader's own category, never another app's.
func TestOwnCategoriesAddOnlyIntoOwn(t *testing.T) {
	_, f, base := guarded(t)
	do(t, "POST", base+"/torrents", `{"links":["magnet:?xt=urn:btih:`+strings.Repeat("d", 40)+`"],"category":"radarr"}`, http.StatusForbidden)
	if strings.Contains(f.saw(), "torrents/add") {
		t.Fatalf("the add reached the client: %s", f.saw())
	}
	do(t, "POST", base+"/torrents", `{"links":["magnet:?xt=urn:btih:`+strings.Repeat("d", 40)+`"]}`, http.StatusOK)
	if !strings.Contains(f.saw(), "torrents/add?category=holyarrs&") {
		t.Fatalf("not added into the own category: %s", f.saw())
	}
}

// Pause all stops only the Downloader's own torrents, never the whole client.
func TestOwnCategoriesPauseAllStopsOnlyOwn(t *testing.T) {
	x, f, _ := guarded(t)
	do(t, "POST", x.srv.URL+"/api/v1/downloads/pause-all", "", http.StatusOK)
	saw := f.saw()
	if !strings.Contains(saw, "torrents/stop?hashes="+scopeOwn+"%7C"+scopeImported) || strings.Contains(saw, scopeRadarr) || strings.Contains(saw, "hashes=all") {
		t.Fatalf("pause all: %s", saw)
	}
}

// mainClient is a qBittorrent-like engine with the change feed, for the reader.
type mainClient struct {
	*fakeClient
	*fakeMain
}

// The background reader keeps only the Downloader's own categories, so the
// Downloads screen, its live updates and title matching never see the rest.
func TestOwnCategoriesReaderKeepsOnlyOwn(t *testing.T) {
	r := newRig(t)
	feed := `{"rid":1,"full_update":true,"torrents":{` +
		`"` + scopeOwn + `":{"name":"Own","category":"holyarrs","state":"downloading","progress":0.1},` +
		`"` + scopeImported + `":{"name":"Imported","category":"holyarrs-imported","state":"uploading","progress":1},` +
		`"` + scopeRadarr + `":{"name":"Radarr.Movie","category":"radarr","state":"uploading","progress":1}}}`
	r.m.Engine = func(domain.Downloader) (Engine, error) {
		return mainClient{r.client, &fakeMain{answers: []string{feed}}}, nil
	}
	exec41(t, r.db, "UPDATE downloaders SET adapter = 'qbittorrent', own_categories_only = 1, post_import_category = 'holyarrs-imported'")
	ctx := context.Background()
	var d domain.Downloader
	if err := database.Get(ctx, r.db.Read(), "downloaders", r.dlID, &d); err != nil {
		t.Fatal(err)
	}
	if err := r.s.read(ctx, d); err != nil {
		t.Fatal(err)
	}
	items := r.s.Store.Current(r.dlID).Items
	if len(items) != 2 || items[scopeRadarr].ID != "" || items[scopeOwn].ID == "" || items[scopeImported].ID == "" {
		t.Fatalf("snapshot %+v", items)
	}
	d.OwnCategoriesOnly = false
	if err := r.s.read(ctx, d); err != nil {
		t.Fatal(err)
	}
	if items := r.s.Store.Current(r.dlID).Items; len(items) != 3 {
		t.Fatalf("switch off: snapshot %+v", items)
	}
}
