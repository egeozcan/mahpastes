/**
 * share-ui.spec.ts
 *
 * Single-instance UI smoke tests for the Share tab.
 *
 * WHAT IS TESTED:
 *   - The sidebar 2×2 grid contains all four tabs (Clips, Watch, Serve, Share).
 *   - Clicking the Share tab activates its view.
 *   - The "Share a Tag" modal opens on clicking "+ Share a Tag".
 *   - The "Follow a Share" modal opens on clicking "+ Follow a Share".
 *   - The follow-modal client-side validation rejects malformed share strings and
 *     shows an error, then hides it when the field is cleared.
 */

import { test, expect } from '../../fixtures/test-fixtures.js';

test.describe('Share - UI smoke tests', () => {

  // One case below stubs a ShareService binding in-page. The page is
  // worker-scoped and outlives this file, so hand the real bindings back
  // afterwards rather than leaking a stub into whatever runs next.
  test.beforeEach(async ({ app }) => {
    await app.page.evaluate(() => {
      const w = window as any;
      const svc = w.go?.main?.ShareService;
      if (!svc) return;
      if (!w.__realShareService) w.__realShareService = { ...svc };
      Object.assign(svc, w.__realShareService);
    }).catch(() => {});
  });

  test.afterEach(async ({ app }) => {
    await app.page.evaluate(() => {
      const w = window as any;
      if (w.__realShareService && w.go?.main?.ShareService) {
        Object.assign(w.go.main.ShareService, w.__realShareService);
      }
    }).catch(() => {});
  });

  test('sidebar 2×2 grid has all four tabs', async ({ app }) => {
    await app.openDrawer();
    await expect(app.page.locator('#view-tab-clips')).toBeVisible();
    await expect(app.page.locator('#view-tab-watch')).toBeVisible();
    await expect(app.page.locator('#view-tab-serve')).toBeVisible();
    await expect(app.page.locator('#view-tab-share')).toBeVisible();
  });

  test('Share tab activates the share view', async ({ app }) => {
    await app.openDrawer();
    await app.page.click('#view-tab-share');
    // The share-view section should become visible (hidden class removed).
    await app.page.waitForFunction(
      () => !(document.getElementById('share-view')?.classList.contains('hidden') ?? true),
      { timeout: 5000 },
    );
    await expect(app.page.locator('#share-view')).toBeVisible();
    // The clips view should be hidden.
    await expect(app.page.locator('#view-tab-share')).toHaveAttribute('aria-selected', 'true');
  });

  test('"Share a Tag" modal opens and closes', async ({ app }) => {
    // Requires at least one tag so the picker is populated.
    await app.createTag('ui-test-share-tag');

    await app.openDrawer();
    await app.page.click('#view-tab-share');
    await app.page.waitForFunction(
      () => !(document.getElementById('share-view')?.classList.contains('hidden') ?? true),
      { timeout: 5000 },
    );

    // Open the modal.
    await app.page.click('#add-share-btn');
    await app.page.waitForFunction(
      () => !(document.getElementById('create-share-modal')?.classList.contains('hidden') ?? true),
      { timeout: 5000 },
    );
    await expect(app.page.locator('#create-share-modal')).toBeVisible();
    await expect(app.page.locator('#create-share-tag-select')).toBeVisible();

    // Close via Cancel button.
    await app.page.click('.create-share-close');
    await app.page.waitForFunction(
      () => document.getElementById('create-share-modal')?.classList.contains('hidden') ?? true,
      { timeout: 5000 },
    );
    await expect(app.page.locator('#create-share-modal')).toBeHidden();
  });

  test('"Follow a Share" modal opens and closes', async ({ app }) => {
    await app.openDrawer();
    await app.page.click('#view-tab-share');
    await app.page.waitForFunction(
      () => !(document.getElementById('share-view')?.classList.contains('hidden') ?? true),
      { timeout: 5000 },
    );

    // Open the follow modal.
    await app.page.click('#add-follow-btn');
    await app.page.waitForFunction(
      () => !(document.getElementById('follow-share-modal')?.classList.contains('hidden') ?? true),
      { timeout: 5000 },
    );
    await expect(app.page.locator('#follow-share-modal')).toBeVisible();
    await expect(app.page.locator('#follow-share-string')).toBeVisible();

    // Tag section should be hidden until a valid string is entered.
    await expect(app.page.locator('#follow-share-tag-section')).toBeHidden();

    // Close via Cancel button.
    await app.page.click('.follow-share-close');
    await app.page.waitForFunction(
      () => document.getElementById('follow-share-modal')?.classList.contains('hidden') ?? true,
      { timeout: 5000 },
    );
    await expect(app.page.locator('#follow-share-modal')).toBeHidden();
  });

  test('follow-modal validation rejects malformed strings', async ({ app }) => {
    await app.openDrawer();
    await app.page.click('#view-tab-share');
    await app.page.waitForFunction(
      () => !(document.getElementById('share-view')?.classList.contains('hidden') ?? true),
      { timeout: 5000 },
    );
    await app.page.click('#add-follow-btn');
    await app.page.waitForFunction(
      () => !(document.getElementById('follow-share-modal')?.classList.contains('hidden') ?? true),
      { timeout: 5000 },
    );

    // Stub TestFollowConnection so the happy-path case doesn't actually dial
    // (this test is about the UI gating, not real peer discovery).
    await app.page.evaluate(() => {
      (window as any).go.main.ShareService.TestFollowConnection = async () => undefined;
    });

    const textarea = app.page.locator('#follow-share-string');
    const invalidEl = app.page.locator('#follow-share-invalid');
    const tagSection = app.page.locator('#follow-share-tag-section');
    const confirmBtn = app.page.locator('#follow-share-confirm-btn');

    // Case 1: plainly wrong string — client-side parse fails → invalidFormat state.
    await textarea.fill('not-a-share-link');
    await app.page.evaluate(() =>
      document.getElementById('follow-share-string')?.dispatchEvent(new Event('input', { bubbles: true })),
    );
    await expect(invalidEl).toBeVisible();
    await expect(tagSection).toBeHidden();
    await expect(confirmBtn).toBeDisabled();

    // Case 2: correct prefix but invalid base64url chars — also fails client-side.
    await textarea.fill('mp-share:v1:!@#$%');
    await app.page.evaluate(() =>
      document.getElementById('follow-share-string')?.dispatchEvent(new Event('input', { bubbles: true })),
    );
    await expect(invalidEl).toBeVisible();
    await expect(tagSection).toBeHidden();
    await expect(confirmBtn).toBeDisabled();

    // Case 3: empty field — no error block visible, confirm still disabled.
    await textarea.fill('');
    await app.page.evaluate(() =>
      document.getElementById('follow-share-string')?.dispatchEvent(new Event('input', { bubbles: true })),
    );
    await expect(invalidEl).toBeHidden();
    await expect(confirmBtn).toBeDisabled();

    // Case 4: valid prefix + base64url blob — passes client parse, stubbed
    // TestFollowConnection resolves, tag section appears. Tag input is empty
    // so confirm stays disabled until user types a tag.
    await textarea.fill('mp-share:v1:AAABBBCCC');
    await app.page.evaluate(() =>
      document.getElementById('follow-share-string')?.dispatchEvent(new Event('input', { bubbles: true })),
    );
    // Tag section appears after the 400ms input debounce + stubbed resolve.
    await expect(tagSection).toBeVisible({ timeout: 3000 });
    // Type a tag — confirm should enable.
    await app.page.locator('#follow-share-local-tag').fill('some-tag');
    await expect(confirmBtn).toBeEnabled();
  });
});
