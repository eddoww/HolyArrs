package releasename

import (
	"regexp"
	"slices"
	"strings"
)

// OldAppQuality is one quality as Sonarr and Radarr name it, with the parts
// their custom format conditions test: the source, the resolution and the
// modifier (remux, disc, raw HD, screener, regional).
type OldAppQuality struct {
	Name       string `json:"name"`
	Source     string `json:"source"` // unknown, cam, telesync, telecine, workprint, dvd, tv, web_dl, web_rip, bluray
	Resolution int    `json:"resolution"`
	Modifier   string `json:"modifier"` // none, regional, screener, raw_hd, br_disk, remux
}

// OldAppQualities are every quality Sonarr and Radarr know, in Radarr's
// names, low to high as their default lists order them. Sonarr's own names
// ("Bluray-1080p Remux") are read with OldAppQualityNamed.
var OldAppQualities = []OldAppQuality{
	{"Unknown", "unknown", 0, "none"},
	{"WORKPRINT", "workprint", 0, "none"},
	{"CAM", "cam", 0, "none"},
	{"TELESYNC", "telesync", 0, "none"},
	{"TELECINE", "telecine", 0, "none"},
	{"REGIONAL", "dvd", 480, "regional"},
	{"DVDSCR", "dvd", 480, "screener"},
	{"SDTV", "tv", 480, "none"},
	{"DVD", "dvd", 0, "none"},
	{"DVD-R", "dvd", 480, "remux"},
	{"WEBDL-480p", "web_dl", 480, "none"},
	{"WEBRip-480p", "web_rip", 480, "none"},
	{"Bluray-480p", "bluray", 480, "none"},
	{"Bluray-576p", "bluray", 576, "none"},
	{"HDTV-720p", "tv", 720, "none"},
	{"WEBDL-720p", "web_dl", 720, "none"},
	{"WEBRip-720p", "web_rip", 720, "none"},
	{"Bluray-720p", "bluray", 720, "none"},
	{"HDTV-1080p", "tv", 1080, "none"},
	{"WEBDL-1080p", "web_dl", 1080, "none"},
	{"WEBRip-1080p", "web_rip", 1080, "none"},
	{"Bluray-1080p", "bluray", 1080, "none"},
	{"Remux-1080p", "bluray", 1080, "remux"},
	{"HDTV-2160p", "tv", 2160, "none"},
	{"WEBDL-2160p", "web_dl", 2160, "none"},
	{"WEBRip-2160p", "web_rip", 2160, "none"},
	{"Bluray-2160p", "bluray", 2160, "none"},
	{"Remux-2160p", "bluray", 2160, "remux"},
	{"BR-DISK", "bluray", 1080, "br_disk"},
	{"Raw-HD", "tv", 1080, "raw_hd"},
}

// sonarrQualityNames are Sonarr's names that differ from Radarr's.
var sonarrQualityNames = map[string]string{"bluray-1080p remux": "Remux-1080p", "bluray-2160p remux": "Remux-2160p"}

// OldAppQualityNamed finds a quality by its Sonarr or Radarr name, ignoring
// case; ok is false for a name neither app uses.
func OldAppQualityNamed(name string) (OldAppQuality, bool) {
	low := strings.ToLower(strings.TrimSpace(name))
	if n, ok := sonarrQualityNames[low]; ok {
		low = strings.ToLower(n)
	}
	for _, q := range OldAppQualities {
		if strings.ToLower(q.Name) == low {
			return q, true
		}
	}
	return OldAppQuality{}, false
}

var (
	reOldWords   = regexp.MustCompile(`[^a-z0-9+]+`)
	reOldResPair = regexp.MustCompile(`^(?:\d{3,4})x(480|576|720|1080|2160)$`)
	reOldRes     = regexp.MustCompile(`^(480|576|720|1080|2160)[pi]$`)
	reOldExt     = regexp.MustCompile(`(?i)\.(mkv|mp4|avi|m4v|wmv|mov|ts|m2ts)$`)
	reOldDisc    = regexp.MustCompile(`^bd(25|50|66|100|iso|mv)$`)
)

// ReadOldAppQuality reads the quality Sonarr and Radarr give a release name.
// It follows what they answer, not HolyArrs's own reading: a name with a
// resolution and no source is a TV copy, a Blu-ray copy named with its disc
// codec and no encoder is a full disc, and "BDRip" without a resolution is
// 480p. Pick rules moved from those apps judge by it, so they keep making
// the same choices.
func ReadOldAppQuality(name string) OldAppQuality {
	q, res := readOldAppQuality(name)
	// A telesync takes the resolution the name gives, as Radarr's
	// resolution conditions see it; a CAM copy keeps none.
	if q.Name == "TELESYNC" {
		q.Resolution = res
	}
	return q
}

