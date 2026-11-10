package sites

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// tvsubtitles reads TVsubtitles.net, a show site that answers with HTML pages
// and needs no account. A search lists shows with their years, a show's season
// page links each episode, and an episode's page lists one box per subtitle
// with its language flag, rip and release group. The download page spells the
// zip's path out in pieces of script text. The page shapes are our reading of
// the site, checked against trimmed copies in testdata with made-up names.
type tvsubtitles struct{ c *client }

// tvsFlags maps the site's flag names to two-letter codes.
var tvsFlags = map[string]string{
	"en": "en", "fr": "fr", "es": "es", "de": "de", "it": "it", "nl": "nl", "pl": "pl", "ro": "ro", "ru": "ru", "tr": "tr",
	"hu": "hu", "gr": "el", "br": "pt", "pt": "pt", "ua": "uk", "cz": "cs", "cn": "zh", "jp": "ja", "ko": "ko", "sv": "sv",
	"da": "da", "fi": "fi", "bg": "bg", "ar": "ar", "no": "no", "he": "he", "hr": "hr", "sr": "sr", "sl": "sl", "fa": "fa",
}

var (
	// tvsShow reads a search result such as "Name (US) (2005-2013)".
	tvsShow = regexp.MustCompile(`^(.+?)(?: \(?\d{4}\)?| \((?:US|UK)\))? \((\d{4})-\d{4}\)$`)
	// tvsCountry drops a country mark from the asked name, as in "Name (US)".
	tvsCountry = regexp.MustCompile(`\s*\((?:US|UK|\d{4})\)$`)
	tvsPiece   = regexp.MustCompile(`var s\d+\s*=\s*'([^']*)';`)
	tvsNumber  = regexp.MustCompile(`^(\d+)x(\d+)$`)
)

func (t *tvsubtitles) Search(ctx context.Context, q Query) ([]Candidate, error) {
	langs := wantedLangs(q.Languages)
	if q.Kind != "episode" || q.Season <= 0 || q.Episode <= 0 || strings.TrimSpace(q.Title) == "" || len(langs) == 0 {
		return nil, nil
	}
	show, year, err := t.show(ctx, q)
	if err != nil || show == "" {
		return nil, err
	}
	body, err := t.c.get(ctx, "/"+show+"-"+strconv.Itoa(q.Season)+".html", "text/html", maxSearchBody, true)
	if err != nil || body == nil {
		return nil, err
	}
	episode := ""
	if table := parsePage(body).first(func(n *node) bool { return n.tag == "table" && n.attr("id") == "table5" }); table != nil {
		for _, tr := range table.all(tagged("tr", "")) {
			tds := tr.all(tagged("td", ""))
			if len(tds) < 2 {
				continue
			}
			m := tvsNumber.FindStringSubmatch(tds[0].textOf())
			a := tds[1].first(tagged("a", ""))
			if m == nil || a == nil || m[1] != strconv.Itoa(q.Season) || m[2] != two(q.Episode) && m[2] != strconv.Itoa(q.Episode) {
				continue
			}
			if href := strings.TrimPrefix(a.attr("href"), "/"); strings.HasPrefix(href, "episode-") && strings.HasSuffix(href, ".html") {
				episode = href
			}
			break
		}
	}
	if episode == "" {
		return nil, nil
	}
	body, err = t.c.get(ctx, "/"+episode, "text/html", maxSearchBody, true)
	if err != nil || body == nil {
		return nil, err
	}
	var out []Candidate
	for _, a := range parsePage(body).all(tagged("a", "")) {
		box := a.first(tagged("div", "subtitlen"))
		href := strings.TrimPrefix(a.attr("href"), "/")
		if box == nil || !strings.HasPrefix(href, "subtitle-") || !strings.HasSuffix(href, ".html") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(href, "subtitle-"), ".html")
		if _, err := strconv.Atoi(id); err != nil {
			continue
		}
		h5 := box.first(tagged("h5", ""))
		if h5 == nil {
			continue
		}
		img := h5.first(tagged("img", ""))
		if img == nil {
			continue
		}
		flag := strings.TrimSuffix(strings.TrimPrefix(img.attr("src"), "images/flags/"), ".gif")
		lang := tvsFlags[flag]
		if lang == "" || !contains(langs, lang) {
			continue
		}
		c := Candidate{Site: t.c.cfg.Name, Key: id, Language: lang, Title: strings.TrimSpace(tvsCountry.ReplaceAllString(q.Title, "")), Year: year,
			Season: q.Season, Episode: q.Episode, Format: "srt", Link: id}
		rip, group := "", ""
		for _, p := range box.all(tagged("p", "")) {
			switch p.attr("title") {
			case "rip":
				rip = p.textOf()
			case "release":
				group = p.textOf()
			case "downloaded":
				c.Downloads, _ = strconv.Atoi(p.textOf())
			case "author":
				c.Uploader = p.textOf()
			}
		}
		// The release reads as a name the scorer understands, as in
		// "Show 1x08 WEB-NTb".
		if name := strings.TrimSpace(c.Title + " " + strconv.Itoa(q.Season) + "x" + two(q.Episode) + " " + rip + "-" + group); rip != "" || group != "" {
			c.ReleaseNames = []string{strings.TrimSuffix(name, "-")}
		}
		out = append(out, c)
	}
	return out, nil
}

