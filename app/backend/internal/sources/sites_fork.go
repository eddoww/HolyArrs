package sources

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// The fork's reading of the built-in sites, as Prowlarr reads them: search
// words cleaned its way, Torznab categories turned into each site's own,
// and IPTorrents read from its torrent list pages with a browser cookie
// (dates, freeleech marks, IMDb searches, whole seasons, and the sign-in
// page told from the list by its sign-out link). A cookie-signed site gets
// the user agent its cookie was made with on every call to its host.

// siteGapMS is the least time between two calls to a built-in site, as
// Prowlarr keeps it, whatever the Source's own limits say.
const siteGapMS = 2000

// Extra keys of a built-in site Source besides the site itself: the user
// agent its cookie was made with, and "1" to list freeleech Releases only.
const (
	extraUserAgent = "user_agent"
	extraFreeleech = "freeleech_only"
)

// withDefaults fills in the timeout and window a Client uses when the
// Source sets none, so a stored Client compares equal to a fresh Config.
func withDefaults(cfg Config) Config {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Hour
	}
	return cfg
}

// siteAgent adds the user agent a site's cookie was made with.
func siteAgent(h http.Header, cfg Config) {
	if cfg.UserAgent != "" {
		h.Set("User-Agent", cfg.UserAgent)
	}
}

var (
	dashes = regexp.MustCompile(`\p{Pd}+`)
	quotes = strings.NewReplacer("\u0060", "'", "\u00b4", "'", "\u2018", "'", "\u2019", "'")
)

// siteWords cleans a search for a site as Prowlarr does: dashes and quotes
// made plain, and only letters, digits, spaces and - . _ ( ) @ / ' [ ] + %
// kept.
func siteWords(s string) string {
	s = quotes.Replace(dashes.ReplaceAllString(s, "-"))
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) || strings.ContainsRune("-._()@/'[]+%", r) {
			return r
		}
		return -1
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// episodeWords is how Prowlarr names an episode in a text search: S01E02,
// S01 for a whole season, or 2024.01.31 for a daily show.
func episodeWords(q Query) string {
	if q.Season <= 0 {
		return ""
	}
	if t, err := time.Parse("2006 01/02", strconv.Itoa(q.Season)+" "+q.Episode); err == nil {
		return t.Format("2006.01.02")
	}
	if q.Episode == "" {
		return fmt.Sprintf("S%02d", q.Season)
	}
	if n, err := strconv.Atoi(q.Episode); err == nil {
		return fmt.Sprintf("S%02dE%02d", q.Season, n)
	}
	return fmt.Sprintf("S%02dE%s", q.Season, q.Episode)
}

// siteCategory maps one of a site's own category numbers to a Torznab one.
type siteCategory struct {
	site    int
	torznab int
}

// siteCategories turns the Torznab categories asked for into the site's
// own, as Prowlarr does: a whole group (2000) takes in every category under
// it (2040, 2045) the site maps.
func siteCategories(cats []siteCategory, asked []int) []int {
	var out []int
	for _, c := range cats {
		for _, a := range asked {
			if (c.torznab == a || a%1000 == 0 && c.torznab/1000 == a/1000) && !slices.Contains(out, c.site) {
				out = append(out, c.site)
			}
		}
	}
	return out
}

// knabenCategoryTable is Knaben's categories as Prowlarr maps them.
var knabenCategoryTable = []siteCategory{
	{1000000, 3000}, {1001000, 3010}, {1002000, 3040}, {1003000, 3030}, {1004000, 3020}, {1005000, 3050}, {1006000, 3050},
	{2000000, 5000}, {2001000, 5040}, {2002000, 5030}, {2003000, 5045}, {2004000, 5080}, {2005000, 5020}, {2006000, 5060},
	{2007000, 5050}, {2008000, 5050},
	{3000000, 2000}, {3001000, 2040}, {3002000, 2030}, {3003000, 2045}, {3004000, 2070}, {3005000, 2010}, {3006000, 2010},
	{3007000, 2060}, {3008000, 2020},
	{6000000, 5070}, {6001000, 5070}, {6002000, 5070}, {6003000, 5070}, {6004000, 5070}, {6005000, 3020}, {6007000, 3050},
	{6008000, 5070},
}

