package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUploadFileAndGetIDClassifiesMarkdownByFilename(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	tests := []struct {
		name            string
		filename        string
		declaredType    string
		wantContentType string
	}{
		{name: "md", filename: "notes.md", declaredType: "application/octet-stream", wantContentType: "text/markdown"},
		{name: "markdown", filename: "notes.markdown", declaredType: "text/plain", wantContentType: "text/markdown"},
		{name: "uppercase", filename: "README.MD", declaredType: "image/png", wantContentType: "text/markdown"},
		{name: "suffix only", filename: "notes.md.exe", declaredType: "application/octet-stream", wantContentType: "application/octet-stream"},
		{name: "preserve declared markdown", filename: "notes.txt", declaredType: "text/markdown", wantContentType: "text/markdown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := app.UploadFileAndGetID(FileData{
				Name:        tt.filename,
				ContentType: tt.declaredType,
				Data:        base64.StdEncoding.EncodeToString([]byte("# Heading")),
			})
			if err != nil {
				t.Fatalf("UploadFileAndGetID: %v", err)
			}

			clip, err := app.GetClipData(id)
			if err != nil {
				t.Fatalf("GetClipData: %v", err)
			}
			if clip.ContentType != tt.wantContentType {
				t.Fatalf("content type = %q, want %q", clip.ContentType, tt.wantContentType)
			}
		})
	}
}

func TestUploadFilesAndUpdateClipDataPromoteMarkdown(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	encoded := base64.StdEncoding.EncodeToString([]byte("# Heading"))
	if err := app.UploadFiles([]FileData{{
		Name:        "bulk.MD",
		ContentType: "application/octet-stream",
		Data:        encoded,
	}}, 0, 0); err != nil {
		t.Fatalf("UploadFiles: %v", err)
	}

	var id int64
	if err := app.db.QueryRow(`SELECT id FROM clips WHERE filename = 'bulk.MD'`).Scan(&id); err != nil {
		t.Fatalf("find uploaded clip: %v", err)
	}
	clip, err := app.GetClipData(id)
	if err != nil {
		t.Fatalf("GetClipData after UploadFiles: %v", err)
	}
	if clip.ContentType != "text/markdown" {
		t.Fatalf("bulk upload content type = %q, want text/markdown", clip.ContentType)
	}

	if err := app.UpdateClipData(id, "image/png", encoded, "updated.markdown"); err != nil {
		t.Fatalf("UpdateClipData to Markdown: %v", err)
	}
	clip, err = app.GetClipData(id)
	if err != nil {
		t.Fatalf("GetClipData after update: %v", err)
	}
	if clip.ContentType != "text/markdown" {
		t.Fatalf("updated content type = %q, want text/markdown", clip.ContentType)
	}
}

func TestRenameClipPromotesMarkdownWithoutDowngrading(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	id, err := app.UploadFileAndGetID(FileData{
		Name:        "notes.txt",
		ContentType: "text/plain",
		Data:        base64.StdEncoding.EncodeToString([]byte("# Heading")),
	})
	if err != nil {
		t.Fatalf("UploadFileAndGetID: %v", err)
	}

	if err := app.RenameClip(id, "notes.MarkDown"); err != nil {
		t.Fatalf("RenameClip to Markdown: %v", err)
	}
	clip, err := app.GetClipData(id)
	if err != nil {
		t.Fatalf("GetClipData after promotion: %v", err)
	}
	if clip.ContentType != "text/markdown" {
		t.Fatalf("content type after promotion = %q, want text/markdown", clip.ContentType)
	}

	if err := app.RenameClip(id, "notes.txt"); err != nil {
		t.Fatalf("RenameClip away from Markdown: %v", err)
	}
	clip, err = app.GetClipData(id)
	if err != nil {
		t.Fatalf("GetClipData after rename away: %v", err)
	}
	if clip.ContentType != "text/markdown" {
		t.Fatalf("content type after rename away = %q, want preserved text/markdown", clip.ContentType)
	}
}

func TestGetClipDataPreservesInvalidUTF8MarkdownBytes(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	original := []byte{0xff, 0xfe, '#', 'x'}
	id, err := app.UploadFileAndGetID(FileData{
		Name:        "broken.md",
		ContentType: "application/octet-stream",
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
		t.Fatal("invalid Markdown reported as valid UTF-8")
	}
	if clip.DataEncoding != "base64" {
		t.Fatalf("data encoding = %q, want base64", clip.DataEncoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(clip.Data)
	if err != nil {
		t.Fatalf("decode returned data: %v", err)
	}
	if !bytes.Equal(decoded, original) {
		t.Fatalf("returned bytes = %v, want %v", decoded, original)
	}
}

func TestRESTCreateClipPromotesMarkdownFilename(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	manager := NewAPIManager(app)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "api.MD")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte("# API")); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/clips", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req = req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, &apiKeyContext{
		KeyID: 1,
		Role:  "editor",
	}))
	rec := httptest.NewRecorder()
	manager.handleCreateClip(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %q", rec.Code, rec.Body.String())
	}
	var clip apiClipResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &clip); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if clip.ContentType != "text/markdown" {
		t.Fatalf("response content type = %q, want text/markdown", clip.ContentType)
	}
}

func TestServeUploadPromotesMarkdownFilename(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	tag, err := app.CreateTag("docs")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	ts := &tagServer{
		tagID:     tag.ID,
		tagName:   tag.Name,
		apiAccess: "readwrite",
		serveKey:  "test-key",
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "served.markdown")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte("# Served")); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/_api/_upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: "_mp_serve_key", Value: "test-key"})
	rec := httptest.NewRecorder()
	app.serveManager.handleFileUpload(rec, req, ts)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %q", rec.Code, rec.Body.String())
	}
	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["content_type"] != "text/markdown" {
		t.Fatalf("response content type = %q, want text/markdown", response["content_type"])
	}
}

func TestInitDBPromotesExistingMarkdownRows(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dir)

	db, err := initDB()
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO clips (filename, content_type, data) VALUES
		('legacy.md', 'application/octet-stream', '# Legacy'),
		('legacy.MARKDOWN', 'image/png', '# Legacy'),
		('legacy.txt', 'text/plain', '# Legacy')`); err != nil {
		t.Fatalf("seed legacy rows: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close first DB: %v", err)
	}

	db, err = initDB()
	if err != nil {
		t.Fatalf("reopen initDB: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT filename, content_type FROM clips ORDER BY id`)
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	defer rows.Close()

	got := map[string]string{}
	for rows.Next() {
		var filename, contentType string
		if err := rows.Scan(&filename, &contentType); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		got[filename] = contentType
	}
	if got["legacy.md"] != "text/markdown" {
		t.Fatalf("legacy.md type = %q, want text/markdown", got["legacy.md"])
	}
	if got["legacy.MARKDOWN"] != "text/markdown" {
		t.Fatalf("legacy.MARKDOWN type = %q, want text/markdown", got["legacy.MARKDOWN"])
	}
	if got["legacy.txt"] != "text/plain" {
		t.Fatalf("legacy.txt type = %q, want text/plain", got["legacy.txt"])
	}
}
