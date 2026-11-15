package sites

import (
	"archive/zip"
	"bytes"
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

const madeUpCue = "1\n00:00:01,000 --> 00:00:02,000\nmade up\n"

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
	s, _ := New("yifysubtitles", cfg(srv.URL))
	_, err := s.Search(context.Background(), Query{Kind: "movie", IMDb: "tt0900001", Languages: []string{"en"}})
	if kindOf(err) != KindDown {
		t.Fatalf("%v", err)
	}
}

// BSPlayer finds subtitles by the file hash: the answers made for that
// very file are hash matches, and one made for another file is left out.
func TestBSPlayerKeepsOnlyThisFilesHash(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch a := r.Header.Get("SOAPAction"); {
		case strings.HasSuffix(a, `#logIn"`):
			serveFile(t, w, "bsplayer-login.xml", "")
		case strings.HasSuffix(a, `#searchSubtitles"`):
			serveFile(t, w, "bsplayer-search.xml", srv.URL)
		default:
			_, _ = w.Write([]byte(`<?xml version="1.0"?><SOAP-ENV:Envelope xmlns:SOAP-ENV="http://schemas.xmlsoap.org/soap/envelope/"><SOAP-ENV:Body><r><return><result>200</result><status>OK</status></return></r></SOAP-ENV:Body></SOAP-ENV:Envelope>`))
		}
	}))
	t.Cleanup(srv.Close)
	s, err := New("bsplayer", cfg(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Search(context.Background(), Query{Kind: "movie", Title: "The Lantern", IMDb: "tt0900001", Hash: "8E245D9679D31E12", Size: 12345678, Languages: []string{"en", "nl"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "5001" || got[1].Key != "5002" || !got[0].HashMatched || !got[1].HashMatched {
		t.Fatalf("%+v", got)
	}
}
