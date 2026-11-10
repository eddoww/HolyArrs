package sites

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// pageServer serves recorded pages by exact path. A value starting with
// "file:" is read from testdata; "zip:" serves a zip with one subtitle; any
// other value is sent as it is. Every call is logged.
type pageServer struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	routes   map[string]string
	calls    []string
	bodies   []string
	referers []string
}

func newPageServer(t *testing.T, routes map[string]string) *pageServer {
	t.Helper()
	p := &pageServer{t: t, routes: routes}
	p.srv = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *pageServer) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.RequestURI()
	p.mu.Lock()
	p.calls = append(p.calls, key)
	p.bodies = append(p.bodies, string(body))
	p.referers = append(p.referers, r.Referer())
	v, ok := p.routes[key]
	p.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasPrefix(v, "file:"):
		b, err := os.ReadFile(filepath.Join("testdata", strings.TrimPrefix(v, "file:")))
		if err != nil {
			p.t.Error(err)
		}
		_, _ = w.Write(bytes.ReplaceAll(b, []byte("DOWNLOAD"), []byte(p.srv.URL)))
	case strings.HasPrefix(v, "zip:"):
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		f, _ := zw.Create("made-up.srt")
		_, _ = f.Write([]byte(strings.TrimPrefix(v, "zip:")))
		_ = zw.Close()
		_, _ = w.Write(buf.Bytes())
	case strings.HasPrefix(v, "redirect:"):
		http.Redirect(w, r, strings.TrimPrefix(v, "redirect:"), http.StatusFound)
	default:
		_, _ = w.Write([]byte(v))
	}
}

