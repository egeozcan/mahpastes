# Compare Feature Improvements Design

**Date**: 2026-02-22
**Goal**: Improve the image comparison modal UX with diff visualization, better controls, image info, swap, keyboard shortcuts, and UI polish.
**Primary use case**: Image versioning — comparing edits to photos, spotting what changed between versions.

## 1. Diff Mode

Add "Diff" as a third mode button alongside Fade and Slider.

- New Go method `App.GetImageDiff(clipIdA, clipIdB int64) (*DiffResult, error)` that reuses the existing pixel-comparison logic from `plugin/api_image.go` but exposed directly to the frontend (not through Lua).
- Returns `DiffResult{Similarity float64, DiffDataUrl string}` — base64 diff image + similarity score.
- Display the diff image in the viewport replacing both layered images.
- Show similarity score as a badge in the header (e.g., "94.2% similar").
- Range slider becomes a **threshold** control in diff mode — adjusting diff sensitivity (currently hardcoded to 30 in the backend).
- Update Go method signature: `App.GetImageDiff(clipIdA, clipIdB int64, threshold int) (*DiffResult, error)`.
- Cache the diff result per threshold value so mode switching doesn't re-request.

## 2. Swap + Image Info

**Swap button**: Icon button in the header (two-arrow swap icon) that swaps which image is top vs bottom. Swaps `src` attributes and invalidates cached diff.

**Image info**: Small metadata line in the header:
```
A: 1920x1080 PNG 2.4MB  |  B: 1280x720 PNG 1.1MB
```
Using `text-[10px]` micro typography. Dimensions from `naturalWidth`/`naturalHeight`, format/size from clip metadata already available.

## 3. Fix Stretch & Alignment

Current bugs:
- Stretch uses hardcoded `1000px` — should use `100%` of viewport.
- Alignment doesn't behave predictably with different-sized images.

Fixes:
- Stretch mode: `width: 100%; height: 100%; object-fit: fill` relative to viewport.
- Non-stretch: `object-fit: contain` with proper max-width/max-height constraints.
- Top image wrapper must always match bottom image's rendered dimensions (use ResizeObserver or compute on load).
- Show small warning icon near alignment controls when images have different native dimensions.

## 4. Keyboard Shortcuts via ShortcutManager

Add a new `comparison` context to the ShortcutManager:
- Add `'comparison'` to context hierarchy, `CATEGORY_ORDER`, and `CATEGORY_LABELS`.
- Update `getActiveContexts()` so comparison modal returns `['comparison', 'global']` instead of `[]`.
- Shortcuts appear in cheat sheet and are customizable in Settings.

Registered shortcuts:

| ID | Default Key | Label |
|---|---|---|
| `compare-mode-fade` | `1` | Fade mode |
| `compare-mode-slider` | `2` | Slider mode |
| `compare-mode-diff` | `3` | Diff mode |
| `compare-swap` | `s` | Swap images |
| `compare-zoom-in` | `+` | Zoom in |
| `compare-zoom-out` | `-` | Zoom out |
| `compare-zoom-fit` | `0` | Fit to viewport |
| `compare-range-left` | `ArrowLeft` | Adjust range left |
| `compare-range-right` | `ArrowRight` | Adjust range right |
| `compare-close` | `Escape` | Close comparison |

## 5. UI Polish

- Fix off-palette colors: active mode button uses `text-blue-600` — change to `text-stone-800`.
- Stretch active state uses `bg-blue-600` — change to `bg-stone-800 text-white`.
- Wider range slider (200px instead of 140px) for more precise control.
- Add subtle "A" / "B" labels as badges in viewport corners.
- Improve slider handle icon — use proper left-right arrow instead of current down arrow.

## Non-Goals

- Rebuilding the modal layout/structure (the current grid layout works).
- Adding side-by-side panel view.
- Persisting comparison settings between sessions.
