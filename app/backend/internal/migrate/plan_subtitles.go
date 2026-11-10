package migrate

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/subtitles"
)

// subtitleSite is one of their subtitle site kinds with a HolyArrs kind.
type subtitleSite struct {
	kind  string
	needs string // the account details to type again, for sites that need an account
}

// subtitleSiteKinds maps Bazarr's subtitle site names to ours.
var subtitleSiteKinds = map[string]subtitleSite{
	"podnapisi":        {kind: "podnapisi"},
	"gestdown":         {kind: "gestdown"},
	"yifysubtitles":    {kind: "yify"},
	"subf2m":           {kind: "subf2m"},
	"tvsubtitles":      {kind: "tvsubtitles"},
	"bsplayer":         {kind: "bsplayer"},
	"opensubtitlescom": {kind: "opensubtitles", needs: "your own API key from the site's consumer page, your username and your password"},
	"subdl":            {kind: "subdl", needs: "your API key"},
}

func (b *builder) subtitles(s *Snapshot) {
	b.cover("subtitles")
	sub := s.Subtitles
	if sub == nil {
		return
	}
	// The default profile's languages become the default wishes.
	def := num(sub.Defaults["movie_default_profile"])
	if def == 0 {
		def = num(sub.Defaults["serie_default_profile"])
	}
	var wants []SubtitleWant
	used := -1
	for i, p := range sub.Profiles {
		if p.ID == def && def != 0 {
			used = i
			for _, l := range p.Languages {
				if code := clip(l.Language, 8); code != "" && len(wants) < 20 {
					hi := "normal"
					if l.HI {
						hi = "hi"
					}
					wants = append(wants, SubtitleWant{Language: code, HearingImpaired: hi, Forced: l.Forced})
				}
			}
		}
	}
	langFrom := b.from("subtitles.languages", -1)
	if used < 0 {
		for _, l := range sub.Languages {
			if code := clip(l, 8); code != "" && len(wants) < 20 {
				wants = append(wants, SubtitleWant{Language: code, HearingImpaired: "normal"})
			}
		}
	} else {
		langFrom = b.from("subtitles.profiles", used)
		if covered(sub.Languages, wants) {
			// Every enabled language is one the profile wants, counting Bokmål
			// and Nynorsk as Norwegian, so nothing stays behind.
			b.p.Mapped = append(b.p.Mapped, b.from("subtitles.languages", -1))
		} else if len(sub.Languages) > 0 {
			b.not(GroupSubtitles, b.from("subtitles.languages", -1), strings.Join(sub.Languages, ", "), "the default profile's languages were moved; the other enabled languages are only choices in Bazarr")
		}
	}
	if len(wants) > 0 {
		var names []string
		for _, w := range wants {
			names = append(names, w.Language)
		}
		b.add(GroupSubtitles, Item{Key: "subtitles:languages", Name: "Default subtitle languages", From: langFrom,
			Detail: strings.Join(names, ", "), Why: "new movies and shows wish for these languages", Subtitles: &SubtitlePlan{Languages: wants}})
	}
	for i, p := range sub.Profiles {
		if i == used {
			continue
		}
		b.not(GroupSubtitles, b.from("subtitles.profiles", i), p.Name, "HolyArrs has one default list of languages and a list per title; set this one on the titles that used it")
	}
	set := &SubtitleSettingsPlan{}
	if used >= 0 {
		// The default profile's cutoff says when a title has its subtitles.
		done := subtitles.DoneAll
		switch c := sub.Profiles[used].Cutoff; c {
		case "":
		case "any":
			done = subtitles.DoneAny
		default:
			if l := subtitles.NormLanguage(c); l != "" {
				done = l
			}
		}
		set.DoneWhen = &done
	}
	var moved []string
	for i, pr := range sub.Providers {
		from := b.from("subtitles.providers", i)
		if strings.EqualFold(pr.Kind, "embeddedsubtitles") {
			b.embedded(pr, from, sub.Defaults, set, &moved)
			continue
		}
		site, ok := subtitleSiteKinds[strings.ToLower(pr.Kind)]
		if !ok {
			why := "HolyArrs has no subtitle site of this kind yet"
			if strings.EqualFold(pr.Kind, "addic7ed") {
				why = "HolyArrs reaches Addic7ed subtitles through Gestdown, with no account; add Gestdown under Settings > Subtitles"
			}
			b.not(GroupSubtitles, from, pr.Name, why)
			continue
		}
		it := Item{Key: "subtitle_site:" + site.kind, Name: clip(pr.Name, 100), From: from, Detail: "subtitle site " + site.kind,
			Subtitles: &SubtitlePlan{SiteKind: site.kind}}
		if site.needs != "" {
			// Bazarr keeps the account in its own way and signs in with its own
			// app key, so the site comes over turned off and waits for yours.
			it.Subtitles.NeedsAccount = true
			it.Needs = site.needs
			it.Why = "comes over turned off until you type " + site.needs + " under Settings > Subtitles"
		}
		b.add(GroupSubtitles, it)
	}
	b.subtitleSettings(sub.Defaults, used >= 0, set, moved)
}

