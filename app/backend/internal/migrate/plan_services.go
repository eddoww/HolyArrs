package migrate

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// sourceKinds are the media kinds a Source from an app serves.
func sourceKinds(k Kind) []string {
	switch k {
	case Radarr:
		return []string{"movie"}
	case Sonarr:
		return []string{"tv"}
	case Lidarr:
		return []string{"music"}
	}
	return []string{"movie", "tv", "music"}
}

func setStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		for mk, v := range m {
			if strings.EqualFold(mk, k) {
				if s := str(v); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func setNum(m map[string]any, keys ...string) (int64, bool) {
	for _, k := range keys {
		for mk, v := range m {
			if strings.EqualFold(mk, k) && v != nil {
				return num(v), true
			}
		}
	}
	return 0, false
}

func hasField(fields []string, names ...string) bool {
	for _, f := range fields {
		for _, n := range names {
			if strings.EqualFold(f, n) {
				return true
			}
		}
	}
	return false
}

// webURL checks an address: http or https with a host, no user part.
func webURL(raw string) string {
	raw = clip(raw, 500)
	low := strings.ToLower(raw)
	if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		return ""
	}
	rest := raw[strings.Index(raw, "://")+3:]
	host := rest
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		host = rest[:i]
	}
	if host == "" || strings.Contains(host, "@") || strings.ContainsAny(raw, " \t") {
		return ""
	}
	return raw
}

func (b *builder) indexers(s *Snapshot) {
	b.cover("indexers", "indexer_proxies")
	for i, px := range s.IndexerProxies {
		b.not(GroupSources, b.from("indexer_proxies", i), px.Name, "HolyArrs sets one proxy address per Source; set it on the Sources that used it")
	}
	for i, ix := range s.Indexers {
		from := b.from("indexers", i)
		if s.Kind == Prowlarr {
			if site, ok := prowlarrSite(ix); ok {
				b.siteSource(s, from, ix, site)
				continue
			}
		}
		format := indexerFormat(ix)
		if format == "" {
			why := "HolyArrs reads Torznab, Newznab and RSS Sources; add this one through the Sources bridge"
			if s.Kind == Prowlarr {
				why = "a Prowlarr site definition; HolyArrs reaches such sites through the Sources bridge (Settings > Sources)"
			}
			b.not(GroupSources, from, ix.Name, why)
			continue
		}
		u := webURL(setStr(ix.Settings, "baseUrl", "feedUrl", "url"))
		if u == "" {
			b.not(GroupSources, from, ix.Name, "no usable address (http or https)")
			continue
		}
		if metadataHost(u) {
			b.not(GroupSources, from, ix.Name, "a link-local or cloud metadata address; HolyArrs never reaches those")
			continue
		}
		if format != "rss" {
			u = feedURL(u, ix.Settings)
		}
		proxied := s.Kind != Prowlarr && prowlarrProxied(ix.Name, u)
		if id, ok := prowlarrIndexerID(u); proxied && ok && b.prowlarrMoved[id] != "" {
			b.not(GroupSources, from, ix.Name, "reached through Prowlarr; this move brings Prowlarr's "+b.prowlarrMoved[id]+" over as its own Source")
			continue
		}
		protocol := "torrent"
		if format == "newznab" || ix.Protocol == "usenet" {
			protocol = "usenet"
		}
		sp := &SourcePlan{Format: format, Protocol: protocol, URL: u, Kinds: sourceKinds(s.Kind), On: ix.Enabled}
		if ix.Priority >= 1 && ix.Priority <= 50 {
			sp.Priority = ix.Priority
		} else {
			sp.Priority = 25
		}
		if n, ok := setNum(ix.Settings, "minimumSeeders"); ok && n >= 0 && n < 10000 && protocol == "torrent" {
			v := int(n)
			sp.MinSeeders = &v
		}
		if cats := list(anyKey(ix.Settings, "categories")); len(cats) > 0 && len(sp.Kinds) == 1 {
			var cs []int
			for _, c := range cats {
				if n := num(c); n > 0 && n < 1000000 && len(cs) < 50 {
					cs = append(cs, int(n))
				}
			}
			if len(cs) > 0 {
				sp.Categories = map[string][]int{sp.Kinds[0]: cs}
			}
		}
		sp.NeedsKey = hasField(ix.NeedsReentry, "apiKey", "passkey", "cookie")
		it := Item{Key: "source:" + strings.ToLower(clip(ix.Name, 80)), Name: clip(ix.Name, 80), From: from, Source: sp,
			Detail: format + " at " + u}
		if proxied {
			it.Detail += " through Prowlarr; move Prowlarr too to get this site as its own Source"
		}
		if sp.NeedsKey {
			it.Needs = "your key"
			it.Detail += "; added paused until you type its key and resume it"
		} else {
			it.Detail += "; added paused: check it and resume it yourself"
		}
		// Prowlarr's indexer tags only pick apps and proxies: a proxy
		// sharing one of them was used for this indexer.
		for _, px := range s.IndexerProxies {
			if sharesTag(ix.Tags, px.Tags) {
				it.Detail += "; used proxy " + clip(px.Name, 80) + " there; set a proxy on this Source"
			}
		}
		if !b.mergeSource(s.Kind, it) {
			b.addTagged(s.Kind, GroupSources, it, ix.Tags)
		}
		b.leftSettings(from, ix, "baseurl", "feedurl", "url", "apipath", "minimumseeders", "categories")
	}
}

