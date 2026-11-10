package database

import (
	"context"
	"database/sql"
	"testing"
)

// 0099 widens own_traits to the conditions match kind, keeps every row and
// keeps the id counter, so a removed trait's id is never given again.
func TestOwnTraitsKeptByConditionsMigration(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	old, err := Open(dir, Options{MainMigrations: upTo(t, 98)})
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