func readOldAppQuality(name string) (OldAppQuality, int) {
	ext := ""
	if m := reOldExt.FindStringSubmatch(name); m != nil {
		ext = strings.ToLower(m[1])
		name = name[:len(name)-len(m[0])]
	}
	low := strings.ToLower(name)
	w := slices.DeleteFunc(reOldWords.Split(low, -1), func(s string) bool { return s == "" })
	line := " " + strings.Join(w, " ") + " "
	has := func(words ...string) bool {
		for _, x := range words {
			if strings.Contains(x, " ") {
				if strings.Contains(line, " "+x+" ") {
					return true
				}
			} else if slices.Contains(w, x) {
				return true
			}
		}
		return false
	}

	res := 0
	for _, t := range w {
		if m := reOldRes.FindStringSubmatch(t); m != nil {
			res = atoi(m[1])
		} else if m := reOldResPair.FindStringSubmatch(t); m != nil {
			res = atoi(m[1])
		}
	}
	if res == 0 && has("4k", "uhd", "ultrahd") {
		res = 2160
	}
	named := func(n string) OldAppQuality { q, _ := OldAppQualityNamed(n); return q }
	byRes := func(prefix string, fallback string) OldAppQuality {
		switch res {
		case 2160:
			return named(prefix + "-2160p")
		case 1080:
			return named(prefix + "-1080p")
		case 720:
			return named(prefix + "-720p")
		case 576:
			if prefix == "Bluray" {
				return named("Bluray-576p")
			}
			return named(prefix + "-480p")
		case 480:
			return named(prefix + "-480p")
		}
		return named(fallback)
	}

	blurayWord := has("bluray", "blu ray", "bd", "bdmux", "uhdbluray")
	ripWord := has("bdrip", "brrip", "bd rip", "br rip")
	discCodec := has("avc", "hevc", "vc1", "vc 1", "mvc", "mpeg2", "mpeg 2")
	encoder := has("x264", "x265", "h264", "h265", "h 264", "h 265", "xvid", "divx", "mkv", "720p", "10bit") || ripWord ||
		(has("dvd") && !has("hd dvd")) || (res == 1080 && has("hevc"))
	explicitDisc := has("br disk", "brdisk", "3d bd", "bluray iso", "iso bluray") || slices.ContainsFunc(w, reOldDisc.MatchString) ||
		((has("complete", "disc", "disk", "iso") || hasPrefixWord(w, "complete")) && (blurayWord || has("hd dvd")))
	remux := has("remux", "bdremux", "uhdremux")

	switch {
	case remux:
		if res == 2160 {
			return named("Remux-2160p"), res
		}
		return named("Remux-1080p"), res
	case (explicitDisc || ((blurayWord || has("hd dvd")) && discCodec)) && !encoder:
		return named("BR-DISK"), res
	case has("rawhd", "raw hd"):
		return named("Raw-HD"), res
	case has("workprint", "wp"):
		return named("WORKPRINT"), res
	case has("cam", "camrip", "hdcam", "hqcam", "cam rip"):
		return named("CAM"), res
	case has("ts", "telesync", "hdts", "pdvd", "hd ts"):
		return named("TELESYNC"), res
	case has("tc", "telecine", "hdtc", "hd tc"):
		return named("TELECINE"), res
	case has("dvdscr", "scr", "screener", "dvd scr"):
		return named("DVDSCR"), res
	case has("dvd9", "dvd5", "dvdr", "dvd r", "dvdiso"):
		return named("DVD-R"), res
	case ripWord:
		return byRes("Bluray", "Bluray-480p"), res
	case blurayWord:
		return byRes("Bluray", "Bluray-720p"), res
	case has("webrip", "web rip"):
		return byRes("WEBRip", "WEBRip-480p"), res
	case has("web", "webdl", "web dl"):
		return byRes("WEBDL", "WEBDL-480p"), res
	case has("r5", "r6"):
		return named("REGIONAL"), res
	case has("hdtv", "pdtv", "sdtv", "tvrip", "dsr", "hdtvrip"):
		if res == 480 || res == 576 || res == 0 {
			return named("SDTV"), res
		}
		return byRes("HDTV", "SDTV"), res
	case has("dvdrip", "dvd", "ntsc", "pal"):
		return named("DVD"), res
	}
	switch res {
	case 2160, 1080, 720:
		return byRes("HDTV", "SDTV"), res
	case 576, 480:
		return named("SDTV"), res
	}
	switch ext {
	case "mp4", "avi", "wmv", "m4v", "mov":
		return named("SDTV"), res
	case "mkv":
		return named("HDTV-720p"), res
	}
	return named("Unknown"), res
}

// hasPrefixWord says whether a word starts with prefix and goes on
// ("completebluray").
func hasPrefixWord(w []string, prefix string) bool {
	return slices.ContainsFunc(w, func(t string) bool { return len(t) > len(prefix) && strings.HasPrefix(t, prefix) })
}
