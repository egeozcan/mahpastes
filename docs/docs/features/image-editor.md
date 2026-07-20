---
sidebar_position: 2
---

# Image Editor

Annotate and modify images directly within mahpastes. Supports highlights, shapes, text, freehand drawing, cropping, selections, anonymization, and transform operations -- all without leaving the app.

## Opening the Editor

1. Click the menu button (three dots) on any image clip and select **Edit**, or select **Edit** from the lightbox Actions menu
2. The editor opens full-screen with the image loaded on a canvas
3. Use the toolbar to select tools and options

:::note
Clicking an image preview opens the lightbox, not the editor. Use the context menu or lightbox Actions menu to open the editor.
:::

![Image editor](/img/screenshots/image-editor.png)

## Drawing Tools

### Brush Tool

Freehand drawing for annotations and highlights.

| Property | Options |
|----------|---------|
| Stroke width | 1--50 pixels |
| Color | Any color via picker |

**Tips:**
- Use thin strokes for underlining
- Use thick strokes for emphasis
- Hold steady for straight-ish lines

### Line Tool

Draw precise straight lines.

| Property | Options |
|----------|---------|
| Stroke width | 1--50 pixels |
| Color | Any color via picker |

**How to use:**
1. Click to set start point
2. Drag to endpoint
3. Release to complete

Hold <span className="keyboard-key">Shift</span> while dragging to snap the line to 45-degree increments.

### Arrow Tool

Draw a line with an arrowhead at the endpoint. The arrowhead scales proportionally with the stroke width.

| Property | Options |
|----------|---------|
| Stroke width | 1--50 pixels |
| Color | Any color via picker |

**How to use:**
1. Click to set start point
2. Drag to endpoint
3. Release to complete -- the arrowhead appears at the endpoint

Hold <span className="keyboard-key">Shift</span> while dragging to snap to 45-degree increments. While dragging, a dashed preview shows the arrow before you commit it.

### Rectangle Tool

Draw rectangles for highlighting areas.

| Property | Options |
|----------|---------|
| Stroke width | 1--50 pixels |
| Color | Any color via picker |
| Fill | Outline only |

While dragging, a dashed preview shows the shape before you commit it.

### Circle Tool

Draw circles from center.

| Property | Options |
|----------|---------|
| Stroke width | 1--50 pixels |
| Color | Any color via picker |
| Fill | Outline only |

**How to use:**
1. Click to set center
2. Drag outward to set radius
3. Release to complete

While dragging, a dashed preview shows the shape before you commit it.

### Text Tool

Add text labels and annotations.

| Property | Options |
|----------|---------|
| Font | Arial, sans-serif |
| Font size | Independent setting, default 16px |
| Color | Any color via picker |

**How to use:**
1. Click where you want text
2. A text input appears at that position
3. Type your annotation
4. Press <span className="keyboard-key">Enter</span> or click elsewhere to commit
5. Press <span className="keyboard-key">Esc</span> to cancel without committing

The font size is controlled by a separate input in the toolbar, independent of the brush size slider.

### Eraser Tool

Remove annotations by restoring the original image pixels.

| Property | Options |
|----------|---------|
| Size | 1--50 pixels |

:::tip
The eraser restores pixels from the original image (the state when you first opened the editor). It removes only your annotations -- it does not erase to transparency or destroy the underlying image.
:::

### Eyedropper Tool

Sample a color from any pixel on the canvas.

**How to use:**
1. Click on any pixel in the image
2. The sampled color is set as the current drawing color, and the color picker updates to match
3. The editor automatically switches back to your previous tool after sampling

This is useful when you want your annotations to match a color already present in the image.

## Selection and Transform Tools

### Select Tool

Create rectangular pixel selections that you can move, resize, duplicate, copy, paste, and delete.

**How to use:**
1. Click and drag to create a selection rectangle
2. The selected region is lifted from the canvas (the area underneath fills with white)
3. Drag inside the selection to move it, or drag the 8 handles to resize
4. Press <span className="keyboard-key">Enter</span> to commit the selection at its new position
5. Press <span className="keyboard-key">Esc</span> to cancel and restore the original

A marching-ants border shows the active selection boundary.

**Selection operations:**

| Action | How |
|--------|-----|
| Move | Drag inside the selection |
| Resize | Drag one of the 8 handles (corners or edge midpoints) |
| Constrain to square | Hold <span className="keyboard-key">Shift</span> while creating a selection |
| Duplicate | Hold <span className="keyboard-key">Alt</span> and drag to place a copy |
| Select all | <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">A</span> |
| Copy | <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">C</span> (internal clipboard) |
| Paste | <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">V</span> (pastes at center) |
| Delete | <span className="keyboard-key">Delete</span> (fills region with white) |

:::note
Copy and paste use an internal editor clipboard, separate from the system clipboard. You can copy a selection and paste it multiple times within the same editing session.
:::

### Crop Tool

Crop the image to a specific region with optional aspect ratio constraints and rotation.

