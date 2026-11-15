package database

import (
	"database/sql"
	"io/fs"
	"strings"
)

// A database a fork build updated (it records 9001 or 9002 under the fork's
// names) holds settings in places upstream's later migrations do not know:
// 0109 rebuilds downloaders without the fork's after-import category and
// own-items switch, and 0121 rebuilds sources with a format check that
// refuses the fork's site formats and drops their user agent and freeleech
// choice. The runner applies every unrecorded migration in number order, so
// on such a database those run after 9001-9004. forkHooks wraps them, in
// their own transactions, so every value lands in upstream's columns and
// keys instead of being lost or failing the update:
//
//   - before 0109 the fork's Downloader columns are set aside;
//   - after 0118 they go into category_after_import and own_only, a
//     download's imported_at into category_moved_at, and a qBittorrent
//     seeding goal turns on upstream's remove after seeding;
//   - 0121 reads the fork's site rows as format 'site', with the site, user
//     agent and freeleech choice in extra and the site's polite limits.
//
// The SQL of each step is in migrations/fork. On any other database
// forkHooks changes nothing.

// The tables and columns the hooks look for.
const (
	forkDownloaders = "downloaders"
	forkDownloads   = "downloads"
	forkSources     = "sources"
	forkKeptTable   = "fork_downloaders_kept"
)

// forkDatabase says the database was updated by a fork build.
func forkDatabase(applied map[int]string) bool {
	return applied[9001] == "9001_download_handling.sql" || applied[9002] == "9002_site_sources.sql"
}

// forkHooks fits the pending migrations of a fork database with the steps
// that carry its values through (see above). applied is what the database
// records, after any renumbering.
func forkHooks(pending []migration, applied map[int]string) {
	if !forkDatabase(applied) {
		return
	}
	for i := range pending {
		m := &pending[i]
		switch m.version {
		case 109:
			m.before = forkKeepDownloaders
		case 118:
			m.after = forkRestoreDownloaders
		case 121:
			m.before = forkSiteRows
			m.sql = forkSiteSQL(m.sql)
		}
	}
}

// hasColumn says whether table has the column.
func hasColumn(tx *sql.Tx, table, column string) (bool, error) {
	var n int
	err := tx.QueryRow("SELECT count(*) FROM pragma_table_info(?) WHERE name = ?", table, column).Scan(&n)
	return n > 0, err
}

// hasTable says whether the table exists.
func hasTable(tx *sql.Tx, table string) (bool, error) {
	var n int
	err := tx.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&n)
	return n > 0, err
}

// forkRun runs one of the fork's step files.
func forkRun(tx *sql.Tx, name string) error {
	b, err := fs.ReadFile(embedded, "migrations/fork/"+name)
	if err != nil {
		return err
	}
	_, err = tx.Exec(string(b))
	return err
}

// forkKeepDownloaders sets the fork's Downloader columns aside before
// upstream's 0109 rebuild leaves them out.
func forkKeepDownloaders(tx *sql.Tx) error {
	has, err := hasColumn(tx, forkDownloaders, "post_import_category")
	if err != nil || !has {
		return err
	}
	if kept, err := hasTable(tx, forkKeptTable); err != nil || kept {
		return err
	}
	return forkRun(tx, "0097_downloaders_keep.sql")
}

// forkRestoreDownloaders puts the set-aside values into upstream's columns
// once 0118 added them, and moves the rest of the fork's download handling
// onto upstream's switches.
func forkRestoreDownloaders(tx *sql.Tx) error {
	kept, err := hasTable(tx, forkKeptTable)
	if err != nil {
		return err
	}
	if kept {
		if err := forkRun(tx, "0118_downloaders_restore.sql"); err != nil {
			return err
		}
	}
	if err := forkRun(tx, "0118_seeding.sql"); err != nil {
		return err
	}
	imported, err := hasColumn(tx, forkDownloads, "imported_at")
	if err != nil || !imported {
		return err
	}
	return forkRun(tx, "0118_downloads_imported.sql")
}

// forkSiteRows gives the fork's site Sources upstream's extra and limits
// before 0121 copies them. The format itself changes in 0121's copy
// (forkSiteSQL), as the table's own check refuses 'site' until then.
func forkSiteRows(tx *sql.Tx) error {
	has, err := hasColumn(tx, forkSources, "user_agent")
	if err != nil || !has {
		return err
	}
	return forkRun(tx, "0121_site_rows.sql")
}

// forkSiteSelect is the part of 0121's copy that reads a row's format.
const forkSiteSelect = "SELECT id, created_at, created_by, updated_at, updated_by, name, protocol, format, url"

// forkSiteSQL makes 0121's copy read a fork site row's format as 'site'.
// Should 0121 ever read differently, the update stops (forkSiteGuard)
// rather than lose the rows.
func forkSiteSQL(q string) string {
	if strings.Count(q, forkSiteSelect) != 1 {
		return forkSiteGuard + q
	}
	return strings.Replace(q, forkSiteSelect, "SELECT id, created_at, created_by, updated_at, updated_by, name, protocol, "+
		"CASE WHEN format IN ('iptorrents', 'knaben', 'subsplease', 'torrentscsv') THEN 'site' ELSE format END, url", 1)
}

// forkSiteGuard fails the migration on purpose, naming why.
const forkSiteGuard = "SELECT * FROM fork_0121_changed_shape_site_sources_would_be_lost;\n"
