package releasename

import (
	"slices"
	"testing"
)

// Names from real Radarr interactive searches that Radarr read as a movie
// and year: a year joined by hyphens, an edition before the year, a year as
// the last word and a leading [bracket]. Words that only look like a joined
// year stay in the title.
func TestRadarrReadNames(t *testing.T) {
	for _, c := range []struct {
		name    string
		title   string
		year    int
		res     int
		edition []string
		group   string
		langs   []string
	}{
		{"Alien Romulus 2024-2160p UHD Blu-ray DV HDR10 DTS 5 1 AV1-RandH", "Alien Romulus", 2024, 2160, nil, "RandH", nil},
		{"Alien Romulus-2024-1080p Bluray OPUS 7 1 AV1-WhiskeyJack", "Alien Romulus", 2024, 1080, nil, "WhiskeyJack", nil},
		{"Alien.Romulus.2024-COMPLETE.UHD.BLURAY-TDi", "Alien Romulus", 2024, 2160, nil, "TDi", nil},
		{"xXx-2002-1080p BluRay x265 SDR DDP 5 1 English-DarQ HONE", "xXx", 2002, 1080, nil, "", []string{"en"}},
		{"Oppenheimer-2023-2160p BluRay x265 HEVC 10bit HDR AAC 5 1 Tigole", "Oppenheimer", 2023, 2160, nil, "", nil},
		{"xXx 15th Anniversary Edition 2002 1080p BluRay DTS x264-HDS", "xXx", 2002, 1080, []string{"Anniversary"}, "HDS", nil},
		{"Harbor.of.Glass.Directors.Cut.2018.1080p.BluRay.x264-MOSSFOX", "Harbor of Glass", 2018, 1080, []string{"Directors Cut"}, "MOSSFOX", nil},
		{"xXx 2002 15th Anniversary Edition 1080p BluRay x264-OFT", "xXx", 2002, 1080, []string{"Anniversary"}, "OFT", nil},
		{"Coco (2017)", "Coco", 2017, 0, nil, "", nil},
		{"Rollerball 1975.mp4", "Rollerball", 1975, 0, nil, "", nil},
		{"Wonder Woman 1984 (2020) 1080p WEB-DL", "Wonder Woman 1984", 2020, 1080, nil, "", nil},
		{"[Norwegian] Troll 2022 2160p HDR10 WebRip EAC3 Will1869", "Troll", 2022, 2160, nil, "Norwegian", []string{"no"}},
		{"[English Norwegian Bokmal] Troll 2022 1080p WEB-DL HEVC x265 5 1 BONE", "Troll", 2022, 1080, nil, "", []string{"en", "no"}},
		{"[Hindi English] Oppenheimer [2023 ] 1080p 10bit [60FPS] BluRay x265 [Hindi DDP+English DTS-HDMA] 5 1 Esub [-=BlacK PearL=-]", "Oppenheimer", 2023, 1080, nil, "", nil},
		// Not a joined year: kept as title words, so no Title matches, as in Radarr.
		{"xXx Collection (2002-2017) 720p ita eng MIRCrew", "xXx Collection 2002-2017", 0, 720, nil, "", nil},
		{"Spider-Man.2002.1080p.BluRay.x264-GRP", "Spider-Man", 2002, 1080, nil, "GRP", nil},
	} {
		p := Parse(c.name, Hints{})
		if CleanName(p.Title.Value) != CleanName(c.title) || p.Year.Value != c.year || p.Resolution.Value != c.res ||
			!slices.Equal(p.Edition, c.edition) || p.Group.Value != c.group {
			t.Errorf("%q: title %q year %d resolution %d edition %v group %q; want %q %d %d %v %q",
				c.name, p.Title.Value, p.Year.Value, p.Resolution.Value, p.Edition, p.Group.Value, c.title, c.year, c.res, c.edition, c.group)
		}
		if c.langs != nil && !slices.Equal(p.Languages, c.langs) {
			t.Errorf("%q: languages %v, want %v", c.name, p.Languages, c.langs)
		}
	}
}