// covered reports whether every enabled language is a wanted one.
func covered(enabled []string, wants []SubtitleWant) bool {
	for _, l := range enabled {
		found := false
		for _, w := range wants {
			found = found || subtitles.NormLanguage(l) == subtitles.NormLanguage(w.Language)
		}
		if !found {
			return false
		}
	}
	return true
}

// embedded handles Bazarr's embedded subtitles site, which reads the tracks
// inside the video. HolyArrs counts those tracks itself, so the site becomes
// the built-in settings rather than a Subtitle site.
func (b *builder) embedded(pr Provider, from string, defaults map[string]any, set *SubtitleSettingsPlan, moved *[]string) {
	if on, ok := defaults["use_embedded_subs"].(bool); ok && !on {
		b.not(GroupSubtitles, from, pr.Name, "HolyArrs does not copy built-in tracks out to files; it counts them as there when Count subtitles inside the video is on")
		return
	}
	if unknown, _ := pr.Settings["unknown_as_fallback"].(bool); unknown {
		if l := clip(str(pr.Settings["fallback_lang"]), 8); l != "" {
			set.BuiltInUnknown = &l
		}
	}
	b.p.Mapped = append(b.p.Mapped, from)
	*moved = append(*moved, "built-in tracks count as there, as with Bazarr's embedded subtitles")
}

// subtitleSettings turns Bazarr's general settings into HolyArrs subtitle
// settings. Each one is moved or listed as not moved.
func (b *builder) subtitleSettings(d map[string]any, profileUsed bool, set *SubtitleSettingsPlan, moved []string) {
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	root := b.from("subtitles.defaults", -1)
	take := func(k string) { b.p.Mapped = append(b.p.Mapped, root+"."+k) }
	asInt := func(k string, lo, hi int) (int, bool) {
		n := int(num(d[k]))
		if _, isBool := d[k].(bool); isBool || n < lo || n > hi || (n == 0 && fmt.Sprint(d[k]) != "0") {
			return 0, false
		}
		return n, true
	}
	for _, k := range keys {
		v := d[k]
		ok := true
		switch k {
		case "movie_default_profile", "serie_default_profile":
			if ok = profileUsed; ok {
				take(k) // its languages are the default languages
			}
		case "movie_default_enabled", "serie_default_enabled":
			if ok, _ = v.(bool); ok {
				take(k) // the default languages apply to every new title
			}
		case "minimum_score", "minimum_score_movie":
			var n int
			if n, ok = asInt(k, 0, 100); ok {
				if k == "minimum_score" {
					set.PassMarkEpisode = &n
					take(k)
				} else {
					set.PassMarkMovie = &n
					take(k)
				}
			}
		case "use_embedded_subs", "adaptive_searching", "upgrade_subs", "upgrade_manual":
			var on bool
			if on, ok = v.(bool); ok {
				f := map[string]**bool{"use_embedded_subs": &set.UseBuiltIn, "adaptive_searching": &set.Adaptive, "upgrade_subs": &set.Better, "upgrade_manual": &set.BetterByHand}[k]
				*f = &on
				take(k)
			}
		case "adaptive_searching_delay", "adaptive_searching_delta":
			var days int
			if days, ok = bazarrDays(str(v)); ok {
				if k == "adaptive_searching_delay" {
					set.AdaptiveAfterDays = &days
					take(k)
				} else {
					set.AdaptiveEveryDays = &days
					take(k)
				}
			}
		case "days_to_upgrade_subs":
			var n int
			if n, ok = asInt(k, 0, 30); ok {
				set.BetterDays = &n
				take(k)
			}
		case "upgrade_frequency":
			var n int
			if n, ok = asInt(k, 6, 168); ok {
				set.BetterHours = &n
				take(k)
			}
		case "wanted_search_frequency", "wanted_search_frequency_movie":
			// HolyArrs has one interval for movies and episodes: the shorter one.
			var n int
			if n, ok = asInt(k, 1, 168); ok {
				if set.SearchHours == nil || n < *set.SearchHours {
					set.SearchHours = &n
				}
				take(k)
			}
		case "hi_extension":
			e := strings.ToLower(str(v))
			if ok = e == "hi" || e == "cc" || e == "sdh"; ok {
				set.HISuffix = &e
				take(k)
			}
		case "ignore_pgs_subs", "ignore_vobsub_subs":
			pgs, ok1 := d["ignore_pgs_subs"].(bool)
			vob, ok2 := d["ignore_vobsub_subs"].(bool)
			if ok = ok1 && ok2 && pgs == vob; ok {
				set.IgnorePictureTracks = &pgs
				take(k)
			} else {
				b.not(GroupSubtitles, root+"."+k, k, "HolyArrs leaves out all picture subtitles or none")
				continue
			}
		case "utf8_encode":
			// HolyArrs always saves subtitles as UTF-8.
			ok, _ = v.(bool)
			if ok {
				take(k)
			} else {
				b.not(GroupSubtitles, root+"."+k, k, "HolyArrs always saves subtitles as UTF-8")
				continue
			}
		case "subfolder":
			if ok = str(v) == "current" || str(v) == ""; ok {
				take(k)
			} else {
				b.not(GroupSubtitles, root+"."+k, k, "HolyArrs saves subtitles beside the video")
				continue
			}
		case "default_und_embedded_subtitles_lang":
			if l := clip(str(v), 8); l != "" {
				set.BuiltInUnknown = &l
			}
			take(k)
		default:
			ok = false
		}
		if !ok {
			b.not(GroupSubtitles, root+"."+k, k, "no HolyArrs subtitle setting matches it")
		}
	}
	if *set == (SubtitleSettingsPlan{}) {
		return
	}
	b.add(GroupSubtitles, Item{Key: "subtitles:settings", Name: "Subtitle settings", From: root, Detail: clip(strings.Join(append(moved, set.words()...), ", "), 500),
		Why: "the pass marks and search timing work as they did in Bazarr", Subtitles: &SubtitlePlan{Settings: set}})
}

