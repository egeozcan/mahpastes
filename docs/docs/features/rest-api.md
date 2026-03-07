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

A key can be scoped to a single tag. When scoped:

- **List clips** returns only clips with that tag
- **Get / download / delete** a clip requires the clip to have that tag
- **Create clip** auto-applies the scoped tag to the new clip
- **Tag management** (create/update/delete tags) is forbidden
- **Add/remove tag on clip** is restricted to the scoped tag only

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
