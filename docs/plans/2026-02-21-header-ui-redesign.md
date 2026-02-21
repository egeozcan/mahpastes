# Header UI Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Surface Add and Archive buttons directly in the header, and add a "Back to Pastes" button in the Watch view.

**Architecture:** Pure frontend changes — add HTML elements, wire event handlers, sync state between header and drawer archive buttons. No backend changes.

**Tech Stack:** Vanilla JS, Tailwind CSS, Playwright e2e tests

---

### Task 1: Add HTML elements to index.html

**Files:**
- Modify: `frontend/index.html:23-65` (search group — add archive button)
- Modify: `frontend/index.html:67-76` (before hamburger — add "Add" button)
- Modify: `frontend/index.html:170-171` (watch view — add back button)

**Step 1: Add archive icon button after the tag filter button, inside the search group div**

In `frontend/index.html`, after the tag filter `</div>` (closing the `relative` div around tag-filter-btn, line 63) and before the closing `</div>` of the search flex group (line 65), insert:

```html
<!-- Archive Toggle Button -->
<button id="header-archive-btn"
    class="relative border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-500 p-2 rounded-md transition-colors"
    aria-label="Toggle archive view" aria-pressed="false"
    style="--wails-draggable: no-drag">
    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
            d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4"></path>
    </svg>
</button>
```

**Step 2: Add "Add" button between the search group and the hamburger button**

In `frontend/index.html`, after the closing `</div>` of the search flex group (line 65) and before the hamburger button comment (line 67), insert:

```html
<!-- Add File Button -->
<button id="header-add-btn"
    class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-500 text-xs font-medium py-2 px-3 rounded-md transition-colors"
    aria-label="Add files"
    style="--wails-draggable: no-drag">
    + Add
</button>
```

**Step 3: Add "Back to Pastes" button at the top of the watch view**

In `frontend/index.html`, after the `<h2 id="watch-heading">` (line 172) and before the global controls div (line 174), insert:

```html
<!-- Back to Pastes -->
<button id="watch-back-btn"
    class="text-xs font-medium text-stone-500 hover:text-stone-700 transition-colors flex items-center gap-1.5 mb-4"
    style="--wails-draggable: no-drag">
    <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M15 19l-7-7 7-7"></path>
    </svg>
    Back to Pastes
</button>
```

**Step 4: Run tests to confirm nothing is broken by HTML-only changes**

Run: `cd e2e && npm test`
Expected: All tests pass (new elements are inert — no JS wiring yet)

**Step 5: Commit**

```bash
git add frontend/index.html
git commit -m "feat: add header archive, add, and watch back buttons (HTML only)"
```

---

### Task 2: Wire up the Add button and header archive button in app.js

**Files:**
- Modify: `frontend/js/app.js:40-44` (element declarations — add new references)
- Modify: `frontend/js/app.js:199-200` (event listeners — add new handlers)

**Step 1: Add element references at the top of app.js**

In `frontend/js/app.js`, after line 44 (`const archiveBtnText = ...`), add:

```javascript
const headerArchiveBtn = document.getElementById('header-archive-btn');
const headerAddBtn = document.getElementById('header-add-btn');
```

**Step 2: Add event listeners**

In `frontend/js/app.js`, after line 200 (`toggleArchiveViewBtn.addEventListener('click', toggleViewMode);`), add:

```javascript
headerArchiveBtn.addEventListener('click', toggleViewMode);
headerAddBtn.addEventListener('click', () => fileInput.click());
```

**Step 3: Run tests**

Run: `cd e2e && npm test`
Expected: All tests pass. The header archive button now works but its visual state doesn't sync yet (that's Task 3).

**Step 4: Commit**

```bash
git add frontend/js/app.js
git commit -m "feat: wire up header add and archive button event handlers"
```

---

### Task 3: Sync header archive button visual state in ui.js

**Files:**
- Modify: `frontend/js/ui.js` — `toggleViewMode()` function

**Step 1: Update toggleViewMode() to sync the header archive button**

In `frontend/js/ui.js`, the `toggleViewMode()` function currently updates `toggleArchiveViewBtn` (the drawer button). Add matching logic for `headerArchiveBtn`.

After the existing block that sets the drawer archive button state (around line 836-844), add the same logic for the header button. Inside the `if (isViewingArchive)` branch:

```javascript
headerArchiveBtn.setAttribute('aria-pressed', 'true');
headerArchiveBtn.classList.add('bg-stone-800', 'text-white', 'border-stone-800');
headerArchiveBtn.classList.remove('border-stone-200', 'text-stone-500', 'hover:border-stone-300', 'hover:bg-stone-100');
```

And in the `else` branch:

```javascript
headerArchiveBtn.setAttribute('aria-pressed', 'false');
headerArchiveBtn.classList.remove('bg-stone-800', 'text-white', 'border-stone-800');
headerArchiveBtn.classList.add('border-stone-200', 'text-stone-500', 'hover:border-stone-300', 'hover:bg-stone-100');
```

Note: `headerArchiveBtn` is declared in `app.js` which loads before `ui.js`, so it's accessible as a global.

**Step 2: Run tests**

