# Bottom Bar Design

## Goal

Move the Add button and expiry selector from the header to a permanent fixed bottom bar. Add a clip count indicator. Free up header space.

## Layout

```
┌──────────────────────────────────────────────────────┐
│  HEADER (sticky top)                                 │
│  Logo | Search | Tag Filter | Archive | Hamburger    │
├──────────────────────────────────────────────────────┤
│                                                      │
│  MAIN CONTENT (gallery grid)                         │
│  padding-bottom: ~56px to clear the bottom bar       │
│                                                      │
├──────────────────────────────────────────────────────┤
│  BULK TOOLBAR (when active, floats above bottom bar) │
├──────────────────────────────────────────────────────┤
│  BOTTOM BAR (fixed, always visible)                  │
│  + Add  |  [No Expiry ▾]  |              12 clips    │
└──────────────────────────────────────────────────────┘
```

## Decisions

- **Always visible**: Bottom bar stays even when bulk toolbar is active.
- **Contents**: Add button, expiry selector, clip count.
- **Style**: Subtle border-top, `bg-stone-50`, mirrors header. No shadow or blur.
- **Element IDs**: Renamed to generic (`add-btn`, `expiry-select`). All JS and test references updated.
- **Approach**: Fixed bottom bar (`position: fixed; bottom: 0`). Simple, predictable, mirrors header.

## Bottom Bar Spec

### HTML

New `<footer>` element after `</main>`:

```html
<footer id="bottom-bar"
    class="fixed bottom-0 left-0 right-0 z-40 bg-stone-50 border-t border-stone-200/60"
    style="--wails-draggable: drag">
  <div class="max-w-7xl mx-auto px-5 py-2.5 flex items-center gap-3">
    <!-- Left: Add + Expiry -->
    <button id="add-btn"
        class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100
               text-stone-500 text-xs font-medium py-2 px-3 rounded-md transition-colors"
        aria-label="Add files"
        style="--wails-draggable: no-drag">
        + Add
    </button>
    <select id="expiry-select"
        class="border border-stone-200 hover:border-stone-300 text-stone-500 text-xs
               font-medium py-2 px-2 rounded-md transition-colors bg-white
               focus:outline-none focus:border-stone-400 focus:ring-1
               focus:ring-stone-400/20"
        aria-label="Upload expiration"
        style="--wails-draggable: no-drag"
        title="Set expiration for new uploads">
        <option value="0">No Expiry</option>
        <option value="15">15m</option>
        <option value="60">1h</option>
        <option value="360">6h</option>
        <option value="1440">24h</option>
        <option value="10080">7d</option>
    </select>

    <!-- Right: Clip count (pushed right via ml-auto) -->
    <span id="clip-count"
        class="ml-auto text-xs font-medium text-stone-400"
        style="--wails-draggable: no-drag">
    </span>
  </div>
</footer>
```

### Styling

- `z-40`: Below header (`z-50`) and bulk toolbar (`z-50`)
- `bg-stone-50 border-t border-stone-200/60`: Matches header exactly
- `max-w-7xl mx-auto px-5`: Matches header content alignment
- `py-2.5`: Compact vertical padding (~44px total height)

### Header Changes

Remove from the header:
- `#header-add-btn` button (becomes `#add-btn` in footer)
- `#upload-expiry-select` select (becomes `#expiry-select` in footer)
- `#file-input` hidden input (move to footer)

Header retains: Logo, search, tag filter, archive toggle, hamburger menu.

### Main Content

Add `pb-14` (56px) to `<main>` to prevent gallery content from hiding behind the fixed footer.

### Bulk Toolbar

Change `bottom-5` to `bottom-16` (~64px) so it clears the bottom bar.

### Clip Count

- Updated whenever gallery renders (in `renderGallery()` or `loadClips()`)
- Format: `"12 clips"` or `"1 clip"`
- Uses `text-xs font-medium text-stone-400` (muted, matches header secondary text)

## ID Renames

| Old ID | New ID | Files affected |
|--------|--------|----------------|
| `header-add-btn` | `add-btn` | `index.html`, `app.js`, `selectors.ts` |
| `upload-expiry-select` | `expiry-select` | `index.html`, `app.js`, `test-fixtures.ts` |

## Files to Modify

1. `frontend/index.html` - Remove add/expiry from header, add `<footer>`, add `pb-14` to main
2. `frontend/js/app.js` - Update element ID references, add clip count update logic
3. `frontend/js/ui.js` - Update clip count when gallery renders
4. `e2e/helpers/selectors.ts` - Update `addButton` selector to `#add-btn`
5. `e2e/fixtures/test-fixtures.ts` - Update expiry select ID reference

## No Behavioral Changes

The add button and expiry selector work identically to before. Only their location and IDs change. The clip count is a new read-only display element.
