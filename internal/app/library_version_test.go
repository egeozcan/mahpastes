package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mustLibraryVersion(t *testing.T, a *App) int64 {
	t.Helper()
	v, err := a.GetLibraryVersion()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Every kind of write a gallery would show moves the counter, whoever makes
// it (here straight SQL, as a plugin or the REST API would); reads do not.
func TestLibraryVersionMovesOnEveryGalleryWrite(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	type step struct {
		name string
		run  func(t *testing.T)
	}
	var clipID, tagID int64
	steps := []step{
		{"insert clip", func(t *testing.T) { clipID = insertTestClip(t, a, "a.txt", "text/plain", []byte("a")) }},
		{"create tag", func(t *testing.T) { tagID = mustCreateTag(t, a, "work") }},
		{"tag clip", func(t *testing.T) {
			if err := a.AddTagToClip(clipID, tagID); err != nil {
				t.Fatal(err)
			}
		}},
		{"rename tag", func(t *testing.T) {
			if err := a.UpdateTag(tagID, "job", ""); err != nil {
				t.Fatal(err)
			}
		}},
		{"archive via SQL", func(t *testing.T) {
			if _, err := a.db.Exec(`UPDATE clips SET is_archived = 1 WHERE id = ?`, clipID); err != nil {
				t.Fatal(err)
			}
		}},
		{"untag", func(t *testing.T) {
			if err := a.RemoveTagFromClip(clipID, tagID); err != nil {
				t.Fatal(err)
			}
		}},
		{"delete clip", func(t *testing.T) {
			if _, err := a.db.Exec(`DELETE FROM clips WHERE id = ?`, clipID); err != nil {
				t.Fatal(err)
			}
		}},
	}

	prev := mustLibraryVersion(t, a)
	for _, step := range steps {
		step.run(t)
		got := mustLibraryVersion(t, a)
		if got <= prev {
			t.Fatalf("%s: version %d did not move past %d", step.name, got, prev)
		}
		prev = got
	}

	if _, err := a.GetClips(false, nil, nil, "created_at", "desc"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetTags(); err != nil {
		t.Fatal(err)
	}
	if got := mustLibraryVersion(t, a); got != prev {
		t.Fatalf("reads moved the version from %d to %d", prev, got)
	}
}

// Re-running the schema setup on an existing library keeps the counter (and
// its triggers) rather than resetting it.
func TestLibraryVersionSurvivesReopen(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()
	insertTestClip(t, a, "a.txt", "text/plain", []byte("a"))
	before := mustLibraryVersion(t, a)
	if before == 0 {
		t.Fatal("insert did not move the version")
	}
	if err := ensureLibraryVersion(a.db); err != nil {
		t.Fatal(err)
	}
	if got := mustLibraryVersion(t, a); got != before {
		t.Fatalf("version reset from %d to %d", before, got)
	}
	insertTestClip(t, a, "b.txt", "text/plain", []byte("b"))
	if got := mustLibraryVersion(t, a); got != before+1 {
		t.Fatalf("version = %d after one insert, want %d", got, before+1)
	}
}

// The counter moves on writes outside any subtree, so tag-scoped keys are
// refused; unscoped keys get it.
func TestLibraryVersionRouteRefusesScopedKeys(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()
	work := mustCreateTag(t, a, "work")
	insertTestClip(t, a, "a.txt", "text/plain", []byte("a"))
	want := mustLibraryVersion(t, a)
	manager := NewAPIManager(a)

	call := func(key *apiKeyContext) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/v1/library/version", nil)
		rec := httptest.NewRecorder()
		manager.handleLibraryVersion(rec, req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, key)))
		return rec
	}

	if rec := call(&apiKeyContext{KeyID: 1, Role: "viewer", ScopedTagID: work}); rec.Code != http.StatusForbidden {
		t.Fatalf("scoped key: code = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	rec := call(&apiKeyContext{KeyID: 2, Role: "viewer"})
	if rec.Code != http.StatusOK {
		t.Fatalf("unscoped key: code = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Version int64 `json:"version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Version != want {
		t.Fatalf("version = %d, want %d", body.Version, want)
	}
}
