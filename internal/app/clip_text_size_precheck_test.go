package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// The editor's 16 MiB cap is a decline-to-open. Before GetClipText, desktop
// read the whole clip through GetClipData and shipped it over IPC only for the
// frontend to refuse it; these pin that an over-cap text clip comes back as
// {too_large, size} with no bytes, while images and in-cap text are untouched.

func insertSizedClip(t *testing.T, app *App, id int64, contentType, filename string, size int) {
	t.Helper()
	if _, err := app.db.Exec(
		`INSERT INTO clips (id, content_type, data, filename) VALUES (?, ?, zeroblob(?), ?)`,
		id, contentType, size, filename,
	); err != nil {
		t.Fatalf("insert clip: %v", err)
	}
}

func TestGetClipTextDeclinesOverCapTextWithoutBytes(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	size := maxEditableTextBytes + 1
	insertSizedClip(t, app, 501, "text/plain", "huge.log", size)

	clip, err := app.GetClipText(501)
	if err != nil {
		t.Fatalf("GetClipText: %v", err)
	}
	if !clip.TooLarge {
		t.Fatal("over-cap text clip not reported too_large")
	}
	if clip.Size != int64(size) {
		t.Fatalf("size = %d, want %d", clip.Size, size)
	}
	if clip.Data != "" {
		t.Fatalf("too-large payload carried %d bytes of data", len(clip.Data))
	}
	if clip.Filename != "huge.log" || clip.ContentType != "text/plain" {
		t.Fatalf("metadata = %q %q", clip.Filename, clip.ContentType)
	}
}

func TestGetClipTextReturnsInCapTextAndLargeImages(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	if _, err := app.db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (502, 'text/plain', CAST('hello' AS BLOB), 'a.txt')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	clip, err := app.GetClipText(502)
	if err != nil {
		t.Fatalf("GetClipText: %v", err)
	}
	if clip.TooLarge || clip.Data != "hello" || clip.DataEncoding != "utf8" {
		t.Fatalf("in-cap text = %+v", clip)
	}

	// The image editor has no 16 MiB cap; GetClipText must not apply it.
	insertSizedClip(t, app, 503, "image/png", "big.png", maxEditableTextBytes+1)
	img, err := app.GetClipText(503)
	if err != nil {
		t.Fatalf("GetClipText image: %v", err)
	}
	if img.TooLarge || img.Data == "" {
		t.Fatalf("image was declined: too_large=%v data=%d", img.TooLarge, len(img.Data))
	}

	if _, err := app.GetClipText(9999); err == nil {
		t.Fatal("missing clip returned no error")
	}
}

func TestHandleGetClipTextDeclinesOverCapTextWithoutBytes(t *testing.T) {
	db := newServerTestDB(t)
	manager := NewAPIManager(&App{db: db})
	size := maxEditableTextBytes + 1
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (11, 'text/plain', zeroblob(?), 'huge.log')`, size); err != nil {
		t.Fatalf("insert: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/clips/11/text", nil)
	req.SetPathValue("id", "11")
	rec := httptest.NewRecorder()
	manager.handleGetClipText(rec, withKey(req, &apiKeyContext{KeyID: 1, Role: "viewer"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %.200s)", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() > 4096 {
		t.Fatalf("too-large response is %d bytes; the clip was shipped", rec.Body.Len())
	}
	var got apiClipTextResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.TooLarge || got.Size != size || got.Data != "" || got.Filename != "huge.log" {
		t.Fatalf("response = %+v", got)
	}
}

// maxEditableTextBytes is a pre-check copy of TextCodec's cap; the two must
// agree or desktop would decline clips the editor could open (or ship ones it
// will refuse).
func TestMaxEditableTextBytesMatchesTextCodec(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "text-editor", "text-codec.js"))
	if err != nil {
		t.Fatalf("read text-codec.js: %v", err)
	}
	m := regexp.MustCompile(`MAX_EDITABLE_BYTES\s*=\s*(\d+)\s*\*\s*(\d+)\s*\*\s*(\d+)`).FindSubmatch(src)
	if m == nil {
		t.Fatal("MAX_EDITABLE_BYTES not found in the a*b*c form this test parses")
	}
	v := 1
	for _, part := range m[1:] {
		n, _ := strconv.Atoi(string(part))
		v *= n
	}
	if v != maxEditableTextBytes {
		t.Fatalf("TextCodec MAX_EDITABLE_BYTES = %d, Go maxEditableTextBytes = %d", v, maxEditableTextBytes)
	}
}
