# Marquee (Rubber-Band) Selection

Add OS-style rubber-band selection to the clip gallery. Users click and drag on empty space between cards to draw a selection rectangle that selects all intersecting clips.

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Trigger zone | Empty gallery space only | Matches OS conventions; avoids conflicts with lightbox, drag-out, folder-drag |
| Intersection mode | Touch (any overlap selects) | macOS Finder behavior; responsive feel |
| Selection behavior | Replace by default, Shift to add | Standard OS convention |
| Folder mode | Works in both modes | Trigger zones don't overlap with folder-drag (empty space vs card elements) |
| Visual style | Semi-transparent stone | Matches existing design system |
| Implementation | Standalone `marquee-select.js` | Follows existing one-feature-per-file pattern |

## Architecture

### New file: `frontend/js/marquee-select.js`

Exports `initMarqueeSelect({ gallery, selectedIds, updateBulkToolbar })` called from `app.js` during startup.

**No changes to existing event handlers in `ui.js`, `app.js`, `folder-drag.js`, or `transfer.js`.**

### Module responsibilities

1. Listen for `mousedown` on the `#gallery` element where `e.target === gallery` and `e.button === 0`
2. After a 5px drag threshold, create and position the marquee overlay `<div>`
3. On `mousemove` (on `document`), resize the overlay and compute card intersections via `getBoundingClientRect()`
4. On `mouseup` (on `document`), finalize selection and clean up

### Dependencies (injected, not imported)

- `gallery` — the `#gallery` `<ul>` element
- `selectedIds` — the `Set` from `app.js` tracking selected clip IDs
- `updateBulkToolbar()` — function from `ui.js` to sync the bulk action bar
- Reads card checkboxes via `gallery.querySelectorAll('.clip-checkbox')`

## Interaction Model

### Starting the marquee

- `mousedown` fires on `#gallery` where `e.target === gallery` (empty space between/after cards)
- Left click only (`e.button === 0`)
- Guard: skip if `window.__internalDragActive` is true (folder-drag in progress)
- Record start coordinates and current `selectedIds` snapshot
- Attach `mousemove` and `mouseup` listeners on `document`

### Drag threshold

- Track cumulative mouse movement from the start point
- The marquee overlay only becomes visible after 5px of movement
- Below threshold: no visual feedback, treated as a click on empty space on mouseup

### During drag

- Use `requestAnimationFrame` to throttle overlay updates and intersection checks
- Compute the rectangle from start point to current mouse position (handle all four drag directions)
- Query all visible cards: `gallery.querySelectorAll('li[data-id]')`, skip any with `display: none`
- For each card, check `getBoundingClientRect()` overlap with the marquee rectangle
- **Without Shift**: clear all selections, then select only intersecting cards
- **With Shift**: preserve the pre-drag snapshot, add intersecting cards on top
- Update each card's checkbox `.checked` state and `.has-checked` class in real time

### Auto-scroll

- Define a 40px scroll zone at the top and bottom edges of the scrollable container (`#main-content`)
- When the mouse enters a scroll zone during drag, scroll smoothly in that direction
- Recalculate card intersections after scroll position changes

### Ending the marquee

- `mouseup` on `document` finalizes the selection
- Call `updateBulkToolbar()` to sync toolbar visibility and count
- Remove the overlay `<div>` and detach `mousemove`/`mouseup` listeners
- If drag never exceeded 5px threshold and Shift not held: clear all selections (click on empty space = deselect all)

## Visual Design

### Marquee overlay

- Absolutely positioned `<div>` appended to `#gallery`
- `#gallery` gets `position: relative` added (via class or inline style, only during drag)
- Styles:
  - `border: 2px solid rgba(28, 25, 23, 0.6)` (stone-800 at 60%)
  - `background: rgba(28, 25, 23, 0.07)` (stone-800 at 7%)
  - `border-radius: 2px`
  - `pointer-events: none`
  - `z-index: 20` (above cards, below checkbox overlay at z-30)
  - `position: absolute`

### Card selection highlight

Reuses existing styles — no new CSS:
- `has-checked` class on the `<li>` card (shows `ring-2 ring-stone-800`)
- Checkbox `.checked = true` (becomes visible via `group-[.has-checked]:opacity-100`)

## Edge Cases

| Scenario | Behavior |
|----------|----------|
| Click empty space without dragging | Deselect all (if Shift not held) |
| Drag doesn't cover any cards | Deselect all (if Shift not held) |
| Hidden cards (search filter active) | Skipped — only visible cards checked |
| Gallery re-renders during drag | Works — `getBoundingClientRect()` queried live each frame |
| Lightbox/modal open | Gallery not interactive; no conflict |
| Right-click / middle-click on empty space | Ignored — `e.button === 0` guard |
| Folder-drag in progress | Ignored — `window.__internalDragActive` guard |
| Touch devices | Not supported — desktop app, mouse events only |

## Testing

New test file: `e2e/tests/bulk/marquee-select.spec.ts`

| Test | What it verifies |
|------|-----------------|
| Basic marquee select | Drag across empty space selects intersecting cards |
| Replace behavior | Existing checkbox selection is cleared by a plain marquee drag |
| Shift+drag adds | Shift-held marquee adds to existing selection |
| Click empty space deselects | Single click on empty space clears all selections |
| Folder mode works | Marquee functions identically in folder mode |
| Filtered cards skipped | Hidden cards from search are not selected by marquee |
| Bulk toolbar syncs | Toolbar appears with correct count after marquee select |

Add `marqueeOverlay` selector to `e2e/helpers/selectors.ts`.

## Documentation Updates

Add a "Marquee Selection" subsection to `docs/docs/features/bulk-actions.md` under "Selecting Multiple Clips", alongside the existing "Checkbox Selection" section. Cover:

- How to use it (click and drag on empty space)
- Replace vs Shift-to-add behavior
- Works in both normal and folder mode

Also add a tip entry in the "Tips" section at the bottom of that page.

## Files Changed

| File | Change |
|------|--------|
| `frontend/js/marquee-select.js` | **New** — all marquee logic |
| `frontend/js/app.js` | Add `import` and `initMarqueeSelect()` call at startup |
| `frontend/index.html` | Add `<script>` tag for `marquee-select.js` |
| `e2e/tests/bulk/marquee-select.spec.ts` | **New** — e2e tests |
| `e2e/helpers/selectors.ts` | Add `marqueeOverlay` selector |
| `docs/docs/features/bulk-actions.md` | Add marquee selection docs |
