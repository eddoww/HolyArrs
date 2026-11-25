package migrate

import (
	"context"

	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// retryKeptRules tries the Pick rules an undo kept because something used
// them once more (those in retry): a Library folder this move made, undone
// after them, may have been all that used them. (Kept imported Pick rules
// are taken back by the next import, which finds them by their profile.)
func (m *Mover) retryKeptRules(ctx context.Context, kept []madeRow, retry map[string]bool, rep *Report, by domain.Actor) {
	for _, x := range kept {
		if !retry[x.key] {
			continue
		}
		if res, detail := m.undoOne(ctx, x, by); res == ResRemoved {
			for i := range rep.Lines {
				if rep.Lines[i].Key == x.key && rep.Lines[i].Result == ResKept {
					rep.Lines[i].Result, rep.Lines[i].Detail = res, detail
					rep.Counts[ResKept]--
					rep.Counts[ResRemoved]++
					break
				}
			}
		}
	}
}
