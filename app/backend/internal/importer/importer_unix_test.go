//go:build unix

package importer

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// Completed download handling with hardlinks: on the same disk the import
// links the downloaded file into the Library, so one copy of the data has
// two names (the torrent keeps seeding from the download folder).
func TestImportHardlinkSharesTheData(t *testing.T) {
	w := setup(t)
	if _, err := (&Importer{Store: w.s}).Import(context.Background(), w.dlID, false, domain.System); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(w.dl, relName+".mkv"), filepath.Join(w.lib, "Dune - Part Two (2024)", placedStem+".mkv")} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); !ok || st.Nlink != 2 {
			t.Fatalf("%s: link count %v, want 2", p, fi.Sys())
		}
	}
}
