# Hide Clip Controls in Non-Clip Views — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Hide clip-related header and bottom bar controls when viewing Watch or Serve views, since they serve no purpose outside the clips view.

**Architecture:** Add `id="clip-controls"` to the existing header wrapper div in `index.html`. In `switchView()` in `serve.js`, toggle `hidden` on that wrapper, the active-tags bar, and three bottom-bar elements. Close open popovers before hiding.

**Tech Stack:** Vanilla JS, HTML, Playwright e2e tests

**Spec:** `docs/superpowers/specs/2026-03-22-hide-clip-controls-design.md`

---

### Task 1: Add `id="clip-controls"` to header wrapper div

**Files:**
- Modify: `frontend/index.html:23`

- [ ] **Step 1: Add the ID**

In `frontend/index.html` line 23, add `id="clip-controls"` to the existing wrapper div:

```html
<!-- Before -->
<div class="flex-1 max-w-md w-full flex items-center gap-2" style="--wails-draggable: no-drag">

<!-- After -->
<div id="clip-controls" class="flex-1 max-w-md w-full flex items-center gap-2" style="--wails-draggable: no-drag">
```

- [ ] **Step 2: Add selector to selectors.ts**

In `e2e/helpers/selectors.ts`, add `clipControls` to the `header` section:

```typescript
header: {
    root: 'header',
    title: 'header h1',
    searchInput: '#search-input',
    drawerToggle: '#drawer-toggle-btn',
    addButton: '#add-btn',
    archiveButton: '#header-archive-btn',
    clipControls: '#clip-controls',  // <-- add this
},
```

- [ ] **Step 3: Commit**

```bash
git add frontend/index.html e2e/helpers/selectors.ts
git commit -m "feat: add id to header clip controls wrapper"
```

---

### Task 2: Toggle clip controls visibility in `switchView()`

**Files:**
- Modify: `frontend/js/serve.js:22-72` (the `switchView()` function)

- [ ] **Step 1: Write the failing e2e test**

Create `e2e/tests/clips/view-switch-controls.spec.ts`:

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

