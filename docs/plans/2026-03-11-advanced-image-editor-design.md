# Advanced Image Editor Design

## Overview

Upgrade the image editor from a lightweight annotation tool to a capable general-purpose editor with crop, selection/move, anonymize, zoom/pan, and additional drawing tools. Supports both screenshot annotation and quick photo editing workflows.

## Current State

The editor has: brush, line, rectangle, circle, text, eraser tools. Dual canvas (main + overlay for previews). Undo/redo via dataURL stack (max 50). PNG-only output. Max 2000px canvas. No zoom, no crop, no selection, no move, no anonymize.

## Architecture

### Modular Tool System

Split `editor.js` (~630 lines) into modules under `frontend/js/editor/`:

```
frontend/js/editor/
├── editor-core.js        # Canvas setup, state management, tool registry, undo/redo
├── editor-tools.js       # Tool base pattern, shared tool utilities
├── tool-brush.js         # Brush + eraser (share logic, differ in compositing)
├── tool-shapes.js        # Line, rectangle, circle (share preview pattern)
├── tool-text.js          # Text placement with dedicated font size control
├── tool-crop.js          # Crop with presets + rotate/straighten
├── tool-select.js        # Rectangular pixel selection, move, resize, copy/paste
├── tool-anonymize.js     # Pixelate + blur (brush and rectangle modes)
├── tool-zoom.js          # Zoom/pan state + controls
└── tool-arrow.js         # Arrow drawing
```

Each tool implements a common interface:

- `activate()` / `deactivate()` — setup/teardown when tool selected
- `onMouseDown/Move/Up()` — input handling
- `onKeyDown()` — tool-specific shortcuts
- `getCursor()` — cursor style for this tool
- `renderOverlay()` — draw on overlay canvas (previews, handles, crop guides)

`editor-core.js` manages: active tool switching, canvas initialization, undo/redo stack, zoom/pan transform state, and coordinate translation between screen space and canvas space.

The existing `editor.js` becomes a thin wrapper that imports these modules and wires them to the DOM. The modal open/close lifecycle in `modals.js` stays unchanged.

### Single-Layer Approach

No layer support. Undo history is the mechanism for reverting changes. All operations bake directly into the canvas.

## Zoom & Pan

CSS `transform: scale() translate()` on the canvas container — not canvas re-rendering. Canvas pixels stay at native resolution.

**Controls:**
- Scroll wheel — zoom centered on cursor
- Space + drag — pan (any tool)
- Pinch gesture — trackpad zoom
- Toolbar buttons — fit to screen, 100%, zoom in/out
- Zoom indicator — shows percentage, clickable for exact value

**Range:** 10% to 800%.

**Coordinate translation:** All tools call `screenToCanvas(x, y)` in `editor-core.js` which accounts for zoom level and pan offset, replacing the existing `getCanvasCoordinates()`.

**Canvas container:** `overflow: hidden` with canvas positioned absolutely. Pan offset clamped to prevent losing the canvas off-screen.

## Crop Tool

**Workflow:**
1. Select crop tool — overlay darkens entire canvas
2. Drag to define crop region — selected area shows at full brightness
3. Adjust: drag edges/corners to resize, drag inside to reposition
4. Optional: choose aspect ratio preset from contextual toolbar dropdown
5. Optional: rotate/straighten via slider (-45° to +45°) — image rotates behind axis-aligned crop frame
6. Confirm (Enter / checkmark) or cancel (Escape / X)

**Aspect ratio presets:** Free, 1:1, 4:3, 3:2, 16:9, plus swap button for orientation flip.

**Rotation:** Bilinear interpolation. Grid overlay during rotation for horizon alignment. Applied to full image before cropping.

**Visuals:**
- Dark overlay outside crop region (`rgba(28, 25, 23, 0.6)`)
- Rule-of-thirds grid inside crop region
- Corner/edge handles: small white squares with stone-800 border
- Dimensions label near crop box in `text-[10px]` micro style

**Execution:** New canvas at crop dimensions, draws relevant portion, replaces editor canvas, saves undo state. Canvas size actually changes.

## Selection Tool

**Workflow:**
1. Drag rectangle on canvas to define selection
2. Marching ants animated border around selected region
3. Actions on selection:
   - **Move** — drag inside to reposition pixels (leaves white behind)
   - **Resize** — drag corner/edge handles to scale
   - **Copy** — Ctrl/Cmd+C to internal clipboard
   - **Paste** — Ctrl/Cmd+V places as new floating selection
   - **Delete** — Delete/Backspace fills with white
4. Click outside to commit (bakes into canvas, saves undo state)
5. Escape to cancel and restore original

**Implementation:**
- `getImageData()` captures selected pixels on creation
- Selected region cleared on main canvas (filled white)
- Captured pixels drawn on overlay canvas at current position
- Move/resize updates overlay position/scale
- Commit draws overlay content onto main canvas

**Handles:** Same style as crop — white squares, stone-800 border, corners and edge midpoints.

**Keyboard modifiers:**
- Shift while dragging: constrain to square
- Shift while resizing: maintain aspect ratio from edges
- Alt/Option while moving: duplicate (copy+move)
- Ctrl/Cmd+A: select all

## Anonymize Tool

**Two modes toggled in contextual toolbar:**

**Brush mode:** Paint over areas — applies pixelation or blur under the cursor instead of drawing color. For irregular shapes and quick text scrubbing.

**Rectangle mode:** Drag rectangle, entire region anonymized on mouse release. For precise blocks — faces, address fields.

