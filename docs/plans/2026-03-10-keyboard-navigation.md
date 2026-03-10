# Keyboard Navigation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the entire app navigable via keyboard using roving tabindex as the standard pattern for all list-like structures, with TDD and inline e2e tests.

**Architecture:** A shared `RovingTabindex` class handles arrow-key navigation and tabindex management for any list/grid. A shared `trapFocus()` utility handles modal focus trapping. All modals get focus-on-open and focus-restore. The ShortcutManager is rewired to derive clip context from DOM focus instead of its internal index.

**Tech Stack:** Vanilla JS, Playwright e2e tests, Tailwind CSS

---

### Task 1: AppHelper keyboard test utilities

**Files:**
- Modify: `e2e/fixtures/test-fixtures.ts` (add methods near line 2320)
- Modify: `e2e/helpers/selectors.ts` (update `focusedClip` selector)

**Step 1: Add `expectFocusOn` and `tabTo` methods to AppHelper**

In `e2e/fixtures/test-fixtures.ts`, add these methods after the `isFocusedClipVisible` method (around line 2323):

```typescript
  /** Assert that the currently focused element matches the given selector. */
  async expectFocusOn(selector: string): Promise<void> {
    await expect(this.page.locator(selector)).toBeFocused();
  }

  /**
   * Press Tab repeatedly until the focused element matches `selector`.
   * Throws after `maxTabs` presses to prevent infinite loops.
   * If `shift` is true, presses Shift+Tab instead.
   */
  async tabTo(selector: string, options?: { shift?: boolean; maxTabs?: number }): Promise<void> {
    const max = options?.maxTabs ?? 30;
    const key = options?.shift ? 'Shift+Tab' : 'Tab';
    for (let i = 0; i < max; i++) {
      await this.page.keyboard.press(key);
      const focused = await this.page.evaluate(
        (sel) => document.activeElement?.matches(sel) ?? false,
        selector
      );
      if (focused) return;
    }
    throw new Error(`Could not tab to "${selector}" within ${max} presses`);
  }
```

**Step 2: Update `getFocusedClipIndex` to use DOM focus instead of ShortcutManager internal state**

Replace the existing `getFocusedClipIndex` method (line 2312-2317) with:

```typescript
  async getFocusedClipIndex(): Promise<number> {
    return this.page.evaluate(() => {
      const focused = document.activeElement;
      if (!focused || !focused.matches('#gallery > li')) return -1;
      const gallery = document.getElementById('gallery');
      if (!gallery) return -1;
      const clips = Array.from(gallery.querySelectorAll(':scope > li'));
      return clips.indexOf(focused);
    });
  }
```

**Step 3: Update `isFocusedClipVisible` to use `:focus-visible` instead of `.clip-focused`**

Replace the existing `isFocusedClipVisible` method (line 2319-2322) with:

```typescript
  async isFocusedClipVisible(): Promise<boolean> {
    return this.page.evaluate(() => {
      const focused = document.activeElement;
      return !!focused && focused.matches('#gallery > li');
    });
  }
```

**Step 4: Update selectors.ts — change `focusedClip` selector**

In `e2e/helpers/selectors.ts`, change line 419:

```typescript
    focusedClip: '#gallery > li:focus-visible',
```

(replacing `.clip-focused`)

**Step 5: Run existing tests to confirm they still compile**

Run: `cd e2e && npx tsc --noEmit`
Expected: No type errors (tests will fail at runtime since implementation isn't done yet — that's fine)

**Step 6: Commit**

```bash
git add e2e/fixtures/test-fixtures.ts e2e/helpers/selectors.ts
git commit -m "feat(e2e): add keyboard test utilities (expectFocusOn, tabTo)"
```

---

### Task 2: RovingTabindex module

**Files:**
- Create: `frontend/js/roving-tabindex.js`

**Step 1: Write a basic test in shortcuts spec to verify roving tabindex on gallery**

In `e2e/tests/shortcuts/shortcuts.spec.ts`, add this test at the end of the `Grid Navigation` describe block (after existing grid tests):

```typescript
    test('should Tab into gallery and focus first clip, then Tab out', async ({ app }) => {
      const img1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const img2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(img1);
      await app.uploadFile(img2);
      await app.expectClipCount(2);

      // Tab into the gallery — first clip should get focus
      await app.tabTo('#gallery > li');
      expect(await app.getFocusedClipIndex()).toBe(0);

      // Arrow right to second clip
      await app.page.keyboard.press('ArrowRight');
      expect(await app.getFocusedClipIndex()).toBe(1);

      // Tab out of gallery — focus should leave gallery
      await app.page.keyboard.press('Tab');
      expect(await app.getFocusedClipIndex()).toBe(-1);
    });

    test('should support Home/End keys in gallery', async ({ app }) => {
      const img1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const img2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      const img3 = await createTempFile(generateTestImage(100, 100, 'green'), 'png');
      await app.uploadFile(img1);
      await app.uploadFile(img2);
      await app.uploadFile(img3);
      await app.expectClipCount(3);

      await app.tabTo('#gallery > li');
      expect(await app.getFocusedClipIndex()).toBe(0);

      // End should jump to last
      await app.page.keyboard.press('End');
      expect(await app.getFocusedClipIndex()).toBe(2);

      // Home should jump to first
      await app.page.keyboard.press('Home');
      expect(await app.getFocusedClipIndex()).toBe(0);
    });
```

**Step 2: Run tests to verify they fail**

