package app

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
)

// writeFilePartWithType writes a file part carrying its own Content-Type.
func writeFilePartWithType(t *testing.T, w *multipart.Writer, name, ctype, data string) {
	t.Helper()
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="`+name+`"`)
	if ctype != "" {
		h.Set("Content-Type", ctype)
	}
	pw, err := w.CreatePart(h)
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	if _, err := pw.Write([]byte(data)); err != nil {
		t.Fatalf("write file part: %v", err)
	}
}

func TestNormalizeClipContentType(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"text/plain", "text/plain", true},
		{"  Text/HTML  ", "text/html", true},
		{"text/plain; charset=utf-8", "text/plain; charset=utf-8", true},
		{"application/vnd.api+json", "application/vnd.api+json", true},
		{"", "", false},
		{"text", "", false},
		{"/plain", "", false},
		{"text/", "", false},
		{"<script>alert(1)</script>\nx", "", false},
		{"text/html\r\nX-Evil: 1", "", false},
		{"text/pl ain", "", false},
		{"text/plain; charset", "", false},
		{"text/" + strings.Repeat("a", maxClipContentTypeBytes), "", false},
	}
	for _, tc := range cases {
		got, ok := normalizeClipContentType(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("normalizeClipContentType(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// The content_type field used to be stored as typed: markup, line breaks and
// arbitrary text became the clip's content type and its Content-Type header.
func TestAPI_CreateClipRejectsMalformedContentTypeField(t *testing.T) {
	for _, bad := range []string{
		"<script>alert(1)</script>\nx",
		"text/html\r\nX-Evil: 1",
		"not a type",
		strings.Repeat("A", 5000),
	} {
		rec, _ := postCreateClip(t, func(w *multipart.Writer) {
			writeField(t, w, "content_type", bad)
			writeFilePart(t, w, "a.bin", "zz")
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("content_type %.30q: status = %d, want 400; %s", bad, rec.Code, rec.Body.String())
		}
	}
}

// The override is an override wherever it sits and whatever it says.
func TestAPI_CreateClipContentTypeFieldAlwaysOverrides(t *testing.T) {
	t.Run("field after the file part", func(t *testing.T) {
		rec, clip := postCreateClip(t, func(w *multipart.Writer) {
			writeFilePart(t, w, "b.txt", "hello")
			writeField(t, w, "content_type", "application/x-custom")
		})
		if rec.Code != http.StatusCreated || clip.ContentType != "application/x-custom" {
			t.Fatalf("status %d clip %+v", rec.Code, clip)
		}
	})
	t.Run("malformed field after the file part", func(t *testing.T) {
		rec, _ := postCreateClip(t, func(w *multipart.Writer) {
			writeFilePart(t, w, "b.txt", "hello")
			writeField(t, w, "content_type", "<b>")
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})
	t.Run("explicit application/octet-stream", func(t *testing.T) {
		rec, clip := postCreateClip(t, func(w *multipart.Writer) {
			writeField(t, w, "content_type", "application/octet-stream")
			writeFilePart(t, w, "c.txt", "hello")
		})
		if rec.Code != http.StatusCreated || clip.ContentType != "application/octet-stream" {
			t.Fatalf("status %d clip %+v", rec.Code, clip)
		}
	})
	t.Run("field beats the part header", func(t *testing.T) {
		rec, clip := postCreateClip(t, func(w *multipart.Writer) {
			writeField(t, w, "content_type", "Text/CSV")
			writeFilePartWithType(t, w, "d.bin", "image/png", "a,b")
		})
		if rec.Code != http.StatusCreated || clip.ContentType != "text/csv" {
			t.Fatalf("status %d clip %+v", rec.Code, clip)
		}
	})
	// The one documented exception: a Markdown filename is always Markdown.
	t.Run("markdown filename stays markdown", func(t *testing.T) {
		rec, clip := postCreateClip(t, func(w *multipart.Writer) {
			writeField(t, w, "content_type", "text/plain")
			writeFilePart(t, w, "doc.md", "# hi")
		})
		if rec.Code != http.StatusCreated || clip.ContentType != "text/markdown" {
			t.Fatalf("status %d clip %+v", rec.Code, clip)
		}
	})
}

// A file part's own header is a hint, not an override: generic or malformed
// values fall back to detection instead of being stored.
func TestAPI_CreateClipPartHeaderContentType(t *testing.T) {
	cases := []struct {
		name, filename, header, want string
	}{
		{"valid header is kept", "x.bin", "application/x-custom", "application/x-custom"},
		{"octet-stream falls back to the extension", "x.png", "application/octet-stream", "image/png"},
		{"malformed header falls back to the extension", "x.png", "<script>alert(1)</script>", "image/png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, clip := postCreateClip(t, func(w *multipart.Writer) {
				writeFilePartWithType(t, w, tc.filename, tc.header, tc.name)
			})
			if rec.Code != http.StatusCreated || clip.ContentType != tc.want {
				t.Fatalf("status %d content type %q, want %q", rec.Code, clip.ContentType, tc.want)
			}
		})
	}
}

func postFindClips(t *testing.T, app *App, key *apiKeyContext, body string) (*httptest.ResponseRecorder, []ClipMatch) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clips/find", bytes.NewBufferString(body))
	req = req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, key))
	rec := httptest.NewRecorder()
	NewAPIManager(app).handleFindClipsByFilenameAndTag(rec, req)
	var matches []ClipMatch
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &matches); err != nil {
			t.Fatalf("decode matches: %v; body %s", err, rec.Body.String())
		}
	}
	return rec, matches
}

// The web UI checks for a same-name clip before every upload or paste, with
// tag_id 0 outside a folder. The route answered 400 "tag_id is required", so
// the skip/overwrite prompt never ran for untagged clips in server mode.
func TestAPI_FindClipsByFilenameAcceptsUntagged(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	tag, err := app.CreateTag("find-scope")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if _, err := app.db.Exec(`INSERT INTO clips (id, content_type, data, filename, content_hash) VALUES
		(1, 'text/plain', 'a', 'same.txt', 'h1'),
		(2, 'text/plain', 'b', 'same.txt', 'h2')`); err != nil {
		t.Fatalf("insert clips: %v", err)
	}
	if _, err := app.db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (2, ?)`, tag.ID); err != nil {
		t.Fatalf("tag clip: %v", err)
	}
	unscoped := &apiKeyContext{KeyID: 1, Role: "viewer"}
	scoped := &apiKeyContext{KeyID: 2, Role: "viewer", ScopedTagID: tag.ID}

	rec, matches := postFindClips(t, app, unscoped, `{"filenames":["same.txt"],"tag_id":0}`)
	if rec.Code != http.StatusOK || len(matches) != 1 || matches[0].ID != 1 {
		t.Fatalf("untagged lookup: status %d matches %+v; %s", rec.Code, matches, rec.Body.String())
	}

	rec, matches = postFindClips(t, app, unscoped, `{"filenames":["same.txt"],"tag_id":`+jsonInt(tag.ID)+`}`)
	if rec.Code != http.StatusOK || len(matches) != 1 || matches[0].ID != 2 {
		t.Fatalf("tagged lookup: status %d matches %+v", rec.Code, matches)
	}

	// Untagged clips are outside a scoped key's subtree: no error, no rows.
	rec, matches = postFindClips(t, app, scoped, `{"filenames":["same.txt"],"tag_id":0}`)
	if rec.Code != http.StatusOK || len(matches) != 0 {
		t.Fatalf("scoped untagged lookup: status %d matches %+v", rec.Code, matches)
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("scoped untagged lookup body = %q, want []", rec.Body.String())
	}

	if rec, _ = postFindClips(t, app, unscoped, `{"filenames":["same.txt"],"tag_id":-1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("negative tag_id: status = %d, want 400", rec.Code)
	}
}

func jsonInt(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
