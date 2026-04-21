# Folder Context Menu

Add a right-click context menu on folder cards in folder view with Open / Move / Rename / Serve / Share / Hide actions, plus live status badges (served, shared) and visual transparency for hidden folders. Folders are hierarchical tags; all operations reuse existing backend APIs.

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Trigger | Right-click, long-press, or `Shift+F10` / context-menu key on a focused card | Native UX + keyboard a11y |
| Move UX | Folder picker modal with tree | Discoverable, no typos, preserves drag-to-move as alternative |
| Serve flow | Inactive: jump to Serve view, add folder to configured entries (user clicks Start). Active: direct `StopServing` from the menu | Consistent with existing view-based config; menu label toggles |
| Share flow | Inactive: jump to Share view, open "Share a tag" modal pre-selected (user clicks Create). Active: direct `StopShare` from the menu | Matches existing modal-based start flow |
| Status refresh | Poll `GetServeStatus` + `GetShareStatus` every 2s while folder view is active | Mirrors existing Serve-view pattern, no new backend events |
| Status icon placement | Small glyph icons in the card's top-right corner | Option D from brainstorm — clean separation from folder graphic, tooltip-friendly |
| Hidden-folder visibility in folder view | Visible at `opacity: 0.5` + light grayscale | Context menu needs to reach them to unhide |
| Delete from menu | Not included (stays in existing folder-view header) | Out of user's requested scope |
| Architecture | New parallel module `folder-context-menu.js` reusing the shared `ContextMenu` primitive | Clip and folder menus have different state shapes; avoid conditional spaghetti in `buildMenuItemList` |
| Backend changes | None | `UpdateTag`, `Start/Stop Serving`, `Start/Stop Share`, `Get/SetHiddenTags` all exist |

## Menu Structure

Built fresh on each open so state reflects the live `folderStatusMap`.

| # | Label | Action | Notes |
|---|-------|--------|-------|
| 1 | Open | Navigate into folder | Same as click |
| 2 | — | divider | |
| 3 | Move… | Open folder picker modal | Reparents via `UpdateTag(id, newPath, color)` |
| 4 | Rename… | Open existing rename dialog | Prefilled with short name |
| 5 | — | divider | |
| 6 | Serve… *or* Stop serving | Inactive: jump to Serve view, pre-add folder. Active: call `StopServing(tagID)` directly | Destructive style when stopping |
| 7 | Share… *or* Stop sharing | Inactive: jump to Share view, open "Share a tag" modal pre-selected. Active: call `StopShare(tagID)` directly | Destructive style when stopping |
| 8 | — | divider | |
| 9 | Hide *or* Unhide | Add / remove tag ID from `SetHiddenTags` array | Neutral style |

The untagged-clips pseudo-folder in the root view has no context menu (not a real tag).

## Architecture

### New files

**`frontend/js/folder-context-menu.js`** — exports `FolderContextMenu`:

- `attach(cardEl, tag, state)` — wires `contextmenu` + keyboard handlers on the card (`Shift+F10`, `ContextMenuKey`, long-press)
- `openFor(tag, state, anchor)` — builds items from live `state`, calls `ContextMenu.open(items, tag.id, anchor, onAction)`
- `handleAction(action, tagID, item)` — dispatches:
  - `open` → `navigateToFolder(tagID)`
  - `move` → `FolderMoveModal.show(tag)`
  - `rename` → existing rename dialog entry point
  - `serve` | `stop-serve` → serve view navigation or `ServeService.StopServing`
  - `share` | `stop-share` → share view navigation or `ShareService.StopShare`
  - `hide` | `unhide` → mutate hidden-tag cache + persist via `App.SetHiddenTags`

**`frontend/js/folder-move-modal.js`** — exports `FolderMoveModal`:

- `show(tag) → Promise<destTagID | "root" | null>`
- Tree built from `App.GetTags()`, keyed by path segments, sorted alphabetically per level
- Disables the moving folder, its descendants, and its current parent
- Live preview line showing the new full path
- On confirm: `App.UpdateTag(tag.id, newPath, tag.color)` → close + toast on success; keep open + inline error on failure
- Uses the app's standard modal markup + focus trap (same pattern as other modals in `modals.js`)

### Modified files

**`frontend/js/ui.js` — `renderFolderCards()`:**

