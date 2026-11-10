package sources

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
)

// Knaben is a public search across many torrent sites, read through its
// JSON API. The Source address is the API's own (https://api.knaben.org).
// Its categories and query follow Prowlarr's Knaben code.
var knabenCategories = []siteCategory{
	{1000000, 3000}, {1001000, 3010}, {1002000, 3040}, {1003000, 3030}, {1004000, 3020}, {1005000, 3050}, {1006000, 3050},
	{2000000, 5000}, {2001000, 5040}, {2002000, 5030}, {2003000, 5045}, {2004000, 5080}, {2005000, 5020}, {2006000, 5060},
	{2007000, 5050}, {2008000, 5050},
	{3000000, 2000}, {3001000, 2040}, {3002000, 2030}, {3003000, 2045}, {3004000, 2070}, {3005000, 2010}, {3006000, 2010},
	{3007000, 2060}, {3008000, 2020},
	{6000000, 5070}, {6001000, 5070}, {6002000, 5070}, {6003000, 5070}, {6004000, 5070}, {6005000, 3020}, {6007000, 3050},
	{6008000, 5070},
}

// knabenAsk is the body of a Knaben search: the newest first, at most 100,
// and nothing its virus check flagged.
type knabenAsk struct {
	OrderBy        string `json:"order_by"`
	OrderDirection string `json:"order_direction"`
	From           int    `json:"from"`
	Size           int    `json:"size"`
	HideUnsafe     bool   `json:"hide_unsafe"`
	SearchType     string `json:"search_type,omitempty"`
	SearchField    string `json:"search_field,omitempty"`
	Query          string `json:"query,omitempty"`
	Categories     []int  `json:"categories,omitempty"`
}

type knabenAnswer struct {
	Hits []struct {
		Title     string `json:"title"`
		Hash      string `json:"hash"`
		Details   string `json:"details"`
		Link      string `json:"link"`
		MagnetURL string `json:"magnetUrl"`
		Bytes     int64  `json:"bytes"`
		Seeders   int    `json:"seeders"`
		Peers     int    `json:"peers"`
		Date      string `json:"date"`
	} `json:"hits"`
}

func knabenSearch(ctx context.Context, c *Client, q Query) ([]Item, error) {
	return knabenList(ctx, c, "search", q.Categories, siteTerm(q), c.cfg.Observe)
}

func knabenFeed(ctx context.Context, c *Client, categories []int, observer func(string, time.Duration, error)) ([]Item, error) {
	return knabenList(ctx, c, "feed", categories, "", observer)
}

func knabenList(ctx context.Context, c *Client, op string, categories []int, term string, observer func(string, time.Duration, error)) ([]Item, error) {
	ask := knabenAsk{OrderBy: "date", OrderDirection: "desc", Size: 100, HideUnsafe: true, Categories: siteCategories(knabenCategories, categories)}
	if term != "" {
		ask.SearchType, ask.SearchField, ask.Query = "100%", "title", term
	}
	body, _ := json.Marshal(ask)
	raw := strings.TrimRight(c.cfg.URL, "/")
	if !strings.HasSuffix(raw, "/v1") {
		raw += "/v1"
	}
	answer, err := c.siteGet(ctx, op, raw, body, observer)
	if err != nil {
		return nil, err
	}
	return knabenParse(answer)
}

var hasZone = regexp.MustCompile(`([+-]\d{2}:\d{2}|Z)$`)

// knabenParse reads a Knaben answer. Torrents nobody seeds are left out, as
// Prowlarr does. A magnet link is used before a link to another site, and a
// date without a zone is Knaben's own time, an hour ahead of UTC.
func knabenParse(body []byte) ([]Item, error) {
	var x knabenAnswer
	if err := json.Unmarshal(body, &x); err != nil {
		return nil, brokenAnswer("Knaben", err)
	}
	out := []Item{}
	for _, h := range x.Hits {
		if h.Seeders <= 0 {
			continue
		}
		it := Item{Title: strings.TrimSpace(h.Title), GUID: h.Details, Link: strings.TrimSpace(h.MagnetURL), Size: h.Bytes, Protocol: "torrent"}
		hash := strings.ToLower(strings.TrimSpace(h.Hash))
		if it.Link == "" {
			it.Link = strings.TrimSpace(h.Link)
		}
		if it.Link == "" && releasename.ValidInfoHash(hash) {
			it.Link = hashMagnet(hash, it.Title)
		}
		if it.Title == "" || it.Link == "" {
			continue
		}
		if releasename.ValidInfoHash(hash) {
			it.InfoHash = hash
		} else {
			it.InfoHash = magnetHash(it.Link)
		}
		if it.GUID == "" {
			it.GUID = it.Link
		}
		seeders, peers := h.Seeders, h.Seeders+h.Peers
		it.Seeders, it.Peers = &seeders, &peers
		date := strings.TrimSpace(h.Date)
		if date != "" && !hasZone.MatchString(date) {
			date += "+01:00"
		}
		if t, err := time.Parse(time.RFC3339, date); err == nil {
			it.Published = t.UTC()
		}
		out = append(out, it)
	}
	return out, nil
}
