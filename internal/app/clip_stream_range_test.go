package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A caller that has already spent something to serve the response (a share
// link claims a download slot first) must not offer ranges: "bytes=0-0" would
// otherwise consume a one-time link for a single byte.
func TestServeStoredClipRangesCanBeDisabled(t *testing.T) {
	db := newServerTestDB(t)
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (30, 'text/plain', '0123456789', 'a.txt')`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/tok", nil)
	req.Header.Set("Range", "bytes=0-0")
	rec := httptest.NewRecorder()
	if !serveStoredClip(rec, req, db, 30, "attachment", false) {
		t.Fatal("serve failed")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (range ignored)", rec.Code)
	}
	if rec.Body.String() != "0123456789" {
		t.Fatalf("body = %q, want the whole clip", rec.Body.String())
	}
	if rec.Header().Get("Accept-Ranges") != "" {
		t.Fatal("Accept-Ranges advertised while ranges are disabled")
	}

	// An unsatisfiable range must not 416 a share link either.
	req = httptest.NewRequest(http.MethodGet, "/s/tok", nil)
	req.Header.Set("Range", "bytes=9999-")
	rec = httptest.NewRecorder()
	serveStoredClip(rec, req, db, 30, "attachment", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// The authenticated endpoint keeps ranges.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/clips/30/data", nil)
	req.Header.Set("Range", "bytes=2-4")
	rec = httptest.NewRecorder()
	serveStoredClip(rec, req, db, 30, "attachment", true)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "234" {
		t.Fatalf("ranged request: %d %q", rec.Code, rec.Body.String())
	}
}
