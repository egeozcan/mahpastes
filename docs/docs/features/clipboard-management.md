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
| **Text** | Plain text, notes | Text preview (first 500 chars) |
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

mahpastes automatically detects the content type and stores it appropriately.

### Drag and Drop

Drop files directly into the app:

1. Select one or more files in Finder/Explorer
2. Drag them onto the mahpastes window
3. Release to import

You can drop:
- Single files
- Multiple files at once

### Expiration

The backend supports per-clip expiration (auto-delete after a set time), but the expiration UI is not yet implemented in the frontend. See [Auto-Delete](./auto-delete.md) for details.

## Viewing Clips

### Gallery View

Clips display in a responsive grid:

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
- **Actions** menu with Copy Path, Copy File, Copy Contents, Save, Edit, Tags, Archive, Delete
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

mahpastes automatically categorizes content:

```
text/plain     → Plain text editor
text/html      → HTML source view
application/json → Formatted JSON view
image/*        → Image viewer/editor
```

## Retrieving Clips

### Context Menu

Click the three-dot menu button on any clip card, or right-click anywhere on the card, to open the context menu. Available actions:

- **Copy Path** -- creates a temporary file and copies the absolute path to clipboard
- **Copy File** -- copies the clip as a file to the system clipboard (macOS only, via NSPasteboard)
- **Copy Contents** -- copies raw text or image data to the system clipboard (available for text/\*, application/json, and image/\* types)
- **Save** -- opens a native save dialog to export the clip to disk
- **Edit** -- opens the image editor or text editor
- **Tags** -- opens the tag popover
- **Archive** / **Restore** -- toggles archive state
- **Delete** -- deletes the clip after confirmation

If plugins define card actions, those appear below a divider in the same menu.

:::note Temporary Files
Files created via "Copy Path" are stored in a temp directory and cleaned up periodically (60-minute lease) and when mahpastes exits. Don't rely on them for permanent storage.
:::

## Organizing Clips

### Search

Filter clips by filename and content type:

- Type in the search bar to filter
- Search is case-insensitive substring matching
- Results update as you type
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
- **HTML**: Content starting with `<!DOCTYPE html`
- **JSON**: Valid JSON objects or arrays

Detection happens automatically when you paste.

### Binary Files

For non-text files:
- Stored as binary blobs
- Displayed with appropriate icons
- Full file preserved for export

## Best Practices

### For Screenshots

1. Take screenshot (system shortcut)
2. Paste into mahpastes immediately
3. Add annotations if needed
4. Copy path for terminal use or copy back to clipboard

### For Code Snippets

1. Copy code from your editor
2. Paste into mahpastes
3. Archive important snippets for later
4. Use search to find them quickly

### For Temporary Files

1. Paste content you need briefly
2. Use as needed
3. Delete manually when done (per-clip expiration UI is not yet available)

## Limits and Performance

- **Clip limit**: 50 clips displayed in gallery (database has no limit)
- **File size**: No hard limit, but very large files may slow performance
- **Preview size**: Text previews limited to 500 characters

For large collections, use search and archive to keep the main gallery manageable.
