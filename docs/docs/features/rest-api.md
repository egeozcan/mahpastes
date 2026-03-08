---
sidebar_position: 15
---

# REST API

Expose clips and tags over an authenticated HTTP API for external tools, scripts, and integrations.

## Overview

The REST API runs a separate HTTP server with key-based authentication. Every request requires a Bearer token. Keys have roles that control access, and can optionally be scoped to a single tag.

## Starting the Server

1. Open the menu drawer and click **API** (or the API button in settings)
2. Set a port (default: `8484`)
3. Optionally enable **Network** toggle to bind to `0.0.0.0` instead of `127.0.0.1`
4. Click **Start Server**

The status line shows the server URL and request count while running.

![API settings](/img/screenshots/api-settings.png)

## Stopping the Server

Click **Stop Server** in the API modal. The server shuts down gracefully with a 5-second timeout.

## API Keys

Every request must include a valid API key. Keys are managed in the API modal.

### Creating a Key

1. Click **+ New Key**
2. Enter a name (e.g. "CI pipeline")
3. Select a role: **Viewer**, **Editor**, or **Admin**
4. Optionally select a tag scope to restrict the key to a single tag
5. Click **Create Key**

The plaintext key is shown once. Copy it immediately -- it cannot be retrieved later. Keys are stored as SHA-256 hashes.

Keys are prefixed with `mp_` (e.g. `mp_a1b2c3d4e5f6...`).

### Revoking a Key

Click **Revoke** on any active key card. The key stops working immediately. Revoked keys remain visible in the list but are grayed out.

## Roles

| Role | Clips | Tags | Scope |
|------|-------|------|-------|
| **viewer** | List, get metadata, download data | List tags | Read-only |
| **editor** | All viewer permissions + create, delete, archive, unarchive, manage clip tags | List tags | Read-write clips |
| **admin** | All editor permissions | Create, update, delete tags | Full access |

## Tag Scope

A key can be scoped to a single tag. When scoped, the key has access to the **full subtree** rooted at that tag -- the scoped tag itself plus all of its descendants.

### Clip Access

- **List clips** returns clips tagged with the scoped tag or any of its subtags
- **Get / download / delete** a clip requires the clip to have the scoped tag or one of its subtags
- **Create clip** auto-applies the scoped tag to the new clip

### Tag Management (Admin Role)

Scoped admin keys can manage tags within their subtree:

- **Create tags** -- can create new subtags under the scoped tag (e.g., a key scoped to `work` can create `work/client2`)
- **Update tags** -- can rename or recolor tags within the subtree
- **Delete tags** -- can delete tags within the subtree
- **List tags** returns the scoped tag and all of its descendants

Tags outside the subtree are not visible or modifiable.

### Clip-Tag Association

- **Add/remove tag on clip** is restricted to tags within the scoped subtree
- The clip must also belong to the scoped subtree

### Serve Management

- Scoped admin keys can start and stop serving for their scoped tag
- Serve management is restricted to the exact scoped tag, not its subtags

Unscoped keys (scope: "All tags") have access to all clips and tags within their role.

## Authentication

Include the key as a Bearer token:

```
Authorization: Bearer mp_a1b2c3d4e5f67890abcdef1234567890
```

Missing or invalid tokens return `401 Unauthorized`. Insufficient role returns `403 Forbidden`.

## Endpoints

All endpoints are prefixed with `/api/v1`.

### Clips

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `GET` | `/clips` | viewer | List clips (paginated) |
| `GET` | `/clips/{id}` | viewer | Get clip metadata |
| `GET` | `/clips/{id}/data` | viewer | Download clip file data |
| `POST` | `/clips` | editor | Upload a new clip (multipart/form-data) |
| `DELETE` | `/clips/{id}` | editor | Delete a clip |
| `PUT` | `/clips/{id}/archive` | editor | Archive a clip |
| `DELETE` | `/clips/{id}/archive` | editor | Unarchive a clip |

### Tags

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `GET` | `/tags` | viewer | List all tags |
| `POST` | `/tags` | admin | Create a tag |
| `PUT` | `/tags/{id}` | admin | Update tag name and color |
| `DELETE` | `/tags/{id}` | admin | Delete a tag |

### Clip-Tag Association

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `PUT` | `/clips/{id}/tags/{tagId}` | editor | Add a tag to a clip |
| `DELETE` | `/clips/{id}/tags/{tagId}` | editor | Remove a tag from a clip |

### Serve (Tag Hosting)

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `GET` | `/serve` | viewer | List running tag servers |
| `POST` | `/serve` | admin | Start serving a tag |
| `DELETE` | `/serve/{tagId}` | admin | Stop serving a tag |

`POST /api/v1/serve` accepts a JSON body:

```json
{
  "tag_id": 5,
  "port": 0,
  "bind_all": false
}
```

- `port`: Set to `0` (or omit) to auto-assign an available port
- `bind_all`: Bind to `0.0.0.0` instead of `127.0.0.1`
- Returns `201` with `ServeInfo` on success
- Returns `409` if the tag is already being served or the port is unavailable
- Tag-scoped keys cannot start or stop servers (returns `403`), but can list servers filtered to their tag

## Query Parameters

`GET /api/v1/clips` supports:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | int | 50 | Results per page (max 200) |
| `offset` | int | 0 | Pagination offset |
| `tag` | int | -- | Filter by tag ID |
| `content_type` | string | -- | Filter by exact content type |
| `archived` | bool | -- | Filter by archive status (`true` / `false`) |
| `search` | string | -- | Search filename and text content |

## Request and Response Format

- All JSON responses use `Content-Type: application/json`
- Errors return `{ "error": "message" }`
- `GET /clips/{id}/data` returns the raw file bytes with the clip's content type
- `POST /clips` accepts `multipart/form-data` with a file part (100 MB max). Optional `?filename=` query parameter overrides the filename
- Duplicate uploads (matching content hash) return the existing clip instead of creating a new one
- CORS headers are set on all responses (`Access-Control-Allow-Origin: *`)

## Related

- [Tag Serve](tag-serve.md) -- unauthenticated file serving for quick sharing
- [Tags](tags.md) -- create and manage tags used for scoping
