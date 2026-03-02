# Clip Deduplication Design

## Goal

Add content hashing to clips and provide deduplication tools. Duplicates are always allowed but visually marked, with merge actions to consolidate them.

## Database

Add `content_hash TEXT` column to the `clips` table with an index for fast lookups. Backfill existing clips on startup by computing SHA-256 in Go (SQLite lacks a native sha256 function).

```sql
ALTER TABLE clips ADD COLUMN content_hash TEXT DEFAULT '';
CREATE INDEX idx_clips_content_hash ON clips(content_hash);
```

Backfill: query clips with empty hash, compute SHA-256 of `data` blob in Go, update each row.

## Hashing

SHA-256 of raw content bytes only — filename and content type are not factored in. Two clips with identical content but different filenames are considered duplicates.

Computed in Go at insert time across all 5 creation paths:

1. `App.UploadFileAndGetID()` — after base64 decode
2. `App.UploadFiles()` — per file in batch
3. `WatcherManager.importFile()` — inherits from UploadFileAndGetID
4. `ClipsAPI.create()` — plugin API
5. `ClipsAPI.createFromURL()` — after download

## Duplicate Detection

After hashing, check `SELECT COUNT(*) FROM clips WHERE content_hash = ? AND id != ?`. If duplicates exist, emit a Wails event so the frontend shows a toast: "Duplicate clip detected — N other copies exist".

Clips are always inserted regardless — duplicates are allowed.

## ClipPreview Changes

Add `DuplicateCount int` to `ClipPreview` struct. The `GetClips()` query adds a subquery:

```sql
(SELECT COUNT(*) FROM clips c2 WHERE c2.content_hash = c.content_hash AND c2.id != c.id) AS duplicate_count
```

## UI Badge

When `duplicate_count > 0`, show a badge on the card in the info area:

```html
<span class="text-[9px] font-medium text-stone-400 bg-stone-100 border border-stone-200 rounded px-1">
  2 copies
</span>
```

No badge when count is 0.

## Card Action: Merge Duplicates

Added to card context menu (`renderCardMenu`), only shown when `duplicate_count > 0`.

Backend method: `App.MergeDuplicates(clipID int64)`

Behavior:
1. Look up `content_hash` for the given clip
2. Find all clips with that hash
3. Keep the oldest clip (lowest `id`)
4. Merge all tags from duplicates onto the survivor via `INSERT OR IGNORE INTO clip_tags`
5. Delete duplicate clips
6. Update survivor's `created_at` to now (moves to top of gallery)
7. Emit `clip:deleted` plugin events for removed clips
8. Reload gallery, show toast: "Merged N duplicates"

Entire operation wrapped in a transaction.

## Sidebar Action: Deduplicate All

Button in the sidebar nav (alongside Clear All, Settings, Plugins).

Backend methods:
- `App.GetDuplicateGroups()` — returns groups of clips sharing the same `content_hash` (only groups with 2+ clips), with preview info per clip
- `App.DeduplicateAll()` — runs merge logic for every duplicate group in one transaction

Frontend flow:
1. Click "Deduplicate" in sidebar
2. Call `GetDuplicateGroups()` to get summary
3. Show confirmation modal listing groups: filename, copy count, which is kept
4. On confirm, call `DeduplicateAll()`
5. Reload gallery, show toast: "Deduplicated: removed N clips from M groups"

## Error Handling

- **Empty content**: SHA-256 of empty bytes is valid — two empty clips are duplicates
- **Backfill failure**: Log and continue per clip, don't block startup
- **Race conditions**: Merge operations wrapped in transactions
- **Archived duplicates**: Duplicate count spans active + archived. Merge pulls archived duplicates into the surviving active clip
- **Expired clips**: Excluded from duplicate count (filtered in queries). Merge deletes them along with other duplicates
