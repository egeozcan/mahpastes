---
sidebar_position: 7
---

# Watch Folders

Automatically import files from designated folders. Set up watch folders to capture screenshots, downloads, or any files as they appear.

## Overview

Watch folders monitor directories on your system:
- New files are automatically imported as clips
- Configure filters to import only specific file types
- Optionally auto-archive imported files
- Pause watching per-folder or globally

## Setting Up a Watch Folder

1. Click the **Watch Folders** button (or gear icon)
2. Click **Add Folder**
3. Select a folder using the file picker
4. Configure filter and options
5. Click **Save**

mahpastes immediately starts watching the folder.

## Configuration Options

### Filter Mode

Control which files are imported:

| Mode | Description |
|------|-------------|
| **All Files** | Import every file (default) |
| **Presets** | Import files matching preset categories |
| **Custom Regex** | Import files matching a regex pattern |

### Presets

When using preset mode, select one or more categories:

| Preset | File Extensions |
|--------|-----------------|
| **Images** | jpg, jpeg, png, gif, webp, heic, bmp, tiff, svg |
| **Documents** | pdf, doc, docx, txt, md, rtf, odt, xls, xlsx |
| **Videos** | mp4, mov, avi, mkv, webm, m4v, wmv |

Multiple presets can be selected (e.g., Images + Documents).

### Custom Regex

For precise control, use a regular expression:

```regex
# Only PNG files
\.png$

# PNG and JPG
\.(png|jpe?g)$

# Files starting with "screenshot"
^screenshot.*

# Any image file
\.(png|jpe?g|gif|webp)$
```

The regex matches against the filename (not the full path).

### Auto-Archive

Enable to automatically move imported files to archive:
- Keeps main gallery clean
- Good for background collection
- Find imports in Archive tab

### Process Existing Files

When adding a new watch folder:
- **Enabled**: Import all existing files in the folder
- **Disabled**: Only import new files going forward

## Managing Watch Folders

### Pause/Resume

#### Per-Folder

1. Open Watch Folders settings
2. Toggle the pause button for a specific folder
3. Paused folders stop importing (but remain configured)

#### Global Pause

1. Click the global pause button
2. All watching stops
3. Click again to resume all

### Edit Configuration

1. Open Watch Folders settings
2. Click the edit button on a folder
3. Modify filter or options
4. Save changes

### Remove

1. Open Watch Folders settings
2. Click the delete button on a folder
3. Confirm removal

The folder is no longer watched. Existing imported clips remain.

## Watch Status

The status indicator shows:
- **Active**: Number of folders actively watching
- **Paused**: Folders or global watching is paused
- **Errors**: Issues with specific folders

### Folder Status Icons

| Icon | Meaning |
|------|---------|
| Green dot | Actively watching |
| Yellow pause | Paused |
| Red warning | Folder missing or error |

## Use Cases

### Screenshots Folder

Automatically capture all screenshots:

```
Folder: ~/Desktop or ~/Screenshots
Filter: Images preset
Auto-archive: Optional
```

Every screenshot you take appears in mahpastes.

### Downloads Monitoring

Import specific download types:

```
Folder: ~/Downloads
Filter: Custom regex \.pdf$
Auto-archive: Yes
```

PDFs are imported and archived automatically.

### Design Assets

Collect exported design files:

```
Folder: ~/Design/Exports
Filter: Images preset
Auto-archive: No
```

Design exports appear in main gallery for quick access.

### Recording Outputs

Capture screen recordings:

```
Folder: ~/Movies
Filter: Videos preset
Auto-archive: Yes
```

Videos are imported and archived for later review.

## Technical Details

### How It Works

1. mahpastes uses filesystem events (fsnotify)
2. New file creation triggers import
3. File is read and stored as a clip
4. Events are debounced (500ms) for stability

### Debouncing

Files are imported after 500ms of stability:
- Prevents importing partially-written files
- Waits for large files to finish writing
- Handles rapid file creation gracefully

### Error Handling

If a file can't be imported:
- An error notification is shown
- Other files continue to be processed
- Check file permissions and format

## Tips

### Best Folder Choices

Good watch folders:
- Screenshot destinations
- Download folders (with filters)
- Export directories
- Drop zones you create

Avoid watching:
- System folders
- Very active directories (hundreds of files)
- Network drives (may be slow or unreliable)

### Filter Strategy

- Start with presets for common use cases
- Use regex for precise matching
- Test regex on a few files first
- Combine with auto-archive for background collection

### Performance

- Watch folders add minimal overhead
- Many watched folders are fine
- Very large folders may slow initial scan (if processing existing)
- Binary files are stored as-is (no processing)

## Troubleshooting

### Folder Not Watching

1. Check folder still exists
2. Verify it's not paused
3. Check global pause is off
4. Restart mahpastes if needed

### Files Not Importing

1. Check filter settings
2. Test filter regex against filename
3. Verify file permissions
4. Check for error notifications

### High Resource Usage

1. Reduce number of watched folders
2. Use more specific filters
3. Disable "process existing" for large folders
4. Avoid watching very active directories
