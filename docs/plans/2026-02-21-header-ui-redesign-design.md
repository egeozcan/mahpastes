# Header UI Redesign

## Summary

Reorganize the header to surface key actions (Add, Archive) directly in the header bar, and add a back-navigation button to the Watch view.

## Changes

### 1. Header Layout

```
[mahpastes]  [search-input | tag-filter-btn | archive-btn]  [+ Add]  [hamburger]
```

**Archive button** (icon-only, inside search group):
- Same style as tag filter button: `border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-500 p-2 rounded-md`
- Archive box SVG icon (`w-4 h-4`)
- Toggles archive view (same behavior as existing drawer button)
- Active state: `bg-stone-800 text-white border-stone-800`
- `aria-label="Toggle archive view"`, `aria-pressed`
- Archive entry remains in the hamburger drawer for discoverability

**Add button** (labeled, between search group and hamburger):
- Style: `border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-500 text-xs font-medium py-2 px-3 rounded-md`
- Content: `+ Add` text
- Triggers existing hidden `#file-input` click
- `aria-label="Add files"`
- `--wails-draggable: no-drag`

### 2. Watch View "Back to Pastes"

At the top of `#watch-view`, before global controls:
- Text button: `← Back to Pastes`
- Style: `text-xs font-medium text-stone-500 hover:text-stone-700 transition-colors flex items-center gap-1.5 mb-4`
- Left arrow SVG icon + text
- Calls `toggleWatchView()` to switch back

## Files Modified

- `frontend/index.html` — Add archive button to search group, add "Add" button, add back button to watch view
- `frontend/js/app.js` — Wire up Add button click handler, wire up header archive button
- `frontend/js/watch.js` — Wire up back-to-pastes button
- `frontend/js/ui.js` — Update `toggleViewMode()` to sync header archive button state
