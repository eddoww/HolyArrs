package subtitles

import (
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/subtitles/sites"
)

// The fixed set of scoring cases, on Bazarr's scale: an episode scores out of
// 360 points and a movie out of 180, shown as a share of 100.
func TestScore_FixedSet(t *testing.T) {
	ep := Video{Kind: "episode", Title: "Quiet Harbour", Year: 2019, Season: 1, Episode: 2, ReleaseName: "Quiet.Harbour.S01E02.1080p.WEB.h264-GRP"}
	mv := Video{Kind: "movie", Title: "The Lantern", Year: 2010, ReleaseName: "The.Lantern.2010.1080p.BluRay.x264-GRP"}
	en := Want{Language: "en", HearingImpaired: HINormal}
	pass := DefaultSettings()
	cases := []struct {
		n       int
		v       Video
		c       sites.Candidate
		total   int
		refused string
	}{
		// Everything but a streaming service: 359 of 360.
		{1, ep, sites.Candidate{Title: "Quiet Harbour", Season: 1, Episode: 2, MatchedByID: true, ReleaseNames: []string{"Quiet.Harbour.S01E02.1080p.WEB.h264-GRP"}}, 99, ""},
		// Show, year, season, episode and hearing-impaired choice: 311 of 360.
		{2, ep, sites.Candidate{Title: "Quiet Harbour", Season: 1, Episode: 2, MatchedByID: true}, 86, ""},
		{3, ep, sites.Candidate{Title: "Quiet Harbour", Season: 1, Episode: 3, MatchedByID: true}, 0, "wrong episode"},
		{4, ep, sites.Candidate{Title: "Quiet Harbour", Season: 1, Episode: 2, MatchedByID: true, HearingImpaired: true}, 86, ""},
		// The same source family as the file lifts it over 90: 336 of 360.
		{5, ep, sites.Candidate{Title: "Quiet Harbour", Season: 1, Episode: 2, ReleaseNames: []string{"Quiet.Harbour.S01E02.720p.WEBRip.x264-OTHER"}}, 93, ""},
		{6, mv, sites.Candidate{ReleaseNames: []string{"The.Lantern.2010.1080p.BluRay.x264-GRP"}}, 99, ""},
		// Title, year, no edition on either side, codec and hearing-impaired choice: 132 of 180.
		{7, mv, sites.Candidate{ReleaseNames: []string{"The.Lantern.2010.720p.WEB.x264-OTHER"}}, 73, ""},
		{8, mv, sites.Candidate{ReleaseNames: []string{"The.Lantern.2010.1080p.BluRay.x264-OTHER"}}, 90, ""},
		{9, mv, sites.Candidate{ReleaseNames: []string{"The.Lantern.2011.1080p.BluRay.x264-GRP"}}, 0, "wrong year"},
		{10, mv, sites.Candidate{ReleaseNames: []string{"Other.Movie.2010.1080p.BluRay.x264-GRP"}}, 0, "wrong title"},
		{11, mv, sites.Candidate{Forced: true, ReleaseNames: []string{"The.Lantern.2010.1080p.BluRay.x264-GRP"}}, 0, "forced does not match the wish"},
		// A remux is a Blu-ray source.
		{12, mv, sites.Candidate{ReleaseNames: []string{"The.Lantern.2010.Remux-1080p"}}, 90, ""},
		// A file hash match backed by the episode and source stands for everything: 360 of 360.
		{13, ep, sites.Candidate{Season: 1, Episode: 2, HashMatched: true, ReleaseNames: []string{"Quiet.Harbour.S01E02.WEB.h264"}}, 100, ""},
		// A hash match the subtitle's name does not back up counts as an id match only.
		{14, mv, sites.Candidate{HashMatched: true, ReleaseNames: []string{"English (SDH).eng.srt"}}, 72, ""},
		{15, mv, sites.Candidate{HashMatched: true, ReleaseNames: []string{"lantern.bluray.x264.srt"}}, 100, ""},
	}
	var ranked []Scored
	for _, c := range cases {
		s := ScoreCandidate(c.v, en, c.c)
		if s.Total != c.total || s.Refused != c.refused {
			t.Errorf("#%d: %d %q, want %d %q (%+v)", c.n, s.Total, s.Refused, c.total, c.refused, s.Parts)
		}
		sum := 0
		for _, p := range s.Parts {
			sum += p.Points
		}
		if sum != s.Total {
			t.Errorf("#%d: parts add up to %d, total %d", c.n, sum, s.Total)
		}
		if c.v.Kind == "movie" && c.n <= 11 {
			ranked = append(ranked, Scored{Candidate: sites.Candidate{Key: string(rune('0' + c.n%10))}, Score: s})
		}
	}
	// Case 2 falls short of the episode pass mark and case 5 meets it.
	if pass.PassMark("episode") != 90 || 86 >= pass.PassMark("episode") || 93 < pass.PassMark("episode") {
		t.Fatal("episode pass mark")
	}
	Rank(ranked)
	if ranked[0].Key != "6" || ranked[1].Key != "8" || ranked[2].Key != "7" {
		t.Fatalf("order %v %v %v", ranked[0].Key, ranked[1].Key, ranked[2].Key)
	}
	over := 0
	for _, r := range ranked {
		if r.Score.Refused == "" && r.Score.Total >= pass.PassMark("movie") {
			over++
		}
	}
	if over != 3 { // 6, 8 and 7 pass, while 9, 10 and 11 are skipped
		t.Fatalf("over the pass mark: %d", over)
	}
}