**How to use:**
1. When you activate the Crop tool, an initial crop rectangle appears covering 80% of the canvas
2. Drag the 8 handles (corners and edge midpoints) to adjust the crop region
3. Drag inside the rectangle to reposition it
4. Click outside the rectangle to start a new crop region from scratch
5. Click **Confirm** (or press <span className="keyboard-key">Enter</span>) to apply the crop
6. Click **Cancel** to discard

**Crop features:**

| Feature | Description |
|---------|-------------|
| Aspect ratio presets | Free, 1:1, 4:3, 3:2, 16:9 |
| Swap orientation | Switch between landscape and portrait for the selected ratio |
| Rotation slider | Straighten the image before cropping |
| Rule-of-thirds grid | Composition guidelines shown inside the crop region |
| Dimensions label | The current crop width and height in pixels, displayed below the region |
| Marching ants | Animated border to distinguish the crop boundary from the image |

After confirming a crop, the canvas resizes to the cropped dimensions and the editor switches back to the Brush tool.

### Anonymize Tool

Obscure sensitive information by pixelating or blurring regions of the image.

**Modes:**
- **Brush mode** -- paint over areas freehand to anonymize them as you drag
- **Rectangle mode** -- drag to define a rectangular area, and the effect is applied when you release

**Effects:**
- **Pixelate** -- replaces the region with blocky pixelated blocks
- **Blur** -- applies a Gaussian-style blur to the region

Both effects add random noise to the result to prevent reversal.

The block size for pixelation and the blur radius are controlled by the brush size slider.

### Transform

Rotate and flip the entire image using the toolbar buttons.

| Operation | Shortcut | Description |
|-----------|----------|-------------|
| Rotate 90 CW | <span className="keyboard-key">R</span> | Rotate the entire canvas 90 degrees clockwise |
| Rotate 90 CCW | <span className="keyboard-key">Shift</span> + <span className="keyboard-key">R</span> | Rotate the entire canvas 90 degrees counter-clockwise |
| Flip horizontal | Toolbar button | Mirror the image left-to-right |
| Flip vertical | Toolbar button | Mirror the image top-to-bottom |

Rotation changes the canvas dimensions (width and height are swapped). The zoom level adjusts to fit the new dimensions automatically.

### Zoom and Pan

Control the view of the canvas without modifying the image.

| Action | Method |
|--------|--------|
| Zoom in/out | <span className="keyboard-key">Ctrl</span>/<span className="keyboard-key">Cmd</span> + scroll wheel, or toolbar buttons |
| Pan | Hold <span className="keyboard-key">Space</span> and drag |
| Zoom to fit | Toolbar button (auto-fits on open) |
| Zoom to 100% | Toolbar button |

The current zoom percentage is displayed in the toolbar.

## Toolbar Options

### Color Picker

Click the color swatch to open the browser's native color picker. Enter hex values or use the visual picker to choose any color. The default drawing color is a warm dark gray (`#44403c`).

### Stroke Width

