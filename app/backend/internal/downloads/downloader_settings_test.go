package downloads

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/secrets"
)

// Hooks passed to Add run in the insert's transaction: they see the new id,
// and a failing hook leaves no Downloader behind.
func TestAddThenHookSameTx(t *testing.T) {
	ctx := context.Background()
	s, err := database.Open(t.TempDir(), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	v, err := secrets.Open(ctx, s, make([]byte, secrets.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	in := Input{Name: "t", Adapter: "transmission", URL: "http://10.0.0.7:9091"}
	var got int64
	if _, err := Add(ctx, s, v, in, domain.System, func(tx *sql.Tx, id int64) error {
		got = id
		_, e := tx.Exec("UPDATE downloaders SET state='disabled' WHERE id=?", id)
		return e
	}); err != nil || got == 0 {
		t.Fatal(err, got)
	}
	var state string
	if err := s.Read().QueryRow("SELECT state FROM downloaders WHERE id=?", got).Scan(&state); err != nil || state != "disabled" {
		t.Fatal(state, err)
	}
	in.Name = "t2"
	if _, err := Add(ctx, s, v, in, domain.System, func(*sql.Tx, int64) error { return errors.New("boom") }); err == nil {
		t.Fatal("hook error lost")
	}
	var n int
	if err := s.Read().QueryRow("SELECT count(*) FROM downloaders WHERE name='t2'").Scan(&n); err != nil || n != 0 {
		t.Fatal("not rolled back", n, err)
	}
	// A secret stored in the same transaction is rolled back too.
	in.Name, in.Adapter, in.URL, in.Secret = "t3", "sabnzbd", "http://10.0.0.8:8080", "key-t3"
	if _, err := Add(ctx, s, v, in, domain.System, func(*sql.Tx, int64) error { return errors.New("boom") }); err == nil {
		t.Fatal("hook error lost")
	}
	vals, _ := v.Values(ctx)
	for _, x := range vals {
		if x == "key-t3" {
			t.Fatal("secret kept after rollback")
		}
	}
}

// A new Downloader manages only its own categories unless told otherwise;
// the after-import category and seeding goal are checked.
func TestDownloaderDownloadHandlingSettings(t *testing.T) {
	srv, _ := dlServer(t)
	var v DownloaderView
	_ = json.Unmarshal(do(t, "POST", srv.URL+"/api/v1/downloaders", `{"name":"Q","kind":"qbittorrent","url":"http://qb.lab:8080","secret":"k"}`, http.StatusCreated), &v)
	if !v.OwnCategoriesOnly || v.PostImportCategory != "" || v.SeedGoal.Ratio != nil || v.SeedGoal.Minutes != nil {
		t.Fatalf("new Downloader %+v", v)
	}
	put := srv.URL + "/api/v1/downloaders/" + itoa(v.ID)
	do(t, "PUT", put, `{"name":"Q","post_import_category":"holyarrs"}`, http.StatusBadRequest)
	do(t, "PUT", put, `{"name":"Q","post_import_category":"a/b"}`, http.StatusBadRequest)
	do(t, "PUT", put, `{"name":"Q","seed_goal":{"ratio":0}}`, http.StatusBadRequest)
	do(t, "PUT", put, `{"name":"Q","seed_goal":{"minutes":0}}`, http.StatusBadRequest)
	_ = json.Unmarshal(do(t, "PUT", put, `{"name":"Q","post_import_category":"holyarrs-imported","own_categories_only":false,"seed_goal":{"ratio":2,"minutes":23040}}`, http.StatusOK), &v)
	if v.OwnCategoriesOnly || v.PostImportCategory != "holyarrs-imported" || v.SeedGoal.Ratio == nil || *v.SeedGoal.Ratio != 2 || *v.SeedGoal.Minutes != 23040 {
		t.Fatalf("saved %+v", v)
	}
	// Left out of an edit, they stay.
	_ = json.Unmarshal(do(t, "PUT", put, `{"name":"Q2"}`, http.StatusOK), &v)
	if v.PostImportCategory != "holyarrs-imported" || v.SeedGoal.Ratio == nil {
		t.Fatalf("kept %+v", v)
	}
	// Only qBittorrent moves torrents between categories or reports a stopped seed.
	var tr DownloaderView
	_ = json.Unmarshal(do(t, "POST", srv.URL+"/api/v1/downloaders", `{"name":"T","kind":"transmission","url":"http://tr.lab:9091"}`, http.StatusCreated), &tr)
	tput := srv.URL + "/api/v1/downloaders/" + itoa(tr.ID)
	do(t, "PUT", tput, `{"name":"T","post_import_category":"done"}`, http.StatusBadRequest)
	do(t, "PUT", tput, `{"name":"T","seed_goal":{"ratio":2}}`, http.StatusBadRequest)
}
