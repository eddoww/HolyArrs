package importer

import (
	"database/sql"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// UsableSideTypes keeps the side file types of a list ("srt, .SUB|idx")
// that HolyArrs accepts, in its form: lower case, no dot, each once, 8
// letters or digits or fewer, at most 10. Types it cannot take are left out.
func UsableSideTypes(list string) []string {
	var out []string
	for _, t := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' || r == '|' || r == ';' }) {
		if one, err := cleanSideTypes([]string{t}); err == nil && !slices.Contains(out, one[0]) && len(out) < 10 {
			out = append(out, one[0])
		}
	}
	return out
}

// PutSideFilesTx checks and stores the side files setting inside tx, for a
// caller that keeps more in the same transaction (an import's undo mark).
func PutSideFilesTx(tx *sql.Tx, in SideFiles, by domain.Actor, now time.Time) error {
	ts, err := cleanSideTypes(in.Types)
	if err != nil {
		return err
	}
	if err := database.PutSetting(tx, sideGroup, sideOnKey, strconv.FormatBool(in.On), by, now); err != nil {
		return err
	}
	return database.PutSetting(tx, sideGroup, sideTypesKey, strings.Join(ts, ","), by, now)
}
