package sites

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
)

// subf2m reads Subf2m, a movie and show site that answers with HTML pages and
// needs no account. A title search lists the films and show seasons it has;
// each has one page per language listing its subtitles with their release
// names. A subtitle's page carries a download button whose link sends the zip
// from the site's file host. The page shapes are our reading of the site,
// checked against trimmed copies in testdata with made-up names.
type subf2m struct{ c *client }

// subf2mFileHost is where the site's download links send the zip.
const subf2mFileHost = "https://isubcdn.com"

// subf2mLangs maps two-letter codes to the site's language path names.
var subf2mLangs = map[string]string{
	"en": "english", "fa": "farsi_persian", "ar": "arabic", "es": "spanish", "pt": "portuguese", "it": "italian", "nl": "dutch",
	"he": "hebrew", "id": "indonesian", "da": "danish", "no": "norwegian", "bn": "bengali", "bg": "bulgarian", "hr": "croatian",
	"sv": "swedish", "vi": "vietnamese", "cs": "czech", "fi": "finnish", "fr": "french", "de": "german", "el": "greek",
	"hu": "hungarian", "is": "icelandic", "ja": "japanese", "mk": "macedonian", "ms": "malay", "pl": "polish", "ro": "romanian",
	"ru": "russian", "sr": "serbian", "th": "thai", "tr": "turkish",
}

// subf2mSeasons are the season words the site writes in show titles.
var subf2mSeasons = []string{"first", "second", "third", "fourth", "fifth", "sixth", "seventh", "eighth", "ninth", "tenth", "eleventh",
	"twelfth", "thirteenth", "fourteenth", "fifteenth", "sixteenth", "seventeenth", "eighteenth", "nineteenth", "twentieth"}

var (
	subf2mYear = regexp.MustCompile(`^(.*?)\s*\((\d{4})\)$`)
	// subf2mRange reads the site's own way of naming one episode, as "S1 - 05".
	subf2mRange = regexp.MustCompile(`(?i)(?:season|s)\s*(\d{1,2})\s?[-−]\s?(\d{1,3})\b`)
)

// subf2mMaxPaths caps how many films or seasons one search opens, as Bazarr does.
const subf2mMaxPaths = 3

type subf2mResult struct {
	path, name string
	year       int
}

func (s *subf2m) Search(ctx context.Context, q Query) ([]Candidate, error) {
	var langs []string
	for _, l := range wantedLangs(q.Languages) {
		if subf2mLangs[l] != "" {
			langs = append(langs, l)
		}
	}
	if len(langs) == 0 || strings.TrimSpace(q.Title) == "" || (q.Kind == "episode" && (q.Season <= 0 || q.Episode <= 0 || q.Season > len(subf2mSeasons))) {
		return nil, nil
	}
	results, err := s.find(ctx, q.Title)
	if err != nil {
		return nil, err
	}
	var paths []subf2mResult
	for _, r := range results {
		if q.Kind == "episode" {
			show, ok := subf2mSeason(r.name, q.Season)
			if !ok || !sameName(show, q.Title) {
				continue
			}
			r.name = show
		} else if !sameName(r.name, q.Title) || (q.Year > 0 && r.year != q.Year) {
			continue
		}
		paths = append(paths, r)
	}
	if len(paths) == 0 && q.Kind != "episode" && idNumber(q.IMDb) > 0 {
		// A title the site knows under another name is still found by its IMDb id.
		if results, err = s.find(ctx, fmt.Sprintf("tt%07d", idNumber(q.IMDb))); err != nil {
			return nil, err
		}
		for _, r := range results {
			if q.Year == 0 || r.year == q.Year {
				paths = append(paths, r)
			}
		}
	}
	for _, p := range paths[:min(len(paths), subf2mMaxPaths)] {
		var out []Candidate
		for _, l := range langs {
			cs, err := s.list(ctx, p, l, q)
			if err != nil {
				return out, err
			}
			out = append(out, cs...)
		}
		if len(out) > 0 {
			return out, nil // the first film or season with subtitles is the one
		}
	}
	return nil, nil
}

// find runs the site's title search and returns the films and seasons listed.
// Names keep the season words; the trailing year is split off.
func (s *subf2m) find(ctx context.Context, query string) ([]subf2mResult, error) {
	body, err := s.c.get(ctx, "/subtitles/searchbytitle?"+url.Values{"query": {query}, "l": {""}}.Encode(), "text/html", maxSearchBody, true)
	if err != nil || body == nil {
		return nil, err
	}
	var out []subf2mResult
	seen := map[string]bool{}
	for _, div := range parsePage(body).all(tagged("div", "title")) {
		a := div.first(tagged("a", ""))
		if a == nil {
			continue
		}
		href := a.attr("href")
		if !strings.HasPrefix(href, "/subtitles/") || strings.Count(href, "/") != 2 || seen[strings.ToLower(href)] || len(href) > 200 {
			continue
		}
		seen[strings.ToLower(href)] = true
		r := subf2mResult{path: href, name: a.textOf()}
		if m := subf2mYear.FindStringSubmatch(r.name); m != nil {
			r.name = m[1]
			r.year, _ = strconv.Atoi(m[2])
		}
		out = append(out, r)
	}
	return out, nil
}

