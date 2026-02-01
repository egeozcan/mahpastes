---
sidebar_position: 2
---

# Image Editor

Annotate and modify images directly within mahpastes. Add highlights, shapes, text, and drawings without leaving the app.

## Opening the Editor

1. Click the edit button on any image clip
2. The editor opens in a modal with the image loaded
3. Use the toolbar to select tools and options

## Tools

### Brush Tool

Freehand drawing for annotations and highlights.

| Property | Options |
|----------|---------|
| Shortcut | <span className="keyboard-key">B</span> |
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
| Shortcut | <span className="keyboard-key">L</span> |
| Stroke width | 1-50 pixels |
| Color | Any color via picker |

**How to use:**
1. Click to set start point
2. Drag to endpoint
3. Release to complete

### Rectangle Tool

Draw rectangles for highlighting areas.

| Property | Options |
|----------|---------|
| Shortcut | <span className="keyboard-key">R</span> |
| Stroke width | 1-50 pixels |
| Color | Any color via picker |
| Fill | Outline only |

**Common uses:**
- Highlight UI elements in screenshots
- Create bounding boxes
- Draw attention to specific areas

### Circle Tool

Draw circles and ellipses.

| Property | Options |
|----------|---------|
| Shortcut | <span className="keyboard-key">C</span> |
| Stroke width | 1-50 pixels |
| Color | Any color via picker |
| Fill | Outline only |

**How to use:**
1. Click to set center
2. Drag outward to set size
3. Release to complete

### Text Tool

Add text labels and annotations.

| Property | Options |
|----------|---------|
| Shortcut | <span className="keyboard-key">T</span> |
| Font size | Adjustable |
| Color | Any color via picker |

**How to use:**
1. Click where you want text
2. Type your annotation
3. Click elsewhere or press Enter to confirm

### Eraser Tool

Remove annotations from the canvas.

| Property | Options |
|----------|---------|
| Shortcut | <span className="keyboard-key">E</span> |
| Size | Adjustable |

**Note:** The eraser removes annotations only, not the original image.

## Toolbar Options

### Color Picker

Click the color swatch to open the color picker:
- Choose from preset colors
- Enter hex values for precise colors
- Recently used colors are remembered

### Stroke Width

Adjust line thickness:
- Slider control for fine adjustment
- Affects brush, line, rectangle, and circle tools

### Undo/Redo

Fix mistakes with full undo support:

| Action | Shortcut |
|--------|----------|
| Undo | <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Z</span> |
| Redo | <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Y</span> |

The editor maintains up to **50 undo steps**.

## Workflow Example

### Annotating a Screenshot

1. **Paste** your screenshot into mahpastes
2. **Click edit** to open the editor
3. **Select Rectangle** (<span className="keyboard-key">R</span>) and draw around the area of interest
4. **Select Text** (<span className="keyboard-key">T</span>) and add a label
5. **Adjust colors** if needed for visibility
6. **Click Save** to update the clip

### Creating a Bug Report

1. Capture the bug as a screenshot
2. Open in editor
3. Use **red rectangles** to highlight the problem area
4. Add **text annotations** explaining the issue
5. Use **arrows** (draw with Line tool) to point at specifics
6. Save and copy path to attach to bug report

## Saving Changes

### Save to Clip

Click **Save** to update the original clip with your annotations. The annotated version replaces the original.

### Cancel

Click **Cancel** or press <span className="keyboard-key">Esc</span> to discard changes and close the editor.

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

## Limitations

- **No layers**: Annotations are flattened on save
- **No selection**: Can't move or resize annotations after drawing
- **No crop**: Use external tools for cropping before importing
- **Raster only**: SVG images are rasterized for editing

For complex image editing needs, use a dedicated image editor and import the result into mahpastes.
