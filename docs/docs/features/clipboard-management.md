---
sidebar_position: 1
---

# Clipboard Management

Store, organize, and retrieve clipboard content.

## Supported Content Types

mahpastes handles these content types:

| Type | Examples | How It's Displayed |
|------|----------|-------------------|
| **Images** | PNG, JPG, GIF, WebP, BMP, TIFF, SVG | Thumbnail preview |
| **Text** | Plain text, notes | Text preview (first 500 characters) |
| **Code** | Any programming language | Monospace text preview |
| **JSON** | API responses, configs | Formatted JSON preview |
| **HTML** | Web page snippets | HTML source preview |
| **Files** | Any file type | Filename with type icon |

## Adding Clips

### Paste from Clipboard

The main way to add content:

1. Copy something to your system clipboard (from any app)
2. Focus the mahpastes window
3. Press <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">V</span>

mahpastes detects the content type and stores it.

### Drag and Drop

Drop files directly into the app:

1. Select one or more files in Finder/Explorer
2. Drag them onto the mahpastes window
3. Release to import

Supported drop types:
- Single files
- Multiple files at once

### Expiration

Set per-clip expiration when uploading (dropdown in the bottom bar) or on existing clips via the context menu. See [Auto-Delete](./auto-delete.md) for details.

## Viewing Clips

### Gallery View

Clips display in a grid:

- **Large thumbnails** for images
- **Text previews** for text-based content
- **Metadata** showing filename, type, and timestamp

![Gallery view](/img/screenshots/gallery.png)

### Lightbox

Click any image clip's preview to open it in a full-screen lightbox.

**Navigation:**
- Arrow keys or arrow buttons to move between images
- Swipe left/right on trackpad or touchscreen
- Trackpad horizontal scroll with momentum detection

**Zoom and pan:**
- Slider in the bottom bar (1x to 4x)
- Trackpad pinch to zoom
- Touch pinch to zoom
- Double-click to reset zoom to 1x
- Click and drag to pan when zoomed in

**Bottom bar:**
- Position counter (e.g. "1/5"), filename, and image resolution
- Zoom slider with percentage display
- **Actions** menu with Open, Open With, Copy (Path / File / Contents), Save, Edit, Tags, Metadata, Rename, Set Expiration / Cancel Expiration, Merge Duplicates (if duplicates exist), Archive, Delete
- Plugin action menu (if plugins define lightbox actions)

**Keyboard shortcuts:**

| Shortcut | Action |
|----------|--------|
| <span className="keyboard-key">Esc</span> | Close lightbox (closes open menus first) |
| <span className="keyboard-key">Arrow Left</span> | Previous image |
| <span className="keyboard-key">Arrow Right</span> | Next image |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">C</span> | Copy image contents to clipboard |

![Image lightbox](/img/screenshots/lightbox.png)

### Opening Content

- **Images**: Click the preview to open in the lightbox
- **Text/Code/JSON/HTML**: Click the preview to open in the text editor

### Content Detection

mahpastes categorizes content by MIME type:

```
text/plain     → Plain text editor
text/html      → HTML source view
application/json → Formatted JSON view
image/*        → Image viewer/editor
```

## Retrieving Clips

### Context Menu

Click the three-dot menu button on any clip card, or right-click anywhere on the card, to open the context menu. The menu uses submenus to group related actions. Available actions:

- **Open** -- opens the clip with the system default application
- **Open With** -- opens a file dialog to choose an application
- **Copy** (submenu) -- expands to show **Path**, **File**, and **Contents**
  - **Path** -- creates a temporary file and copies the absolute path to clipboard
  - **File** -- copies the clip as a file to the system clipboard (macOS via NSPasteboard, Windows via PowerShell; not supported on Linux)
  - **Contents** -- copies raw text or image data to the system clipboard (available for text/\*, application/json, and image/\* types; non-PNG images are converted to PNG before copying)
