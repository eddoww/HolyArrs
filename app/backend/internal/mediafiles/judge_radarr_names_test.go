package mediafiles

import (
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
)

// Releases a real Radarr search matched to Alien: Romulus (2024) and
// xXx (2002) find the same Title here; the packs Radarr could not match
// find none.
func TestMatchTitleRadarrSearchNames(t *testing.T) {
	lib := []TitleRef{
		{ID: 1, Name: "Alien: Romulus", Year: 2024, Kind: "movie"},
		{ID: 2, Name: "xXx", Year: 2002, Kind: "movie"},
		{ID: 3, Name: "xXx: State of the Union", Year: 2005, Kind: "movie"},
	}
	for _, c := range []struct {
		name string
		want int64
	}{
		{"Alien Romulus 2024-2160p UHD Blu-ray DV HDR10 DTS 5 1 AV1-RandH", 1},
		{"Alien Romulus-2024-1080p Bluray OPUS 7 1 AV1-WhiskeyJack", 1},
		{"Alien.Romulus.2024-COMPLETE.UHD.BLURAY-TDi", 1},
		{"Alien: Romulus (2024) [4K UHD 2160p Blu-ray]", 1},
		{"xXx-2002-1080p BluRay x265 SDR DDP 5 1 English-DarQ HONE", 2},
		{"xXx 15th Anniversary Edition 2002 1080p BluRay DTS x264-HDS", 2},
		{"XXx (2002) 15TH ANNIVERSARY EDITION REPACK 1080p BluRay 5 1-LAMA", 2},
		{"xXx Collection (2002-2017) 720p ita eng MIRCrew", 0},
		{"xXx Trilogy (2002-2017) BDRip 1080p", 0},
	} {
		m := MatchTitle(releasename.Parse(c.name, releasename.Hints{}), lib)
		var got int64
		if m.Title != nil {
			got = m.Title.ID
		}
		if got != c.want {
			t.Errorf("%s: matched Title %d, want %d (%s)", c.name, got, c.want, m.Detail)
		}
	}
}
