package database

import (
	"context"
	"database/sql"
	"testing"
)

// 0095 widens the move_credentials kinds and keeps every saved row.
func TestMoveCredentialsKeptByAllAppsMigration(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	old, err := Open(dir, Options{MainMigrations: upTo(t, 94)})
	if err != nil {
		t.Fatal(err)
	}
	err = old.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO move_credentials (kind, secret_id, created_at, created_by) VALUES ('radarr', 7, 'c', '{}'), ('seerr', 8, 'c', '{}')`)
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
	if err := s.Read().QueryRow("SELECT count(*) FROM move_credentials WHERE (kind = 'radarr' AND secret_id = 7) OR (kind = 'seerr' AND secret_id = 8)").Scan(&n); err != nil || n != 2 {
		t.Fatalf("rows kept %d %v", n, err)
	}
	err = s.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO move_credentials (kind, secret_id, created_at, created_by) VALUES ('qbittorrent', 9, 'c', '{}'), ('sabnzbd', 10, 'c', '{}'), ('overseerr', 11, 'c', '{}')`)
		return err
	})
	if err != nil {
		t.Fatalf("the new kinds: %v", err)
	}
}

// 0097 keeps every saved key with an empty address, lets a kind keep one key
// per address, and gives every kept read an address.
func TestMoveReadsByAddressMigration(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	old, err := Open(dir, Options{MainMigrations: upTo(t, 96)})
	if err != nil {
		t.Fatal(err)
	}
	err = old.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO move_credentials (kind, secret_id, created_at, created_by) VALUES ('sonarr', 7, 'c', '{}')`); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO migrate_snapshots (created_at, owner_id, app_kind, via, size, data) VALUES ('c', 1, 'sonarr', 'api', 2, '{}')`)
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
	var addr string
	if err := s.Read().QueryRow("SELECT address FROM move_credentials WHERE kind = 'sonarr' AND secret_id = 7").Scan(&addr); err != nil || addr != "" {
		t.Fatalf("kept key %q %v", addr, err)
	}
	if err := s.Read().QueryRow("SELECT address FROM migrate_snapshots").Scan(&addr); err != nil || addr != "" {
		t.Fatalf("kept read %q %v", addr, err)
	}
	err = s.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO move_credentials (kind, address, secret_id, created_at, created_by) VALUES ('sonarr', 'http://a:8989', 8, 'c', '{}'), ('sonarr', 'http://b:8989', 9, 'c', '{}')`)
		return err
	})
	if err != nil {
		t.Fatalf("two Sonarr keys: %v", err)
	}
}