- **Save** -- opens a native save dialog to export the clip to disk
- **Edit** -- opens the image editor or text editor (shown for editable types)
- **Tags** -- opens the tag popover
- **Metadata** -- opens the metadata modal for viewing and editing key-value pairs
- **Rename** -- opens a prompt to change the clip's filename
- **Set Expiration** -- opens expiration preset popover (shown when clip has no expiry)
- **Cancel Expiration** -- removes expiration (shown when clip has an active expiry)
- **Archive** / **Restore** -- toggles archive state
- **Merge Duplicates** -- merges duplicate clips (shown when duplicates exist)
- **Delete** -- deletes the clip after confirmation

If plugins define card actions, they appear in a **Plugins** submenu below a divider.

When more than one clip is selected, right-clicking one of the selected clips opens the bulk actions instead of this menu. See [Bulk Actions](./bulk-actions.md#bulk-context-menu).

:::note Temporary Files
Files created via "Copy Path" are stored in a temp directory and cleaned up periodically (60-minute lease) and when mahpastes exits. Don't rely on them for permanent storage.
:::

## Organizing Clips

### Search

Filter clips by filename and content type:

- Type in the search bar to filter
- Search is case-insensitive substring matching against the filename and MIME type
- Results update as you type
- Press <span className="keyboard-key">Enter</span> to move focus to the first visible result
- Filters only the currently visible cards (does not search clip contents)
- Works in both the main gallery and archive views (whichever is active)

![Search filtering](/img/screenshots/search.png)

### Archive

Move important clips to a separate space:

1. Click the menu button (three dots) on a clip
2. Select **Archive**
3. The clip moves to the Archive view
4. Access archived clips via the Archive button

Archived clips:
- Don't appear in the main gallery
- Can be unarchived at any time

:::warning
Archiving does **not** remove expiration. If a clip has an auto-delete timer, it will still expire even when archived.
:::

### Delete

Remove clips you no longer need:

1. Click the menu button (three dots) on a clip
2. Select **Delete**
3. Confirm the deletion

Deleted clips are permanently removed from the database.

## Content Type Details

### Images

Supported formats:
- PNG, JPG, JPEG, GIF
- WebP, BMP, TIFF
- SVG (displayed as image)

When pasting from clipboard, images are typically captured as PNG.

### Text Content

mahpastes distinguishes between:

- **Plain text**: Regular text content
- **HTML**: Content whose trimmed body starts with `<!DOCTYPE html` (case-sensitive prefix match)
- **JSON**: Any string that parses as valid JSON

Detection runs at upload time when the incoming content type is `text/plain` or empty. Files uploaded with an explicit non-text content type keep their original type.

### Binary Files

For non-text files:
- Stored as binary blobs
- Displayed with appropriate icons
- Full file preserved for export

## Sorting

Control the order of clips in the gallery using the sort popover in the bottom bar.

![Sort popover](/img/screenshots/sort-popover.png)

| Sort Field | Description |
|------------|-------------|
| **Date added** | Creation timestamp (default, newest first) |
| **Filename** | Alphabetical by filename |
| **File size** | Clip data size in bytes |
| **Content type** | MIME type string |

Click the active sort field to toggle between ascending and descending. Click a different field to switch to it (defaults to descending). The sort preference (field and direction) is persisted to the settings table and restored on app launch. A secondary tiebreaker of `created_at` + `id` keeps the order stable when two clips share the same value for the primary sort field.

## Limits and Performance

- **Clip limit**: 50 clips displayed in gallery (database has no limit)
- **File size**: No hard limit, but very large files may slow performance
- **Preview size**: Text previews limited to 500 characters

For large collections, use search and archive to keep the main gallery manageable.

## Related

- [Drag and Drop](./drag-and-drop.md) -- drag clips out of the app
- [Bulk Actions](./bulk-actions.md) -- operate on multiple clips at once
- [Tags](./tags.md) -- organize clips with color-coded labels
- [Image Editor](./image-editor.md) -- annotate images
- [Text Editor](./text-editor.md) -- edit text clips
- [Metadata](./metadata.md) -- attach key-value pairs to clips
