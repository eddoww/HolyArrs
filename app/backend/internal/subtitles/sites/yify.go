package sites

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// yify reads YIFY Subtitles, a movie site that answers with HTML pages and
// needs no account. A movie's page is found by its IMDb id and lists one table
// row per subtitle: language, release names, a hearing-impaired mark and the
// uploader. Each subtitle's own page carries the link to its zip. The page
// shapes are our reading of the site, checked against trimmed copies in
// testdata with made-up names.
type yify struct{ c *client }

// yifyLangs maps the site's language names to two-letter codes.
var yifyLangs = map[string]string{
	"albanian": "sq", "arabic": "ar", "bengali": "bn", "brazillian portuguese": "pt", "brazilian portuguese": "pt", "bulgarian": "bg",
	"bosnian": "bs", "burmese": "my", "chinese": "zh", "chinese bg code": "zh", "big 5 code": "zh", "croatian": "hr", "czech": "cs",
	"danish": "da", "dutch": "nl", "english": "en", "estonian": "et", "farsi/persian": "fa", "finnish": "fi", "french": "fr",
	"german": "de", "greek": "el", "hebrew": "he", "hungarian": "hu", "icelandic": "is", "indonesian": "id", "italian": "it",
	"japanese": "ja", "korean": "ko", "lithuanian": "lt", "macedonian": "mk", "malay": "ms", "malayalam": "ml", "norwegian": "no",
	"polish": "pl", "portuguese": "pt", "romanian": "ro", "russian": "ru", "serbian": "sr", "slovenian": "sl", "spanish": "es",
	"swedish": "sv", "thai": "th", "turkish": "tr", "ukrainian": "uk", "urdu": "ur", "vietnamese": "vi",
}

func (y *yify) Search(ctx context.Context, q Query) ([]Candidate, error) {
	imdb := idNumber(q.IMDb)
	langs := wantedLangs(q.Languages)
	if q.Kind != "movie" || imdb == 0 || len(langs) == 0 {
		return nil, nil
	}
	body, err := y.c.get(ctx, fmt.Sprintf("/movie-imdb/tt%07d", imdb), "text/html", maxSearchBody, true)
	if err != nil || body == nil {
		return nil, err
	}
	table := parsePage(body).first(tagged("table", "other-subs"))
	if table == nil {
		return nil, nil
	}
	var out []Candidate
	for _, tr := range table.all(tagged("tr", "")) {
		tds := tr.all(tagged("td", ""))
		if len(tds) < 5 {
			continue
		}
		lang := yifyLangs[strings.ToLower(tds[1].textOf())]
		if lang == "" || !contains(langs, lang) {
			continue
		}
		a := tds[2].first(tagged("a", ""))
		if a == nil {
			continue
		}
		href := a.attr("href")
		if !strings.HasPrefix(href, "/subtitles/") || strings.Contains(href, "..") || len(href) > 300 {
			continue
		}
		c := Candidate{Site: y.c.cfg.Name, Key: strings.TrimPrefix(href, "/subtitles/"), Language: lang, MatchedByID: true,
			HearingImpaired: tds[3].first(tagged("span", "hi-subtitle")) != nil, Uploader: tds[4].textOf(), Format: "srt", Link: href}
		for _, l := range a.lines() {
			l = strings.TrimSpace(strings.TrimPrefix(l, "subtitle "))
			if l != "" && len(l) <= 300 && len(c.ReleaseNames) < maxReleaseNames {
				c.ReleaseNames = append(c.ReleaseNames, l)
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// Download opens the subtitle's page and follows its download button to the zip.
func (y *yify) Download(ctx context.Context, c Candidate) ([]byte, error) {
	if !strings.HasPrefix(c.Link, "/subtitles/") || strings.Contains(c.Link, "..") {
		return nil, &Error{Kind: KindUnreadable, Msg: "the site sent a link HolyArrs cannot follow"}
	}
	body, err := y.c.get(ctx, c.Link, "text/html", maxSearchBody, false)
	if err != nil {
		return nil, err
	}
	a := parsePage(body).first(tagged("a", "download-subtitle"))
	if a == nil || !strings.HasPrefix(a.attr("href"), "/subtitle/") || strings.Contains(a.attr("href"), "..") {
		return nil, &Error{Kind: KindUnreadable, Msg: "the subtitle's page has no download link"}
	}
	// The site refuses the zip without its subtitle page as the Referer.
	page, err := y.c.ref(c.Link)
	if err != nil {
		return nil, err
	}
	zip, err := y.c.ref(a.attr("href"))
	if err != nil {
		return nil, err
	}
	return y.c.do(ctx, http.MethodGet, zip, nil, map[string]string{"Referer": page}, "", maxDownloadBody, false)
}

// wantedLangs returns the distinct two-letter codes of a query's languages.
func wantedLangs(in []string) []string {
	var out []string
	for _, l := range in {
		if l = baseLang(l); l != "" && !contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

// maxReleaseNames caps the release names one candidate keeps.
const maxReleaseNames = 20