func (p *pageServer) site(t *testing.T, kind string) Site {
	t.Helper()
	s, err := New(kind, cfg(p.srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const madeUpCue = "1\n00:00:01,000 --> 00:00:02,000\nmade up\n"

func TestYIFYRecorded(t *testing.T) {
	p := newPageServer(t, map[string]string{
		"GET /movie-imdb/tt0900001":                            "file:yify-movie.html",
		"GET /subtitles/the-lantern-2010-dutch-yify-900001":    "file:yify-subtitle.html",
		"GET /subtitle/the-lantern-2010-dutch-yify-900001.zip": "zip:" + madeUpCue,
	})
	s := p.site(t, "yify")
	ctx := context.Background()
	got, err := s.Search(ctx, Query{Kind: "movie", Title: "The Lantern", Year: 2010, IMDb: "tt900001", Languages: []string{"nl", "pt"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 { // the row linking off the site is left out
		t.Fatalf("%+v", got)
	}
	first := got[0]
	if first.Key != "the-lantern-2010-dutch-yify-900001" || first.Language != "nl" || first.HearingImpaired || !first.MatchedByID ||
		strings.Join(first.ReleaseNames, "|") != "The.Lantern.2010.1080p.BluRay.x264-GRP|The.Lantern.2010.720p.BluRay.x264-GRP" || first.Uploader != "someone" {
		t.Errorf("first %+v", first)
	}
	if !got[1].HearingImpaired || got[2].Language != "pt" {
		t.Errorf("hi %+v pt %+v", got[1], got[2])
	}
	b, err := s.Download(ctx, first)
	if err != nil || !bytes.HasPrefix(b, []byte("PK")) {
		t.Fatalf("download %q %v", b, err)
	}
	if ref := p.referers[len(p.referers)-1]; ref != p.srv.URL+"/subtitles/the-lantern-2010-dutch-yify-900001" {
		t.Errorf("the zip was asked without its page as Referer: %q", ref)
	}
	// Shows, and movies without an IMDb id, are not asked for.
	if got, _ := s.Search(ctx, Query{Kind: "episode", Title: "The Lantern", Season: 1, Episode: 1, IMDb: "tt900001", Languages: []string{"nl"}}); len(got) != 0 {
		t.Error("episode answered")
	}
	if got, _ := s.Search(ctx, Query{Kind: "movie", Title: "The Lantern", Languages: []string{"nl"}}); len(got) != 0 {
		t.Error("no id answered")
	}
	// An IMDb id the site does not know gives nothing rather than an error.
	if got, err := s.Search(ctx, Query{Kind: "movie", IMDb: "tt0900002", Languages: []string{"nl"}}); err != nil || len(got) != 0 {
		t.Fatalf("unknown %+v %v", got, err)
	}
}

func TestSubf2mRecordedMovie(t *testing.T) {
	p := newPageServer(t, map[string]string{
		"GET /subtitles/searchbytitle?l=&query=The+Lantern": "file:subf2m-search.html",
		"GET /subtitles/the-lantern/dutch":                  "file:subf2m-movie-dutch.html",
		"GET /subtitles/the-lantern/dutch/700001":           "file:subf2m-subtitle.html",
		"GET /subtitles/the-lantern/dutch/700001/download":  "redirect:/files/700001.zip",
		"GET /files/700001.zip":                             "zip:" + madeUpCue,
	})
	s := p.site(t, "subf2m")
	ctx := context.Background()
	got, err := s.Search(ctx, Query{Kind: "movie", Title: "The Lantern", Year: 2010, IMDb: "tt0900001", Languages: []string{"nl", "xx"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	c := got[0]
	if c.Key != "the-lantern/dutch/700001" || c.Language != "nl" || !c.MatchedByID || c.Title != "The Lantern" || c.Year != 2010 ||
		strings.Join(c.ReleaseNames, "|") != "The.Lantern.2010.1080p.BluRay.x264-GRP|The.Lantern.2010.720p.BluRay.x264-GRP" {
		t.Errorf("first %+v", c)
	}
	b, err := s.Download(ctx, c)
	if err != nil || !bytes.HasPrefix(b, []byte("PK")) {
		t.Fatalf("download %q %v", b, err)
	}
	// A page for another film of the same name, by its IMDb id, gives nothing.
	if got, err := s.Search(ctx, Query{Kind: "movie", Title: "The Lantern", Year: 2010, IMDb: "tt0900009", Languages: []string{"nl"}}); err != nil || len(got) != 0 {
		t.Fatalf("other id %+v %v", got, err)
	}
}

func TestSubf2mRecordedEpisode(t *testing.T) {
	p := newPageServer(t, map[string]string{
		"GET /subtitles/searchbytitle?l=&query=Quiet+Harbour": "file:subf2m-search.html",
		"GET /subtitles/quiet-harbour-first-season/dutch":     "file:subf2m-season-dutch.html",
	})
	s := p.site(t, "subf2m")
	got, err := s.Search(context.Background(), Query{Kind: "episode", Title: "Quiet Harbour", Year: 2018, Season: 1, Episode: 2, IMDb: "tt0900555", Languages: []string{"nl"}})
	if err != nil {
		t.Fatal(err)
	}
	// Only the two subtitles for episode 2: not episode 3 and not the season pack.
	if len(got) != 2 || got[0].Key != "quiet-harbour-first-season/dutch/800001" || got[1].Key != "quiet-harbour-first-season/dutch/800004" {
		t.Fatalf("%+v", got)
	}
	if got[0].Season != 1 || got[0].Episode != 2 || got[0].Title != "Quiet Harbour" || got[0].Year != 0 || !got[0].MatchedByID {
		t.Errorf("first %+v", got[0])
	}
	if name, ok := subf2mSeason("Quiet Harbour - Season 3", 3); !ok || name != "Quiet Harbour" {
		t.Errorf("season 3: %q %v", name, ok)
	}
	if _, ok := subf2mSeason("Quiet Harbour - Second Season", 1); ok {
		t.Error("second season taken for the first")
	}
}

// The download of a site without a key may move to its file host, and only
// there.
func TestSubf2mRedirectsOnlyToItsFileHost(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("PK")) }))
	t.Cleanup(other.Close)
	p := newPageServer(t, map[string]string{
		"GET /subtitles/the-lantern/dutch/700001":          "file:subf2m-subtitle.html",
		"GET /subtitles/the-lantern/dutch/700001/download": "redirect:" + other.URL + "/x.zip",
	})
	s, err := New("subf2m", cfg(p.srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	c := Candidate{Link: "/subtitles/the-lantern/dutch/700001"}
	if _, err := s.Download(context.Background(), c); err == nil {
		t.Fatal("redirect to an unknown host followed")
	}
	s.(*subf2m).c.otherOrigins = []string{strings.ToLower(other.URL)}
	if b, err := s.Download(context.Background(), c); err != nil || string(b) != "PK" {
		t.Fatalf("file host %q %v", b, err)
	}
}

func TestTVsubtitlesRecorded(t *testing.T) {
	p := newPageServer(t, map[string]string{
		"POST /search2.php":                              "file:tvsubtitles-search.html",
		"GET /tvshow-902-1.html":                         "file:tvsubtitles-season.html",
		"GET /episode-50002.html":                        "file:tvsubtitles-episode.html",
		"GET /download-60001.html":                       "file:tvsubtitles-download.html",
		"GET /files/Quiet%20Harbour_1x02_WEB.GRP.en.zip": "zip:" + madeUpCue,
	})
	s := p.site(t, "tvsubtitles")
	ctx := context.Background()
	got, err := s.Search(ctx, Query{Kind: "episode", Title: "Quiet Harbour (US)", Year: 2018, Season: 1, Episode: 2, Languages: []string{"en", "nl"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if p.bodies[0] != "qs=Quiet+Harbour" {
		t.Errorf("search form %q", p.bodies[0])
	}
	en, nl := got[0], got[1]
	if en.Key != "60001" || en.Language != "en" || en.Title != "Quiet Harbour" || en.Year != 2018 || en.Season != 1 || en.Episode != 2 ||
		en.Downloads != 812 || strings.Join(en.ReleaseNames, "|") != "Quiet Harbour 1x02 WEB-GRP" {
		t.Errorf("en %+v", en)
	}
	if nl.Language != "nl" || strings.Join(nl.ReleaseNames, "|") != "Quiet Harbour 1x02 HDTV" {
		t.Errorf("nl %+v", nl)
	}
	b, err := s.Download(ctx, en)
	if err != nil || !bytes.HasPrefix(b, []byte("PK")) {
		t.Fatalf("download %q %v", b, err)
	}
	// An episode the season page does not list gives nothing.
	if got, err := s.Search(ctx, Query{Kind: "episode", Title: "Quiet Harbour", Year: 2018, Season: 1, Episode: 9, Languages: []string{"en"}}); err != nil || len(got) != 0 {
		t.Fatalf("missing episode %+v %v", got, err)
	}
	if got, _ := s.Search(ctx, Query{Kind: "movie", Title: "Quiet Harbour", Languages: []string{"en"}}); len(got) != 0 {
		t.Error("movie answered")
	}
}

func TestBSPlayerRecorded(t *testing.T) {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte(madeUpCue))
	_ = zw.Close()
	var mu sync.Mutex
	var actions []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/download/file/abc123" {
			_, _ = w.Write(gz.Bytes())
			return
		}
		body, _ := io.ReadAll(r.Body)
		action := r.Header.Get("SOAPAction")
		actions = append(actions, action)
		switch {
		case strings.HasSuffix(action, `#logIn"`):
			if !strings.Contains(string(body), "<AppID>HolyArrs v1</AppID>") {
				t.Errorf("login body %s", body)
			}
			serveFile(t, w, "bsplayer-login.xml", "")
		case strings.HasSuffix(action, `#searchSubtitles"`):
			if !strings.Contains(string(body), "<movieHash>8e245d9679d31e12</movieHash><movieSize>12345678</movieSize><languageId>eng,dut</languageId><imdbId>900001</imdbId>") {
				t.Errorf("search body %s", body)
			}
			serveFile(t, w, "bsplayer-search.xml", "http://"+r.Host)
		case strings.HasSuffix(action, `#logOut"`):
			_, _ = w.Write([]byte("<ok/>"))
		}
	}))
	t.Cleanup(srv.Close)
	s, err := New("bsplayer", cfg(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	got, err := s.Search(ctx, Query{Kind: "movie", Title: "The Lantern", IMDb: "tt0900001", Hash: "8E245D9679D31E12", Size: 12345678, Languages: []string{"en", "nl"}})
	if err != nil {
		t.Fatal(err)
	}
	// The third item is for another file hash and is left out.
	if len(got) != 2 || got[0].Key != "5001" || !got[0].HashMatched || got[0].Language != "en" || got[0].HearingImpaired ||
		got[1].Language != "nl" || !got[1].HearingImpaired {
		t.Fatalf("%+v", got)
	}
	if len(actions) != 3 || !strings.HasSuffix(actions[2], `#logOut"`) {
		t.Errorf("calls %q", actions)
	}
	b, err := s.Download(ctx, got[0])
	if err != nil || string(b) != madeUpCue {
		t.Fatalf("download %q %v", b, err)
	}
	// Without a file hash the service is only signed in to and out of.
	mu.Lock()
	actions = nil
	mu.Unlock()
	if got, err := s.Search(ctx, Query{Kind: "movie", Title: "The Lantern", Languages: []string{"en"}}); err != nil || len(got) != 0 {
		t.Fatalf("no hash %+v %v", got, err)
	}
	if len(actions) != 2 {
		t.Errorf("no hash calls %q", actions)
	}
}

func TestBSPlayerNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch a := r.Header.Get("SOAPAction"); {
		case strings.HasSuffix(a, `#logIn"`):
			serveFile(t, w, "bsplayer-login.xml", "")
		case strings.HasSuffix(a, `#searchSubtitles"`):
			serveFile(t, w, "bsplayer-notfound.xml", "")
		}
	}))
	t.Cleanup(srv.Close)
	s, _ := New("bsplayer", cfg(srv.URL))
	got, err := s.Search(context.Background(), Query{Kind: "episode", Title: "Quiet Harbour", Season: 1, Episode: 2, Hash: "8e245d9679d31e12", Size: 1, Languages: []string{"en"}})
	if err != nil || len(got) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}

func serveFile(t *testing.T, w http.ResponseWriter, name, download string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Error(err)
	}
	_, _ = w.Write(bytes.ReplaceAll(b, []byte("DOWNLOAD"), []byte(download)))
}

// A site without an account that answers 403 rests like a site that is down,
// rather than for refused account details.
func TestKeylessForbiddenIsDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	t.Cleanup(srv.Close)
	s, _ := New("yify", cfg(srv.URL))
	_, err := s.Search(context.Background(), Query{Kind: "movie", IMDb: "tt0900001", Languages: []string{"en"}})
	if kindOf(err) != KindDown {
		t.Fatalf("%v", err)
	}
}