Run: `cd e2e && npx playwright test tests/shortcuts/shortcuts.spec.ts -g "should Tab into gallery"`
Expected: FAIL — gallery clips aren't tabbable yet

**Step 3: Create the RovingTabindex module**

Create `frontend/js/roving-tabindex.js`:

```javascript
// --- Roving Tabindex ---
// Reusable keyboard navigation for list-like structures.
// Usage: const rover = RovingTabindex({ container, itemSelector, ... });

const RovingTabindex = (() => {
    /**
     * @param {Object} opts
     * @param {HTMLElement} opts.container       - The parent element
     * @param {string}      opts.itemSelector    - CSS selector for navigable items
     * @param {'horizontal'|'vertical'|'grid'} [opts.orientation='vertical']
     * @param {number|Function} [opts.columns=1] - Column count (grid only)
     * @param {boolean}    [opts.wrap=false]      - Wrap at edges
     * @param {Function}   [opts.onFocus]         - Called with (item, index) on focus
     * @param {Function}   [opts.onActivate]      - Called with (item, index) on Enter/Space
     */
    function create(opts) {
        const { container, itemSelector, onFocus, onActivate } = opts;
        const orientation = opts.orientation || 'vertical';
        const wrap = opts.wrap || false;
        const getColumns = typeof opts.columns === 'function'
            ? opts.columns
            : () => (opts.columns || 1);

        let activeIndex = 0;

        function getItems() {
            return Array.from(container.querySelectorAll(itemSelector));
        }

        function setTabIndexes(items, focusIdx) {
            items.forEach((item, i) => {
                item.setAttribute('tabindex', i === focusIdx ? '0' : '-1');
            });
        }

        function focusItem(items, idx) {
            if (idx < 0 || idx >= items.length) return;
            activeIndex = idx;
            setTabIndexes(items, idx);
            items[idx].focus();
            items[idx].scrollIntoView({ behavior: 'smooth', block: 'nearest' });
            if (onFocus) onFocus(items[idx], idx);
        }

        function clampOrWrap(idx, total) {
            if (wrap) {
                return ((idx % total) + total) % total;
            }
            return Math.max(0, Math.min(idx, total - 1));
        }

        function handleKeydown(e) {
            const items = getItems();
            if (items.length === 0) return;

            // Only handle if the event target is one of our items
            const currentIdx = items.indexOf(e.target);
            if (currentIdx === -1) return;

            const cols = getColumns();
            let nextIdx = currentIdx;
            let handled = true;

            switch (e.key) {
                case 'ArrowRight':
                    if (orientation === 'vertical') { handled = false; break; }
                    nextIdx = clampOrWrap(currentIdx + 1, items.length);
                    break;
                case 'ArrowLeft':
                    if (orientation === 'vertical') { handled = false; break; }
                    nextIdx = clampOrWrap(currentIdx - 1, items.length);
                    break;
                case 'ArrowDown':
                    if (orientation === 'horizontal') { handled = false; break; }
                    if (orientation === 'grid') {
                        nextIdx = clampOrWrap(currentIdx + cols, items.length);
                    } else {
                        nextIdx = clampOrWrap(currentIdx + 1, items.length);
                    }
                    break;
                case 'ArrowUp':
                    if (orientation === 'horizontal') { handled = false; break; }
                    if (orientation === 'grid') {
                        nextIdx = clampOrWrap(currentIdx - cols, items.length);
                    } else {
                        nextIdx = clampOrWrap(currentIdx - 1, items.length);
                    }
                    break;
                case 'Home':
                    nextIdx = 0;
                    break;
                case 'End':
                    nextIdx = items.length - 1;
                    break;
                case 'Enter':
                case ' ':
                    if (onActivate) onActivate(items[currentIdx], currentIdx);
                    e.preventDefault();
                    return;
                default:
                    handled = false;
            }

            if (!handled) return;
            if (nextIdx !== currentIdx) {
                e.preventDefault();
                focusItem(items, nextIdx);
            } else {
                e.preventDefault(); // Prevent scroll on arrow keys
            }
        }

        // When the container regains focus (e.g., Tab into), focus the active item
        function handleFocusin(e) {
            const items = getItems();
            if (items.length === 0) return;
            // Only handle if focus is coming from outside the container
            if (container.contains(e.relatedTarget)) return;
            // If the target is the container itself or an item, focus the active item
            const targetIdx = items.indexOf(e.target);
            if (targetIdx !== -1) {
                // User tabbed to the item with tabindex="0" — update active
                activeIndex = targetIdx;
                if (onFocus) onFocus(items[targetIdx], targetIdx);
            }
        }

        // Initialize
        function update() {
            const items = getItems();
            if (items.length === 0) return;
            if (activeIndex >= items.length) activeIndex = items.length - 1;
            if (activeIndex < 0) activeIndex = 0;
            setTabIndexes(items, activeIndex);
        }

        function reset() {
            activeIndex = 0;
            update();
        }

        function getActiveIndex() {
            return activeIndex;
        }

        function setActiveIndex(idx) {
            const items = getItems();
            if (idx >= 0 && idx < items.length) {
                activeIndex = idx;
                setTabIndexes(items, idx);
            }
        }

        function destroy() {
            container.removeEventListener('keydown', handleKeydown);
            container.removeEventListener('focusin', handleFocusin);
        }

        // Attach listeners
        container.addEventListener('keydown', handleKeydown);
        container.addEventListener('focusin', handleFocusin);

        // Initial setup
        update();

        return { update, reset, destroy, getActiveIndex, setActiveIndex, getItems };
    }

    return { create };
})();
```