Adjust line thickness:
- Slider control from 1 to 50 pixels
- Affects brush, eraser, line, arrow, rectangle, circle, and anonymize tools
- Press <span className="keyboard-key">]</span> to increase by 2, <span className="keyboard-key">[</span> to decrease by 2

### Font Size

When the Text tool is active, a font size input appears in the toolbar. The default font size is 16 pixels. This control is independent of the stroke width slider.

### Opacity

Control transparency of annotations:
- Slider from 0% to 100%
- Affects all drawing tools
- Press <span className="keyboard-key">Shift</span> + <span className="keyboard-key">]</span> to increase by 10%, <span className="keyboard-key">Shift</span> + <span className="keyboard-key">[</span> to decrease by 10%

### Zoom and Pan

Navigate around the canvas, especially at high zoom levels.

| Control | Action |
|---------|--------|
| **Zoom In** button | Increase zoom by 10% |
| **Zoom Out** button | Decrease zoom by 10% |
| **Fit** button | Fit the image to the editor viewport (capped at 100%) |
| **100%** button | View at actual pixel size |
| <span className="keyboard-key">Ctrl</span>/<span className="keyboard-key">Cmd</span> + scroll | Zoom in/out centered on the cursor |
| Hold <span className="keyboard-key">Space</span> + drag | Pan the canvas (works with any tool active) |

The zoom range is 10% to 800%. The current zoom level is displayed as a percentage in the toolbar.

### Undo/Redo

Fix mistakes with full undo support:

| Action | Shortcut |
|--------|----------|
| Undo | <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Z</span> |
| Redo | <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Shift</span> + <span className="keyboard-key">Z</span> or <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Y</span> |
| Save as new clip | <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">S</span> |

The editor maintains up to **50 undo steps** (with a 100 MB memory ceiling). The redo stack is cleared when you make a new edit. Undo and redo fully support operations that change the canvas dimensions, such as crop and rotate.

## Keyboard Shortcuts

Every tool and common action has a keyboard shortcut.

### Tool Shortcuts

| Shortcut | Tool |
|----------|------|
| <span className="keyboard-key">B</span> | Brush |
| <span className="keyboard-key">E</span> | Eraser |
| <span className="keyboard-key">L</span> | Line |
| <span className="keyboard-key">W</span> | Arrow |
| <span className="keyboard-key">U</span> | Rectangle |
| <span className="keyboard-key">O</span> | Circle |
| <span className="keyboard-key">T</span> | Text |
| <span className="keyboard-key">I</span> | Eyedropper |
| <span className="keyboard-key">V</span> | Select |
| <span className="keyboard-key">X</span> | Anonymize |
| <span className="keyboard-key">C</span> | Crop |

### Action Shortcuts

| Shortcut | Action |
|----------|--------|
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Z</span> | Undo |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Shift</span> + <span className="keyboard-key">Z</span> | Redo |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Y</span> | Redo (alternative) |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">S</span> | Save as new clip |
| <span className="keyboard-key">R</span> | Rotate 90 CW |
| <span className="keyboard-key">Shift</span> + <span className="keyboard-key">R</span> | Rotate 90 CCW |
| <span className="keyboard-key">]</span> | Increase brush size |
| <span className="keyboard-key">[</span> | Decrease brush size |
| <span className="keyboard-key">Shift</span> + <span className="keyboard-key">]</span> | Increase opacity |
| <span className="keyboard-key">Shift</span> + <span className="keyboard-key">[</span> | Decrease opacity |
| <span className="keyboard-key">Enter</span> | Confirm crop or selection |
| <span className="keyboard-key">Esc</span> | Cancel selection / Close editor |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">A</span> | Select all (Select tool) |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">C</span> | Copy selection (Select tool) |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">V</span> | Paste selection (Select tool) |
| <span className="keyboard-key">Delete</span> | Delete selection (Select tool) |
| Hold <span className="keyboard-key">Space</span> + drag | Pan canvas |
| <span className="keyboard-key">Ctrl</span>/<span className="keyboard-key">Cmd</span> + scroll | Zoom |

## Workflow Example

### Annotating a Screenshot

1. **Paste** your screenshot into mahpastes
2. Open the context menu and select **Edit**
3. Press <span className="keyboard-key">U</span> for the **Rectangle** tool and draw around the area of interest
4. Press <span className="keyboard-key">T</span> for the **Text** tool and add a label
5. **Adjust colors** if needed for visibility
6. **Click Save** to overwrite or **Save As** to create a new annotated clip

### Creating a Bug Report

1. Capture the bug as a screenshot
2. Open in editor
3. Use **red rectangles** (<span className="keyboard-key">U</span>) to highlight the problem area
4. Add **text annotations** (<span className="keyboard-key">T</span>) explaining the issue
5. Use **arrows** (<span className="keyboard-key">W</span>) to point at specifics
6. Save and copy path to attach to bug report

### Redacting Sensitive Information

1. Open the image in the editor
2. Press <span className="keyboard-key">X</span> for the **Anonymize** tool
3. Choose **Pixelate** or **Blur** from the context toolbar
4. Switch between **Brush** mode (paint freehand) or **Rectangle** mode (drag a box)
5. Anonymize names, emails, or other sensitive content
6. Save -- the anonymization cannot be reversed

## Saving Changes

### Save (Overwrite)

Click **Save** to overwrite the original clip with your edits. The original clip's content is replaced in place. The filename remains the same.

### Save as New Clip

Click **Save As** (or press <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">S</span>) to create a new clip with your annotations. The original clip is preserved unchanged, and the new clip is saved with `_edited` appended to the filename.

The editor preserves the original image format when saving:
- **JPEG** originals are saved as JPEG
- **WebP** originals are saved as WebP
- All other formats (PNG, GIF, BMP, etc.) are saved as PNG

### Cancel

Click the close button (X) or press <span className="keyboard-key">Esc</span> to close the editor. If the image has changed since you opened it, mahpastes asks you to confirm before discarding the unsaved changes. Undoing all edits back to the original image lets you close without a prompt.

## Tips

### Visibility

- Use contrasting colors against the image
- Use the Eyedropper (<span className="keyboard-key">I</span>) to match colors already in the image
- Increase stroke width for small images

### Annotation Style

- Keep annotations minimal and focused
- Use consistent colors for similar annotations
- Red for errors/problems, green for correct, blue for information

### Performance

- Very large images may be slower to edit
- Complex annotations (many shapes) are still performant
- The undo stack is capped at 100 MB of memory to keep things responsive

## Touch Support

The editor supports touch drawing on touchscreen devices. Touch events are mapped to the same drawing tools. Shift-snap for the Line and Arrow tools is not available via touch.

## Limitations

- **Canvas max 4000px**: Images are downscaled to a maximum of 4000 pixels on either dimension
- **No layers**: Annotations are flattened on save
- **Raster only**: SVG images are rasterized for editing
- **Single text line**: The Text tool commits a single line per placement -- for multiline text, place multiple text annotations

For complex image editing needs, use a dedicated image editor and import the result into mahpastes.
