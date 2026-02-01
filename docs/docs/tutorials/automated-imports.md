---
sidebar_position: 3
---

# Automated File Imports

Set up watch folders to automatically capture files as they're created. No manual paste or drag-drop required.

## Use Cases

- **Screenshots**: Capture every screenshot automatically
- **Downloads**: Import specific file types from Downloads
- **Design exports**: Collect exported assets
- **Recordings**: Import screen recordings
- **Drop zones**: Create custom import folders

## Setting Up Watch Folders

### Basic Setup

1. Click the **Watch Folders** button (gear icon)
2. Click **Add Folder**
3. Select the folder to watch
4. Configure options
5. Click **Save**

### Configuration Options

| Option | Description |
|--------|-------------|
| **Filter Mode** | All files, presets, or custom regex |
| **Filter Presets** | Images, Documents, or Videos |
| **Custom Regex** | Pattern to match filenames |
| **Auto-Archive** | Move imports to archive automatically |
| **Process Existing** | Import files already in folder |

## Common Configurations

### Screenshots Folder

Automatically capture all screenshots:

| Setting | Value |
|---------|-------|
| Folder | `~/Desktop` or `~/Screenshots` |
| Filter Mode | Presets |
| Presets | Images |
| Auto-Archive | Off |
| Process Existing | No |

Every new screenshot appears in mahpastes immediately.

### Downloads (PDFs Only)

Import only PDF downloads:

| Setting | Value |
|---------|-------|
| Folder | `~/Downloads` |
| Filter Mode | Custom |
| Regex | `\.pdf$` |
| Auto-Archive | Yes |
| Process Existing | No |

PDFs are imported and archived, other files ignored.

### Design Exports

Capture exported design assets:

| Setting | Value |
|---------|-------|
| Folder | `~/Design/Exports` |
| Filter Mode | Presets |
| Presets | Images |
| Auto-Archive | No |
| Process Existing | No |

Design exports appear in main gallery for quick access.

### Screen Recordings

Import video recordings:

| Setting | Value |
|---------|-------|
| Folder | `~/Movies` |
| Filter Mode | Presets |
| Presets | Videos |
| Auto-Archive | Yes |
| Process Existing | No |

Videos are archived for later review.

## Custom Regex Patterns

For precise control over which files are imported.

### Syntax

The regex matches against the **filename** (not the full path).

### Examples

```regex
# Only PNG files
\.png$

# PNG or JPG
\.(png|jpe?g)$

# Files starting with "screenshot"
^screenshot.*

# Files containing "export"
export

# Numbered files (e.g., image001.png)
\d{3}\.(png|jpg)$

# Specific naming convention
^[A-Z]{3}-\d{4}\.pdf$
```

### Testing Your Regex

Before saving:
1. Think of example filenames
2. Test if your regex matches
3. Consider edge cases

Example test:
```
Pattern: \.pdf$
Matches: document.pdf ✓
Matches: report.PDF ✗ (case-sensitive)
Matches: pdf-guide.txt ✗
```

For case-insensitive matching:
```regex
(?i)\.pdf$
```

## Managing Multiple Watch Folders

### Adding Multiple Folders

You can watch several folders simultaneously:

1. Screenshots folder → Images preset
2. Downloads folder → PDFs only
3. Design exports → Images preset
4. Custom drop zone → All files

### Pause Controls

#### Per-Folder Pause

Temporarily stop watching a specific folder:

1. Open Watch Folders settings
2. Click the pause button for that folder
3. The folder stops being watched
4. Click again to resume

#### Global Pause

Stop all watching at once:

1. Click the global pause button
2. All folders stop being watched
3. Click again to resume all

### Removing Watch Folders

1. Open Watch Folders settings
2. Click the delete button for the folder
3. Confirm removal

The folder is no longer watched. Existing clips remain.

## How It Works

### File Detection

1. mahpastes monitors folder using filesystem events
2. New file creation triggers processing
3. File is read after 500ms debounce (ensures complete write)
4. File is imported as a clip
5. Original file is deleted (moved to mahpastes)

### Debouncing

Files are processed after 500ms of stability:
- Prevents importing partially-written files
- Handles large files that take time to write
- Manages rapid file creation gracefully

### Error Handling

If import fails:
- Error notification is shown
- Original file is preserved
- Other files continue processing

## Workflow Examples

### Zero-Touch Screenshot Workflow

1. Set up watch folder for Screenshots
2. Take screenshots normally
3. Screenshots auto-appear in mahpastes
4. Annotate and share as needed
5. No manual import required

### Design Asset Collection

1. Create export folder in design tool
2. Set up watch folder for that location
3. Export designs as usual
4. Assets appear in mahpastes
5. Review, compare, or share

### Document Collection

1. Watch Downloads folder with PDF filter
2. Download documents normally
3. PDFs auto-import and archive
4. Other downloads stay in Downloads
5. Find PDFs easily in mahpastes Archive

## Tips

### Choose Folders Wisely

Good choices:
- Dedicated screenshot folders
- Export destinations
- Custom drop zones you create
- Specific subfolders of Downloads

Avoid:
- System folders
- Very active directories
- Network drives (unreliable events)
- Temporary system folders

### Use Auto-Archive

Enable auto-archive for:
- Background collection
- Keeping main gallery clean
- Files you'll review later

Disable for:
- Active working materials
- Content you need to see immediately

### Create Drop Zones

Create dedicated folders for mahpastes:

```bash
mkdir ~/mahpastes-inbox
```

Then:
1. Add as watch folder with "All files"
2. Drop any file there to import
3. mahpastes cleans up automatically

### Monitor Status

Check watch status regularly:
- Green = actively watching
- Yellow = paused
- Red = folder missing or error

## Troubleshooting

### Files Not Importing

1. Check folder still exists
2. Verify watch is not paused
3. Check filter matches file type
4. Look for error notifications
5. Restart mahpastes if needed

### Original Files Not Deleted

Happens when:
- Import fails (check errors)
- File permissions prevent deletion
- File in use by another process

### Too Many Imports

If overwhelmed by imports:
1. Use more specific filters
2. Enable auto-archive
3. Reduce number of watched folders
4. Create dedicated import folders