- Reads from `folderStatusMap` (module-scope `{ [tagID]: { served, shared, servePaused, sharePaused, serveURL, serveRequests, shareFollowers, hidden } }`)
- Changes card class `overflow-hidden` → `overflow-visible` so absolute-positioned badges aren't clipped
- Adds:
  - `card.tabIndex = 0` (folder cards currently aren't focusable)
  - `data-hidden="true"` and visual classes when tag is in hidden list
  - Composite `aria-label` including name, clip count, active states, hidden flag
  - `.folder-status-badges` container with badge `<span>`s for each active state
  - `FolderContextMenu.attach(card, tag, state)` call

**`frontend/js/app.js`:**

- Adds `folderStatusPoller` (start/stop controlled by the view-switch handler)
- When folder view becomes active:
  1. Runs initial poll immediately
  2. Schedules `setInterval(2000)` calls to `ServeService.GetServeStatus()` + `ShareService.GetShareStatus()`
  3. On each response: updates `folderStatusMap`, calls `updateFolderBadgesInPlace()` (DOM mutation on existing `[data-folder]` cards — not a full re-render)
- When switching to non-folder view: clears interval

**`frontend/js/wails-api.js`:**

- Thin wrappers for `ServeService.GetServeStatus` / `ShareService.GetShareStatus` / `ShareService.GetShareStatus` if not already exported

**Serve-view integration (`serve.js`):**

Expose `openServeViewForTag(tagID)`:

1. Switch to Serve view
2. Add the folder to configured entries if not already there (reuses the existing add-entry logic invoked today by the "Add" button + tag picker)
3. Scroll / flash-highlight the row
4. **Does not auto-start** — user clicks Start to confirm

**Share-view integration (`share.js`):**

Share's "start" flow is a modal (not a list row like Serve). Expose `openShareFlowForTag(tagID)`:

1. Switch to Share view
2. Open the existing "Share a tag" modal with the tag pre-selected in its dropdown
3. **Does not auto-create** — user clicks Create to confirm

If either `serve.js` / `share.js` already has an equivalent internal helper, thin-wrap it; otherwise extract from the current handler logic.

### State flow diagram

```
FolderView-active ─► startPoller ──► GetServeStatus + GetShareStatus (every 2s)
                                            │
                                            ▼
                                   updateFolderStatusMap
                                            │
                                            ▼
                                   updateBadgesInPlace(cards)
                                            │
                                            ▼
                                   badge DOM mutated, focus preserved

right-click / Shift+F10 ─► FolderContextMenu.openFor(tag, liveState)
                                            │
                                            ▼
                                   builds items from state (labels flip)
                                            │
                                            ▼
                                   ContextMenu.open(items, …, onAction)
                                            │
                                            ▼
                                   handleAction → existing backend API
                                            │
                                            ▼
                                   toast + poll picks up new state next tick
```

### Shared primitives reused (no changes)

- `ContextMenu` (`frontend/js/context-menu.js`) — generic items/dividers/submenus/keyboard/ARIA
- `tooltips.js` — badge tooltips
- `toast()` — success/error confirmation
- Existing rename-folder dialog — triggered from Rename item

## Visual Design

### Badges

- Position: `absolute; top: 8px; right: 8px`, flex row, `gap: 4px`
- Each badge: 14×14 container with 12×12 stroked SVG glyph inside
- Served: globe glyph, `stroke: #059669` (emerald-600)
- Shared: chain/link glyph, `stroke: #1d4ed8` (blue-700)
- Paused variants (for Share pause/resume): dashed container outline + glyph color `stone-500`, tooltip `"paused"`
- Appearance/disappearance: `opacity 150ms` transition, respects `prefers-reduced-motion`
- Badges render once `folderStatusMap` is populated (first poll); no blocking skeleton — acceptable 1–2s delay on entering folder view

### Tooltip content

| State | Tooltip |
|-------|---------|
| Served, running, URL known | `"Serving on http://localhost:44557 · 12 requests"` |
| Served, running, URL pending | `"Serving this folder"` |
| Served, paused | `"Serving paused — click to resume in Serve view"` |
| Shared, running, follower count known | `"Sharing this folder — 2 followers"` |
| Shared, running, count pending | `"Sharing this folder"` |
| Shared, paused | `"Sharing paused"` |

### Hidden folder

- Visual: `opacity: 0.5` + `filter: grayscale(0.4)` (grayscale prevents color badges staying vivid on a dimmed card)
- Remains visible in folder view (unlike in tag-filter dropdown where hidden means not listed)
- Rationale: context menu needs to reach them to unhide

### Folder card DOM (revised)

```html
<li class="bg-white rounded-md border border-stone-200 overflow-visible flex flex-col items-center justify-center p-6 cursor-pointer transition-all duration-150 hover:border-stone-300 hover:scale-[1.02] relative"
    data-testid="folder-card-client1"
    data-folder="42"
    data-hidden="true"
    draggable="true"
    aria-grabbed="false"
    tabindex="0"
    aria-label="Folder: work/client1, 8 clips, served, hidden">
  <div class="folder-status-badges absolute top-2 right-2 flex gap-1">
    <span role="img"
          aria-label="Served on localhost:44557"
          data-tooltip="Serving on http://localhost:44557 · 12 requests">
      <svg aria-hidden="true" focusable="false" …>…globe…</svg>
    </span>
    <!-- share badge optional -->
  </div>
  <svg class="w-10 h-10 mb-2" …>…folder icon…</svg>
  <span class="text-xs font-medium text-stone-700">client1</span>
  <span class="text-[10px] text-stone-400 mt-0.5">8 clips</span>
</li>
```

`overflow-hidden` on the card is dropped (no content currently needs it and it would clip badges).

## Accessibility

- Badge `<span>` has `role="img"` + `aria-label` describing the state (with URL / follower count when known). Inner SVG is `aria-hidden="true"` + `focusable="false"`
- Card `aria-label` is a composite: `"Folder: {path}, {count} clips[, served][, shared][, hidden]"`. Announced on focus
- Card is keyboard-focusable (`tabindex="0"`); Enter/Space navigates into the folder (preserving existing click behavior)
- Context menu invoked by `Shift+F10`, the ContextMenu key, right-click, or long-press. Existing `ContextMenu` primitive handles roving tabindex, Esc, arrow keys
- Hidden folders are not `aria-hidden` — screen reader still announces them (the hidden state is in the label)
- Tooltips on badges use existing `tooltips.js` (already accessible)
- All new transitions respect `prefers-reduced-motion`

## Move Picker Modal

### Layout

```
┌─────────────────────────────────────────────┐
│  Move folder                            [×] │
├─────────────────────────────────────────────┤
│  Moving:  work/client1                      │
│                                             │
│  Destination:                               │
│  ┌─────────────────────────────────────┐   │
│  │ ○ Root (top level)                  │   │
│  │ ▾ archive                           │   │
│  │    • 2024                           │   │
│  │    • 2025                           │   │
│  │ ▾ personal                          │   │
│  │    • photos                         │   │
│  │ ▾ work             ← current parent │   │
│  │    ▸ client1       ← self, disabled │   │
│  │    • client2                        │   │
│  │    • client3                        │   │
│  └─────────────────────────────────────┘   │
│                                             │
│  New path: work/client2/client1             │
│                                             │
│           [ Cancel ]  [ Move ]              │
└─────────────────────────────────────────────┘
```

### Tree behavior

- Source: `App.GetTags()` built into a tree keyed by path segments, sorted alphabetically per level
- `Root` pseudo-entry always at top as a radio option
- Expand/collapse via `▾` / `▸` chevrons
- Keyboard: up/down arrows navigate rows, left collapses, right expands, Enter confirms, Esc cancels

### Disabled destinations

| Target | Reason text |
|--------|-------------|
| The folder being moved | "Cannot move folder into itself" |
| Any descendant of the folder | "Cannot move into own subfolder" |
| Current parent (including Root if already top-level) | "Already here" |

### Preview line

Updates live as user selects a destination. Computes `newPath = destinationPath + "/" + shortName` (or `shortName` alone for Root). User sees the exact string that will be sent to `UpdateTag` before clicking Move.

### Confirm flow

1. Compute `newPath`
2. Call `App.UpdateTag(tag.id, newPath, tag.color)` (color preserved)
3. On success: close modal, toast `"Moved to {newPath}"`, trigger folder-view re-render
4. On error: keep modal open, inline error under preview line, Move button still enabled for retry

### Known error cases surfaced inline

- Name collision at destination (backend rejects if `{dest}/{shortName}` already exists)
- Any tag in subtree currently served (backend rejects per `app.go:1390-1400`)
- Invalid path / reserved segment (backend validates)

Wording: `"Cannot move: tag \"work/client1\" is currently being served. Stop serving first."` and equivalents.

## Error Handling Summary

| Action | Failure mode | UX response |
|--------|--------------|-------------|
| Move | Collision, serve-active subtree, invalid path | Modal stays open, inline error, retry enabled |
| Rename | Same | Existing dialog surfaces error |
| Serve (start) | Port bind fail, already served | Toast with error; Serve view reflects state |
| Stop serving | Race (already stopped) | Silent no-op; log in dev |
| Share (start) | Already active, network | Toast with error |
| Stop sharing | Race | Silent no-op |
| Hide/Unhide | `SetHiddenTags` persist fails | Toast error; revert in-memory cache to match backend |

### Race protection

- Menu is built fresh on each open — label reflects state at open time
- If state changes between open and click, operation is issued anyway; toast reports the real outcome
- Status poller uses a generation counter (same pattern as `_folderRenderGen`) so stale responses are dropped

## Testing

### New test directory: `e2e/tests/folder-context-menu/`

| File | Coverage |
|------|----------|
| `menu-structure.spec.ts` | All items present in correct order, dividers, label flipping by state, keyboard open via `Shift+F10`, Esc close |
| `move-modal.spec.ts` | Tree structure correct, destination selection, preview updates, confirm calls `UpdateTag`, disabled-self / disabled-descendant / disabled-current-parent, collision error inline, serve-active error inline |
| `serve-toggle.spec.ts` | Serve from menu → jumps to Serve view + folder pre-added; back in folder view, menu now shows "Stop serving"; click → direct stop + toast |
| `share-toggle.spec.ts` | Same pattern for Share |
| `hide-toggle.spec.ts` | Hide sets `data-hidden="true"` + opacity + updated `aria-label`; Unhide reverses; state persists across app reload |
| `status-badges.spec.ts` | Start serving → globe badge appears within poll interval; start sharing → link badge; stop both → badges disappear; combined state; tooltip content; paused variant |
| `a11y.spec.ts` | Cards focusable; `aria-label` composition; `Shift+F10` opens menu; ESC closes; badge `role="img"` + labels; screen-reader announcement order |

### AppHelper fixture additions (`e2e/fixtures/test-fixtures.ts`)

- `app.rightClickFolder(name)` — opens the folder context menu
- `app.getFolderContextMenuItem(action)` — locator for menu item assertions
- `app.expectFolderBadge(name, type)` — asserts badge presence, `type ∈ {'served', 'shared', 'served-paused', 'shared-paused'}`

### Regression sweep (existing tests must still pass)

- `tags/folder-mode.spec.ts`, `tags/delete-folder-view.spec.ts`, `tags/rename-folder-view.spec.ts`
- `folder-drag/folder-drag.spec.ts` (drag-to-move still works; no right-click conflict)
- `serve/serve-basic.spec.ts`, `serve/serve-ui.spec.ts` (Serve view unchanged apart from being navigation target)
- `tags/tag-hidden.spec.ts` (hidden semantics unchanged in filter dropdown)

## Out of Scope (v1)

- Context menu on breadcrumb pills (cards in the grid only)
- Multi-select / bulk-folder context menu
- Context menu on root or untagged pseudo-folder
- Pause/Resume actions inside the context menu (Share supports them; remain in Share view)
- User-customizable keyboard shortcut for opening the menu

## File Summary

**New**
- `frontend/js/folder-context-menu.js`
- `frontend/js/folder-move-modal.js`
- `e2e/tests/folder-context-menu/menu-structure.spec.ts`
- `e2e/tests/folder-context-menu/move-modal.spec.ts`
- `e2e/tests/folder-context-menu/serve-toggle.spec.ts`
- `e2e/tests/folder-context-menu/share-toggle.spec.ts`
- `e2e/tests/folder-context-menu/hide-toggle.spec.ts`
- `e2e/tests/folder-context-menu/status-badges.spec.ts`
- `e2e/tests/folder-context-menu/a11y.spec.ts`

**Modified**
- `frontend/js/ui.js` — `renderFolderCards()` adds badges, tabindex, aria-label, context-menu attach; changes `overflow-hidden` → `overflow-visible`
- `frontend/js/app.js` — folder-view status poller wiring
- `frontend/js/wails-api.js` — wrappers for `GetServeStatus` / `GetShareStatus` if missing
- `frontend/js/serve.js` — exposes `selectAndAddEntryForTag(tagID)` for external navigation entry
- `frontend/js/share.js` — same
- `frontend/css/main.css` — `.folder-status-badges`, hidden-folder styling, paused badge styling
- `e2e/fixtures/test-fixtures.ts` — new helpers
- `e2e/helpers/selectors.ts` — selectors for badges + menu items

**Unchanged**
- All Go backend files (no new APIs needed)
- `context-menu.js` (reused as-is)
- `tooltips.js`, `toast()`, rename dialog
