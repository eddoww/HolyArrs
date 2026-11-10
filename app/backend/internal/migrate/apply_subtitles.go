package migrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"

	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
	"github.com/HolyArrs/holyarrs/app/backend/internal/subtitles"
)

func (r *run) subtitles(it Item) (string, string) {
	sp := it.Subtitles
	switch {
	case sp.SiteKind != "":
		return r.subtitleSite(it)
	case sp.Settings != nil:
		return r.subtitleSettings(it)
	}
	cur := subtitles.ReadSettings(r.ctx, r.m.Store.Read())
	next := cur
	next.Languages = nil
	for _, w := range sp.Languages {
		next.Languages = append(next.Languages, subtitles.Want{Language: w.Language, HearingImpaired: w.HearingImpaired, Forced: w.Forced})
	}
	if err := next.Validate(); err != nil {
		return ResFailed, "the languages were not taken: " + err.Error()
	}
	if reflect.DeepEqual(next.Languages, cur.Languages) {
		return ResAlready, "these are already the default languages"
	}
	if r.dry {
		return ResChange, "default languages: " + it.Detail
	}
	before, _ := json.Marshal(cur.Languages)
	after, _ := json.Marshal(next.Languages)
	err := subtitles.SaveSettings(r.ctx, r.m.Store, next, r.by, func(tx *sql.Tx) error {
		return r.markTx(tx, it.Key, "subtitle_languages", 0, string(before), string(after))
	})
	return result(err, ResChanged, "default languages: "+it.Detail)
}

// subtitleSite adds one Subtitle site. A site that needs an account comes in
// turned off without one and waits for the person to type it.
func (r *run) subtitleSite(it Item) (string, string) {
	sp := it.Subtitles
	here, err := subtitles.Sites(r.ctx, r.m.Store.Read())
	if err != nil {
		return ResFailed, err.Error()
	}
	name := it.Name
	for _, s := range here {
		if s.Kind == sp.SiteKind {
			return ResAlready, "a subtitle site of this kind is here"
		}
		if s.Name == it.Name {
			name = clip(it.Name+" (moved)", 100)
		}
	}
	if r.dry {
		return ResCreate, "subtitle site " + sp.SiteKind
	}
	on := !sp.NeedsAccount
	_, err = subtitles.AddSite(r.ctx, r.m.Store, nil, subtitles.SiteInput{Name: name, Kind: sp.SiteKind, Enabled: &on}, r.by, r.markWith(it.Key, "subtitle_site"))
	detail := "subtitle site " + sp.SiteKind
	if sp.NeedsAccount {
		detail += ", turned off until you type its account details"
	}
	return result(err, ResCreated, detail)
}

// subtitleSettings sets the subtitle settings that came from the app. The
// mark keeps the settings as they were, for undo.
func (r *run) subtitleSettings(it Item) (string, string) {
	cur := subtitles.ReadSettings(r.ctx, r.m.Store.Read())
	next := cur
	applySubtitlePlan(&next, it.Subtitles.Settings)
	if err := next.Validate(); err != nil {
		return ResFailed, "the settings were not taken: " + err.Error()
	}
	if reflect.DeepEqual(next, cur) {
		return ResAlready, "the subtitle settings are already set this way"
	}
	if r.dry {
		return ResChange, it.Detail
	}
	before, _ := json.Marshal(cur)
	after, _ := json.Marshal(it.Subtitles.Settings)
	err := subtitles.SaveSettings(r.ctx, r.m.Store, next, r.by, func(tx *sql.Tx) error {
		return r.markTx(tx, it.Key, "subtitle_settings", 0, string(before), string(after))
	})
	return result(err, ResChanged, it.Detail)
}

