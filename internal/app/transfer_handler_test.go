package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestHandler(t *testing.T) (*TransferFileHandler, string) {
	t.Helper()
	tempDir := t.TempDir()
	app := &App{tempDir: tempDir}
	handler := &TransferFileHandler{app: app}
	app.transferHandler = handler
	return handler, tempDir
}

func TestTransferHandler_ServesValidFile(t *testing.T) {
	handler, tempDir := newTestHandler(t)

	if err := os.WriteFile(filepath.Join(tempDir, "5_photo.png"), []byte("png-data"), 0644); err != nil {
		t.Fatal(err)
	}

	token, err := generateTransferToken()
	if err != nil {
		t.Fatal(err)
	}
	handler.RegisterToken(token, "5_photo.png")

	req := httptest.NewRequest(http.MethodGet, "/transfer/"+token+"/5_photo.png", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "png-data" {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "image/png") {
		t.Fatalf("expected image/png content-type, got %q", ct)
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, "5_photo.png") {
		t.Fatalf("unexpected Content-Disposition: %q", cd)
	}
}

func TestTransferHandler_RejectsNonGET(t *testing.T) {
	handler, tempDir := newTestHandler(t)

	if err := os.WriteFile(filepath.Join(tempDir, "1_test.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	token, _ := generateTransferToken()
	handler.RegisterToken(token, "1_test.txt")

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/transfer/"+token+"/1_test.txt", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: expected 405, got %d", method, rec.Code)
		}
	}
}

func TestTransferHandler_RejectsInvalidToken(t *testing.T) {
	handler, tempDir := newTestHandler(t)

	if err := os.WriteFile(filepath.Join(tempDir, "5_photo.png"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	token, _ := generateTransferToken()
	handler.RegisterToken(token, "5_photo.png")

	// Wrong token
	req := httptest.NewRequest(http.MethodGet, "/transfer/badtoken123/5_photo.png", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for bad token, got %d", rec.Code)
	}

	// Token for different file
	req = httptest.NewRequest(http.MethodGet, "/transfer/"+token+"/other.png", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for mismatched file, got %d", rec.Code)
	}
}

func TestTransferHandler_RejectsNoToken(t *testing.T) {
	handler, _ := newTestHandler(t)

	// No token in path (old URL format)
	req := httptest.NewRequest(http.MethodGet, "/transfer/5_photo.png", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing token, got %d", rec.Code)
	}
}

func TestTransferHandler_PathTraversal(t *testing.T) {
	handler, tempDir := newTestHandler(t)

	// Create a file outside the temp dir
	outsideDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outsideDir, "secret.txt"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "safe.txt"), []byte("safe"), 0644); err != nil {
		t.Fatal(err)
	}

	token, _ := generateTransferToken()
	handler.RegisterToken(token, "safe.txt")

	traversalPaths := []string{
		"/transfer/" + token + "/../../etc/passwd",
		"/transfer/" + token + "/../secret.txt",
		"/transfer/" + token + "/..%2F..%2Fetc%2Fpasswd",
		"/transfer/" + token + "/.",
		"/transfer/" + token + "/..",
	}

	for _, path := range traversalPaths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("expected non-200 for traversal path %q, got %d", path, rec.Code)
		}
	}
}

