package downloads

import (
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// ownCategories are the categories a Downloader works in: its own and, when set, the after-import one.
func ownCategories(d domain.Downloader) []string {
	out := []string{d.Category}
	if d.PostImportCategory != "" && d.PostImportCategory != d.Category {
		out = append(out, d.PostImportCategory)
	}
	return out
}