// iptSearch reads IPTorrents' torrent list with the cookie: the newest
// list for the feed, else a search. A movie with an IMDb id is searched by
// the id in the description, as Prowlarr does; anything else by its words.
// A whole season ends with a * so every episode matches.
func (c *Client) iptSearch(ctx context.Context, base string, q Query, k iptKey) ([]Item, error) {
	var parts []string
	for _, n := range siteCategories(iptCategories, q.Categories) {
		parts = append(parts, strconv.Itoa(n)+"=")
	}
	if c.cfg.FreeleechOnly {
		parts = append(parts, "free=on")
	}
	if !q.feed {
		term, imdb := siteText(q), ""
		if !q.TV && !q.Music && q.IMDb != "" {
			imdb, term = "tt"+strings.TrimPrefix(q.IMDb, "tt"), ""
		}
		if q.TV && q.Season > 0 && q.Episode == "" && q.Words == "" {
			term += "*"
		}
		if imdb != "" {
			// IPTorrents searches with Sphinx: +( ) asks for every word, and qf=all looks in the description too.
			parts = append(parts, "q="+url.QueryEscape("+("+imdb+")"), "qf=all")
		}
		if term != "" {
			parts = append(parts, "q="+url.QueryEscape("+("+term+")"))
		}
	}
	raw := base + "/t"
	if len(parts) > 0 {
		raw += "?" + strings.Join(parts, "&")
	}
	h := http.Header{"Cookie": {k.cookie()}}
	siteAgent(h, c.cfg)
	body, err := c.send(ctx, http.MethodGet, raw, nil, h)
	if err != nil {
		return nil, err
	}
	return iptParse(body, base+"/", time.Now())
}

// IPTorrents is a private site read from its torrent list pages, signed in
// with the cookie of a browser session. Its categories, query words and
// columns follow Prowlarr's IPTorrents code.
var iptCategories = []siteCategory{
	{72, 2000}, {87, 2060}, {77, 2030}, {101, 2045}, {89, 2040}, {90, 2030}, {96, 2030}, {6, 2070}, {48, 2050},
	{54, 2000}, {62, 2030}, {38, 2010}, {68, 2000}, {20, 2080}, {7, 2030}, {100, 2000},
	{73, 5000}, {26, 5080}, {55, 5060}, {78, 5030}, {23, 5040}, {24, 5030}, {25, 5030}, {66, 5030}, {82, 5020},
	{65, 5000}, {83, 5020}, {79, 5030}, {22, 5010}, {5, 5040}, {99, 5040}, {4, 5030}, {60, 5070},
	{75, 3000}, {3, 3010}, {80, 3040}, {93, 3000}, {37, 3020}, {21, 3020}, {64, 3030},
}

// iptRow is one torrent row as read.
type iptRow struct {
	cells     []string
	title     string
	details   string
	download  string
	category  string
	free      bool
	sub       string
	hasTitle  bool
	inTitle   bool
	inSub     bool
	subDepth  int
	cellIndex int
}

var errIPTCategoryText = errors.New(`set "Torrents - Category column" to "Icons" in your IPTorrents settings, wait a minute, then try again`)

// iptParse reads the torrent table of an IPTorrents list page. A page
// without the sign-out link is the sign-in page: the cookie was turned down
// or has run out.
func iptParse(body []byte, base string, now time.Time) ([]Item, error) {
	if !bytes.Contains(body, []byte("lout.php")) {
		return nil, &Error{Kind: KindLoginRefused, Msg: "the sign-in cookie was not taken or has run out; paste a fresh one from your browser"}
	}
	start := bytes.Index(body, []byte(`<table id="torrents"`))
	if start < 0 {
		return []Item{}, nil
	}
	table := body[start:]
	if end := bytes.Index(table, []byte("</table>")); end >= 0 {
		table = table[:end+len("</table>")]
	}
	header, rows, err := iptRows(table)
	if err != nil {
		return nil, brokenAnswer("IPTorrents", err)
	}
	col := func(name string, def int) int {
		for i, h := range header {
			if h == name {
				return i
			}
		}
		return def
	}
	sizeAt, filesAt := col("Sort by size", 5), col("Sort by files", -1)
	out := []Item{}
	for _, r := range rows {
		if !r.hasTitle || r.download == "" {
			continue
		}
		if r.category == "" {
			return nil, &Error{Kind: KindBroken, Msg: errIPTCategoryText.Error()}
		}
		at := 6
		if len(r.cells) == 10 {
			at = 7
		}
		seedAt, leechAt := col("Sort by seeders", at+1), col("Sort by leechers", at+2)
		cell := func(i int) string {
			if i < 0 || i >= len(r.cells) {
				return ""
			}
			return r.cells[i]
		}
		it := Item{Title: iptTitle(r.title), GUID: iptLink(base, r.details), Link: iptLink(base, r.download), Size: siteSize(cell(sizeAt)),
			Protocol: "torrent", Freeleech: r.free}
		if it.Title == "" || it.Link == "" {
			continue
		}
		seeders, sErr := strconv.Atoi(strings.TrimSpace(cell(seedAt)))
		leechers, lErr := strconv.Atoi(strings.TrimSpace(cell(leechAt)))
		if sErr == nil {
			it.Seeders = &seeders
			if lErr == nil {
				peers := seeders + leechers
				it.Peers = &peers
			}
		}
		if filesAt >= 0 {
			if n, err := strconv.Atoi(strings.TrimSpace(strings.ReplaceAll(cell(filesAt), "Go to files", ""))); err == nil && n > 0 {
				it.Files = n
			}
		}
		// The line under the name ends in "| 1.9 weeks ago by uploader".
		parts := strings.Split(r.sub, "|")
		when, _, _ := strings.Cut(parts[len(parts)-1], " by ")
		it.Published = timeAgo(when, now)
		out = append(out, it)
	}
	return out, nil
}

