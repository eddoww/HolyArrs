package downloads

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// After an import: the Download is marked imported, a Usenet job is
// cleared (ClearFinished), and a torrent moves to the Downloader's
// after-import category. Later the seeding clean-up removes imported
// torrents the client stopped at their goal. Both leave the Library's
// hardlinks alone: only the torrent goes, never the files.

// Recategorizer is the optional part of an engine that moves torrents to another category.
type Recategorizer interface {
	SetCategory(ctx context.Context, hashes []string, category string) error
}

// SetCategory moves torrents to a category, creating it first (a 409 means
// it is there). A torrent added with its own save path keeps its files where
// they are; one in automatic mode follows the category's folder.
func (q *Qbittorrent) SetCategory(ctx context.Context, hashes []string, category string) error {
	hs, err := hashList(hashes)
	if err != nil {
		return err
	}
	if category != "" {
		if _, err := q.call(ctx, http.MethodPost, "torrents/createCategory", url.Values{"category": {category}}, http.StatusConflict); err != nil {
			return err
		}
	}
	_, err = q.call(ctx, http.MethodPost, "torrents/setCategory", url.Values{"hashes": {hs}, "category": {category}})
	return err
}

// AfterImport runs once an Import of downloadID fully succeeded.
func (m *Manager) AfterImport(ctx context.Context, downloadID int64) {
	if err := markImported(ctx, m.Store, downloadID, m.now()); err != nil {
		return
	}
	m.ClearFinished(ctx, downloadID)
	m.moveImported(ctx, downloadID)
}

// moveImported moves an imported torrent to its Downloader's after-import category.
func (m *Manager) moveImported(ctx context.Context, downloadID int64) {
	var d domain.Download
	if err := database.Get(ctx, m.Store.Read(), "downloads", downloadID, &d); err != nil || d.ExternalID == "" {
		return
	}
	var dr domain.Downloader
	if err := database.Get(ctx, m.Store.Read(), "downloaders", d.DownloaderID, &dr); err != nil {
		return
	}
	if dr.Protocol != "torrent" || dr.PostImportCategory == "" || readOnlyFor(dr) {
		return
	}
	err := m.ControlEngine(ctx, dr, func(e Engine) error {
		r, ok := e.(Recategorizer)
		if !ok {
			return ErrNotSupported
		}
		return r.SetCategory(ctx, []string{d.ExternalID}, dr.PostImportCategory)
	})
	if errors.Is(err, ErrNotFound) {
		return // removed already: nothing left to move
	}
	if err != nil {
		m.event(ctx, &d, "download_not_moved", fmt.Sprintf("Could not move the imported download to the %s category in %s: %v", dr.PostImportCategory, dr.Name, firstLine(err.Error())))
		return
	}
	m.event(ctx, &d, "download_changed", fmt.Sprintf("Moved the imported download to the %s category in %s", dr.PostImportCategory, dr.Name))
	if m.Sync != nil {
		m.Kick(dr.ID)
	}
}

// seedCleanupEvery is how often the seeding clean-up looks at the clients.
const seedCleanupEvery = 15 * time.Minute

// seedingStopped says qBittorrent stopped a finished torrent: at its share
// limit, or by hand. qBittorrent 5 says stoppedUP, older ones pausedUP.
func seedingStopped(j Job) bool {
	return j.Progress >= 1 && (j.Detail == "stoppedUP" || j.Detail == "pausedUP")
}

// seedGoalMet says a stopped torrent reached the ratio or the seeding time.
func seedGoalMet(j Job, g domain.SeedGoal) bool {
	return g.Ratio != nil && j.Ratio >= *g.Ratio || g.Minutes != nil && j.SeedingTime >= int64(*g.Minutes)*60
}

// SeedCleanup removes, from each qBittorrent Downloader with a seeding goal,
// the torrents HolyArrs imported that the client stopped and that reached
// the goal. Only the torrent goes: its files stay, so the Library's
// hardlinks are untouched. It returns how many it removed.
func (m *Manager) SeedCleanup(ctx context.Context) (int, error) {
	ds, err := m.Downloaders(ctx)
	if err != nil {
		return 0, err
	}
	removed := 0
	var errs []error
	for _, dr := range ds {
		g := dr.SeedGoal
		if dr.Adapter != "qbittorrent" || g.Ratio == nil && g.Minutes == nil || readOnlyFor(dr) || dr.State == "disabled" {
			continue
		}
		mine, err := importedDownloads(ctx, m.Store.Read(), dr.ID)
		if err != nil || len(mine) == 0 {
			errs = append(errs, err)
			continue
		}
		e, err := m.engine(ctx, dr)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		seen := map[string]bool{}
		for _, cat := range ownCategories(dr) {
			jobs, err := e.List(ctx, cat)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			for _, j := range jobs {
				id, ok := mine[strings.ToLower(j.ID)]
				if !ok || seen[j.ID] || !seedingStopped(j) || !seedGoalMet(j, g) {
					continue
				}
				seen[j.ID] = true
				var d domain.Download
				if database.Get(ctx, m.Store.Read(), "downloads", id, &d) != nil {
					continue
				}
				if err := e.Remove(ctx, j.ID, false); err != nil && !errors.Is(err, ErrNotFound) {
					m.event(ctx, &d, "download_not_removed", fmt.Sprintf("Could not remove the seeded download from %s: %v", dr.Name, firstLine(err.Error())))
					errs = append(errs, err)
					continue
				}
				removed++
				m.event(ctx, &d, "download_removed", fmt.Sprintf("Removed the seeded download from %s (ratio %s, seeded %s); its files stay", dr.Name,
					strconv.FormatFloat(j.Ratio, 'f', 2, 64), hours(time.Duration(j.SeedingTime)*time.Second)))
			}
		}
		if len(seen) > 0 && m.Sync != nil {
			m.Kick(dr.ID)
		}
	}
	return removed, errors.Join(errs...)
}