func TestTransferHandler_MissingFile(t *testing.T) {
	handler, _ := newTestHandler(t)

	token, _ := generateTransferToken()
	handler.RegisterToken(token, "nonexistent.txt")

	req := httptest.NewRequest(http.MethodGet, "/transfer/"+token+"/nonexistent.txt", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestTransferHandler_EmptyTempDir(t *testing.T) {
	app := &App{tempDir: ""}
	handler := &TransferFileHandler{app: app}

	token, _ := generateTransferToken()
	handler.RegisterToken(token, "file.txt")

	req := httptest.NewRequest(http.MethodGet, "/transfer/"+token+"/file.txt", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestTransferHandler_NonTransferPath(t *testing.T) {
	handler, _ := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/other/path", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestTransferHandler_ServesMediaRangeFromLeasedTempFile(t *testing.T) {
	db := newServerTestDB(t)
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (7, 'video/mp4', '0123456789', 'movie.mp4')`); err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	app := &App{db: db, tempDir: tempDir}
	app.tempStore = NewTempClipStore(db, tempDir, 0, 0)
	handler := NewTransferFileHandler(app)
	app.transferHandler = handler

	prepared, err := app.PrepareClipMediaItem(7)
	if err != nil {
		t.Fatal(err)
	}
	// Playback is backed by a real file so the browser can seek it: SQLite
	// cannot seek into a blob, and re-reading it per range is quadratic.
	if prepared.AbsPath == "" {
		t.Fatal("media preview did not materialize a temp file")
	}
	if _, err := os.Stat(prepared.AbsPath); err != nil {
		t.Fatalf("temp file missing: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, prepared.TransferURL, nil)
	req.Header.Set("Range", "bytes=3-6")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "3456" {
		t.Fatalf("unexpected body %q", got)
	}
	if got := rec.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("Accept-Ranges = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline") {
		t.Fatalf("Content-Disposition = %q, want inline", got)
	}
}

func TestTransferHandler_ReusesLeasedMediaFile(t *testing.T) {
	db := newServerTestDB(t)
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (8, 'video/mp4', '0123456789', 'movie.mp4')`); err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	app := &App{db: db, tempDir: tempDir}
	app.tempStore = NewTempClipStore(db, tempDir, 0, 0)
	handler := NewTransferFileHandler(app)
	app.transferHandler = handler

	first, err := app.PrepareClipMediaItem(8)
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.PrepareClipMediaItem(8)
	if err != nil {
		t.Fatal(err)
	}
	// A second open must not recopy the clip: for a large video that copy is
	// the whole cost of playback.
	if first.AbsPath != second.AbsPath {
		t.Fatalf("recopied clip: %q then %q", first.AbsPath, second.AbsPath)
	}
}

func TestTransferHandler_RejectsNonVideoMediaPreview(t *testing.T) {
	db := newServerTestDB(t)
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (9, 'image/png', 'x', 'a.png')`); err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	app := &App{db: db, tempDir: tempDir}
	app.tempStore = NewTempClipStore(db, tempDir, 0, 0)
	app.transferHandler = NewTransferFileHandler(app)

	if _, err := app.PrepareClipMediaItem(9); err == nil {
		t.Fatal("expected non-video clip to be rejected")
	}
}

func TestGenerateTransferToken(t *testing.T) {
	token1, err := generateTransferToken()
	if err != nil {
		t.Fatal(err)
	}
	token2, err := generateTransferToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token1) != 32 { // 16 bytes = 32 hex chars
		t.Fatalf("expected 32-char token, got %d", len(token1))
	}
	if token1 == token2 {
		t.Fatal("expected unique tokens")
	}
}

func TestTransferHandler_PlaybackSlidesMediaLease(t *testing.T) {
	db := newServerTestDB(t)
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (11, 'video/mp4', '0123456789', 'movie.mp4')`); err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	app := &App{db: db, tempDir: tempDir}
	app.tempStore = NewTempClipStore(db, tempDir, 0, 0)
	handler := NewTransferFileHandler(app)
	app.transferHandler = handler

	prepared, err := app.PrepareClipMediaItem(11)
	if err != nil {
		t.Fatal(err)
	}

	// Age the file and the capability past halfway, as a long playback would.
	stale := time.Now().Add(-defaultTempLeaseTTL + time.Minute)
	if err := os.Chtimes(prepared.AbsPath, stale, stale); err != nil {
		t.Fatal(err)
	}
	handler.mu.Lock()
	var token string
	for tok, item := range handler.mediaTokens {
		token = tok
		item.expiresAt = time.Now().Add(time.Minute)
		handler.mediaTokens[tok] = item
	}
	handler.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, prepared.TransferURL, nil)
	req.Header.Set("Range", "bytes=0-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", rec.Code)
	}

	handler.mu.RLock()
	renewed := handler.mediaTokens[token]
	handler.mu.RUnlock()
	if time.Until(renewed.expiresAt) < defaultTempLeaseTTL/2 {
		t.Fatalf("capability was not extended: expires in %v", time.Until(renewed.expiresAt))
	}
	info, err := os.Stat(prepared.AbsPath)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(info.ModTime()) > time.Minute {
		t.Fatalf("file lease was not refreshed: mtime %v", info.ModTime())
	}
}

func TestTransferHandler_MediaUsesStoredContentType(t *testing.T) {
	db := newServerTestDB(t)
	// A video whose filename claims it is text. tempFilenameForClip keeps that
	// name, so guessing the type from the extension would serve text/plain and,
	// with nosniff, stop it playing.
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (12, 'video/mp4', '0123456789', 'recording.txt')`); err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	app := &App{db: db, tempDir: tempDir}
	app.tempStore = NewTempClipStore(db, tempDir, 0, 0)
	handler := NewTransferFileHandler(app)
	app.transferHandler = handler

	prepared, err := app.PrepareClipMediaItem(12)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, prepared.TransferURL, nil))
	if got := rec.Header().Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("Content-Type = %q, want video/mp4", got)
	}
}