// leftSettings lists an indexer's settings that no Source setting takes as
// not moved. used are the lower-case names that were moved.
func (b *builder) leftSettings(from string, ix Provider, used ...string) {
	keys := make([]string, 0, len(ix.Settings))
	for k := range ix.Settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if slices.Contains(used, strings.ToLower(k)) || strings.HasPrefix(k, "link.") {
			continue
		}
		b.not(GroupSources, from+"."+k, ix.Name+": "+k, "no Source setting matches it")
	}
}

func anyKey(m map[string]any, key string) any {
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return nil
}

func hostURL(host string, port int, tls bool, base string) string {
	host = clip(host, 200)
	if host == "" || strings.ContainsAny(host, "/@?# ") {
		return ""
	}
	scheme := "http"
	if tls {
		scheme = "https"
	}
	u := scheme + "://" + host
	if port > 0 && port < 65536 {
		u += ":" + strconv.Itoa(port)
	}
	if base = strings.Trim(clip(base, 100), "/"); base != "" && !strings.Contains(base, "..") {
		u += "/" + base
	}
	return webURL(u)
}

func (b *builder) downloaders(s *Snapshot) {
	b.cover("download_clients", "downloader")
	for i, d := range s.DownloadClients {
		from := b.from("download_clients", i)
		var adapter string
		switch k := strings.ToLower(d.Kind); k {
		case "qbittorrent", "sabnzbd", "transmission", "deluge", "nzbget":
			adapter = k
		}
		if adapter == "" {
			b.not(GroupDownloaders, from, d.Name, "HolyArrs talks to qBittorrent, Transmission, Deluge, SABnzbd and NZBGet only")
			continue
		}
		port, _ := setNum(d.Settings, "port")
		tls := strings.EqualFold(setStr(d.Settings, "useSsl"), "true")
		u := hostURL(setStr(d.Settings, "host"), int(port), tls, setStr(d.Settings, "urlBase"))
		if u == "" || metadataHost(u) {
			b.not(GroupDownloaders, from, d.Name, "no usable address")
			continue
		}
		cat := setStr(d.Settings, "movieCategory", "tvCategory", "musicCategory", "category")
		// The category imported torrents move to (Post-Import Category);
		// HolyArrs moves them on qBittorrent only.
		post := clean64(setStr(d.Settings, "movieImportedCategory", "tvImportedCategory", "musicImportedCategory"))
		if post != "" && adapter != "qbittorrent" {
			b.not(GroupDownloaders, from+".post_import_category", d.Name+": "+post, "HolyArrs moves imported torrents to another category on qBittorrent only")
			post = ""
		}
		locked := hasField(d.NeedsReentry, "password", "apiKey")
		key := "downloader:" + u
		if first := b.itemByKey(GroupDownloaders, key); first != nil && b.clientSnaps[key] != 0 && b.clientSnaps[key] != b.snapID {
			b.shareClient(first, from, clean64(cat), post)
		} else if b.clientSnaps[key] == 0 {
			b.clientSnaps[key] = b.snapID
		}
		b.addDownloader(s.Kind, from, d.Name, &DownloaderPlan{Adapter: adapter, URL: u, Username: clip(setStr(d.Settings, "username"), 100),
			Category: clean64(cat), PostImportCategory: post, Priority: d.Priority, NeedsInput: locked}, d.Tags)
	}
	if d := s.Downloader; d != nil {
		adapter := map[Kind]string{QBittorrent: "qbittorrent", SABnzbd: "sabnzbd"}[s.Kind]
		from := b.from("downloader", -1)
		u := hostURL(d.Host, d.Port, d.TLS, d.URLBase)
		if u == "" || adapter == "" || metadataHost(u) {
			b.not(GroupDownloaders, from, string(s.Kind), "no usable address")
		} else {
			b.addDownloader(s.Kind, from, map[string]string{"qbittorrent": "qBittorrent", "sabnzbd": "SABnzbd"}[adapter],
				&DownloaderPlan{Adapter: adapter, URL: u, Username: clip(d.Username, 100), NeedsInput: d.NeedsPassword || adapter == "sabnzbd"}, nil)
		}
		if d.SavePath != "" || d.CompletePath != "" {
			b.not(GroupDownloaders, from+".save_path", "download folders", "the Downloader keeps its own folders; HolyArrs reads where each download lands")
		}
		for i, c := range d.Categories {
			b.not(GroupDownloaders, b.from("downloader.categories", i), c.Name, "HolyArrs uses one category of its own per Downloader (holyarrs)")
		}
		for i, sv := range d.Servers {
			b.not(GroupDownloaders, b.from("downloader.servers", i), sv.Name, "usenet servers stay set up in SABnzbd")
		}
	}
}

