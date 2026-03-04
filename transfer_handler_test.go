package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
