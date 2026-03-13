---
sidebar_position: 9.5
---

# Duplicate Detection

mahpastes computes a SHA-256 content hash for every clip at upload time. Clips with identical content are grouped as duplicates.

## How It Works

1. On upload, the SHA-256 hash of the raw clip data is computed and stored in the `content_hash` column
2. When mahpastes starts, any existing clips that are missing a hash are backfilled automatically -- so duplicates among older clips are detected without any manual action
3. Clips sharing the same hash are considered duplicates
4. Empty hashes are excluded from detection

## Visual Indicators

![Duplicate detection badges on clip cards](/img/screenshots/deduplication.png)

- Clip cards show a **duplicate count** badge when duplicates exist
- A toast notification appears at upload time when the content already exists in your library, alerting you to the duplicate

## Merging Duplicates

### Per-Clip

1. Right-click a clip card that has a duplicate count badge
2. Select **Merge Duplicates** from the context menu
3. The oldest clip (lowest ID) is kept as the survivor
4. Tags from all duplicates are merged onto the survivor (INSERT OR IGNORE)
5. Duplicate clips are deleted and their temporary transfer files are cleaned up
6. The survivor's `created_at` is bumped to now so it appears at the top of the gallery
7. A `clip:deleted` plugin event is emitted for each removed duplicate

Metadata is **not** merged -- only tags are combined.

### Bulk Deduplicate

When duplicates exist in your library, a **Deduplicate** button appears in the navigation drawer. Clicking it opens a confirmation dialog that lists every duplicate group -- showing the filename, total copy count, and how many clips will be removed. After you confirm, mahpastes iterates over all groups and merges each one using the same logic as per-clip merge.

## REST API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/dedup` | List all duplicate groups |
| POST | `/api/v1/dedup/{clipId}/merge` | Merge a specific duplicate group, keeping the oldest clip |
| POST | `/api/v1/dedup/all` | Deduplicate all groups at once |

## CLI

The `mp` CLI provides the same deduplication commands:

```bash
# List all duplicate groups
mp dedup list

# Merge a specific duplicate group
mp dedup merge <clipId>

# Deduplicate all groups at once
mp dedup all
```

## Related

- [Clipboard Management](./clipboard-management.md) -- context menu overview
- [Bulk Actions](./bulk-actions.md) -- other batch operations
- [REST API](./rest-api.md#deduplication) -- deduplication endpoints
