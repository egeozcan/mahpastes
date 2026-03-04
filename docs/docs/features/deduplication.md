---
sidebar_position: 9.5
---

# Duplicate Detection

mahpastes computes a SHA-256 content hash for every clip at upload time. Clips with identical content are grouped as duplicates.

## How It Works

1. On upload, the SHA-256 hash of the clip data is stored in the `content_hash` column
2. Existing clips without hashes are backfilled on startup
3. Clips sharing the same hash are considered duplicates
4. Empty hashes are excluded from detection

## Visual Indicators

![Duplicate detection badges on clip cards](/img/screenshots/deduplication.png)

- Clip cards show a **duplicate count** badge when duplicates exist
- A toast notification appears when uploading content that already exists

## Merging Duplicates

### Per-Clip

1. Right-click a clip card with duplicates
2. Select **Merge Duplicates** from the context menu
3. The oldest clip (lowest ID) is kept as the survivor
4. Tags from all duplicates are merged onto the survivor (INSERT OR IGNORE)
5. Duplicate clips are deleted
6. The survivor's `created_at` is bumped to now

Metadata is **not** merged -- only tags are combined.

### Bulk Deduplicate

When duplicates exist in your library, a **Deduplicate** button appears in the navigation drawer. Clicking it iterates over all duplicate groups and merges each one using the same logic as per-clip merge.

## Backend API

| Method | Description |
|--------|-------------|
| `GetDuplicateGroups()` | Returns groups of clips sharing the same content hash |
| `MergeDuplicates(ids)` | Merges a set of duplicate clip IDs, keeping the oldest |
| `DeduplicateAll()` | Merges all duplicate groups at once |

## Related

- [Clipboard Management](./clipboard-management.md) -- context menu overview
- [Bulk Actions](./bulk-actions.md) -- other batch operations