// words describes the planned settings in plain words.
func (p *SubtitleSettingsPlan) words() []string {
	var out []string
	add := func(ok bool, f string, a ...any) {
		if ok {
			out = append(out, fmt.Sprintf(f, a...))
		}
	}
	add(p.PassMarkEpisode != nil, "episode pass mark %d", deref(p.PassMarkEpisode))
	add(p.PassMarkMovie != nil, "movie pass mark %d", deref(p.PassMarkMovie))
	add(p.SearchHours != nil, "missing subtitles every %d hours", deref(p.SearchHours))
	add(p.Adaptive != nil, "adaptive searching %s", onOff(derefBool(p.Adaptive)))
	add(p.AdaptiveAfterDays != nil, "slow down after %d days", deref(p.AdaptiveAfterDays))
	add(p.AdaptiveEveryDays != nil, "then every %d days", deref(p.AdaptiveEveryDays))
	add(p.Better != nil, "better subtitles %s", onOff(derefBool(p.Better)))
	add(p.BetterHours != nil, "every %d hours", deref(p.BetterHours))
	add(p.BetterDays != nil, "for %d days", deref(p.BetterDays))
	add(p.BetterByHand != nil, "hand choices too %s", onOff(derefBool(p.BetterByHand)))
	add(p.UseBuiltIn != nil, "count built-in tracks %s", onOff(derefBool(p.UseBuiltIn)))
	add(p.IgnorePictureTracks != nil, "leave out picture subtitles %s", onOff(derefBool(p.IgnorePictureTracks)))
	add(p.BuiltInUnknown != nil, "tracks without a language count as %s", derefStr(p.BuiltInUnknown))
	add(p.HISuffix != nil, "hearing-impaired files end in .%s", derefStr(p.HISuffix))
	switch d := derefStr(p.DoneWhen); {
	case p.DoneWhen == nil:
	case d == subtitles.DoneAll:
		out = append(out, "a title is done once every language is there")
	case d == subtitles.DoneAny:
		out = append(out, "a title is done once any one language is there")
	default:
		out = append(out, "a title is done once "+d+" is there")
	}
	return out
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func derefBool(p *bool) bool { return p != nil && *p }

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// bazarrDays reads Bazarr's adaptive search times, such as 3w or 5d, as days.
func bazarrDays(s string) (int, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n <= 0 {
		return 0, false
	}
	switch s[len(s)-1] {
	case 'd':
	case 'w':
		n *= 7
	default:
		return 0, false
	}
	return n, n <= 365
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
