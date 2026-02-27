# Bottom Bar Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Move the Add button and expiry selector from the header into a fixed bottom bar, rename element IDs to generic names, add a clip count indicator, and update all tests.

**Architecture:** A new `<footer>` element fixed to the viewport bottom mirrors the header's styling. The existing bulk toolbar and task queue bar shift up to clear it. Clip count is updated from `loadClips()`.

**Tech Stack:** HTML, Tailwind CSS, vanilla JS, Playwright e2e tests

---

### Task 1: Add the bottom bar HTML and remove elements from header

**Files:**
- Modify: `frontend/index.html:19-98` (header section)
- Modify: `frontend/index.html:196` (file input)
- Modify: `frontend/index.html:212` (main element)
- Modify: `frontend/index.html:260` (bulk toolbar)
- Modify: `frontend/index.html:353` (after main close tag)
- Modify: `frontend/index.html:357` (toast notification)
- Modify: `frontend/index.html:1051` (task queue bar)

**Step 1: Remove the Add button and Expiry select from the header**

In `frontend/index.html`, delete lines 78-98 (the `<!-- Add File Button -->` button and `<!-- Expiration Dropdown -->` select). These elements move to the new footer.

Before:
```html
            <!-- Add File Button -->
            <button id="header-add-btn"
                class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-500 text-xs font-medium py-2 px-3 rounded-md transition-colors"
                aria-label="Add files"
                style="--wails-draggable: no-drag">
                + Add
            </button>

            <!-- Expiration Dropdown -->
            <select id="upload-expiry-select"
                ...
            </select>

            <!-- Hamburger Menu Button -->
```

After (only hamburger menu button remains):
```html
            <!-- Hamburger Menu Button -->
```

**Step 2: Move the hidden file input from its current location to just before the new footer**

Delete the file input at line 196:
```html
    <input type="file" id="file-input" class="hidden" multiple aria-hidden="true">
```

It will be placed inside the new footer in step 4.

**Step 3: Add `pb-14` to `<main>` to clear the fixed bottom bar**

Change line 212 from:
```html
    <main class="max-w-7xl mx-auto p-5">
```
to:
```html
    <main class="max-w-7xl mx-auto p-5 pb-14">
```

**Step 4: Add the bottom bar footer after `</main>`**

Insert the following after the closing `</main>` tag (line 353):

```html
    <!-- Bottom Bar -->
    <input type="file" id="file-input" class="hidden" multiple aria-hidden="true">
    <footer id="bottom-bar"
        class="fixed bottom-0 left-0 right-0 z-40 bg-stone-50 border-t border-stone-200/60"
        style="--wails-draggable: drag">
        <div class="max-w-7xl mx-auto px-5 py-2.5 flex items-center gap-3">
            <button id="add-btn"
                class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-500 text-xs font-medium py-2 px-3 rounded-md transition-colors"
                aria-label="Add files"
                style="--wails-draggable: no-drag">
                + Add
            </button>
            <select id="expiry-select"
                class="border border-stone-200 hover:border-stone-300 text-stone-500 text-xs font-medium py-2 px-2 rounded-md transition-colors bg-white focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20"
                aria-label="Upload expiration"
                style="--wails-draggable: no-drag"
                title="Set expiration for new uploads">
                <option value="0">No Expiry</option>
                <option value="15">15m</option>
                <option value="60">1h</option>
                <option value="360">6h</option>
                <option value="1440">24h</option>
                <option value="10080">7d</option>
            </select>
            <span id="clip-count" class="ml-auto text-xs font-medium text-stone-400"
                style="--wails-draggable: no-drag"></span>
        </div>
    </footer>
```

**Step 5: Shift the bulk toolbar up to clear the bottom bar**

Change the bulk toolbar `bottom-5` to `bottom-16` in its class list (line 260):

From:
```html
class="fixed bottom-5 left-1/2 ...
```
To:
```html
class="fixed bottom-16 left-1/2 ...
```

**Step 6: Shift the toast notification up to clear the bottom bar**

Change toast `bottom-4` to `bottom-16` (line 357):

From:
```html
class="fixed bottom-4 right-4 ...
```
To:
```html
class="fixed bottom-16 right-4 ...
```

**Step 7: Shift the task queue bar up above the bottom bar**

The task queue bar at line 1051 currently uses `fixed bottom-0`. Change it to `bottom-12` so it sits above the bottom bar:

From:
```html
class="fixed bottom-0 left-0 right-0 z-40 ...
```
To:
```html
class="fixed bottom-12 left-0 right-0 z-40 ...
```

**Step 8: Verify the HTML changes render correctly**

Run: `cd /Users/egecan/Code/mahpastes && make dev`

Visually confirm:
- Header no longer has Add button or expiry selector
- Bottom bar appears at bottom with Add button, expiry dropdown, and empty clip count
- Bulk toolbar (when clips selected) floats above the bottom bar
- Toast notifications appear above the bottom bar

**Step 9: Commit**

```bash
git add frontend/index.html
git commit -m "feat: add fixed bottom bar with add button and expiry selector"
```

---

### Task 2: Update JS element references and add clip count logic

**Files:**
- Modify: `frontend/js/app.js:59,67,117,120,243,519`
- Modify: `frontend/js/wails-api.js:4-38`

**Step 1: Update element ID references in app.js**

In `frontend/js/app.js`, update the cached element references:

Line 59 — keep as-is (file-input ID unchanged):
```javascript
const fileInput = document.getElementById('file-input');
```

