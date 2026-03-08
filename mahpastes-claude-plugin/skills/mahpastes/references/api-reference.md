# mahpastes REST API Reference

Base URL: `$MAHPASTES_API_URL` (default `http://127.0.0.1:8484`)

All requests require `Authorization: Bearer <api-key>` header.

## Clips

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `GET` | `/api/v1/clips` | viewer | List clips (paginated) |
| `GET` | `/api/v1/clips/{id}` | viewer | Get clip metadata |
| `GET` | `/api/v1/clips/{id}/data` | viewer | Download clip file data |
| `POST` | `/api/v1/clips` | editor | Upload a new clip (multipart/form-data) |
| `DELETE` | `/api/v1/clips/{id}` | editor | Delete a clip |
| `PUT` | `/api/v1/clips/{id}/archive` | editor | Archive a clip |
| `DELETE` | `/api/v1/clips/{id}/archive` | editor | Unarchive a clip |

### Query Parameters for `GET /api/v1/clips`

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | int | 50 | Results per page (max 200) |
| `offset` | int | 0 | Pagination offset |
| `tag` | int | — | Filter by tag ID |
| `content_type` | string | — | Filter by exact content type |
| `archived` | bool | — | Filter by archive status |
| `search` | string | — | Search filename and text content |

### Clip Response

```json
{
  "id": 42,
  "filename": "screenshot.png",
  "content_type": "image/png",
  "size": 125000,
  "is_archived": false,
  "created_at": "2026-03-01T12:00:00Z",
  "tags": [{"id": 1, "name": "docs", "color": "#abcdef", "count": 5}]
}
```

### Upload

```bash
curl -X POST -F "file=@./myfile.png" \
  -H "Authorization: Bearer $KEY" \
  "$URL/api/v1/clips?filename=myfile.png"
```

- Max file size: 100 MB
- Duplicate content (matching SHA-256 hash) returns the existing clip
- Optional `?filename=` query parameter overrides the uploaded filename

## Tags

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `GET` | `/api/v1/tags` | viewer | List all tags with clip counts |
| `POST` | `/api/v1/tags` | admin | Create a tag |
| `PUT` | `/api/v1/tags/{id}` | admin | Update tag name and color |
| `DELETE` | `/api/v1/tags/{id}` | admin | Delete a tag |

### Tag Response

```json
{"id": 1, "name": "docs", "color": "#abcdef", "count": 5}
```

## Clip-Tag Association

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `PUT` | `/api/v1/clips/{id}/tags/{tagId}` | editor | Add a tag to a clip |
| `DELETE` | `/api/v1/clips/{id}/tags/{tagId}` | editor | Remove a tag from a clip |

## Serve (Tag Hosting)

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `GET` | `/api/v1/serve` | viewer | List running tag servers |
| `POST` | `/api/v1/serve` | admin | Start serving a tag |
| `DELETE` | `/api/v1/serve/{tagId}` | admin | Stop serving a tag |

### Start Serving

```bash
curl -X POST -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"tag_id": 5, "port": 0, "bind_all": false}' \
  "$URL/api/v1/serve"
```

- `port: 0` → auto-assign available port
- Returns `201` with `ServeInfo`
- Returns `409` if already serving or port unavailable

### ServeInfo Response

```json
{
  "tag_id": 5,
  "tag_name": "docs",
  "port": 9100,
  "bind_all": false,
  "url": "http://127.0.0.1:9100",
  "running": true,
  "request_count": 0
}
```

## Roles

| Role | Clips | Tags | Serve |
|------|-------|------|-------|
| **viewer** | List, get, download | List | List servers |
| **editor** | + create, delete, archive, tag | List | List servers |
| **admin** | + all editor | + create, update, delete | + start, stop |

## Tag Scope

Keys can be scoped to a single tag. When scoped:
- Clip operations are restricted to clips with that tag
- Tag management (create/update/delete) is forbidden
- Serve management (start/stop) is forbidden
- List servers is filtered to the scoped tag

## Error Responses

```json
{"error": "description of what went wrong"}
```

| Status | Meaning |
|--------|---------|
| 400 | Bad request (invalid JSON, missing fields) |
| 401 | Missing or invalid API key |
| 403 | Insufficient permissions (role or scope) |
| 404 | Resource not found |
| 409 | Conflict (already serving, port unavailable) |
