package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// homelabMigrations are the fork's migrations 0097-0101 as the homelab
// builds shipped them, before upstream took those numbers.
//
//go:embed testdata/homelab_0097_0101/*.sql
var homelabMigrations embed.FS

// homelabUpTo is the migration set of a homelab build: upstream's 0001-0096
// and the fork's own 0097 up to last, under their old numbers.
func homelabUpTo(t *testing.T, last int) fs.FS {
	t.Helper()
	out := upTo(t, 96).(fstest.MapFS)
	ents, err := fs.ReadDir(homelabMigrations, "testdata/homelab_0097_0101")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		var v int
		if _, err := fmt.Sscanf(e.Name(), "%04d_", &v); err != nil || v > last {
			continue
		}
		b, err := fs.ReadFile(homelabMigrations, "testdata/homelab_0097_0101/"+e.Name())
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = &fstest.MapFile{Data: b}
	}
	return out
}

// homelabFixture writes rows into every table the fork's migrations touched.
func homelabFixture(t *testing.T, s *Store, traits bool) {
	t.Helper()
	qs := []string{
		`INSERT INTO sources(id,created_at,created_by,updated_at,updated_by,name,protocol,format,url,user_agent,freeleech_only,state)
		 VALUES(4,'a','{}','b','{}','IPTorrents','torrent','iptorrents','https://iptorrents.com','Mozilla/5.0 Fixture',1,'enabled')`,
		`INSERT INTO sources(id,created_at,created_by,updated_at,updated_by,name,protocol,format,url) VALUES(6,'a','{}','b','{}','Knaben','torrent','knaben','https://knaben.eu')`,
		`INSERT INTO downloaders(id,created_at,created_by,updated_at,updated_by,name,adapter,protocol,url,category,post_import_category,own_categories_only)
		 VALUES(2,'a','{}','b','{}','qBittorrent','qbittorrent','torrent','http://qbit:8080','radarr','radarr-imported',1)`,
	}
	if traits {
		qs = append(qs,
			`INSERT INTO own_traits (id, created_at, created_by, updated_at, updated_by, name, match, terms, conditions) VALUES
			 (3, 'c', '{}', 'u', '{}', 'Dutch subs', 'name', '["NLSubs"]', '[]'),
			 (5, 'c', '{}', 'u', '{}', 'Norwegian', 'conditions', '[]', '[{"name":"Norwegian","kind":"language","value":"no","required":true}]'),
			 (9, 'c', '{}', 'u', '{}', 'Gone', 'group', '["x"]', '[]')`,
			`DELETE FROM own_traits WHERE id = 9`)
	}
	if err := s.Write(context.Background(), func(tx *sql.Tx) error {
		for _, q := range qs {
			if _, err := tx.Exec(q); err != nil {
				return fmt.Errorf("%s: %w", q, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// tableRows reads every table's rows as column-to-value maps.
func tableRows(t *testing.T, db *sql.DB) map[string][]map[string]any {
	t.Helper()
	names := []string{}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name != 'schema_migrations'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	_ = rows.Close()
	out := map[string][]map[string]any{}
	for _, n := range names {
		r, err := db.Query(`SELECT * FROM "` + n + `"`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := r.Columns()
		var list []map[string]any
		for r.Next() {
			v := make([]any, len(cols))
			p := make([]any, len(cols))
			for i := range v {
				p[i] = &v[i]
			}
			if err := r.Scan(p...); err != nil {
				t.Fatal(err)
			}
			m := map[string]any{}
			for i, c := range cols {
				if b, ok := v[i].([]byte); ok {
					v[i] = string(b)
				}
				m[c] = v[i]
			}
			list = append(list, m)
		}
		_ = r.Close()
		out[n] = list
	}
	return out
}

// schemaOf describes a database's schema so that the same tables, columns,
// checks, indexes and triggers compare equal whatever order ALTER TABLE
// added columns in.
func schemaOf(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT type, name, ifnull(sql, '') FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var typ, name, text string
		if err := rows.Scan(&typ, &name, &text); err != nil {
			t.Fatal(err)
		}
		text = strings.Join(strings.Fields(text), " ")
		if typ == "table" {
			text = sortedParts(text)
		}
		out[typ+" "+name] = text
	}
	return out
}

// sortedParts sorts the top-level parts of a CREATE TABLE's column list.
func sortedParts(create string) string {
	open := strings.IndexByte(create, '(')
	if open < 0 || !strings.HasSuffix(create, ")") {
		return create
	}
	body := create[open+1 : len(create)-1]
	var parts []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			parts = append(parts, strings.TrimSpace(body[start:i]))
			start = i + 1
		}
	}
	parts = append(parts, strings.TrimSpace(body[start:]))
	slices.Sort(parts)
	return create[:open] + "(" + strings.Join(parts, ", ") + ")"
}

func appliedList(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query("SELECT version, name FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v int
		var n string
		if err := rows.Scan(&v, &n); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%d %s", v, n))
	}
	return out
}

// A database the homelab builds updated (the fork's old 0097-0101, none of
// upstream's 0097 and up) upgrades on its next start: a pinned backup is
// taken, the fork's migrations are renumbered, the missing upstream ones run,
// and the result has the schema of a new database and every row it had.
func TestForkDatabaseUpgradesFromHomelab(t *testing.T) {
	for _, c := range []struct {
		name   string
		last   int
		traits bool
	}{
		{"all five", 101, true},
		{"before trait conditions", 98, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			old, err := Open(dir, Options{MainMigrations: homelabUpTo(t, c.last)})
			if err != nil {
				t.Fatal(err)
			}
			homelabFixture(t, old, c.traits)
			before := tableRows(t, old.Read())
			oldApplied := map[int]string{}
			for _, l := range appliedList(t, old.Read()) {
				var v int
				var n string
				_, _ = fmt.Sscanf(l, "%d %s", &v, &n)
				oldApplied[v] = n
			}
			if oldApplied[97] != "0097_download_handling.sql" {
				t.Fatalf("the fixture is not a homelab database: %v", oldApplied[97])
			}
			if err := old.Close(); err != nil {
				t.Fatal(err)
			}

			s, err := Open(dir, Options{})
			if err != nil {
				t.Fatalf("first start of the merged build: %v", err)
			}
			defer s.Close()

			ms, err := EmbeddedMigrations("migrations/main")
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, m := range ms {
				want = append(want, fmt.Sprintf("%d %s", m.Version, m.Name))
			}
			if got := appliedList(t, s.Read()); !reflect.DeepEqual(got, want) {
				t.Fatalf("applied after the upgrade:\n%v\nwant\n%v", got, want)
			}

			fresh, err := Open(t.TempDir(), Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer fresh.Close()
			if got, want := schemaOf(t, s.Read()), schemaOf(t, fresh.Read()); !reflect.DeepEqual(got, want) {
				for k := range want {
					if got[k] != want[k] {
						t.Errorf("%s:\n got  %s\n want %s", k, got[k], want[k])
					}
				}
				for k := range got {
					if _, ok := want[k]; !ok {
						t.Errorf("%s is left over: %s", k, got[k])
					}
				}
			}

			// Every row keeps every value it had; new columns only add.
			after := tableRows(t, s.Read())
			for table, rows := range before {
				got := after[table]
				if len(got) != len(rows) {
					t.Errorf("%s: %d rows, had %d", table, len(got), len(rows))
					continue
				}
				had, have := make([]string, len(rows)), make([]string, len(got))
				for i := range rows {
					kept := map[string]any{}
					for col := range rows[i] {
						kept[col] = got[i][col]
					}
					had[i], have[i] = fmt.Sprint(rows[i]), fmt.Sprint(kept)
				}
				sort.Strings(had)
				sort.Strings(have)
				if !reflect.DeepEqual(had, have) {
					t.Errorf("%s rows changed:\n%v\nhad\n%v", table, have, had)
				}
			}
			if fk := downloaderRows(t, s.Read(), "PRAGMA foreign_key_check"); len(fk) != 0 {
				t.Fatalf("foreign key violations %v", fk)
			}
			backups, _ := filepath.Glob(filepath.Join(dir, "backups", fmt.Sprintf("main-pre-update-%04d-to-9004-*.db", c.last)))
			if len(backups) != 1 {
				t.Fatalf("pre-update backups %v", backups)
			}

			// The upgraded tables take what both upstream and the fork store.
			err = s.Write(context.Background(), func(tx *sql.Tx) error {
				for _, q := range []string{
					`INSERT INTO own_traits (created_at, updated_at, name, match, terms) VALUES ('c', 'u', 'Huge', 'size', '["60-"]')`,
					`INSERT INTO own_traits (created_at, updated_at, name, match, terms, conditions) VALUES ('c', 'u', 'HDR', 'conditions', '[]', '[{"kind":"release_title","value":"HDR"}]')`,
					`INSERT INTO own_traits (created_at, updated_at, name, match, terms, source, source_id, specs) VALUES ('c', 'u', 'TRaSH x', 'name', '[]', 'trash', 'radarr:x', '[]')`,
					`UPDATE downloaders SET category_movies = 'radarr', category_shows = 'sonarr' WHERE id = 2`,
					`UPDATE user_profiles SET language = 'nl'`,
					`UPDATE titles SET score = 7.5, votes = 3`,
				} {
					if _, err := tx.Exec(q); err != nil {
						return fmt.Errorf("%s: %w", q, err)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if c.traits {
				var id int64
				if err := s.Read().QueryRow("SELECT min(id) FROM own_traits WHERE name = 'Huge'").Scan(&id); err != nil || id != 10 {
					t.Fatalf("a new trait took id %d (%v); the removed 9 must stay unused", id, err)
				}
				var conds string
				if err := s.Read().QueryRow("SELECT conditions FROM own_traits WHERE id = 5 AND match = 'conditions'").Scan(&conds); err != nil || !strings.Contains(conds, `"language"`) {
					t.Fatalf("the Norwegian trait lost its conditions: %q %v", conds, err)
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}

			// A second start finds nothing to do.
			again, err := Open(dir, Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer again.Close()
			n, _ := filepath.Glob(filepath.Join(dir, "backups", "main-pre-update-*.db"))
			if len(n) != 1 {
				t.Fatalf("a second start updated again: %v", n)
			}
		})
	}
}

// A start cut off after the renumbering finishes the missing migrations on
// the next start, even though a later number is already recorded.
func TestForkRenumberResumesAfterACutOff(t *testing.T) {
	dir := t.TempDir()
	old, err := Open(dir, Options{MainMigrations: homelabUpTo(t, 101)})
	if err != nil {
		t.Fatal(err)
	}
	_ = old.Close()
	// The renumbering and the first upstream migration, then a stop.
	partial := upTo(t, 98).(fstest.MapFS)
	for _, m := range mustEmbedded(t) {
		if m.Version >= 9001 || m.Version == 102 || m.Version == 103 {
			partial[m.Name] = &fstest.MapFile{Data: []byte(m.SQL)}
		}
	}
	cut, err := Open(dir, Options{MainMigrations: partial})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := SchemaVersion(cut.main.db); v != 9004 {
		t.Fatalf("schema %d after the cut-off start", v)
	}
	_ = cut.Close()
	s, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var want []string
	for _, m := range mustEmbedded(t) {
		want = append(want, fmt.Sprintf("%d %s", m.Version, m.Name))
	}
	if got := appliedList(t, s.Read()); !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded after resuming:\n%v\nwant\n%v", got, want)
	}
}

func mustEmbedded(t *testing.T) []Migration {
	t.Helper()
	ms, err := EmbeddedMigrations("migrations/main")
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

// laterUpstream is a migration upstream adds after the fork's own range was
// recorded: the first number below 9001 no built-in migration uses.
func laterUpstream(t *testing.T) (fstest.MapFS, int) {
	t.Helper()
	all := fstest.MapFS{}
	top := 0
	for _, m := range mustEmbedded(t) {
		all[m.Name] = &fstest.MapFile{Data: []byte(m.SQL)}
		if m.Version < 9001 {
			top = max(top, m.Version)
		}
	}
	v := top + 1
	all[fmt.Sprintf("%04d_later_upstream.sql", v)] = &fstest.MapFile{Data: []byte("CREATE TABLE later_upstream (id INTEGER PRIMARY KEY);")}
	return all, v
}

// The fork's migrations live from 9001 up. A newer upstream migration, lower
// than those, still runs on a database that recorded them, after a backup.
func TestUpstreamMigrationRunsBelowTheForkRange(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	later, v := laterUpstream(t)
	s, err = Open(dir, Options{MainMigrations: later})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var name string
	if err := s.Read().QueryRow("SELECT name FROM schema_migrations WHERE version = ?", v).Scan(&name); err != nil || name != fmt.Sprintf("%04d_later_upstream.sql", v) {
		t.Fatalf("migration %d recorded as %q (%v)", v, name, err)
	}
	if _, err := s.Read().Exec("SELECT count(*) FROM later_upstream"); err != nil {
		t.Fatalf("the later upstream migration did not run: %v", err)
	}
	if b, _ := filepath.Glob(filepath.Join(dir, "backups", "main-pre-update-9004-to-9004-*.db")); len(b) != 1 {
		t.Fatalf("pre-update backups %v", b)
	}
}

// A database that recorded a migration this build does not have is newer
// data, even when its number is below this build's newest.
func TestUnknownMigrationBelowTheForkRangeIsRefused(t *testing.T) {
	dir := t.TempDir()
	later, v := laterUpstream(t)
	s, err := Open(dir, Options{MainMigrations: later})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	_, err = Open(dir, Options{})
	var nd *NewerDataError
	if !errors.As(err, &nd) || nd.Have != v {
		t.Fatalf("got %v, want newer data with migration %d", err, v)
	}
}