// HolyArrs' own categories, for a client more than one old app shares.
const (
	ownCategory         = "holyarrs"
	ownImportedCategory = "holyarrs-imported"
)

// shareClient makes the Downloader first, which another old app read in
// this move downloads through too, use HolyArrs' own categories: one
// category cannot serve both apps' titles, and HolyArrs must not take over
// their torrents while they still run. Each app's categories are listed as
// not moved.
func (b *builder) shareClient(first *Item, from, cat, post string) {
	d := first.Downloader
	why := "more than one old app downloads through this client, so HolyArrs uses its own " + ownCategory + " category"
	if d.Adapter == "qbittorrent" {
		why += " and moves imported torrents to " + ownImportedCategory
	}
	why += " and leaves theirs alone"
	if !b.sharedClients[first.Key] {
		b.sharedClients[first.Key] = true
		if w := categoryWords(d.Category, d.PostImportCategory); w != "" {
			b.not(GroupDownloaders, first.From+".category", first.Name+": "+w, why)
		}
		d.Category = ownCategory
		if d.PostImportCategory != "" {
			d.PostImportCategory = ownImportedCategory
		}
	}
	if post != "" && d.Adapter == "qbittorrent" {
		d.PostImportCategory = ownImportedCategory
	}
	if w := categoryWords(cat, post); w != "" {
		b.not(GroupDownloaders, from+".category", first.Name+": "+w, why)
	}
}

// categoryWords names a download client's category and its after-import
// category ("sonarr, then sonarr-imported").
func categoryWords(cat, post string) string {
	switch {
	case cat != "" && post != "":
		return cat + ", then " + post
	case post != "":
		return post
	}
	return cat
}

