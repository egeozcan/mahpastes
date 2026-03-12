# File Upload via Tag Serve API — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `POST /_api/_upload` endpoint so HTML apps served via tag serve can upload files as clips, tagged to the served tag or any subtag under it.

**Architecture:** New handler in `serve_file_upload.go` intercepts `/_api/_upload` before the existing JSON API router. Accepts multipart form data with `file` (required), `tag` (optional relative subtag path), and `content_type` (optional override). Auto-creates tags via `App.CreateTag`. Reuses existing auth cookie and access level checks.

**Tech Stack:** Go standard library `net/http`, `mime/multipart`; Playwright e2e tests; existing `App.CreateTag` and `App.AddTagToClip` for tag operations.

**Design doc:** `docs/plans/2026-03-12-serve-file-upload-design.md`

---

### Task 1: Write `validateSubtagPath` unit tests

**Files:**
- Create: `serve_file_upload_test.go`

**Step 1: Write the failing tests**

```go
package main

import (
	"testing"
)

func TestValidateSubtagPath(t *testing.T) {
	tests := []struct {
		name       string
		servedTag  string
		relTag     string
		wantFull   string
		wantErr    bool
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
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/egecan/Code/mahpastes && go test -run TestValidateSubtagPath -v`
Expected: FAIL — `validateSubtagPath` undefined

**Step 3: Write minimal implementation**

Create `serve_file_upload.go` with just the validation function:

```go
package main

import (
	"fmt"
	"strings"
)

// validateSubtagPath validates a relative tag path and returns the full tag name.
// An empty relativeTag returns the servedTagName unchanged.
// Rejects "..", ".", empty segments, and "_api" segments.
func validateSubtagPath(servedTagName, relativeTag string) (string, error) {
	relativeTag = strings.TrimSpace(relativeTag)
	if relativeTag == "" {
		return servedTagName, nil
	}

	segments := strings.Split(relativeTag, "/")
	for _, seg := range segments {
		if seg == "" {
			return "", fmt.Errorf("invalid tag path: empty segment")
		}
		if seg == ".." || seg == "." {
			return "", fmt.Errorf("invalid tag path: %q not allowed", seg)
		}
		if seg == "_api" {
			return "", fmt.Errorf("invalid tag path: '_api' is a reserved segment")
		}
	}

	return servedTagName + "/" + relativeTag, nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/egecan/Code/mahpastes && go test -run TestValidateSubtagPath -v`
Expected: PASS — all cases green

**Step 5: Commit**

```bash
git add serve_file_upload.go serve_file_upload_test.go
git commit -m "feat(serve): add validateSubtagPath helper with tests"
```

---

### Task 2: Write the file upload handler

**Files:**
- Modify: `serve_file_upload.go`

**Step 1: Write the upload handler**

Add to `serve_file_upload.go` — the full `handleFileUpload` method plus necessary imports:

```go
package main

import (
	"crypto/subtle"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

const maxUploadSize = 10 * 1024 * 1024 // 10 MB

// handleFileUpload handles POST /_api/_upload for file uploads via the serve API.
func (sm *ServeManager) handleFileUpload(w http.ResponseWriter, r *http.Request, ts *tagServer) {
	// CORS preflight.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Only POST is allowed.
	if r.Method != http.MethodPost {
		jsonAPIError(w, http.StatusMethodNotAllowed, "method not allowed: use POST")
		return
	}

	// Must be readwrite mode.
	if ts.apiAccess != "readwrite" {
		if ts.apiAccess == "read" {
			jsonAPIError(w, http.StatusForbidden, "forbidden: API is read-only")
		} else {
			http.NotFound(w, r)
		}
		return
	}

	// Validate cookie authentication.
	cookie, err := r.Cookie("_mp_serve_key")
	if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(ts.serveKey)) != 1 {
		jsonAPIError(w, http.StatusUnauthorized, "unauthorized: missing or invalid serve key")
		return
	}

	// Parse multipart form with size limit.
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		jsonAPIError(w, http.StatusRequestEntityTooLarge, "file too large (max 10 MB)")
		return
	}

	// Extract the file.
	file, header, err := r.FormFile("file")
	if err != nil {
		jsonAPIError(w, http.StatusBadRequest, "missing required 'file' field")
		return
	}
	defer file.Close()

	// Read file data (enforce size limit).
	data, err := io.ReadAll(io.LimitReader(file, maxUploadSize+1))
	if err != nil {
		jsonAPIError(w, http.StatusInternalServerError, "failed to read file data")
		return
	}
	if int64(len(data)) > maxUploadSize {
		jsonAPIError(w, http.StatusRequestEntityTooLarge, "file too large (max 10 MB)")
		return
	}

	// Determine content type: form field > multipart header > sniffing.
	contentType := r.FormValue("content_type")
	if contentType == "" {
		contentType = header.Header.Get("Content-Type")
	}
	if contentType == "" || contentType == "application/octet-stream" || contentType == "text/plain" {
		// Sniff for HTML/JSON like UploadFiles does.
		if contentType == "text/plain" || contentType == "" {
			trimmed := strings.TrimSpace(string(data))
			if strings.HasPrefix(trimmed, "<!DOCTYPE html") || strings.HasPrefix(trimmed, "<!doctype html") {
				contentType = "text/html"
			} else if isJSON(trimmed) {
				contentType = "application/json"
			} else if contentType == "" {
				contentType = "application/octet-stream"
			}
		}
	}

	// Resolve target tag.
	relativeTag := r.FormValue("tag")
	fullTagName, err := validateSubtagPath(ts.tagName, relativeTag)
	if err != nil {
		jsonAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Resolve or create the target tag.
	var targetTagID int64
	if fullTagName == ts.tagName {
		targetTagID = ts.tagID
	} else {
		// CreateTag auto-creates ancestors and returns the leaf.
		tag, err := sm.app.CreateTag(fullTagName)
		if err != nil {
			// If tag already exists, look it up.
			if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "already exists") {
				var id int64
				lookupErr := sm.app.db.QueryRow("SELECT id FROM tags WHERE name = ?", fullTagName).Scan(&id)
				if lookupErr != nil {
					jsonAPIError(w, http.StatusInternalServerError, fmt.Sprintf("failed to resolve tag: %v", lookupErr))
					return
				}
				targetTagID = id
			} else {
				jsonAPIError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create tag: %v", err))
				return
			}
		} else {
			targetTagID = tag.ID
		}
	}

	// Insert clip.
	filename := header.Filename
	contentHash := computeContentHash(data)
	result, err := sm.app.db.Exec(
		"INSERT INTO clips (content_type, data, filename, content_hash) VALUES (?, ?, ?, ?)",
		contentType, data, filename, contentHash,
	)
	if err != nil {
		jsonAPIError(w, http.StatusInternalServerError, fmt.Sprintf("failed to insert clip: %v", err))
		return
	}
	clipID, _ := result.LastInsertId()

	// Tag the clip.
	if err := sm.app.AddTagToClip(clipID, targetTagID); err != nil {
		log.Printf("serve upload: failed to tag clip %d with tag %d: %v", clipID, targetTagID, err)
	}

	// Emit plugin event.
	if sm.app.pluginManager != nil {
		sm.app.pluginManager.EmitEvent("clip:created", map[string]interface{}{
			"id":           clipID,
			"content_type": contentType,
			"filename":     filename,
		})
	}

	// Return success.
	jsonAPIResponse(w, http.StatusCreated, map[string]interface{}{
		"id":           clipID,
		"filename":     filename,
		"content_type": contentType,
		"tag":          fullTagName,
		"tag_id":       targetTagID,
	})
}
```

**Step 2: Build to verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`
Expected: PASS — no compile errors

**Step 3: Commit**

```bash
git add serve_file_upload.go
git commit -m "feat(serve): add handleFileUpload handler for POST /_api/_upload"
```

---

### Task 3: Wire the route into `makeHandler`

**Files:**
- Modify: `serve_manager.go:200-207` (the `/_api` routing block in `makeHandler`)

**Step 1: Add route for `/_api/_upload` before the existing `/_api` handler**

In `serve_manager.go`, inside `makeHandler`, replace the `/_api` routing block. The `/_api/_upload` check must come before the general `/_api` dispatch so the JSON API handler doesn't try to parse `_upload` as a clip stem.

Replace this block (lines ~200-207):
```go
		// Route /_api/* requests to the JSON API handler only when API is enabled.
		// When disabled, fall through to normal file/subtag resolution so existing
		// clips or subtags named "_api" remain reachable.
		if ts.apiAccess != "none" && (strings.HasPrefix(r.URL.Path, "/_api/") || r.URL.Path == "/_api") {
			atomic.AddInt64(&ts.requestCount, 1)
			sm.handleJSONAPI(w, r, ts)
			return
		}
