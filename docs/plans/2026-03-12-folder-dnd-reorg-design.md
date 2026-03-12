# Folder View Drag-and-Drop Reorganization Design

**Date:** 2026-03-12

## Summary

Drag-and-drop clips and folders within folder view to reorganize them — move clips between folders and reparent folders under other folders.

## Drag Initiation

In folder mode, all `<li>` cards (clips and folders) get `draggable="true"` on the element itself. The existing drag-out handle (`data-action="drag-out"`) is hidden in folder mode since the whole card is the drag surface.

On `dragstart`:
- Determine what's being dragged: clip (`data-id`) or folder (`data-folder`)
- If it's a clip and it's part of the current selection (`selectedIds`), gather all selected clip IDs — otherwise just the single clip
- Store drag payload in `dataTransfer.setData()` as JSON: `{ type: "clips"|"folder", ids: [...], sourceFolderId }`
- Render a custom drag image via offscreen DOM element using `setDragImage()`
- Dragged cards get `opacity-40` while in flight

### Custom Drag Image

- White rounded card (`bg-white rounded-md border border-stone-800`), `px-3 py-2`
- Single clip: small thumbnail + filename in `text-xs font-medium text-stone-800`
- Multi-clip: stacked look (offset `bg-stone-100` rectangles behind), count badge (`bg-stone-800 text-white text-[10px] font-medium rounded-full w-5 h-5`) at top-right
- Folder: folder icon + short name

## Drop Targets

### Folder cards in the gallery

Any `<li data-folder>` in the current view. Highlight on dragenter: `border-stone-800 bg-stone-50 scale-[1.03] transition-all duration-150`. Removed on dragleave/drop.

### Breadcrumb path segments

Each ancestor tag pill in the breadcrumb bar becomes a drop target. Same highlight style on hover. Allows dragging clips/folders "up" to parent folders.

### Root breadcrumb (home icon)

New home icon (`w-4 h-4`, `stroke="currentColor"`, `stroke-width="1.5"`, `opacity-60`) as first element in breadcrumb bar when in folder mode. `aria-label="Root folder"`. Dropping here removes the tag (for clips) or reparents to top-level (for folders). Hover: `hover:opacity-100 hover:bg-stone-100 rounded p-1 transition-colors`. Drop highlight: `bg-stone-100 ring-1 ring-stone-800 rounded`.

### Invalid targets

Show `cursor: no-drop`, no visual change:
- Folder card dropped on itself
- Clip dropped on the folder it's already in (current folder)
- Defensive: folder dropped on its own descendant (shouldn't be visible in same view, but guarded)

`dragover` calls `e.preventDefault()` on valid targets and sets `dropEffect = 'move'`.

## Drop Handling

### Clips onto a folder

- Target is root: remove current folder's tag from each clip via `BulkRemoveTag`
- Target is a folder: call `BulkAddTag(clipIds, targetTagId)` — tree exclusivity handles removing the old same-tree tag automatically
- After success: reload clips, clear selection

### Folder onto another folder (reparenting)

- Compute new tag name:
  - Target is root → short name only (e.g., `work/design` → `design`)
  - Target is a folder → `targetTagName + "/" + shortName`
- Call `RenameTagTree(tagId, newName)`:
  1. Look up current tag name
  2. Validate new name (reject `_api` segments, empty)
  3. Check no existing tag has the exact new name — error if conflict
  4. Rename the tag: `UPDATE tags SET name = ? WHERE id = ?`
  5. Find all descendant tags: `SELECT id, name FROM tags WHERE name LIKE oldName || '/%'`
  6. Replace old prefix with new prefix for each descendant
  7. All in single transaction
  8. Emit `tag:updated` plugin events
- After success: reload clips and tags
- On error (e.g., name conflict): toast with error message

## Backend Changes

### New: `RenameTagTree(tagID int64, newName string) error`

In `app.go`. Renames a tag and all its descendants in a single transaction. Validates against `_api` reserved segments and duplicate names.

### New: `BulkRemoveTag(clipIDs []int64, tagID int64) error`

In `app.go`. Removes a tag from multiple clips in a transaction. Emits `tag:removed_from_clip` events. Needed for dropping clips onto root.

## Frontend Architecture

### New file: `frontend/js/folder-drag.js`

Isolated module for all folder drag-and-drop logic. Key functions:
- `initFolderDrag()` — event delegation on gallery and breadcrumb for drag events
- `buildDragImage(type, count, label)` — offscreen DOM element for `setDragImage()`
- `resolveDragPayload(e)` — determines clip IDs (factoring in selection) or folder ID
- `resolveDropTarget(e)` — walks up from `e.target` to find drop target, returns tag ID or `null` for root
- `isValidDrop(payload, target)` — validation checks
- `executeDrop(payload, target)` — calls Wails API, reloads gallery

### Changes to existing files

- `ui.js` — `renderFolderCards()`: add `draggable="true"` to folder cards. Clip cards: add `draggable="true"` in folder mode, hide drag-out handle. Add root home icon to breadcrumb in folder mode.
- `app.js` — call `initFolderDrag()` after `loadClips()` in folder mode
- `wails-api.js` — add `renameTagTree()` and `bulkRemoveTag()` wrappers
- `index.html` — include `folder-drag.js` script tag

## Accessibility

- `aria-grabbed` on draggable cards
- `aria-dropeffect="move"` on drop targets during drag
- `role="listitem"` preserved on cards
- Visually hidden live region announcing "Moved {n} clip(s) to {folder}" or "Moved folder {name} to {target}" on success
- Root home icon: `aria-label="Root folder"`

## Testing Strategy

E2E tests in `e2e/tests/folder-drag/`:

**Clip drag:**
- Single clip onto folder card → moves to target folder
- Single clip onto breadcrumb parent → moves up one level
- Single clip onto root home icon → becomes untagged
- Selected clip (multi-select) drag → all selected move
- Unselected clip drag when selection exists → only that clip moves

**Folder drag:**
- Folder onto another folder → becomes subfolder, descendants renamed
- Folder onto breadcrumb parent → moves up one level
- Folder onto root → becomes top-level tag
- Folder with subfolders → all descendants follow
- Folder onto itself → no-op (silent rejection)

**Edge cases:**
- Last clip in folder → folder persists empty
- Name conflict on folder reparent → toast error, no change
- Rapid successive drags → sequential completion, no races

**Visual:**
- Drop target highlights on dragenter/dragleave
- Dragged card opacity reduction
- Drag image renders with count badge for multi-select
