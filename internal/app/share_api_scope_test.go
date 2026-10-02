package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// shareAPIHarness drives the real registered route table (cors → auth →
// route wrappers → handler) with real API keys, so these tests cover the
// guards as they are wired at registration time, not just the handlers.
type shareAPIHarness struct {
	t    *testing.T
	app  *App
	am   *APIManager
	keyN int
}

func newShareAPIHarness(t *testing.T) *shareAPIHarness {
	t.Helper()
	app, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	am := NewAPIManager(app)
	if _, err := am.Start(0, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = am.Stop() })
	return &shareAPIHarness{t: t, app: app, am: am}
}

func (h *shareAPIHarness) tag(name string) int64 {
	h.t.Helper()
	tag, err := h.app.CreateTag(name)
	if err != nil {
		h.t.Fatalf("CreateTag(%q): %v", name, err)
	}
	return tag.ID
}

func (h *shareAPIHarness) key(role string, scopedTagID int64) string {
	h.t.Helper()
	h.keyN++
	res, err := h.am.CreateKey(fmt.Sprintf("%s-%d", role, h.keyN), role, scopedTagID)
	if err != nil {
		h.t.Fatalf("CreateKey(%s, scope=%d): %v", role, scopedTagID, err)
	}
	return res.Key
}

func (h *shareAPIHarness) do(method, path, key, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Authorization", "Bearer "+key)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.am.server.Handler.ServeHTTP(rec, req)
	return rec
}

