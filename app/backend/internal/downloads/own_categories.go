package downloads

import (
	"context"
	"errors"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// "Only manage HolyArrs' own categories": on a client other apps share,
// a Downloader with OwnCategoriesOnly reads, shows and steers only the
// items in its category and its after-import category. Items elsewhere
// never reach the Downloads screen, and every action naming one is refused.

// ErrOutsideCategories refuses an action on an item outside the Downloader's own categories.
var ErrOutsideCategories = errors.New("this Downloader only manages HolyArrs' own categories, and this item is in another one")

// ownCategories are the categories a Downloader works in: its own and, when set, the after-import one.
func ownCategories(d domain.Downloader) []string {
	out := []string{d.Category}
	if d.PostImportCategory != "" && d.PostImportCategory != d.Category {
		out = append(out, d.PostImportCategory)
	}
	return out
}

// inOwnCategories says an item in category cat belongs to d.
func inOwnCategories(d domain.Downloader, cat string) bool {
	for _, c := range ownCategories(d) {
		if c == cat {
			return true
		}
	}
	return false
}

// onlyOwn keeps the items in d's own categories when the switch is on.
func onlyOwn(d domain.Downloader, jobs []Job) []Job {
	if !d.OwnCategoriesOnly {
		return jobs
	}
	out := jobs[:0:0]
	for _, j := range jobs {
		if inOwnCategories(d, j.Category) {
			out = append(out, j)
		}
	}
	return out
}

// ownItemsOnly returns ErrOutsideCategories unless every id is an item in
// d's own categories (always nil while the switch is off). The background
// reader's snapshot answers first; ids it cannot place are looked up in the
// client, one list per own category.
func (m *Manager) ownItemsOnly(ctx context.Context, e Engine, d domain.Downloader, ids []string) error {
	if !d.OwnCategoriesOnly {
		return nil
	}
	left := map[string]bool{}
	for _, id := range ids {
		left[strings.ToLower(id)] = true
	}
	take := func(jobs []Job) {
		for _, j := range jobs {
			if inOwnCategories(d, j.Category) {
				delete(left, strings.ToLower(j.ID))
			}
		}
	}
	if m.storeOn() {
		v := m.Sync.View(d.ID)
		jobs := make([]Job, 0, len(v.Items))
		for _, j := range v.Items {
			jobs = append(jobs, j)
		}
		take(jobs)
	}
	for _, cat := range ownCategories(d) {
		if len(left) == 0 {
			break
		}
		jobs, err := e.List(ctx, cat)
		if err != nil {
			return err
		}
		take(jobs)
	}
	if len(left) > 0 {
		return ErrOutsideCategories
	}
	return nil
}

// pauseOwn is Pause all and Resume all for a Downloader that manages only
// its own categories: it stops or starts the items in those, not the whole
// client. A client with no per-item bulk call is left alone.
func pauseOwn(ctx context.Context, d domain.Downloader, e Engine, pause bool) error {
	t, ok := e.(TorrentBulk)
	if !ok {
		return ErrNotSupported
	}
	var hashes []string
	for _, cat := range ownCategories(d) {
		jobs, err := e.List(ctx, cat)
		if err != nil {
			return err
		}
		for _, j := range jobs {
			hashes = append(hashes, j.ID)
		}
	}
	if len(hashes) == 0 {
		return nil
	}
	action := BulkStart
	if pause {
		action = BulkPause
	}
	return t.Bulk(ctx, action, hashes)
}

// ControlTorrentItems is ControlTorrentClient for a call on the named
// torrents. On a Downloader that manages only its own categories, every one
// of them must be in those categories.
func (m *Manager) ControlTorrentItems(ctx context.Context, downloaderID int64, hashes []string, write bool, call func(e Engine) error) error {
	return m.ControlTorrentClient(ctx, downloaderID, write, func(e Engine) error {
		var dr domain.Downloader
		if err := database.Get(ctx, m.Store.Read(), "downloaders", downloaderID, &dr); err != nil {
			return ErrNoDownloader
		}
		if err := m.ownItemsOnly(ctx, e, dr, hashes); err != nil {
			return err
		}
		return call(e)
	})
}
