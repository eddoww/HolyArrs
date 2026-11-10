package releasename

import (
	"slices"
	"testing"
)

// TestReadOldAppQuality: names read to the quality Sonarr and Radarr give
// them (each case is what Radarr or Sonarr answered for that name).
func TestReadOldAppQuality(t *testing.T) {
	for _, c := range []struct{ name, want string }{
		{"Big Buck Bunny (2008) 720p Bluray nHD x264-NhaNc3", "Bluray-720p"},
		{"Big Buck Bunny (2008) DVDRip", "DVD"},
		{"Coco 2017 2160p UHD Blu-ray HEVC TrueHD 7 1-WhiteRhino [NO RAR]", "BR-DISK"},
		{"Coco 3D 2017 MULTi COMPLETE BLURAY-SharpHD", "BR-DISK"},
		{"Coco (2017) 1080p BluRay HDR10 10Bit AC-3 TrueHD 7 1 Atmos HEVC-d3g", "Bluray-1080p"},
		{"Coco 2017 NORDiC ENG 1080p REMUX BluRay AVC DTS-HD MA TrueHD 7 1 Atmos-NorTekst", "Remux-1080p"},
		{"Oppenheimer.2023.BDRemux.1080p", "Remux-1080p"},
		{"Oppenheimer.2023.2160p.UHDRemux.Hybrid.HDR.DV-TheEqualizer", "Remux-2160p"},
		{"Coco.2017.BRRip.XViD-ETRG", "Bluray-480p"},
		{"Coco (2017) [Cartoon] BluRay H264 DolbyD 5.1 + nickarad", "Bluray-720p"},
		{"Coco 2017 m1080p BluRay x264-GeneMige", "Bluray-720p"},
		{"From S01E01 1080p HEVC x265-MeGusta", "HDTV-1080p"},
		{"The Last of Us S01E01 480p x264-mSD", "SDTV"},
		{"Coco 3D-FSBS MultiAudio 4k hevc", "HDTV-2160p"},
		{"The.Last.of.Us.S01E01.WEB.x264-TORRENTGALAXY", "WEBDL-480p"},
		{"Dune Part Two 2024 576p WEBRip x265 AAC-SSN", "WEBRip-480p"},
		{"Kaos S01E01 2160p NF WEB-DL DDP5 1 HEVC-NTb", "WEBDL-2160p"},
		{"Dune Part Two 2024 1080p HQCAM x264 AAC -HushRips", "CAM"},
		{"Dune Part Two 2024 NEW 1080p HD-TS x264 AAC -HushRips", "TELESYNC"},
		{"Coco (2017) DVD9", "DVD-R"},
		{"Coco.2017.R6.WEB-DL.1080P.H264.AAC2.0.2Audio-JBY", "WEBDL-1080p"},
		{"Rollerball 1975.mp4", "SDTV"},
		{"Notes from the Last Row S01E01 XviD-AFG", "Unknown"},
	} {
		if got := ReadOldAppQuality(c.name); got.Name != c.want {
			t.Errorf("%s: %s, want %s", c.name, got.Name, c.want)
		}
	}
}

// TestOldAppQualityNamed: Sonarr's own names read as Radarr's, with the
// parts custom format conditions test.
func TestOldAppQualityNamed(t *testing.T) {
	q, ok := OldAppQualityNamed("Bluray-2160p Remux")
	if !ok || q.Name != "Remux-2160p" || q.Source != "bluray" || q.Resolution != 2160 || q.Modifier != "remux" {
		t.Fatalf("sonarr remux %+v %v", q, ok)
	}
	if q, ok := OldAppQualityNamed("br-disk"); !ok || q.Resolution != 1080 || q.Modifier != "br_disk" {
		t.Fatalf("br-disk %+v", q)
	}
	if _, ok := OldAppQualityNamed("HDTV-480p"); ok {
		t.Fatal("a name neither app uses must not be found")
	}
}

// TestReleaseLanguages: languages read from tags, Nordic markers included;
// title words never count.
func TestReleaseLanguages(t *testing.T) {
	for _, c := range []struct {
		name     string
		want     []string
		original bool
	}{
		{"Coco.2017.NORDiC.ENG.1080p.BluRay.x264-RAPiDCOWS", []string{"da", "fi", "no", "sv", "en"}, true},
		{"Frozen.2013.NORDiC.1080p.WEB-DL.H264-NoTrace", []string{"da", "fi", "no", "sv"}, true},
		{"Troll 2022 NORWEGIAN 1080p WEBRip x264-WORLD", []string{"no"}, false},
		{"Frozen 2013 - Frost.Norsk Tale - Norwegian 720p", []string{"no"}, false},
		{"Troll.2022.DUAL-AUDIO.NOR-ENG.1080p.10bit.WEBRip.6CH.x265.HEVC-PSA", []string{"no", "en"}, false},
		{"Frozen.2013.SWEDiSH.720p.BluRay.x264-GRUNDiG", []string{"sv"}, false},
		{"Inside Out 2 2024 Bluray 2160p AV1 HDR10 EN/FR/ES OPUS 7.1-UH", []string{"en", "fr", "es"}, false},
		{"Inside.Out.2.2024.1080p.WEB-DL.ENG.LATINO.DDP5.1.Atmos.H264-BEN.THE.MEN", []string{"en", "es-419"}, false},
		{"The Danish Girl 2015 1080p BluRay x264-SPARKS", nil, false},
		{"Movie 2020 ORIGINAL 1080p WEB-DL", nil, true},
		{"Movie 2020 1080p BluRay DTS-ES x264-GRP", nil, false},
	} {
		p := Parse(c.name, Hints{})
		if !slices.Equal(p.Languages, c.want) || p.Has("original_audio") != c.original {
			t.Errorf("%s: %v original %v, want %v %v", c.name, p.Languages, p.Has("original_audio"), c.want, c.original)
		}
	}
	if p := Parse("The Danish Girl 2015 1080p BluRay x264-SPARKS", Hints{}); p.Title.Value != "The Danish Girl" {
		t.Errorf("title %q", p.Title.Value)
	}
}
