package migrate

import (
	"context"
	"strings"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/subtitles"
)

// ourBazarr is a Bazarr set up the way many are: one profile of English,
// Dutch and Norwegian, six sites including the embedded one and an account
// site, and the usual general settings.
func ourBazarr(_, bazarr, _ *Snapshot) {
	bazarr.Subtitles = &SubtitleSetup{Languages: []string{"en", "nb", "nl", "nn", "no"},
		Profiles: []SubtitleProfile{{ID: 1, Name: "English + Dutch/Norwegian", Cutoff: "any",
			Languages: []SubtitleLanguage{{Language: "en"}, {Language: "nl"}, {Language: "no"}}}},
		Providers: []Provider{
			{Name: "embeddedsubtitles", Kind: "embeddedsubtitles", Enabled: true, Settings: map[string]any{"fallback_lang": "en", "hi_fallback": false, "unknown_as_fallback": false}},
			{Name: "tvsubtitles", Kind: "tvsubtitles", Enabled: true},
			{Name: "yifysubtitles", Kind: "yifysubtitles", Enabled: true},
			{Name: "subf2m", Kind: "subf2m", Enabled: true, Settings: map[string]any{"verify_ssl": true}, NeedsReentry: []string{"user_agent"}},
			{Name: "bsplayer", Kind: "bsplayer", Enabled: true},
			{Name: "opensubtitlescom", Kind: "opensubtitlescom", Enabled: true, Settings: map[string]any{"include_ai_translated": false}, NeedsReentry: []string{"password", "username"}},
		},
		Defaults: map[string]any{"movie_default_profile": int64(1), "serie_default_profile": int64(1), "movie_default_enabled": true, "serie_default_enabled": true,
			"use_embedded_subs": true, "adaptive_searching": true, "adaptive_searching_delay": "3w", "adaptive_searching_delta": "1w",
			"minimum_score": int64(90), "minimum_score_movie": int64(70), "upgrade_subs": true, "days_to_upgrade_subs": int64(7), "upgrade_frequency": int64(12),
			"upgrade_manual": true, "wanted_search_frequency": int64(6), "wanted_search_frequency_movie": int64(6), "hi_extension": "hi",
			"ignore_pgs_subs": false, "ignore_vobsub_subs": false, "utf8_encode": true, "subfolder": "current", "default_und_embedded_subtitles_lang": ""}}
}

func TestBazarrPlanMovesSitesAndSettings(t *testing.T) {
	l := newMoveLabWith(t, ourBazarr)
	p := l.plan(t)
	// Everything comes over: nb and nn are Norwegian, which the profile wants.
	for _, n := range p.NotMoved {
		if n.Group == GroupSubtitles {
			t.Errorf("not moved: %+v", n)
		}
	}
	items := map[string]Item{}
	for _, g := range p.Groups {
		for _, it := range g.Items {
			items[it.Key] = it
		}
	}
	for _, kind := range []string{"tvsubtitles", "yify", "subf2m", "bsplayer"} {
		if it, ok := items["subtitle_site:"+kind]; !ok || it.Subtitles.NeedsAccount {
			t.Errorf("site %s: %+v", kind, it)
		}
	}
	if os := items["subtitle_site:opensubtitles"]; !os.Subtitles.NeedsAccount || !strings.Contains(os.Needs, "API key") {
		t.Errorf("opensubtitles: %+v", os)
	}
	set := items["subtitles:settings"].Subtitles
	if set == nil || set.Settings == nil {
		t.Fatalf("no settings item: %+v", items)
	}
	s := set.Settings
	if *s.PassMarkEpisode != 90 || *s.PassMarkMovie != 70 || *s.AdaptiveAfterDays != 21 || *s.AdaptiveEveryDays != 7 || *s.SearchHours != 6 ||
		*s.BetterDays != 7 || *s.BetterHours != 12 || !*s.BetterByHand || !*s.UseBuiltIn || *s.IgnorePictureTracks || *s.HISuffix != "hi" ||
		s.BuiltInUnknown != nil || s.DoneWhen == nil || *s.DoneWhen != subtitles.DoneAny {
		t.Errorf("settings %+v", s)
	}
}

// The default profile's cutoff says when a title has its subtitles: any one
// language (Bazarr's 65535), one named language (Bokmål counts as
// Norwegian), or every language when it has none.
func TestBazarrPlanMovesCutoff(t *testing.T) {
	for cutoff, want := range map[string]string{"any": subtitles.DoneAny, "nb": "no", "nl": "nl", "": subtitles.DoneAll} {
		l := newMoveLabWith(t, func(a, bazarr, c *Snapshot) {
			ourBazarr(a, bazarr, c)
			bazarr.Subtitles.Profiles[0].Cutoff = cutoff
		})
		var got *string
		for _, it := range l.plan(t).Group(GroupSubtitles).Items {
			if it.Key == "subtitles:settings" {
				got = it.Subtitles.Settings.DoneWhen
			}
		}
		if got == nil || *got != want {
			t.Errorf("cutoff %q: done when %v, want %q", cutoff, got, want)
		}
	}
}

func TestBazarrMoveAppliesAndUndoes(t *testing.T) {
	l := newMoveLabWith(t, ourBazarr)
	ctx := context.Background()
	before := subtitles.ReadSettings(ctx, l.st.Read())
	mv := l.move(t, Choices{})
	if mv.State != "done" {
		t.Fatalf("move %+v", mv)
	}
	if got := lineOf(t, mv.Report, "subtitle_site:opensubtitles"); got.Result != ResCreated || got.Needs == "" {
		t.Errorf("opensubtitles line %+v", got)
	}
	sites, err := subtitles.Sites(ctx, l.st.Read())
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, s := range sites {
		states[s.Kind] = s.State
	}
	if len(sites) != 5 || states["opensubtitles"] != "disabled" || states["yify"] != "enabled" || states["bsplayer"] != "enabled" {
		t.Fatalf("sites %+v", states)
	}
	set := subtitles.ReadSettings(ctx, l.st.Read())
	var langs []string
	for _, w := range set.Languages {
		langs = append(langs, w.Language)
	}
	if strings.Join(langs, ",") != "en,nl,no" || !set.BetterByHand || set.AdaptiveAfterDays != 21 || set.PassMarkEpisode != 90 || set.DoneWhen != subtitles.DoneAny {
		t.Fatalf("settings %+v", set)
	}
	// A second move finds everything there already.
	again := l.move(t, Choices{})
	if got := lineOf(t, again.Report, "subtitles:settings"); got.Result != ResAlready {
		t.Errorf("second move %+v", got)
	}
	undone := l.undo(t, mv.ID)
	if undone.State != "undone" {
		t.Fatalf("undo %+v", undone)
	}
	after := subtitles.ReadSettings(ctx, l.st.Read())
	if after.BetterByHand != before.BetterByHand || len(after.Languages) != len(before.Languages) || after.DoneWhen != before.DoneWhen {
		t.Errorf("undo left %+v", after)
	}
	if n := l.count(t, "SELECT count(*) FROM subtitle_sites"); n != 0 {
		t.Errorf("%d sites left after undo", n)
	}
}
