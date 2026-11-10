package sources

import (
	"context"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
)

// A site Source is read straight from a site's own pages or API, with no
// Jackett or Prowlarr in between. Each site is its own format. The searches
// and the reading of answers follow Prowlarr's own code for the same sites.
const (
	SiteIPTorrents  = "iptorrents"
	SiteKnaben      = "knaben"
	SiteSubsPlease  = "subsplease"
	SiteTorrentsCSV = "torrentscsv"
)

// Site is what a site Source needs that a feed does not.
type Site struct {
	Format string
	Label  string
	// URL is the address a new Source of this site uses when none is typed.
	URL string
	// SignIn says the site signs in with a browser cookie, kept as the key.
	SignIn bool
	// Feed says the site has a newest list.
	Feed bool
	// Kinds are the media kinds the site serves; a Source of it may serve fewer.
	Kinds []string
}

// siteGapMS is the least time between two calls to a site, as Prowlarr
// keeps it, whatever the Source's own limits say.
const siteGapMS = 2000

var sites = map[string]Site{
	SiteIPTorrents:  {Format: SiteIPTorrents, Label: "IPTorrents", URL: "https://iptorrents.com/", SignIn: true, Feed: true, Kinds: []string{"movie", "tv", "music"}},
	SiteKnaben:      {Format: SiteKnaben, Label: "Knaben", URL: "https://api.knaben.org/", Feed: true, Kinds: []string{"movie", "tv", "music"}},
	SiteSubsPlease:  {Format: SiteSubsPlease, Label: "SubsPlease", URL: "https://subsplease.org/", Feed: true, Kinds: []string{"movie", "tv"}},
	SiteTorrentsCSV: {Format: SiteTorrentsCSV, Label: "TorrentsCSV", URL: "https://torrents-csv.com/", Kinds: []string{"movie", "tv"}},
}

// siteSearch runs a search on a site Source.
func (c *Client) siteSearch(ctx context.Context, q Query) ([]Item, error) {
	switch c.cfg.Format {
	case SiteIPTorrents:
		return iptSearch(ctx, c, q)
	case SiteKnaben:
		return knabenSearch(ctx, c, q)
	case SiteSubsPlease:
		return subsPleaseSearch(ctx, c, q)
	case SiteTorrentsCSV:
		return torrentsCSVSearch(ctx, c, q)
	}
	return nil, &Error{Kind: KindBroken, Msg: "no such site"}
}

// siteFeed reads the newest Releases of a site Source in these categories.
func (c *Client) siteFeed(ctx context.Context, categories []int, observer func(string, time.Duration, error)) ([]Item, error) {
	switch c.cfg.Format {
	case SiteIPTorrents:
		return iptFeed(ctx, c, categories, observer)
	case SiteKnaben:
		return knabenFeed(ctx, c, categories, observer)
	case SiteSubsPlease:
		return subsPleaseFeed(ctx, c, categories, observer)
	}
	return nil, &Error{Kind: KindBroken, Msg: sites[c.cfg.Format].Label + " has no newest list"}
}

// SiteOf returns the site a format names, or false for a feed format.
func SiteOf(format string) (Site, bool) {
	s, ok := sites[format]
	return s, ok
}

// SignsIn says whether a Source signs in to its site with a cookie, so
// fetching its Release files needs the cookie and user agent too.
func SignsIn(src domain.Source) bool {
	s, ok := sites[src.Format]
	return ok && s.SignIn
}

// Errors for a site Source that cannot be saved.
var (
	ErrSiteKinds = errors.New("this site does not serve every media kind ticked")
	ErrSiteFeed  = errors.New("this site has no newest list; use it for searches only")
	ErrCookie    = errors.New("the cookie and user agent must be one line of plain text, at most 4000 characters")
)

// fillSite checks the parts of in that only a site Source has. The key is
// the sign-in cookie for a site that signs in, and nothing for the others.
func fillSite(in Input, src *domain.Source, key string) error {
	s, ok := sites[src.Format]
	if !ok {
		src.UserAgent, src.FreeleechOnly = "", false
		return nil
	}
	for _, k := range src.Kinds {
		if !slices.Contains(s.Kinds, k) {
			return fmt.Errorf("%w: %s serves %s", ErrSiteKinds, s.Label, strings.Join(s.Kinds, ", "))
		}
	}
	if !s.Feed && src.UseFor.Feed {
		if in.UseFor != nil {
			return ErrSiteFeed
		}
		src.UseFor.Feed = false
	}
	if in.UserAgent != nil {
		src.UserAgent = strings.TrimSpace(*in.UserAgent)
	}
	if in.FreeleechOnly != nil {
		src.FreeleechOnly = *in.FreeleechOnly
	}
	if !s.SignIn {
		src.UserAgent, src.FreeleechOnly = "", false
	}
	for _, v := range []string{key, src.UserAgent} {
		if len(v) > 4000 || strings.IndexFunc(v, func(r rune) bool { return r < ' ' || r == 0x7f }) >= 0 {
			return ErrCookie
		}
	}
	return nil
}

