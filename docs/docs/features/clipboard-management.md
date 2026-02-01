---
sidebar_position: 1
---

# Clipboard Management

The core of mahpastes: capture, organize, and retrieve any content from your clipboard.

## Supported Content Types

mahpastes handles a wide variety of content:

| Type | Examples | How It's Displayed |
|------|----------|-------------------|
| **Images** | PNG, JPG, GIF, WebP, HEIC | Thumbnail preview |
| **Text** | Plain text, notes | Text preview (first 500 chars) |
| **Code** | Any programming language | Syntax-highlighted preview |
| **JSON** | API responses, configs | Formatted JSON preview |
| **HTML** | Web page snippets | HTML source preview |
| **Files** | Any file type | Filename with type icon |

## Adding Clips

### Paste from Clipboard

The primary way to add content:

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
- Folders (imports all files inside)

### With Expiration

When adding clips, you can set them to auto-delete:

1. Before pasting, click the expiration dropdown
2. Select a duration:
   - **5 minutes** — Quick temporary clips
   - **10 minutes** — Short-term storage
   - **30 minutes** — Medium-term
   - **2 hours** — Longer temporary storage
3. Paste as normal

Clips with expiration show a clock icon with remaining time.

## Viewing Clips

### Gallery View

Clips display in a responsive grid:

- **Large thumbnails** for images
- **Text previews** for text-based content
- **Metadata** showing filename, type, and timestamp

### Lightbox

Click any image clip to open it in a full-screen lightbox:

- View at full resolution
- Navigate between images with arrow keys
- Press <span className="keyboard-key">Esc</span> to close

### Content Detection

mahpastes automatically categorizes content:

```
text/plain     → Plain text editor
text/html      → HTML source view
application/json → Formatted JSON view
image/*        → Image viewer/editor
```

## Retrieving Clips

### Copy to Clipboard

Get content back to your system clipboard:

- **Quick copy**: Double-click any clip
- **Button**: Click the copy icon on the clip

The content is now in your clipboard, ready to paste elsewhere.

### Copy Path

For terminal workflows or apps that need file paths:

1. Click the path icon on any clip
2. mahpastes creates a temporary file
3. The absolute path is copied to clipboard

Example use:
```bash
# After copying path
cat /Users/you/Library/Application Support/mahpastes/clip_temp_files/42_screenshot.png
```

:::note Temporary Files
Files created via "Copy Path" are stored in a temp directory and cleaned up when mahpastes exits. Don't rely on them for permanent storage.
:::

### Save to Disk

Export a clip as a permanent file:

1. Click the download icon on a clip
2. Choose a save location in the dialog
3. The file is saved with its original filename (or a generated one)

## Organizing Clips

### Search

Filter clips instantly:

- Type in the search bar to filter by filename
- Search is case-insensitive
- Results update as you type

### Archive

Move important clips to a separate space:

1. Click the archive icon on a clip
2. The clip moves to the Archive tab
3. Access archived clips via the Archive tab

Archived clips:
- Don't appear in the main gallery
- Are never auto-deleted (expiration is removed)
- Can be unarchived at any time

### Delete

Remove clips you no longer need:

1. Click the delete icon on a clip
2. Confirm the deletion

Deleted clips are permanently removed from the database.

## Content Type Details

### Images

Supported formats:
- PNG, JPG, JPEG, GIF
- WebP, HEIC, BMP, TIFF
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

1. Paste with expiration set (e.g., 5 min)
2. Use as needed
3. Let mahpastes clean up automatically

## Limits and Performance

- **Clip limit**: 50 clips displayed in gallery (database has no limit)
- **File size**: No hard limit, but very large files may slow performance
- **Preview size**: Text previews limited to 500 characters

For large collections, use search and archive to keep the main gallery manageable.
