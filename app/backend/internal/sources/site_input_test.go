package sources

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// A new site Source takes the site's address, kinds and newest list when
// none are sent, is always a torrent Source, keeps its cookie in the vault
// and shows it masked.
func TestAddSiteSourceDefaults(t *testing.T) {
	s, v := openVault(t)
	ua, free := "Mozilla/5.0 Firefox", true
	src, err := Add(context.Background(), s, v, Input{Name: "IPT", Format: SiteIPTorrents, Protocol: "usenet", APIKey: "uid=1; pass=abcdef",
		UserAgent: &ua, FreeleechOnly: &free}, domain.System)
	if err != nil {
		t.Fatal(err)
	}
	if src.URL != "https://iptorrents.com/" || src.Protocol != "torrent" || !slices.Equal(src.Kinds, []string{"movie", "tv", "music"}) ||
		!src.UseFor.Feed || !src.UseFor.AutoSearch || src.SecretRef == nil || src.UserAgent != ua || !src.FreeleechOnly ||
		src.MinSeeders == nil || *src.MinSeeders != 1 || src.Categories["tv"][0] != 5000 {
		t.Fatalf("IPTorrents %+v", src)
	}
	view := SourceViewOf(src, Summary{})
	if view.APIKey == "uid=1; pass=abcdef" || view.UserAgent != ua || !view.FreeleechOnly {
		t.Fatalf("view %+v", view)
	}
	cfg, err := ConfigFor(context.Background(), src, v)
	if err != nil || cfg.APIKey != "uid=1; pass=abcdef" || cfg.UserAgent != ua || !cfg.FreeleechOnly {
		t.Fatalf("config %+v %v", cfg, err)
	}
	csv, err := Add(context.Background(), s, v, Input{Name: "CSV", Format: SiteTorrentsCSV}, domain.System)
	if err != nil {
		t.Fatal(err)
	}
	if csv.UseFor.Feed || !csv.UseFor.HandSearch || !slices.Equal(csv.Kinds, []string{"movie", "tv"}) || csv.UserAgent != "" {
		t.Fatalf("TorrentsCSV %+v", csv)
	}
	// A public site keeps no user agent or freeleech choice.
	kn, err := Add(context.Background(), s, v, Input{Name: "Knaben", Format: SiteKnaben, UserAgent: &ua, FreeleechOnly: &free}, domain.System)
	if err != nil || kn.UserAgent != "" || kn.FreeleechOnly || kn.URL != "https://api.knaben.org/" {
		t.Fatalf("Knaben %+v %v", kn, err)
	}
}

func TestSiteSourceRefusals(t *testing.T) {
	s, v := openVault(t)
	ctx := context.Background()
	if _, err := Add(ctx, s, v, Input{Name: "CSV", Format: SiteTorrentsCSV, Kinds: []string{"movie", "music"}}, domain.System); !errors.Is(err, ErrSiteKinds) || !strings.Contains(err.Error(), "TorrentsCSV serves movie, tv") {
		t.Fatalf("kinds: %v", err)
	}
	if _, err := Add(ctx, s, v, Input{Name: "CSV", Format: SiteTorrentsCSV, UseFor: &domain.SourceUse{Feed: true, AutoSearch: true}}, domain.System); !errors.Is(err, ErrSiteFeed) {
		t.Fatalf("feed: %v", err)
	}
	if _, err := Add(ctx, s, v, Input{Name: "IPT", Format: SiteIPTorrents, APIKey: "uid=1\r\nX-Evil: 1"}, domain.System); !errors.Is(err, ErrCookie) {
		t.Fatalf("cookie: %v", err)
	}
	if _, err := Add(ctx, s, v, Input{Name: "IPT", Format: SiteIPTorrents, URL: "https://iptorrents.com/t?q=x"}, domain.System); !errors.Is(err, ErrURL) {
		t.Fatalf("address with a query: %v", err)
	}
	if _, err := Add(ctx, s, v, Input{Name: "Other", Format: "jackett", URL: "https://x.example"}, domain.System); !errors.Is(err, ErrFormat) {
		t.Fatalf("format: %v", err)
	}
}

// Changing a site Source keeps its cookie on the same host and asks for it
// again on another, like any key; a Source can change to a site and back.
func TestChangeSiteSource(t *testing.T) {
	s, v := openVault(t)
	ctx := context.Background()
	src, err := Add(ctx, s, v, Input{Name: "IPT", Format: SiteIPTorrents, APIKey: "uid=1; pass=abcdef"}, domain.System)
	if err != nil {
		t.Fatal(err)
	}
	ua := "Agent 2"
	got, err := Change(ctx, s, v, src.ID, Input{Name: "IPT", UserAgent: &ua}, domain.System)
	if err != nil || got.SecretRef == nil || got.UserAgent != ua || got.Format != SiteIPTorrents {
		t.Fatalf("change %+v %v", got, err)
	}
	if _, err := Change(ctx, s, v, src.ID, Input{Name: "IPT", URL: "https://ipt.example/"}, domain.System); !errors.Is(err, ErrKeyAgain) {
		t.Fatalf("new host kept the cookie: %v", err)
	}
	got, err = Change(ctx, s, v, src.ID, Input{Name: "IPT", Format: "torznab", URL: "https://iptorrents.com/api"}, domain.System)
	if err != nil || got.UserAgent != "" || got.Format != "torznab" {
		t.Fatalf("to torznab %+v %v", got, err)
	}
}
