---
sidebar_position: 2
---

# Quick Start

Learn the essentials of mahpastes in under 5 minutes.

## Your First Clip

### Method 1: Paste from Clipboard

1. Copy something to your system clipboard (text, image, or file)
2. Focus the mahpastes window
3. Press <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">V</span>

The content appears as a clip in your gallery.

### Method 2: Drag and Drop

1. Drag any file from Finder
2. Drop it onto the mahpastes window

Multiple files can be dropped at once.

## Understanding the Gallery

The main window shows your clips in a grid layout:

- **Images** display as thumbnails
- **Text/Code** shows a preview of the content
- **Files** show the filename and type icon

![Gallery view](/img/screenshots/gallery.png)

### Clip Actions

Open the clip context menu (three dots on each card) to access actions:

| Button | Action |
|--------|--------|
| **Copy Contents** | Copy content back to clipboard |
| **Copy File** | Copy the clip as a file to clipboard |
| **Copy Path** | Create temp file and copy its path |
| **Edit** | Open in image or text editor |
| **Archive** | Move to archive |
| **Download** | Save to disk |
| **Delete** | Remove permanently |

![Clip card menu](/img/screenshots/card-menu.png)

## Working with Clips

### Copy Back to Clipboard

Use a clip's three-dot menu and click **Copy Contents**. You can then paste it anywhere.

For images in the lightbox, <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">C</span> also copies the active clip contents.

### Copy Path

Need to reference a file in terminal or another app? Use **Copy Path**:

1. Open the clip menu (three dots)
2. Click **Copy Path**
3. mahpastes creates a temporary file
4. The file path is copied to your clipboard
5. Paste the path wherever you need it

Temp files are cleaned up when you quit mahpastes.

### Search and Filter

Use the search bar to filter clips:

- Type a filename to find specific clips
- Filter by type (images, text, etc.)
- Results update instantly

## Clip Expiration

Set per-clip expiration at upload time using the bottom bar dropdown, or on existing clips via the context menu. Presets: 15 minutes, 1 hour, 6 hours, 24 hours, 7 days.

See [Auto-Delete](/features/auto-delete) for details.

## Using the Archive

The archive keeps important clips separate from your active workspace.

### Archive a Clip

Open a clip's three-dot menu and click **Archive**. It moves to the archive section.

### View Archived Clips

Click the **Archive** button in the header, or open the menu drawer and click **Archive**, to see archived clips.

### Unarchive

In Archive view, open a clip's menu and click **Restore** to move it back to the main gallery.

## Editing Clips

### Edit Images

1. Click the edit button on an image clip
2. Use the toolbar to annotate:
   - **Brush** — Freehand drawing
   - **Line** — Draw straight lines
   - **Rectangle** — Draw rectangles
   - **Circle** — Draw circles
   - **Text** — Add text annotations
   - **Eraser** — Erase annotations
3. Click **Save As** to create a new clip with your edits

### Edit Text

1. Click the edit button on a text clip
2. Modify the content in the editor
3. Click **Save As** to create a new clip with the changes

## Bulk Operations

Select multiple clips for batch operations:

1. Click the checkbox on each clip you want to select
2. Use the bulk action bar that appears:
   - **Copy** — Copy selected clips as files to system clipboard
   - **Tag** — Add or remove tags from selected clips
   - **Archive** — Move selected to archive
   - **Download** — Save as ZIP file
   - **Delete** — Remove all selected
   - **Compare** — Compare two selected images side-by-side

## Keyboard Shortcuts

Essential shortcuts for power users:

| Shortcut | Action |
|----------|--------|
| <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">V</span> | Paste from clipboard |
| <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">C</span> | Copy clip contents (lightbox) or copy selected clips as files (gallery) |
| <span className="keyboard-key">Esc</span> | Close modal/dialog |
| <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">Z</span> | Undo (in editor) |
| <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">Y</span> | Redo (in editor) |
| <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">S</span> | Save (in editor) |

See all shortcuts in [Keyboard Shortcuts](/getting-started/keyboard-shortcuts).

## What's Next?

Now that you know the basics:

- Set up [Watch Folders](/features/watch-folders) for automatic imports
- Learn about [Image Comparison](/features/image-comparison)
- Explore the full [Features guide](/features/clipboard-management)
