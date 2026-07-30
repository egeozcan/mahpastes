package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetClipDataProtectsAllInvalidUTF8Bytes covers the generalization of the
// bridge guard. It used to be Markdown-specific, so a text/plain clip containing
// an invalid byte crossed the JSON bridge as string(data) — already
// replacement-decoded, with no way for the frontend to recover it.
func TestGetClipDataProtectsAllInvalidUTF8Bytes(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	// Latin-1 "café" plus a stray FF: valid Latin-1, invalid UTF-8.
	original := []byte{0x63, 0x61, 0x66, 0xe9, 0xff, 0x0a}

	cases := []struct {
		name        string
		filename    string
		contentType string
		wantType    string
	}{
		// The regression: previously utf8 + replacement-decoded.
		{"plain text", "broken.txt", "text/plain", "text/plain"},
		{"json content type", "broken.json", "application/json", "application/json"},
		// These already took the base64 path via the binary branch, but assert
		// them so a future refactor cannot quietly re-narrow the guard.
		{"markdown", "broken.md", "application/octet-stream", "text/markdown"},
		{"extension-classified yaml", "broken.yaml", "application/octet-stream", "application/octet-stream"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := app.UploadFileAndGetID(FileData{
				Name:        tc.filename,
				ContentType: tc.contentType,
				Data:        base64.StdEncoding.EncodeToString(original),
			})
			if err != nil {
				t.Fatalf("UploadFileAndGetID: %v", err)
			}

			clip, err := app.GetClipData(id)
			if err != nil {
				t.Fatalf("GetClipData: %v", err)
			}
			if clip.ValidUTF8 {
				t.Fatal("invalid bytes reported as valid UTF-8")
			}
			if clip.DataEncoding != "base64" {
				t.Fatalf("data encoding = %q, want base64 (a utf8 payload has already lost the bytes)", clip.DataEncoding)
			}
			if clip.ContentType != tc.wantType {
				t.Fatalf("content type = %q, want %q", clip.ContentType, tc.wantType)
			}
			decoded, err := base64.StdEncoding.DecodeString(clip.Data)
			if err != nil {
				t.Fatalf("decode returned data: %v", err)
			}
			if !bytes.Equal(decoded, original) {
				t.Fatalf("returned bytes = %v, want %v", decoded, original)
			}
		})
	}
}

