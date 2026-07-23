# Accessible Lightbox Zoom Design

**Date:** 2026-07-23

**Status:** Approved

## Summary

Replace the globally coordinated lightbox implementation with a dedicated `LightboxController` module. The module will own lightbox selection, asynchronous image loading, focus restoration, viewport transforms, input handling, toolbar state, and accessibility semantics.

The redesigned viewer opens images at Fit, reports zoom as actual image scale, zooms around the cursor or gesture focal point, and provides complete keyboard, mouse, touchpad, and touchscreen operation. Its visual zoom controls will be integrated into the existing bottom bar.

## Problem

The current lightbox spreads its state and behavior across `app.js`, `ui.js`, `modals.js`, `wails-api.js`, and `editor.js`. This creates several related defects:

- Navigating replaces the saved focus opener with an element inside the lightbox, so closing may not return focus to the originating clip.
- Concurrent image requests can complete out of order or reactivate a closed lightbox.
- Delayed image removal can delete an image belonging to a newly reopened lightbox.
- Zoom uses a fit-relative transform while displaying native-size percentages, so the slider and displayed value disagree.
- Zoom and pan are not fully keyboard accessible.
- Pointer zoom is centered on the image instead of the cursor or gesture focal point.
- The range input lacks a proper accessible name and visible focus treatment.
- Loading and failure states are not communicated to the user.
- The integrated bottom bar does not adapt sufficiently to narrow windows.
- Desktop close and navigation controls can be positioned outside the viewport, a defect hidden by test helpers that invoke synthetic DOM clicks.

## Goals

1. Restore focus to the exact lightbox opener after any amount of navigation.
2. Make zoom and pan predictable and complete for keyboard, mouse, touchpad, and touch.
3. Keep the focal image point stationary while pointer or pinch zooming.
4. Define `100%` as one image pixel per screen pixel.
5. Provide visible, labeled, keyboard-operable zoom controls in the bottom bar.
6. Prevent stale image loads and delayed cleanup from mutating a newer lightbox state.
7. Provide immediate loading and actionable failure states.
8. Make navigation operate on a stable collection of visible image IDs.
9. Improve testability by concentrating the behavior behind one module interface.
10. Preserve existing plugin actions, file actions, and configurable shortcut integration.

## Non-goals

- Generalizing the viewport implementation for the editor or comparison viewer.
- Changing backend image storage or Wails bindings.
- Adding image rotation, editing, minimaps, or thumbnails.
- Replacing the existing plugin or context-menu systems.
- Allowing arbitrary zoom below Fit.

## Chosen approach

Create a dedicated lightbox module rather than patching the existing globals or introducing a shared editor/comparison viewport abstraction.

A local patch would leave lifecycle and transform state split across callers. A shared viewport abstraction would expand the work into modules with different interaction requirements. A dedicated lightbox module provides a clean seam without broadening the feature.

## Module interface

Add `frontend/js/lightbox.js` with a single `LightboxController` instance. Its external interface is:

```javascript
LightboxController.open({ clips, currentId, opener })
LightboxController.openSingle({ clip, opener })
LightboxController.setClips(clips)
LightboxController.command(name, detail)
LightboxController.close()
```

Callers identify what should open or issue semantic commands. They do not mutate the current index, image element, slider, timers, pan, or scale.

Representative commands include:

```text
next
previous
zoom-in
zoom-out
fit
actual-size
pan
retry
```

The controller accepts explicit dependencies for image retrieval, plugin action rendering, file action rendering, focus trapping, announcements, and toast/error reporting. Existing implementations remain adapters at those seams.

## Owned state

The controller owns:

```text
phase: closed | loading | ready | error
clips: stable ordered clip descriptors
currentId: stable clip identifier
requestGeneration: monotonically increasing load token
opener: original focus target
closeTimer: pending animation cleanup, if any
naturalSize: image width and height
viewportSize: usable viewport width and height
fitScale: actual Fit scale
scale: current actual image scale
panX / panY: viewport-space translation
pointerGesture: active drag, pinch, or wheel gesture state
wheelNavigation: accumulated delta, dominant axis, direction, cooldown
```

The current clip is always resolved by ID. Index is derived from the current ordered collection and is never the authoritative identity.

## DOM and visual design

The lightbox keeps a persistent DOM shell and image element. It contains:

- A close button inside the viewport.
- Previous and next controls inside the viewport.
- An image viewport with loading and error overlays.
- A caption used as the dialog label.
- A polite status region for loading, navigation, and errors.
- Plugin and file actions.
- An integrated bottom zoom toolbar.

The approved zoom toolbar is:

```text
[−] [logarithmic slider] [+] [Fit] [1:1] [67%]
```

