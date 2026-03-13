---
sidebar_position: 15
---

# REST API

Expose clips and tags over an authenticated HTTP API for external tools, scripts, and integrations.

## Overview

The REST API runs a separate HTTP server with key-based authentication. Every request requires a Bearer token. Keys have roles that control access, and can optionally be scoped to a single tag.

**Base URL**: `http://127.0.0.1:{port}/api/v1`

**Content types**:
- All JSON responses use `Content-Type: application/json`
- Errors return `{ "error": "message" }`
- CORS headers are set on all responses (`Access-Control-Allow-Origin: *`)

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
| **editor** | All viewer permissions + create, delete, archive, unarchive, rename, expire, manage clip tags, metadata, clipboard | List tags | Read-write clips |
| **admin** | All editor permissions | Create, update, delete tags, manage watch folders, plugins, serve, backup, hidden tags, dedup-all | Full access |

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
- Attempting to serve a different tag returns `403 Forbidden`
- Serve management is restricted to the exact scoped tag, not its subtags

Unscoped keys (scope: "All tags") have access to all clips and tags within their role.

## Authentication

Include the key as a Bearer token:

```
Authorization: Bearer mp_a1b2c3d4e5f67890abcdef1234567890
```

Missing or invalid tokens return `401 Unauthorized`. Insufficient role returns `403 Forbidden`.

## Endpoint Reference

All paths below are relative to `/api/v1`.

### Clips

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `GET` | `/clips` | viewer | List clips (paginated, filterable) |
| `GET` | `/clips/{id}` | viewer | Get clip metadata |
| `GET` | `/clips/{id}/data` | viewer | Download raw clip content |
| `POST` | `/clips` | editor | Upload a new clip |
| `DELETE` | `/clips/{id}` | editor | Delete a clip |
| `PUT` | `/clips/{id}/archive` | editor | Archive a clip |
| `DELETE` | `/clips/{id}/archive` | editor | Unarchive a clip |
| `PATCH` | `/clips/{id}` | editor | Rename a clip |
| `PUT` | `/clips/{id}/expiration` | editor | Set expiration timer |
| `DELETE` | `/clips/{id}/expiration` | editor | Cancel expiration |

#### List clips

```
GET /api/v1/clips
```

Query parameters:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | int | 50 | Results per page (max 200) |
| `offset` | int | 0 | Pagination offset |
| `tag` | int | -- | Filter by tag ID |
| `content_type` | string | -- | Filter by exact content type |
| `archived` | bool | -- | Filter by archive status (`true` / `false`) |
| `search` | string | -- | Search filename and text content |

#### Get clip metadata

```
GET /api/v1/clips/{id}
```

Returns the clip object with `id`, `filename`, `content_type`, `size`, `created_at`, `archived`, `tags`, and other fields.

#### Download clip data

```
GET /api/v1/clips/{id}/data
```

Returns the raw file bytes with the clip's original content type and `Content-Length` header.

#### Upload a clip

```
POST /api/v1/clips
Content-Type: multipart/form-data
```

Send a file part in the multipart body (100 MB max). Optional `?filename=` query parameter overrides the uploaded filename.

Duplicate uploads (matching content hash) return the existing clip instead of creating a new one.

#### Delete a clip

```
DELETE /api/v1/clips/{id}
```

Returns `204 No Content` on success.

#### Archive / Unarchive

```
PUT    /api/v1/clips/{id}/archive     # Archive
DELETE /api/v1/clips/{id}/archive     # Unarchive
```

Returns `204 No Content` on success.

#### Rename a clip

```
PATCH /api/v1/clips/{id}
Content-Type: application/json
```

```json
{ "filename": "new-name.png" }
```

#### Set / Cancel expiration

```
PUT /api/v1/clips/{id}/expiration
Content-Type: application/json
```

```json
{ "minutes": 60 }
```

The clip is automatically deleted after the specified number of minutes.

```
DELETE /api/v1/clips/{id}/expiration
```

Cancels a pending expiration.

---

### Clip Metadata

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `GET` | `/clips/{id}/metadata` | viewer | Get all metadata key-value pairs |
| `PUT` | `/clips/{id}/metadata` | editor | Replace all metadata |
| `PUT` | `/clips/{id}/metadata/{key}` | editor | Set a single metadata value |
| `DELETE` | `/clips/{id}/metadata/{key}` | editor | Delete a single metadata key |