```

With:
```go
		// Route /_api/_upload to file upload handler (must come before general /_api).
		if ts.apiAccess != "none" && (r.URL.Path == "/_api/_upload" || strings.HasPrefix(r.URL.Path, "/_api/_upload/")) {
			atomic.AddInt64(&ts.requestCount, 1)
			sm.handleFileUpload(w, r, ts)
			return
		}

		// Route /_api/* requests to the JSON API handler only when API is enabled.
		// When disabled, fall through to normal file/subtag resolution so existing
		// clips or subtags named "_api" remain reachable.
		if ts.apiAccess != "none" && (strings.HasPrefix(r.URL.Path, "/_api/") || r.URL.Path == "/_api") {
			atomic.AddInt64(&ts.requestCount, 1)
			sm.handleJSONAPI(w, r, ts)
			return
		}
```

**Step 2: Build to verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`
Expected: PASS

**Step 3: Run existing Go tests to verify nothing broke**

Run: `cd /Users/egecan/Code/mahpastes && go test ./... 2>&1 | tail -20`
Expected: All existing tests PASS

**Step 4: Commit**

```bash
git add serve_manager.go
git commit -m "feat(serve): wire /_api/_upload route into makeHandler"
```

---

### Task 4: Write e2e tests for file upload

**Files:**
- Create: `e2e/tests/serve/file-upload.spec.ts`

**Step 1: Write the e2e tests**

Model after `e2e/tests/serve/json-api.spec.ts`. Use Playwright `request` API with `multipart` option for file uploads.

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { request as playwrightRequest } from '@playwright/test';
import { generateTestImage, generateTestText, createTempFile } from '../../helpers/test-data';
import * as fs from 'fs';
import * as path from 'path';

/**
 * Helper: create an APIRequestContext with a base URL for the served tag,
 * visit the root to obtain the auth cookie, and return the context.
 */
async function createAuthenticatedContext(port: number) {
  const ctx = await playwrightRequest.newContext({
    baseURL: `http://127.0.0.1:${port}`,
  });
  // Visit root to get the auth cookie set
  await ctx.get('/');
  return ctx;
}

