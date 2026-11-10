package migrate

import (
	"net/url"
	"slices"
	"strings"
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