func TestScore_Hints(t *testing.T) {
	mv := Video{Kind: "movie", Title: "The Lantern", Aliases: []string{"La Linterna"}, Year: 2010, ReleaseName: "x"}
	base := sites.Candidate{Title: "La Linterna", Year: 2010}
	s := ScoreCandidate(mv, Want{Language: "en", HearingImpaired: HIOnly}, base)
	if s.Total != 72 || partMatched(s, "as_wished") {
		t.Errorf("alias, not hi: %d %+v", s.Total, s.Parts)
	}
	c := base
	c.HearingImpaired = true
	if s := ScoreCandidate(mv, Want{Language: "en", HearingImpaired: HIOnly}, c); s.Total != 72 || !partMatched(s, "as_wished") {
		t.Errorf("hi: %d %+v", s.Total, s.Parts)
	}
	if s := ScoreCandidate(mv, Want{Language: "en", HearingImpaired: HIEither}, c); !partMatched(s, "as_wished") {
		t.Errorf("either: %+v", s.Parts)
	}
	// Every part a movie can score is listed; the hash only when it counted.
	if len(s.Parts) != 10 {
		t.Errorf("movie parts: %d", len(s.Parts))
	}
	// A streaming service in both names and release groups that put out the
	// same releases count.
	v := Video{Kind: "movie", Title: "The Lantern", Year: 2010, ReleaseName: "The.Lantern.2010.1080p.AMZN.WEB-DL.DDP5.1.H.264-FraMeSToR"}
	s = ScoreCandidate(v, Want{Language: "en"}, sites.Candidate{ReleaseNames: []string{"The.Lantern.2010.720p.AMZN.WEBRip.x264-W4NK3R"}})
	if !partMatched(s, "service") || !partMatched(s, "group") || !partMatched(s, "source") {
		t.Errorf("service, group, source: %+v", s.Parts)
	}
}

func partMatched(s Score, name string) bool {
	for _, p := range s.Parts {
		if p.Name == name {
			return p.Matched
		}
	}
	return false
}

func TestRank_Ties(t *testing.T) {
	cs := []Scored{
		{Candidate: sites.Candidate{Key: "a", Downloads: 5}, Score: Score{Total: 80}, Priority: 25, Order: 0},
		{Candidate: sites.Candidate{Key: "b", Trusted: true}, Score: Score{Total: 80}, Priority: 25, Order: 1},
		{Candidate: sites.Candidate{Key: "c", Downloads: 9}, Score: Score{Total: 80}, Priority: 25, Order: 2},
		{Candidate: sites.Candidate{Key: "d"}, Score: Score{Total: 80}, Priority: 1, Order: 3},
		{Candidate: sites.Candidate{Key: "e"}, Score: Score{Refused: "wrong title"}, Order: 4},
	}
	Rank(cs)
	got := ""
	for _, c := range cs {
		got += c.Key
	}
	if got != "bdcae" {
		t.Fatalf("%s", got)
	}
}

// Only the first MaxReleaseNames names of a candidate are scored, so an exact
// match beyond the cap goes unseen.
func TestScore_CapsReleaseNames(t *testing.T) {
	mv := Video{Kind: "movie", Title: "The Lantern", Year: 2010, ReleaseName: "The.Lantern.2010.1080p.BluRay.x264-GRP"}
	var names []string
	for range MaxReleaseNames {
		names = append(names, "The.Lantern.2010.720p.WEB.x264-OTHER")
	}
	far := ScoreCandidate(mv, Want{Language: "en"}, sites.Candidate{ReleaseNames: append(names, mv.ReleaseName)})
	near := ScoreCandidate(mv, Want{Language: "en"}, sites.Candidate{ReleaseNames: append([]string{mv.ReleaseName}, names...)})
	if far.Total >= near.Total {
		t.Fatalf("name past the cap was scored: far %d near %d", far.Total, near.Total)
	}
}
