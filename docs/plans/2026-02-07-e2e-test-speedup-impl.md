# E2E Test Speedup Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Eliminate all ~140 `waitForTimeout` calls, fix 2 flaky watch tests, enable headless mode, and reduce test runtime from ~30 min to ~5-10 min.

**Architecture:** Replace hardcoded sleeps with proper Playwright assertions (waitFor, expect.poll, toBeVisible). Fix flaky watch tests by polling DB before asserting UI. Remove serial mode where possible.

**Tech Stack:** Playwright, TypeScript, Wails e2e fixtures

---

### Task 1: Enable Headless Mode in Playwright Config

**Files:**
- Modify: `e2e/playwright.config.ts`

**Step 1: Add headless: true to config**

In `playwright.config.ts`, the `use` block currently has no `headless` setting (defaults vary). Explicitly set it:

```typescript
use: {
  headless: true,
  trace: 'on-first-retry',
  screenshot: 'only-on-failure',
  video: 'retain-on-failure',
  actionTimeout: 10000,
  navigationTimeout: 30000,
},
```

**Step 2: Run tests to verify headless works**

Run: `cd e2e && npx playwright test --project=chromium tests/clips/upload.spec.ts`
Expected: Tests run without browser windows appearing.

**Step 3: Commit**

```bash
git add e2e/playwright.config.ts
git commit -m "perf(e2e): enable headless mode by default"
```

---

### Task 2: Fix Fixture Helpers — Remove Sleeps from Core Operations

**Files:**
- Modify: `e2e/fixtures/test-fixtures.ts`

This is the largest task. Replace all ~46 `waitForTimeout` calls with proper waits. Group by category:

**Step 1: Fix `waitForReady()` — remove 100ms sleep (line 70)**

Replace `await this.page.waitForTimeout(100);` with nothing — the `page.evaluate` above already waited for the UI to initialize.

**Step 2: Fix `uploadFile()` / `uploadFiles()` / `pasteText()` — replace 1000ms sleeps (lines 86, 97, 111)**

Replace each `await this.page.waitForTimeout(1000);` with:
```typescript
// Wait for clip to appear in gallery
await this.page.locator(selectors.gallery.clipCard).first().waitFor({ state: 'visible', timeout: 10000 });
```

**Step 3: Fix `refreshClips()` — replace 500ms sleep (line 124)**

Remove `await this.page.waitForTimeout(500);` — the `waitForReady()` call on the next line already handles this.

**Step 4: Fix `deleteAllClips()` — replace 500ms sleep (line 234)**

Replace `await this.page.waitForTimeout(500);` with:
```typescript
// Wait for gallery to be empty before reloading
await expect(this.page.locator(selectors.gallery.clipCard)).toHaveCount(0, { timeout: 5000 });
```

The reload + waitForReady that follows will handle the rest.

**Step 5: Fix `closeAllModalsSafe()` — remove all 100ms sleeps (lines 266, 276, 290, 300, 310, 321, 331, 342)**

Each modal close method (`closeLightbox`, `closeImageEditor`, etc.) already waits for the modal to be hidden. Remove all 8 `await this.page.waitForTimeout(100);` calls after modal close operations.

**Step 6: Fix `resetUIState()` — remove 100ms sleep (line 439)**

Remove `await this.page.waitForTimeout(100);` after clearing search input — Playwright auto-waits for input clearing.

**Step 7: Fix `openWatchView()` — remove 100ms sleep (line 700)**

Remove `await this.page.waitForTimeout(100);` — the `waitForSelector` above already waits for the view to render.

**Step 8: Fix `toggleGlobalWatch()` — replace 300ms sleep (line 745)**

Replace `await this.page.waitForTimeout(300);` with:
```typescript
// Wait for global watch state to be reflected in UI
await expect(this.page.locator(selectors.watch.globalLabel)).toContainText(
  enabled ? /active/i : /paused/i, { timeout: 5000 }
);
```

**Step 9: Fix `addWatchFolder()` — replace 300ms sleep (line 780)**

Replace `await this.page.waitForTimeout(300);` with:
```typescript
// Wait for folder count to update
await expect(this.page.locator(selectors.watch.folderCard)).not.toHaveCount(0, { timeout: 5000 });
```

**Step 10: Fix `deleteAllWatchFolders()` — replace 300ms sleep (line 807)**

Remove `await this.page.waitForTimeout(300);` — the evaluate above is synchronous, no UI to wait for.

