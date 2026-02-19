---
sidebar_position: 2
---

# Image Editor

Annotate and modify images directly within mahpastes. Add highlights, shapes, text, and drawings without leaving the app.

## Opening the Editor

1. Click the menu button (three dots) on any image clip and select **Edit**, or select **Edit** from the lightbox Actions menu
2. The editor opens full-screen with the image loaded on a canvas
3. Use the toolbar to select tools and options

:::note
Clicking an image preview opens the lightbox, not the editor. Use the context menu or lightbox Actions menu to open the editor.
:::

![Image editor](/img/screenshots/image-editor.png)

## Tools

### Brush Tool

Freehand drawing for annotations and highlights.

| Property | Options |
|----------|---------|
| Stroke width | 1-50 pixels |
| Color | Any color via picker |

**Tips:**
- Use thin strokes for underlining
- Use thick strokes for emphasis
- Hold steady for straight-ish lines

### Line Tool

Draw precise straight lines.

| Property | Options |
|----------|---------|
| Stroke width | 1-50 pixels |
| Color | Any color via picker |

**How to use:**
1. Click to set start point
2. Drag to endpoint
3. Release to complete

Hold <span className="keyboard-key">Shift</span> while dragging to snap the line to 45-degree increments.

### Rectangle Tool

Draw rectangles for highlighting areas.

| Property | Options |
|----------|---------|
| Stroke width | 1-50 pixels |
| Color | Any color via picker |
| Fill | Outline only |

While dragging, a dashed preview shows the shape before you commit it.

### Circle Tool

Draw circles from center.

| Property | Options |
|----------|---------|
| Stroke width | 1-50 pixels |
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
| Font | Arial |
| Font size | Brush size x 2 pixels |
| Color | Any color via picker |

**How to use:**
1. Click where you want text
2. A text input appears at that position
3. Type your annotation
4. Press <span className="keyboard-key">Enter</span> or click elsewhere to commit
5. Press <span className="keyboard-key">Esc</span> to cancel without committing

### Eraser Tool

Erase pixels from the canvas.

| Property | Options |
|----------|---------|
| Size | 1-50 pixels |

:::warning
The eraser uses `destination-out` compositing. It erases all pixels it touches, including the original image. It erases to transparency, not to the original image underneath.
:::

## Toolbar Options

### Color Picker

Click the color swatch to open the browser's native color picker. Enter hex values or use the visual picker to choose any color.

### Stroke Width

Adjust line thickness:
- Slider control for fine adjustment
- Affects brush, line, rectangle, and circle tools

### Opacity

Control transparency of annotations:
- Slider from 0% to 100%
- Affects all drawing tools

### Undo/Redo

Fix mistakes with full undo support:

| Action | Shortcut |
|--------|----------|
| Undo | <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Z</span> |
| Redo | <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Shift</span> + <span className="keyboard-key">Z</span> or <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Y</span> |
| Save | <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">S</span> |

The editor maintains up to **50 undo steps**. The redo stack is cleared when you make a new edit.

## Workflow Example

### Annotating a Screenshot

1. **Paste** your screenshot into mahpastes
2. Open the context menu and select **Edit**
3. **Select Rectangle** and draw around the area of interest
4. **Select Text** and add a label
5. **Adjust colors** if needed for visibility
6. **Click Save As** to create a new annotated clip

### Creating a Bug Report

1. Capture the bug as a screenshot
2. Open in editor
3. Use **red rectangles** to highlight the problem area
4. Add **text annotations** explaining the issue
5. Use **arrows** (draw with Line tool) to point at specifics
6. Save and copy path to attach to bug report

## Saving Changes

### Save as New Clip

Click **Save As** (or press <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">S</span>) to create a new clip with your annotations. The original clip is preserved unchanged, and an edited copy is saved with `_edited` appended to the filename.

The saved image is always PNG, regardless of the original format.

### Cancel

Click **Cancel** (X button) or press <span className="keyboard-key">Esc</span> to discard changes and close the editor.

## Tips and Best Practices

### Visibility

- Use contrasting colors against the image
- Add a white or black outline effect for text over busy backgrounds
- Increase stroke width for small images

### Annotation Style

- Keep annotations minimal and focused
- Use consistent colors for similar annotations
- Red for errors/problems, green for correct, blue for information

### Performance

- Very large images may be slower to edit
- Complex annotations (many shapes) are still performant
- Save periodically for complex edits

## Touch Support

The editor supports touch drawing on touchscreen devices. Touch events are mapped to the same drawing tools. Shift-snap for the line tool is not available via touch.

## Limitations

- **Canvas max 2000px**: Images are downscaled to a maximum of 2000 pixels on either dimension
- **Always saves as PNG**: Regardless of original format (JPG, WebP, etc.)
- **No layers**: Annotations are flattened on save
- **No selection**: Can't move or resize annotations after drawing
- **No crop**: Use external tools for cropping before importing
- **Raster only**: SVG images are rasterized for editing
- **No tool shortcuts**: Tools must be selected by clicking the toolbar buttons

For complex image editing needs, use a dedicated image editor and import the result into mahpastes.