#### Get all metadata

```
GET /api/v1/clips/{id}/metadata
```

Returns a JSON object of key-value pairs: `{"source": "screenshot", "project": "docs"}`.

#### Replace all metadata

```
PUT /api/v1/clips/{id}/metadata
Content-Type: application/json
```

```json
{ "source": "screenshot", "project": "docs" }
```

Atomically replaces all existing metadata with the provided pairs.

#### Set a single key

```
PUT /api/v1/clips/{id}/metadata/{key}
Content-Type: application/json
```

```json
{ "value": "screenshot" }
```

#### Delete a single key

```
DELETE /api/v1/clips/{id}/metadata/{key}
```

Returns `204 No Content`.

---

### Clip-Tag Association

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `PUT` | `/clips/{id}/tags/{tagId}` | editor | Assign a tag to a clip |
| `DELETE` | `/clips/{id}/tags/{tagId}` | editor | Remove a tag from a clip |

:::note Tree Exclusivity
Assigning a tag automatically removes any existing tags from the same root tree. For example, adding `work/client2` removes `work/client1` if both share the `work` root.
:::

---

### Bulk Operations

All bulk endpoints accept a JSON body with an `ids` array. Minimum role is **editor** unless noted.

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `POST` | `/clips/bulk/delete` | editor | Delete multiple clips |
| `POST` | `/clips/bulk/archive` | editor | Archive multiple clips |
| `POST` | `/clips/bulk/unarchive` | editor | Unarchive multiple clips |
| `POST` | `/clips/bulk/expire` | editor | Set expiration on multiple clips |
| `POST` | `/clips/bulk/cancel-expire` | editor | Cancel expiration on multiple clips |
| `POST` | `/clips/bulk/tag` | editor | Add a tag to multiple clips |
| `POST` | `/clips/bulk/untag` | editor | Remove a tag from multiple clips |
| `POST` | `/clips/bulk/download` | viewer | Download multiple clips as a ZIP |
| `POST` | `/clips/bulk/copy` | editor | Copy clip content to system clipboard |

#### Request bodies

**Delete / Archive / Unarchive / Cancel-expire:**

```json
{ "ids": [1, 2, 3] }
```

**Expire:**

```json
{ "ids": [1, 2, 3], "minutes": 60 }
```

**Tag / Untag:**

```json
{ "ids": [1, 2, 3], "tag_id": 5 }
```

**Download:**

```json
{ "ids": [1, 2, 3] }
```

Returns a `application/zip` response containing the clip files.

**Copy:**

```json
{ "ids": [1] }
```

Copies the first clip's content to the system clipboard.

---

### Tags

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `GET` | `/tags` | viewer | List all tags |
| `POST` | `/tags` | admin | Create a tag |
| `PUT` | `/tags/{id}` | admin | Update tag name and color |
| `DELETE` | `/tags/{id}` | admin | Delete a tag |
| `GET` | `/tags/{id}/children` | viewer | List direct child tags |
| `GET` | `/tags/{id}/clips` | viewer | List clips with this tag |
| `GET` | `/tags/hidden` | viewer | Get hidden tag IDs |
| `PUT` | `/tags/hidden` | admin | Set hidden tag IDs |

#### Create a tag

```
POST /api/v1/tags
Content-Type: application/json
```

```json
{ "name": "work/client1" }
```

Use `/` separators to create hierarchical tags. Parent tags are created automatically if they don't exist.

