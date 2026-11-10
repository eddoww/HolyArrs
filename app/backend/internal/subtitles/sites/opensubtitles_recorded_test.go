package sites

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// osRecorded serves the OpenSubtitles.com replies in testdata, shaped after
// the site's public API reference, and checks every call is made the way the
// reference asks: the key and the app name on every call, sorted query
// parameters, and the token only on downloads.
type osRecorded struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	queries  []string
	download string // the testdata file the download call answers with
	login    string
	status   map[string]int
}

func newOSRecorded(t *testing.T) *osRecorded {
	o := &osRecorded{t: t, download: "opensubtitles-download.json", login: "opensubtitles-login.json", status: map[string]int{}}
	o.srv = httptest.NewServer(http.HandlerFunc(o.serve))
	t.Cleanup(o.srv.Close)
	return o
}

func (o *osRecorded) reply(w http.ResponseWriter, name string, status int) {
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		o.t.Error(err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(bytes.ReplaceAll(b, []byte("DOWNLOAD"), []byte(o.srv.URL)))
}

func (o *osRecorded) serve(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if strings.HasPrefix(r.URL.Path, "/download/made-up/") {
		// The real link is on another host and gets neither key nor token;
		// TestCredentialsStayOnSiteHost checks that.
		_, _ = w.Write([]byte(madeUpCue))
		return
	}
	if r.Header.Get("Api-Key") != fakeKey {
		o.reply(w, "opensubtitles-key-refused.json", http.StatusForbidden)
		return
	}
	if r.Header.Get("User-Agent") != "HolyArrs v1" {
		o.t.Errorf("user agent %q", r.Header.Get("User-Agent"))
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/login":
		var in struct{ Username, Password string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Username != fakeUser || in.Password != fakePass {
			o.reply(w, "opensubtitles-login-refused.json", http.StatusUnauthorized)
			return
		}
		o.reply(w, o.login, http.StatusOK)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/subtitles":
		if r.Header.Get("Authorization") != "" {
			o.t.Error("a search sent the token")
		}
		o.queries = append(o.queries, r.URL.RawQuery)
		if r.URL.Query().Get("type") == "episode" {
			o.reply(w, "opensubtitles-search-episode.json", http.StatusOK)
			return
		}
		o.reply(w, "opensubtitles-search-movie.json", http.StatusOK)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/download":
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"file_id":7000001}` || r.Header.Get("Authorization") != "Bearer eyJmYWtlIjoidG9rZW4ifQ.made-up-token" {
			o.t.Errorf("download body %s auth %q", body, r.Header.Get("Authorization"))
		}
		status := http.StatusOK
		if o.download == "opensubtitles-quota.json" {
			status = http.StatusNotAcceptable
		}
		o.reply(w, o.download, status)
	default:
		http.NotFound(w, r)
	}
}

func (o *osRecorded) site(t *testing.T, key string) Site {
	t.Helper()
	s, err := New("opensubtitles", Config{Name: "OS", URL: o.srv.URL + "/api/v1", Key: key, Username: fakeUser, Password: fakePass, GapMS: 1, AllowLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOpenSubtitlesRecordedMovie(t *testing.T) {
	o := newOSRecorded(t)
	s := o.site(t, fakeKey)
	ctx := context.Background()
	got, err := s.Search(ctx, Query{Kind: "movie", Title: "The Lantern", Year: 2010, IMDb: "tt0900001", Hash: "8E245D9679D31E12", Languages: []string{"nl", "en"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "ai_translated=exclude&imdb_id=900001&languages=en%2Cnl&machine_translated=exclude&moviehash=8e245d9679d31e12&type=movie"; o.queries[0] != want {
		t.Errorf("query\n got %s\nwant %s", o.queries[0], want)
	}
	// The AI translation is left out even if the site sends it.
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	first, hi, forced := got[0], got[1], got[2]
	if first.Key != "7000001" || first.Language != "nl" || !first.HashMatched || !first.MatchedByID || !first.Trusted || first.Forced ||
		first.FPS != 23.976 || first.Downloads != 5210 || first.ReleaseNames[0] != "The.Lantern.2010.1080p.BluRay.x264-GRP" || first.Uploader != "someone" {
		t.Errorf("first %+v", first)
	}
	// Hearing impaired with foreign parts is a full subtitle; foreign parts
	// alone is a forced one.
	if !hi.HearingImpaired || hi.Forced || hi.HashMatched {
		t.Errorf("hi %+v", hi)
	}
	if forced.Language != "en" || !forced.Forced {
		t.Errorf("forced %+v", forced)
	}
	b, err := s.Download(ctx, first)
	if err != nil || string(b) != madeUpCue {
		t.Fatalf("download %q %v", b, err)
	}
}

func TestOpenSubtitlesRecordedEpisode(t *testing.T) {
	o := newOSRecorded(t)
	s := o.site(t, fakeKey)
	got, err := s.Search(context.Background(), Query{Kind: "episode", Title: "Quiet Harbour", Year: 2018, Season: 1, Episode: 2, IMDb: "tt0900555", Languages: []string{"pt"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "ai_translated=exclude&episode_number=2&languages=pt&machine_translated=exclude&parent_imdb_id=900555&season_number=1&type=episode"; o.queries[0] != want {
		t.Errorf("query\n got %s\nwant %s", o.queries[0], want)
	}
	if len(got) != 1 || got[0].Language != "pt" || got[0].Title != "Quiet Harbour" || got[0].Season != 1 || got[0].Episode != 2 || !got[0].MatchedByID || got[0].HashMatched {
		t.Fatalf("%+v", got)
	}
}

func TestOpenSubtitlesRecordedRefusals(t *testing.T) {
	o := newOSRecorded(t)
	ctx := context.Background()
	q := Query{Kind: "movie", Title: "The Lantern", Year: 2010, IMDb: "tt0900001", Languages: []string{"nl"}}
	// A wrong API key is named as such.
	_, err := o.site(t, "not-the-key").Search(ctx, q)
	var se *Error
	if kindOf(err) != KindLoginRefused || !errors.As(err, &se) || !strings.Contains(se.Msg, "API key") {
		t.Fatalf("wrong key: %v", err)
	}
	// The daily download count runs out: the site says when it resets.
	o.download = "opensubtitles-quota.json"
	s := o.site(t, fakeKey)
	got, err := s.Search(ctx, q)
	if err != nil || len(got) == 0 {
		t.Fatal(err)
	}
	_, err = s.Download(ctx, got[0])
	if kindOf(err) != KindDownloadLimit {
		t.Fatalf("quota: %v", err)
	}
	// A wrong password is refused at sign-in.
	s, _ = New("opensubtitles", Config{Name: "OS", URL: o.srv.URL + "/api/v1", Key: fakeKey, Username: fakeUser, Password: "wrong-word-xyz", GapMS: 1, AllowLocal: true})
	if _, err := s.Download(ctx, got[0]); kindOf(err) != KindLoginRefused || strings.Contains(err.Error(), "wrong-word-xyz") {
		t.Fatalf("password: %v", err)
	}
}
