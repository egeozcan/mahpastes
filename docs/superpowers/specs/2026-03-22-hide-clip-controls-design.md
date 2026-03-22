# Hide Clip Controls in Non-Clip Views

## Problem

When viewing Watch Folders or Serve views, clip-related controls (search bar, tag filters, sort, archive toggle, folder mode, add button, expiry dropdown, clip count) remain visible even though they serve no purpose outside the clips view.

## Solution

Extend `switchView()` in `frontend/js/serve.js` to toggle visibility of clip-related controls when switching between views.

## Controls to Hide

### Header

Add `id="clip-controls"` to the existing wrapper `<div>` at line 23 of `index.html` (the `flex-1 max-w-md` div that contains search, tag filter, sort, divider, archive, and folder mode). Toggle this single element to hide/show all header clip controls at once.

Also hide `#active-tags-container` (the filter pill bar below the header).

### Bottom Bar

- `#add-btn`
- `#expiry-select`
- `#clip-count`

`#loading-status` remains visible — it shows plugin task progress which is app-wide, not clip-specific. The `#bottom-bar` footer itself stays visible to house `#loading-status`.

### Always Visible

- MAHPASTES logo (`#logo-btn`)
- Hamburger menu (`#drawer-toggle-btn`) with activity indicator
- `#loading-status` (plugin task progress)
- `#bottom-bar` container

## Approach

In `switchView()`, toggle `hidden` on `#clip-controls`, `#active-tags-container`, `#add-btn`, `#expiry-select`, and `#clip-count` based on whether the target view is clips or not.

Before hiding, explicitly close any open popovers/dropdowns: call `closeSortPopover()` (or equivalent) and close the tag filter dropdown. The sort popover is appended to `document.body` and would otherwise remain as a floating orphan. Keyboard shortcuts (W/S) bypass click-outside handlers, so relying on those is insufficient.

No transition animation — instant `hidden` toggle via `display: none` is appropriate here since the view is switching entirely, not partially updating.

## Edge Cases

- **Active filters/search**: State is preserved but hidden. Returning to clips reveals them unchanged.
- **Bulk toolbar**: Already tied to clip selection state, won't appear in non-clip views naturally.
- **Open dropdowns/popovers**: Explicitly closed on view switch (see Approach).

## Files Modified

- `frontend/index.html` — add `id="clip-controls"` to header wrapper div
- `frontend/js/serve.js` — add hide/show and popover-close logic in `switchView()`

## Testing

New e2e test assertions (not just relying on existing tests, since this is new behavior):

- Verify `#clip-controls` is hidden when entering Watch view
- Verify `#clip-controls` is hidden when entering Serve view
- Verify `#add-btn`, `#expiry-select`, `#clip-count` are hidden in non-clip views
- Verify all controls restored when returning to Clips view
- Verify filter/search state preserved across view switches
- Verify sort popover closes when switching away from clips