Run: `cd e2e && npm test`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add frontend/js/ui.js
git commit -m "feat: sync header archive button visual state on toggle"
```

---

### Task 4: Wire up watch "Back to Pastes" button in watch.js

**Files:**
- Modify: `frontend/js/watch.js` — add element ref and listener

**Step 1: Add element reference and event listener**

In `frontend/js/watch.js`, after line 17 (`const addFolderBtn = ...`), add:

```javascript
const watchBackBtn = document.getElementById('watch-back-btn');
```

After the `toggleWatchViewBtn.addEventListener` block (which is later in the file), add:

```javascript
watchBackBtn.addEventListener('click', toggleWatchView);
```

Note: We need to find where `toggleWatchViewBtn`'s click listener is registered. Looking at the code, `toggleWatchViewBtn.addEventListener('click', toggleWatchView)` is registered somewhere in watch.js. Add the watchBackBtn listener right after it.

**Step 2: Run tests**

Run: `cd e2e && npm test`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add frontend/js/watch.js
git commit -m "feat: wire up watch view back-to-pastes button"
```

---

### Task 5: Update e2e test selectors and fixtures

**Files:**
- Modify: `e2e/helpers/selectors.ts` — add new selectors
- Modify: `e2e/fixtures/test-fixtures.ts` — update `fastReset()`, update `toggleArchiveView()`

**Step 1: Add selectors for new elements**

In `e2e/helpers/selectors.ts`, in the `header` section (around line 6-11), add:

```typescript
addButton: '#header-add-btn',
archiveButton: '#header-archive-btn',
```

In the `watch` section (around line 134-144), add:

```typescript
backButton: '#watch-back-btn',
```

**Step 2: Update fastReset() to reset header archive button**

In `e2e/fixtures/test-fixtures.ts`, inside the `fastReset()` method's `page.evaluate`, after the block that resets the drawer archive button (around line 658-666), add:

```typescript
// Reset header archive button UI
const headerArchiveBtn = document.getElementById('header-archive-btn');
if (headerArchiveBtn) {
    headerArchiveBtn.setAttribute('aria-pressed', 'false');
    headerArchiveBtn.classList.remove('bg-stone-800', 'text-white', 'border-stone-800');
    headerArchiveBtn.classList.add('border-stone-200', 'text-stone-500', 'hover:border-stone-300', 'hover:bg-stone-100');
}
```

**Step 3: Update toggleArchiveView() to use the header button instead of the drawer**

In `e2e/fixtures/test-fixtures.ts`, the `toggleArchiveView()` method (around line 1074-1079) currently opens the drawer and clicks the drawer archive button. Update it to use the header button instead (faster, no drawer interaction):

```typescript
async toggleArchiveView(): Promise<void> {
    await this.page.locator(selectors.header.archiveButton).click();
    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });
}
```

**Step 4: Run tests**

Run: `cd e2e && npm test`
Expected: All tests pass with the new selectors and fixture changes.

**Step 5: Commit**

```bash
git add e2e/helpers/selectors.ts e2e/fixtures/test-fixtures.ts
git commit -m "test: update selectors and fixtures for header UI changes"
```

---

### Task 6: Add e2e tests for new header buttons

**Files:**
- Create: `e2e/tests/clips/header-buttons.spec.ts`

**Step 1: Write tests for the Add button and header archive button**

```typescript
import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
import { createTempFile, generateTestImage } from '../../helpers/test-data.js';
import * as path from 'path';

test.describe('Header Buttons', () => {
  test('Add button triggers file input', async ({ app, page }) => {
    // The Add button should trigger the hidden file input
    const fileInput = page.locator(selectors.upload.fileInput);

    // Create a test image and upload via the file input that Add button triggers
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await fileInput.setInputFiles(imagePath);
    await app.expectClipCount(1);
  });

  test('Header archive button toggles archive view', async ({ app, page }) => {
    // Upload a clip then archive it
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.archiveClip(path.basename(imagePath));

    // Click header archive button
    await page.locator(selectors.header.archiveButton).click();
    await page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });

    // Should see the archived clip
    await app.expectClipCount(1);

    // Button should show active state
    const pressed = await page.locator(selectors.header.archiveButton).getAttribute('aria-pressed');
    expect(pressed).toBe('true');

    // Click again to go back
    await page.locator(selectors.header.archiveButton).click();
    await page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });

    // Should be back in active view (0 clips since we archived it)
    await app.expectClipCount(0);

    const pressedAfter = await page.locator(selectors.header.archiveButton).getAttribute('aria-pressed');
    expect(pressedAfter).toBe('false');
  });

  test('Watch view back button returns to clips view', async ({ app, page }) => {
    // Open watch view
    await app.openWatchView();
    expect(await app.isWatchViewOpen()).toBe(true);

    // Click back button
    await page.locator(selectors.watch.backButton).click();

    // Watch view should be hidden
    await page.waitForFunction(
      (sel) => document.querySelector(sel)?.classList.contains('hidden'),
      selectors.watch.view,
      { timeout: 5000 }
    );
    expect(await app.isWatchViewOpen()).toBe(false);
  });
});
```

**Step 2: Run the new tests**

Run: `cd e2e && npm test -- tests/clips/header-buttons.spec.ts`
Expected: All 3 tests pass.

**Step 3: Run full test suite**

Run: `cd e2e && npm test`
Expected: All tests pass.

**Step 4: Commit**

```bash
git add e2e/tests/clips/header-buttons.spec.ts
git commit -m "test: add e2e tests for header add, archive, and watch back buttons"
```

---

### Task 7: Final verification

**Step 1: Run full test suite one more time**

Run: `cd e2e && npm test`
Expected: All tests pass.

**Step 2: Visual check (optional)**

Run: `make dev`
Verify:
- Header shows: `[mahpastes] [search | tag-filter | archive] [+ Add] [hamburger]`
- Clicking "+ Add" opens file picker
- Clicking archive icon toggles archive view with visual state change
- Archive button in drawer still works
- Watch view shows "Back to Pastes" at top
- Clicking "Back to Pastes" returns to clips view