**Step 4: Add `<script>` tag for roving-tabindex.js in `frontend/index.html`**

Find the script tag for `shortcuts.js` and add `roving-tabindex.js` before it (since shortcuts.js will depend on it):

```html
<script src="js/roving-tabindex.js"></script>
```

**Step 5: Commit**

```bash
git add frontend/js/roving-tabindex.js frontend/index.html
git commit -m "feat: add RovingTabindex module for keyboard navigation"
```

---

### Task 3: Focus trap utility and focus-visible CSS

**Files:**
- Modify: `frontend/js/utils.js` (add `trapFocus` function after line 40)
- Modify: `frontend/css/main.css` (update `focus-visible` style at line 52)

**Step 1: Write failing e2e test for lightbox focus trap (all focusable elements, not just buttons)**

In `e2e/tests/images/lightbox.spec.ts`, find the lightbox test describe block and add:

```typescript
  test('should trap Tab focus within lightbox', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    await app.openLightbox(path.basename(imagePath));

    // Tab through all focusable elements — should cycle back to first
    const firstFocusable = await app.page.evaluate(() => {
      const lb = document.getElementById('lightbox');
      const els = lb.querySelectorAll('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])');
      return els[0]?.id || els[0]?.tagName || null;
    });
    expect(firstFocusable).toBeTruthy();

    // Tab past last element should cycle to first
    const focusableCount = await app.page.evaluate(() => {
      const lb = document.getElementById('lightbox');
      return lb.querySelectorAll('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])').length;
    });

    // Press Tab enough times to cycle
    for (let i = 0; i < focusableCount + 1; i++) {
      await app.page.keyboard.press('Tab');
    }

    // Focus should still be inside lightbox
    const focusInLightbox = await app.page.evaluate(() => {
      const lb = document.getElementById('lightbox');
      return lb?.contains(document.activeElement) ?? false;
    });
    expect(focusInLightbox).toBe(true);
  });

  test('should restore focus to gallery clip after lightbox close', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    // Focus the clip via Tab
    await app.tabTo('#gallery > li');
    expect(await app.getFocusedClipIndex()).toBe(0);

    // Open lightbox with Enter
    await app.page.keyboard.press('Enter');
    expect(await app.isLightboxOpen()).toBe(true);

    // Close lightbox with Escape
    await app.page.keyboard.press('Escape');
    await app.page.waitForFunction(() => {
      const lb = document.getElementById('lightbox');
      return !lb?.classList.contains('active');
    });

    // Wait for focus restoration (lightbox has a 300ms delay)
    await app.page.waitForTimeout(400);

    // Focus should be back on the gallery clip
    expect(await app.getFocusedClipIndex()).toBe(0);
  });
```

**Step 2: Run tests to verify they fail**

Run: `cd e2e && npx playwright test tests/images/lightbox.spec.ts -g "should trap Tab focus"`
Expected: FAIL

**Step 3: Add `trapFocus` utility to utils.js**

In `frontend/js/utils.js`, add after line 40 (after `closeConfirmDialog`):

```javascript
/**
 * Trap Tab/Shift+Tab focus within a container.
 * Returns a cleanup function to remove the listener.
 * @param {HTMLElement} container
 * @returns {Function} cleanup
 */
function trapFocus(container) {
    function handler(e) {
        if (e.key !== 'Tab') return;

        const focusable = container.querySelectorAll(
            'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
        );
        if (focusable.length === 0) return;

        const first = focusable[0];
        const last = focusable[focusable.length - 1];

        if (e.shiftKey && document.activeElement === first) {
            last.focus();
            e.preventDefault();
        } else if (!e.shiftKey && document.activeElement === last) {
            first.focus();
            e.preventDefault();
        }
    }

    container.addEventListener('keydown', handler);
    return () => container.removeEventListener('keydown', handler);
}
```

**Step 4: Update focus-visible CSS**

In `frontend/css/main.css`, replace the existing `*:focus-visible` rule (lines 52-55):

```css
/* Focus states */
*:focus-visible {
    outline: 2px solid #a8a29e; /* stone-400 */
    outline-offset: 2px;
    border-radius: 4px;
}

/* Clip cards: match existing focused card style */
#gallery > li:focus-visible {
    outline: 2px solid #a8a29e;
    outline-offset: 2px;
    transform: scale(1.02);
    z-index: 1;
}

/* Buttons on dark backgrounds get lighter ring */
.bg-stone-800 button:focus-visible,
.bg-stone-800:focus-visible,
#shortcuts-cheatsheet button:focus-visible {
    outline-color: #d6d3d1; /* stone-300 */
}

/* Inputs keep their existing Tailwind focus ring */
input:focus-visible,
textarea:focus-visible,
select:focus-visible {
    outline: none;
}
```

**Step 5: Commit**

```bash
git add frontend/js/utils.js frontend/css/main.css
git commit -m "feat: add trapFocus utility and update focus-visible styles"
```

---

### Task 4: Gallery grid roving tabindex

**Files:**
- Modify: `frontend/js/app.js` (wire up RovingTabindex on gallery)
- Modify: `frontend/js/shortcuts.js` (remove `focusedClipIndex`, rewire context detection)
- Modify: `frontend/css/main.css` (remove `.clip-focused` class)

**Step 1: Wire up RovingTabindex on the gallery grid**

