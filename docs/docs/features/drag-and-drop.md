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

Each clip card has a grip icon (drag handle) on the left side. The drag handle goes through a preparation sequence:

1. **Hover** over the drag handle -- a 1-second arming countdown begins (shown as a progress animation)
2. **Preparing** -- the app fetches the clip data from the backend and writes a temporary file (shown as a spinner)
3. **Ready** -- the handle returns to the grip icon, indicating the clip is ready to drag
4. **Drag** -- click and hold the handle, drag to the target app, release to drop

The preparation happens automatically on hover. If the clip was already prepared from a previous hover, the ready state is immediate.

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

Temp files created for drag-out (and Copy Path/Copy File) are stored in `clip_temp_files/` inside the app's data directory. They are:

- Reused if you drag the same clip again
- Given a 60-minute lease, after which they are pruned
- Pruned every 10 minutes for stale or orphaned files
- Fully cleaned up when mahpastes exits

You don't need to manage these files manually.

## Tips

- Drag images directly into email compose windows to attach them
- Drag text clips into editors to insert the content
- Drag multiple clips by selecting them first with bulk select, then using bulk actions to copy or download
