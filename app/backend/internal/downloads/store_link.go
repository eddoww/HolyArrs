package downloads

import (
	"context"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// webDownloaders are the yt-dlp Downloaders that can take a link now, first
// by priority.
func webDownloaders(ctx context.Context, q database.Lister) ([]domain.Downloader, error) {
	return database.Select[domain.Downloader](ctx, q, "downloaders", "protocol = 'web' AND removed = 0 AND state <> 'disabled' ORDER BY priority, id")
}

// jobInUse says whether a Download row follows the Downloader's job ext.
func jobInUse(ctx context.Context, q database.Querier, downloaderID int64, ext string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, "SELECT count(*) FROM downloads WHERE downloader_id = ? AND external_id = ?", downloaderID, ext).Scan(&n)
	return n > 0, err
}