:::note Reserved Names
Tag names cannot contain `_api` as a path segment. See [Tag Serve](tag-serve.md#reserved-tag-names) for details.
:::

#### Update a tag

```
PUT /api/v1/tags/{id}
Content-Type: application/json
```

```json
{ "name": "work/client2", "color": "#e74c3c" }
```

#### Delete a tag

```
DELETE /api/v1/tags/{id}
```

Returns `204 No Content`. Deleting a parent tag does not delete its children.

#### List child tags

```
GET /api/v1/tags/{id}/children
```

Returns an array of direct child tags (one level deep).

#### List clips for a tag

```
GET /api/v1/tags/{id}/clips
```

Query parameters:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | int | 50 | Results per page |
| `offset` | int | 0 | Pagination offset |

#### Get / Set hidden tags

```
GET /api/v1/tags/hidden
```

Returns `{ "ids": [1, 2] }`.

```
PUT /api/v1/tags/hidden
Content-Type: application/json
```

```json
{ "ids": [1, 2] }
```

Replaces the full list of hidden tag IDs.

---

### Deduplication

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `GET` | `/dedup` | viewer | List duplicate groups |
| `POST` | `/dedup/{clipId}/merge` | editor | Merge duplicates (keeps specified clip) |
| `POST` | `/dedup/all` | admin | Deduplicate all groups at once |

#### List duplicates

```
GET /api/v1/dedup
```

Returns an array of duplicate groups. Each group contains clips that share the same content hash.

#### Merge duplicates

```
POST /api/v1/dedup/{clipId}/merge
```

Keeps the specified clip as the survivor. Tags from all duplicates are merged onto it, then the duplicates are deleted.

#### Deduplicate all

```
POST /api/v1/dedup/all
```

Iterates over all duplicate groups and merges each one, keeping the oldest clip in each group.

---

### Watch Folders

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `GET` | `/watch` | viewer | List watch folders |
| `GET` | `/watch/status` | viewer | Get watch system status |
| `GET` | `/watch/{id}` | viewer | Get a specific folder |
| `POST` | `/watch` | admin | Add a watch folder |
| `PUT` | `/watch/{id}` | admin | Update folder config |
| `DELETE` | `/watch/{id}` | admin | Remove a folder |
| `PUT` | `/watch/{id}/pause` | admin | Pause a folder |
| `DELETE` | `/watch/{id}/pause` | admin | Resume a folder |
| `POST` | `/watch/{id}/process` | admin | Process existing files |
| `PUT` | `/watch/global-pause` | admin | Pause all watchers |
| `DELETE` | `/watch/global-pause` | admin | Resume all watchers |

#### Add a watch folder

```
POST /api/v1/watch
Content-Type: application/json
```

```json
{
  "path": "/Users/me/Screenshots",
  "filter_mode": "presets",
  "filter_presets": ["images"],
  "filter_regex": "",
  "auto_tag_id": 5,
  "auto_archive": false
}
```

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | Absolute path to the folder |
| `filter_mode` | string | `"all"`, `"presets"`, or `"regex"` |
| `filter_presets` | string[] | Preset names when `filter_mode` is `"presets"` |
| `filter_regex` | string | Regex pattern when `filter_mode` is `"regex"` |
| `auto_tag_id` | int | Tag ID to auto-apply to imports (0 for none) |
| `auto_archive` | bool | Auto-archive imported files |

#### Update a watch folder

```
PUT /api/v1/watch/{id}
Content-Type: application/json
```

Same body fields as the add endpoint.

#### Pause / Resume a folder

```
PUT    /api/v1/watch/{id}/pause    # Pause
DELETE /api/v1/watch/{id}/pause    # Resume
```

Returns `204 No Content`.

#### Global pause / resume

```
PUT    /api/v1/watch/global-pause    # Pause all
DELETE /api/v1/watch/global-pause    # Resume all
```

Returns `204 No Content`.

#### Process existing files

```
POST /api/v1/watch/{id}/process
```

Triggers a one-time scan of all existing files in the folder.

---

### Plugins

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `GET` | `/plugins` | viewer | List installed plugins |
| `GET` | `/plugins/actions` | viewer | List all plugin UI actions |
| `POST` | `/plugins` | admin | Install a plugin |
| `POST` | `/plugins/check-updates` | admin | Check for available updates |
| `DELETE` | `/plugins/{id}` | admin | Remove a plugin |
| `PUT` | `/plugins/{id}/enable` | admin | Enable a plugin |
| `PUT` | `/plugins/{id}/disable` | admin | Disable a plugin |
| `GET` | `/plugins/{id}/storage` | admin | Get all plugin storage |
| `GET` | `/plugins/{id}/storage/{key}` | admin | Get a storage value |
| `PUT` | `/plugins/{id}/storage/{key}` | admin | Set a storage value |
| `POST` | `/plugins/{id}/actions/{actionId}` | editor | Execute a plugin action |
| `POST` | `/plugins/{id}/update` | admin | Update a plugin |

#### Install a plugin

```
POST /api/v1/plugins
Content-Type: application/json
```

Install from a URL:

```json
{ "url": "https://example.com/plugin.lua" }
```

Install from a local path:

```json
{ "path": "/Users/me/plugins/my-plugin.lua" }
```

#### Remove a plugin

```
DELETE /api/v1/plugins/{id}
```

Returns `204 No Content`.

#### Enable / Disable

```
PUT /api/v1/plugins/{id}/enable
PUT /api/v1/plugins/{id}/disable
```

Returns `204 No Content`.

#### Plugin storage

```
GET /api/v1/plugins/{id}/storage
```

Returns all key-value pairs for the plugin.

```
GET /api/v1/plugins/{id}/storage/{key}
```

Returns `{ "value": "..." }`.

```
PUT /api/v1/plugins/{id}/storage/{key}
Content-Type: application/json
```

```json
{ "value": "my-api-key-here" }
```

#### Execute a plugin action

```
POST /api/v1/plugins/{id}/actions/{actionId}
Content-Type: application/json
```

```json
{ "clip_id": 42, "options": { "style": "watercolor" } }
```

Pass `clip_id` to identify the target clip and `options` for any form fields defined in the action manifest.

#### Check for updates

```
POST /api/v1/plugins/check-updates
```

Returns update availability info for all installed plugins.

#### Update a plugin

```
POST /api/v1/plugins/{id}/update
```

Downloads and installs the latest version from the plugin's source URL.

---

### Serve (Tag Hosting)

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `GET` | `/serve` | viewer | List running tag servers |
| `POST` | `/serve` | admin | Start serving a tag |
| `DELETE` | `/serve/{tagId}` | admin | Stop serving a tag |

#### Start serving

```
POST /api/v1/serve
Content-Type: application/json
```

```json
{
  "tag_id": 5,
  "port": 0,
  "bind_all": false,
  "api_access": "none"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `tag_id` | int | ID of the tag to serve (required) |
| `port` | int | Port number, or `0` to auto-assign |
| `bind_all` | bool | Bind to `0.0.0.0` instead of `127.0.0.1` |
| `api_access` | string | `"none"` (default), `"read"`, or `"readwrite"` -- controls the [JSON API](tag-serve.md#json-api) |

- Returns `201 Created` with server info on success
- Returns `409 Conflict` if the tag is already being served or the port is unavailable

:::tip Scoped Keys
Tag-scoped admin keys can start and stop servers for their own scoped tag. They receive `403 Forbidden` only when attempting to serve a different tag.
:::

#### Stop serving

```
DELETE /api/v1/serve/{tagId}
```

Returns `204 No Content`. Returns `404` if no server is running for that tag.

---

### Backup

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `GET` | `/backup` | admin | Download a backup ZIP |
| `POST` | `/backup/restore` | admin | Restore from a backup ZIP |

#### Download backup

```
GET /api/v1/backup
```

Returns an `application/zip` file containing the full database and all clip data.

#### Restore from backup

```
POST /api/v1/backup/restore
Content-Type: multipart/form-data
```

Upload a backup ZIP file as a multipart form. This replaces the current database and clip data.

---

### Clipboard

| Method | Path | Min Role | Description |
|--------|------|----------|-------------|
| `POST` | `/clipboard/copy` | editor | Copy clip content to system clipboard |
| `POST` | `/clipboard/copy-file` | editor | Copy clip as a file reference |

#### Copy content

```
POST /api/v1/clipboard/copy
Content-Type: application/json
```

```json
{ "clip_id": 1 }
```

Copies the clip's raw content (text or image data) to the system clipboard.

#### Copy as file

```
POST /api/v1/clipboard/copy-file
Content-Type: application/json
```

```json
{ "clip_id": 1 }
```

Places a file reference on the system clipboard (macOS: NSPasteboard file URL, Windows: PowerShell SetFileDropList). You can then paste the clip as a file into Finder, Explorer, or other apps.

## Error Responses

All errors return JSON with an `error` field:

```json
{ "error": "clip not found" }
```

Common status codes:

| Status | Meaning |
|--------|---------|
| `400` | Bad request (invalid JSON, missing fields) |
| `401` | Missing or invalid API key |
| `403` | Insufficient role or tag scope violation |
| `404` | Resource not found |
| `409` | Conflict (duplicate, port in use) |
| `413` | Request body too large |
| `500` | Internal server error |

## Related

- [Tag Serve](tag-serve.md) -- unauthenticated file serving for quick sharing, plus JSON API and [file upload](tag-serve.md#file-upload) for served HTML apps
- [Tags](tags.md) -- create and manage tags used for scoping
- [CLI (`mp`)](../tutorials/cli-workflow.md) -- command-line interface that wraps this REST API