**Step 11: Fix `search()` and `clearSearch()` — replace 300ms sleeps (lines 830, 835)**

Replace `await this.page.waitForTimeout(300);` in both with:
```typescript
// Wait for gallery to re-render after search
await this.page.waitForFunction(() => {
  // @ts-ignore
  return window.__appReady === true;
}, { timeout: 5000 });
```

**Step 12: Fix `toggleArchiveView()` — replace 300ms sleep (line 840)**

Replace `await this.page.waitForTimeout(300);` with:
```typescript
// Wait for archive button state to change
const btn = this.page.locator(selectors.header.archiveButton);
await expect(btn).toHaveAttribute('aria-pressed', /.+/, { timeout: 5000 });
```

**Step 13: Fix `createTag()` and `deleteTag()` — replace 300ms sleeps (lines 948, 962)**

Remove both `await this.page.waitForTimeout(300);` — the `page.evaluate` calls are already awaited and complete synchronously.

**Step 14: Fix `addTagToClip()` and `removeTagFromClip()` — replace 500ms sleeps (lines 1005, 1041)**

Replace each with:
```typescript
// Wait for UI to refresh after tag operation
await this.page.waitForFunction(() => {
  // @ts-ignore
  return window.__appReady === true;
}, { timeout: 5000 });
```

**Step 15: Fix `filterByTag()`, `filterByTags()`, `clearTagFilters()` — replace 500ms sleeps (lines 1090, 1119, 1134)**

Replace each with:
```typescript
// Wait for gallery to re-render after filter change
await this.page.waitForFunction(() => {
  // @ts-ignore
  return window.__appReady === true;
}, { timeout: 5000 });
```

**Step 16: Fix `deleteAllTags()` — replace 300ms sleep (line 1246)**

Remove `await this.page.waitForTimeout(300);` — synchronous API call.

**Step 17: Fix `importPlugin()` — replace 500ms sleep (line 1303)**

Remove `await this.page.waitForTimeout(500);` — the evaluate call already completed the import.

**Step 18: Fix `enablePlugin()`, `disablePlugin()`, `removePlugin()` — replace 300ms sleeps (lines 1374, 1386, 1398)**

Remove all three `await this.page.waitForTimeout(300);` — API calls are synchronous.

**Step 19: Fix `deleteAllPlugins()` — replace 300ms sleep (line 1429)**

Remove `await this.page.waitForTimeout(300);` — synchronous API call.

**Step 20: Fix `openPluginsModal()` — replace 500ms sleep (line 1457)**

Replace `await this.page.waitForTimeout(500);` with:
```typescript
// Wait for plugin list to load (either plugin cards or empty state visible)
await expect(
  this.page.locator(`${selectors.plugins.list} > li, ${selectors.plugins.emptyState}`).first()
).toBeVisible({ timeout: 5000 });
```

**Step 21: Fix `togglePluginViaUI()` — replace 500ms sleep (line 1481)**

Replace `await this.page.waitForTimeout(500);` with:
```typescript
// Wait for plugin state to change
await expect(this.page.locator(selectors.plugins.pluginCard(pluginId))).toBeVisible({ timeout: 5000 });
```

**Step 22: Fix `removePluginViaUI()` — replace 200ms sleep (line 1488)**

Replace `await this.page.waitForTimeout(200);` with:
```typescript
// Wait for expanded section to appear
await expect(this.page.locator(selectors.plugins.pluginRemove(pluginId))).toBeVisible({ timeout: 5000 });
```

**Step 23: Fix `closeCardMenu()` — replace 100ms sleep (line 1557)**

Replace `await this.page.waitForTimeout(100);` with:
```typescript
// Wait for menu to close
await this.page.locator(selectors.cardMenu.dropdown).waitFor({ state: 'hidden', timeout: 5000 });
```

**Step 24: Fix lightbox plugin menu waits — replace 200ms sleeps (lines 1586, 1604)**

Replace each `await this.page.waitForTimeout(200);` with:
```typescript
// Wait for plugin menu to appear
await this.page.locator(selectors.lightbox.pluginMenu).waitFor({ state: 'visible', timeout: 5000 });
```

**Step 25: Fix `waitForPluginStorage()` and `waitForPluginStorageContains()` — replace 100ms polling sleeps (lines 1348, 1360)**

Replace the manual polling loops with `expect.poll()`:

