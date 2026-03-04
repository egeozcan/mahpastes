---
sidebar_position: 9
---

# Bulk Actions

Select and operate on multiple clips at once. Archive, download, or delete many clips efficiently.

## Selecting Multiple Clips

### Checkbox Selection

Each clip card has a checkbox that appears on hover:

1. Hover over a clip to reveal the checkbox (top-right corner)
2. Click the checkbox to select/deselect
3. Use the **Select All** checkbox to select all visible clips

Selected clips show a checkmark indicator.

![Bulk actions toolbar](/img/screenshots/bulk-actions.png)

### Selection Indicator

When clips are selected:
- A count appears showing "X selected"
- A bulk action bar appears at the bottom
- A **Cancel** button lets you clear the selection

## Available Bulk Actions

### Copy

Copy all selected clips as files to the system clipboard (macOS and Windows):

1. Select clips
2. Click **Copy** in the action bar
3. All selected clips are placed on the system clipboard as files

Press <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">C</span> while clips are selected as an alternative.

### Tag

Apply or remove tags from all selected clips:

1. Select clips
2. Click **Tag** in the action bar
3. Check or uncheck tags in the popover
4. Create a new tag and apply it immediately from the same popover

### Compare

Compare two images side by side:

1. Select exactly **two** image clips
2. The **Compare** button appears in the action bar
3. Click to open the comparison modal

This button only appears when exactly two clips with `image/*` content types are selected.

### Archive

Move all selected clips to the archive:

1. Select clips
2. Click **Archive** in the action bar
3. All selected clips move to Archive

If clips are already archived (in Archive view), this becomes **Restore**.

### Download as ZIP

Export all selected clips as a single ZIP file:

1. Select clips
2. Click **Download** in the action bar
3. Choose save location in the dialog
4. A ZIP file is created with all clip contents

ZIP file contents:
- Each clip as a separate file
- Filenames: `id_originalname.ext` or `clip_id.ext`
- Preserves original file formats

### Set Expiration

Apply an expiration to all selected clips:

1. Select clips
2. Click **Set Expiration** in the action bar (or press <span className="keyboard-key">X</span>)
3. Choose a preset (15m, 1h, 6h, 24h, 7d)
4. The expiration is applied to all selected clips

### Clear Expiry

Remove expiration from all selected clips:

1. Select clips that have active expirations
2. Click **Clear Expiry** in the action bar
3. All selected clips revert to no expiry

### Delete

Remove all selected clips permanently:

1. Select clips
2. Click **Delete** in the action bar
3. Confirm the deletion
4. All selected clips are permanently removed

:::warning Permanent Deletion
Bulk delete cannot be undone. Make sure you've selected the right clips before confirming.
:::

## Tips

- Use Select All to select all visible clips, Cancel to clear
- Double-check your selection before deleting -- delete is permanent
- Consider archiving instead of deleting if you might need clips later
- For large exports, ZIP creation may take a moment

## Related

- [Image Comparison](./image-comparison.md) -- compare two selected images
- [Tags](./tags.md) -- bulk tagging workflow
- [Auto-Delete](./auto-delete.md) -- bulk expiration actions
- [Keyboard Shortcuts](/getting-started/keyboard-shortcuts) -- shortcut keys for bulk operations
