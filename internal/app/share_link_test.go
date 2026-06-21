package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// setupShareLinkTest builds a minimal App + APIManager backed by a real
// initDB() database. It deliberately avoids setupTestApp (which spins up a
// libp2p ShareManager) since share links only touch the DB.
func setupShareLinkTest(t *testing.T) (*APIManager, *App, int64, func()) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dir)

	db, err := initDB()
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	app := &App{db: db}
	am := &APIManager{app: app, shareLimiter: newLoginRateLimiter()}

	res, err := db.Exec("INSERT INTO clips (content_type, data, filename) VALUES (?, ?, ?)",
		"text/plain", []byte("hello share"), "hello.txt")
	if err != nil {
		t.Fatalf("insert clip: %v", err)
	}
	clipID, _ := res.LastInsertId()

	return am, app, clipID, func() { db.Close() }
}

func shareMux(am *APIManager) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /s/{token}", am.handleShareView)
	return mux
}

func getShare(t *testing.T, mux *http.ServeMux, token string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/s/"+token, nil))
	return rec
}

func TestShareLink_CreateAndView(t *testing.T) {
	am, app, clipID, cleanup := setupShareLinkTest(t)
	defer cleanup()

	result, err := am.CreateShareLink(clipID, "draft", 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}
	if len(result.Token) != shareTokenBytes*2 {
		t.Fatalf("token length = %d, want %d hex chars", len(result.Token), shareTokenBytes*2)
	}
	if result.Path != "/s/"+result.Token {
		t.Fatalf("path = %q, want /s/<token>", result.Path)
	}
	if result.Info.TokenPrefix != result.Token[:8]+"..." {
		t.Fatalf("prefix = %q", result.Info.TokenPrefix)
	}

	// The plaintext token must never be stored — only its SHA-256 hash.
	wantHash := sha256.Sum256([]byte(result.Token))
	var storedHash string
	if err := app.db.QueryRow("SELECT token_hash FROM share_links WHERE id = ?", result.Info.ID).Scan(&storedHash); err != nil {
		t.Fatalf("read hash: %v", err)
	}
	if storedHash != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("stored hash mismatch")
	}
	// No row should match the plaintext token in the hash column.
	var plainMatches int
	if err := app.db.QueryRow("SELECT COUNT(*) FROM share_links WHERE token_hash = ?", result.Token).Scan(&plainMatches); err != nil {
		t.Fatalf("plaintext probe: %v", err)
	}
	if plainMatches != 0 {
		t.Fatalf("plaintext token must not appear in token_hash")
	}

	mux := shareMux(am)
	rec := getShare(t, mux, result.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "hello share" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	// Stored-XSS defenses must be present on the public path.
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("nosniff = %q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'none'; sandbox" {
		t.Errorf("CSP = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got == "" || got[:10] != "attachment" {
		t.Errorf("Content-Disposition = %q, want attachment", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestShareLink_RevokeIsInstant(t *testing.T) {
	am, _, clipID, cleanup := setupShareLinkTest(t)
	defer cleanup()

	result, err := am.CreateShareLink(clipID, "", 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}
	mux := shareMux(am)
	if rec := getShare(t, mux, result.Token); rec.Code != http.StatusOK {
		t.Fatalf("pre-revoke status = %d, want 200", rec.Code)
	}

	if err := am.RevokeShareLink(result.Info.ID); err != nil {
		t.Fatalf("RevokeShareLink: %v", err)
	}
	if rec := getShare(t, mux, result.Token); rec.Code != http.StatusNotFound {
		t.Fatalf("post-revoke status = %d, want 404", rec.Code)
	}

	// Double-revoke is rejected.
	if err := am.RevokeShareLink(result.Info.ID); err == nil {
		t.Fatalf("second revoke should error")
	}
}

func TestShareLink_Expired(t *testing.T) {
	am, app, clipID, cleanup := setupShareLinkTest(t)
	defer cleanup()

	result, err := am.CreateShareLink(clipID, "", 3600, 0, 0)
	if err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}
	// Force the expiry into the past.
	if _, err := app.db.Exec("UPDATE share_links SET expires_at = datetime('now','-1 hour') WHERE id = ?", result.Info.ID); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if rec := getShare(t, shareMux(am), result.Token); rec.Code != http.StatusNotFound {
		t.Fatalf("expired status = %d, want 404", rec.Code)
	}
}

func TestShareLink_MaxDownloads(t *testing.T) {
	am, app, clipID, cleanup := setupShareLinkTest(t)
	defer cleanup()

	result, err := am.CreateShareLink(clipID, "", 0, 2, 0)
	if err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}
	mux := shareMux(am)
	for i := 0; i < 2; i++ {
		if rec := getShare(t, mux, result.Token); rec.Code != http.StatusOK {
			t.Fatalf("download %d status = %d, want 200", i+1, rec.Code)
		}
	}
	if rec := getShare(t, mux, result.Token); rec.Code != http.StatusNotFound {
		t.Fatalf("over-cap status = %d, want 404", rec.Code)
	}
	var count int64
	if err := app.db.QueryRow("SELECT download_count FROM share_links WHERE id = ?", result.Info.ID).Scan(&count); err != nil {
		t.Fatalf("read count: %v", err)
	}
	if count != 2 {
		t.Fatalf("download_count = %d, want 2 (over-cap request must not increment)", count)
	}
}

func TestShareLink_ArchivedClipNotServed(t *testing.T) {
	am, app, clipID, cleanup := setupShareLinkTest(t)
	defer cleanup()

	result, err := am.CreateShareLink(clipID, "", 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}
	if _, err := app.db.Exec("UPDATE clips SET is_archived = 1 WHERE id = ?", clipID); err != nil {
		t.Fatalf("archive clip: %v", err)
	}
	if rec := getShare(t, shareMux(am), result.Token); rec.Code != http.StatusNotFound {
		t.Fatalf("archived-clip status = %d, want 404", rec.Code)
	}
}

func TestShareLink_UnknownTokenIsUniform404(t *testing.T) {
	am, _, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	mux := shareMux(am)
	for _, tok := range []string{"deadbeef", "not-a-real-token", "0000000000000000000000000000000000000000000000000000000000000000"} {
		if rec := getShare(t, mux, tok); rec.Code != http.StatusNotFound {
			t.Fatalf("token %q status = %d, want 404", tok, rec.Code)
		}
	}
}

func TestShareLink_DeletingClipCascadesLink(t *testing.T) {
	am, app, clipID, cleanup := setupShareLinkTest(t)
	defer cleanup()

	result, err := am.CreateShareLink(clipID, "", 0, 0, 0)
	if err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}
	if _, err := app.db.Exec("DELETE FROM clips WHERE id = ?", clipID); err != nil {
		t.Fatalf("delete clip: %v", err)
	}
	var count int
	if err := app.db.QueryRow("SELECT COUNT(*) FROM share_links WHERE id = ?", result.Info.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("share_links row should cascade-delete with its clip, found %d", count)
	}
}

func TestShareLink_CreateForMissingClipFails(t *testing.T) {
	am, _, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	if _, err := am.CreateShareLink(999999, "", 0, 0, 0); err == nil {
		t.Fatalf("expected error for missing clip")
	}
}

func TestShareLink_FutureExpiryServesInAnyTimezone(t *testing.T) {
	am, _, clipID, cleanup := setupShareLinkTest(t)
	defer cleanup()

	// Force a negative-offset local zone. With the original code (binding a raw
	// time.Time) the driver stored a local-offset string that mis-compared
	// against CURRENT_TIMESTAMP and 404'd a still-valid link; storing UTC text +
	// datetime() comparison fixes it.
	if loc, err := time.LoadLocation("America/New_York"); err == nil {
		orig := time.Local
		time.Local = loc
		defer func() { time.Local = orig }()
	}

	result, err := am.CreateShareLink(clipID, "", 3600, 0, 0)
	if err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}
	if rec := getShare(t, shareMux(am), result.Token); rec.Code != http.StatusOK {
		t.Fatalf("future-expiring link status = %d, want 200", rec.Code)
	}
}

func TestShareLink_HeadDoesNotConsumeSlot(t *testing.T) {
	am, app, clipID, cleanup := setupShareLinkTest(t)
	defer cleanup()

	result, err := am.CreateShareLink(clipID, "", 0, 1, 0) // one-time link
	if err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}
	mux := shareMux(am)

	// A HEAD (e.g. a link-unfurl bot) must not drain the one allowed download.
	recH := httptest.NewRecorder()
	mux.ServeHTTP(recH, httptest.NewRequest("HEAD", "/s/"+result.Token, nil))
	if recH.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d, want 200", recH.Code)
	}
	var count int64
	if err := app.db.QueryRow("SELECT download_count FROM share_links WHERE id = ?", result.Info.ID).Scan(&count); err != nil {
		t.Fatalf("read count: %v", err)
	}
	if count != 0 {
		t.Fatalf("HEAD consumed a download slot: download_count = %d", count)
	}
	// The real recipient's GET still works.
	if rec := getShare(t, mux, result.Token); rec.Code != http.StatusOK {
		t.Fatalf("GET after HEAD status = %d, want 200", rec.Code)
	}
}

