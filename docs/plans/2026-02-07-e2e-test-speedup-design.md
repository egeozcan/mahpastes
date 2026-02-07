# E2E Test Speedup & Flaky Test Fixes

## Problem

- Test suite takes ~30 minutes to complete
- ~140 `waitForTimeout` calls totaling ~92 seconds of hardcoded sleep (per-worker, amplified by serial blocks)
- 2 flaky watch folder tests timing out intermittently
- Parallel Wails windows steal focus during development

## Goals

1. Replace all `waitForTimeout` calls with proper Playwright assertions
2. Fix 2 flaky watch folder auto-import tests
3. Run Playwright in headless mode by default
4. Remove unnecessary serial mode constraints
5. Target: ~5-10 minute total test runtime (down from ~30 min)

## Design

### Phase 1: Config & Infrastructure

- Enable `headless: true` in `playwright.config.ts`
- Keep `test:headed` npm script for debugging
- No changes to worker count (keep 4, reassess after)

### Phase 2: Fixture Helper Cleanup

Replace all ~46 `waitForTimeout` calls in `test-fixtures.ts` with proper waits.

**Patterns to replace:**

| Current Pattern | Replacement |
|----------------|-------------|
| Sleep after modal close | `waitFor({ state: 'hidden' })` on modal element |
| Sleep after API call that updates UI | `expect(locator).toHaveCount(N)` or `.toBeVisible()` |
| Sleep after navigation/tab switch | `expect(element).toHaveAttribute(...)` or `.toBeVisible()` |
| Sleep after click | Playwright auto-wait (remove entirely) |
| 100ms "UI settle" waits | Remove — Playwright handles this |

**New helpers to add:**

```typescript
async waitForPluginTaskComplete(timeout = 5000): Promise<void>
// Polls task queue for no active tasks

async waitForModalHidden(modalSelector: string): Promise<void>
// Waits for modal to reach state: 'hidden'
```

**Enhanced existing helpers:**
- `executePluginAction()` — auto-wait for task completion
- Modal close methods — wait for hidden state instead of sleeping

### Phase 3: Watch Folder Flaky Test Fixes

**Root cause:** Tests poll DOM for clip count, but the pipeline (fsnotify detect → process → DB write → UI refresh) has variable latency under load.

**Fix:** Poll backend DB first, then assert UI:

```typescript
await expect.poll(
  async () => app.getClipCountFromDB(),
  { timeout: 30000, intervals: [500, 1000, 2000] }
).toBeGreaterThanOrEqual(1);
await app.refreshClips();
await app.expectClipCount(1);
```

**Additional changes:**
- Each watch test gets its own isolated temp directory (already provided by fixture)
- Remove `mode: 'serial'` from `watch/import.spec.ts` and `tags/tag-watch-folder.spec.ts`
- Keep single 500ms wait after file write (legitimate fsnotify detection window)

### Phase 4: Plugin Test Wait Cleanup (~50s eliminated)

**Plugin scheduler** (18s): Replace `waitForTimeout(2500-3000)` with polling for expected side effects via `expect.poll()` on plugin storage values.

**Plugin execution** (12.4s): Enhance `executePluginAction()` to auto-wait for task queue drain. Remove all post-action sleeps.

**Plugin error handling** (7.6s): Wait for observable outcomes — error toasts, UI state changes.

**Plugin settings** (6.4s): Wait for save indicators, debounce completion via polling.

### Phase 5: Remaining Test Files (~12s)

Replace scattered `waitForTimeout` calls in:
- `images/lightbox.spec.ts` — wait for lightbox visibility
- `images/editor.spec.ts` — wait for canvas/editor ready
- `search/filtering.spec.ts` — wait for filter results
- `bulk/operations.spec.ts` — wait for selection state
- `edge-cases/errors.spec.ts` — wait for error UI
- `watch/folders.spec.ts`, `watch/filters.spec.ts` — wait for folder list updates

## Risks

- Some `waitForTimeout` calls may mask real race conditions in the app. If removing them surfaces new failures, add targeted Playwright waits (not sleep).
- Removing serial mode from watch tests could surface shared-state issues if test isolation isn't perfect.
- Plugin scheduler tests may need app-side changes if there's no observable side effect to poll.

## Files Modified

- `e2e/playwright.config.ts` — headless mode
- `e2e/fixtures/test-fixtures.ts` — fixture helpers (~46 changes)
- `e2e/tests/watch/import.spec.ts` — flaky fix + remove serial
- `e2e/tests/tags/tag-watch-folder.spec.ts` — flaky fix + remove serial
- `e2e/tests/plugins/plugin-scheduler.spec.ts` — polling
- `e2e/tests/plugins/plugin-execution.spec.ts` — auto-wait
- `e2e/tests/plugins/plugin-error-handling.spec.ts` — observable waits
- `e2e/tests/plugins/settings.spec.ts` — observable waits
- `e2e/tests/plugins/ui-actions.spec.ts` — minor cleanup
- `e2e/tests/plugins/plugin-fs-api.spec.ts` — minor cleanup
- `e2e/tests/plugins/plugin-http-api.spec.ts` — minor cleanup
- `e2e/tests/images/lightbox.spec.ts` — visibility waits
- `e2e/tests/images/editor.spec.ts` — editor ready waits
- `e2e/tests/search/filtering.spec.ts` — filter result waits
- `e2e/tests/bulk/operations.spec.ts` — selection waits
- `e2e/tests/bulk/selection.spec.ts` — selection waits
- `e2e/tests/edge-cases/errors.spec.ts` — error UI waits
- `e2e/tests/edge-cases/expiration.spec.ts` — minor cleanup
- `e2e/tests/watch/folders.spec.ts` — folder list waits
- `e2e/tests/watch/filters.spec.ts` — filter UI waits
- `e2e/tests/clips/view.spec.ts` — minor cleanup
- `e2e/tests/plugins/plugins-ui.spec.ts` — minor cleanup
