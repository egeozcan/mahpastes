# mahpastes REST API Reference

## Base URL

`$MAHPASTES_API_URL` (default: `http://127.0.0.1:8484`)

## Authentication

All requests require: `Authorization: Bearer $MAHPASTES_API_KEY`

API keys have roles with increasing privilege:
- **viewer** (level 0): List and view clips/tags, list servers
- **editor** (level 1): Create, delete, archive clips; manage clip-tag associations
- **admin** (level 2): Full access including tag CRUD and serve management

Keys can optionally be scoped to a single tag. Scoped keys restrict API behavior as documented per-endpoint below.

## Tag-Scoped Key Behavior Summary

A tag-scoped key confines all operations to clips within its scoped tag:

| Operation | Scoped Key Behavior |
|-----------|-------------------|
| List clips | Only clips with scoped tag; requesting `?tag=OTHER` returns empty |
| Get/download clip | 403 if clip not in scoped tag |
| Upload clip | Auto-applies scoped tag to new clip (and to existing duplicate) |
| Delete/archive clip | 403 if clip not in scoped tag |
| List tags | Returns only the scoped tag (single-element array) |
| Create/update/delete tag | 403: `"tag-scoped keys cannot manage tags"` |
| Add/remove tag on clip | Can only add/remove the scoped tag; 403 for other tags |
| List servers | Only shows servers for scoped tag |
| Start/stop serve | 403 if tag ID differs from scoped tag |

**Detection**: List tags (`GET /api/v1/tags`). A scoped key returns a single-element array containing only its scoped tag. An unscoped key returns all tags.

## Clip Endpoints

### List Clips

`GET /api/v1/clips`

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| limit | int | 50 | Max results (1-200) |
| offset | int | 0 | Pagination offset |
| search | string | — | Filter by filename substring (case-insensitive) |
| tag | int | — | Filter by tag ID |
| content_type | string | — | Filter by MIME type |
| archived | bool | false | Include archived clips |

Response:

```json
{
  "clips": [
    {
      "id": 1,
      "filename": "example.txt",
      "content_type": "text/plain",
      "size": 1024,
      "is_archived": false,
      "created_at": "2026-03-08T10:30:00Z",
      "tags": [{"id": 1, "name": "research", "color": ""}]
    }
  ],
  "total": 42,
  "limit": 50,
  "offset": 0
}
```

### Get Clip

`GET /api/v1/clips/{id}`

Response: Single clip object (same shape as list item).

### Download Clip Data

`GET /api/v1/clips/{id}/data`

Response: Raw file content with appropriate `Content-Type` header.

```bash
curl -s -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  "$MAHPASTES_API_URL/api/v1/clips/42/data" -o document.pdf
```

### Upload Clip

`POST /api/v1/clips`

Content-Type: `multipart/form-data`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| file | file | Yes | File to upload |

| Query Parameter | Type | Description |
|-----------------|------|-------------|
| filename | string | Override the uploaded filename |

```bash
curl -s -X POST \
  -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  -F "file=@/path/to/file.txt" \
  "$MAHPASTES_API_URL/api/v1/clips?filename=custom-name.txt"
```

Response (201): Clip object with `id`, `filename`, `content_type`, `size`, `tags`.

**Scoped key**: The scoped tag is automatically applied to the new clip. If a duplicate already exists (same content hash), the scoped tag is added to the existing clip and that clip is returned.

### Delete Clip

`DELETE /api/v1/clips/{id}`

Response: 204 No Content

### Archive Clip

`PUT /api/v1/clips/{id}/archive`

Response: 200 OK

### Unarchive Clip

`DELETE /api/v1/clips/{id}/archive`

Response: 200 OK

## Tag Endpoints

### List Tags

`GET /api/v1/tags`

Response: Array of tag objects.

```json
[
  {"id": 1, "name": "research", "color": ""},
  {"id": 2, "name": "website", "color": "#4a90d9"}
]
```

### Create Tag

`POST /api/v1/tags`

Body: `{"name": "tag-name"}`

Response (201): Tag object with `id`, `name`, `color`.

### Update Tag

`PUT /api/v1/tags/{id}`

Body: `{"name": "new-name", "color": "#hex"}`

Response: 200 OK

### Delete Tag

`DELETE /api/v1/tags/{id}`

Response: 204 No Content

## Clip-Tag Association Endpoints

### Add Tag to Clip

`PUT /api/v1/clips/{clipId}/tags/{tagId}`

Response: 200 OK

### Remove Tag from Clip

`DELETE /api/v1/clips/{clipId}/tags/{tagId}`

Response: 200 OK

## Tag-Serve Endpoints

### List Running Servers

`GET /api/v1/serve`

Response: Array of server status objects.

```json
[
  {
    "tag_id": 1,
    "tag_name": "my-site",
    "port": 3000,
    "bind_all": false,
    "request_count": 42
  }
]
```

### Start Serving

`POST /api/v1/serve`

Body:

```json
{
  "tag_id": 1,
  "port": 3000,
  "bind_all": false
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| tag_id | int | Yes | Tag to serve |
| port | int | Yes | HTTP port (1024-65535) |
| bind_all | bool | No | Bind to 0.0.0.0 instead of 127.0.0.1 (default: false) |
| api_access | string | No | JSON API access: `"none"` (default), `"read"`, `"readwrite"` |

When `api_access` is `"read"` or `"readwrite"`, the tag server exposes a JSON API at `/_api/` that HTML clips can use to read/write JSON clips in the same tag.

Response (200): Server status object.

Served files are accessible at `http://127.0.0.1:PORT/FILENAME`. If a clip named `index.html` exists in the tag, it is served at the root path `/`.

### Stop Serving

`DELETE /api/v1/serve/{tagId}`

Response: 204 No Content

## Error Responses

All errors return JSON:

```json
{
  "error": "Description of the error"
}
```

Common status codes:
- **400**: Bad request (invalid parameters)
- **401**: Unauthorized (missing or invalid API key)
- **403**: Forbidden (insufficient role, or tag-scoped key accessing resources outside its scope)
- **404**: Not found
- **409**: Conflict (e.g., tag already being served, port in use)
- **500**: Internal server error

## Notes

- CORS is enabled on all endpoints (`Access-Control-Allow-Origin: *`)
- Tag-serve excludes archived clips from the served file list
- Search is a case-insensitive substring match on filename
- Filenames are automatically deduplicated in tag-serve (appends ` (2)`, ` (3)`, etc.)
- The `file` field in upload must be `multipart/form-data`, not JSON
- Maximum file count per list request is 200; use `offset` for pagination