test.describe('Serve - File Upload', () => {
  test.afterEach(async ({ app }) => {
    await app.stopAllServers();
  });

  test('should upload a file to the served tag', async ({ app }) => {
    // Create a tag and start serving it with readwrite API
    await app.createTag('upload-basic');
    const { port } = await app.startServingTag('upload-basic', 'readwrite');
    const ctx = await createAuthenticatedContext(port);

    try {
      // Create a test image to upload
      const imageData = generateTestImage(50, 50, [255, 0, 0]);

      const res = await ctx.post('/_api/_upload', {
        multipart: {
          file: {
            name: 'test-upload.png',
            mimeType: 'image/png',
            buffer: imageData,
          },
        },
      });
      expect(res.status()).toBe(201);
      const body = await res.json();
      expect(body.id).toBeGreaterThan(0);
      expect(body.filename).toBe('test-upload.png');
      expect(body.content_type).toBe('image/png');
      expect(body.tag).toBe('upload-basic');

      // Verify the file is now accessible on the server
      const fileRes = await ctx.get('/test-upload.png');
      expect(fileRes.status()).toBe(200);
    } finally {
      await ctx.dispose();
    }
  });

  test('should upload a file with subtag', async ({ app }) => {
    await app.createTag('upload-sub');
    const { port } = await app.startServingTag('upload-sub', 'readwrite');
    const ctx = await createAuthenticatedContext(port);

    try {
      const textContent = Buffer.from('hello world', 'utf-8');

      const res = await ctx.post('/_api/_upload', {
        multipart: {
          file: {
            name: 'note.txt',
            mimeType: 'text/plain',
            buffer: textContent,
          },
          tag: 'child/grandchild',
        },
      });
      expect(res.status()).toBe(201);
      const body = await res.json();
      expect(body.tag).toBe('upload-sub/child/grandchild');
      expect(body.tag_id).toBeGreaterThan(0);

      // Verify the subtag was auto-created and file is accessible there
      const fileRes = await ctx.get('/child/grandchild/note.txt');
      expect(fileRes.status()).toBe(200);
    } finally {
      await ctx.dispose();
    }
  });

  test('should reject upload in read-only mode', async ({ app }) => {
    await app.createTag('upload-readonly');
    const { port } = await app.startServingTag('upload-readonly', 'read');
    const ctx = await createAuthenticatedContext(port);

    try {
      const res = await ctx.post('/_api/_upload', {
        multipart: {
          file: {
            name: 'test.txt',
            mimeType: 'text/plain',
            buffer: Buffer.from('test'),
          },
        },
      });
      expect(res.status()).toBe(403);
    } finally {
      await ctx.dispose();
    }
  });

  test('should reject upload without auth cookie', async ({ app }) => {
    await app.createTag('upload-noauth');
    const { port } = await app.startServingTag('upload-noauth', 'readwrite');

    // Create context WITHOUT visiting root first (no cookie)
    const ctx = await playwrightRequest.newContext({
      baseURL: `http://127.0.0.1:${port}`,
    });

    try {
      const res = await ctx.post('/_api/_upload', {
        multipart: {
          file: {
            name: 'test.txt',
            mimeType: 'text/plain',
            buffer: Buffer.from('test'),
          },
        },
      });
      expect(res.status()).toBe(401);
    } finally {
      await ctx.dispose();
    }
  });

  test('should reject invalid tag path with traversal', async ({ app }) => {
    await app.createTag('upload-traverse');
    const { port } = await app.startServingTag('upload-traverse', 'readwrite');
    const ctx = await createAuthenticatedContext(port);

    try {
      const res = await ctx.post('/_api/_upload', {
        multipart: {
          file: {
            name: 'test.txt',
            mimeType: 'text/plain',
            buffer: Buffer.from('test'),
          },
          tag: '../evil',
        },
      });
      expect(res.status()).toBe(400);
      const body = await res.json();
      expect(body.error).toContain('..');
    } finally {
      await ctx.dispose();
    }
  });

  test('should reject upload when apiAccess is none', async ({ app }) => {
    await app.createTag('upload-none');
    const { port } = await app.startServingTag('upload-none', 'none');

    const ctx = await playwrightRequest.newContext({
      baseURL: `http://127.0.0.1:${port}`,
    });

    try {
      const res = await ctx.post('/_api/_upload', {
        multipart: {
          file: {
            name: 'test.txt',
            mimeType: 'text/plain',
            buffer: Buffer.from('test'),
          },
        },
      });
      // With apiAccess=none, /_api/_upload isn't routed — falls through to file serving = 404
      expect(res.status()).toBe(404);
    } finally {
      await ctx.dispose();
    }
  });

  test('should detect content type from file content', async ({ app }) => {
    await app.createTag('upload-detect');
    const { port } = await app.startServingTag('upload-detect', 'readwrite');
    const ctx = await createAuthenticatedContext(port);

    try {
      const htmlContent = Buffer.from('<!DOCTYPE html><html><body>Hello</body></html>');

      const res = await ctx.post('/_api/_upload', {
        multipart: {
          file: {
            name: 'page.html',
            mimeType: 'text/plain',
            buffer: htmlContent,
          },
        },
      });
      expect(res.status()).toBe(201);
      const body = await res.json();
      expect(body.content_type).toBe('text/html');
    } finally {
      await ctx.dispose();
    }
  });
});
```

**Step 2: Run the tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test -- tests/serve/file-upload.spec.ts 2>&1 | tail -30`
Expected: All 7 tests PASS

**Step 3: Commit**