// TestGetClipDataKeepsValidTextAsUTF8 pins the other half of the branch: valid
// text still crosses as a plain string, and everything else as base64, so the
// frontend decoder must handle both encodings after classification rather than
// inferring text from the content type.
func TestGetClipDataKeepsValidTextAsUTF8(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	// A BOM'd CRLF document: the case where a lossy path is invisible in a
	// string comparison but obvious in the bytes.
	original := []byte{0xef, 0xbb, 0xbf, 'a', 0x0d, 0x0a}

	textID, err := app.UploadFileAndGetID(FileData{
		Name:        "fine.txt",
		ContentType: "text/plain",
		Data:        base64.StdEncoding.EncodeToString(original),
	})
	if err != nil {
		t.Fatalf("UploadFileAndGetID: %v", err)
	}
	clip, err := app.GetClipData(textID)
	if err != nil {
		t.Fatalf("GetClipData: %v", err)
	}
	if !clip.ValidUTF8 {
		t.Fatal("valid UTF-8 reported as invalid")
	}
	if clip.DataEncoding != "utf8" {
		t.Fatalf("data encoding = %q, want utf8", clip.DataEncoding)
	}
	if !bytes.Equal([]byte(clip.Data), original) {
		t.Fatalf("returned bytes = %v, want %v (BOM and CR must survive)", []byte(clip.Data), original)
	}

	// A valid UTF-8 config.yaml stored as application/octet-stream — the case the
	// media-MIME guard deliberately leaves conditional. It arrives as base64 even
	// though the bytes are perfectly good text.
	yamlID, err := app.UploadFileAndGetID(FileData{
		Name:        "config.yaml",
		ContentType: "application/octet-stream",
		Data:        base64.StdEncoding.EncodeToString([]byte("key: value\n")),
	})
	if err != nil {
		t.Fatalf("UploadFileAndGetID: %v", err)
	}
	yamlClip, err := app.GetClipData(yamlID)
	if err != nil {
		t.Fatalf("GetClipData: %v", err)
	}
	if !yamlClip.ValidUTF8 {
		t.Fatal("valid UTF-8 yaml reported as invalid")
	}
	if yamlClip.DataEncoding != "base64" {
		t.Fatalf("data encoding = %q, want base64 for an application content type", yamlClip.DataEncoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(yamlClip.Data)
	if err != nil {
		t.Fatalf("decode returned data: %v", err)
	}
	if string(decoded) != "key: value\n" {
		t.Fatalf("decoded = %q, want %q", decoded, "key: value\n")
	}
}

// TestUploadSniffingIsUnchanged asserts the backend's existing text/plain ->
// application/json promotion as *current behavior* rather than assuming it is
// absent. This feature adds no new silent MIME rewriting; it also does not remove
// the rewriting that was already there, and a Save As from notes.txt to copy.json
// therefore still lands as application/json.
func TestUploadSniffingIsUnchanged(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	cases := []struct {
		name     string
		filename string
		body     string
		declared string
		want     string
	}{
		{"json body under a .json name", "copy.json", `{"a":1}`, "text/plain", "application/json"},
		{"json body under a .txt name", "notes.txt", `{"a":1}`, "text/plain", "application/json"},
		{"html body", "page.txt", "<!DOCTYPE html><p>x</p>", "text/plain", "text/html"},
		{"ordinary prose", "notes.txt", "just words", "text/plain", "text/plain"},
		{"declared type is respected", "data.txt", `{"a":1}`, "text/csv", "text/csv"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := app.UploadFileAndGetID(FileData{
				Name:        tc.filename,
				ContentType: tc.declared,
				Data:        base64.StdEncoding.EncodeToString([]byte(tc.body)),
			})
			if err != nil {
				t.Fatalf("UploadFileAndGetID: %v", err)
			}
			clip, err := app.GetClipData(id)
			if err != nil {
				t.Fatalf("GetClipData: %v", err)
			}
			if clip.ContentType != tc.want {
				t.Fatalf("content type = %q, want %q", clip.ContentType, tc.want)
			}
		})
	}
}

// --- GET /api/v1/clips/{id}/text ---------------------------------------------

func TestHandleGetClipTextReturnsOneAtomicPayload(t *testing.T) {
	db := newServerTestDB(t)
	manager := NewAPIManager(&App{db: db})

	original := []byte{0xef, 0xbb, 0xbf, 'k', 'e', 'y', ':', ' ', 'v', 0x0d, 0x0a}
	if _, err := db.Exec(
		`INSERT INTO clips (id, content_type, data, filename) VALUES (7, 'application/octet-stream', ?, 'config.yaml')`,
		original,
	); err != nil {
		t.Fatalf("insert clip: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/clips/7/text", nil)
	req.SetPathValue("id", "7")
	rec := httptest.NewRecorder()
	manager.handleGetClipText(rec, withKey(req, &apiKeyContext{KeyID: 1, Role: "viewer"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var got apiClipTextResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// The exact shape TextCodec already consumes from the desktop binding, so one
	// frontend decoder serves both surfaces.
	if got.Filename != "config.yaml" {
		t.Fatalf("filename = %q, want config.yaml (the rest-glue bug returned an empty filename)", got.Filename)
	}
	if got.ContentType != "application/octet-stream" {
		t.Fatalf("content type = %q", got.ContentType)
	}
	if got.DataEncoding != "base64" {
		t.Fatalf("data encoding = %q, want base64", got.DataEncoding)
	}
	if !got.ValidUTF8 {
		t.Fatal("valid UTF-8 bytes reported as invalid")
	}
	if got.Size != len(original) {
		t.Fatalf("size = %d, want %d", got.Size, len(original))
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Data)
	if err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if !bytes.Equal(decoded, original) {
		t.Fatalf("bytes = %v, want %v (BOM and CR must survive the round trip)", decoded, original)
	}
}

func TestHandleGetClipTextReportsInvalidUTF8(t *testing.T) {
	db := newServerTestDB(t)
	manager := NewAPIManager(&App{db: db})

	original := []byte{0x63, 0x61, 0x66, 0xe9}
	if _, err := db.Exec(
		`INSERT INTO clips (id, content_type, data, filename) VALUES (8, 'text/plain', ?, 'latin1.txt')`,
		original,
	); err != nil {
		t.Fatalf("insert clip: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/clips/8/text", nil)
	req.SetPathValue("id", "8")
	rec := httptest.NewRecorder()
	manager.handleGetClipText(rec, withKey(req, &apiKeyContext{KeyID: 1, Role: "viewer"}))

	var got apiClipTextResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ValidUTF8 {
		t.Fatal("invalid UTF-8 reported as valid")
	}
	// Always base64, so the invalid bytes reach the frontend intact instead of
	// being replacement-decoded in transit.
	decoded, err := base64.StdEncoding.DecodeString(got.Data)
	if err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if !bytes.Equal(decoded, original) {
		t.Fatalf("bytes = %v, want %v", decoded, original)
	}
}

func TestHandleGetClipTextRejectsMissingAndOutOfScope(t *testing.T) {
	db := newServerTestDB(t)
	manager := NewAPIManager(&App{db: db})

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'scope', '#111111'), (2, 'other', '#222222')`); err != nil {
		t.Fatalf("insert tags: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (9, 'text/plain', 'x', 'x.txt')`); err != nil {
		t.Fatalf("insert clip: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (9, 2)`); err != nil {
		t.Fatalf("insert clip_tag: %v", err)
	}

	missing := httptest.NewRequest("GET", "/api/v1/clips/404/text", nil)
	missing.SetPathValue("id", "404")
	rec := httptest.NewRecorder()
	manager.handleGetClipText(rec, withKey(missing, &apiKeyContext{KeyID: 1, Role: "viewer"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing clip status = %d, want 404", rec.Code)
	}

	bad := httptest.NewRequest("GET", "/api/v1/clips/abc/text", nil)
	bad.SetPathValue("id", "abc")
	rec = httptest.NewRecorder()
	manager.handleGetClipText(rec, withKey(bad, &apiKeyContext{KeyID: 1, Role: "viewer"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id status = %d, want 400", rec.Code)
	}

	// A tag-scoped key must not read a clip outside its subtree through the new
	// route any more than through the existing ones.
	scoped := httptest.NewRequest("GET", "/api/v1/clips/9/text", nil)
	scoped.SetPathValue("id", "9")
	rec = httptest.NewRecorder()
	manager.handleGetClipText(rec, withKey(scoped, &apiKeyContext{KeyID: 1, Role: "viewer", ScopedTagID: 1}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("out-of-scope status = %d, want 403", rec.Code)
	}
}

// TestUpdateClipDataRejectsMissingClip covers a data-loss path, not a cosmetic one.
//
// UpdateClipData discarded RowsAffected, so updating a clip that had expired or been
// deleted through the API, the CLI, or another window returned nil. The editor takes
// nil as "saved": it clears the draft and closes, destroying the only copy of the
// user's edit. A zero-row UPDATE has to fail.
func TestUpdateClipDataRejectsMissingClip(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	id, err := app.UploadFileAndGetID(FileData{
		Name:        "notes.txt",
		ContentType: "text/plain",
		Data:        base64.StdEncoding.EncodeToString([]byte("original")),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	// The happy path still works.
	if err := app.UpdateClipData(id, "text/plain",
		base64.StdEncoding.EncodeToString([]byte("edited")), "notes.txt"); err != nil {
		t.Fatalf("update of an existing clip should succeed: %v", err)
	}

	// Now the clip goes away underneath the open editor.
	if err := app.DeleteClip(id); err != nil {
		t.Fatalf("delete: %v", err)
	}

	err = app.UpdateClipData(id, "text/plain",
		base64.StdEncoding.EncodeToString([]byte("edited again")), "notes.txt")
	if err == nil {
		t.Fatal("updating a deleted clip must fail; returning nil makes the editor " +
			"report success, clear the draft, and close, losing the edit")
	}
}
