package migrate

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/sources"
)

// indexerFormat is the Source format an old app's indexer kind reads as,
// "" when HolyArrs reads it only through a bridge.
func indexerFormat(ix Provider) string {
	switch strings.ToLower(ix.Kind) {
	case "torznab":
		return "torznab"
	case "newznab":
		return "newznab"
	case "torrentrssindexer":
		return "rss"
	}
	return ""
}

// prowlarrSite is the site Source a Prowlarr indexer of its own code
// becomes: Prowlarr names its IPTorrents, Knaben, SubsPlease and TorrentsCSV
// code by the site's name, which is the HolyArrs format in lower case.
func prowlarrSite(ix Provider) (sources.Site, bool) {
	return sources.SiteOf(strings.ToLower(ix.Kind))
}

// feedURL is the address the old app called: its base address with the
// API path after it. Sonarr and Radarr add the path (/api unless set
// otherwise) to the base, so a Prowlarr proxy address such as
// http://prowlarr/4/ is called as http://prowlarr/4/api. A base with no path
// stays as it is, since HolyArrs adds /api to it itself, and a base that
// already ends in the path is not given it twice.
func feedURL(base string, settings map[string]any) string {
	p := "/api"
	if v, ok := anyKey(settings, "apiPath").(string); ok {
		p = clip(v, 100)
	}
	p = "/" + strings.Trim(p, "/")
	u, err := url.Parse(base)
	if err != nil || p == "/" || strings.Trim(u.Path, "/") == "" || strings.HasSuffix(strings.TrimRight(u.Path, "/"), p) {
		return base
	}
	return strings.TrimRight(base, "/") + p
}

// prowlarrPath matches the address Prowlarr gives an app for one of its
// indexers: its own address, the indexer's number, then /api.
var prowlarrPath = regexp.MustCompile(`/(\d+)/api$`)

// prowlarrIndexerID reads the Prowlarr indexer number out of such an address.
func prowlarrIndexerID(u string) (int64, bool) {
	p, err := url.Parse(u)
	if err != nil {
		return 0, false
	}
	m := prowlarrPath.FindStringSubmatch(strings.TrimRight(p.Path, "/"))
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	return n, err == nil
}

// prowlarrProxied says an indexer of Sonarr, Radarr or Lidarr is one that
// Prowlarr put there: Prowlarr names it "<site> (Prowlarr)" and points it
// at its own address for that site.
func prowlarrProxied(name, u string) bool {
	_, ok := prowlarrIndexerID(u)
	return ok && strings.HasSuffix(strings.ToLower(strings.TrimSpace(name)), "(prowlarr)")
}

// prowlarrSources notes, before any app is planned, which Prowlarr indexers
// this move brings over as Sources, by their number, so the copies Prowlarr
// put in Sonarr, Radarr and Lidarr are not moved a second time.
func (b *builder) prowlarrSources(in []Input) {
	b.prowlarrMoved = map[int64]string{}
	for _, x := range in {
		if x.Snap == nil || x.Snap.Kind != Prowlarr {
			continue
		}
		for _, ix := range x.Snap.Indexers {
			_, site := prowlarrSite(ix)
			if ix.ID > 0 && (site || indexerFormat(ix) != "" && webURL(setStr(ix.Settings, "baseUrl", "feedUrl", "url")) != "") {
				b.prowlarrMoved[ix.ID] = clip(ix.Name, 80)
			}
		}
	}
}

// mergeSource folds a Source that another app already gave, by the same
// name and address, into the one planned: Sonarr's copy serves shows and
// Radarr's movies, so the Source serves both, with both apps' categories.
// It reports whether it did.
func (b *builder) mergeSource(app Kind, it Item) bool {
	if !b.keys[it.Key] {
		return false
	}
	g := b.items[GroupSources]
	for i := range g.Items {
		have := g.Items[i].Source
		if g.Items[i].Key != it.Key || have == nil || have.Format != it.Source.Format || have.URL != it.Source.URL {
			continue
		}
		for _, k := range []string{"movie", "tv", "music"} {
			if slices.Contains(it.Source.Kinds, k) && !slices.Contains(have.Kinds, k) {
				have.Kinds = append(have.Kinds, k)
			}
		}
		slices.SortStableFunc(have.Kinds, func(a, c string) int { return kindOrder(a) - kindOrder(c) })
		for k, cs := range it.Source.Categories {
			if have.Categories == nil {
				have.Categories = map[string][]int{}
			}
			for _, c := range cs {
				if !slices.Contains(have.Categories[k], c) {
					have.Categories[k] = append(have.Categories[k], c)
				}
			}
		}
		have.On = have.On || it.Source.On
		have.NeedsKey = have.NeedsKey || it.Source.NeedsKey
		if g.Items[i].Needs == "" {
			g.Items[i].Needs = it.Needs
		}
		have.Priority = min(have.Priority, it.Source.Priority)
		if have.MinSeeders == nil {
			have.MinSeeders = it.Source.MinSeeders
		}
		g.Items[i].Detail += "; also serves " + mediaWord(app) + " titles, as in " + string(app)
		b.p.Mapped = append(b.p.Mapped, it.From)
		return true
	}
	return false
}

func kindOrder(k string) int {
	return slices.Index([]string{"movie", "tv", "music"}, k)
}

// siteSource plans the site Source a Prowlarr indexer of one of the sites
// HolyArrs reads itself becomes. A site that signs in needs its cookie typed
// again; the user agent and freeleech choice come over.
func (b *builder) siteSource(s *Snapshot, from string, ix Provider, site sources.Site) {
	u := webURL(setStr(ix.Settings, "baseUrl"))
	if u == "" || site.Format == sources.SiteKnaben {
		// Prowlarr calls Knaben's API at its own address, whatever the site address says.
		u = site.URL
	}
	if metadataHost(u) {
		b.not(GroupSources, from, ix.Name, "a link-local or cloud metadata address; HolyArrs never reaches those")
		return
	}
	sp := &SourcePlan{Format: site.Format, Protocol: "torrent", URL: u, Kinds: slices.Clone(site.Kinds), On: ix.Enabled, Priority: 25}
	if ix.Priority >= 1 && ix.Priority <= 50 {
		sp.Priority = ix.Priority
	}
	if n, ok := setNum(ix.Settings, "torrentBaseSettings.appMinimumSeeders"); ok && n >= 0 && n < 10000 {
		v := int(n)
		sp.MinSeeders = &v
	}
	used := []string{"baseurl", "torrentbasesettings.appminimumseeders"}
	if site.SignIn {
		sp.UserAgent = clip(setStr(ix.Settings, "userAgent"), 500)
		sp.FreeleechOnly = boolean(anyKey(ix.Settings, "freeLeechOnly"))
		sp.NeedsKey = true
		used = append(used, "useragent", "freeleechonly")
	}
	it := Item{Key: "source:" + strings.ToLower(clip(ix.Name, 80)), Name: clip(ix.Name, 80), From: from, Source: sp,
		Detail: site.Label + " at " + u + ", read by HolyArrs itself; added paused: check it and resume it yourself"}
	if sp.NeedsKey {
		it.Needs = "your cookie"
		it.Detail = site.Label + " at " + u + ", read by HolyArrs itself; added paused until you paste its cookie and resume it"
	}
	for _, px := range s.IndexerProxies {
		if sharesTag(ix.Tags, px.Tags) {
			it.Detail += "; used proxy " + clip(px.Name, 80) + " there; set a proxy on this Source"
		}
	}
	b.addTagged(s.Kind, GroupSources, it, ix.Tags)
	b.leftSettings(from, ix, used...)
}
