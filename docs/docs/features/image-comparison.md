---
sidebar_position: 3
---

# Image Comparison

Compare two images side-by-side with interactive fade and slider modes. Perfect for before/after comparisons, design reviews, and spotting differences.

## Starting a Comparison

1. Select exactly two image clips using the checkboxes
2. The **Compare** button appears in the bulk action bar
3. Click **Compare** to open the comparison modal with both images loaded

![Image comparison](/img/screenshots/image-comparison.png)

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

A vertical divider reveals each image on its respective side.

**How it works:**
- Drag the vertical divider left or right
- Left side shows the first image, right side shows the second
- Click anywhere in the comparison area to reposition the divider (not just the handle)
- The position slider below also controls the divider
- Touch drag is supported

**Best for:**
- Pixel-perfect comparisons
- Spotting specific differences
- Before/after views

## Controls

### Zoom

| Control | Action |
|---------|--------|
| **Zoom In** button | Multiply zoom by 1.2x (max 500%) |
| **Zoom Out** button | Divide zoom by 1.2x (min 10%) |
| **Fit** button | Fit images to viewport (capped at 100%) |

The current zoom level is displayed as a percentage. Zoom is applied via CSS transform.

### Alignment

Position the images within the comparison viewport:
- **Horizontal**: Left, Center, or Right
- **Vertical**: Top, Middle, or Bottom

### Stretch

When images have different sizes:
- **Off**: Images display at natural size with `object-fit: contain`
- **On**: The bottom image is forced to 1000x1000px with `object-fit: fill`

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

### Common Workflow

1. Take screenshots of both states
2. Paste into mahpastes
3. Select both, click Compare
4. Start with fade mode to see overall changes
5. Switch to slider mode for precise inspection
6. Zoom into areas of interest
7. Close when done

## Defaults

- Mode: Fade
- Slider/opacity position: 50%
- Alignment: Center / Middle
- Stretch: Off

## Closing

Close the comparison modal by clicking the X button or clicking outside the comparison area. There is no keyboard shortcut to close the comparison modal.

## Limitations

- Only two images can be compared at once
- Slider mode has a vertical divider only (no horizontal split)
- Images are compared visually only (no diff calculation)
- No <span className="keyboard-key">Esc</span> key to close (use the X button or click outside)
- Very large images may affect performance

For advanced image differencing (pixel-level diff highlighting), consider specialized diff tools.
