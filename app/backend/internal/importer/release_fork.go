package importer

import (
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/releasename"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

// byRelease matches a file that names the Title too vaguely (no year), or
// names nothing, by the Release HolyArrs sent (Target.ReleaseName, matched to
// the Title before the download), as the older apps import what they
// grabbed. A file naming another title is not. It returns the match and the
// name the file is placed under.
func (t Target) byRelease(p releasename.Result, m mediafiles.Match, ref mediafiles.TitleRef, size int64) (mediafiles.Match, releasename.Result) {
	if t.ReleaseName == "" || !(m.Title == nil && p.Title.Value == "" || m.Title != nil && confidence(m.Conf) < MinConfidence) {
		return m, p
	}
	rp := releasename.Parse(t.ReleaseName, releasename.Hints{Size: size})
	if rm := mediafiles.MatchTitle(rp, []mediafiles.TitleRef{ref}); rm.Title != nil && confidence(rm.Conf) >= MinConfidence {
		rm.Detail = "by the Release sent: " + rm.Detail
		return rm, rp
	}
	return m, p
}
