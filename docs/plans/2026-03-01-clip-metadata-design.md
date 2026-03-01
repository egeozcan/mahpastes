# Clip Metadata Design

**Date**: 2026-03-01
**Status**: Approved

## Overview

Add string key-value metadata support to clips. Users can view and edit metadata via a centered modal accessed from the card context menu. Plugins can read/write metadata silently via a new Lua API.

## Decisions

- **Visibility**: Modal only — no metadata shown on clip cards
- **Modal style**: Centered modal (like settings), not a popover
- **Access**: Card context menu only, not in lightbox
- **Plugin access**: Silent — no permission required
- **Limits**: 50 key-value pairs per clip
- **Storage**: JSON column on clips table

## Data Layer

### Schema Migration

```sql
ALTER TABLE clips ADD COLUMN metadata TEXT DEFAULT '{}';
```

### Go Representation

Add `Metadata map[string]string` to `ClipPreview`. Deserialize on read, serialize on write. Enforce 50-pair limit in Go methods.

### App Methods

- `GetClipMetadata(clipID int64) -> map[string]string, error` — read and parse JSON column
- `SetClipMetadata(clipID int64, key string, value string) -> error` — read-modify-write single key
- `DeleteClipMetadata(clipID int64, key string) -> error` — remove single key
- `SetClipMetadataBulk(clipID int64, metadata map[string]string) -> error` — replace all metadata

## Frontend — Meta Modal

### Entry Point

New "Metadata" action in card context menu, placed between "Tags" and "Set Expiration".

### Modal Structure

- **Header**: "Metadata" title + close button
- **Content**: Scrollable list of key-value rows. Each row:
  - Text input for key (~40% width)
  - Text input for value (~50% width)
  - Delete button (X icon)
- **Footer**: "Add Field" button (secondary, left) + "Save" button (primary, right)
- **Empty state**: "No metadata. Click 'Add Field' to get started."

### Behavior

- Opens with current metadata loaded via `GetClipMetadata`
- Changes are local until Save
- Save calls `SetClipMetadataBulk` to replace all metadata
- Duplicate keys show visual warning
- Standard form input styling from design system

### New File

`frontend/js/metadata.js` — modal open/close, row rendering, save logic.

## Plugin API

### New Module: `metadata`

File: `plugin/api_metadata.go`

Functions:
- `metadata.get(clip_id)` — returns Lua table of all key-value pairs
- `metadata.set(clip_id, key, value)` — upsert single key-value pair
- `metadata.delete(clip_id, key)` — remove single key
- `metadata.set_bulk(clip_id, table)` — replace all metadata

No permission required. Follows `api_tags.go` pattern.

### Example Usage

```lua
metadata.set(new_clip.id, "generator", "fal-ai")
metadata.set(new_clip.id, "model", model_id)
metadata.set(new_clip.id, "prompt", options.prompt)
```

## Testing

### New Directory: `e2e/tests/metadata/`

### Test Cases

- Open meta modal from card context menu
- Add a key-value pair and save
- Edit an existing value and save
- Delete a key-value pair and save
- Verify empty state message
- Verify 50 pair limit
- Verify duplicate key prevention
- Verify metadata persists after close/reopen
- Verify metadata deleted when clip is deleted

### AppHelper Additions

- `openMetadataModal(clipName)` — right-click card, click "Metadata"
- `addMetadata(key, value)` — add a row in the modal
- `saveMetadata()` — click Save
- `expectMetadataRow(key, value)` — assert a row exists

### Selector Additions

New selectors for metadata modal, rows, inputs, and buttons.