// iptRows reads the header names and torrent rows out of the torrent table.
// The page is HTML, not XML, so cells and rows are often left open; a new
// cell or row closes the one before.
func iptRows(table []byte) ([]string, []*iptRow, error) {
	d := xml.NewDecoder(bytes.NewReader(table))
	d.Strict, d.AutoClose, d.Entity = false, xml.HTMLAutoClose, xml.HTMLEntity
	var header []string
	var rows []*iptRow
	var row *iptRow
	inHead, inTH := false, false
	for {
		tok, err := d.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			attr := func(name string) string {
				for _, a := range t.Attr {
					if strings.EqualFold(a.Name.Local, name) {
						return a.Value
					}
				}
				return ""
			}
			class := " " + attr("class") + " "
			switch strings.ToLower(t.Name.Local) {
			case "thead":
				inHead = true
			case "tbody":
				inHead = false
			case "tr":
				inTH = false
				if !inHead {
					row = &iptRow{cellIndex: -1}
					rows = append(rows, row)
				}
			case "th":
				inTH = inHead
				if inTH {
					header = append(header, "")
				}
			case "td":
				if row != nil {
					row.cells = append(row.cells, "")
					row.cellIndex = len(row.cells) - 1
				}
			case "a":
				href := attr("href")
				switch {
				case row == nil:
				case strings.Contains(class, " hv ") && !row.hasTitle:
					row.hasTitle, row.inTitle, row.details = true, true, href
				case strings.HasPrefix(href, "/download.php/") && row.download == "":
					row.download = href
				case row.cellIndex == 0 && strings.HasPrefix(href, "?") && row.category == "":
					row.category = href[1:]
				}
			case "span":
				if row != nil && strings.Contains(class, " free ") {
					row.free = true
				}
			case "div":
				if row != nil && row.inSub {
					row.subDepth++
				} else if row != nil && strings.Contains(class, " sub ") {
					row.inSub, row.subDepth = true, 0
				}
			}
		case xml.EndElement:
			switch strings.ToLower(t.Name.Local) {
			case "thead":
				inHead = false
			case "th":
				inTH = false
			case "a":
				if row != nil {
					row.inTitle = false
				}
			case "div":
				if row != nil && row.inSub {
					if row.subDepth == 0 {
						row.inSub = false
					} else {
						row.subDepth--
					}
				}
			}
		case xml.CharData:
			text := string(t)
			if inTH && len(header) > 0 {
				header[len(header)-1] += text
			}
			if row == nil {
				continue
			}
			if row.cellIndex >= 0 {
				row.cells[row.cellIndex] += text
			}
			if row.inTitle {
				row.title += text
			}
			if row.inSub {
				row.sub += text
			}
		}
	}
	for i := range header {
		header[i] = strings.TrimSpace(header[i])
	}
	return header, rows, nil
}

// iptLink makes a site path into a full address, with spaces and the like escaped.
func iptLink(base, href string) string {
	if href == "" {
		return ""
	}
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	path, query, _ := strings.Cut(strings.TrimPrefix(href, "/"), "?")
	u := url.URL{Scheme: b.Scheme, Host: b.Host, Path: b.Path + path, RawQuery: query}
	return u.String()
}

