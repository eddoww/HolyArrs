package downloads

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
)

// markImported stamps a Download as fully imported, once.
func markImported(ctx context.Context, s *database.Store, downloadID int64, at time.Time) error {
	return s.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE downloads SET imported_at = COALESCE(imported_at, ?) WHERE id = ?", database.FormatTime(at), downloadID)
		return err
	})
}

// importedDownloads maps the lower-case client id of each Download of a
// Downloader that HolyArrs imported to its id.
func importedDownloads(ctx context.Context, q database.Lister, downloaderID int64) (map[string]int64, error) {
	rows, err := q.QueryContext(ctx, "SELECT id, external_id FROM downloads WHERE downloader_id = ? AND imported_at IS NOT NULL AND external_id != ''", downloaderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id int64
		var ext string
		if err := rows.Scan(&id, &ext); err != nil {
			return nil, err
		}
		out[strings.ToLower(ext)] = id
	}
	return out, rows.Err()
}
