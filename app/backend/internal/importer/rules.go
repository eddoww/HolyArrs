package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// FileRules are the user settings for holding replaced files and free space.
type FileRules struct {
	HoldingDays        int  `json:"holding_days"`
	MinFreeMB          int  `json:"min_free_mb"`
	SkipFreeSpaceCheck bool `json:"skip_free_space_check"`
	// ImportExtraFiles places a download's extra files (ExtraFileExtensions,
	// lower case, no dot) beside the imported video.
	ImportExtraFiles    bool     `json:"import_extra_files"`
	ExtraFileExtensions []string `json:"extra_file_extensions"`
}

var (
	ErrMinFree     = errors.New("min_free_mb must be between 100 and 1048576")
	ErrHoldingDays = errors.New("holding_days must be between 0 (keep) and 3650")
	ErrExtraExt    = errors.New("extra_file_extensions holds at most 20 file types of 1 to 10 letters or digits, and no video, music or program types")
)

// DefaultFileRules returns the rules used before the user changes them.
func DefaultFileRules() FileRules {
	return FileRules{HoldingDays: 7, MinFreeMB: 100, ImportExtraFiles: true, ExtraFileExtensions: append([]string(nil), DefaultExtraExtensions...)}
}

// Validate reports whether the rules are in range.
func (r FileRules) Validate() error {
	if r.MinFreeMB < 100 || r.MinFreeMB > 1<<20 {
		return ErrMinFree
	}
	if r.HoldingDays < 0 || r.HoldingDays > 3650 {
		return ErrHoldingDays
	}
	if _, ok := extraExtensions(strings.Join(r.ExtraFileExtensions, ",")); !ok {
		return ErrExtraExt
	}
	return nil
}

// extraExtensions reads a list of extra file types ("srt, .SUB,idx"): lower
// case, no dot, each once. ok is false for a type that is not 1 to 10
// letters or digits, a media or program type, or more than 20 of them.
func extraExtensions(list string) ([]string, bool) {
	out := []string{}
	seen := map[string]bool{}
	for _, e := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' }) {
		e = strings.ToLower(strings.TrimPrefix(e, "."))
		if e == "" || len(e) > 10 || strings.Trim(e, "abcdefghijklmnopqrstuvwxyz0123456789") != "" || IsMedia("."+e) || programExt["."+e] {
			return nil, false
		}
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out, len(out) <= 20
}

// ReadFileRules loads the saved rules, or the defaults.
func ReadFileRules(ctx context.Context, db database.Lister) (FileRules, error) {
	r := DefaultFileRules()
	rows, err := db.QueryContext(ctx, "SELECT key, value FROM settings WHERE grp = 'where_files_go' AND key IN ('holding_days', 'min_free_mb', 'skip_free_space_check', 'import_extra_files', 'extra_file_extensions')")
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return r, err
		}
		n, nerr := strconv.Atoi(v)
		switch k {
		case "holding_days":
			if nerr == nil && n >= 0 && n <= 3650 {
				r.HoldingDays = n
			}
		case "min_free_mb":
			if nerr == nil && n >= 100 && n <= 1<<20 {
				r.MinFreeMB = n
			}
		case "skip_free_space_check":
			r.SkipFreeSpaceCheck = v == "true"
		case "import_extra_files":
			r.ImportExtraFiles = v == "true"
		case "extra_file_extensions":
			if exts, ok := extraExtensions(v); ok {
				r.ExtraFileExtensions = exts
			}
		}
	}
	return r, rows.Err()
}

// SaveFileRules validates and stores the rules.
func SaveFileRules(ctx context.Context, s *database.Store, r FileRules, by domain.Actor) error {
	if err := r.Validate(); err != nil {
		return err
	}
	exts, _ := extraExtensions(strings.Join(r.ExtraFileExtensions, ","))
	return s.Write(ctx, func(tx *sql.Tx) error {
		now := database.FormatTime(time.Now())
		for k, v := range map[string]string{"holding_days": strconv.Itoa(r.HoldingDays), "min_free_mb": strconv.Itoa(r.MinFreeMB),
			"skip_free_space_check": strconv.FormatBool(r.SkipFreeSpaceCheck), "import_extra_files": strconv.FormatBool(r.ImportExtraFiles),
			"extra_file_extensions": strings.Join(exts, ",")} {
			if _, err := tx.Exec(`INSERT INTO settings (grp, key, value, created_at, created_by, updated_at, updated_by) VALUES ('where_files_go', ?, ?, ?, ?, ?, ?)
				ON CONFLICT (grp, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
				k, v, now, actorText(by), now, actorText(by)); err != nil {
				return err
			}
		}
		return nil
	})
}

// SpaceError is returned when a disk lacks room for an import.
type SpaceError struct {
	Dir              string
	Need, Keep, Free int64
}

// Error returns a message with the free and needed space.
func (e *SpaceError) Error() string {
	return fmt.Sprintf("not enough free space in %s: this file needs %d MB and %d MB must stay free, only %d MB are free",
		e.Dir, (e.Need+mib-1)/mib, e.Keep/mib, e.Free/mib)
}

const mib = 1 << 20

// CheckSpaceMin fails when dir cannot hold size more bytes plus minMB.
func CheckSpaceMin(dir string, size int64, minMB int) error {
	return checkSpaceReserved(existingDir(dir), size, 0, minMB)
}

func existingDir(dir string) string {
	for p := dir; ; {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		up := filepath.Dir(p)
		if up == p {
			return dir
		}
		p = up
	}
}

func checkSpaceReserved(dir string, size, reserved int64, minMB int) error {
	keep := int64(minMB) * mib
	free := freeSpaceOf(dir)
	if free >= 0 && free-reserved < size+keep {
		return &SpaceError{Dir: dir, Need: size, Keep: keep, Free: max(free-reserved, 0)}
	}
	return nil
}
