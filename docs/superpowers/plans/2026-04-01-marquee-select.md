# Marquee (Rubber-Band) Selection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add OS-style rubber-band selection to the clip gallery — click and drag on empty space to select intersecting clips.

**Architecture:** A standalone `frontend/js/marquee-select.js` module that listens for mousedown on the `#gallery` background, draws an absolutely-positioned overlay, computes card intersections via `getBoundingClientRect()`, and updates the existing `selectedIds` Set + checkbox state. No changes to existing event handlers.

**Tech Stack:** Vanilla JS, Playwright e2e tests

**Spec:** `docs/superpowers/specs/2026-04-01-marquee-select-design.md`

---

### Task 1: Add test selector and write failing e2e tests

**Files:**
- Modify: `e2e/helpers/selectors.ts:44-57` (gallery section)
- Create: `e2e/tests/bulk/marquee-select.spec.ts`

- [ ] **Step 1: Add `marqueeOverlay` selector to `selectors.ts`**

In `e2e/helpers/selectors.ts`, add `marqueeOverlay` to the `gallery` section after the `emptyState` entry:

```typescript
  // Clip gallery
  gallery: {
    container: '#gallery',
    clipCard: '#gallery > li:not([data-folder])',
    clipCardByName: (name: string) => `#gallery > li[data-filename="${name.toLowerCase()}"]`,
    clipCardById: (id: string) => `#gallery > li[data-id="${id}"]`,
    clipCheckbox: '.clip-checkbox',
    clipImage: '#gallery > li img',
    clipPreview: '.preview-container',
    clipTitle: '#gallery > li p',
    clipType: '#gallery > li span',
    expirationBadge: '.absolute.top-2.left-2',
    emptyState: '#empty-state',
    marqueeOverlay: '.marquee-overlay',
  },