test.describe('View Switch Controls Visibility', () => {
  test('should hide clip controls in watch view', async ({ app }) => {
    // Controls visible in clips view
    await expect(app.page.locator('#clip-controls')).toBeVisible();
    await expect(app.page.locator(selectors.bottomBar.addButton)).toBeVisible();
    await expect(app.page.locator(selectors.bottomBar.expirySelect)).toBeVisible();
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toBeVisible();

    // Switch to watch view
    await app.openWatchView();

    // Controls should be hidden
    await expect(app.page.locator('#clip-controls')).toBeHidden();
    await expect(app.page.locator(selectors.bottomBar.addButton)).toBeHidden();
    await expect(app.page.locator(selectors.bottomBar.expirySelect)).toBeHidden();
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toBeHidden();
  });

  test('should restore clip controls when returning to clips view', async ({ app }) => {
    await app.openWatchView();
    await app.closeWatchView();

    await expect(app.page.locator('#clip-controls')).toBeVisible();
    await expect(app.page.locator(selectors.bottomBar.addButton)).toBeVisible();
    await expect(app.page.locator(selectors.bottomBar.expirySelect)).toBeVisible();
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toBeVisible();
  });

  test('should hide clip controls in serve view', async ({ app }) => {
    await app.switchToServeView();

    await expect(app.page.locator('#clip-controls')).toBeHidden();
    await expect(app.page.locator(selectors.bottomBar.addButton)).toBeHidden();
    await expect(app.page.locator(selectors.bottomBar.expirySelect)).toBeHidden();
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toBeHidden();
  });

  test('should hide active tags bar when switching away with filters active', async ({ app }) => {
    // Create a tag and apply it as a filter so active-tags-container becomes visible
    await app.createTag('filter-test');
    await app.filterByTag('filter-test');
    await expect(app.page.locator('#active-tags-container')).toBeVisible();

    // Switch to watch view — active tags bar should hide
    await app.openWatchView();
    await expect(app.page.locator('#active-tags-container')).toBeHidden();

    // Return to clips — active tags bar should reappear (filter still active)
    await app.closeWatchView();
    await expect(app.page.locator('#active-tags-container')).toBeVisible();
  });

  test('should close sort popover when switching views', async ({ app }) => {
    // Open the sort popover
    await app.openSortPopover();
    await expect(app.page.locator(selectors.sort.popover)).toBeVisible();

    // Switch to watch view — popover should close
    await app.openWatchView();
    await expect(app.page.locator(selectors.sort.popover)).toBeHidden();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd e2e && npx playwright test tests/clips/view-switch-controls.spec.ts --reporter=line 2>&1 | tail -30
```

Expected: FAIL — controls are still visible in watch/serve views.

- [ ] **Step 3: Implement the visibility toggle in `switchView()`**

In `frontend/js/serve.js`, modify the `switchView()` function. After the "Update legacy flags" block (after line 53), add the clip controls visibility toggle:

```javascript
// --- inside switchView(), after the legacy flags block ---

// Toggle clip-related controls visibility
const isClipView = (view === 'clips');
const clipControls = document.getElementById('clip-controls');
const activeTagsContainer = document.getElementById('active-tags-container');
const addBtn = document.getElementById('add-btn');
const expirySelect = document.getElementById('expiry-select');
const clipCount = document.getElementById('clip-count');

// Close open popovers before hiding (keyboard shortcuts bypass click-outside handlers)
if (!isClipView) {
    if (typeof closeSortPopover === 'function') closeSortPopover();
    if (typeof closeTagFilterDropdown === 'function') closeTagFilterDropdown(false);
}

if (clipControls) clipControls.classList.toggle('hidden', !isClipView);
if (addBtn) addBtn.classList.toggle('hidden', !isClipView);
if (expirySelect) expirySelect.classList.toggle('hidden', !isClipView);
if (clipCount) clipCount.classList.toggle('hidden', !isClipView);

// active-tags-container: only hide on departure, don't force-show on return.
// Its visibility is managed by updateActiveTagsDisplay() which runs during loadClips().
// Using toggle() would incorrectly show an empty container when no filters are active.
if (!isClipView && activeTagsContainer) activeTagsContainer.classList.add('hidden');
```

- [ ] **Step 4: Run the new test to verify it passes**

```bash
cd e2e && npx playwright test tests/clips/view-switch-controls.spec.ts --reporter=line 2>&1 | tail -30
```

Expected: PASS

- [ ] **Step 5: Run the full test suite to check for regressions**

```bash
cd e2e && npm test 2>&1 | tail -50
```

Expected: All tests pass. Watch for failures in `watch/`, `serve/`, `clips/` tests that may rely on controls being visible.

- [ ] **Step 6: Commit**

```bash
git add frontend/js/serve.js e2e/tests/clips/view-switch-controls.spec.ts
git commit -m "feat: hide clip controls in watch and serve views"
```

---

### Task 3: Verify edge cases and fix regressions

**Files:**
- Possibly modify: `frontend/js/serve.js`, `frontend/js/tags.js`, `frontend/js/ui.js`
- Test: `e2e/tests/clips/view-switch-controls.spec.ts`

- [ ] **Step 1: Add edge case test — filter state preserved across view switch**

Add to `e2e/tests/clips/view-switch-controls.spec.ts`:

```typescript
test('should preserve search text across view switch', async ({ app }) => {
    // Type search text
    await app.page.locator(selectors.header.searchInput).fill('test-query');

    // Switch to watch and back
    await app.openWatchView();
    await app.closeWatchView();

    // Search text should be preserved
    const value = await app.page.locator(selectors.header.searchInput).inputValue();
    expect(value).toBe('test-query');
});
```

- [ ] **Step 2: Run the edge case test**

```bash
cd e2e && npx playwright test tests/clips/view-switch-controls.spec.ts --reporter=line 2>&1 | tail -30
```

Expected: PASS (search state is just hidden, not cleared).

- [ ] **Step 3: Run full test suite one final time**

```bash
cd e2e && npm test 2>&1 | tail -50
```

Expected: All tests pass.

- [ ] **Step 4: Commit if any fixes were needed**

```bash
git add -A
git commit -m "test: add edge case tests for view-switch control visibility"
```
