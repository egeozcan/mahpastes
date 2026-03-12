# File Upload via Tag Serve API

**Date**: 2026-03-12
**Status**: Approved

## Problem

HTML apps hosted via tag serve mode can read/write JSON data through the `/_api` prefix, but cannot upload files (images, text, etc.) as new clips. This limits what SPA apps can do — they can't save user-generated content back into mahpastes.

## Solution

Add a `POST /_api/_upload` endpoint that accepts multipart file uploads, creates clips in the database, and tags them to the served tag or any subtag underneath it (auto-creating tags as needed).

## Endpoint Design

```
POST /_api/_upload
Content-Type: multipart/form-data
Cookie: _mp_serve_key=<token>

Form fields:
  file:         <binary>          (required — the file to upload)
  tag:          "c/d"             (optional — relative subtag path under served tag)
  content_type: "image/png"       (optional — override auto-detection from multipart header)
```

### Access Control

- Requires `apiAccess == "readwrite"` (same gate as JSON write operations)
- Cookie auth via `_mp_serve_key` (same as existing `/_api` endpoints)
- Read-only mode (`apiAccess == "read"`) returns 403

### Tag Resolution

| Served tag | `tag` form field | Resolved tag |
|------------|-----------------|--------------|
| `a/b`      | _(empty)_       | `a/b`        |
| `a/b`      | `c`             | `a/b/c`      |
| `a/b`      | `c/d/e`         | `a/b/c/d/e`  |

- Tags that don't exist are auto-created via `CreateTag` (which auto-creates ancestors)
- Tag path must not escape the served root (no `../` traversal)
- Tag path must not contain `_api` segments (existing CreateTag validation)

### Validation

- File required (400 if missing)
- File size capped at 10 MB (413 if exceeded)
- Tag path validated as descendant of served tag (400 if not)

### Response

**201 Created:**
```json
{
  "id": 42,
  "filename": "photo.png",
  "content_type": "image/png",
  "tag": "a/b/c/d",
  "tag_id": 7
}
```

**Errors:** 400, 401, 403, 413, 500 with `{"error": "message"}` body.

## Implementation Architecture

### Routing

In `serve_manager.go` `makeHandler`, detect `/_api/_upload` before the general `/_api` check. This prevents the JSON API handler from trying to parse `_upload` as a clip stem.

### New File: `serve_file_upload.go`

Handler function `handleFileUpload(w, r, ts)`:

1. Validate readwrite mode + cookie auth + POST method
2. Parse multipart form with 10 MB limit
3. Extract `file` field (required) + optional `tag` and `content_type` fields
4. Content type detection: use provided content_type field, fall back to multipart header, fall back to sniffing (same logic as UploadFiles)
5. Resolve target tag:
   - No tag field → `ts.tagID`
   - Tag field present → validate as descendant, `CreateTag(ts.tagName + "/" + tag)` if needed
6. Insert clip: `INSERT INTO clips (content_type, data, filename, content_hash) VALUES (?, ?, ?, ?)`
7. Tag clip: `AddTagToClip(clipID, targetTagID)` — enforces tree exclusivity
8. Emit `clip:created` plugin event
9. Return 201 with clip metadata JSON

### Tag Validation Helper

```go
func validateSubtagPath(servedTagName, relativeTag string) (string, error)
```

- Rejects empty segments, `..`, `.`, and `_api` segments
- Returns the full tag name (`servedTagName + "/" + relativeTag`)

## Testing

### E2E Tests (`e2e/tests/serve/upload.spec.ts`)

1. Upload file to served tag (no tag field) — verify clip created + tagged
2. Upload with subtag `sub/child` — verify tag auto-created, clip tagged
3. Reject in read-only mode → 403
4. Reject without auth cookie → 401
5. Reject oversized file → 413
6. Reject tag outside served root → 400
7. Verify content type auto-detection works

### Go Unit Tests (`serve_file_upload_test.go`)

- `validateSubtagPath`: valid subtags, empty tag, `..` traversal, `_api` segment
- Integration: multipart upload → verify clip + tag in DB

## Documentation Updates

- `CLAUDE.md` — Add File Upload API section under Tag Serve JSON API
- `docs/docs/features/tag-serve.md` — Add file upload section
- `docs/docs/features/rest-api.md` — Add upload endpoint reference
- `docs/docs/developers/api-reference.md` — Add endpoint specification
- `docs/docs/developers/backend.md` — Add to serve manager section
