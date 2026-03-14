---
sidebar_position: 9
---

# Bulk Actions

Select and operate on multiple clips at once. Archive, download, or delete many clips efficiently.

## Selecting Multiple Clips

### Checkbox Selection

Each clip card has a checkbox that appears on hover:

1. Hover over a clip to reveal the checkbox (top-right corner)
2. Click the checkbox to select or deselect
3. Hold <span className="keyboard-key">Shift</span> and click a second checkbox to select the entire range between the two
4. Use the **Select All** checkbox in the toolbar to select all visible clips

Selected clips show a ring highlight and a checkmark indicator.

![Bulk actions toolbar](/img/screenshots/bulk-actions.png)

### Selection Indicator

When clips are selected:
- A count appears showing "N selected"
- A bulk action bar slides up at the bottom of the window
- A **Cancel** button lets you clear the selection (or press <span className="keyboard-key">Esc</span>)

## Available Bulk Actions

### Copy

Copy all selected clips as files to the system clipboard:

1. Select clips
2. Click **Copy** in the action bar (or press <span className="keyboard-key">C</span>)
3. All selected clips are placed on the system clipboard as file references

You can also press <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">C</span> as an alternative.

:::note Platform Support
Copy as files is supported on macOS (via NSPasteboard) and Windows (via PowerShell). On Linux, this action is not available.
:::

### Tag

Apply or remove tags from all selected clips:

1. Select clips
2. Click **Tag** in the action bar (or press <span className="keyboard-key">T</span>)
3. Check or uncheck tags in the popover
4. Create a new tag and apply it immediately from the same popover

Tag tree exclusivity is enforced per clip -- adding a tag from one tree automatically removes any existing tag in the same tree. See [Tags](./tags.md) for details on tree exclusivity.

### Compare

Compare two images side by side:

1. Select exactly **two** image clips
2. The **Compare** button appears in the action bar
3. Click to open the comparison modal

This button only appears when exactly two clips with `image/*` content types are selected.

### Archive

Move all selected clips to the archive:

1. Select clips
2. Click **Archive** in the action bar (or press <span className="keyboard-key">E</span>)
3. All selected clips move to Archive

When you are viewing the Archive, this button changes to **Restore** and moves selected clips back to the main gallery.

:::warning Bulk Restore Bug
The bulk archive operation currently always sets clips to archived (`is_archived = 1`) regardless of context. Clicking **Restore** in the archive view re-archives the selected clips instead of restoring them. Use single-clip restore via the context menu as a workaround.
:::

### Download as ZIP

Export all selected clips as a single ZIP file:

1. Select clips
2. Click **Download** in the action bar (or press <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">D</span>)
3. Choose a save location in the native file dialog
4. A ZIP file is created containing all clip contents

The default filename is `clips_YYYYMMDDHHMMSS.zip`. Inside the archive:
- Clips with an original filename are stored as `{id}_{originalname}` (e.g. `42_photo.png`)
- Clips without a filename are stored as `clip_{id}.{ext}`, where the extension is derived from the MIME type

### Set Expiration

Apply an expiration to all selected clips:

1. Select clips
2. Click **Set Expiration** in the action bar (or press <span className="keyboard-key">X</span>)
3. Choose a preset from the popover (15m, 1h, 6h, 24h, 7d)
4. The expiration timer starts immediately for all selected clips

See [Auto-Delete](./auto-delete.md) for details on how expiration works.

### Clear Expiry

Remove expiration from all selected clips:

1. Select clips that have active expirations
2. Click **Clear Expiry** in the action bar
3. All selected clips revert to no expiry

The **Clear Expiry** button only appears when at least one selected clip has an active expiration timer.

### Delete

Remove all selected clips permanently:

1. Select clips
2. Click **Delete** in the action bar (or press <span className="keyboard-key">D</span>)
3. Confirm the deletion
4. All selected clips are permanently removed

:::warning Permanent Deletion
Bulk delete cannot be undone. Make sure you've selected the right clips before confirming.
:::

## Keyboard Shortcuts

When clips are selected, these shortcuts are available:

| Shortcut | Action |
|----------|--------|
| <span className="keyboard-key">C</span> | Copy selected as files |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">C</span> | Copy selected as files |
| <span className="keyboard-key">D</span> | Delete selected |
| <span className="keyboard-key">E</span> | Archive / Restore selected |
| <span className="keyboard-key">T</span> | Open tag popover |
| <span className="keyboard-key">X</span> | Set expiration |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">D</span> | Download selected as ZIP |
| <span className="keyboard-key">Esc</span> | Clear selection |

## Tips

- Use <span className="keyboard-key">Shift</span>-click to select a range of consecutive clips quickly
- Double-check your selection before deleting -- delete is permanent
- Consider archiving instead of deleting if you might need clips later
- For large exports, ZIP creation may take a moment

## Related

- [Image Comparison](./image-comparison.md) -- compare two selected images
- [Tags](./tags.md) -- bulk tagging workflow
- [Auto-Delete](./auto-delete.md) -- bulk expiration actions
- [Keyboard Shortcuts](../getting-started/keyboard-shortcuts) -- shortcut keys for bulk operations