```

- [ ] **Step 2: Create `e2e/tests/bulk/marquee-select.spec.ts` with all tests**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

// Helper: perform a marquee drag from (startX, startY) to (endX, endY)
async function marqueeDrag(
  page: any,
  startX: number,
  startY: number,
  endX: number,
  endY: number,
  options?: { shift?: boolean }
) {
  await page.mouse.move(startX, startY);
  if (options?.shift) {
    await page.keyboard.down('Shift');
  }
  await page.mouse.down();
  // Move in steps to trigger mousemove events and exceed threshold
  await page.mouse.move(endX, endY, { steps: 10 });
  await page.mouse.up();
  if (options?.shift) {
    await page.keyboard.up('Shift');
  }
}

// Helper: get empty space coordinates to the right of clips in the gallery
async function getGalleryEmptySpace(page: any) {
  const gallery = page.locator(selectors.gallery.container);
  const galleryBox = await gallery.boundingBox();
  // Right side of gallery is always empty when there are fewer clips than columns
  return {
    x: galleryBox!.x + galleryBox!.width - 20,
    y: galleryBox!.y + 20,
    galleryBox: galleryBox!,
  };
}

test.describe('Marquee Selection', () => {
  test('should select clips via marquee drag', async ({ app }) => {
    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png'),
    ]);
    await app.uploadFiles(files);
    await app.expectClipCount(3);

    const { x: emptyX, y: emptyY, galleryBox } = await getGalleryEmptySpace(app.page);

    // Drag from empty space on the right across all clips to the left
    await marqueeDrag(
      app.page,
      emptyX,
      emptyY,
      galleryBox.x + 5,
      emptyY
    );

    // All 3 clips should be selected
    const selectedCount = app.page.locator(selectors.bulk.selectedCount);
    await expect(selectedCount).toHaveText('3 selected');
  });

  test('should replace existing selection on plain marquee drag', async ({ app }) => {
    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png'),
    ]);
    const filenames = files.map((f) => path.basename(f));
    await app.uploadFiles(files);
    await app.expectClipCount(3);

    // Select all via checkbox
    await app.selectAll();
    const selectedCount = app.page.locator(selectors.bulk.selectedCount);
    await expect(selectedCount).toHaveText('3 selected');

    // Get position of the first clip only (leftmost)
    const firstClip = app.page.locator(selectors.gallery.clipCardByName(filenames[0]));
    const firstClipBox = await firstClip.boundingBox();

    const { x: emptyX } = await getGalleryEmptySpace(app.page);

    // Marquee drag that only covers the first clip
    // Start from empty space, drag left but only to the first clip
    await marqueeDrag(
      app.page,
      emptyX,
      firstClipBox!.y + firstClipBox!.height / 2,
      firstClipBox!.x + firstClipBox!.width / 2,
      firstClipBox!.y + firstClipBox!.height / 2
    );

    // Should NOT be "3 selected" anymore — plain marquee replaces
    // At least 1 clip should be selected (the one we dragged over)
    await expect(selectedCount).not.toHaveText('3 selected');
  });

  test('should add to existing selection with Shift+marquee', async ({ app }) => {
    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png'),
    ]);
    const filenames = files.map((f) => path.basename(f));
    await app.uploadFiles(files);
    await app.expectClipCount(3);

    // Select the first clip via checkbox
    await app.selectClip(filenames[0]);
    const selectedCount = app.page.locator(selectors.bulk.selectedCount);
    await expect(selectedCount).toHaveText('1 selected');

    // Get position of the last clip
    const lastClip = app.page.locator(selectors.gallery.clipCardByName(filenames[2]));
    const lastClipBox = await lastClip.boundingBox();

    const { x: emptyX } = await getGalleryEmptySpace(app.page);

    // Shift+marquee drag over the last clip only
    await marqueeDrag(
      app.page,
      emptyX,
      lastClipBox!.y + lastClipBox!.height / 2,
      lastClipBox!.x + lastClipBox!.width / 2,
      lastClipBox!.y + lastClipBox!.height / 2,
      { shift: true }
    );

    // Should now have 2 selected (first from checkbox + last from shift-marquee)
    await expect(selectedCount).toHaveText('2 selected');
  });

  test('should deselect all when clicking empty space', async ({ app }) => {
    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
    ]);
    await app.uploadFiles(files);
    await app.expectClipCount(2);

    // Select all via checkbox
    await app.selectAll();
    const selectedCount = app.page.locator(selectors.bulk.selectedCount);
    await expect(selectedCount).toHaveText('2 selected');

    // Click on empty space (no drag — just click)
    const { x: emptyX, y: emptyY } = await getGalleryEmptySpace(app.page);
    await app.page.mouse.click(emptyX, emptyY);

    // Bulk toolbar should be hidden (no selection)
    const toolbar = app.page.locator(selectors.bulk.toolbar);
    await expect(toolbar).toHaveClass(/opacity-0/);
  });

  test('should work in folder mode', async ({ app }) => {
    // Create a tag and enable folder mode
    await app.createTag('test-folder');
    await app.enableFolderMode();

    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
    ]);
    await app.uploadFiles(files);
    await app.expectClipCount(2);

    const { x: emptyX, y: emptyY, galleryBox } = await getGalleryEmptySpace(app.page);

    // Marquee drag across all clips
    await marqueeDrag(
      app.page,
      emptyX,
      emptyY,
      galleryBox.x + 5,
      emptyY
    );

    const selectedCount = app.page.locator(selectors.bulk.selectedCount);
    await expect(selectedCount).toHaveText('2 selected');
  });

  test('should not select filtered-out clips', async ({ app }) => {
    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png'),
    ]);
    const filenames = files.map((f) => path.basename(f));
    await app.uploadFiles(files);
    await app.expectClipCount(3);

    // Search for only the first file to filter out the others
    await app.search(filenames[0]);

    const { x: emptyX, y: emptyY, galleryBox } = await getGalleryEmptySpace(app.page);

    // Marquee drag across entire gallery width
    await marqueeDrag(
      app.page,
      emptyX,
      emptyY,
      galleryBox.x + 5,
      emptyY
    );

    // Only the visible (matching) clip should be selected
    const selectedCount = app.page.locator(selectors.bulk.selectedCount);
    await expect(selectedCount).toHaveText('1 selected');
  });

  test('should show bulk toolbar with correct count after marquee', async ({ app }) => {
    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
    ]);
    await app.uploadFiles(files);
    await app.expectClipCount(2);

    const { x: emptyX, y: emptyY, galleryBox } = await getGalleryEmptySpace(app.page);

    // Marquee drag across all clips
    await marqueeDrag(
      app.page,
      emptyX,
      emptyY,
      galleryBox.x + 5,
      emptyY
    );

    // Bulk toolbar should be visible with correct count
    const toolbar = app.page.locator(selectors.bulk.toolbar);
    await expect(toolbar).toHaveClass(/opacity-100/);
    const selectedCount = app.page.locator(selectors.bulk.selectedCount);
    await expect(selectedCount).toHaveText('2 selected');
  });
});
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd e2e && npx playwright test tests/bulk/marquee-select.spec.ts 2>&1 | tail -30`

