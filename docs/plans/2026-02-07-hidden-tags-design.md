# Hidden Tags Design

## Problem

Users want to hide pastes with certain tags from the main gallery view. The setting should persist across restarts.

## Behavior

- Tags can be marked as "hidden" in settings.
- Clips with any hidden tag are excluded from the main gallery.
- If a user explicitly filters by a hidden tag in the filter dropdown, the hiding is overridden for that tag (explicit filter wins).
- Hide wins: if a clip has both a hidden and visible tag, the clip is hidden.

## Persistence

Use the existing `settings` table (key-value store):

- **Key**: `hidden_tags`
- **Value**: JSON array of tag IDs, e.g. `[3, 7, 12]`

No new tables or migrations needed.

### Go Methods

Add to `app.go`:

- `GetHiddenTags() []int64` — reads `hidden_tags` from settings, parses JSON, returns empty slice if unset.
- `SetHiddenTags(ids []int64) error` — serializes to JSON, saves via `SetSetting`.

## Backend Query Changes

`GetClips()` signature changes from `(isViewingArchive bool, tagFilters []int64)` to `(isViewingArchive bool, tagFilters []int64, hiddenTags []int64)`.

When `hiddenTags` is non-empty, use a LEFT JOIN anti-join pattern to exclude clips:

```sql
SELECT c.id, c.content_type, c.filename, c.created_at, c.expires_at, SUBSTR(c.data, 1, 500), c.is_archived
FROM clips c
LEFT JOIN clip_tags ht ON c.id = ht.clip_id AND ht.tag_id IN (?, ?)
WHERE ht.clip_id IS NULL
  AND c.is_archived = ?
  AND (c.expires_at IS NULL OR c.expires_at > CURRENT_TIMESTAMP)
ORDER BY c.created_at DESC
LIMIT 50
```

This leverages the existing primary key index on `clip_tags(clip_id, tag_id)` — no subquery, no table scan.

When no tags are hidden, the LEFT JOIN is skipped and the query is unchanged.

**Override logic**: When a hidden tag is also present in `tagFilters`, it is removed from the `hiddenTags` list before building the query. This way, explicitly filtering by a hidden tag overrides the hiding.

The "with tag filters" path applies the same LEFT JOIN exclusion (minus any overridden tags) alongside the existing INNER JOIN for tag filtering.

## Settings UI

New "Hidden Tags" section in the settings modal:

- Section header: `HIDDEN TAGS` styled as `text-sm font-semibold uppercase tracking-wide text-stone-400`.
- List of all tags, each row showing: color dot, tag name, clip count, and a toggle switch.
- Toggle style: `bg-stone-300` (off) / `bg-stone-800` (on), consistent with the stone design system.
- Empty state: "No tags yet" in muted text.
- Changes save immediately on toggle via `SetSetting("hidden_tags", JSON.stringify(ids))`.

## Frontend Integration

### State

- Module-level `hiddenTags` array cached alongside `allTags` and `activeTagFilters`.
- Fetched on startup via `GetHiddenTags()`.
- Updated when toggled in settings.

### loadClips() Changes

- Compute effective hidden tags: `hiddenTags` minus any IDs also in `activeTagFilters`.
- Pass effective hidden tags to `GetClips()` as the third parameter.

### Tag Filter Dropdown

- Hidden tags still appear in the dropdown but visually dimmed (reduced opacity or eye-slash icon).
- Clicking a hidden tag adds it to `activeTagFilters` as normal, which overrides the hiding.
- When a hidden tag is actively filtered, it renders as a normal active filter pill in the header.

## Files to Modify

| File | Changes |
|------|---------|
| `app.go` | Add `GetHiddenTags()`, `SetHiddenTags()`. Modify `GetClips()` to accept and apply `hiddenTags` param. |
| `frontend/js/wails-api.js` | Update `loadClips()` to pass hidden tags. |
| `frontend/js/app.js` | Add `hiddenTags` state, fetch on startup. |
| `frontend/js/tags.js` | Dim hidden tags in filter dropdown. |
| `frontend/js/settings.js` | Add Hidden Tags section with toggles. |
| `frontend/index.html` | Add settings section markup if needed. |
| `frontend/wailsjs/` | Regenerate bindings after Go changes. |

## Testing

E2E tests to add:

- Clip with hidden tag is excluded from gallery.
- Clip with hidden + visible tag is still excluded (hide wins).
- Explicitly filtering by hidden tag shows those clips.
- Hidden tag setting persists across app restarts.
- Toggle in settings adds/removes tag from hidden list.
- Hidden tags appear dimmed in filter dropdown.
