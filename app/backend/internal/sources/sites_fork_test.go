package sources

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// iptServer serves the fork's IPTorrents list page fixture (or another) and
// records each request.
func iptServer(t *testing.T, page *string, got *[]*http.Request) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = append(*got, r)
		name := *page
		if name == "iptorrents_text_categories.html" {
			name = "iptorrents.html"
		}
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Error(err)
		}
		if *page == "iptorrents_text_categories.html" {
			b = []byte(strings.Replace(string(b), `<a href="?48">`, `<a href="/t?48">`, 1))
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSiteTextFollowsProwlarr(t *testing.T) {
	for _, c := range []struct {
		q    Query
		want string
	}{
		{Query{Title: "Dune: Part Two", Year: 2024}, "Dune Part Two 2024"},
		{Query{Title: "Marvel’s Agents – S.H.I.E.L.D.", TV: true, Season: 2, Episode: "5"}, "Marvel's Agents - S.H.I.E.L.D. S02E05"},
		{Query{Title: "Show", TV: true, Season: 3}, "Show S03"},
		{Query{Title: "Daily Show", TV: true, Season: 2024, Episode: "01/31"}, "Daily Show 2024.01.31"},
		{Query{Title: "All Seasons", TV: true}, "All Seasons"},
		{Query{Music: true, Artist: "Björk", Album: "Homogenic!"}, "Björk Homogenic"},
	} {
		if got := siteText(c.q); got != c.want {
			t.Errorf("siteText(%+v) = %q, want %q", c.q, got, c.want)
		}
	}
}

func TestSiteCategoriesTakeInTheWholeGroup(t *testing.T) {
	got := siteCategories(iptCategoryTable, []int{2000})
	for _, want := range []int{72, 101, 48, 20} {
		if !slices.Contains(got, want) {
			t.Errorf("movies took %v, want %d in it", got, want)
		}
	}
	if slices.Contains(got, 73) || slices.Contains(got, 60) {
		t.Errorf("movies took a TV category: %v", got)
	}
	if got := siteCategories(iptCategoryTable, []int{5070}); !slices.Equal(got, []int{60}) {
		t.Errorf("anime %v", got)
	}
	if got := siteCategories(knabenCategoryTable, []int{2045}); !slices.Equal(got, []int{3003000}) {
		t.Errorf("knaben 4K movies %v", got)
	}
}

func TestTimeAgoAndSize(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Duration{"40.0 minutes ago": 40 * time.Minute, "1.9 weeks ago ": time.Duration(1.9 * 7 * 24 * float64(time.Hour)),
		"2 hours, 30 min ago": 150 * time.Minute, "1.2 months ago": time.Duration(1.2 * 30 * 24 * float64(time.Hour))} {
		if got := now.Sub(timeAgo(in, now)); got.Round(time.Second) != want.Round(time.Second) {
			t.Errorf("timeAgo(%q) = %v ago, want %v", in, got, want)
		}
	}
	if !timeAgo("whenever", now).IsZero() {
		t.Error("unknown words should give no time")
	}
	for in, want := range map[string]int64{"14.4 GB": 15461882265, "700 MB": 700 << 20, "1,024 KB": 1 << 20, "": 0} {
		if got := siteSize(in); got != want {
			t.Errorf("siteSize(%q) = %d, want %d", in, got, want)
		}
	}
}

// The IPTorrents list read with the cookie: the IMDb search, categories,
// freeleech switch and user agent go out; dates, sizes, seeders and the
// freeleech mark come back.
func TestIPTorrentsSearchAndRead(t *testing.T) {
	page := "iptorrents.html"
	var got []*http.Request
	srv := iptServer(t, &page, &got)
	c := New(Config{URL: srv.URL, Format: "site", Site: "iptorrents", Protocol: "torrent", AllowLocal: true, APIKey: "uid=1234; pass=abcdef0123",
		SiteAgent: "Mozilla/5.0 Test", FreeleechOnly: true})
	items, err := c.Search(context.Background(), Query{Title: "Dune Part Two", Year: 2024, IMDb: "tt15239678", Categories: []int{2000}})
	if err != nil {
		t.Fatal(err)
	}
	r := got[0]
	if r.URL.Path != "/t" || r.Header.Get("Cookie") != "uid=1234; pass=abcdef0123" || r.Header.Get("User-Agent") != "Mozilla/5.0 Test" {
		t.Fatalf("request %s %v", r.URL, r.Header)
	}
	q := r.URL.Query()
	if q.Get("q") != "+(tt15239678)" || q.Get("qf") != "all" || q.Get("free") != "on" || !q.Has("72") || !q.Has("101") || q.Has("73") {
		t.Fatalf("query %s", r.URL.RawQuery)
	}
	if len(items) != 2 {
		t.Fatalf("items %+v", items)
	}
	a, b := items[0], items[1]
	if a.Title != "Dune Part Two 2024 1080p BluRay x264-GRP" || a.GUID != srv.URL+"/t/1000001" ||
		a.Link != srv.URL+"/download.php/1000001/Dune%20Part%20Two%202024%201080p%20BluRay%20x264-GRP.torrent" ||
		a.Size != 15461882265 || *a.Seeders != 21 || *a.Peers != 24 || !a.Freeleech || a.Protocol != "torrent" || a.InfoHash != "" {
		t.Fatalf("first %+v", a)
	}
	if ago := time.Since(a.Published); ago < 13*24*time.Hour || ago > 14*24*time.Hour {
		t.Fatalf("first published %v", a.Published)
	}
	if b.Title != "Some Show S01E02 1080p WEB H264-GRP" || b.Freeleech || *b.Seeders != 5 || *b.Peers != 7 || time.Since(b.Published) > 41*time.Minute {
		t.Fatalf("second %+v", b)
	}
}

// A whole season is searched with a *, the newest list asks no words, and a
// raw browser cookie without pass (a Cloudflare session) is a cookie too.
func TestIPTorrentsSeasonFeedAndRawCookie(t *testing.T) {
	page := "iptorrents.html"
	var got []*http.Request
	srv := iptServer(t, &page, &got)
	c := siteClient(srv.URL, "iptorrents", "uid=1234; cf_clearance=abc.def-123; PHPSESSID=x")
	if _, err := c.Search(context.Background(), Query{Title: "Some Show", TV: true, Season: 1, Categories: []int{5000}}); err != nil {
		t.Fatal(err)
	}
	if n, err := c.Test(context.Background()); err != nil || n != 2 {
		t.Fatalf("test %d %v", n, err)
	}
	if raw := got[0].URL.RawQuery; !strings.Contains(raw, "q=%2B%28Some+Show+S01%2A%29") || !strings.Contains(raw, "60=") || strings.Contains(raw, "free=") {
		t.Fatalf("season query %s", raw)
	}
	if got[0].Header.Get("Cookie") != "uid=1234; cf_clearance=abc.def-123; PHPSESSID=x" {
		t.Fatalf("cookie %q", got[0].Header.Get("Cookie"))
	}
	if got[len(got)-1].URL.Path != "/t" || strings.Contains(got[len(got)-1].URL.RawQuery, "q=") {
		t.Fatalf("newest list query %q", got[len(got)-1].URL.RawQuery)
	}
	// A uid with an RSS key is the RSS mode, not a cookie.
	if iptKeys("uid=77; tp=rsskey", "").cookie() != "" {
		t.Fatal("uid and tp read as a cookie")
	}
}

// The sign-in page in place of the list means the cookie was turned down,
// and a list whose category column is text needs a site setting changed.
func TestIPTorrentsSignInAndSettingErrors(t *testing.T) {
	page := "iptorrents_login.html"
	var got []*http.Request
	srv := iptServer(t, &page, &got)
	c := siteClient(srv.URL, "iptorrents", "uid=1234; pass=secretvalue")
	_, err := c.Search(context.Background(), Query{Title: "Dune"})
	var se *Error
	if !errors.As(err, &se) || se.Kind != KindLoginRefused || !strings.Contains(Explain(err), "cookie") {
		t.Fatalf("sign-in page: %v", err)
	}
	page = "iptorrents_text_categories.html"
	_, err = c.Search(context.Background(), Query{Title: "Dune"})
	if !errors.As(err, &se) || se.Kind != KindBroken || !strings.Contains(se.Msg, "Icons") {
		t.Fatalf("text categories: %v", err)
	}
}

// A site keeps at least 2 seconds between calls, its user agent (upstream's
// site_agent) and freeleech choice come from extra, and the file header
// carries both cookie and user agent.
func TestSiteConfigFromExtra(t *testing.T) {
	src := domain.Source{Format: "site", URL: "https://iptorrents.com", Extra: map[string]string{"site": "iptorrents", "site_login": "cookie", "site_agent": "UA/1", "freeleech_only": "1"}}
	cfg, err := ConfigFor(context.Background(), src, nil)
	if err != nil || cfg.GapMS != siteGapMS || cfg.SiteAgent != "UA/1" || !cfg.FreeleechOnly {
		t.Fatalf("config %+v %v", cfg, err)
	}
	src.Limits.GapMS = 5000
	if cfg, _ := ConfigFor(context.Background(), src, nil); cfg.GapMS != 5000 {
		t.Fatalf("a longer gap was cut to %d", cfg.GapMS)
	}
	ref := int64(1)
	src.SecretRef = &ref
	h, err := FileHeader(context.Background(), src, oneSecret("uid=1; pass=2"))
	if err != nil || h.Get("Cookie") != "uid=1; pass=2" || h.Get("User-Agent") != "UA/1" {
		t.Fatalf("file header %v %v", h, err)
	}
}

// route: POST /api/v1/sources
// The site Source form keeps the freeleech choice for IPTorrents only (the
// user agent is upstream's site_agent), and refuses a cookie of more than
// one line.
func TestSiteSourceFreeleech(t *testing.T) {
	t.Parallel()
	srv, _ := sourceServer(t)
	var x SourceView
	_ = json.Unmarshal(do(t, "POST", srv.URL+"/api/v1/sources", `{"name":"IPT","format":"site","site":"iptorrents","site_login":"cookie","api_key":"uid=1; pass=2","site_agent":" Mozilla/5.0 X ","freeleech_only":true}`, 201), &x)
	if x.SiteAgent != "Mozilla/5.0 X" || !x.FreeleechOnly {
		t.Fatalf("added %+v", x)
	}
	var k SourceView
	_ = json.Unmarshal(do(t, "POST", srv.URL+"/api/v1/sources", `{"name":"K","format":"site","site":"knaben","freeleech_only":true}`, 201), &k)
	if k.FreeleechOnly {
		t.Fatalf("knaben kept freeleech %+v", k)
	}
	do(t, "POST", srv.URL+"/api/v1/sources", `{"name":"IPT2","format":"site","site":"iptorrents","api_key":"uid=1;\npass=2"}`, 400)
}
