package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The REST clip shapes carried no expires_at, so the web UI could set an
// expiry but never show the badge or offer to cancel it. Every shape reports
// it now, and reports null (not an absent field) for a clip without one: the
// gallery patches a card from the single-clip row, and after a cancel it has
// to be told the expiry is gone.
func TestAPI_ClipShapesReportExpiresAt(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	expires := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	if _, err := app.db.Exec(`INSERT INTO clips (id, content_type, data, filename, content_hash, expires_at) VALUES
		(1, 'text/plain', 'a', 'temp.txt', 'h1', ?),
		(2, 'text/plain', 'b', 'keep.txt', 'h2', NULL)`, expires); err != nil {
		t.Fatalf("insert clips: %v", err)
	}
	key := &apiKeyContext{KeyID: 1, Role: "viewer"}
	am := NewAPIManager(app)
	get := func(handler http.HandlerFunc, target string, pathID string) map[string]json.RawMessage {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req = req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, key))
		if pathID != "" {
			req.SetPathValue("id", pathID)
		}
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d: %s", target, rec.Code, rec.Body.String())
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("GET %s: decode: %v", target, err)
		}
		return body
	}
	check := func(what string, clip map[string]json.RawMessage, want *time.Time) {
		t.Helper()
		raw, ok := clip["expires_at"]
		if !ok {
			t.Fatalf("%s: expires_at is absent; it must be present (null when there is none)", what)
		}
		var got *time.Time
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("%s: expires_at %s: %v", what, raw, err)
		}
		switch {
		case want == nil && got != nil:
			t.Fatalf("%s: expires_at = %v, want null", what, got)
		case want != nil && (got == nil || !got.Equal(*want)):
			t.Fatalf("%s: expires_at = %v, want %v", what, got, want)
		}
	}
	clipsByID := func(body map[string]json.RawMessage) map[int64]map[string]json.RawMessage {
		t.Helper()
		var list []map[string]json.RawMessage
		if err := json.Unmarshal(body["clips"], &list); err != nil {
			t.Fatalf("decode clips: %v", err)
		}
		out := map[int64]map[string]json.RawMessage{}
		for _, c := range list {
			var id int64
			_ = json.Unmarshal(c["id"], &id)
			out[id] = c
		}
		return out
	}

	check("single clip with expiry", get(am.handleGetClip, "/api/v1/clips/1", "1"), &expires)
	check("single clip without", get(am.handleGetClip, "/api/v1/clips/2", "2"), nil)

	legacy := clipsByID(get(am.handleListClips, "/api/v1/clips?limit=10", ""))
	check("plain listing with expiry", legacy[1], &expires)
	check("plain listing without", legacy[2], nil)

	// The gallery's paged listing (the App-backed path).
	paged := clipsByID(get(am.handleListClips, "/api/v1/clips?limit=10&offset=0&sort=created_at&order=desc&untagged=true", ""))
	check("paged listing with expiry", paged[1], &expires)
	check("paged listing without", paged[2], nil)
}
