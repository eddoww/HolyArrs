package migrate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
	"github.com/HolyArrs/holyarrs/app/backend/internal/requests"
)

// twoTier is a request app's setup with a plain and a 4K Radarr and Sonarr
// that are the same apps with other folders and profiles, signing in with
// Emby (as settings.json and the settings API hold them, keys included).
var twoTier = map[string]any{
	"main": map[string]any{"apiKey": "S3CR3T_MAIN", "mediaServerType": 3, "partialRequestsEnabled": true, "enableSpecialEpisodes": false},
	"radarr": []any{
		map[string]any{"name": "Radarr", "hostname": "radarr.local", "port": 80, "apiKey": "S3CR3T_RADARR", "activeProfileName": "HD-1080p", "activeDirectory": "/media/Movies", "is4k": false, "isDefault": true},
		map[string]any{"name": "Radarr 4K", "hostname": "radarr.local", "port": 80, "apiKey": "S3CR3T_RADARR", "activeProfileName": "Ultra-HD", "activeDirectory": "/media/Movies-4k", "is4k": true, "isDefault": true},
	},
	"sonarr": []any{
		map[string]any{"name": "Sonarr", "hostname": "sonarr.local", "port": 80, "apiKey": "S3CR3T_SONARR", "activeProfileName": "HD-1080p", "activeDirectory": "/media/TV", "is4k": false, "isDefault": true},
		map[string]any{"name": "Sonarr 4K", "hostname": "sonarr.local", "port": 80, "apiKey": "S3CR3T_SONARR", "activeProfileName": "Ultra-HD", "activeDirectory": "/media/TV-4K", "is4k": true, "isDefault": true},
	},
	"jellyfin": map[string]any{"name": "emby", "ip": "192.168.100.31", "port": 8098, "useSsl": false, "urlBase": "", "apiKey": "S3CR3T_EMBY"},
}

func checkTwoTier(t *testing.T, s *Snapshot) {
	t.Helper()
	if len(s.RequestServers) != 4 {
		t.Fatalf("servers %+v", s.RequestServers)
	}
	r4k := s.RequestServers[1]
	if r4k.Kind != "movie" || !r4k.FourK || !r4k.Default || r4k.Profile != "Ultra-HD" || r4k.Folder != "/media/Movies-4k" || r4k.Address != "radarr.local:80" {
		t.Fatalf("Radarr 4K %+v", r4k)
	}
	if s.RequestServers[3].Kind != "show" || !s.RequestServers[3].FourK {
		t.Fatalf("Sonarr 4K %+v", s.RequestServers[3])
	}
	if s.Settings["media_server"] != "emby" || s.Settings["media_server_url"] != "http://192.168.100.31:8098" ||
		s.Settings["partial_requests"] != true || s.Settings["special_episodes"] != false {
		t.Fatalf("settings %+v", s.Settings)
	}
}

// The request app's Radarr and Sonarr servers and its media server are
// read through the API and from a backup, with no key.
func TestSeerrServersRead(t *testing.T) {
	key := "S3CR3T_SEERR"
	srv := fakeAPI(t, "X-API-Key", key, map[string]any{
		"/api/v1/status":            map[string]any{"version": "2.7.3"},
		"/api/v1/user":              map[string]any{"pageInfo": map[string]any{"pages": 1}, "results": []any{}},
		"/api/v1/request":           map[string]any{"pageInfo": map[string]any{"pages": 1}, "results": []any{}},
		"/api/v1/settings/main":     twoTier["main"],
		"/api/v1/settings/radarr":   twoTier["radarr"],
		"/api/v1/settings/sonarr":   twoTier["sonarr"],
		"/api/v1/settings/jellyfin": twoTier["jellyfin"],
	})
	checkTwoTier(t, read(t, api(Seerr, srv.URL, key)))

	settings := `{"main":{"apiKey":"S3CR3T_MAIN","mediaServerType":3,"partialRequestsEnabled":true,"enableSpecialEpisodes":false},` +
		`"radarr":[{"name":"Radarr","hostname":"radarr.local","port":80,"apiKey":"S3CR3T_RADARR","activeProfileName":"HD-1080p","activeDirectory":"/media/Movies","is4k":false,"isDefault":true},` +
		`{"name":"Radarr 4K","hostname":"radarr.local","port":80,"apiKey":"S3CR3T_RADARR","activeProfileName":"Ultra-HD","activeDirectory":"/media/Movies-4k","is4k":true,"isDefault":true}],` +
		`"sonarr":[{"name":"Sonarr","activeProfileName":"HD-1080p","activeDirectory":"/media/TV","isDefault":true},{"name":"Sonarr 4K","activeProfileName":"Ultra-HD","activeDirectory":"/media/TV-4K","is4k":true,"isDefault":true}],` +
		`"jellyfin":{"ip":"192.168.100.31","port":8098,"apiKey":"S3CR3T_EMBY"}}`
	zp := filepath.Join(t.TempDir(), "config.zip")
	makeZip(t, zp, map[string][]byte{"config/db/db.sqlite3": dbBytes(t, seerrDB(true)...), "config/settings.json": []byte(settings)})
	s := read(t, file(Seerr, zp))
	if len(s.RequestServers) != 4 || s.RequestServers[1].Folder != "/media/Movies-4k" || s.Settings["media_server_url"] != "http://192.168.100.31:8098" {
		t.Fatalf("backup %+v %+v", s.RequestServers, s.Settings)
	}
}