func TestShareLink_ScopedKeyListAndRevoke(t *testing.T) {
	am, app, clipA, cleanup := setupShareLinkTest(t)
	defer cleanup()

	resB, err := app.db.Exec("INSERT INTO clips (content_type, data, filename) VALUES (?, ?, ?)", "text/plain", []byte("b"), "b.txt")
	if err != nil {
		t.Fatalf("insert clipB: %v", err)
	}
	clipB, _ := resB.LastInsertId()

	rTagA, _ := app.db.Exec("INSERT INTO tags (name, color) VALUES (?, ?)", "teamA", "#fff")
	tagA, _ := rTagA.LastInsertId()
	rTagB, _ := app.db.Exec("INSERT INTO tags (name, color) VALUES (?, ?)", "teamB", "#fff")
	tagB, _ := rTagB.LastInsertId()
	if _, err := app.db.Exec("INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)", clipA, tagA); err != nil {
		t.Fatalf("tag clipA: %v", err)
	}
	if _, err := app.db.Exec("INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)", clipB, tagB); err != nil {
		t.Fatalf("tag clipB: %v", err)
	}

	linkA, _ := am.CreateShareLink(clipA, "a", 0, 0, 0)
	linkB, _ := am.CreateShareLink(clipB, "b", 0, 0, 0)

	scoped := func(req *http.Request) *http.Request {
		ctx := context.WithValue(req.Context(), apiKeyContextKey, &apiKeyContext{KeyID: 1, Role: "admin", ScopedTagID: tagA})
		return req.WithContext(ctx)
	}

	// List returns only the in-scope link.
	recL := httptest.NewRecorder()
	am.handleListShareLinks(recL, scoped(httptest.NewRequest("GET", "/api/v1/links", nil)))
	if recL.Code != http.StatusOK {
		t.Fatalf("list status = %d", recL.Code)
	}
	var listed []ShareLinkInfo
	if err := json.Unmarshal(recL.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != linkA.Info.ID {
		t.Fatalf("scoped list = %+v, want only linkA (id %d)", listed, linkA.Info.ID)
	}

	// Revoking an out-of-scope link is a uniform 404 and leaves it active.
	recR := httptest.NewRecorder()
	reqR := scoped(httptest.NewRequest("DELETE", "/api/v1/links/"+strconv.FormatInt(linkB.Info.ID, 10), nil))
	reqR.SetPathValue("id", strconv.FormatInt(linkB.Info.ID, 10))
	am.handleRevokeShareLink(recR, reqR)
	if recR.Code != http.StatusNotFound {
		t.Fatalf("out-of-scope revoke status = %d, want 404", recR.Code)
	}
	var revoked int
	app.db.QueryRow("SELECT is_revoked FROM share_links WHERE id = ?", linkB.Info.ID).Scan(&revoked)
	if revoked != 0 {
		t.Fatalf("out-of-scope link must remain active")
	}

	// Revoking an in-scope link succeeds.
	recR2 := httptest.NewRecorder()
	reqR2 := scoped(httptest.NewRequest("DELETE", "/api/v1/links/"+strconv.FormatInt(linkA.Info.ID, 10), nil))
	reqR2.SetPathValue("id", strconv.FormatInt(linkA.Info.ID, 10))
	am.handleRevokeShareLink(recR2, reqR2)
	if recR2.Code != http.StatusNoContent {
		t.Fatalf("in-scope revoke status = %d, want 204", recR2.Code)
	}
}

func TestShareLink_ListExposesPrefixNotToken(t *testing.T) {
	am, _, clipID, cleanup := setupShareLinkTest(t)
	defer cleanup()

	result, err := am.CreateShareLink(clipID, "labelled", 0, 5, 0)
	if err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}
	links, err := am.ListShareLinks()
	if err != nil {
		t.Fatalf("ListShareLinks: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("len = %d, want 1", len(links))
	}
	l := links[0]
	if l.TokenPrefix != result.Token[:8]+"..." {
		t.Errorf("prefix = %q", l.TokenPrefix)
	}
	if l.Name != "labelled" {
		t.Errorf("name = %q", l.Name)
	}
	if l.MaxDownloads == nil || *l.MaxDownloads != 5 {
		t.Errorf("max_downloads = %v, want 5", l.MaxDownloads)
	}
}
