package database

import (
	"context"
	"database/sql"
	"testing"
)

// 0103 rebuilds own_traits to allow the size kind and keeps every trait, its
// id, and the AUTOINCREMENT mark, so a removed trait's id is never reused.
func TestOwnTraitsKeptBySizeMigration(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	old, err := Open(dir, Options{MainMigrations: upTo(t, 102)})
	if err != nil {
		t.Fatal(err)
	}
	err = old.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO own_traits (id, created_at, updated_at, name, match, terms, source, source_id, specs) VALUES
			(3, 'c', 'u', 'Remux', 'name', '["remux"]', '', '', '[]'),
			(9, 'c', 'u', 'TRaSH HDR', 'group', '["x"]', 'trash', 'abc', '[{"k":1}]')`)
		if err != nil {
			return err
		}
		// A removed trait leaves the counter above the highest id left.
		_, err = tx.Exec(`INSERT INTO own_traits (id, created_at, updated_at, name, match) VALUES (12, 'c', 'u', 'Gone', 'name')`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`DELETE FROM own_traits WHERE id = 12`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = old.Close()

	s, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var n int
	if err := s.Read().QueryRow(`SELECT count(*) FROM own_traits WHERE
		(id = 3 AND name = 'Remux' AND match = 'name' AND terms = '["remux"]') OR
		(id = 9 AND source = 'trash' AND source_id = 'abc' AND specs = '[{"k":1}]')`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("traits kept %d %v", n, err)
	}
	var id int64
	err = s.Write(ctx, func(tx *sql.Tx) error {
		r, err := tx.Exec(`INSERT INTO own_traits (created_at, updated_at, name, match, terms) VALUES ('c', 'u', 'Huge', 'size', '["60-"]')`)
		if err != nil {
			return err
		}
		id, err = r.LastInsertId()
		return err
	})
	if err != nil {
		t.Fatalf("size trait: %v", err)
	}
	if id <= 12 {
		t.Fatalf("a removed trait's id came back: %d", id)
	}
	// The name stays unique without regard to case.
	err = s.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO own_traits (created_at, updated_at, name, match) VALUES ('c', 'u', 'remux', 'name')`)
		return err
	})
	if err == nil {
		t.Fatal("a second trait named remux was accepted")
	}
}

// 0107 widens own_traits to the conditions match kind, keeps every row and
// keeps the id counter, so a removed trait's id is never given again.
func TestOwnTraitsKeptByConditionsMigration(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	old, err := Open(dir, Options{MainMigrations: upTo(t, 106)})
	if err != nil {
		t.Fatal(err)
	}
	err = old.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO own_traits (id, created_at, created_by, updated_at, updated_by, name, match, terms)
			VALUES (3, 'c', '{}', 'u', '{}', 'Dutch subs', 'name', '["NLSubs"]'), (9, 'c', '{}', 'u', '{}', 'Bad groups', 'group', '["yify"]')`); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM own_traits WHERE id = 9`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = old.Close()
	s, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var name, match, terms, conds string
	if err := s.Read().QueryRow("SELECT name, match, terms, conditions FROM own_traits WHERE id = 3").Scan(&name, &match, &terms, &conds); err != nil ||
		name != "Dutch subs" || match != "name" || terms != `["NLSubs"]` || conds != "[]" {
		t.Fatalf("row kept %q %q %q %q %v", name, match, terms, conds, err)
	}
	var id int64
	err = s.Write(ctx, func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO own_traits (created_at, created_by, updated_at, updated_by, name, match, conditions)
			VALUES ('c', '{}', 'u', '{}', 'HDR', 'conditions', '[{"kind":"release_title","value":"HDR"}]')`)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil || id != 10 {
		t.Fatalf("a conditions trait after 9 was removed: id %d %v", id, err)
	}
}