In `frontend/js/app.js`, find the initialization section at the bottom of the file (where `ShortcutManager.init()` is called). Add gallery roving tabindex setup. First, search for where shortcuts are registered to understand init flow:

```javascript
// Gallery roving tabindex — single Tab stop, arrow keys navigate within
const galleryRover = RovingTabindex.create({
    container: gallery,
    itemSelector: ':scope > li',
    orientation: 'grid',
    columns: () => ShortcutManager.getGridColumnCount?.() ?? getGalleryColumnCount(),
    wrap: false,
    onFocus: (item, index) => {
        // ShortcutManager needs to know a clip is focused for context detection
    },
    onActivate: (item) => {
        // Enter/Space on a clip card opens lightbox or triggers the card's primary action
        const clipId = parseInt(item.dataset.id, 10);
        if (clipId) {
            const idx = imageClips.findIndex(c => c.id === clipId);
            if (idx >= 0) {
                openLightbox(idx);
            }
        }
    },
});
```

Expose `galleryRover` via `window.__testHelpers` and as a global for ShortcutManager:

```javascript
window.__galleryRover = galleryRover;
Object.assign(window.__testHelpers, {
    galleryRover: galleryRover,
});
```

After every `loadClips()` call completes and renders the gallery, call `galleryRover.update()` to re-index the new items. Find where `renderCards` finishes in `ui.js` or where `loadClips` resolves.

**Step 2: Rewire ShortcutManager — remove internal focus state, use DOM focus**

In `frontend/js/shortcuts.js`:

a) Remove `let focusedClipIndex = -1;` (line 15)

b) Replace `getActiveContexts` clip detection (line 98-100):

Old:
```javascript
if (focusedClipIndex >= 0) {
    contexts.push('clip');
}
```

New:
```javascript
if (document.activeElement && document.activeElement.matches('#gallery > li')) {
    contexts.push('clip');
}
```

c) Replace `setFocusedClipIndex`, `clearFocus`, `navigateGrid`, `getFocusedClip`, `getFocusedClipId` methods. These now delegate to the gallery's RovingTabindex or read from `document.activeElement`:

```javascript
function getFocusedClip() {
    const active = document.activeElement;
    if (active && active.matches('#gallery > li')) return active;
    return null;
}

function getFocusedClipId() {
    const clip = getFocusedClip();
    if (!clip) return null;
    return parseInt(clip.dataset.id, 10);
}

function clearFocus() {
    const rover = window.__galleryRover;
    if (rover) rover.reset();
    // Blur current clip if focused
    const clip = getFocusedClip();
    if (clip) clip.blur();
}
```

d) Remove `navigateGrid` and `setFocusedClipIndex` from the public API — the RovingTabindex module handles this now.

e) Keep the `navigateGrid` shortcut registrations (ArrowUp/Down/Left/Right in gallery context) but change their callbacks to use the gallery rover:

```javascript
// In the shortcut registration section of app.js:
// Arrow keys — let roving tabindex handle them when gallery is focused.
// Only register the initial "focus gallery" shortcuts for when nothing is focused.
```

Actually, the RovingTabindex handles arrow keys on its own since it listens on the container. The ShortcutManager arrow key shortcuts for gallery context should be removed or changed to just focus the first clip (initial entry).

For the gallery context arrow key shortcuts registered in `app.js`, change them to:
- If no clip is focused, focus the first clip via `galleryRover.setActiveIndex(0)` and focus it
- If a clip is already focused, do nothing (RovingTabindex handles it)

**Step 3: Remove `.clip-focused` class from CSS**

In `frontend/css/main.css`, remove lines 294-300:

```css
/* Keyboard navigation focus indicator */
.clip-focused {
    outline: 2px solid #a8a29e; /* stone-400 */
    outline-offset: 2px;
    transform: scale(1.02);
    z-index: 1;
}
```

(Already covered by the `#gallery > li:focus-visible` rule added in Task 3)

**Step 4: Update `galleryRover.update()` call after gallery re-renders**

Find where `renderCards()` finishes in `frontend/js/ui.js` and add `window.__galleryRover?.update()` at the end:

```javascript
// At the end of renderCards() or renderFolderCards():
if (window.__galleryRover) window.__galleryRover.update();
```

**Step 5: Run the grid navigation tests**

Run: `cd e2e && npx playwright test tests/shortcuts/shortcuts.spec.ts -g "Grid Navigation"`
Expected: PASS for all grid navigation tests including the new Tab-in/Tab-out tests

**Step 6: Run ALL existing tests to verify no regressions**

Run: `cd e2e && npm test`
Expected: All tests pass

**Step 7: Commit**

```bash
git add frontend/js/shortcuts.js frontend/js/app.js frontend/js/ui.js frontend/css/main.css frontend/js/roving-tabindex.js
git commit -m "feat: adopt roving tabindex for gallery grid, remove clip-focused class"
```

---

### Task 5: Modal focus management — lightbox

**Files:**
- Modify: `frontend/js/modals.js` (replace inline focus trap with `trapFocus`, focus-on-open container)

**Step 1: Replace lightbox inline focus trap with shared `trapFocus`**

In `frontend/js/modals.js`, find the inline Tab focus trap (lines 893-909):

```javascript
// Tab focus trap for lightbox (not routed through ShortcutManager since Tab shouldn't be rebindable)
lightbox.addEventListener('keydown', (e) => {
    if (e.key !== 'Tab') return;
    if (!lightbox.classList.contains('active')) return;

    const focusableElements = lightbox.querySelectorAll('button');
    const first = focusableElements[0];
    const last = focusableElements[focusableElements.length - 1];

    if (e.shiftKey && document.activeElement === first) {
        last.focus();
        e.preventDefault();
    } else if (!e.shiftKey && document.activeElement === last) {
        first.focus();
        e.preventDefault();
    }
});
```

