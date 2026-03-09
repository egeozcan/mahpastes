---
name: mahpastes
description: This skill should be used when Claude Code needs to store generated artifacts, host websites or static files via HTTP, manage a research knowledge base, or create interactive web apps with JSON data persistence using the mahpastes clipboard manager's REST API and tag-serve system. Triggers on "save to mahpastes", "upload to mahpastes", "store in mahpastes", "host this site", "serve these files", "deploy to mahpastes", "search mahpastes", "find in mahpastes", "persist this to mahpastes", "tag this in mahpastes", "make this available over HTTP via mahpastes", "retrieve from mahpastes", "create interactive app in mahpastes", "JSON API with mahpastes", or "serve data from mahpastes".
---

# mahpastes Integration

mahpastes is a desktop clipboard manager with a REST API and HTTP tag-serve system. Use it to persist artifacts, host static sites, and maintain a searchable knowledge base.

## Prerequisites

Before any operation, verify that `MAHPASTES_API_URL` and `MAHPASTES_API_KEY` environment variables are set. If either is missing, tell the user to run `/mahpastes:setup` and do not proceed.

Verify connectivity:

```bash
curl -sf -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  "$MAHPASTES_API_URL/api/v1/tags"
```

A `200` response confirms the API is reachable and the key is valid. Any other response means the server is not running or the key is invalid — tell the user to check mahpastes and re-run `/mahpastes:setup`.

## Authentication

All API requests require the header: `Authorization: Bearer $MAHPASTES_API_KEY`

Keys have roles: viewer (read-only), editor (create/delete clips), admin (full access including tag CRUD and serve management). Admin is required for tag-serve and tag management.

### Tag-Scoped Keys

API keys can be scoped to a single tag. Detect a scoped key by listing tags — a scoped key returns only its one tag. Scoped keys change behavior significantly:

- **Uploads auto-tag**: Every uploaded clip automatically receives the scoped tag. No manual tagging needed.
- **Visibility restricted**: Only clips tagged with the scoped tag are visible. Listing, viewing, downloading, deleting, and archiving clips outside the scope returns 403.
- **Tag CRUD blocked**: Cannot create, update, or delete tags (403: `"tag-scoped keys cannot manage tags"`).
- **Tag associations restricted**: Can only add/remove the scoped tag on clips, not other tags.
- **Tag-serve restricted**: Can only start/stop serve for the scoped tag.
- **Search scoped**: Search results only include clips within the scoped tag.

When a 403 with `"tag-scoped keys cannot manage tags"` is received, do not retry — adapt the workflow to work within the scope (see Workflow Patterns below).

## Core Operations

### Upload a File

```bash
curl -s -X POST \
  -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  -F "file=@/path/to/file" \
  "$MAHPASTES_API_URL/api/v1/clips"
```

Response (201): `{"id": N, "filename": "...", "content_type": "...", "size": N, "tags": []}`

To override the filename, append `?filename=custom-name.ext` to the URL.

To upload in-memory content, write it to a temp file first, upload, then clean up.

### Tag a Clip

```bash
# Create a tag
curl -s -X POST \
  -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"tag-name"}' \
  "$MAHPASTES_API_URL/api/v1/tags"

# Add tag to clip
curl -s -X PUT \
  -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  "$MAHPASTES_API_URL/api/v1/clips/CLIP_ID/tags/TAG_ID"
```

### Search Clips

```bash
curl -s -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  "$MAHPASTES_API_URL/api/v1/clips?search=QUERY&limit=50"
```

Response: `{"clips": [...], "total": N, "limit": N, "offset": N}`

Filter by tag (`tag=TAG_ID`), content type (`content_type=image/png`), or archived status (`archived=true|false`).

### Download Clip Data

```bash
curl -s -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  "$MAHPASTES_API_URL/api/v1/clips/ID/data" -o output-file
```

### Start Tag-Serve (Web Hosting)

```bash
curl -s -X POST \
  -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"tag_id":TAG_ID,"port":PORT,"bind_all":false,"api_access":"none"}' \
  "$MAHPASTES_API_URL/api/v1/serve"
```