```typescript
async waitForPluginStorage(pluginId: number, key: string, expectedValue: string, timeout = 5000): Promise<boolean> {
  try {
    await expect.poll(
      async () => this.getPluginStorage(pluginId, key),
      { timeout, intervals: [100, 200, 500] }
    ).toBe(expectedValue);
    return true;
  } catch {
    return false;
  }
}

async waitForPluginStorageContains(pluginId: number, key: string, substring: string, timeout = 5000): Promise<boolean> {
  try {
    await expect.poll(
      async () => this.getPluginStorage(pluginId, key),
      { timeout, intervals: [100, 200, 500] }
    ).toContain(substring);
    return true;
  } catch {
    return false;
  }
}
```

**Step 26: Fix `bulkAddTag()` and `bulkRemoveTag()` — replace 500ms sleeps (lines 1194, 1230)**

Replace each with:
```typescript
await this.page.waitForFunction(() => {
  // @ts-ignore
  return window.__appReady === true;
}, { timeout: 5000 });
```

**Step 27: Fix `restoreBackupViaAPI()` — replace 1000ms sleep (line 1691)**

Replace `await this.page.waitForTimeout(1000);` with:
```typescript
// Wait for restore to complete (just need the reload below to work)
```
Then rely on the `page.reload()` + `waitForReady()` that follows.

**Step 28: Run tests to verify fixtures still work**

Run: `cd e2e && npx playwright test --project=chromium tests/clips/upload.spec.ts tests/clips/delete.spec.ts`
Expected: PASS

**Step 29: Commit**

```bash
git add e2e/fixtures/test-fixtures.ts
git commit -m "perf(e2e): replace all waitForTimeout in fixtures with proper Playwright waits"
```

---

### Task 3: Fix Flaky Watch Folder Tests

**Files:**
- Modify: `e2e/tests/watch/import.spec.ts`
- Modify: `e2e/tests/tags/tag-watch-folder.spec.ts`

**Step 1: Fix `watch/import.spec.ts` — remove serial mode and fix all waits**

Remove `test.describe.configure({ mode: 'serial' });` (line 11).

For the first test "should auto-import image file created in watched folder" (lines 15-31):
- Replace `await app.page.waitForTimeout(1000);` (line 22) with `await app.page.waitForTimeout(500);` (legitimate fsnotify init wait)
- Replace `await app.waitForClipCount(1, 30000);` (line 30) with DB polling + UI assert:
```typescript
await expect.poll(
  async () => app.getClipCountFromDB(),
  { timeout: 30000, intervals: [500, 1000, 2000], message: 'Waiting for watch import' }
).toBeGreaterThanOrEqual(1);
await app.refreshClips();
await app.expectClipCount(1);
```

Apply the same pattern to ALL `waitForClipCount(N, 30000)` calls in this file.

For file-between delays: keep `await app.page.waitForTimeout(100);` (line 66) since it's needed for fsnotify between rapid file writes.

For "Paused Watch" tests (lines 245-279): replace `await app.page.waitForTimeout(1500);` with `await app.page.waitForTimeout(500);` — just need a short wait to confirm nothing happened, 1500 is excessive.

For "Process Existing" test (line 217): replace `await app.page.waitForTimeout(2000);` with DB polling:
```typescript
await expect.poll(
  async () => app.getClipCountFromDB(),
  { timeout: 10000, intervals: [500, 1000] }
).toBeGreaterThanOrEqual(0);
```

For "should not process existing files" (line 238): replace `await app.page.waitForTimeout(1000);` with `await app.page.waitForTimeout(500);`.

**Step 2: Fix `tags/tag-watch-folder.spec.ts` — remove serial mode and fix all waits**

Remove `test.describe.configure({ mode: 'serial' });` (line 7).

For "should auto-tag files imported from watch folder" (line 112):
- Replace `await app.page.waitForTimeout(500);` (line 136) → remove (API call is synchronous)
- Replace `await app.page.waitForTimeout(1000);` (line 145) → `await app.page.waitForTimeout(500);` (fsnotify init)
- Replace `await app.waitForClipCount(1, 30000);` (line 153) with DB polling pattern (same as Task 3 Step 1)
- Replace `await app.page.waitForTimeout(500);` (line 160) → remove (loadClips above is awaited)

Apply similar pattern to all other tests in this file.

**Step 3: Run watch tests to verify fixes**

Run: `cd e2e && npx playwright test tests/watch/import.spec.ts tests/tags/tag-watch-folder.spec.ts --retries=2`
Expected: PASS (may need 1-2 retries initially)

**Step 4: Commit**