Replace with:

```javascript
// Focus trap for lightbox — uses shared trapFocus utility
let lightboxFocusTrapCleanup = null;
```

Then in `openLightbox()` (around line 456-458), after `lightbox.classList.add('active')`:

```javascript
    lightbox.classList.add('active');
    updateLightboxNav();

    // Focus management: trap focus and focus the container
    if (lightboxFocusTrapCleanup) lightboxFocusTrapCleanup();
    lightboxFocusTrapCleanup = trapFocus(lightbox);
    lightbox.focus();
```

In `closeLightbox()` (around line 468), add cleanup:

```javascript
function closeLightbox() {
    closeLightboxPluginMenu();
    closeLightboxFileMenu(true);
    lightbox.classList.remove('active');
    resetLightboxZoom();
    if (lightboxFocusTrapCleanup) {
        lightboxFocusTrapCleanup();
        lightboxFocusTrapCleanup = null;
    }
    setTimeout(() => {
        lightboxImg?.parentNode?.removeChild(lightboxImg);
        if (lastFocusedElementBeforeLightbox) {
            lastFocusedElementBeforeLightbox.focus();
        }
    }, 300);
}
```

**Step 2: Run lightbox focus tests**

Run: `cd e2e && npx playwright test tests/images/lightbox.spec.ts -g "focus"`
Expected: PASS

**Step 3: Commit**

```bash
git add frontend/js/modals.js
git commit -m "feat: use shared trapFocus for lightbox, improve focus management"
```

---

### Task 6: Modal focus management — comparison, settings, plugins, confirm dialog

**Files:**
- Modify: `frontend/js/modals.js` (comparison modal focus trap + focus-on-open)
- Modify: `frontend/js/settings.js` (settings modal focus trap + focus-on-open)
- Modify: `frontend/js/plugins.js` (plugins modal focus trap + focus-on-open)
- Modify: `frontend/js/utils.js` (confirm dialog — already focuses cancel button, add focus trap)

**Step 1: Write failing tests for settings modal focus trap**

In `e2e/tests/shortcuts/shortcuts.spec.ts`, add a new describe block:

```typescript
  test.describe('Modal Focus Management', () => {
    test('should focus settings modal container on open and restore focus on close', async ({ app }) => {
      await app.page.locator('body').click();

      // Open settings with , key
      await app.page.keyboard.press(',');
      await expect(app.page.locator(selectors.settings.modal)).not.toHaveClass(/opacity-0/);

      // Focus should be inside settings modal
      const focusInSettings = await app.page.evaluate(() => {
        const modal = document.getElementById('settings-modal');
        return modal?.contains(document.activeElement) ?? false;
      });
      expect(focusInSettings).toBe(true);

      // Close with Escape
      await app.page.keyboard.press('Escape');

      // Focus should return to body or previously focused element
      const focusInModal = await app.page.evaluate(() => {
        const modal = document.getElementById('settings-modal');
        return modal?.contains(document.activeElement) ?? false;
      });
      expect(focusInModal).toBe(false);
    });

    test('should focus confirm button in confirm dialog', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Trigger delete via card menu to get confirm dialog
      await app.clickDeleteInCardMenu(path.basename(imagePath));

      // Cancel button should be focused (it's the safe default)
      await expect(app.page.locator(selectors.confirm.cancelButton)).toBeFocused();
    });
  });
```

**Step 2: Run tests to verify they fail**