// show finds the site's show page for the query and the show's first year.
func (t *tvsubtitles) show(ctx context.Context, q Query) (string, int, error) {
	name := strings.TrimSpace(tvsCountry.ReplaceAllString(q.Title, ""))
	body, err := t.c.post(ctx, "/search2.php", url.Values{"qs": {name}}, "text/html", maxSearchBody)
	if err != nil {
		return "", 0, err
	}
	var named []string
	var years []int
	for _, a := range parsePage(body).all(tagged("a", "")) {
		href := strings.TrimPrefix(a.attr("href"), "/")
		if !strings.HasPrefix(href, "tvshow-") || !strings.HasSuffix(href, ".html") {
			continue
		}
		m := tvsShow.FindStringSubmatch(a.textOf())
		if m == nil || !sameName(m[1], name) {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(href, "tvshow-"), ".html")
		if _, err := strconv.Atoi(id); err != nil {
			continue
		}
		year, _ := strconv.Atoi(m[2])
		if q.Year > 0 && year == q.Year {
			return "tvshow-" + id, year, nil
		}
		named, years = append(named, "tvshow-"+id), append(years, year)
	}
	// Without a show of the same first year, one show of that name is taken;
	// its year then says nothing about the match.
	if len(named) == 1 && q.Year == 0 {
		return named[0], years[0], nil
	}
	if len(named) == 1 {
		return named[0], 0, nil
	}
	return "", 0, nil
}

// Download reads the download page's script for the zip's path and fetches it.
func (t *tvsubtitles) Download(ctx context.Context, c Candidate) ([]byte, error) {
	if _, err := strconv.Atoi(c.Link); err != nil {
		return nil, &Error{Kind: KindUnreadable, Msg: "the site sent a link HolyArrs cannot follow"}
	}
	body, err := t.c.get(ctx, "/download-"+c.Link+".html", "text/html", maxSearchBody, false)
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	for _, m := range tvsPiece.FindAllSubmatch(body, 20) {
		sb.Write(m[1])
	}
	p := sb.String()
	if !strings.HasPrefix(p, "files/") || strings.Contains(p, "..") || len(p) > 300 {
		return nil, &Error{Kind: KindUnreadable, Msg: "the download page has no link HolyArrs can follow"}
	}
	return t.c.get(ctx, "/"+(&url.URL{Path: p}).EscapedPath(), "", maxDownloadBody, false)
}
