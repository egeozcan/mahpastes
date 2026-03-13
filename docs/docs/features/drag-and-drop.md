---
sidebar_position: 11
---

# Drag and Drop

Drag clips directly from mahpastes into other applications on your system.

## Overview

mahpastes supports dragging clips out of the app and dropping them into other programs. This works like dragging a file from Finder or Explorer -- the receiving app gets a real file it can open, attach, or process.

Supported drop targets include:
- **Finder / Explorer** -- drop to save as a file
- **Mail / Outlook** -- drop to attach
- **Slack / Discord** -- drop to share
- **Any app** that accepts file drops

:::note Folder Mode
Drag handles are hidden when folder mode is active. Switch back to normal gallery view to drag clips out.
:::

## How to Drag a Clip

Each clip card has a grip icon (drag handle) on the left side. The drag handle goes through a preparation sequence:

1. **Hover** over the drag handle -- the app first checks whether a temp file already exists from a previous hover. If not, a 1-second arming countdown begins (shown as a circular progress animation).
2. **Preparing** -- after the countdown, the app fetches the clip data from the backend and writes a temporary file to disk (shown as a spinner).
3. **Ready** -- the handle returns to the grip icon, indicating the clip is ready to drag.
4. **Drag** -- click and hold the handle, drag to the target app, release to drop.

If you click the drag handle before it reaches the **Ready** state, preparation starts immediately. Once a clip has been prepared, subsequent hovers skip the countdown and show the ready state right away (until the temp file's 60-minute lease expires).

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

| Platform | Status | Mechanism |
|----------|--------|-----------|
| **macOS** | Fully supported | Native drag via NSView.dragFile (CGo) |
| **Windows** | Fully supported | DataTransfer DownloadURL backed by an HTTP transfer handler |
| **Linux** | Planned | Not yet implemented |

### macOS

On macOS, dragging a clip triggers a native OS drag operation using `NSView.dragFile`. The app passes a `file://` URI pointing to the prepared temp file, so every standard macOS drop target (Finder, Mail, Slack, etc.) receives a real file reference. This is initiated directly from a mouse event for reliable behavior.

### Windows

On Windows, drag-out uses Chromium's `DownloadURL` DataTransfer type. Because Chromium rejects `file://` URIs in DownloadURL with a network error, the app serves the temp file over a local HTTP endpoint (`/transfer/{token}/{filename}`) with a one-time random token for authorization. The receiving app downloads the file from that URL, producing a standard file drop (CF_HDROP). Multiple DataTransfer types (`text/uri-list`, `DownloadURL`, `text/plain`, etc.) are set for broad compatibility.

### Linux

Linux drag-out is planned for a future release. A placeholder strategy (`linux-fileuri-v1`) exists in the codebase but is not yet functional.

## Temporary Files

Temp files created for drag-out (and Copy Path / Copy File) are stored in `clip_temp_files/` inside the app's data directory. They are:

- Reused if you drag the same clip again within the lease window
- Given a 60-minute lease, refreshed each time the file is accessed
- Pruned every 10 minutes -- stale files (past their lease) and orphaned files (whose clip was deleted) are removed
- Fully cleaned up when mahpastes exits

You don't need to manage these files manually.

## Tips

- Drag images directly into email compose windows to attach them
- Drag text clips into editors to insert the content
- You cannot drag multiple clips at once -- use [Bulk Actions](./bulk-actions.md) to copy or download several clips together
- If the drag handle stays in the spinner state for a long time, the clip data may be very large

## Related

- [Clipboard Management](./clipboard-management.md) -- other ways to retrieve clips
- [Bulk Actions](./bulk-actions.md) -- copy or download multiple clips
