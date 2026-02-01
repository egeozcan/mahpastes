---
sidebar_position: 3
---

# Image Comparison

Compare two images side-by-side with interactive fade and slider modes. Perfect for before/after comparisons, design reviews, and spotting differences.

## Starting a Comparison

1. Select two image clips (hold <span className="keyboard-key">Shift</span> and click)
2. Click the **Compare** button that appears
3. The comparison modal opens with both images loaded

Alternatively:
1. Open an image in the viewer
2. Click the compare icon
3. Select the second image to compare

## Comparison Modes

### Fade Mode

Smoothly transition between two images with opacity control.

**How it works:**
- A slider controls the blend between images
- 0% = First image only
- 100% = Second image only
- Any position in between = Blended view

**Best for:**
- Subtle color changes
- Overall composition differences
- A/B testing designs

### Slider Mode

A movable divider reveals each image on its respective side.

**How it works:**
- Drag the vertical or horizontal divider
- Left/top shows the first image
- Right/bottom shows the second image
- The divider can be positioned anywhere

**Best for:**
- Pixel-perfect comparisons
- Spotting specific differences
- Before/after views

## Controls

### Zoom

| Control | Action |
|---------|--------|
| <span className="keyboard-key">+</span> | Zoom in |
| <span className="keyboard-key">-</span> | Zoom out |
| <span className="keyboard-key">0</span> | Reset to fit |
| Mouse wheel | Zoom in/out |
| Zoom slider | Precise zoom control |

### Pan

When zoomed in:
- Click and drag to pan
- Both images pan together (locked)

### Alignment

Toggle between:
- **Horizontal split** — Images side by side
- **Vertical split** — Images top and bottom

### Stretch

When images have different sizes:
- **Off**: Images maintain aspect ratio
- **On**: Images stretch to fill the same area

:::tip Comparing Different-Sized Images
Enable Stretch mode when comparing images of different dimensions. This aligns them spatially for easier comparison.
:::

## Use Cases

### Design Review

1. Export two versions of a design
2. Import both into mahpastes
3. Compare to review changes
4. Use slider mode to check alignment

### Bug Verification

1. Screenshot before the fix
2. Screenshot after the fix
3. Compare to verify the bug is resolved
4. Use fade mode to highlight the change

### Photo Editing

1. Original photo
2. Edited version
3. Compare to judge the edit quality
4. Zoom in to check details

### A/B Testing Designs

1. Variant A screenshot
2. Variant B screenshot
3. Switch between with fade mode
4. Present to stakeholders

## Tips

### Getting the Best Comparison

- **Same dimensions**: Crop images to the same size before importing
- **Same position**: Screenshot the same area for accurate comparison
- **High zoom**: Check fine details at high zoom levels
- **Both modes**: Use fade for overview, slider for specifics

### Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| <span className="keyboard-key">←</span> / <span className="keyboard-key">→</span> | Move slider (slider mode) |
| <span className="keyboard-key">+</span> / <span className="keyboard-key">-</span> | Zoom in/out |
| <span className="keyboard-key">0</span> | Reset zoom |
| <span className="keyboard-key">Esc</span> | Close comparison |

### Common Workflow

1. Take screenshots of both states
2. Paste into mahpastes
3. Select both, click Compare
4. Start with fade mode to see overall changes
5. Switch to slider mode for precise inspection
6. Zoom into areas of interest
7. Close when done

## Limitations

- Only two images can be compared at once
- Images are compared visually only (no diff calculation)
- Very large images may affect performance

For advanced image differencing (pixel-level diff highlighting), consider specialized diff tools.
