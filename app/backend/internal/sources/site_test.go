package sources

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// siteClient is a Client for a site Source at a test server.
func siteClient(format, url string) *Client {
	return New(Config{URL: url, Format: format, Protocol: "torrent", AllowLocal: true})
}

func TestSiteTermFollowsProwlarr(t *testing.T) {
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
		if got := siteTerm(c.q); got != c.want {
			t.Errorf("siteTerm(%+v) = %q, want %q", c.q, got, c.want)
		}
	}
}

func TestSiteCategoriesTakeInTheWholeGroup(t *testing.T) {
	got := siteCategories(iptCategories, []int{2000})
	for _, want := range []int{72, 101, 48, 20} {
		if !slices.Contains(got, want) {
			t.Errorf("movies took %v, want %d in it", got, want)
		}
	}
	if slices.Contains(got, 73) || slices.Contains(got, 60) {
		t.Errorf("movies took a TV category: %v", got)
	}
	if got := siteCategories(iptCategories, []int{5070}); !slices.Equal(got, []int{60}) {
		t.Errorf("anime %v", got)
	}
}

func TestMagnetHashReadsHexAndBase32(t *testing.T) {
	if h := magnetHash("magnet:?xt=urn:btih:C50C83BCF28CE06475A9C2CBB9196C712FC38358&dn=x"); h != "c50c83bcf28ce06475a9c2cbb9196c712fc38358" {
		t.Errorf("hex %q", h)
	}
	if h := magnetHash("magnet:?xt=urn:btih:7MQKREFLWT6YWI7ZNTB434YSGIXXTORD&dn=x"); len(h) != 40 {
		t.Errorf("base32 %q", h)
	}
	if h := magnetHash("https://example.org/x.torrent"); h != "" {
		t.Errorf("not a magnet %q", h)
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

func TestIPTorrentsSearchAndRead(t *testing.T) {
	var got *http.Request
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		_, _ = w.Write(file(t, "iptorrents.html"))
	})
	c := New(Config{URL: srv.URL + "/", Format: SiteIPTorrents, Protocol: "torrent", AllowLocal: true, APIKey: "uid=1234; pass=abcdef0123",
		UserAgent: "Mozilla/5.0 Test", FreeleechOnly: true})
	items, err := c.Search(context.Background(), Query{Title: "Dune Part Two", Year: 2024, IMDb: "tt15239678", Categories: []int{2000}})
	if err != nil {
		t.Fatal(err)
	}
	if got.URL.Path != "/t" || got.Header.Get("Cookie") != "uid=1234; pass=abcdef0123" || got.Header.Get("User-Agent") != "Mozilla/5.0 Test" {
		t.Fatalf("request %s %v", got.URL, got.Header)
	}
	q := got.URL.Query()
	if q.Get("q") != "+(tt15239678)" || q.Get("qf") != "all" || q.Get("free") != "on" || !q.Has("72") || !q.Has("101") || q.Has("73") {
		t.Fatalf("query %s", got.URL.RawQuery)
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

func TestIPTorrentsSeasonAndFeed(t *testing.T) {
	var raw []string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		raw = append(raw, r.URL.RawQuery)
		_, _ = w.Write(file(t, "iptorrents.html"))
	})
	c := siteClient(SiteIPTorrents, srv.URL)
	if _, err := c.Search(context.Background(), Query{Title: "Some Show", TV: true, Season: 1, Categories: []int{5000}}); err != nil {
		t.Fatal(err)
	}
	if n, err := c.Test(context.Background()); err != nil || n != 2 {
		t.Fatalf("test %d %v", n, err)
	}
	if !strings.Contains(raw[0], "q=%2B%28Some+Show+S01%2A%29") || !strings.Contains(raw[0], "60=") || strings.Contains(raw[0], "free=") {
		t.Fatalf("season query %s", raw[0])
	}
	if raw[1] != "" {
		t.Fatalf("newest list query %q", raw[1])
	}
}

// The sign-in page in place of the list means the cookie was turned down,
// and a list whose category column is text needs a site setting changed.
func TestIPTorrentsSignInAndSettingErrors(t *testing.T) {
	page := "iptorrents_login.html"
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		b := file(t, page)
		if page == "iptorrents.html" {
			b = []byte(strings.Replace(string(b), `<a href="?48">`, `<a href="/t?48">`, 1))
		}
		_, _ = w.Write(b)
	})
	c := New(Config{URL: srv.URL, Format: SiteIPTorrents, Protocol: "torrent", AllowLocal: true, APIKey: "uid=1234; pass=secretvalue"})
	_, err := c.Search(context.Background(), Query{Title: "Dune"})
	var se *Error
	if !errors.As(err, &se) || se.Kind != KindLoginRefused || !strings.Contains(Explain(err), "cookie") {
		t.Fatalf("sign-in page: %v", err)
	}
	page = "iptorrents.html"
	_, err = c.Search(context.Background(), Query{Title: "Dune"})
	if !errors.As(err, &se) || se.Kind != KindBroken || !strings.Contains(se.Msg, "Icons") {
		t.Fatalf("text categories: %v", err)
	}
	if strings.Contains(c.scrub("cookie uid=1234; pass=secretvalue and secretvalue"), "secretvalue") {
		t.Fatal("the cookie leaked into a message")
	}
}

