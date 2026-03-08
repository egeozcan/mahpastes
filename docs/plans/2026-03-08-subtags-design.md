# Subtags (Hierarchical Tags) Design

## Overview

Add hierarchical tag support using `/`-separated names (e.g., `work/client1/projectABC`). Hierarchy is derived from the name — no schema changes. Features include cascading filters, a folder browsing mode, tree-structured dropdowns, subtag folders in tag serve, and subtag access for scoped API keys.

## Data Model & Backend

No schema changes. Tags remain flat in the DB. Hierarchy is derived by parsing `/` in tag names.

### Key backend changes

- **`CreateTag(name)`**: When creating `work/client1/projectABC`, auto-create `work` and `work/client1` if they don't exist. Intermediate tags inherit the color of the first ancestor that exists, or get a new color from rotation if none exist.
- **`GetDescendantTagIDs(tagID)`** (new internal helper): Given a tag, find all tags whose name starts with `tagName + "/"`. Used by `GetClips` to expand filters.
- **`GetClips` filtering**: When `activeTagFilters` includes a tag, expand it to include all descendant tag IDs before building the SQL query. AND logic stays — filtering by `work` AND `photos` means a clip must match a descendant of `work` AND a descendant of `photos`.
- **`DeleteTag`**: Deleting a parent does NOT cascade-delete children. Deleting `work` leaves `work/client1` intact.
- **`SetHiddenTags`**: Only top-level tags can be in the hidden list. Backend resolves descendants — hiding `work` automatically hides all `work/*` tags.
- **`UpdateTag` (rename)**: Renaming `work` to `job` also renames `work/client1` → `job/client1`, `work/client1/projectABC` → `job/client1/projectABC`, etc.
- **Orphan auto-delete**: Only auto-delete a tag if it has no clips AND no children (intermediate structural tags must survive even with 0 clips).

## Tag Filter Dropdown (Tree View)

Current flat alphabetical checkbox list becomes an indented tree with expand/collapse.

- Top-level tags at base indentation. Children indented per depth level.
- Parent tags get a disclosure triangle (▶/▼) to expand/collapse. Leaf tags have none.
- Checking a parent filters by that tag and all descendants (backend handles expansion). The checkbox itself doesn't auto-check children.
- Tags at all levels are independently checkable.
- Hidden tags show eye-slash icon at top level only. Children are hidden implicitly and don't render when parent is hidden.
- Tree expanded by default.

## Folder Mode

### Toggle button

Next to the sort button in the header. Folder icon, same styling as sort button (`border border-stone-200`). Toggles `aria-pressed` with active state (`bg-stone-100 border-stone-300`). State is NOT persisted across sessions.

### Behavior

- **Root level (no tag filter)**: Folder cards for each top-level tag + all clips not tagged with any hierarchical tag. Clips with only flat tags (no `/`) also appear.
- **Navigated into a tag (e.g., `work`)**: Folder cards for immediate children (`work/client1`, `work/client2`) + clips tagged directly with `work` (not with any `work/*` subtag).
- **Folder cards**: Same card dimensions but with folder icon, short name (e.g., `client1` not `work/client1`), clip count (including descendants), tag's inherited color as accent. Clicking navigates deeper by adding/replacing the tag filter.

### Navigation

Active tags section at top serves as breadcrumb in folder mode. Navigated to `work/client1` shows `work` and `work/client1` as separate removable pills. Removing `work/client1` goes back to `work`. Removing `work` goes to root.

Exiting folder mode switches back to normal view with current tag filters preserved.

## Active Tag Filters & Navigation

- Subtag pills show full path (`work/client1`) for clarity.
- In folder mode, pills act as breadcrumbs. Removing deepest subtag navigates up one level. Removing a parent removes it and all descendant filters.
- Outside folder mode, removing a tag just removes that filter.
- Clear all and badge count unchanged (counts active filter tags, not expanded descendants).

## Tag Management

### Settings modal (hidden tags)

- Only top-level tags appear as toggleable items. Subtags not listed separately.
- Hiding a top-level tag hides all descendants implicitly.
- Orphaned subtags (parent deleted) appear at top level in settings.

### Tag popover (assigning tags to clips)

- Tree structure matching filter dropdown — indented with disclosure triangles.
- Any tag at any level assignable. Assigning `work` does NOT auto-assign `work/client1`.
- Creating a tag with `/` in the input auto-creates intermediates.

### Card tag pills

- Show short name (leaf segment) to save space. Tooltip shows full path on hover.
- Clicking a pill filters to that specific tag (descendants included via backend expansion).

### Tag deletion

- Deleting a parent does NOT delete children.
- Auto-delete only if tag has no clips AND no children.

## Serve Integration

When serving a tag, subtags become directories in the URL structure.

- **`GET /`** on served `work` tag: Clips tagged directly with `work` as files + immediate subtag folders (`client1/`, `client2/`) as directory links.
- **`GET /client1/`**: Clips tagged `work/client1` as files + deeper subtag folders.
- **File access**: `GET /client1/report.pdf` resolves to clip `report.pdf` tagged with `work/client1`.
- **Directory listing**: Both HTML and JSON include `type` field (`"file"` or `"directory"`). HTML shows folder icon for directories.
- **`index.html`**: Scoped per level. If tagged with `work`, serves at `/`. If tagged with `work/client1`, serves at `/client1/`.
- **Duplicate filenames**: Same resolution as today but scoped per directory level.

## Tag-Scoped API Key Subtag Access

Tag-scoped keys can access the full subtree of their scoped tag.

- **`GET /api/v1/clips`**: Key scoped to `work` lists clips tagged with `work`, `work/client1`, `work/client1/projectABC`, etc.
- **`GET /api/v1/clips/{id}/data`**: Can download any clip with a tag in the scoped subtree.
- **`POST /api/v1/clips`**: Can create clips and tag them with any subtag under scope.
- **Tag creation**: Can create subtags under scope (auto-creates intermediates). Cannot create tags outside scope.
- **Serve management**: Can start/stop serve for scoped tag (unchanged). Served content includes subtag folder structure automatically.
- **Scope validation**: Check changes from `tagID == scopedTagID` to "tag is scopedTag or a descendant" (prefix match on name).

## Documentation Updates

- `docs/docs/features/tags.md` — Subtag hierarchy, folder mode, tree filtering
- `docs/docs/features/tag-serve.md` — Subtag folder structure in served URLs
- `docs/docs/features/rest-api.md` — Tag-scoped key subtag access, subtag creation via API
- `docs/docs/developers/database-schema.md` — Hierarchy-from-names convention
- `docs/docs/developers/api-reference.md` — Updated tag methods (auto-create, rename cascade)
- `docs/docs/plugins/api-reference.md` — Plugin tag API changes if affected