// subf2mSeason splits a show result such as "Name - Second Season" or
// "Name - Season 2" into the show name, and reports whether it is season n.
// It returns "" when it is another season or not a show season at all.
func subf2mSeason(name string, n int) (string, bool) {
	i := strings.LastIndex(name, " - ")
	if i < 0 {
		return "", false
	}
	show, part := strings.TrimSpace(name[:i]), strings.ToLower(strings.Join(strings.Fields(name[i+3:]), " "))
	ok := part == subf2mSeasons[n-1]+" season" || part == "season "+strconv.Itoa(n) || part == strconv.Itoa(n)+" season" ||
		strings.Contains(part, "complete series")
	if !ok {
		return "", false
	}
	return show, true
}

// list reads one language page of a film or season.
func (s *subf2m) list(ctx context.Context, r subf2mResult, lang string, q Query) ([]Candidate, error) {
	body, err := s.c.get(ctx, r.path+"/"+subf2mLangs[lang], "text/html", maxSearchBody, true)
	if err != nil || body == nil {
		return nil, err
	}
	page := parsePage(body)
	byID := false
	if a := page.first(tagged("a", "imdb")); a != nil {
		onPage := idNumber(a.attr("href")[strings.LastIndex(a.attr("href"), "/")+1:])
		if want := idNumber(q.IMDb); want > 0 && onPage > 0 {
			if onPage != want {
				return nil, nil // another title of the same name
			}
			byID = true
		}
	}
	var out []Candidate
	for _, li := range page.all(tagged("li", "item")) {
		dl := li.first(tagged("a", "download"))
		if dl == nil {
			continue
		}
		href := dl.attr("href")
		if !strings.HasPrefix(href, r.path+"/") || strings.Contains(href, "..") || len(href) > 300 {
			continue
		}
		c := Candidate{Site: s.c.cfg.Name, Key: strings.TrimPrefix(href, "/subtitles/"), Language: lang, Title: r.name, MatchedByID: byID,
			Format: "srt", Link: href}
		if q.Kind != "episode" {
			c.Year = r.year
		}
		if list := li.first(tagged("ul", "scrolllist")); list != nil {
			for _, x := range list.all(tagged("li", "")) {
				if t := x.textOf(); t != "" && len(t) <= 300 && len(c.ReleaseNames) < maxReleaseNames {
					c.ReleaseNames = append(c.ReleaseNames, t)
				}
			}
		}
		if q.Kind == "episode" {
			// Only subtitles for the one episode; a whole season in one archive is
			// left out, since its files would need picking apart.
			if !subf2mEpisode(c.ReleaseNames, q.Season, q.Episode) {
				continue
			}
			c.Season, c.Episode = q.Season, q.Episode
		}
		out = append(out, c)
	}
	return out, nil
}

// subf2mEpisode reports whether any release name is for that very episode.
func subf2mEpisode(names []string, season, episode int) bool {
	for _, n := range names {
		if m := subf2mRange.FindStringSubmatch(n); m != nil {
			se, _ := strconv.Atoi(m[1])
			ep, _ := strconv.Atoi(m[2])
			if se == season && ep == episode {
				return true
			}
			continue
		}
		r := releasename.Parse(n, releasename.Hints{Show: true})
		if r.Show == nil || len(r.Show.Seasons) != 1 || r.Show.Seasons[0] != season {
			continue
		}
		if slices.Contains(r.Show.Episodes, episode) {
			return true
		}
	}
	return false
}

// Download opens the subtitle's page and follows its download button. The
// site answers with a redirect to the zip on its file host.
func (s *subf2m) Download(ctx context.Context, c Candidate) ([]byte, error) {
	if !strings.HasPrefix(c.Link, "/subtitles/") || strings.Contains(c.Link, "..") {
		return nil, &Error{Kind: KindUnreadable, Msg: "the site sent a link HolyArrs cannot follow"}
	}
	body, err := s.c.get(ctx, c.Link, "text/html", maxSearchBody, false)
	if err != nil {
		return nil, err
	}
	a := parsePage(body).first(func(n *node) bool { return n.tag == "a" && n.attr("id") == "downloadButton" })
	if a == nil || !strings.HasPrefix(a.attr("href"), c.Link+"/") || strings.Contains(a.attr("href"), "..") {
		return nil, &Error{Kind: KindUnreadable, Msg: "the subtitle's page has no download link"}
	}
	return s.c.get(ctx, a.attr("href"), "", maxDownloadBody, false)
}
