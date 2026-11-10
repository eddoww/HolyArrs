package migrate

import (
	"fmt"
	"slices"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/importer"
)

// handlingFrom is one app's download handling settings, gathered so apps
// that disagree are seen once all are read.
type handlingFrom struct {
	from string
	h    map[string]any
}

// downloadHandling gathers an app's download handling settings and lists
// the ones HolyArrs always does its own way.
func (b *builder) downloadHandling(s *Snapshot) {
	b.cover("download_handling")
	h := s.DownloadHandling
	if len(h) == 0 {
		return
	}
	from := b.from("download_handling", -1)
	b.handling = append(b.handling, handlingFrom{from: from, h: h})
	left := map[string]bool{}
	if v := strings.ToLower(str(h["downloadpropersandrepacks"])); v != "" && v != "preferandupgrade" {
		left["downloadpropersandrepacks"] = true
		b.not(GroupDownloaders, from+".downloadpropersandrepacks", "propers and repacks: "+v,
			"HolyArrs sets this per Pick rules preset (Fixed copies); presets start on take any, which is prefer and upgrade")
	}
	always := []struct{ key, what, why string }{
		{"copyusinghardlinks", "use hardlinks off", "HolyArrs always hardlinks when the download and the Library are on one disk, and copies otherwise"},
		{"enablecompleteddownloadhandling", "completed download handling off", "HolyArrs always imports finished downloads"},
		{"autoredownloadfailed", "redownload failed off", "HolyArrs always looks for another Release after a failed download"},
		{"autoredownloadfailedfrominteractivesearch", "redownload failed from a manual choice off", "HolyArrs always looks for another Release after a failed download, also one picked by hand"},
	}
	for _, a := range always {
		if v, ok := h[a.key]; ok && !boolean(v) {
			left[a.key] = true
			b.not(GroupDownloaders, from+"."+a.key, a.what, a.why)
		}
	}
	// The rest moves, or matches what HolyArrs always does.
	for _, k := range handlingKeys {
		if _, ok := h[k]; ok && !left[k] {
			b.p.Mapped = append(b.p.Mapped, from+"."+k)
		}
	}
}

// handlingItem plans one Download handling item from every app's settings.
// Where apps disagree the stricter one wins: more free space kept, the
// free-space check on, extra files on (every app's types), the shorter
// feed interval.
func (b *builder) handlingItem() {
	if len(b.handling) == 0 {
		return
	}
	p := &DownloadHandlingPlan{}
	for _, x := range b.handling {
		h := x.h
		if v, ok := h["minimumfreespacewhenimporting"]; ok {
			n := int(min(max(num(v), 100), 1<<20))
			if p.MinFreeMB == nil || n > *p.MinFreeMB {
				p.MinFreeMB = &n
			}
		}
		if v, ok := h["skipfreespacecheckwhenimporting"]; ok {
			skip := boolean(v)
			if p.SkipFreeSpaceCheck == nil || !skip {
				p.SkipFreeSpaceCheck = &skip
			}
		}
		if v, ok := h["importextrafiles"]; ok {
			on := boolean(v)
			if p.ImportExtraFiles == nil || on {
				p.ImportExtraFiles = &on
			}
			for _, e := range importer.UsableExtraExtensions(str(h["extrafileextensions"])) {
				if on && !slices.Contains(p.ExtraFileExtensions, e) && len(p.ExtraFileExtensions) < 20 {
					p.ExtraFileExtensions = append(p.ExtraFileExtensions, e)
				}
			}
		}
		if v, ok := h["rsssyncinterval"]; ok {
			m := int(num(v))
			if m != 0 {
				m = min(max(m, 10), 120)
			}
			if p.FeedMinutes == nil || m != 0 && (*p.FeedMinutes == 0 || m < *p.FeedMinutes) {
				p.FeedMinutes = &m
			}
		}
	}
	var said []string
	if p.MinFreeMB != nil {
		said = append(said, fmt.Sprintf("keep %d MB free when importing", *p.MinFreeMB))
	}
	if p.SkipFreeSpaceCheck != nil && *p.SkipFreeSpaceCheck {
		said = append(said, "skip the free-space check")
	}
	switch {
	case p.ImportExtraFiles == nil:
	case *p.ImportExtraFiles && len(p.ExtraFileExtensions) > 0:
		said = append(said, "bring "+strings.Join(p.ExtraFileExtensions, ", ")+" files along")
	case *p.ImportExtraFiles:
		said = append(said, "bring extra files along")
	default:
		said = append(said, "leave extra files behind")
	}
	switch {
	case p.FeedMinutes == nil:
	case *p.FeedMinutes == 0:
		said = append(said, "feed check off")
	default:
		said = append(said, fmt.Sprintf("check the feeds every %d minutes", *p.FeedMinutes))
	}
	if len(said) == 0 {
		return
	}
	b.add(GroupDownloaders, Item{Key: "download_handling", Name: "Download handling", From: b.handling[0].from, Detail: strings.Join(said, "; "),
		Why: "where the apps differ the stricter setting wins", DownloadHandling: p})
}