func TestKnabenSearchAndRead(t *testing.T) {
	var body string
	var got *http.Request
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":[
			{"title":"Dune.2021.1080p.BluRay.x264-GRP","hash":"C50C83BCF28CE06475A9C2CBB9196C712FC38358","details":"https://site.example/t/1","link":null,
			 "magnetUrl":"magnet:?xt=urn:btih:C50C83BCF28CE06475A9C2CBB9196C712FC38358&dn=Dune","bytes":1717545252,"seeders":61,"peers":17,"date":"2026-08-23T12:32:54+00:00"},
			{"title":"Dune.2021.Dead","hash":"0D07E4F103E30DA703C41CE6ACCAF5CEEE3E5D7E","magnetUrl":"magnet:?xt=urn:btih:0D07E4F103E30DA703C41CE6ACCAF5CEEE3E5D7E","seeders":0},
			{"title":"Dune.2021.Link","hash":"","details":"https://site.example/t/3","link":"https://site.example/dl/3.torrent","bytes":5,"seeders":2,"peers":0,"date":"2026-08-23T12:00:00"}]}`))
	})
	c := siteClient(SiteKnaben, srv.URL)
	items, err := c.Search(context.Background(), Query{Title: "Dune", Year: 2021, Categories: []int{2000}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != http.MethodPost || got.URL.Path != "/v1" || got.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("request %s %s %v", got.Method, got.URL, got.Header)
	}
	for _, want := range []string{`"query":"Dune 2021"`, `"search_field":"title"`, `"order_by":"date"`, `"size":100`, `"hide_unsafe":true`, `3000000`, `3003000`} {
		if !strings.Contains(body, want) {
			t.Errorf("body %s lacks %s", body, want)
		}
	}
	if strings.Contains(body, "2000000") {
		t.Errorf("movie search asked for TV: %s", body)
	}
	if len(items) != 2 {
		t.Fatalf("items %+v", items)
	}
	a, b := items[0], items[1]
	if a.InfoHash != "c50c83bcf28ce06475a9c2cbb9196c712fc38358" || !strings.HasPrefix(a.Link, "magnet:") || *a.Seeders != 61 || *a.Peers != 78 ||
		a.Size != 1717545252 || a.GUID != "https://site.example/t/1" || !a.Published.Equal(time.Date(2026, 8, 23, 12, 32, 54, 0, time.UTC)) {
		t.Fatalf("first %+v", a)
	}
	if b.Link != "https://site.example/dl/3.torrent" || b.InfoHash != "" || !b.Published.Equal(time.Date(2026, 8, 23, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("a link and a date in Knaben's own time %+v", b)
	}
	if _, err := c.Feed(context.Background(), []int{5000}); err != nil || strings.Contains(body, `"query"`) || !strings.Contains(body, "2000000") {
		t.Fatalf("newest list body %s %v", body, err)
	}
}

func TestSubsPleaseSearchAndRead(t *testing.T) {
	var got []string
	answer := `{"One Piece - 1180":{"time":"09/27/26","release_date":"Sun, 27 Sep 2026 16:50:17 +0000","show":"One Piece","episode":"1180","downloads":[
		{"res":"480","magnet":"magnet:?xt=urn:btih:7MQKREFLWT6YWI7ZNTB434YSGIXXTORD&dn=x&xl=382937687"},
		{"res":"1080","magnet":"magnet:?xt=urn:btih:OPJ54OODVM6FHF6EYN37EL52MNMA5OFF&dn=y"}]}}`
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Path+"?"+r.URL.RawQuery)
		_, _ = w.Write([]byte(answer))
	})
	c := siteClient(SiteSubsPlease, srv.URL)
	items, err := c.Search(context.Background(), Query{Title: "[SubsPlease] One Piece 1080p", TV: true, Season: 1, Episode: "1180"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != "/api/?f=search&s=One+Piece+1180&tz=UTC" {
		t.Fatalf("search %s", got[0])
	}
	if len(items) != 2 || items[0].Title != "[SubsPlease] One Piece - 1180 (1080p)" || items[1].Title != "[SubsPlease] One Piece - 1180 (480p)" {
		t.Fatalf("items %+v", items)
	}
	if items[1].Size != 382937687 || items[0].Size != 1300<<20 || items[0].Seeders != nil || len(items[0].InfoHash) != 40 ||
		!items[0].Published.Equal(time.Date(2026, 9, 27, 16, 50, 17, 0, time.UTC)) {
		t.Fatalf("read %+v", items[0])
	}
	if _, err := c.Search(context.Background(), Query{Title: "Show", TV: true, Season: 2, Episode: "3"}); err != nil || got[1] != "/api/?f=search&s=Show+S2+03&tz=UTC" {
		t.Fatalf("later season %s %v", got[1], err)
	}
	if _, err := c.Feed(context.Background(), nil); err != nil || got[2] != "/api/?f=latest&tz=UTC" {
		t.Fatalf("newest list %s %v", got[2], err)
	}
	answer = "[]"
	if items, err := c.Search(context.Background(), Query{Title: "Nothing", TV: true}); err != nil || len(items) != 0 {
		t.Fatalf("empty answer %v %v", items, err)
	}
}

func TestTorrentsCSVSearchAndRead(t *testing.T) {
	var got []string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Path+"?"+r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"torrents":[
			{"infohash":"ED0DA850C273E3E15A819BDCBBF418BC85107EC8","name":"Dune (2021) [1080p] [WEBRip] ","size_bytes":2947989023,"created_unix":1634482853,"seeders":945,"leechers":171},
			{"infohash":"1018e90e7b2151782170f366048431452c157b3c","name":"Dune (2021) [1080p] [BluRay]","size_bytes":3075970854,"created_unix":1638590074,"seeders":null},
			null,{"infohash":"bad","name":"Broken"}]}`))
	})
	c := siteClient(SiteTorrentsCSV, srv.URL+"/")
	items, err := c.Search(context.Background(), Query{Title: "Dune", Year: 2021})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != "/service/search?q=Dune+2021&size=100" {
		t.Fatalf("search %s", got[0])
	}
	if len(items) != 2 || items[0].Title != "Dune (2021) [1080p] [BluRay]" || *items[0].Seeders != 0 {
		t.Fatalf("items newest first %+v", items)
	}
	b := items[1]
	if b.InfoHash != "ed0da850c273e3e15a819bdcbbf418bc85107ec8" || *b.Peers != 1116 || !strings.HasPrefix(b.Link, "magnet:?xt=urn:btih:ed0da850c273e3e15a819bdcbbf418bc85107ec8&dn=Dune") ||
		!strings.Contains(b.Link, "tr=") || b.GUID != "magnet:?xt=urn:btih:ed0da850c273e3e15a819bdcbbf418bc85107ec8" {
		t.Fatalf("read %+v", b)
	}
	if items, err := c.Search(context.Background(), Query{Title: "It"}); err != nil || len(items) != 0 || len(got) != 1 {
		t.Fatalf("a too short search was sent: %v %v", got, err)
	}
	if n, err := c.Test(context.Background()); err != nil || n != 2 {
		t.Fatalf("test %d %v", n, err)
	}
	if _, err := c.Feed(context.Background(), nil); err == nil {
		t.Fatal("TorrentsCSV has no newest list")
	}
}

