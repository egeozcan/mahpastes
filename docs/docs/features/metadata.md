---
sidebar_position: 5.5
---

# Clip Metadata

Attach arbitrary key-value pairs to any clip. Metadata is stored as a JSON column on the clips table and backed up with your data.

![Metadata modal](/img/screenshots/metadata-modal.png)

## Adding Metadata

1. Right-click a clip card (or click the three-dot menu)
2. Select **Metadata**
3. In the modal, type a key and value
4. Click **Add** to save the pair

## Editing and Deleting

The metadata modal shows all existing pairs for the clip. Each row has a delete button to remove that key. Edit a value by deleting and re-adding the key.

## Limits

| Constraint | Limit |
|------------|-------|
| Key length | 256 characters |
| Value length | 4096 characters |
| Pairs per clip | 50 |

Writes use an atomic read-modify-write inside a database transaction, so concurrent updates do not corrupt data.

## Backend API

| Method | Description |
|--------|-------------|
| `GetClipMetadata(id)` | Returns all key-value pairs for a clip |
| `SetClipMetadata(id, key, value)` | Sets a single key-value pair |
| `DeleteClipMetadata(id, key)` | Removes a single key |
| `SetClipMetadataBulk(id, metadata)` | Atomically replaces all metadata |

## Plugin API

Plugins access metadata through the `metadata` module:

```lua
-- Get all metadata for a clip
local meta = metadata.get(clip_id)

-- Set a key-value pair
metadata.set(clip_id, "source", "screenshot")

-- Delete a key
metadata.delete(clip_id, "source")

-- Replace all metadata atomically
metadata.set_bulk(clip_id, {
    source = "screenshot",
    project = "docs",
})
```

The same limits (key 256 chars, value 4096 chars, 50 pairs) apply to plugin calls.

## Backup

Metadata is included in backup ZIP files and restored along with clips. The JSON column travels with the clip row, so no separate export step is needed.

## Related

- [Clipboard Management](./clipboard-management.md) -- context menu overview
- [Plugin API Reference](../plugins/api-reference.md) -- metadata module reference
