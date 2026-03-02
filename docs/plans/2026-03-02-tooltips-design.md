# Tooltips Design

## Overview

Add custom styled tooltips to all interactive elements in the app. Tooltips provide helpful guidance text on hover, going beyond simple labels to explain what each action actually does.

## Requirements

- Custom CSS tooltips matching the stone color palette (dark bg, light text)
- All interactive elements get tooltips -- buttons, icon buttons, menu items, toggles
- Tooltip text should be technically accurate and informative (not just repeat the label)
- Include keyboard shortcuts where applicable
- Smart positioning: default below, auto-flip to avoid viewport clipping
- 300ms hover delay to prevent flashing during casual mouse movement
- Settings toggle to disable tooltips (enabled by default)

## Approach: CSS + Minimal JS (Approach A)

Attribute-driven system using `data-tooltip` attributes and CSS `::after` pseudo-elements. A small JS module (~50-60 lines) handles viewport-edge detection and position flipping.

### Why This Approach

- Matches the app's vanilla JS philosophy (no dependencies)
- Minimal JS footprint -- CSS handles rendering
- Easy to add to any element: just add `data-tooltip="text"`
- Works with dynamically created elements via the attribute

## Architecture

### New File: `frontend/js/tooltips.js`

- Event delegation on `document` for `mouseenter`/`mouseleave` on `[data-tooltip]`
- On `mouseenter`: check element's `getBoundingClientRect()` against viewport bounds
- Set `data-tooltip-pos` to `above`, `below`, `left`, or `right` based on available space (default: `below`)
- On `mouseleave`: remove the position override
- Check settings on init; toggle `tooltips-disabled` class on `<body>`

### CSS Additions in `frontend/css/main.css`

- `[data-tooltip]` base: `position: relative`
- `[data-tooltip]::after`: content from `attr(data-tooltip)`, styled `bg-stone-800 text-white text-[10px] font-medium px-2 py-1 rounded shadow-sm`, opacity 0
- `[data-tooltip]:hover::after`: opacity 1 after 300ms delay
- Position variants: `[data-tooltip-pos="above"]::after`, `[data-tooltip-pos="below"]::after`, etc.
- Arrow via `[data-tooltip]::before` pointing toward trigger
- `z-index: 270` (above all existing layers including toasts at 260)
- `body.tooltips-disabled [data-tooltip]::after, body.tooltips-disabled [data-tooltip]::before { display: none; }`

### Settings Integration

- Add "Show tooltips" toggle in Settings modal
- Store in app preferences (Go side)
- Default: enabled
- When disabled, add `tooltips-disabled` class on `<body>` to suppress via CSS

## Tooltip Content Catalog

**IMPLEMENTATION NOTE**: Agents must read the actual source code for each action to verify tooltip descriptions are technically accurate before applying them.

### Header Bar

| Element | Tooltip |
|---|---|
| Tag filter button | "Filter clips by tag (F)" |
| Archive toggle | "Show archived clips (A)" |
| Sort button | "Change sort order" |
| Menu button | "Open navigation menu" |

### Card Actions

| Element | Tooltip |
|---|---|
| Drag handle | "Creates a temp file -- drag into another app to export" |
| Three-dot menu | "More actions for this clip" |

### Card Context Menu / Lightbox File Menu

| Element | Tooltip |
|---|---|
| Copy Path | "Create a temp file and copy its path to clipboard" |
| Copy File | "Place file on clipboard for pasting into other apps" |
| Copy Contents | "Copy the raw text or data to clipboard" |
| Save | "Save a copy to your Downloads folder" |
| Edit | "Open in the built-in image editor for annotation" |
| Tags | "Add or remove tags" |
| Metadata | "View and edit file metadata" |
| Set Expiration | "Schedule auto-deletion after a time period" |
| Cancel Expiration | "Cancel the scheduled auto-deletion" |
| Archive | "Move to archive without deleting" |
| Restore | "Move back from archive" |
| Merge Duplicates | "Find clips with identical content and merge them" |
| Delete | "Permanently delete -- this cannot be undone" |

### Lightbox

| Element | Tooltip |
|---|---|
| Close | "Close viewer (Esc)" |
| Previous | "Previous clip (Left)" |
| Next | "Next clip (Right)" |
| Plugin actions trigger | "Run plugin actions on this clip" |
| File actions trigger | "File operations and management" |
| Zoom slider | "Adjust zoom level" |

### Image Comparison Modal

| Element | Tooltip |
|---|---|
| Swap | "Swap left and right images (S)" |
| Close | "Close comparison (Esc)" |
| Fade mode | "Crossfade opacity between both images (1)" |
| Slider mode | "Drag a divider to reveal each image (2)" |
| Diff mode | "Compute and highlight pixel-level differences (3)" |
| Zoom out | "Zoom out (-)" |
| Zoom in | "Zoom in (+)" |
| Zoom fit | "Fit image to view (0)" |
| Stretch toggle | "Scale images to fill the view area" |
| Align H / Align V | "Change image alignment" |

### Image Editor Toolbar

| Element | Tooltip |
|---|---|
| Brush | "Freehand brush (B)" |
| Line | "Draw straight line (L)" |
| Rectangle | "Draw rectangle (R)" |
| Circle | "Draw circle (C)" |
| Text | "Add text annotation (T)" |
| Eraser | "Erase drawn content (E)" |
| Undo | "Undo last action (Ctrl+Z)" |
| Redo | "Redo last action (Ctrl+Y)" |
| Color input | "Pick drawing color" |
| Opacity range | "Adjust brush opacity" |
| Size range | "Adjust brush size" |
| Save | "Save edits as a new clip in your library" |
| Close | "Discard all edits and close editor (Esc)" |

### Footer Bar

| Element | Tooltip |
|---|---|
| Add files button | "Upload files to your library" |
| Expiry select | "New uploads will auto-delete after this duration" |
| Loading status | "Shows sync status -- click to retry if stalled" |

### Nav Drawer

| Element | Tooltip |
|---|---|
| Close | "Close menu" |
| Watch view | "Monitor folders and auto-import new files" |
| Archive view | "View archived clips" |
| Clear temporary | "Remove all clips that were marked temporary" |
| Deduplicate | "Scan library for identical files and merge them" |
| Settings | "Configure app preferences" |
| Plugins | "Manage installed plugins" |

### Watch Folder Actions

| Element | Tooltip |
|---|---|
| Add Folder | "Pick a folder to monitor for new files" |
| Global watch toggle | "Master switch for all folder watchers" |
| Pause/Resume | "Pause watching this folder" / "Resume watching this folder" |
| Remove | "Stop watching this folder and remove it from the list" |

### Bulk Toolbar

| Element | Tooltip |
|---|---|
| Select All | "Select all visible clips" |
| Compare | "Open selected images in side-by-side comparison view" |
| Tag | "Add a tag to selected clips" |
| Expire | "Set expiration on selected clips" |
| Clear Expiry | "Remove expiration from selected clips" |
| Copy | "Copy selected files to clipboard for pasting" |
| Download | "Save selected files to your Downloads folder" |
| Archive/Restore | "Archive selected clips" / "Restore selected clips" |
| Delete | "Permanently delete selected -- cannot be undone" |
| Cancel | "Deselect all clips" |

### Modal Close Buttons

All close X buttons across modals (settings, plugins, queue, metadata, etc.): "Close"

## Testing

- Add e2e tests verifying tooltips appear on hover for key elements
- Test viewport-edge flipping (tooltip near bottom of screen flips to above)
- Test settings toggle hides/shows tooltips