The toolbar follows the existing stone palette and compact mono typography. Buttons have at least a 32×32 CSS-pixel target, and the slider has a visually compact track inside a hit area at least 32 pixels high. Every interactive target has a visible focus state. On narrow windows, the bottom bar becomes two rows: primary actions and image information occupy one row; zoom controls occupy the other. Secondary resolution metadata truncates before controls become unusable. Safe-area insets are respected.

The lightbox and popup transitions honor `prefers-reduced-motion`. Desktop and narrow-window rules keep close and navigation controls within the viewport.

## Scale model

### Definitions

- **Actual scale:** rendered image pixels divided by natural image pixels.
- **100% / 1:1:** one image pixel per screen pixel.
- **Fit:** the largest scale that fits the complete image inside the usable viewport, capped at 100% so small images are not enlarged automatically.
- **Maximum:** 800% actual scale.
- **Minimum:** the current Fit scale.

The image opens at Fit. The Fit scale is recomputed after image load and when the usable viewport changes.

### Slider mapping

The native range control uses 101 positions (`0` through `100`) mapped logarithmically between Fit and 800%. This gives useful precision near Fit and 100% without making high magnifications unreachable. Its visible value and `aria-valuetext` report actual image scale, for example:

```text
67% actual; Fit is 34%
```

`Home` selects Fit and `End` selects 800%. Arrow keys move one slider position; Page Up and Page Down move ten positions. The separate `+` and `-` commands multiply or divide actual scale by 1.2 before clamping.

### Focal-point invariant

When scale changes around a focal point, the controller adjusts pan so the same image-space coordinate remains under that viewport coordinate.

- Mouse and modified-wheel zoom use the pointer position.
- Touchpad pinch and touchscreen pinch use the gesture centroid.
- Double-click uses the pointer position.
- Toolbar, slider, and keyboard zoom use the viewport center.

After every transform, pan is constrained. If an image dimension is smaller than the viewport, it is centered on that axis. If it is larger, its edges cannot be panned beyond the viewport far enough to lose the image.

## Input contract

### Keyboard

| Input | Behavior |
|---|---|
| `+` | Zoom in around viewport center |
| `-` | Zoom out around viewport center |
| `0` | Fit |
| `1` | Actual size / 1:1 |
| `Shift + Arrow` | Pan by 48 CSS pixels while zoomed |
| Left / Right Arrow | Previous / next image |
| Escape | Close the active menu first, then the lightbox |
| Tab / Shift+Tab | Traverse and remain within the active lightbox or its menu |

Keyboard pan commands clamp at image bounds and provide no-op feedback only through control state, not noisy announcements.

### Mouse

- `Ctrl/Cmd + wheel` zooms around the cursor.
- Dragging a zoomed image pans using pointer capture.
- Double-click toggles between Fit and 1:1 around the pointer.
- Toolbar buttons and slider remain available without gestures.

### Touchpad

- Pinch zooms around the gesture focal point.
- Two-finger scrolling pans while the image is zoomed and pannable.
- At Fit, when the image is not pannable, vertical or horizontal scrolling navigates.
- Down or right navigates to the next image; up or left navigates to the previous image.

### Touchscreen

- Pinch zooms around the gesture centroid.
- One-finger drag pans while zoomed.
- At Fit, a qualifying horizontal swipe navigates.

### Wheel navigation guard

At Fit, wheel and two-finger navigation uses accumulated deltas, dominant-axis selection, a threshold, and a cooldown. One physical gesture advances at most one image. Momentum events do not skip multiple images. Wheel events never navigate while the image is above Fit, even when pan has reached an edge.

## Loading and lifecycle

1. Capture the opener only when transitioning from closed to open.
2. Activate the dialog shell immediately.
3. Set the viewport to `aria-busy="true"` and show a loading indicator.
4. Increment `requestGeneration` and retrieve the requested image.
5. Commit the result only if the generation and `currentId` still match.
6. Compute natural dimensions, Fit, pan, slider state, caption, and actions.
7. Mark the viewport ready and announce the loaded filename and position.

Navigating repeats the loading sequence without changing the saved opener. Closing increments the generation so pending loads become stale. The image element remains persistent; close animations do not remove whichever image happens to be referenced later.

## Collection replacement

Gallery rendering supplies the controller with an ordered snapshot containing only images visible under the current gallery state, including client-side search visibility.

`setClips` atomically replaces that snapshot:

- Preserve `currentId` when it still exists.
- If it disappeared, clamp its former index into the replacement collection, preferring the next clip at that index and otherwise the previous final clip.
- Close the lightbox when no images remain.
- Recompute boundary controls after replacement.

Linked Markdown images use `openSingle` and do not swap or retain references to the gallery's global array.

## Focus and accessibility