Run: `cd e2e && npx playwright test tests/shortcuts/shortcuts.spec.ts -g "Modal Focus"`
Expected: FAIL (settings doesn't focus on open)

**Step 3: Add focus management to comparison modal**

In `frontend/js/modals.js`, in `openComparisonModal()` (around line 914), after `comparisonModal.classList.add('active')`:

```javascript
    comparisonModal.classList.add('active');
    // Focus management
    if (comparisonFocusTrapCleanup) comparisonFocusTrapCleanup();
    comparisonFocusTrapCleanup = trapFocus(comparisonModal);
    comparisonModal.focus();
```

Add cleanup variable at top of file:
```javascript
let comparisonFocusTrapCleanup = null;
```

In the comparison close function, clean up:
```javascript
    if (comparisonFocusTrapCleanup) {
        comparisonFocusTrapCleanup();
        comparisonFocusTrapCleanup = null;
    }
```

**Step 4: Add focus management to settings modal**

In `frontend/js/settings.js`, find the `openSettings` function. Add:

```javascript
function openSettings() {
    lastFocusedElementBeforeSettings = document.activeElement;
    // ... existing code to show modal ...
    settingsModal.focus();
    if (settingsFocusTrapCleanup) settingsFocusTrapCleanup();
    settingsFocusTrapCleanup = trapFocus(settingsModal);
}
```

Add cleanup in `closeSettings`:
```javascript
function closeSettings() {
    // ... existing close code ...
    if (settingsFocusTrapCleanup) {
        settingsFocusTrapCleanup();
        settingsFocusTrapCleanup = null;
    }
    if (lastFocusedElementBeforeSettings) {
        lastFocusedElementBeforeSettings.focus();
    }
}
```

Add state variables at top:
```javascript
let lastFocusedElementBeforeSettings = null;
let settingsFocusTrapCleanup = null;
```

**Step 5: Add focus management to plugins modal**

Same pattern as settings — find `openPlugins`/`closePlugins` in `frontend/js/plugins.js`:

```javascript
let lastFocusedElementBeforePlugins = null;
let pluginsFocusTrapCleanup = null;

function openPlugins() {
    lastFocusedElementBeforePlugins = document.activeElement;
    // ... existing show code ...
    pluginsModal.focus();
    if (pluginsFocusTrapCleanup) pluginsFocusTrapCleanup();
    pluginsFocusTrapCleanup = trapFocus(pluginsModal);
}

function closePlugins() {
    // ... existing close code ...
    if (pluginsFocusTrapCleanup) {
        pluginsFocusTrapCleanup();
        pluginsFocusTrapCleanup = null;
    }
    if (lastFocusedElementBeforePlugins) {
        lastFocusedElementBeforePlugins.focus();
    }
}
```

**Step 6: Add focus trap to confirm dialog**

In `frontend/js/utils.js`, in `showConfirmDialog` (line 6), add focus trap:

```javascript
let confirmFocusTrapCleanup = null;

function showConfirmDialog(title, message, callback) {
    // ... existing code ...
    lastFocusedElement = document.activeElement;
    if (confirmFocusTrapCleanup) confirmFocusTrapCleanup();
    confirmFocusTrapCleanup = trapFocus(dialog);
    setTimeout(() => {
        document.getElementById('confirm-no-btn').focus();
    }, 100);
}

function closeConfirmDialog() {
    // ... existing code ...
    if (confirmFocusTrapCleanup) {
        confirmFocusTrapCleanup();
        confirmFocusTrapCleanup = null;
    }
    if (lastFocusedElement) {
        lastFocusedElement.focus();
    }
}
```

**Step 7: Run modal focus tests**

Run: `cd e2e && npx playwright test tests/shortcuts/shortcuts.spec.ts -g "Modal Focus"`
Expected: PASS

**Step 8: Run all tests**

Run: `cd e2e && npm test`
Expected: All pass

**Step 9: Commit**

```bash
git add frontend/js/modals.js frontend/js/settings.js frontend/js/plugins.js frontend/js/utils.js
git commit -m "feat: add focus trap and focus-on-open to all modals"
```

---

### Task 7: View tabs roving tabindex

**Files:**
- Modify: `frontend/js/app.js` (or wherever view tabs are initialized)
- Modify: `e2e/tests/shortcuts/shortcuts.spec.ts` (add view tab keyboard tests)

**Step 1: Write failing test**

In `e2e/tests/shortcuts/shortcuts.spec.ts`, add:

```typescript
  test.describe('View Tabs Keyboard Navigation', () => {
    test('should navigate view tabs with arrow keys', async ({ app }) => {
      // Open drawer to show view tabs
      await app.openDrawer();

      // Tab to the view tabs
      await app.tabTo('#view-tab-clips');

      // Arrow right to Watch tab
      await app.page.keyboard.press('ArrowRight');
      await app.expectFocusOn('#view-tab-watch');

      // Arrow right to Serve tab
      await app.page.keyboard.press('ArrowRight');
      await app.expectFocusOn('#view-tab-serve');

      // Arrow right at end should not wrap (wrap=false)
      await app.page.keyboard.press('ArrowRight');
      await app.expectFocusOn('#view-tab-serve');
    });
  });
```

**Step 2: Run test to verify it fails**

Run: `cd e2e && npx playwright test tests/shortcuts/shortcuts.spec.ts -g "View Tabs"`
Expected: FAIL

**Step 3: Add roving tabindex to view tabs**

Find the view tabs container in `frontend/index.html` — the `nav` element containing `#view-tab-clips`, `#view-tab-watch`, `#view-tab-serve`. Add `role="tablist"` if not present.

In the JS file where these tabs are initialized (likely `app.js`), add:

```javascript
// View tabs roving tabindex
const viewTabsContainer = document.querySelector('[role="tablist"]');
if (viewTabsContainer) {
    RovingTabindex.create({
        container: viewTabsContainer,
        itemSelector: '[role="tab"]',
        orientation: 'horizontal',
        wrap: false,
        onActivate: (tab) => tab.click(),
    });
}
```

**Step 4: Run test**

Run: `cd e2e && npx playwright test tests/shortcuts/shortcuts.spec.ts -g "View Tabs"`
Expected: PASS

**Step 5: Commit**

```bash
git add frontend/js/app.js frontend/index.html
git commit -m "feat: add roving tabindex to view tabs"
```

---

### Task 8: Tag filter dropdown roving tabindex

**Files:**
- Modify: `frontend/js/tags.js` (add roving tabindex to tag filter list)
- Modify: `e2e/tests/tags/` (add keyboard tests)

**Step 1: Write failing test**

In the appropriate tags spec file (check which file tests the filter dropdown), add:

```typescript
  test('should navigate tag filter checkboxes with arrow keys', async ({ app }) => {
    // Create two tags
    await app.createTag('alpha');
    await app.createTag('beta');

    // Open tag filter
    await app.page.locator(selectors.tags.filterButton).click();
    await expect(app.page.locator(selectors.tags.filterDropdown)).toBeVisible();

    // Tab into the tag list
    await app.tabTo('#tag-filter-list label');

    // Arrow down to next tag
    await app.page.keyboard.press('ArrowDown');

    // The second label should be focused
    const focusedText = await app.page.evaluate(() => {
      return document.activeElement?.textContent?.trim() ?? '';
    });
    expect(focusedText).toContain('beta');

    // Space to toggle the checkbox
    await app.page.keyboard.press('Space');

    // Tag should be selected
    await app.expectClipCount(0); // Filtering by beta with no clips tagged
  });
```

**Step 2: Run test to verify it fails**

Run: `cd e2e && npx playwright test tests/tags/ -g "arrow keys"`
Expected: FAIL

**Step 3: Add roving tabindex to tag filter list**

In `frontend/js/tags.js`, find `renderTagFilterDropdown()`. After the list is rendered, initialize roving tabindex:

```javascript
// At module level:
let tagFilterRover = null;

// Inside renderTagFilterDropdown(), after building the list:
if (tagFilterRover) tagFilterRover.destroy();
tagFilterRover = RovingTabindex.create({
    container: document.getElementById('tag-filter-list'),
    itemSelector: 'label',
    orientation: 'vertical',
    wrap: false,
    onActivate: (label) => {
        const checkbox = label.querySelector('input[type="checkbox"]');
        if (checkbox) {
            checkbox.checked = !checkbox.checked;
            checkbox.dispatchEvent(new Event('change', { bubbles: true }));
        }
    },
});
```

**Step 4: Run test**

Run: `cd e2e && npx playwright test tests/tags/ -g "arrow keys"`
Expected: PASS

**Step 5: Commit**

```bash
git add frontend/js/tags.js e2e/tests/tags/
git commit -m "feat: add roving tabindex to tag filter dropdown"
```

---

### Task 9: Watch folder list roving tabindex

**Files:**
- Modify: `frontend/js/watch.js` (add roving tabindex to folder list)
- Modify: `e2e/tests/watch/` (add keyboard tests)

**Step 1: Write failing test**

In the appropriate watch spec file, add:

```typescript
  test('should navigate watch folder cards with arrow keys', async ({ app }) => {
    // Switch to watch view
    await app.switchToWatchView();

    // Add two watch folders
    await app.addWatchFolder(folder1Path);
    await app.addWatchFolder(folder2Path);

    // Tab to the folder list
    await app.tabTo('#watch-folder-list > li');

    // Arrow down to second folder
    await app.page.keyboard.press('ArrowDown');

    // Second folder card should be focused
    const focusInList = await app.page.evaluate(() => {
      const cards = document.querySelectorAll('#watch-folder-list > li');
      return cards[1]?.contains(document.activeElement) || document.activeElement === cards[1];
    });
    expect(focusInList).toBe(true);
  });
```

**Step 2: Run test to verify it fails**

Run: `cd e2e && npx playwright test tests/watch/ -g "arrow keys"`
Expected: FAIL

**Step 3: Add roving tabindex to watch folder list**

In `frontend/js/watch.js`, find where folder cards are rendered. After rendering, initialize:

```javascript
let watchFolderRover = null;

// After rendering folder cards:
if (watchFolderRover) watchFolderRover.destroy();
const folderList = document.getElementById('watch-folder-list');
if (folderList && folderList.children.length > 0) {
    watchFolderRover = RovingTabindex.create({
        container: folderList,
        itemSelector: ':scope > li',
        orientation: 'vertical',
        wrap: false,
    });
}
```

Make each `li` focusable by ensuring it has `tabindex` set by the rover.

**Step 4: Run test**

Run: `cd e2e && npx playwright test tests/watch/ -g "arrow keys"`
Expected: PASS

**Step 5: Commit**

```bash
git add frontend/js/watch.js e2e/tests/watch/
git commit -m "feat: add roving tabindex to watch folder list"
```

---

### Task 10: Bulk toolbar roving tabindex

**Files:**
- Modify: `frontend/js/app.js` (add roving tabindex to bulk toolbar)
- Modify: `e2e/tests/bulk/` (add keyboard tests)

**Step 1: Write failing test**

In the appropriate bulk spec file, add:

```typescript
  test('should navigate bulk toolbar buttons with arrow keys', async ({ app }) => {
    const img1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
    const img2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
    await app.uploadFile(img1);
    await app.uploadFile(img2);
    await app.expectClipCount(2);

    // Select both clips to show bulk toolbar
    await app.selectAllClips();

    // Tab to bulk toolbar
    await app.tabTo('#bulk-toolbar button');

    // Arrow right to navigate through buttons
    await app.page.keyboard.press('ArrowRight');

    // Should be on next button
    const focusInToolbar = await app.page.evaluate(() => {
      const toolbar = document.getElementById('bulk-toolbar');
      return toolbar?.contains(document.activeElement) ?? false;
    });
    expect(focusInToolbar).toBe(true);
  });
```

**Step 2: Run test to verify it fails**

Run: `cd e2e && npx playwright test tests/bulk/ -g "arrow keys"`
Expected: FAIL

**Step 3: Add roving tabindex to bulk toolbar**

In `frontend/js/app.js`, find where the bulk toolbar is shown (when `selectedIds.size > 0`). Add:

```javascript
let bulkToolbarRover = null;

// When bulk toolbar becomes visible:
function updateBulkToolbar() {
    // ... existing toolbar visibility logic ...

    if (bulkToolbar.classList.contains('pointer-events-auto')) {
        if (!bulkToolbarRover) {
            bulkToolbarRover = RovingTabindex.create({
                container: bulkToolbar,
                itemSelector: 'button:not(.hidden)',
                orientation: 'horizontal',
                wrap: false,
            });
        } else {
            bulkToolbarRover.update();
        }
    } else {
        if (bulkToolbarRover) {
            bulkToolbarRover.destroy();
            bulkToolbarRover = null;
        }
    }
}
```

**Step 4: Run test**

Run: `cd e2e && npx playwright test tests/bulk/ -g "arrow keys"`
Expected: PASS

**Step 5: Commit**

```bash
git add frontend/js/app.js e2e/tests/bulk/
git commit -m "feat: add roving tabindex to bulk toolbar"
```

---

### Task 11: Plugin list roving tabindex

**Files:**
- Modify: `frontend/js/plugins.js` (add roving tabindex to plugin list)

**Step 1: Write failing test**

In `e2e/tests/plugins/` appropriate spec file, add:

```typescript
  test('should navigate plugin list with arrow keys', async ({ app }) => {
    // Install two plugins first
    await app.installPluginFromPath(plugin1Path);
    await app.installPluginFromPath(plugin2Path);

    // Open plugins modal
    await app.openPluginsModal();

    // Tab into the plugin list
    await app.tabTo('[data-testid="plugins-list"] > div');

    // Arrow down to next plugin
    await app.page.keyboard.press('ArrowDown');

    // Focus should be on second plugin card
    const focusInList = await app.page.evaluate(() => {
      const list = document.querySelector('[data-testid="plugins-list"]');
      return list?.contains(document.activeElement) ?? false;
    });
    expect(focusInList).toBe(true);
  });
```

**Step 2: Add roving tabindex to plugin list**

In `frontend/js/plugins.js`, after rendering plugin cards:

```javascript
let pluginListRover = null;

// After rendering plugin list:
if (pluginListRover) pluginListRover.destroy();
const pluginList = document.querySelector('[data-testid="plugins-list"]');
if (pluginList && pluginList.children.length > 0) {
    pluginListRover = RovingTabindex.create({
        container: pluginList,
        itemSelector: ':scope > div',
        orientation: 'vertical',
        wrap: false,
    });
}
```

**Step 3: Run test, commit**

```bash
git add frontend/js/plugins.js e2e/tests/plugins/
git commit -m "feat: add roving tabindex to plugin list"
```

---

### Task 12: Keyboard-only clip workflow tests

**Files:**
- Modify: `e2e/tests/clips/upload.spec.ts` or `e2e/tests/clips/delete.spec.ts` (add keyboard variants)

**Step 1: Write keyboard-only clip lifecycle test**

In `e2e/tests/clips/` (pick the appropriate spec file), add:

```typescript
  test('should delete clip via keyboard only', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    // Tab to gallery
    await app.tabTo('#gallery > li');
    expect(await app.getFocusedClipIndex()).toBe(0);

    // Press Delete/Backspace to trigger delete (if shortcut exists)
    // Or use the context menu via keyboard
    await app.page.keyboard.press('Backspace');

    // Confirm dialog should appear
    await expect(app.page.locator(selectors.confirm.dialog)).toHaveClass(/opacity-100/);

    // Cancel button should be focused (safe default)
    await expect(app.page.locator(selectors.confirm.cancelButton)).toBeFocused();

    // Tab to confirm button and press Enter
    await app.page.keyboard.press('Tab');
    await app.page.keyboard.press('Enter');

    // Clip should be deleted
    await app.expectClipCount(0);
  });

  test('should archive clip via keyboard only', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    // Tab to gallery, focus clip
    await app.tabTo('#gallery > li');
    expect(await app.getFocusedClipIndex()).toBe(0);

    // Press 'e' to archive (clip context shortcut)
    await app.page.keyboard.press('e');

    // Clip should be archived (removed from non-archive view)
    await app.expectClipCount(0);
  });
```

**Step 2: Write keyboard search-to-open workflow test**

In `e2e/tests/search/` appropriate spec file, add:

```typescript
  test('should search and open clip via keyboard only', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    const filename = path.basename(imagePath);

    // Press / to focus search
    await app.page.locator('body').click();
    await app.page.keyboard.press('/');
    await expect(app.page.locator(selectors.header.searchInput)).toBeFocused();

    // Type part of filename
    await app.page.keyboard.type(filename.substring(0, 8));

    // Tab to gallery
    await app.page.keyboard.press('Escape'); // Close search focus
    await app.tabTo('#gallery > li');

    // Enter to open lightbox
    await app.page.keyboard.press('Enter');
    expect(await app.isLightboxOpen()).toBe(true);

    // Escape to close
    await app.page.keyboard.press('Escape');
    await app.page.waitForFunction(() => {
      const lb = document.getElementById('lightbox');
      return !lb?.classList.contains('active');
    });
  });
```

**Step 3: Run these tests**

Run: `cd e2e && npx playwright test tests/clips/ tests/search/ -g "keyboard"`
Expected: PASS

**Step 4: Commit**

```bash
git add e2e/tests/clips/ e2e/tests/search/
git commit -m "test: add keyboard-only clip workflow e2e tests"
```

---

### Task 13: Final full test suite run and cleanup

**Step 1: Run the complete e2e test suite**

Run: `cd e2e && npm test`
Expected: All tests pass

**Step 2: If any tests fail, fix them**

Follow the red-green-refactor cycle: understand why the test fails, fix the minimum code needed, verify the fix.

**Step 3: Final commit if any fixes were needed**

```bash
git add -A
git commit -m "fix: resolve test regressions from keyboard navigation changes"
```
