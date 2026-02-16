---
sidebar_position: 11
---

# Drag and Drop

Drag clips directly from mahpastes into other applications on your system.

## Overview

mahpastes supports dragging clips out of the app and dropping them into other programs. This works like dragging a file from Finder -- the receiving app gets a real file it can open, attach, or process.

Supported drop targets include:
- **Finder** -- drop to save as a file
- **Mail / Outlook** -- drop to attach
- **Slack / Discord** -- drop to share
- **Any app** that accepts file drops

## How to Drag a Clip

1. Hover over a clip card in the gallery
2. Click and hold the drag handle
3. Drag the clip to the target app
4. Release to drop

mahpastes creates a temporary file when the drag starts and hands it off to the operating system. The receiving app gets the file with the correct filename and content type.

## What Gets Transferred

| Clip Type | What's Dragged |
|-----------|---------------|
| **Images** | The image file (PNG, JPG, etc.) |
| **Text** | A text file with the content |
| **JSON** | A `.json` file |
| **HTML** | An `.html` file |
| **Other** | The original file |

The file keeps its original filename. If the clip was pasted without a filename, mahpastes generates one based on the content type.

## Platform Support

| Platform | Status |
|----------|--------|
| **macOS** | Fully supported |
| **Windows** | Planned |
| **Linux** | Planned |

On macOS, drag-out uses native pasteboard APIs to provide file URIs that work with all standard macOS apps.

## Temporary Files

When you start dragging a clip, mahpastes writes the clip data to a temporary file. These temp files are:

- Stored in the app's data directory under `clip_temp_files/`
- Reused if you drag the same clip again
- Cleaned up when mahpastes exits

You don't need to manage these files manually.

## Tips

- Drag images directly into email compose windows to attach them
- Drag text clips into editors to insert the content
- Drag multiple clips by selecting them first with bulk select, then using bulk actions to copy or download