// The plan turns the request app's servers into where approved requests
// go, plain and 4K, makes the 4K folders' titles their 4K Version and
// gives each folder its server's Pick rules; the move sets the request
// rules and the Emby sign-in address (off), and undo puts them back.
func TestMoveTwoTierRequestSetup(t *testing.T) {
	var movies4k string
	l := newMoveLabWith(t, func(radarr, _, seerr *Snapshot) {
		radarr.Folders = []Folder{{Path: "/media/Movies"}, {Path: "/media/Movies-4k"}}
		radarr.Titles[0].Path = "/media/Movies-4k/The Matrix (1999)"
		radarr.QualityProfiles = []Profile{{ID: 2, Name: "Ultra-HD", Cutoff: "Remux-2160p"}, {ID: 4, Name: "HD-1080p", Cutoff: "Bluray-1080p"}}
		seerr.Settings = map[string]any{"partial_requests": true, "special_episodes": false, "media_server": "emby", "media_server_url": "http://192.168.100.31:8098"}
		seerr.RequestServers = []RequestServer{
			{Kind: "movie", Name: "Radarr", Default: true, Profile: "HD-1080p", Folder: "/media/Movies"},
			{Kind: "movie", Name: "Radarr 4K", Default: true, FourK: true, Profile: "Ultra-HD", Folder: "/media/Movies-4k"},
		}
	})
	root := filepath.Dir(l.movies)
	movies4k = filepath.Join(root, "films-4k")
	if err := os.MkdirAll(filepath.Join(movies4k, "The Matrix (1999)"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(l.file, filepath.Join(movies4k, "The Matrix (1999)", "The.Matrix.1999.2160p.mkv")); err != nil {
		t.Fatal(err)
	}
	l.rules = []PathRule{{From: "/media/Movies", To: filepath.ToSlash(l.movies)}, {From: "/media/Movies-4k", To: filepath.ToSlash(movies4k)}}
	p := l.plan(t)
	var target *Item
	for i, it := range p.Group(GroupRequests).Items {
		if it.Key == "wishlist_targets:seerr" {
			target = &p.Group(GroupRequests).Items[i]
		}
	}
	if target == nil || target.AskSettings.FourK == nil || target.AskSettings.FourK.Folder != filepath.ToSlash(movies4k) || target.AskSettings.FourK.Preset != "4K" ||
		target.AskSettings.Movie.Preset != "Standard" {
		t.Fatalf("request targets %+v", target)
	}
	if tp := p.Group(GroupTitles).Items[0].Title; tp.VersionLabel != mediafiles.FourKName {
		t.Fatalf("title in the 4K folder %+v", tp)
	}
	mv := l.move(t, Choices{})
	if mv.State != "done" {
		t.Fatalf("move %+v", mv.Report)
	}
	if line := lineOf(t, mv.Report, "wishlist_targets:seerr"); line.Result != ResChanged {
		t.Fatalf("targets line %+v", line)
	}
	ctx := context.Background()
	set, err := requests.ReadSettings(ctx, l.st.Read())
	if err != nil {
		t.Fatal(err)
	}
	f4k := int64(l.count(t, "SELECT id FROM library_folders WHERE path = ?", filepath.Clean(movies4k)))
	fhd := int64(l.count(t, "SELECT id FROM library_folders WHERE path = ?", filepath.Clean(l.movies)))
	r4k := int64(l.count(t, "SELECT id FROM pick_rules WHERE name = '4K'"))
	if set.FourKLibraryFolderID != f4k || set.FourKPickRulesID != r4k || set.LibraryFolderID != fhd || set.PickRulesID == 0 {
		t.Fatalf("request rules %+v (4K folder %d, rules %d)", set, f4k, r4k)
	}
	if n := l.count(t, "SELECT count(*) FROM library_folders WHERE id = ? AND default_pick_rules_id = ?", f4k, r4k); n != 1 {
		t.Fatal("the 4K folder does not start new titles on 4K")
	}
	if n := l.count(t, "SELECT count(*) FROM settings WHERE grp = 'this_app' AND key = 'sign_in_emby_url' AND value = 'http://192.168.100.31:8098'"); n != 1 {
		t.Fatal("the Emby sign-in address was not kept")
	}
	if !hasNext(mv.Report, "Sign in with Emby came over off") {
		t.Fatalf("next %v", mv.Report.Next)
	}
	undone := l.undo(t, mv.ID)
	if undone.State != "undone" {
		t.Fatalf("undo %+v", undone)
	}
	if n := l.count(t, "SELECT count(*) FROM settings WHERE grp = 'this_app' AND key IN ('sign_in_emby_url', 'ask_4k_library_folder_id')"); n != 0 {
		t.Fatalf("%d settings stayed after undo", n)
	}
}

// A request app's servers name the profiles Pick rules were made from in
// the same move. Sonarr and Radarr both have "HD-1080p" with other rules,
// so Sonarr's becomes "HD-1080p (Sonarr)": approved movies and the movie
// folder take Radarr's, approved shows and the show folder Sonarr's, never
// the other kind's.
func TestMoveRequestTargetsUseMadeRules(t *testing.T) {
	steps := func(qs ...string) []ProfileStep {
		out := make([]ProfileStep, 0, len(qs))
		for _, q := range qs {
			out = append(out, ProfileStep{Quality: q, Allowed: true})
		}
		return out
	}
	l := newMoveLabWith(t, func(radarr, _, seerr *Snapshot) {
		radarr.QualityProfiles = []Profile{{ID: 2, Name: "HD-1080p", Cutoff: "Bluray-1080p", UpgradeAllowed: true,
			Steps: steps("WEBDL-1080p", "Bluray-1080p", "Remux-1080p")}}
		seerr.RequestServers = []RequestServer{
			{Kind: "movie", Name: "Radarr", Default: true, Profile: "HD-1080p", Folder: "/movies"},
			{Kind: "show", Name: "Sonarr", Default: true, Profile: "HD-1080p", Folder: "/tv"},
		}
	})
	shows := filepath.Join(filepath.Dir(l.movies), "tv")
	if err := os.MkdirAll(shows, 0o755); err != nil {
		t.Fatal(err)
	}
	l.rules = append(l.rules, PathRule{From: "/tv", To: filepath.ToSlash(shows)})
	sonarr := &Snapshot{Kind: Sonarr, Version: "4.0.20", Via: ViaAPI, Folders: []Folder{{Path: "/tv"}},
		QualityProfiles: []Profile{{ID: 4, Name: "HD-1080p", Cutoff: "Bluray-1080p", UpgradeAllowed: true, Steps: steps("WEBDL-1080p", "Bluray-1080p")}}}
	sonarr.finish()
	saved, err := Save(context.Background(), l.st, 1, sonarr)
	if err != nil {
		t.Fatal(err)
	}
	l.snaps = append(l.snaps, saved.ID)

	var target *AskSettingsPlan
	for _, it := range l.plan(t).Group(GroupRequests).Items {
		if it.Key == "wishlist_targets:seerr" {
			target = it.AskSettings
		}
	}
	if target == nil || target.Movie == nil || target.Show == nil || target.Movie.Preset != "HD-1080p" || target.Show.Preset != "HD-1080p (Sonarr)" {
		t.Fatalf("request targets %+v", target)
	}
	mv := l.move(t, Choices{})
	if mv.State != "done" {
		t.Fatalf("move %+v", mv.Report)
	}
	movieRules := int64(l.count(t, `SELECT id FROM pick_rules WHERE name = 'HD-1080p' AND kinds = '["movie"]'`))
	showRules := int64(l.count(t, `SELECT id FROM pick_rules WHERE name = 'HD-1080p (Sonarr)' AND kinds = '["show"]'`))
	if movieRules == 0 || showRules == 0 {
		t.Fatalf("made rules: movie %d, show %d", movieRules, showRules)
	}
	set, err := requests.ReadSettings(context.Background(), l.st.Read())
	if err != nil {
		t.Fatal(err)
	}
	if set.PickRulesID != movieRules || set.ShowPickRulesID != showRules {
		t.Fatalf("request rules: movies %d, shows %d; want %d and %d", set.PickRulesID, set.ShowPickRulesID, movieRules, showRules)
	}
	for path, want := range map[string]int64{l.movies: movieRules, shows: showRules} {
		if n := l.count(t, "SELECT count(*) FROM library_folders WHERE path = ? AND default_pick_rules_id = ?", filepath.Clean(path), want); n != 1 {
			t.Fatalf("the folder %s does not start new titles on Pick rules %d", path, want)
		}
	}
	// Undo removes the show folder and its rules; the movie folder stays, as
	// its title stays in History, and so do the rules it starts titles on.
	if u := l.undo(t, mv.ID); u.State != "undone" {
		t.Fatalf("undo %+v", u)
	}
	if n := l.count(t, "SELECT count(*) FROM pick_rules WHERE id = ?", showRules); n != 0 {
		t.Fatal("the show folder's Pick rules stayed after undo")
	}
}
