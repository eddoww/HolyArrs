package requests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// OpenShow4KAsk finds the oldest open 4K ask for a show with any of ids, if
// any, so a show already in the Library can offer its own Ask in 4K.
func OpenShow4KAsk(ctx context.Context, db *sql.DB, ids map[string]string, me int64) (*AskBrief, error) {
	var or []string
	args := []any{me}
	for _, k := range []string{"tmdb", "tvdb", "tvmaze", "imdb"} {
		if ids[k] != "" {
			or = append(or, "json_extract(a.source_ids, '$."+k+"') = ?")
			args = append(args, ids[k])
		}
	}
	if len(or) == 0 {
		return nil, nil
	}
	var b AskBrief
	var seasons string
	err := db.QueryRowContext(ctx, openShow4KAskSQL+strings.Join(or, " OR ")+`)
		ORDER BY a.id LIMIT 1`, args...).Scan(&b.ID, &b.State, &seasons, &b.Mine, &b.People)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.StateLabel = StateLabels[b.State]
	b.Seasons = []int{}
	_ = json.Unmarshal([]byte(seasons), &b.Seasons)
	return &b, nil
}
