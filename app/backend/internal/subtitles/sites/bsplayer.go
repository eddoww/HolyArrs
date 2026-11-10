package sites

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// bsplayer asks BSPlayer's subtitle service, a SOAP API without an account
// that finds subtitles by the video's file hash and size only. Every search
// signs in as an anonymous app, asks once for all languages and signs out.
// Subtitles come gzipped from the service's file host. The answer shapes are
// our reading of the API, checked against recorded replies in testdata.
type bsplayer struct{ c *client }

// Fixed parts of the BSPlayer service.
const (
	bspPath     = "/v1.php"
	bspAppID    = "HolyArrs v1"
	bspFileHost = "http://download.bsplayer-subtitles.com"
)

// bspLangs maps two-letter codes to the service's three-letter codes.
var bspLangs = map[string]string{
	"ar": "ara", "bg": "bul", "cs": "cze", "da": "dan", "de": "ger", "el": "ell", "en": "eng", "es": "spa", "fi": "fin", "fr": "fre",
	"hu": "hun", "it": "ita", "ja": "jpn", "ko": "kor", "nl": "dut", "no": "nor", "pl": "pol", "pt": "por", "ro": "rum", "ru": "rus",
	"sv": "swe", "tr": "tur", "uk": "ukr", "zh": "chi",
}

type bspItem struct {
	ID       string `xml:"subID"`
	Link     string `xml:"subDownloadLink"`
	Lang     string `xml:"subLang"`
	Name     string `xml:"subName"`
	Format   string `xml:"subFormat"`
	Season   string `xml:"season"`
	Episode  string `xml:"episode"`
	FileHash string `xml:"movieHash"`
}

// bspLogin is a sign-in answer: a status and the session handle as data.
type bspLogin struct {
	Body struct {
		Response struct {
			Return struct {
				Status string `xml:"status"`
				Data   string `xml:"data"`
			} `xml:"return"`
		} `xml:",any"`
	} `xml:"Body"`
}

// bspSearch is a search answer: a nested status and the subtitles as items.
type bspSearch struct {
	Body struct {
		Response struct {
			Return struct {
				Result struct {
					Status string `xml:"status"`
				} `xml:"result"`
				Items []bspItem `xml:"data>item"`
			} `xml:"return"`
		} `xml:",any"`
	} `xml:"Body"`
}

// call sends one SOAP call and reads the answer into out.
func (b *bsplayer) call(ctx context.Context, fn, params string, out any) error {
	raw, err := b.c.ref(bspPath)
	if err != nil {
		return err
	}
	body := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://schemas.xmlsoap.org/soap/envelope/" xmlns:SOAP-ENC="http://schemas.xmlsoap.org/soap/encoding/" ` +
		`xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema" xmlns:ns1="` + html.EscapeString(raw) + `">` +
		`<SOAP-ENV:Body SOAP-ENV:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><ns1:` + fn + `>` + params + `</ns1:` + fn + `></SOAP-ENV:Body></SOAP-ENV:Envelope>`
	hdr := map[string]string{"Content-Type": "text/xml; charset=utf-8", "SOAPAction": `"http://api.bsplayer-subtitles.com/v1.php#` + fn + `"`}
	answer, err := b.c.do(ctx, http.MethodPost, raw, []byte(body), hdr, "text/xml", maxSearchBody, false)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := xml.Unmarshal(bytes.TrimSpace(answer), out); err != nil {
		return unreadable(fn, err)
	}
	return nil
}

func xmlText(s string) string {
	var sb strings.Builder
	_ = xml.EscapeText(&sb, []byte(s))
	return sb.String()
}

func (b *bsplayer) signIn(ctx context.Context) (string, error) {
	var a bspLogin
	if err := b.call(ctx, "logIn", "<username></username><password></password><AppID>"+xmlText(bspAppID)+"</AppID>", &a); err != nil {
		return "", err
	}
	r := a.Body.Response.Return
	token := strings.TrimSpace(r.Data)
	if r.Status != "OK" || token == "" || len(token) > 200 || strings.ContainsAny(token, "<> \r\n") {
		return "", &Error{Kind: KindDown, Msg: "the service did not let HolyArrs in (" + clipText(r.Status) + ")"}
	}
	return token, nil
}