Line 67 — rename `header-add-btn` to `add-btn`:
```javascript
const headerAddBtn = document.getElementById('add-btn');
```

Line 117 — rename `upload-expiry-select` to `expiry-select`:
```javascript
const uploadExpirySelect = document.getElementById('expiry-select');
```

Add a new cached element after `uploadExpirySelect` (around line 118):
```javascript
const clipCountEl = document.getElementById('clip-count');
```

**Step 2: Add `updateClipCount` function in app.js**

Add after the `getUploadExpirationMinutes` function (around line 122):

```javascript
function updateClipCount(count) {
    if (clipCountEl) {
        clipCountEl.textContent = count === 1 ? '1 clip' : `${count} clips`;
    }
}
```

**Step 3: Call `updateClipCount` from `loadClips` in wails-api.js**

In `frontend/js/wails-api.js`, inside the `loadClips` function:

After the loop that creates clip cards (after line 22), add the count update:

```javascript
        if (clips && clips.length > 0) {
            for (const clip of clips) {
                await createClipCard(clip);
            }
            updateClipCount(clips.length);
        } else {
            // ... empty msg handling ...
            updateClipCount(0);
        }
```

Add `updateClipCount(0)` in the else branch as well (after line 32).

**Step 4: Verify JS changes work**

Run: `cd /Users/egecan/Code/mahpastes && make dev`

Confirm:
- Add button in bottom bar opens file picker
- Expiry selector in bottom bar works
- Clip count shows correct number (e.g., "3 clips", "1 clip", "0 clips")
- Clip count updates after upload, delete, archive operations

**Step 5: Commit**

```bash
git add frontend/js/app.js frontend/js/wails-api.js
git commit -m "feat: update JS refs for bottom bar and add clip count"
```

---

### Task 3: Update e2e test selectors and fixtures

**Files:**
- Modify: `e2e/helpers/selectors.ts:11`
- Modify: `e2e/fixtures/test-fixtures.ts:738`

**Step 1: Update the `addButton` selector**

In `e2e/helpers/selectors.ts`, line 11, change:
```typescript
    addButton: '#header-add-btn',
```
to:
```typescript
    addButton: '#add-btn',
```

Also add a new `bottomBar` section to selectors after the `header` section:
```typescript
  // Bottom bar
  bottomBar: {
    root: '#bottom-bar',
    addButton: '#add-btn',
    expirySelect: '#expiry-select',
    clipCount: '#clip-count',
  },
```

**Step 2: Update the expiry select ID in test fixtures**

In `e2e/fixtures/test-fixtures.ts`, line 738, change:
```typescript
      const expirySelect = document.getElementById('upload-expiry-select') as HTMLSelectElement;
```
to:
```typescript
      const expirySelect = document.getElementById('expiry-select') as HTMLSelectElement;
```

**Step 3: Run e2e tests to verify nothing is broken**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`

All existing tests should pass with the updated selectors.

**Step 4: Commit**

```bash
git add e2e/helpers/selectors.ts e2e/fixtures/test-fixtures.ts
git commit -m "test: update selectors for bottom bar element IDs"
```

---

### Task 4: Add e2e test for bottom bar and clip count

**Files:**
- Create: `e2e/tests/clips/bottom-bar.spec.ts`

**Step 1: Write the bottom bar test**

Create `e2e/tests/clips/bottom-bar.spec.ts`:

```typescript
import { test } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import { createTempFile } from '../../helpers/test-data';
import { generateTestImage } from '../../helpers/test-data';

test.describe('Bottom Bar', () => {
  test('should display the bottom bar with add button and expiry selector', async ({ app }) => {
    const page = app.page;
    await page.waitForSelector(selectors.bottomBar.root);
    await app.expectVisible(selectors.bottomBar.addButton);
    await app.expectVisible(selectors.bottomBar.expirySelect);
  });

  test('should show correct clip count after uploading', async ({ app }) => {
    const page = app.page;

    // Initially 0 clips
    await page.waitForSelector(selectors.bottomBar.clipCount);
    await app.expectText(selectors.bottomBar.clipCount, '0 clips');

    // Upload one file
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);
    await app.expectText(selectors.bottomBar.clipCount, '1 clip');

    // Upload another
    const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
    await app.uploadFile(imagePath2);
    await app.expectClipCount(2);
    await app.expectText(selectors.bottomBar.clipCount, '2 clips');
  });

  test('should update clip count after deleting', async ({ app }) => {
    const page = app.page;
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);
    await app.expectText(selectors.bottomBar.clipCount, '1 clip');

    await app.deleteClip(require('path').basename(imagePath));
    await app.expectClipCount(0);
    await app.expectText(selectors.bottomBar.clipCount, '0 clips');
  });
});
```

**Step 2: Check that AppHelper has `expectText` and `expectVisible` methods**

If `expectText` doesn't exist in `e2e/fixtures/test-fixtures.ts`, add it:

```typescript
async expectText(selector: string, text: string) {
    await expect(this.page.locator(selector)).toHaveText(text);
}
```

If `expectVisible` doesn't exist, add it:
```typescript
async expectVisible(selector: string) {
    await expect(this.page.locator(selector)).toBeVisible();
}
```

**Step 3: Run the new test**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/clips/bottom-bar.spec.ts`

All 3 tests should pass.

**Step 4: Run the full test suite**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`

All tests should pass.

**Step 5: Commit**

```bash
git add e2e/tests/clips/bottom-bar.spec.ts e2e/fixtures/test-fixtures.ts
git commit -m "test: add e2e tests for bottom bar and clip count"
```