```bash
git add e2e/tests/serve/file-upload.spec.ts
git commit -m "test: add e2e tests for serve file upload API"
```

---

### Task 5: Run full e2e suite

**Files:** None (verification only)

**Step 1: Run all existing tests to verify nothing is broken**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test 2>&1 | tail -50`
Expected: All tests PASS (including the new file-upload tests and existing JSON API tests)

**Step 2: Fix any failures**

If any existing tests fail, diagnose and fix before proceeding. The upload handler should not affect existing behavior since `/_api/_upload` is a new route that doesn't conflict with the JSON API's `/_api/{clipStem}` pattern.

---

### Task 6: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md` — add new section after "Tag Serve JSON API" section, add file to file structure

**Step 1: Add File Upload API section**

After the existing "Tag Serve JSON API" section (after line ~365 "Key files: serve_json_api.go..."), add:

```markdown
### Tag Serve File Upload API

HTML apps served from a tag can upload files as new clips via `POST /_api/_upload`.

**Endpoint**: `POST /_api/_upload` with `Content-Type: multipart/form-data`. Requires `apiAccess == "readwrite"` and valid `_mp_serve_key` cookie.

**Form fields**:
- `file` (required) — the file to upload (10 MB max)
- `tag` (optional) — relative subtag path under the served tag (e.g., `child/grandchild`). Auto-creates tags if needed.
- `content_type` (optional) — override content type auto-detection

**Tag resolution**: If serving tag `a/b` and tag field is `c/d`, the clip is tagged `a/b/c/d`. Empty tag field → served tag itself. Path traversal (`..`) and `_api` segments are rejected.

**Response**: `201 Created` with `{"id", "filename", "content_type", "tag", "tag_id"}`.

**Key files**: `serve_file_upload.go` (upload handler, tag validation), `serve_manager.go` (`/_api/_upload` routing).
```

Also add `serve_file_upload.go` to the file structure listing near `serve_json_api.go`.

**Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add serve file upload API to CLAUDE.md"
```

---

### Task 7: Update `docs/docs/features/tag-serve.md`

**Files:**
- Modify: `docs/docs/features/tag-serve.md`

**Step 1: Add File Upload section**

After the "JSON API" section (before "Activity Indicators"), add a new `## File Upload` section:

```markdown
## File Upload

Upload files from HTML apps hosted in a tag directly back into mahpastes as new clips.

### Requirements

- API access must be set to **API R/W** (readwrite mode)
- Uses the same cookie authentication as the JSON API

### Endpoint

```
POST /_api/_upload
Content-Type: multipart/form-data
```

Form fields:

| Field | Required | Description |
|-------|----------|-------------|
| `file` | Yes | The file to upload (max 10 MB) |
| `tag` | No | Relative subtag path (e.g., `photos/vacation`). Creates tag if it doesn't exist |
| `content_type` | No | Override auto-detected content type |

### Tag Targeting

Uploaded files are tagged to the served tag by default. Use the `tag` field to target a subtag:

| Served tag | `tag` field | Clip tagged to |
|------------|-------------|---------------|
| `myapp` | _(empty)_ | `myapp` |
| `myapp` | `data` | `myapp/data` |
| `myapp` | `data/images` | `myapp/data/images` |

Tags are auto-created if they don't exist yet, including intermediate parents.

### Example

From an `index.html` in the same tag:

```javascript
// Upload a file to the served tag
const formData = new FormData();
formData.append('file', fileInput.files[0]);

const res = await fetch('/_api/_upload', {
  method: 'POST',
  credentials: 'include',
  body: formData,
});
const result = await res.json();
// result = { id: 42, filename: "photo.png", content_type: "image/png", tag: "myapp", tag_id: 5 }

// Upload to a subtag
const formData2 = new FormData();
formData2.append('file', canvas.toBlob());
formData2.append('tag', 'exports/renders');

await fetch('/_api/_upload', {
  method: 'POST',
  credentials: 'include',
  body: formData2,
});
```

### Response

**Success (201 Created):**

```json
{
  "id": 42,
  "filename": "photo.png",
  "content_type": "image/png",
  "tag": "myapp/data/images",
  "tag_id": 7
}
```

**Errors:**

| Status | Meaning |
|--------|---------|
| 400 | Missing file, invalid tag path |
| 401 | Missing or invalid auth cookie |
| 403 | API is in read-only mode |
| 413 | File exceeds 10 MB limit |
| 404 | API access is disabled |
```