**Effect types (toggle):**
- **Pixelate** — grid of solid-color blocks. Block size controlled by brush size slider.
- **Blur** — Gaussian blur via `ctx.filter = 'blur(Npx)'`. Radius controlled by brush size slider.

**Visual feedback:**
- Brush mode: circle cursor with crosshatch/grid pattern
- Rectangle mode: real-time preview of effect on overlay canvas during drag

**Implementation:**
- `getImageData()` on target region
- Pixelate: iterate in block-sized steps, average each block's color, fill
- Blur: draw region to offscreen canvas with `ctx.filter`, draw back
- Write processed data back with `putImageData()` or `drawImage()`

## Arrow Tool

Works like the line tool with an arrowhead at the endpoint. Arrowhead size scales with brush size. Shift+snap for 45-degree angles. Same overlay preview pattern as other shapes.

## Image Rotate/Flip

Toolbar buttons: rotate 90° CW, rotate 90° CCW, flip horizontal, flip vertical. Creates new canvas at appropriate dimensions (width/height swap for 90°), redraws, replaces editor canvas, saves undo state.

## Eyedropper

Click to pick pixel color as current drawing color. Shows magnified preview circle around cursor. Auto-switches back to previously active tool after picking.

## Text Tool Enhancement

Replace brush-size-based font sizing with a dedicated font size input (8-200px) that appears in the contextual toolbar when text tool is active. Brush size slider hides, font size input takes its place.

## Output Format Preservation

Track original clip content type. On save: JPEG input saves as JPEG (`canvas.toDataURL('image/jpeg', 0.92)`), PNG stays PNG, WebP stays WebP (JPEG fallback if browser lacks WebP encoding support). Filename extension updates accordingly.

## Toolbar Layout

Two-row toolbar grouped by function:

```
Row 1 (tools):
[Select] [Crop]  |  [Brush] [Eraser]  |  [Line] [Arrow] [Rect] [Circle]  |  [Text] [Anonymize]  |  [Eyedropper]

Row 2 (properties + actions):
[Color] [Opacity slider] [Size slider]  |  [Undo] [Redo]  |  [Rot CW] [Rot CCW] [Flip H] [Flip V]  |  [Zoom: Fit | 100% | +/- | percentage]
```

**Contextual sub-toolbars:** Crop presets, rotate slider, anonymize mode/effect toggles, and font size only appear when their parent tool is active.

Tool groups separated by thin vertical dividers (`border-stone-200`). Active tool: `bg-stone-800 text-white`.

## Keyboard Shortcuts — ShortcutManager Integration

All editor shortcuts integrate with the existing `ShortcutManager` system instead of ad-hoc keyboard handling.

**Changes to ShortcutManager:**

1. Add `'editor'` to `getActiveContexts()` — return `['editor']` instead of `[]` when editor modal is active
2. Add `'editor'` to context hierarchy in `contextsOverlap()` — leaf context under `global`
3. Add `'editor'` to `CATEGORY_ORDER` and `CATEGORY_LABELS`
4. Register all editor shortcuts with `context: 'editor'`, `category: 'editor'`
5. Remove ad-hoc `keydown` listener from `editor.js`

**Tool selection shortcuts:**

| Key | Tool |
|-----|------|
| V | Select |
| M | Move/Pan |
| C | Crop |
| B | Brush |
| E | Eraser |
| L | Line |
| W | Arrow |
| U | Rectangle |
| O | Circle |
| T | Text |
| X | Anonymize |
| I | Eyedropper |

**Action shortcuts:**

| Shortcut | Action |
|----------|--------|
| Mod+Z | Undo |
| Mod+Shift+Z / Mod+Y | Redo |
| Mod+S | Save as new clip |
| Mod+C | Copy selection |
| Mod+V | Paste selection |
| Mod+A | Select all |
| Delete / Backspace | Delete selection |
| Escape | Cancel operation / deselect / close editor |
| Enter | Confirm crop / commit text |
| R | Rotate 90° CW |
| Shift+R | Rotate 90° CCW |
| [ | Decrease brush size |
| ] | Increase brush size |
| Shift+[ | Decrease opacity |
| Shift+] | Increase opacity |
| Mod+0 | Zoom to fit |
| Mod+1 | Zoom to 100% |
| Mod+= | Zoom in |
| Mod+- | Zoom out |
| Space+drag | Pan (any tool) |

**Benefits:** All shortcuts appear in cheat sheet (`?`), are user-rebindable in Settings, and have conflict detection across contexts.

**Input guard:** Single-key shortcuts auto-suppressed when text input or filename input is focused (handled by ShortcutManager's existing input guard).

## Undo/Redo & Performance

**Improved storage:** `ImageData` objects instead of dataURL strings — avoids base64 encode/decode overhead.

**Memory management:** 50-state limit with ~100MB memory ceiling. Oldest states (beyond the original) dropped when ceiling exceeded. Original state always preserved.

**Canvas size limit:** Raised from 2000px to 4000px max. With zoom, users can work at detail level without aggressive downscaling. 4000x4000 RGBA = ~64MB, manageable for single-canvas rendering.

**Overlay performance:** `requestAnimationFrame` for marching ants animation. Anonymize brush preview only redraws affected region, not full overlay.

## Design System Compliance

All new UI elements follow the existing design language:
- Stone color palette exclusively
- `text-xs font-medium`, IBM Plex Mono
- `rounded-md`, `transition-colors`
- Active state: `bg-stone-800 text-white`
- Handles: white with stone-800 border
- Overlays: stone-900 at reduced opacity
- Icons: inline SVG, `stroke="currentColor"`, `stroke-width="1.5"`, `w-4 h-4`
