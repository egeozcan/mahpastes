# mahresources Upload Plugin Design

## Overview

A Lua plugin for mahpastes that uploads clips to a mahresources instance. Supports both manual upload via card action and automatic upload on new clips.

## Approach

Pure Lua plugin — no Go changes needed. The HTTP API's string body passes raw bytes through `strings.NewReader`, so multipart form bodies can be constructed in Lua with `base64.decode()` for binary data.

## Plugin Manifest

- **Events:** `clip:created` (for auto-upload, gated by setting)
- **Network:** Static domain in manifest; user edits to match their server (e.g., `["localhost"] = {"POST"}`)
- **UI:** Card action "Upload to mahresources" (async, all file types)

### Settings

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `server_url` | text | `localhost:8181` | mahresources server (no protocol) |
| `owner_id` | text | `1` | Default owner group ID |
| `auto_upload` | checkbox | `false` | Auto-upload new clips |
| `content_filter` | select | `all` | Filter: "All files", "Images only", "Text only" |

## Core Logic

### upload_clip(clip_id)

1. Load settings from `storage.get()`
2. `clips.get(clip_id)` for metadata (filename, content_type)
3. Check content filter — skip if type doesn't match
4. `clips.get_data(clip_id)` → base64 string
5. `base64.decode(data)` → raw binary
6. Build multipart form body (resource file field + ownerId text field)
7. `http.post("http://" .. server_url .. "/v1/resource", ...)`
8. Parse response, toast success/error

### on_clip_created(data)

- Return early if `auto_upload` setting is disabled
- Call `upload_clip(data.id)`

### on_ui_action("upload", clip_ids, options)

- `task.start()` for batch progress
- Loop clip_ids, call `upload_clip()` each, `task.progress()` per clip
- Continue on individual failures, report count
- `task.complete()` or `task.fail()`

## Multipart Form Construction

~20 lines of Lua. Boundary string using `utils.time()`, `\r\n` separators per multipart spec. Fields: `resource` (file with filename + content type) and `ownerId` (text).

## Error Handling

- **Network errors / server down:** Toast error, don't disable plugin (transient)
- **Content filter skip:** Silent on auto-upload, toast on manual
- **Batch failures:** Continue on individual failures, report error count at end

## Configuration Note

The `network` block in the manifest must match the user's mahresources domain. Default is `localhost`. Users with remote instances must edit the network block in the Lua file.

## mahresources API

- **Endpoint:** `POST /v1/resource`
- **Format:** `multipart/form-data`
- **File field:** `resource`
- **Auth:** None (mahresources has no authentication)
- **Dedup:** Server-side SHA1 dedup; re-uploading the same file is harmless