```bash
git add e2e/tests/watch/import.spec.ts e2e/tests/tags/tag-watch-folder.spec.ts
git commit -m "fix(e2e): fix flaky watch tests with DB polling, remove serial mode"
```

---

### Task 4: Fix Plugin Scheduler Tests

**Files:**
- Modify: `e2e/tests/plugins/plugin-scheduler.spec.ts`

**Step 1: Replace all `waitForTimeout` calls with `expect.poll`**

For "should execute scheduled task at specified interval" (line 30):
```typescript
// Replace: await app.page.waitForTimeout(500);
// With: (remove - plugin init is synchronous)

// Replace: await app.page.waitForTimeout(2500);
// With:
await expect.poll(
  async () => parseInt(await app.getPluginStorage(plugin!.id, 'tick_count') || '0'),
  { timeout: 5000, intervals: [200, 500, 1000], message: 'Waiting for first tick' }
).toBeGreaterThanOrEqual(1);

// Replace second: await app.page.waitForTimeout(2500);
// With:
const count1 = parseInt(await app.getPluginStorage(plugin!.id, 'tick_count'));
await expect.poll(
  async () => parseInt(await app.getPluginStorage(plugin!.id, 'tick_count') || '0'),
  { timeout: 5000, intervals: [200, 500, 1000], message: 'Waiting for second tick' }
).toBeGreaterThan(count1);
```

Apply similar `expect.poll` pattern to all 4 tests in this file. The key insight: instead of sleeping for the tick interval + buffer, poll for the expected storage value change.

**Step 2: Run scheduler tests**

Run: `cd e2e && npx playwright test tests/plugins/plugin-scheduler.spec.ts`
Expected: PASS

**Step 3: Commit**

```bash
git add e2e/tests/plugins/plugin-scheduler.spec.ts
git commit -m "perf(e2e): replace scheduler test sleeps with expect.poll"
```

---

### Task 5: Fix Plugin Execution Tests

**Files:**
- Modify: `e2e/tests/plugins/plugin-execution.spec.ts`

**Step 1: Replace all `waitForTimeout(500)` calls after API operations**

Pattern: Every `await app.page.waitForTimeout(500);` after `importPluginFromPath` or after `uploadFile`/`deleteClip` (waiting for plugin event processing) should be replaced with polling for the expected storage value:

```typescript
// Replace: await app.page.waitForTimeout(500); // after importPluginFromPath
// With:
await expect.poll(
  async () => app.getPluginStorage(plugin!.id, 'loaded'),
  { timeout: 5000, intervals: [100, 200, 500] }
).toBe('true');
```

For event processing waits (after upload/delete):
```typescript
// Replace: await app.page.waitForTimeout(500); // after event
// With:
await expect.poll(
  async () => app.getPluginStorage(plugin!.id, 'count_clip_created'),
  { timeout: 5000, intervals: [100, 200, 500] }
).toBe('1');
```

Each test already checks the storage value immediately after the wait — just move the assertion into `expect.poll`.

**Step 2: Fix "should persist data between events" (line 292)**

Replace the `waitForTimeout(300)` between uploads with nothing (Playwright auto-waits), and replace the final `waitForTimeout(500)` with:
```typescript
await expect.poll(
  async () => {
    const log = await app.getPluginStorage(plugin!.id, 'event_log');
    try { return JSON.parse(log).length; } catch { return 0; }
  },
  { timeout: 5000, intervals: [100, 200, 500] }
).toBe(3);
```

**Step 3: Run plugin execution tests**

Run: `cd e2e && npx playwright test tests/plugins/plugin-execution.spec.ts`
Expected: PASS

**Step 4: Commit**

```bash
git add e2e/tests/plugins/plugin-execution.spec.ts
git commit -m "perf(e2e): replace plugin execution test sleeps with polling"
```

---

### Task 6: Fix Plugin Error Handling Tests

**Files:**
- Modify: `e2e/tests/plugins/plugin-error-handling.spec.ts`

**Step 1: Replace all waits with expect.poll on storage values**

Same pattern as Task 5. Every `waitForTimeout(500)` after upload is waiting for the plugin to process the event. Replace with:
```typescript
await expect.poll(
  async () => app.getPluginStorage(plugin!.id, 'error_count'),
  { timeout: 5000, intervals: [100, 200, 500] }
).toBe('1');
```

For `waitForTimeout(1000)` and `waitForTimeout(1500)` (more generous waits for multiple events), use the same pattern with appropriate expected values.

**Step 2: Run error handling tests**

