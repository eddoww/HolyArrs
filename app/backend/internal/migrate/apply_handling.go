package migrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"slices"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/importer"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/jobs"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// handlingMark is the download handling a move found and left, for undo.
type handlingMark struct {
	Rules     importer.FileRules `json:"rules"`
	FeedEvery int                `json:"feed_every"`
	FeedOn    bool               `json:"feed_on"`
}

// readHandling reads the Where files go rules and the feed check schedule.
func readHandling(ctx context.Context, db *sql.DB) (handlingMark, error) {
	var h handlingMark
	var err error
	if h.Rules, err = importer.ReadFileRules(ctx, db); err != nil {
		return h, err
	}
	h.FeedEvery, h.FeedOn, err = jobs.ScheduleEvery(ctx, db, jobs.KindFeedCheck)
	return h, err
}

// writeHandling saves h and runs then in the same transaction.
func writeHandling(ctx context.Context, s *database.Store, h handlingMark, by domain.Actor, then func(tx *sql.Tx) error) error {
	return importer.SaveFileRules(ctx, s, h.Rules, by, func(tx *sql.Tx) error {
		if err := jobs.SetScheduleEveryTx(tx, jobs.KindFeedCheck, h.FeedEvery, h.FeedOn, by, time.Now().UTC()); err != nil {
			return err
		}
		if then != nil {
			return then(tx)
		}
		return nil
	})
}

func (r *run) downloadHandling(it Item) (string, string) {
	p := it.DownloadHandling
	cur, err := readHandling(r.ctx, r.m.Store.Read())
	if err != nil {
		return ResFailed, err.Error()
	}
	next := cur
	next.Rules.ExtraFileExtensions = slices.Clone(cur.Rules.ExtraFileExtensions)
	if p.MinFreeMB != nil {
		next.Rules.MinFreeMB = *p.MinFreeMB
	}
	if p.SkipFreeSpaceCheck != nil {
		next.Rules.SkipFreeSpaceCheck = *p.SkipFreeSpaceCheck
	}
	if p.ImportExtraFiles != nil {
		next.Rules.ImportExtraFiles = *p.ImportExtraFiles
	}
	if len(p.ExtraFileExtensions) > 0 {
		next.Rules.ExtraFileExtensions = p.ExtraFileExtensions
	}
	if p.FeedMinutes != nil {
		next.FeedOn = *p.FeedMinutes > 0
		if next.FeedOn {
			next.FeedEvery = *p.FeedMinutes * 60
		}
	}
	if reflect.DeepEqual(next, cur) {
		return ResAlready, "download handling is already set this way"
	}
	if err := next.Rules.Validate(); err != nil {
		return ResFailed, "these settings do not fit: " + err.Error()
	}
	if r.dry {
		return ResChange, it.Detail
	}
	before, _ := json.Marshal(cur)
	after, _ := json.Marshal(next)
	err = writeHandling(r.ctx, r.m.Store, next, r.by, func(tx *sql.Tx) error {
		return r.markTx(tx, it.Key, "download_handling", 0, string(before), string(after))
	})
	return result(err, ResChanged, it.Detail)
}

// undoHandling puts the download handling back as the move found it,
// unless someone changed it since.
func undoHandling(ctx context.Context, s *database.Store, beforeJSON, afterJSON string, by domain.Actor) (string, string) {
	var old, moved handlingMark
	if json.Unmarshal([]byte(beforeJSON), &old) != nil || json.Unmarshal([]byte(afterJSON), &moved) != nil {
		return ResKept, keptUnknown
	}
	cur, err := readHandling(ctx, s.Read())
	if err != nil {
		return ResFailed, err.Error()
	}
	if !reflect.DeepEqual(cur, moved) {
		return ResKept, keptChanged
	}
	if err := writeHandling(ctx, s, old, by, nil); err != nil {
		return ResFailed, err.Error()
	}
	return ResRemoved, "download handling is back as it was"
}