**Step 2: Commit**

```bash
git add docs/docs/features/tag-serve.md
git commit -m "docs: add file upload section to tag-serve feature docs"
```

---

### Task 8: Update `docs/docs/features/rest-api.md`

**Files:**
- Modify: `docs/docs/features/rest-api.md`

**Step 1: Add a note in the Related section**

At the bottom of the file, update the Related section to mention the serve file upload:

Replace:
```markdown
## Related

- [Tag Serve](tag-serve.md) -- unauthenticated file serving for quick sharing
- [Tags](tags.md) -- create and manage tags used for scoping
```

With:
```markdown
## Related

- [Tag Serve](tag-serve.md) -- unauthenticated file serving for quick sharing, plus JSON API and [file upload](tag-serve.md#file-upload) for served HTML apps
- [Tags](tags.md) -- create and manage tags used for scoping
```

**Step 2: Commit**

```bash
git add docs/docs/features/rest-api.md
git commit -m "docs: cross-reference serve file upload in rest-api docs"
```

---

### Task 9: Update `docs/docs/developers/api-reference.md`

**Files:**
- Modify: `docs/docs/developers/api-reference.md`

**Step 1: Add ServeService file upload reference**

After the TransferService section (before "## Events"), add a new section:

```markdown
## Serve File Upload API

The tag serve system exposes a file upload endpoint for HTML apps hosted within a served tag. This is not a Wails binding — it's an HTTP endpoint on the tag's serve port.

### POST /_api/_upload

Upload a file as a new clip, tagged to the served tag or a subtag.

**Requirements:** `apiAccess` must be `"readwrite"`, valid `_mp_serve_key` cookie.

**Request:** `multipart/form-data` with fields:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `file` | binary | Yes | File data (max 10 MB) |
| `tag` | string | No | Relative subtag path under served tag |
| `content_type` | string | No | Override auto-detected content type |

**Response (201 Created):**
```json
{
  "id": 42,
  "filename": "photo.png",
  "content_type": "image/png",
  "tag": "myapp/exports",
  "tag_id": 7
}
```

**Errors:**

| Status | Body | Condition |
|--------|------|-----------|
| 400 | `{"error": "..."}` | Missing file, invalid tag path |
| 401 | `{"error": "..."}` | No auth cookie |
| 403 | `{"error": "..."}` | Read-only mode |
| 413 | `{"error": "..."}` | File > 10 MB |

**JavaScript usage:**
```javascript
const formData = new FormData();
formData.append('file', blob, 'screenshot.png');
formData.append('tag', 'renders');  // optional subtag

const res = await fetch('/_api/_upload', {
  method: 'POST',
  credentials: 'include',
  body: formData,
});
const { id, filename, content_type, tag, tag_id } = await res.json();
```

---
```

**Step 2: Commit**

```bash
git add docs/docs/developers/api-reference.md
git commit -m "docs: add serve file upload to developer API reference"
```

---

### Task 10: Update `docs/docs/developers/backend.md`

**Files:**
- Modify: `docs/docs/developers/backend.md`

**Step 1: Add `serve_file_upload.go` to the file structure listing**

In the file structure section at the top, add `serve_file_upload.go` after `serve_json_api.go`:

```
├── serve_json_api.go    JSON API handler for served tags
├── serve_file_upload.go File upload handler for served tags
├── serve_manager.go     Tag HTTP server lifecycle and routing
```

**Step 2: Commit**

```bash
git add docs/docs/developers/backend.md
git commit -m "docs: add serve_file_upload.go to backend file structure"
```

---

### Task 11: Final verification

**Files:** None (verification only)

**Step 1: Run all Go tests**

Run: `cd /Users/egecan/Code/mahpastes && go test ./... 2>&1 | tail -20`
Expected: All PASS

**Step 2: Run full e2e suite**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test 2>&1 | tail -50`
Expected: All PASS

**Step 3: Build the app**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`
Expected: Clean build with no errors