Expected: All 7 tests FAIL (marquee drag has no effect because `marquee-select.js` doesn't exist yet).

- [ ] **Step 4: Commit test infrastructure**

```bash
git add e2e/helpers/selectors.ts e2e/tests/bulk/marquee-select.spec.ts
git commit -m "test: add failing e2e tests for marquee selection"
```

---

### Task 2: Create `marquee-select.js`

**Files:**
- Create: `frontend/js/marquee-select.js`

- [ ] **Step 1: Create `frontend/js/marquee-select.js` with complete implementation**

```javascript
/**
 * Marquee (rubber-band) selection for the clip gallery.
 *
 * Allows users to click and drag on empty space in the gallery grid
 * to draw a selection rectangle that selects all intersecting clips.
 *
 * Usage: initMarqueeSelect({ gallery, selectedIds, updateBulkToolbar })
 */

// eslint-disable-next-line no-unused-vars
function initMarqueeSelect({ gallery, selectedIds, updateBulkToolbar }) {
    const DRAG_THRESHOLD = 5;
    const SCROLL_ZONE = 40;
    const SCROLL_SPEED = 8;

    let isDragging = false;
    let startPageX = 0;
    let startPageY = 0;
    let overlay = null;
    let preSnapshot = new Set();
    let shiftHeld = false;
    let rafId = null;

    gallery.addEventListener('mousedown', onMouseDown);

    function onMouseDown(e) {
        // Only trigger on the gallery background (empty space), not on cards
        if (e.target !== gallery) return;
        // Left click only
        if (e.button !== 0) return;
        // Guard against folder drag in progress
        if (window.__internalDragActive) return;

        e.preventDefault();

        shiftHeld = e.shiftKey;
        preSnapshot = new Set(selectedIds);

        // Store start position in page coordinates (scroll-invariant)
        startPageX = e.pageX;
        startPageY = e.pageY;

        isDragging = false;

        document.addEventListener('mousemove', onMouseMove);
        document.addEventListener('mouseup', onMouseUp);
    }

    function onMouseMove(e) {
        const dx = e.pageX - startPageX;
        const dy = e.pageY - startPageY;

        if (!isDragging) {
            if (Math.abs(dx) < DRAG_THRESHOLD && Math.abs(dy) < DRAG_THRESHOLD) return;
            isDragging = true;
            createOverlay();

            // Clear existing selection if Shift not held
            if (!shiftHeld) {
                clearAllSelections();
            }
        }

        if (rafId) cancelAnimationFrame(rafId);
        rafId = requestAnimationFrame(() => {
            updateOverlayPosition(e.pageX, e.pageY);
            updateCardSelections();
            autoScroll(e.clientY);
        });
    }

    function onMouseUp() {
        document.removeEventListener('mousemove', onMouseMove);
        document.removeEventListener('mouseup', onMouseUp);

        if (rafId) {
            cancelAnimationFrame(rafId);
            rafId = null;
        }

        if (!isDragging) {
            // Click on empty space without dragging — deselect all (unless Shift held)
            if (!shiftHeld) {
                clearAllSelections();
                updateBulkToolbar();
            }
        } else {
            updateBulkToolbar();
        }

        removeOverlay();
        isDragging = false;
    }

    function createOverlay() {
        overlay = document.createElement('div');
        overlay.className = 'marquee-overlay';
        overlay.style.cssText =
            'position:absolute;' +
            'border:2px solid rgba(28,25,23,0.6);' +
            'background:rgba(28,25,23,0.07);' +
            'border-radius:2px;' +
            'pointer-events:none;' +
            'z-index:20;';
        gallery.style.position = 'relative';
        gallery.appendChild(overlay);
    }

    function removeOverlay() {
        if (overlay) {
            overlay.remove();
            overlay = null;
            gallery.style.position = '';
        }
    }

    function updateOverlayPosition(currentPageX, currentPageY) {
        if (!overlay) return;

        // Convert page coordinates to gallery-relative coordinates
        const galleryRect = gallery.getBoundingClientRect();
        const galleryPageLeft = galleryRect.left + window.scrollX;
        const galleryPageTop = galleryRect.top + window.scrollY;

        const x1 = startPageX - galleryPageLeft;
        const y1 = startPageY - galleryPageTop;
        const x2 = currentPageX - galleryPageLeft;
        const y2 = currentPageY - galleryPageTop;

        overlay.style.left = Math.min(x1, x2) + 'px';
        overlay.style.top = Math.min(y1, y2) + 'px';
        overlay.style.width = Math.abs(x2 - x1) + 'px';
        overlay.style.height = Math.abs(y2 - y1) + 'px';
    }

    function updateCardSelections() {
        if (!overlay) return;

        const overlayRect = overlay.getBoundingClientRect();
        const cards = gallery.querySelectorAll('li[data-id]');

        // Start from snapshot if Shift held, else empty
        const next = shiftHeld ? new Set(preSnapshot) : new Set();

        for (const card of cards) {
            if (card.style.display === 'none') continue;
            if (rectsIntersect(overlayRect, card.getBoundingClientRect())) {
                next.add(Number(card.dataset.id));
            }
        }

        syncSelection(next);
    }

    function rectsIntersect(a, b) {
        return !(a.right < b.left || a.left > b.right || a.bottom < b.top || a.top > b.bottom);
    }

    function syncSelection(next) {
        selectedIds.clear();
        for (const id of next) selectedIds.add(id);

        const cards = gallery.querySelectorAll('li[data-id]');
        for (const card of cards) {
            const id = Number(card.dataset.id);
            const cb = card.querySelector('.clip-checkbox');
            const selected = next.has(id);
            card.classList.toggle('has-checked', selected);
            if (cb) cb.checked = selected;
        }

        // Sync the Select All checkbox
        const selectAllCb = document.getElementById('select-all-checkbox');
        if (selectAllCb) {
            const allCheckboxes = gallery.querySelectorAll('.clip-checkbox');
            selectAllCb.checked = allCheckboxes.length > 0 &&
                Array.from(allCheckboxes).every(cb => cb.checked);
        }
    }

    function clearAllSelections() {
        syncSelection(new Set());
    }

    function autoScroll(clientY) {
        if (clientY < SCROLL_ZONE) {
            window.scrollBy(0, -SCROLL_SPEED);
        } else if (clientY > window.innerHeight - SCROLL_ZONE) {
            window.scrollBy(0, SCROLL_SPEED);
        }
    }
}
```

- [ ] **Step 2: Verify no syntax errors**

Run: `node -c frontend/js/marquee-select.js`

Expected: No output (syntax is valid).

---

### Task 3: Wire up and integrate

**Files:**
- Modify: `frontend/index.html` (script tags section, around line 1691)
- Modify: `frontend/js/app.js` (load handler, around line 1025)

- [ ] **Step 1: Add `<script>` tag to `frontend/index.html`**

Add the script tag right after `folder-drag.js` (both are gallery interaction features):

```html
    <script src="js/folder-drag.js"></script>
    <script src="js/marquee-select.js"></script>
```

- [ ] **Step 2: Call `initMarqueeSelect()` in `frontend/js/app.js`**

In the `window.addEventListener('load', ...)` handler, add the init call after the `initTransferCapabilities` block (around line 1025) and before `await loadClips()`:

```javascript
        if (typeof initTransferCapabilities === 'function') {
            await initTransferCapabilities();
        }

        // Marquee (rubber-band) selection
        initMarqueeSelect({
            gallery: document.getElementById('gallery'),
            selectedIds,
            updateBulkToolbar,
        });
```

- [ ] **Step 3: Run the e2e tests**

Run: `cd e2e && npx playwright test tests/bulk/marquee-select.spec.ts 2>&1 | tail -40`

Expected: All 7 tests PASS.

- [ ] **Step 4: Run the full test suite to check for regressions**

Run: `cd e2e && npm test 2>&1 | tail -50`

Expected: All tests pass. If any existing tests fail, investigate and fix before committing.

- [ ] **Step 5: Commit implementation + tests**

```bash
git add frontend/js/marquee-select.js frontend/index.html frontend/js/app.js e2e/helpers/selectors.ts e2e/tests/bulk/marquee-select.spec.ts
git commit -m "feat: add marquee (rubber-band) selection to clip gallery

Click and drag on empty space in the gallery to draw a selection
rectangle. Clips intersected by the rectangle are selected. Shift+drag
adds to existing selection. Click on empty space deselects all.

Works in both normal and folder mode."
```

---

### Task 4: Update documentation

**Files:**
- Modify: `docs/docs/features/bulk-actions.md`

- [ ] **Step 1: Add "Marquee Selection" subsection to `docs/docs/features/bulk-actions.md`**

After the "### Checkbox Selection" subsection (ends around line 19), add:

```markdown
### Marquee Selection

Click and drag on the empty space between clip cards to draw a selection rectangle. Any clip that the rectangle touches gets selected -- just like selecting files in Finder or Explorer.

1. Click on the empty space in the gallery grid (between or after cards)
2. Drag to draw a selection rectangle
3. Release to confirm the selection

**Modifier keys:**
- **Plain drag**: Replaces the current selection with the marquee selection
- <span className="keyboard-key">Shift</span> **+ drag**: Adds the marquee selection to any existing selection

Marquee selection works in both the normal gallery view and folder mode.
```

- [ ] **Step 2: Add a tip about marquee selection to the "Tips" section**

In the `## Tips` section (around line 146), add a new bullet:

```markdown
- Click and drag on empty space to marquee-select multiple clips at once
```

- [ ] **Step 3: Commit documentation**

```bash
git add docs/docs/features/bulk-actions.md
git commit -m "docs: add marquee selection to bulk actions documentation"
```