Run: `cd e2e && npx playwright test tests/plugins/plugin-error-handling.spec.ts`
Expected: PASS

**Step 3: Commit**

```bash
git add e2e/tests/plugins/plugin-error-handling.spec.ts
git commit -m "perf(e2e): replace error handling test sleeps with polling"
```

---

### Task 7: Fix Plugin Settings Tests

**Files:**
- Modify: `e2e/tests/plugins/settings.spec.ts`

**Step 1: Replace expand waits with visibility assertion**

Every test does:
```typescript
await card.locator(selectors.plugins.expandToggle).click();
await app.page.waitForTimeout(500);
```

Replace with:
```typescript
await card.locator(selectors.plugins.expandToggle).click();
await expect(card.locator(selectors.pluginSettings.section)).toBeVisible({ timeout: 5000 });
// (For tests where plugin has no settings, use the remove button or description visibility instead)
```

**Step 2: Replace debounce waits with storage polling**

Every `await app.page.waitForTimeout(500); // Wait for debounce` should be replaced with polling for the storage value that's asserted right after:
```typescript
// Replace: await app.page.waitForTimeout(500); // Wait for debounce
// Followed by: const value = await app.getPluginStorage(plugin!.id, 'endpoint');
// With:
await expect.poll(
  async () => app.getPluginStorage(plugin!.id, 'endpoint'),
  { timeout: 5000, intervals: [100, 200, 500] }
).toBe('https://custom.api.com');
```

**Step 3: Replace close/reopen waits**

In "settings persist after closing and reopening modal" (line 130): replace `await app.page.waitForTimeout(300);` between close and reopen with nothing — `closePluginsModal` already waits for opacity-0.

**Step 4: Run settings tests**

Run: `cd e2e && npx playwright test tests/plugins/settings.spec.ts`
Expected: PASS

**Step 5: Commit**

```bash
git add e2e/tests/plugins/settings.spec.ts
git commit -m "perf(e2e): replace settings test sleeps with polling"
```

---

### Task 8: Fix Remaining Plugin Tests (ui-actions, fs-api, http-api, plugins-ui)

**Files:**
- Modify: `e2e/tests/plugins/ui-actions.spec.ts`
- Modify: `e2e/tests/plugins/plugin-fs-api.spec.ts`
- Modify: `e2e/tests/plugins/plugin-http-api.spec.ts`
- Modify: `e2e/tests/plugins/plugins-ui.spec.ts`
- Modify: `e2e/tests/plugins/tag-plugin-api.spec.ts`

**Step 1: Fix ui-actions.spec.ts**

- Line 27: `waitForTimeout(500)` after enablePlugin → remove (API is sync)
- Line 47: `waitForTimeout(300)` after disablePlugin → remove
- Line 82: `waitForTimeout(200)` after menu close → remove (Playwright auto-waits)
- Lines 295, 301, 328, 334: `waitForTimeout(200/300)` after plugin menu click → replace with `await this.page.locator(selectors.lightbox.pluginMenu).waitFor({ state: 'visible' })` or `await expect(app.page.locator(selectors.pluginOptions.modal + '.opacity-100')).toBeVisible()`

**Step 2: Fix plugin-fs-api.spec.ts**

All 7 `waitForTimeout(500)` calls are after `importPluginFromPath`. Replace each with:
```typescript
await expect.poll(
  async () => app.getPluginStorage(plugin!.id, 'fs_test_initialized'),
  { timeout: 5000, intervals: [100, 200, 500] }
).toBe('true');
```

**Step 3: Fix plugin-http-api.spec.ts**

All 3 `waitForTimeout(500)` calls follow the same pattern. Replace with polling for the storage value checked next.

**Step 4: Fix plugins-ui.spec.ts**

- Line 30: `waitForTimeout(300)` after backdrop click → replace with `await expect(app.page.locator(selectors.plugins.modal + '.opacity-0')).toBeVisible()`
- Line 195: `waitForTimeout(200)` after expand toggle → replace with `await expect(card.locator('text=This is a test description')).toBeVisible()`

**Step 5: Fix tag-plugin-api.spec.ts**

- Line 139: `waitForTimeout(300)` after UpdateTag → remove (API is sync, assertion follows)

**Step 6: Run all plugin tests**

Run: `cd e2e && npx playwright test tests/plugins/`
Expected: PASS

**Step 7: Commit**

```bash
git add e2e/tests/plugins/
git commit -m "perf(e2e): replace remaining plugin test sleeps with proper waits"
```

---

### Task 9: Fix Remaining Test Files

