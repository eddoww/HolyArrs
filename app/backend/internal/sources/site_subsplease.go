package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// SubsPlease is a public anime site read through its JSON API. Each episode
// comes in several resolutions, one magnet link each. The query words and
// names follow Prowlarr's SubsPlease code.

type subsPleaseEntry struct {
	ReleaseDate string `json:"release_date"`
	Show        string `json:"show"`
	Episode     string `json:"episode"`
	Downloads   []struct {
		Res    string `json:"res"`
		Magnet string `json:"magnet"`
	} `json:"downloads"`
}

var (
	subsPleaseName = regexp.MustCompile(`(?i)\[?SubsPlease\]?\s*`)
	resolutionWord = regexp.MustCompile(`(?i)\d{3,4}p`)
	magnetLength   = regexp.MustCompile(`(?i)&xl=(\d+)`)
)

// subsPleaseSearch searches by show title, with S2 for a later season and
// the episode number in two digits. A movie is searched by its title only,
// since SubsPlease names movies without a year.
func subsPleaseSearch(ctx context.Context, c *Client, q Query) ([]Item, error) {
	term := siteWords(q.Title)
	if q.Music {
		return []Item{}, nil
	}
	if q.TV {
		if q.Season > 1 {
			term += " S" + strconv.Itoa(q.Season)
		}
		if n, err := strconv.Atoi(q.Episode); err == nil && n > 0 {
			term += fmt.Sprintf(" %02d", n)
		}
	}
	term = strings.TrimSpace(subsPleaseName.ReplaceAllString(term, ""))
	if m := resolutionWord.FindString(term); m != "" {
		term = strings.Join(strings.Fields(strings.Replace(term, m, "", 1)), " ")
	}
	return subsPleaseList(ctx, c, "search", url.Values{"tz": {"UTC"}, "f": {"search"}, "s": {term}}, c.cfg.Observe)
}

func subsPleaseFeed(ctx context.Context, c *Client, _ []int, observer func(string, time.Duration, error)) ([]Item, error) {
	return subsPleaseList(ctx, c, "feed", url.Values{"tz": {"UTC"}, "f": {"latest"}}, observer)
}

func subsPleaseList(ctx context.Context, c *Client, op string, v url.Values, observer func(string, time.Duration, error)) ([]Item, error) {
	body, err := c.siteGet(ctx, op, c.siteBase()+"api/?"+v.Encode(), nil, observer)
	if err != nil {
		return nil, err
	}
	return subsPleaseParse(body)
}

// subsPleaseParse reads a SubsPlease answer: an object of episodes, or an
// empty list when nothing matched. A size the magnet link does not give is
// guessed from the resolution, as Prowlarr does. Seeders are not known.
func subsPleaseParse(body []byte) ([]Item, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 || bytes.Equal(body, []byte("[]")) {
		return []Item{}, nil
	}
	var x map[string]subsPleaseEntry
	if err := json.Unmarshal(body, &x); err != nil {
		return nil, brokenAnswer("SubsPlease", err)
	}
	out := []Item{}
	for _, e := range x {
		published := time.Time{}
		if t, err := time.Parse(time.RFC1123Z, strings.TrimSpace(e.ReleaseDate)); err == nil {
			published = t.UTC()
		}
		for _, d := range e.Downloads {
			if d.Magnet == "" || e.Show == "" {
				continue
			}
			it := Item{Title: fmt.Sprintf("[SubsPlease] %s - %s (%sp)", e.Show, e.Episode, d.Res), GUID: d.Magnet, Link: d.Magnet,
				Protocol: "torrent", Published: published, Files: 1, InfoHash: magnetHash(d.Magnet), Size: subsPleaseSize(d.Magnet, d.Res)}
			out = append(out, it)
		}
	}
	// The answer is an object, so put the newest first in a fixed order.
	slices.SortFunc(out, func(a, b Item) int {
		if c := b.Published.Compare(a.Published); c != 0 {
			return c
		}
		return strings.Compare(a.Title, b.Title)
	})
	return out, nil
}

func subsPleaseSize(magnet, res string) int64 {
	if m := magnetLength.FindStringSubmatch(magnet); m != nil {
		if n, err := strconv.ParseInt(m[1], 10, 64); err == nil && n > 0 {
			return n
		}
	}
	switch res {
	case "1080":
		return 1300 << 20
	case "720":
		return 700 << 20
	case "480":
		return 350 << 20
	}
	return 1 << 30
}