func clean64(s string) string {
	s = clip(s, 64)
	if strings.ContainsAny(s, `/\`) {
		return ""
	}
	return s
}

func (b *builder) addDownloader(app Kind, from, name string, d *DownloaderPlan, tags []string) {
	if d.Priority < 1 || d.Priority > 50 {
		d.Priority = 1
	}
	it := Item{Key: "downloader:" + d.URL, Name: clip(name, 80), From: from, Downloader: d, Detail: d.Adapter + " at " + d.URL}
	it.Detail += "; added paused: check it and resume it yourself"
	if d.NeedsInput {
		it.Needs = "your password"
		if d.Adapter == "sabnzbd" {
			it.Needs = "your key"
		}
		it.Detail = d.Adapter + " at " + d.URL + "; added paused until you type " + it.Needs + " and resume it"
	}
	b.addTagged(app, GroupDownloaders, it, tags)
}

// namingKeys maps their naming settings (lower case) to ours.
var namingKeys = map[string]string{
	"renamemovies": "rename_on_import", "renameepisodes": "rename_on_import", "renametracks": "rename_on_import",
	"colonreplacementformat": "colon", "replaceillegalcharacters": "illegal",
	"standardmovieformat": "file_pattern", "moviefolderformat": "folder_pattern",
	"seriesfolderformat": "show_folder_pattern", "seasonfolderformat": "season_folder_pattern",
	"standardepisodeformat": "episode_pattern", "dailyepisodeformat": "daily_pattern", "animeepisodeformat": "anime_pattern",
	"multiepisodestyle": "multi_episode_style",
}

// patternWords maps their pattern words to ours, longest first.
var patternWords = [][2]string{
	{"S{season:00}E{episode:00}", "{episode}"}, {"s{season:00}e{episode:00}", "{episode}"},
	{"{Movie TitleThe}", "{sort_title}"}, {"{Movie Title}", "{title}"}, {"{Movie CleanTitle}", "{title}"},
	{"{Series TitleThe}", "{sort_title}"}, {"{Series TitleYear}", "{title} ({year})"}, {"{Series Title}", "{title}"}, {"{Series CleanTitle}", "{title}"},
	{"{Release Year}", "{year}"}, {"{Series Year}", "{year}"}, {"{Quality Full}", "{quality}"}, {"{Quality Title}", "{quality}"},
	{"{Edition Tags}", "{edition}"}, {"{Release Group}", "{group}"}, {"{TmdbId}", "{tmdb}"}, {"{ImdbId}", "{imdb}"},
	{"{Original Title}", "{release}"}, {"{Episode Title}", "{episode_title}"}, {"{Episode CleanTitle}", "{episode_title}"},
	{"{Air-Date}", "{air_date}"}, {"{Air Date}", "{air_date}"}, {"{absolute:000}", "{absolute}"}, {"{absolute:00}", "{absolute}"},
	{"{season:00}", "{season_padded}"}, {"{season:0}", "{season}"},
}

// translatePattern rewrites one of their patterns in our words; ok is
// false when a word has no match.
func translatePattern(p string) (string, string, bool) {
	out := clip(p, 200)
	for _, w := range patternWords {
		out = strings.ReplaceAll(out, w[0], w[1])
	}
	// Every word left must be one of ours.
	for rest := out; ; {
		i := strings.IndexByte(rest, '{')
		if i < 0 {
			break
		}
		j := strings.IndexByte(rest[i:], '}')
		if j < 0 {
			return "", "an unclosed {", false
		}
		w := rest[i+1 : i+j]
		if !ourWord(w) {
			return "", "uses {" + clip(w, 40) + "}, which has no HolyArrs word", false
		}
		rest = rest[i+j+1:]
	}
	return out, "", true
}

var ourWords = map[string]bool{"title": true, "sort_title": true, "year": true, "quality": true, "resolution": true, "source": true,
	"edition": true, "group": true, "tmdb": true, "imdb": true, "release": true, "season": true, "episode": true,
	"episode_title": true, "air_date": true, "absolute": true, "season_padded": true}

func ourWord(w string) bool { return ourWords[w] }

func (b *builder) naming(s *Snapshot) {
	b.cover("naming")
	if len(s.Naming) == 0 {
		return
	}
	keys := make([]string, 0, len(s.Naming))
	for k := range s.Naming {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := map[string]string{}
	var froms []string
	for _, k := range keys {
		v := s.Naming[k]
		from := b.from("naming", -1) + "." + k
		ours, ok := namingKeys[strings.ToLower(k)]
		if !ok {
			b.not(GroupNaming, from, k, "no HolyArrs naming setting matches it")
			continue
		}
		switch ours {
		case "rename_on_import":
			out[ours] = strconv.FormatBool(boolean(v))
		case "illegal":
			out[ours] = map[bool]string{true: "replace", false: "remove"}[boolean(v)]
		case "colon":
			c := map[string]string{"0": "remove", "delete": "remove", "1": "dash", "dash": "dash", "2": "space_dash", "spacedash": "space_dash",
				"3": "space_dash_space", "spacedashspace": "space_dash_space", "4": "smart", "smart": "smart"}[strings.ToLower(str(v))]
			if c == "" {
				b.not(GroupNaming, from, k, "this colon choice has no HolyArrs match")
				continue
			}
			out[ours] = c
		case "multi_episode_style":
			m := map[string]string{"0": "each", "extend": "each", "1": "each_dash", "duplicate": "each_dash", "2": "each", "repeat": "each",
				"3": "each_dash", "scene": "each_dash", "4": "range", "range": "range", "5": "short_range", "prefixedrange": "range"}[strings.ToLower(str(v))]
			if m == "" {
				b.not(GroupNaming, from, k, "this style has no HolyArrs match")
				continue
			}
			out[ours] = m
		default:
			pat, why, ok := translatePattern(str(v))
			if !ok || pat == "" || strings.ContainsAny(pat, `/\`) {
				if why == "" {
					why = "it names more than one folder"
				}
				b.not(GroupNaming, from, k, why)
				continue
			}
			out[ours] = pat
		}
		froms = append(froms, k)
	}
	if len(out) == 0 {
		return
	}
	b.add(GroupNaming, Item{Key: fmt.Sprintf("naming:%s", s.Kind), Name: GroupLabels[GroupNaming] + " from " + string(s.Kind), From: b.from("naming", -1),
		Detail: fmt.Sprintf("%d settings: %s", len(out), strings.Join(froms, ", ")), Why: "only settings with a HolyArrs match; nothing on disk is renamed", Naming: out})
}

// personKey names a person from an app.
func personKey(app Kind, id int64) string { return fmt.Sprintf("person:%s:%d", app, id) }

func stablePersonKey(app Kind, u User) string {
	kind := strings.ToLower(clip(u.Kind, 20))
	external := clip(u.ExternalID, 100)
	if external == "" || kind == "" || strings.ContainsAny(kind+external, ":/\\") {
		return personKey(app, u.SourceID)
	}
	return fmt.Sprintf("person:%s:%s:%s", app, kind, external)
}

func weekly(limit, days int) *int {
	if limit <= 0 || limit > 1000 {
		return nil
	}
	if days != 7 {
		return nil
	}
	w := limit
	return &w
}

// defaultWeekly differs from a person's quota: an explicit zero in the old
// app means unlimited, while a missing value means there is no setting to move.
func defaultWeekly(settings map[string]any, kind string) *int {
	limit, ok := settings["default_"+kind+"_limit"]
	if !ok {
		return nil
	}
	n := int(num(limit))
	if n == 0 {
		return &n
	}
	return weekly(n, int(num(settings["default_"+kind+"_days"])))
}

func sourceTime(v string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