**Files:**
- Modify: `e2e/tests/images/lightbox.spec.ts`
- Modify: `e2e/tests/images/editor.spec.ts`
- Modify: `e2e/tests/search/filtering.spec.ts`
- Modify: `e2e/tests/bulk/operations.spec.ts`
- Modify: `e2e/tests/bulk/selection.spec.ts`
- Modify: `e2e/tests/edge-cases/errors.spec.ts`
- Modify: `e2e/tests/edge-cases/expiration.spec.ts`
- Modify: `e2e/tests/watch/folders.spec.ts`
- Modify: `e2e/tests/watch/filters.spec.ts`
- Modify: `e2e/tests/clips/view.spec.ts`
- Modify: `e2e/tests/tags/tag-filter.spec.ts`

**Step 1: Fix lightbox.spec.ts**

- Line 42: `waitForTimeout(300)` after Escape → replace with `await app.page.waitForSelector('#lightbox:not(.active)')`
- Lines 76, 94, 112, 131: `waitForTimeout(200)` after navigation → remove (lightbox stays open, just navigates image)

**Step 2: Fix editor.spec.ts**

- Lines 208, 212: `waitForTimeout(200)` after Ctrl+Z/Y → remove (editor redraws synchronously)

**Step 3: Fix filtering.spec.ts**

- Line 107: `waitForTimeout(400)` after typing → replace with `await app.page.locator(selectors.gallery.clipCard).or(app.page.locator(selectors.gallery.emptyState)).first().waitFor({ timeout: 5000 })`

**Step 4: Fix bulk/operations.spec.ts**

- Line 207: `waitForTimeout(500)` after bulkArchive → replace with `await app.expectClipCount(1)` (the remaining clip count)

**Step 5: Fix bulk/selection.spec.ts**

- Line 74: `waitForTimeout(300)` after deselect → remove (UI updates synchronously on checkbox change)

**Step 6: Fix edge-cases/errors.spec.ts**

- Line 92: `waitForTimeout(500)` after Escape → replace with `await app.page.waitForSelector('#editor-modal:not(.active)')`
- Lines 114: `waitForTimeout(500)` after dblclick → replace with `await app.page.waitForTimeout(200)` (reduce to minimal, then check dialog state)
- Line 183: `waitForTimeout(500)` → replace with `await app.page.waitForSelector(`${selectors.watch.view}.hidden`, { timeout: 5000 })` after close watch view

**Step 7: Fix edge-cases/expiration.spec.ts**

- Line 66: `waitForTimeout(500)` after CancelExpiration → remove (API is sync, assertion follows)

**Step 8: Fix watch/folders.spec.ts**

- Line 78: `waitForTimeout(500)` after removeWatchFolder → replace with `await expect(app.page.locator(selectors.watch.folderCount)).toContainText(String(initialCount - 1))`
- Lines 105, 111: `waitForTimeout(500)` after toggleGlobalWatch → remove (fixture helper already waits)

**Step 9: Fix watch/filters.spec.ts**

- Line 14: `waitForTimeout(500)` → remove (fixture helper already waits)
- Line 175: `waitForTimeout(300)` → remove (toggle view already waits)
- Line 260: `waitForTimeout(500)` → remove

**Step 10: Fix clips/view.spec.ts**

- Line 126: `waitForTimeout(100)` between two uploads → remove (uploadFile already waits for clip)

**Step 11: Fix tags/tag-filter.spec.ts**

- Lines 128, 152, 178: `waitForTimeout(500)` after clicking tag checkbox → replace with waiting for gallery to re-render: `await app.page.locator(selectors.gallery.clipCard).or(app.page.locator(selectors.gallery.emptyState)).first().waitFor({ timeout: 5000 })`

**Step 12: Run ALL tests**

Run: `cd e2e && npx playwright test`
Expected: ALL PASS

**Step 13: Commit**

```bash
git add e2e/tests/
git commit -m "perf(e2e): replace all remaining test sleeps with proper waits"
```

---

### Task 10: Final Verification and Timing

**Step 1: Run full test suite 3 times**

Run: `cd e2e && time npx playwright test`
Run it 3 times to check for flakiness. Record timing.

Expected: All pass, runtime ~5-10 minutes.

**Step 2: If any tests fail, fix with targeted Playwright waits (not sleeps)**

If a removed wait causes failures, add a targeted assertion that waits for the specific observable event (element visible, count changes, etc).

**Step 3: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix(e2e): stabilize tests after wait removal"
```
