//go:build livesites

package sources

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestLiveSites searches the real sites, read only, and logs what each one
// answers, to compare with Prowlarr by hand. Nothing is downloaded and
// nothing is sent to a Downloader. HOLYARRS_LIVE_SEARCHES holds one search
// per line: "movie|title|year|imdb id", "tv|title|season|episode" or
// "anime|title|season|episode". IPTorrents is asked only when
// HOLYARRS_LIVE_IPT_COOKIE (and HOLYARRS_LIVE_IPT_AGENT) are set.
func TestLiveSites(t *testing.T) {
	searches := strings.TrimSpace(os.Getenv("HOLYARRS_LIVE_SEARCHES"))
	if searches == "" {
		t.Skip("HOLYARRS_LIVE_SEARCHES is not set")
	}
	for _, line := range strings.Split(searches, "\n") {
		f := strings.Split(strings.TrimSpace(line), "|")
		for len(f) < 4 {
			f = append(f, "")
		}
		q := Query{Title: f[1]}
		cats := map[string][]int{"movie": {2000}, "tv": {5000}, "anime": {5070}}[f[0]]
		q.Categories = cats
		switch f[0] {
		case "movie":
			q.Year, _ = strconv.Atoi(f[2])
			q.IMDb = f[3]
		default:
			q.TV = true
			q.Season, _ = strconv.Atoi(f[2])
			q.Episode = f[3]
		}
		for _, format := range []string{SiteIPTorrents, SiteKnaben, SiteSubsPlease, SiteTorrentsCSV} {
			cfg := Config{URL: sites[format].URL, Format: format, Protocol: "torrent", GapMS: siteGapMS, Timeout: 30 * time.Second}
			if format == SiteIPTorrents {
				if cfg.APIKey = os.Getenv("HOLYARRS_LIVE_IPT_COOKIE"); cfg.APIKey == "" {
					continue
				}
				cfg.UserAgent = os.Getenv("HOLYARRS_LIVE_IPT_AGENT")
			}
			items, err := New(cfg).Search(context.Background(), q)
			if err != nil {
				t.Logf("%s %q: %v", format, line, err)
				continue
			}
			t.Logf("%s %q: %d Releases", format, line, len(items))
			for i, it := range items[:min(len(items), 5)] {
				seeders := -1
				if it.Seeders != nil {
					seeders = *it.Seeders
				}
				t.Logf("  %d. %s (%d MiB, %d seeders, freeleech %v, hash %q)", i+1, it.Title, it.Size>>20, seeders, it.Freeleech, it.InfoHash)
			}
		}
	}
}