// siteDefaults fills a new site Source's address, kinds and categories when
// none were sent.
func siteDefaults(in Input, src *domain.Source) {
	s, ok := sites[src.Format]
	if !ok {
		return
	}
	if in.Kinds == nil {
		src.Kinds = slices.Clone(s.Kinds)
	}
	if in.Categories == nil {
		src.Categories = map[string][]int{}
		for _, k := range s.Kinds {
			src.Categories[k] = slices.Clone(DefaultCategories[k])
		}
	}
	if in.UseFor == nil {
		src.UseFor = domain.SourceUse{Feed: s.Feed, AutoSearch: true, HandSearch: true}
	}
}

// siteBase is the Source address with one trailing slash.
func (c *Client) siteBase() string {
	return strings.TrimRight(c.cfg.URL, "/") + "/"
}

// siteGet fetches one page or answer of a site Source and records the call.
func (c *Client) siteGet(ctx context.Context, op, raw string, body []byte, observer func(string, time.Duration, error)) ([]byte, error) {
	if err := c.wait(ctx); err != nil {
		return nil, err
	}
	start := time.Now()
	out, err := c.fetch(ctx, raw, body)
	if observer != nil && ctx.Err() == nil {
		observer(op, time.Since(start), err)
	}
	return out, err
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

// siteTerm is the text a site searches for: the movie title and year, the
// show title and episode, or the artist and album.
func siteTerm(q Query) string {
	switch {
	case q.Music:
		return siteWords(q.Artist + " " + q.Album)
	case q.TV:
		return strings.TrimSpace(siteWords(q.Title) + " " + episodeWords(q))
	}
	t := siteWords(q.Title)
	if q.Year > 0 {
		t += " " + strconv.Itoa(q.Year)
	}
	return strings.TrimSpace(t)
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

// magnetHash reads the info hash out of a magnet link, in hex.
func magnetHash(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || !strings.EqualFold(u.Scheme, "magnet") {
		return ""
	}
	for _, xt := range u.Query()["xt"] {
		v, ok := strings.CutPrefix(strings.ToLower(xt), "urn:btih:")
		if !ok {
			continue
		}
		if releasename.ValidInfoHash(v) {
			return v
		}
		if len(v) == 32 {
			if b, err := base32.StdEncoding.DecodeString(strings.ToUpper(v)); err == nil {
				return hex.EncodeToString(b)
			}
		}
	}
	return ""
}

// publicTrackers are the trackers Prowlarr puts in a magnet link it builds
// from an info hash alone.
var publicTrackers = []string{
	"http://tracker.opentrackr.org:1337/announce",
	"udp://tracker.auctor.tv:6969/announce",
	"udp://opentracker.i2p.rocks:6969/announce",
	"https://opentracker.i2p.rocks:443/announce",
	"udp://open.demonii.com:1337/announce",
	"udp://tracker.openbittorrent.com:6969/announce",
	"http://tracker.openbittorrent.com:80/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"udp://tracker.moeking.me:6969/announce",
	"udp://explodie.org:6969/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://uploads.gamecoast.net:6969/announce",
	"udp://tracker1.bt.moack.co.kr:80/announce",
	"udp://tracker.tiny-vps.com:6969/announce",
	"udp://tracker.theoks.net:6969/announce",
	"udp://tracker.skyts.net:6969/announce",
	"udp://tracker-udp.gbitt.info:80/announce",
	"udp://open.tracker.ink:6969/announce",
	"udp://movies.zsw.ca:6969/announce",
}

// hashMagnet builds a magnet link from an info hash and a name.
func hashMagnet(hash, name string) string {
	parts := []string{"xt=urn:btih:" + hash, "dn=" + url.QueryEscape(name)}
	for _, t := range publicTrackers {
		parts = append(parts, "tr="+url.QueryEscape(t))
	}
	return "magnet:?" + strings.Join(parts, "&")
}

// siteTest checks a site answers: its newest list, or a small search for a
// site without one. It returns how many Releases came back.
func (c *Client) siteTest(ctx context.Context) (int, error) {
	var items []Item
	var err error
	if sites[c.cfg.Format].Feed {
		items, err = c.siteFeed(ctx, nil, c.cfg.Observe)
	} else {
		items, err = c.siteSearch(ctx, Query{Title: "1080p"})
	}
	return len(items), err
}

// brokenAnswer is the error for a site answer HolyArrs cannot read.
func brokenAnswer(site string, err error) error {
	return &Error{Kind: KindBroken, Msg: site + " answered with something HolyArrs could not read: " + err.Error()}
}