func (b *bsplayer) signOut(ctx context.Context, token string) {
	_ = b.call(context.WithoutCancel(ctx), "logOut", "<handle>"+xmlText(token)+"</handle>", nil)
}

func (b *bsplayer) Search(ctx context.Context, q Query) ([]Candidate, error) {
	var codes []string
	for _, l := range wantedLangs(q.Languages) {
		if c := bspLangs[l]; c != "" {
			codes = append(codes, c)
		}
	}
	if len(codes) == 0 {
		return nil, nil
	}
	token, err := b.signIn(ctx)
	if err != nil {
		return nil, err
	}
	defer b.signOut(ctx, token)
	if len(q.Hash) != 16 || q.Size <= 0 {
		return nil, nil // the service finds subtitles by file hash only; signing in showed it answers
	}
	imdb := "*"
	if n := idNumber(q.IMDb); n > 0 && q.Kind == "movie" {
		imdb = strconv.Itoa(n)
	}
	var a bspSearch
	if err := b.call(ctx, "searchSubtitles", "<handle>"+xmlText(token)+"</handle><movieHash>"+xmlText(strings.ToLower(q.Hash))+"</movieHash>"+
		"<movieSize>"+strconv.FormatInt(q.Size, 10)+"</movieSize><languageId>"+strings.Join(codes, ",")+"</languageId><imdbId>"+imdb+"</imdbId>", &a); err != nil {
		return nil, err
	}
	r := a.Body.Response.Return
	switch r.Result.Status {
	case "OK":
	case "Not found", "":
		return nil, nil
	default:
		return nil, &Error{Kind: KindDown, Msg: "the service answered " + clipText(r.Result.Status)}
	}
	var out []Candidate
	for _, it := range r.Items {
		lang := ""
		for two, three := range bspLangs {
			if strings.EqualFold(three, strings.TrimSpace(it.Lang)) {
				lang = two
			}
		}
		if lang == "" || !contains(wantedLangs(q.Languages), lang) || !strings.HasPrefix(it.Link, "http") || len(it.Link) > 500 ||
			!strings.EqualFold(strings.TrimSpace(it.FileHash), q.Hash) {
			continue
		}
		name := clipText(it.Name)
		c := Candidate{Site: b.c.cfg.Name, Key: clipText(it.ID), Language: lang, HashMatched: true, Format: strings.ToLower(clipText(it.Format)), Link: it.Link}
		if c.Key == "" {
			continue
		}
		if name != "" {
			c.ReleaseNames = []string{name}
			low := strings.ToLower(name)
			c.HearingImpaired = strings.Contains(low, "sdh") || strings.Contains(low, "hearing impaired")
		}
		if q.Kind == "episode" {
			c.Season, _ = strconv.Atoi(strings.TrimSpace(it.Season))
			c.Episode, _ = strconv.Atoi(strings.TrimSpace(it.Episode))
		}
		out = append(out, c)
	}
	return out, nil
}

// Download fetches the gzipped subtitle from the service's file host.
func (b *bsplayer) Download(ctx context.Context, c Candidate) ([]byte, error) {
	data, err := b.c.fetch(ctx, c.Link, maxDownloadBody)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(data, []byte{0x1F, 0x8B}) {
		return data, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, unreadable("download", err)
	}
	out, err := io.ReadAll(io.LimitReader(zr, maxDownloadBody+1))
	if err != nil {
		return nil, unreadable("download", err)
	}
	if len(out) > maxDownloadBody {
		return nil, &Error{Kind: KindUnreadable, Msg: "the subtitle unpacks too big"}
	}
	return out, nil
}

// clipText trims a value from an answer and caps its length.
func clipText(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[:300]
	}
	return strings.ToValidUTF8(s, "")
}
