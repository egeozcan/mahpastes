# Hide Clip Controls in Non-Clip Views

## Problem

When viewing Watch Folders or Serve views, clip-related controls (search bar, tag filters, sort, archive toggle, folder mode, add button, expiry dropdown, clip count) remain visible even though they serve no purpose outside the clips view.

## Solution

Extend `switchView()` in `frontend/js/serve.js` to toggle visibility of clip-related controls when switching between views.

## Controls to Hide

### Header

- `#search-input` (or its parent wrapper if one exists)
- `#tag-filter-btn` (and `#tag-filter-badge`)
- `#sort-btn`
- Vertical divider between sort and archive buttons
- `#header-archive-btn`
- `#folder-mode-btn`
- `#active-tags-container` (filter pill bar)

### Bottom Bar

- `#add-btn`
- `#expiry-select`
- `#clip-count`

### Always Visible

- MAHPASTES logo (`#logo-btn`)
- Hamburger menu (`#drawer-toggle-btn`) with activity indicator

## Approach

In `switchView()`, add `classList.toggle('hidden', isNonClipView)` calls for each control listed above. When switching to clips view, controls are shown. When switching to watch or serve, controls are hidden.

## Edge Cases

- **Active filters/search**: State is preserved but hidden. Returning to clips reveals them unchanged.
- **Bulk toolbar**: Already tied to clip selection state, won't appear in non-clip views naturally.
- **Tag filter dropdown**: If open when switching views, `switchView()` already triggers view resets that close dropdowns.

## Files Modified

- `frontend/js/serve.js` — add hide/show logic in `switchView()`

## Testing

- Existing e2e tests for view switching in `e2e/tests/watch/` and `e2e/tests/serve/`
- Verify controls hidden when entering Watch view
- Verify controls hidden when entering Serve view
- Verify controls restored when returning to Clips view
- Verify filter/search state preserved across view switches