- The dialog uses the changing caption as its accessible name.
- Opening focuses the visible close button.
- The page behind the lightbox is inert while the lightbox is active.
- The original opener is captured once and retained through navigation.
- Closing restores the exact opener when it remains connected and visible.
- If the opener disappeared, focus moves to the visible clip at the opener's former gallery index, then the previous final clip when that index no longer exists, and finally the gallery container.
- The range has a programmatic label, actual-scale `aria-valuetext`, visible focus state, and associated percentage description.
- Loading and error changes use `aria-busy` and a restrained `aria-live="polite"` region.
- Plugin and file menus participate in the same focus model. Closing a menu returns focus to its trigger; Tab never leaves focus on a removed menu item.
- Image alternative text uses the raw filename rather than HTML-escaped text.
- Muted toolbar text meets normal-text contrast requirements.

## Error handling

A failed image load keeps the requested clip selected and replaces the loading overlay with an inline error state. It provides:

- Retry
- Previous and Next, when available
- Close

The previous image is never left visible beneath metadata for the failed clip. Retrying creates a new request generation. Stale success or error completions are ignored. Errors are announced politely and logged for diagnostics.

## Integration changes

- `frontend/js/app.js`: route listeners and shortcut callbacks through semantic controller commands.
- `frontend/js/ui.js`: open with the visible image snapshot and originating control; stop mutating `imageClips`.
- `frontend/js/wails-api.js`: atomically provide replacement clip collections after gallery loads.
- `frontend/js/editor.js`: use `openSingle` for Markdown reference images.
- `frontend/js/modals.js`: retain comparison behavior; remove lightbox state, gestures, loading, and menu ownership.
- `frontend/index.html`: add the persistent viewport, loading/error UI, integrated toolbar, labels, and status region; load `lightbox.js` before callers.
- `frontend/css/modals.css`: replace the lightbox section with viewport-safe, responsive, focus-visible, and reduced-motion styling.
- `e2e/helpers/selectors.ts`: centralize caption, status, image-info, file trigger, zoom buttons, Fit, and 1:1 selectors.
- `e2e/fixtures/test-fixtures.ts`: wait for active and settled lightbox states and use actionability-preserving locator clicks.

No Go methods or generated Wails bindings change.

## Test strategy

Development follows test-first slices.

### Lifecycle regressions

- Out-of-order image responses commit only the latest requested clip.
- Escape during loading keeps the lightbox closed after the request resolves.
- Closing and reopening immediately does not remove the new image.
- Gallery replacement preserves or predictably replaces the current ID.

### Focus

- Opening focuses the visible close button.
- Navigating by button and keyboard does not replace the saved opener.
- Closing returns focus to the exact opener.
- Removed openers fall back to the nearest visible clip and then the gallery.
- Plugin-menu Arrow, Escape, and Tab behavior remains contained.

### Content and navigation

- Image source, caption, position, natural dimensions, and current actions agree.
- Previous and next controls change actual clip content.
- Boundary controls and single-image behavior are explicit.
- Wheel navigation works on both axes at Fit and advances only once per gesture.
- Search-hidden images are absent from the navigation collection.

### Zoom and pan

- Fit and 1:1 calculate correct actual scales.
- Maximum clamps at 800%.
- Keyboard, buttons, slider, modified wheel, pinch, drag, and two-finger pan update the same state.
- Cursor and gesture focal-point invariants hold within a small pixel tolerance.
- `Shift + Arrow` pans; plain arrows continue navigating.
- Zoom and pan reset predictably on navigation and reopen.
- Resize recomputes Fit and keeps a sensible focal image point.

### Accessibility and responsive behavior

- Every control has an accessible name and visible focus treatment.
- Slider value text reports actual scale.
- Busy, ready, navigation, and error status changes are announced appropriately.
- Reduced-motion disables nonessential transitions.
- Desktop and narrow viewports keep controls visible, non-overlapping, and actionable.
- Tests use real pointer/locator interaction rather than synthetic DOM clicks.

### Verification commands

Run focused suites during development, followed by the complete project suite:

```bash
cd e2e
npx playwright test tests/images/lightbox.spec.ts
npx playwright test tests/shortcuts/shortcuts.spec.ts --grep "Lightbox"
npx playwright test tests/plugins/ui-actions.spec.ts --grep "Lightbox"
npm test 2>&1 | tail -50
```

## Acceptance criteria

The redesign is complete when:

1. Focus returns to the exact originating clip after navigating and closing.
2. Fit, 1:1, and 800% have consistent actual-scale semantics in UI and accessibility output.
3. Pointer and pinch zoom preserve their focal image point.
4. Keyboard users can zoom, reset, select 1:1, and inspect all pannable content.
5. Mouse and touchpad users can zoom and pan without accidental multi-image skips.
6. Wheel navigation works vertically and horizontally only when the image is at Fit and not pannable.
7. Toolbar controls are visible, labeled, focusable, and responsive.
8. Stale loads cannot show the wrong image, reopen a closed dialog, or remove a reopened image.
9. Loading and failure states are visible and announced.
10. Actions, metadata, caption, and image always refer to the same current clip.
11. Focus remains contained when plugin or file menus are open.
12. Focused and full E2E suites pass.
