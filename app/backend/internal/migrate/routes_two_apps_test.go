package migrate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/secrets"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/webserver"
	"github.com/HolyArrs/holyarrs/app/backend/internal/migrate/secretsfile"
)

// Two Sonarrs read one after the other stay two reads with their own
// address, and each keeps its own key: Read again with the first one's
// address uses the first one's key (issue 165).
func TestTwoSonarrsKeepTheirReadsAndKeys(t *testing.T) {
	st, err := database.Open(t.TempDir(), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	v, err := secrets.Open(context.Background(), st, make([]byte, secrets.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	sonarr := func(key, title string) *httptest.Server {
		return fakeAPI(t, "X-Api-Key", key, map[string]any{
			"/api/v3/system/status": map[string]any{"appName": "Sonarr", "version": "4.0.20"},
			"/api/v3/series":        []any{map[string]any{"id": 1, "title": title, "tvdbId": 81189, "path": "/tv/" + title}},
		})
	}
	first, second := sonarr("key-first", "One"), sonarr("key-second", "Two")
	r := webserver.NewRouter()
	h := &Handlers{Store: st, Secrets: v, Mover: func() *Mover { return nil }, SafeMode: func() bool { return false }}
	h.Routes(r)
	owner := webserver.ContextWith(httptest.NewRequest("GET", "/", nil).Context(), webserver.Caller{UserID: 1, Name: "owner", Role: webserver.RoleOwner, Via: "session"})
	do := func(body string) map[string]any {
		t.Helper()
		req := httptest.NewRequest("POST", "/api/v1/migrate/read", strings.NewReader(body)).WithContext(owner)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("read %s: %d %s", body, rec.Code, rec.Body.String())
		}
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	do(`{"kind":"sonarr","url":"` + first.URL + `","key":"key-first","allow_local_network":true}`)
	do(`{"kind":"sonarr","url":"` + second.URL + `","key":"key-second","allow_local_network":true}`)
	reads, err := List(context.Background(), st)
	if err != nil || len(reads) != 2 {
		t.Fatalf("reads %d %v", len(reads), err)
	}
	if reads[0].Address != strings.ToLower(second.URL) || reads[1].Address != strings.ToLower(first.URL) {
		t.Fatalf("read addresses %q %q", reads[0].Address, reads[1].Address)
	}
	saved, err := secretsfile.ListMoveCredentials(context.Background(), st, v)
	if err != nil || len(saved) != 2 {
		t.Fatalf("saved keys %+v %v", saved, err)
	}
	// Read again with the first Sonarr's saved key: its own key and address.
	got := do(`{"kind":"sonarr","use_saved_key":true,"url":"` + first.URL + `"}`)
	if snap, _ := got["snapshot"].(map[string]any); snap["address"] != strings.ToLower(first.URL) {
		t.Fatalf("read again went to %v", snap["address"])
	}
	// Forgetting the second read forgets only the second key.
	req := httptest.NewRequest("DELETE", "/api/v1/migrate/snapshots/2", nil).WithContext(owner)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("forget: %d %s", rec.Code, rec.Body.String())
	}
	if c, ok, _ := secretsfile.ReadMoveCredential(context.Background(), st, v, "sonarr", ""); !ok || c.APIKey != "key-first" {
		t.Fatalf("the first Sonarr lost its key: %+v %v", c, ok)
	}
}