// Every site call waits the site's least gap and a site's own bad answer is
// a broken Source, not a down one.
func TestSiteGapAndBrokenAnswer(t *testing.T) {
	cfg, err := ConfigFor(context.Background(), domain.Source{Format: SiteKnaben, URL: "https://api.knaben.org/"}, nil)
	if err != nil || cfg.GapMS != siteGapMS {
		t.Fatalf("gap %d %v", cfg.GapMS, err)
	}
	cfg, _ = ConfigFor(context.Background(), domain.Source{Format: SiteKnaben, Limits: domain.SourceLimits{GapMS: 5000}}, nil)
	if cfg.GapMS != 5000 {
		t.Fatalf("a longer gap was cut to %d", cfg.GapMS)
	}
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>not json</html>")) })
	_, err = siteClient(SiteKnaben, srv.URL).Search(context.Background(), Query{Title: "Dune"})
	var se *Error
	if !errors.As(err, &se) || se.Kind != KindBroken {
		t.Fatalf("not json: %v", err)
	}
}

// The transport that fetches Release files gives a site that signs in its
// cookie and user agent on its own host, and never on another or over a
// downgraded scheme.
func TestSignInTransportOnlyOnTheSiteHost(t *testing.T) {
	var cookies []string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		cookies = append(cookies, r.Header.Get("Cookie")+"|"+r.Header.Get("User-Agent"))
	})
	cfg := Config{URL: srv.URL + "/", Format: SiteIPTorrents, AllowLocal: true, APIKey: "uid=1; pass=2", UserAgent: "UA"}
	h := &http.Client{Transport: Transport(cfg)}
	for _, u := range []string{srv.URL + "/download.php/1/x.torrent", strings.Replace(srv.URL, "127.0.0.1", "localhost", 1) + "/x"} {
		resp, err := h.Get(u)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	if len(cookies) != 2 || cookies[0] != "uid=1; pass=2|UA" || strings.Contains(cookies[1], "uid=1") {
		t.Fatalf("cookies sent %q", cookies)
	}
	if _, ok := Transport(Config{URL: srv.URL, Format: SiteKnaben, AllowLocal: true}).(signInTransport); ok {
		t.Fatal("a public site got a sign-in transport")
	}
}