Clips tagged with TAG_ID become accessible at `http://127.0.0.1:PORT/FILENAME`. A clip named `index.html` is served at the root path.
Set `api_access` to `"read"` or `"readwrite"` to enable the JSON API on the served tag. When enabled, HTML clips can fetch `/_api/{clipName}/{path}` to read/write JSON clips.

### Delete a Clip

```bash
curl -s -X DELETE \
  -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  "$MAHPASTES_API_URL/api/v1/clips/ID"
```

Response: 204 No Content.

### Stop Tag-Serve

```bash
curl -s -X DELETE \
  -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  "$MAHPASTES_API_URL/api/v1/serve/TAG_ID"
```

## Workflow Patterns

### 1. Artifact Storage

Persist generated files (code, configs, documents) for later retrieval.

1. Generate the file content and write to a local path
2. Upload via `POST /api/v1/clips`
3. Optionally create/find a tag and associate it with the clip for organization
4. Report the clip ID and filename to the user

Use descriptive filenames — they are the primary identifier when searching later.

**With a scoped key**: Skip step 3. Uploads are auto-tagged with the scoped tag. Organization is limited to filenames since additional tags cannot be created or applied.

### 2. Web Hosting via Tag-Serve

Host generated HTML sites, documentation, or static file collections over HTTP.

1. Create a tag for the site (e.g., `project-docs`, `landing-page`)
2. Upload all site files (HTML, CSS, JS, images) via the API
3. Tag each uploaded clip with the site tag
4. Ensure at least one file is named `index.html` for the root page
5. Start serving: `POST /api/v1/serve` with the tag ID and a port
6. Report the URL: `http://127.0.0.1:PORT/`

The serve port must differ from the API port. Choose a port in the 3000-9000 range.

**With a scoped key**: Skip steps 1 and 3. The scoped tag is the site tag — uploads are auto-tagged. List tags to get the scoped tag ID, then upload files and start serving with that tag ID. Scoped keys work well for dedicated hosting since all uploads land in the right tag automatically.

### 3. Research Knowledge Base

Store research findings, notes, and references for organized retrieval.

1. Upload research content with descriptive filenames (e.g., `rust-async-patterns.md`, `oauth2-flow-notes.txt`)
2. Create topic tags (e.g., `research-rust`, `research-security`)
3. Tag each clip with its topic(s)
4. To retrieve: search by text with `?search=` or filter by tag with `?tag=TAG_ID`
5. Download clip data for content that matches

**With a scoped key**: Skip steps 2-3. All uploads auto-tag with the scoped tag, so multi-topic organization is not available. Rely on descriptive filenames and the `?search=` parameter for retrieval instead. All clips share one tag, making search the primary discovery mechanism.

### 4. Interactive Web App with JSON API

Build self-contained web apps that persist data via JSON clips.

1. Create a tag for the app (e.g., `todo-app`)
2. Upload `index.html` with the app UI (uses `fetch('/_api/...', { credentials: 'include' })`)
3. Upload `todos.json` with initial data (e.g., `[]`)
4. Tag both clips with the app tag
5. Start serving with read-write API: `POST /api/v1/serve` with `"api_access": "readwrite"`
6. The HTML can now GET/POST/PUT/PATCH/DELETE via `/_api/todos/{id}`

**With a scoped key**: Skip steps 1 and 4. Use the scoped tag ID for serving.

## Important Notes

- Upload uses `multipart/form-data` (`curl -F`), not JSON body
- Tag-serve only includes non-archived clips
- Search matches on filename substring (case-insensitive)
- Maximum 200 clips per list request — use `offset` for pagination
- Tag-serve has CORS enabled (`Access-Control-Allow-Origin: *`)
- Each tag can only be served on one port at a time

## Additional Resources

For the complete API reference including archive/unarchive, tag update/delete, API key roles, pagination details, and error codes, consult:
- **`references/api-reference.md`** — Full REST API documentation