func (h *shareAPIHarness) count(query string, args ...any) int {
	h.t.Helper()
	var n int
	if err := h.app.db.QueryRow(query, args...).Scan(&n); err != nil {
		h.t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// P2P share/follow is instance-global, so a tag-scoped key — whatever its
// role — must not reach any /api/v1/share* route. Before the fix a viewer
// scoped to `public` read the share string (the full follow capability) for
// `private`, and an admin scoped to `public` could publish any tag.
func TestShareRoutesRejectTagScopedKeys(t *testing.T) {
	h := newShareAPIHarness(t)
	public := h.tag("public")
	private := h.tag("private")
	other := h.tag("other")
	if _, err := h.app.shareManager.StartShare(private); err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	scopedViewer := h.key("viewer", public)
	scopedAdmin := h.key("admin", public)

	for _, path := range []string{"/api/v1/share", "/api/v1/share/logs"} {
		rec := h.do(http.MethodGet, path, scopedViewer, "")
		if rec.Code != http.StatusForbidden {
			t.Errorf("scoped viewer GET %s: code = %d, want 403 (body %q)", path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), ShareStringPrefix) {
			t.Errorf("scoped viewer GET %s leaked a share string: %q", path, rec.Body.String())
		}
	}

	// Malformed share strings and unknown follow ids keep the pre-fix
	// behaviour fast (400 without dialing), so the red run fails on the
	// status code rather than hanging on the network.
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/share", ""},
		{http.MethodGet, "/api/v1/share/logs", ""},
		{http.MethodPost, "/api/v1/share/publish", fmt.Sprintf(`{"tag_id":%d}`, other)},
		{http.MethodDelete, fmt.Sprintf("/api/v1/share/publish/%d", private), ""},
		{http.MethodPut, fmt.Sprintf("/api/v1/share/publish/%d/pause", private), "{}"},
		{http.MethodDelete, fmt.Sprintf("/api/v1/share/publish/%d/pause", private), ""},
		{http.MethodPost, "/api/v1/share/follow", `{"share_string":"mp-share:v1:AAAA","local_tag_name":"public/in"}`},
		{http.MethodPost, "/api/v1/share/test-follow", `{"share_string":"mp-share:v1:AAAA"}`},
		{http.MethodPost, "/api/v1/share/follow-direct", `{"share_string":"mp-share:v1:AAAA","local_tag_name":"public/in"}`},
		{http.MethodDelete, "/api/v1/share/follow/999", ""},
		{http.MethodPost, "/api/v1/share/follow/999/reconnect", "{}"},
		{http.MethodPut, "/api/v1/share/follow/999/pause", "{}"},
		{http.MethodDelete, "/api/v1/share/follow/999/pause", ""},
		{http.MethodPut, "/api/v1/share/follow/999/tag", `{"local_tag_name":"public/in"}`},
	}
	for _, rt := range routes {
		rec := h.do(rt.method, rt.path, scopedAdmin, rt.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("scoped admin %s %s: code = %d, want 403 (body %q)", rt.method, rt.path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), ShareStringPrefix) {
			t.Errorf("scoped admin %s %s leaked a share string: %q", rt.method, rt.path, rec.Body.String())
		}
	}

	// The rejected mutations must not have landed.
	if n := h.count(`SELECT COUNT(*) FROM shares WHERE tag_id = ?`, other); n != 0 {
		t.Errorf("scoped admin publish created %d shares rows for an out-of-scope tag", n)
	}
	if n := h.count(`SELECT COUNT(*) FROM shares WHERE tag_id = ? AND status = 'active'`, private); n != 1 {
		t.Errorf("scoped admin stop/pause touched the existing share: %d active rows, want 1", n)
	}
	if n := h.count(`SELECT COUNT(*) FROM follows`); n != 0 {
		t.Errorf("scoped admin follow created %d follows rows", n)
	}
}

// Unscoped viewers keep GET /api/v1/share (the server-mode web UI polls it
// every 2s) but only an admin may see the share string itself.
func TestShareStatusRedactsShareStringForNonAdmin(t *testing.T) {
	h := newShareAPIHarness(t)
	private := h.tag("private")
	info, err := h.app.shareManager.StartShare(private)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}
	if !strings.HasPrefix(info.ShareString, ShareStringPrefix) {
		t.Fatalf("StartShare returned %q, want a share string", info.ShareString)
	}

	decode := func(rec *httptest.ResponseRecorder) []ShareInfo {
		t.Helper()
		var body struct {
			Shares []ShareInfo `json:"shares"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %q: %v", rec.Body.String(), err)
		}
		return body.Shares
	}

	for _, role := range []string{"viewer", "editor"} {
		rec := h.do(http.MethodGet, "/api/v1/share", h.key(role, 0), "")
		if rec.Code != http.StatusOK {
			t.Fatalf("unscoped %s GET /api/v1/share: code = %d, want 200 (body %q)", role, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), ShareStringPrefix) {
			t.Errorf("unscoped %s GET /api/v1/share leaked a share string: %q", role, rec.Body.String())
		}
		shares := decode(rec)
		if len(shares) != 1 || shares[0].TagID != private {
			t.Errorf("unscoped %s should still see the share listed, got %+v", role, shares)
		}

		logs := h.do(http.MethodGet, "/api/v1/share/logs", h.key(role, 0), "")
		if logs.Code != http.StatusOK {
			t.Errorf("unscoped %s GET /api/v1/share/logs: code = %d, want 200", role, logs.Code)
		}
		if strings.Contains(logs.Body.String(), ShareStringPrefix) {
			t.Errorf("unscoped %s share logs leaked a share string: %q", role, logs.Body.String())
		}
	}

	rec := h.do(http.MethodGet, "/api/v1/share", h.key("admin", 0), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("unscoped admin GET /api/v1/share: code = %d, want 200", rec.Code)
	}
	if shares := decode(rec); len(shares) != 1 || shares[0].ShareString != info.ShareString {
		t.Errorf("unscoped admin should get the share string %q, got %+v", info.ShareString, shares)
	}

	// The desktop ShareService binding reads ShareManager.GetShareStatus
	// directly; the local user must keep getting the string there.
	shares, _ := h.app.shareManager.GetShareStatus()
	if len(shares) != 1 || shares[0].ShareString != info.ShareString {
		t.Errorf("ShareManager.GetShareStatus must keep the share string for the desktop binding, got %+v", shares)
	}
}

// Share events are broadcast to every unscoped SSE session regardless of
// role, so their payloads must never carry the share string. The frontend
// only uses them as a cue to re-fetch status.
func TestShareEventsOmitShareString(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	tag, err := app.CreateTag("private")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	type event struct {
		name    string
		payload string
	}
	var mu sync.Mutex
	var events []event
	app.shareManager.SetEventFn(func(name string, data ...any) {
		b, err := json.Marshal(data)
		if err != nil {
			t.Errorf("marshal %s payload: %v", name, err)
		}
		mu.Lock()
		events = append(events, event{name, string(b)})
		mu.Unlock()
	})

	info, err := app.shareManager.StartShare(tag.ID)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}
	if !strings.HasPrefix(info.ShareString, ShareStringPrefix) {
		t.Fatalf("StartShare must still return the share string to its caller, got %q", info.ShareString)
	}
	if err := app.shareManager.PauseShare(tag.ID); err != nil {
		t.Fatalf("PauseShare: %v", err)
	}
	if err := app.shareManager.ResumeShare(tag.ID); err != nil {
		t.Fatalf("ResumeShare: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	updated := 0
	for _, ev := range events {
		if ev.name == "share:publication-updated" {
			updated++
		}
		if strings.Contains(ev.payload, ShareStringPrefix) {
			t.Errorf("%s payload carries a share string: %s", ev.name, ev.payload)
		}
	}
	// StartShare, PauseShare and ResumeShare each emit one.
	if updated < 3 {
		t.Errorf("saw %d share:publication-updated events, want >= 3 (events: %+v)", updated, events)
	}
}
