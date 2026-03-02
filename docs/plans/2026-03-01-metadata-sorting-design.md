# System Metadata Display & Clip Sorting

## Overview

Add read-only system metadata to the metadata modal and a sort control to the gallery header, with persistent sort preferences.

## System Metadata in Metadata Modal

### Fixed Rows

At the top of the existing metadata modal, display a non-editable "System Info" section with:

| Field | Source | Format |
|-------|--------|--------|
| Date added | `created_at` | Localized date+time (e.g., "Mar 1, 2026, 2:34 PM") |
| Filename | `filename` | As stored |
| Content type | `content_type` | MIME type (e.g., "image/png") |
| File size | `size` (`LENGTH(data)`) | Human-readable (e.g., "245 KB") |

### Visual Treatment

- Styled with `bg-stone-50` background to distinguish from editable metadata below
- Non-interactive — no edit/delete buttons on these rows
- Separated from user metadata by a subtle divider
- Data sourced from `ClipPreview` fields already loaded — no extra DB query

### Implementation

- `openMetadataModal()` receives clip preview data (id, filename, content_type, created_at, size)
- Fixed rows rendered first, then existing editable metadata rows below

## Sorting

### Sort Control UI

- Icon button (up/down arrows) in the gallery header bar
- Clicking opens a popover with:
  - **Sort by:** Date added, Filename, File size, Content type (radio-style)
  - **Direction:** Ascending / Descending toggle
- Selecting an option immediately re-fetches clips and closes the popover
- Default: Date added, Descending (preserves current behavior)

### Backend Changes

`GetClips()` gets two new parameters: `sortField string`, `sortDir string`.

Sort field mapping (whitelist-validated):
- `"date"` → `c.created_at`
- `"name"` → `c.filename`
- `"size"` → `LENGTH(c.data)`
- `"type"` → `c.content_type`

Direction: `"asc"` or `"desc"`, defaulting to `"desc"`.

Secondary sort for stability: always append `, c.created_at DESC, c.id DESC` when primary sort is not `created_at`.

### Persistence

- Sort preference saved via existing `GetSetting`/`SetSetting` mechanism
- Keys: `sort_field` (default `"date"`), `sort_dir` (default `"desc"`)
- Loaded on app startup, applied to initial `GetClips()` call
- Invalid stored values fall back to defaults

## Edge Cases

- Clips with identical sort values: stable via secondary sort on `created_at DESC, id DESC`
- NULL/empty filenames: sort last (SQLite default for NULLs with ASC, handled explicitly for DESC)
- Sort preference defaults gracefully if stored value is invalid
- Sorting works correctly with tag filters and in archive view

## Testing

E2E tests to add:
- System metadata rows appear in metadata modal with correct values
- Sort button opens popover with all options
- Sorting by each field produces correct order
- Sort direction toggle works
- Sort preference persists across app restart (reload)
- Sorting works with tag filters active
- Sorting works in archive view
