package sources

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
)

// TorrentsCSV is a public torrent database read through its JSON search.
// It has no newest list and no categories; a search needs 3 letters or
// more. The query and answer follow Prowlarr's TorrentsCSV code.

type torrentsCSVAnswer struct {
	Torrents []*struct {
		InfoHash string `json:"infohash"`
		Name     string `json:"name"`
		Size     int64  `json:"size_bytes"`
		Created  int64  `json:"created_unix"`
		Seeders  *int   `json:"seeders"`
		Leechers *int   `json:"leechers"`
	} `json:"torrents"`
}

func torrentsCSVSearch(ctx context.Context, c *Client, q Query) ([]Item, error) {
	term := siteTerm(q)
	if len([]rune(term)) < 3 || q.Music {
		return []Item{}, nil
	}
	v := url.Values{"size": {"100"}, "q": {term}}
	body, err := c.siteGet(ctx, "search", c.siteBase()+"service/search?"+v.Encode(), nil, c.cfg.Observe)
	if err != nil {
		return nil, err
	}
	return torrentsCSVParse(body)
}

// torrentsCSVParse reads a TorrentsCSV answer, newest first. Each Release
// is a magnet link built from its info hash.
func torrentsCSVParse(body []byte) ([]Item, error) {
	var x torrentsCSVAnswer
	if err := json.Unmarshal(body, &x); err != nil {
		return nil, brokenAnswer("TorrentsCSV", err)
	}
	out := []Item{}
	for _, t := range x.Torrents {
		if t == nil {
			continue
		}
		hash, name := strings.ToLower(strings.TrimSpace(t.InfoHash)), strings.TrimSpace(t.Name)
		if !releasename.ValidInfoHash(hash) || name == "" {
			continue
		}
		seeders, leechers := 0, 0
		if t.Seeders != nil {
			seeders = *t.Seeders
		}
		if t.Leechers != nil {
			leechers = *t.Leechers
		}
		peers := seeders + leechers
		it := Item{Title: name, GUID: "magnet:?xt=urn:btih:" + hash, Link: hashMagnet(hash, name), InfoHash: hash, Size: t.Size,
			Protocol: "torrent", Seeders: &seeders, Peers: &peers}
		if t.Created > 0 {
			it.Published = time.Unix(t.Created, 0).UTC()
		}
		out = append(out, it)
	}
	slices.SortStableFunc(out, func(a, b Item) int { return b.Published.Compare(a.Published) })
	return out, nil
}