// applySubtitlePlan sets every planned setting on s.
func applySubtitlePlan(s *subtitles.Settings, p *SubtitleSettingsPlan) {
	setInt := func(dst *int, v *int) {
		if v != nil {
			*dst = *v
		}
	}
	setBool := func(dst *bool, v *bool) {
		if v != nil {
			*dst = *v
		}
	}
	setInt(&s.PassMarkEpisode, p.PassMarkEpisode)
	setInt(&s.PassMarkMovie, p.PassMarkMovie)
	setInt(&s.SearchHours, p.SearchHours)
	setBool(&s.Adaptive, p.Adaptive)
	setInt(&s.AdaptiveAfterDays, p.AdaptiveAfterDays)
	setInt(&s.AdaptiveEveryDays, p.AdaptiveEveryDays)
	setBool(&s.Better, p.Better)
	setInt(&s.BetterHours, p.BetterHours)
	setInt(&s.BetterDays, p.BetterDays)
	setBool(&s.BetterByHand, p.BetterByHand)
	setBool(&s.UseBuiltIn, p.UseBuiltIn)
	setBool(&s.IgnorePictureTracks, p.IgnorePictureTracks)
	if p.BuiltInUnknown != nil {
		s.BuiltInUnknown = subtitles.NormLanguage(*p.BuiltInUnknown)
	}
	if p.HISuffix != nil {
		s.HISuffix = *p.HISuffix
	}
}

// undoSubtitleSettings puts back the settings a move changed, unless someone
// changed one of them since.
func undoSubtitleSettings(ctx context.Context, m *Mover, before, after string, by domain.Actor) (string, string) {
	var old subtitles.Settings
	var moved SubtitleSettingsPlan
	if after == "" || json.Unmarshal([]byte(before), &old) != nil || json.Unmarshal([]byte(after), &moved) != nil {
		return ResKept, keptUnknown
	}
	cur := subtitles.ReadSettings(ctx, m.Store.Read())
	same := cur
	applySubtitlePlan(&same, &moved)
	_ = same.Validate()
	if !reflect.DeepEqual(same, cur) {
		return ResKept, keptChanged
	}
	// Only the moved settings go back; the rest stay as they are now.
	back := cur
	applySubtitlePlan(&back, settingsAsPlan(old, &moved))
	if err := subtitles.SaveSettings(ctx, m.Store, back, by); err != nil {
		return ResFailed, err.Error()
	}
	return ResRemoved, "the subtitle settings are back as they were"
}

// settingsAsPlan returns the values s has for the settings p names.
func settingsAsPlan(s subtitles.Settings, p *SubtitleSettingsPlan) *SubtitleSettingsPlan {
	out := &SubtitleSettingsPlan{}
	pick := func(dst **int, has *int, v int) {
		if has != nil {
			*dst = &v
		}
	}
	pickBool := func(dst **bool, has *bool, v bool) {
		if has != nil {
			*dst = &v
		}
	}
	pick(&out.PassMarkEpisode, p.PassMarkEpisode, s.PassMarkEpisode)
	pick(&out.PassMarkMovie, p.PassMarkMovie, s.PassMarkMovie)
	pick(&out.SearchHours, p.SearchHours, s.SearchHours)
	pickBool(&out.Adaptive, p.Adaptive, s.Adaptive)
	pick(&out.AdaptiveAfterDays, p.AdaptiveAfterDays, s.AdaptiveAfterDays)
	pick(&out.AdaptiveEveryDays, p.AdaptiveEveryDays, s.AdaptiveEveryDays)
	pickBool(&out.Better, p.Better, s.Better)
	pick(&out.BetterHours, p.BetterHours, s.BetterHours)
	pick(&out.BetterDays, p.BetterDays, s.BetterDays)
	pickBool(&out.BetterByHand, p.BetterByHand, s.BetterByHand)
	pickBool(&out.UseBuiltIn, p.UseBuiltIn, s.UseBuiltIn)
	pickBool(&out.IgnorePictureTracks, p.IgnorePictureTracks, s.IgnorePictureTracks)
	if p.BuiltInUnknown != nil {
		v := s.BuiltInUnknown
		out.BuiltInUnknown = &v
	}
	if p.HISuffix != nil {
		v := s.HISuffix
		out.HISuffix = &v
	}
	return out
}
