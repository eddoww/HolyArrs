package database

import (
	"database/sql"
	"fmt"
	"io/fs"
	"strings"
)

// Fork builds numbered five main migrations 0097-0101 before upstream
// HolyArrs gave those numbers to its own. The fork's own migrations now
// live from 9001 up, a range upstream never reaches, so its new numbers
// never collide and an upstream migration not yet recorded runs whatever
// its number (plan). A database such a build updated records the old ones
// under their old numbers and names, so on its next start the runner
// renumbers them (forkRenumber) before anything else runs: each goes to the
// number it has now, one that does exactly what an upstream migration does
// takes that migration's number, and the upstream migrations still missing
// then run in order. It all happens after the pre-update backup, in
// transactions checked like every migration.

// forkStep is one migration a fork build recorded under an old number.
type forkStep struct {
	oldVersion int
	oldName    string
	newVersion int
	newName    string
}

// forkSteps are the fork's old migrations and where each lives now.
// 0100_move_reads_by_address is the same SQL as upstream's
// 0102_move_per_instance, so it counts as that one.
var forkSteps = []forkStep{
	{97, "0097_download_handling.sql", 9001, "9001_download_handling.sql"},
	{98, "0098_site_sources.sql", 9002, "9002_site_sources.sql"},
	{99, "0099_trait_conditions.sql", 9003, "9003_trait_conditions.sql"},
	{100, "0100_move_reads_by_address.sql", 102, "0102_move_per_instance.sql"},
	{101, "0101_subtitle_language_guessed.sql", 9004, "9004_subtitle_language_guessed.sql"},
}

// forkTraitsStandIn are the upstream migrations the own_traits rebuild of
// a database that ran the old 0099 stands in for, besides 9003 itself.
var forkTraitsStandIn = []forkStep{
	{newVersion: 97, newName: "0097_own_trait_formats.sql"},
	{newVersion: 103, newName: "0103_own_trait_size.sql"},
}

// appliedMigrations reads the version and name of every applied migration.
func appliedMigrations(db *sql.DB) (map[int]string, error) {
	rows, err := db.Query("SELECT version, name FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var v int
		var name string
		if err := rows.Scan(&v, &name); err != nil {
			return nil, err
		}
		out[v] = name
	}
	return out, rows.Err()
}

// forkRenumber returns the step that renumbers a fork database's old
// migrations (nil when it has none), and changes applied to what it holds
// once that step ran.
func forkRenumber(applied map[int]string) (*migration, error) {
	var found []forkStep
	for _, s := range forkSteps {
		if applied[s.oldVersion] == s.oldName {
			found = append(found, s)
		}
	}
	if len(found) == 0 {
		return nil, nil
	}
	var b strings.Builder
	traits := applied[99] == "0099_trait_conditions.sql"
	if traits {
		rebuild, err := fs.ReadFile(embedded, "migrations/fork/0099_own_traits_to_9003.sql")
		if err != nil {
			return nil, err
		}
		b.Write(rebuild)
	}
	// Each old row keeps when it ran and takes its new number and name.
	// No new number is an old one, so the order does not matter.
	for _, s := range found {
		if _, taken := applied[s.newVersion]; taken {
			return nil, fmt.Errorf("migration %d is recorded both as %s and under its fork number %d", s.newVersion, applied[s.newVersion], s.oldVersion)
		}
		fmt.Fprintf(&b, "\nUPDATE schema_migrations SET version = %d, name = '%s' WHERE version = %d AND name = '%s';", s.newVersion, s.newName, s.oldVersion, s.oldName)
		delete(applied, s.oldVersion)
		applied[s.newVersion] = s.newName
	}
	if traits {
		for _, s := range forkTraitsStandIn {
			if _, taken := applied[s.newVersion]; taken {
				return nil, fmt.Errorf("migration %d is recorded already, next to the fork's old 0099", s.newVersion)
			}
			fmt.Fprintf(&b, "\nINSERT INTO schema_migrations(version, name, applied_at) VALUES (%d, '%s', strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ', 'now'));", s.newVersion, s.newName)
			applied[s.newVersion] = s.newName
		}
	}
	top := 0
	for v := range applied {
		top = max(top, v)
	}
	// Its version is the newest it records, so the backup before it is
	// named for the schema the update reaches.
	return &migration{version: top, name: "renumbering the fork's migrations 0097-0101", sql: b.String(), renumber: true}, nil
}
