package app

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateSubtagPath(t *testing.T) {
	tests := []struct {
		name      string
		servedTag string
		relTag    string
		wantFull  string
		wantErr   bool
	}{
		{"empty tag returns served tag", "a/b", "", "a/b", false},
		{"single segment", "a/b", "c", "a/b/c", false},
		{"multi segment", "a/b", "c/d/e", "a/b/c/d/e", false},
		{"trims whitespace", "a/b", " c/d ", "a/b/c/d", false},
		{"rejects dotdot", "a/b", "../evil", "", true},
		{"rejects dotdot in middle", "a/b", "c/../evil", "", true},
		{"rejects single dot", "a/b", "./c", "", true},
		{"rejects empty segment", "a/b", "c//d", "", true},
		{"rejects _api segment", "a/b", "_api/foo", "", true},
		{"rejects _api as only segment", "a/b", "_api", "", true},
		{"allows _api substring", "a/b", "my_api_stuff", "a/b/my_api_stuff", false},
		{"top-level tag with subtag", "photos", "vacation/beach", "photos/vacation/beach", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateSubtagPath(tt.servedTag, tt.relTag)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSubtagPath(%q, %q) error = %v, wantErr %v", tt.servedTag, tt.relTag, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.wantFull {
				t.Errorf("validateSubtagPath(%q, %q) = %q, want %q", tt.servedTag, tt.relTag, got, tt.wantFull)
			}
		})
	}
}

func TestServeUploadRefusesMalformedContentType(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	tag, err := app.CreateTag("uploads")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	ts := &tagServer{tagID: tag.ID, tagName: tag.Name, apiAccess: "readwrite", serveKey: "test-key"}

	upload := func(contentType string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("content_type", contentType); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
		part, err := writer.CreateFormFile("file", "data.bin")
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := part.Write([]byte{0, 1, 2, 3}); err != nil {
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
		return rec
	}

	for _, bad := range []string{"not a type", "image/png\r\nX-Injected: 1", "<script>"} {
		if rec := upload(bad); rec.Code != http.StatusBadRequest {
			t.Errorf("content_type %q: status = %d, want 400; body %q", bad, rec.Code, rec.Body.String())
		}
	}
	if rec := upload("image/png"); rec.Code != http.StatusCreated {
		t.Fatalf("valid content_type: status = %d, want 201; body %q", rec.Code, rec.Body.String())
	}
}