var (
	iptOddChars   = regexp.MustCompile("[\u0000-\u0008\u000A-\u001F\u0100-\uFFFF]")
	iptRequest    = regexp.MustCompile(`(?i)[\(\[\{]REQ(UEST(ED)?)?[\)\]\}]`)
	iptLangPrefix = regexp.MustCompile(`(?i)^\[[a-z0-9 ._-]+\][-._ ](.*-[a-z0-9]+)$`)
)

// iptTitle cleans a Release name as Prowlarr does: odd characters and
// request marks out, and a leading [language] that would read as a group.
func iptTitle(s string) string {
	s = iptOddChars.ReplaceAllString(s, "")
	s = iptRequest.ReplaceAllString(s, "")
	s = iptLangPrefix.ReplaceAllString(s, "$1")
	return strings.Trim(s, " -:")
}

var sizeWords = regexp.MustCompile(`(?i)([\d.,]+)\s*([kmgtp]?i?b|bytes?)`)

// siteSize reads a size such as "14.4 GB" or "700 MB", in steps of 1024.
func siteSize(s string) int64 {
	m := sizeWords.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	n, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil {
		return 0
	}
	switch strings.ToUpper(m[2][:1]) {
	case "K":
		n *= 1 << 10
	case "M":
		n *= 1 << 20
	case "G":
		n *= 1 << 30
	case "T":
		n *= 1 << 40
	case "P":
		n *= 1 << 50
	}
	return int64(n)
}

var agoPart = regexp.MustCompile(`([\d.]+)\s*([a-z]+)`)

// timeAgo reads "40.0 minutes ago" or "1.9 weeks ago" as a time before now.
// A month counts as 30 days and a year as 365. The zero time means unknown.
func timeAgo(s string, now time.Time) time.Time {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.Contains(s, "now") {
		return now.UTC()
	}
	var ago time.Duration
	found := false
	for _, m := range agoPart.FindAllStringSubmatch(strings.NewReplacer(",", "", "ago", "", "and", "").Replace(s), -1) {
		v, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		unit := time.Duration(0)
		switch u := m[2]; {
		case strings.HasPrefix(u, "sec") || u == "s":
			unit = time.Second
		case strings.HasPrefix(u, "min") || u == "m":
			unit = time.Minute
		case strings.HasPrefix(u, "hour") || strings.HasPrefix(u, "hr") || u == "h":
			unit = time.Hour
		case strings.HasPrefix(u, "day") || u == "d":
			unit = 24 * time.Hour
		case strings.HasPrefix(u, "week") || strings.HasPrefix(u, "wk") || u == "w":
			unit = 7 * 24 * time.Hour
		case strings.HasPrefix(u, "month") || u == "mo":
			unit = 30 * 24 * time.Hour
		case strings.HasPrefix(u, "year") || u == "y":
			unit = 365 * 24 * time.Hour
		default:
			continue
		}
		ago += time.Duration(v * float64(unit))
		found = true
	}
	if !found {
		return time.Time{}
	}
	return now.Add(-ago).UTC().Truncate(time.Second)
}

// brokenAnswer is the error for a site answer HolyArrs cannot read.
func brokenAnswer(site string, err error) error {
	return &Error{Kind: KindBroken, Msg: site + " answered with something HolyArrs could not read: " + err.Error()}
}

// ErrCookie means a site's cookie or user agent is not one line of text.
var ErrCookie = errors.New("the cookie and user agent must be one line of plain text, at most 4000 characters")

// fillSignIn keeps the user agent and freeleech choice of a site that signs
// in with a browser cookie in the Source's extra, and checks the cookie
// and user agent are one line. Other sites keep neither.
func fillSignIn(in Input, src *domain.Source, site Site, key string) error {
	if !site.Private {
		delete(src.Extra, extraUserAgent)
		delete(src.Extra, extraFreeleech)
		return nil
	}
	if in.UserAgent != nil {
		src.Extra[extraUserAgent] = strings.TrimSpace(*in.UserAgent)
	}
	if in.FreeleechOnly != nil {
		src.Extra[extraFreeleech] = ""
		if *in.FreeleechOnly {
			src.Extra[extraFreeleech] = "1"
		}
	}
	for _, v := range []string{key, src.Extra[extraUserAgent]} {
		if len(v) > 4000 || strings.IndexFunc(v, func(r rune) bool { return r < ' ' || r == 0x7f }) >= 0 {
			return ErrCookie
		}
	}
	return nil
}
