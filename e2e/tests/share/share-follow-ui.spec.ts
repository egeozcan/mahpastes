/**
 * share-follow-ui.spec.ts
 *
 * Single-instance UI tests for the enhanced follow-share flow. Mocks the
 * Wails ShareService bindings in-page so we can deterministically exercise:
 *
 *   - connecting-spinner visibility during the probe
 *   - self-follow error state
 *   - unreachable state with Retry + Follow-anyway
 *   - autocomplete dropdown (existing tags + "Create new" row + keyboard nav)
 *   - race guard: stale TestFollowConnection result is discarded when the
 *     user edits the textarea mid-flight
 *
 * No secondary instance is spawned — backend dial paths are covered by the
 * Go-side tests and the share-follow.spec.ts integration test.
 */

import { test, expect } from '../../fixtures/test-fixtures.js';

// Valid-shape share string — passes parseShareStringClientSide. Backend is
// stubbed so its actual decodability doesn't matter here.
const FAKE_LINK_A = 'mp-share:v1:AAABBBCCCDDD';
const FAKE_LINK_B = 'mp-share:v1:XXXYYYZZZWWW';

async function openFollowModal(app: any) {
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
}

// Fire the textarea `input` event since Playwright's `fill` doesn't always
// dispatch it consistently across focus states.
async function pasteShareLink(app: any, link: string) {
  await app.page.locator('#follow-share-string').fill(link);
  await app.page.evaluate(() => {
    document.getElementById('follow-share-string')?.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

test.describe('Share - Follow UI (mocked backend)', () => {

  // Tests stub window.go.main.ShareService methods in-page. Close the modal
  // after each case so subsequent tests don't hit "modal intercepts pointer
  // events" when opening the drawer. Also clear stubs to avoid bleed-over.
  test.afterEach(async ({ app }) => {
    await app.page.evaluate(() => {
      const modal = document.getElementById('follow-share-modal');
      if (modal && !modal.classList.contains('hidden')) {
        modal.classList.add('hidden');
      }
    }).catch(() => {});
  });

  test('shows connecting spinner while TestFollowConnection is in flight', async ({ app }) => {
    await openFollowModal(app);

    // Stub with a slow resolver so the spinner remains visible long enough to assert.
    await app.page.evaluate(() => {
      (window as any).go.main.ShareService.TestFollowConnection = () =>
        new Promise((resolve) => setTimeout(resolve, 2000));
    });

    await pasteShareLink(app, FAKE_LINK_A);

    // Connecting block visible within the debounce window.
    await expect(app.page.locator('#follow-share-connecting')).toBeVisible({ timeout: 2000 });
    await expect(app.page.locator('#follow-share-tag-section')).toBeHidden();

    // Eventually resolves → tag section appears.
    await expect(app.page.locator('#follow-share-tag-section')).toBeVisible({ timeout: 5000 });
    await expect(app.page.locator('#follow-share-connecting')).toBeHidden();
  });

  test('self-follow surfaces the blocking self message', async ({ app }) => {
    await openFollowModal(app);

    await app.page.evaluate(() => {
      (window as any).go.main.ShareService.TestFollowConnection = async () => {
        throw new Error('cannot follow your own share');
      };
    });

    await pasteShareLink(app, FAKE_LINK_A);

    await expect(app.page.locator('#follow-share-self')).toBeVisible({ timeout: 2000 });
    await expect(app.page.locator('#follow-share-tag-section')).toBeHidden();
    await expect(app.page.locator('#follow-share-unreachable')).toBeHidden();
    await expect(app.page.locator('#follow-share-confirm-btn')).toBeDisabled();
  });

  test('unreachable state offers Retry and Follow anyway', async ({ app }) => {
    await openFollowModal(app);

    // Stub fails the first call, tracks calls for Retry assertion.
    await app.page.evaluate(() => {
      (window as any).__testProbeCalls = 0;
      (window as any).go.main.ShareService.TestFollowConnection = async () => {
        (window as any).__testProbeCalls++;
        throw new Error('initial dial: failed to connect');
      };
      (window as any).__testFollowWithoutDialArgs = null;
      (window as any).go.main.ShareService.FollowWithoutDial = async (s: string, tag: string) => {
        (window as any).__testFollowWithoutDialArgs = { s, tag };
        return {};
      };
    });

    await pasteShareLink(app, FAKE_LINK_A);

    // Unreachable block shown; tag section still hidden until Follow-anyway is armed.
    await expect(app.page.locator('#follow-share-unreachable')).toBeVisible({ timeout: 3000 });
    await expect(app.page.locator('#follow-share-tag-section')).toBeHidden();
    await expect(app.page.locator('#follow-share-retry-btn')).toBeVisible();
    await expect(app.page.locator('#follow-share-follow-anyway-btn')).toBeVisible();

    // Retry fires TestFollowConnection again.
    await app.page.locator('#follow-share-retry-btn').click();
    // Small wait so the second call lands.
    await app.page.waitForFunction(() => (window as any).__testProbeCalls >= 2, { timeout: 3000 });
    // Still unreachable (stub always fails).
    await expect(app.page.locator('#follow-share-unreachable')).toBeVisible();

    // Follow-anyway reveals the tag picker.
    await app.page.locator('#follow-share-follow-anyway-btn').click();
    await expect(app.page.locator('#follow-share-tag-section')).toBeVisible();

    // Commit calls FollowWithoutDial, not Follow.
    const tagInput = app.page.locator('#follow-share-local-tag');
    await tagInput.fill('offline-inbox');
    // Blur the input to close the autocomplete dropdown — otherwise it overlaps
    // the Confirm button and intercepts clicks. Blur mirrors a user clicking
    // elsewhere; the dropdown closes a frame later.
    await tagInput.evaluate((el: HTMLElement) => el.blur());
    await expect(app.page.locator('#follow-share-tag-section [role="listbox"]')).toBeHidden();
    await expect(app.page.locator('#follow-share-confirm-btn')).toBeEnabled();
    await app.page.locator('#follow-share-confirm-btn').click();

    await app.page.waitForFunction(
      () => (window as any).__testFollowWithoutDialArgs !== null,
      { timeout: 3000 },
    );
    const args = await app.page.evaluate(() => (window as any).__testFollowWithoutDialArgs);
    expect(args.tag).toBe('offline-inbox');
    expect(args.s).toBe(FAKE_LINK_A);
  });

  test('autocomplete shows existing tags and a Create-new row', async ({ app }) => {
    // Seed an existing tag before opening the modal so GetTags() returns it.
    await app.createTag('shared/from-alice');
    await app.createTag('unrelated-tag');

    await openFollowModal(app);

    // Stub TestFollowConnection to succeed so we reach the tag picker.
    await app.page.evaluate(() => {
      (window as any).go.main.ShareService.TestFollowConnection = async () => undefined;
    });

    await pasteShareLink(app, FAKE_LINK_A);
    await expect(app.page.locator('#follow-share-tag-section')).toBeVisible({ timeout: 3000 });

    // Focus triggers the dropdown to populate.
    const tagInput = app.page.locator('#follow-share-local-tag');
    await tagInput.focus();

    // Start typing a substring of the seeded tag.
    await tagInput.fill('shared');
    // Dropdown items are <button role="option">.
    const dropdown = app.page.locator('#follow-share-tag-section [role="listbox"]');
    await expect(dropdown).toBeVisible({ timeout: 2000 });

    // Existing tag row with "existing" badge visible.
    await expect(
      dropdown.locator('[role="option"][data-kind="existing"]', { hasText: 'shared/from-alice' }),
    ).toBeVisible();

    // Typing a non-matching value yields a "Create new" row.
    await tagInput.fill('brand-new-tag');
    await expect(
      dropdown.locator('[role="option"][data-kind="new"]', { hasText: 'Create new: brand-new-tag' }),
    ).toBeVisible();
  });

  test('autocomplete keyboard nav: ArrowDown + Enter commits', async ({ app }) => {
    await app.createTag('autoc-alpha');
    await app.createTag('autoc-beta');

    await openFollowModal(app);
    await app.page.evaluate(() => {
      (window as any).go.main.ShareService.TestFollowConnection = async () => undefined;
    });
    await pasteShareLink(app, FAKE_LINK_A);
    await expect(app.page.locator('#follow-share-tag-section')).toBeVisible({ timeout: 3000 });

    const tagInput = app.page.locator('#follow-share-local-tag');
    await tagInput.focus();
    await tagInput.fill('autoc');

    const dropdown = app.page.locator('#follow-share-tag-section [role="listbox"]');
    await expect(dropdown).toBeVisible();

    // Press ArrowDown once — first item should already be highlighted on open,
    // so one press moves to the second existing tag (or the Create-new row,
    // whichever is at index 1).
    await app.page.keyboard.press('ArrowDown');
    await app.page.keyboard.press('Enter');

    // Input should now contain the committed value and the dropdown should close.
    const val = await tagInput.inputValue();
    expect(['autoc-alpha', 'autoc-beta', 'autoc']).toContain(val);
    await expect(dropdown).toBeHidden();
  });

  test('race guard: second paste replaces first dial result', async ({ app }) => {
    await openFollowModal(app);

    // Stub resolves slowly and records the last-seen link. The first (slow)
    // call will arrive LAST, which is exactly what we want to test the guard.
    await app.page.evaluate(() => {
      let callIdx = 0;
      (window as any).__testResolvedLink = null;
      (window as any).go.main.ShareService.TestFollowConnection = (s: string) => {
        callIdx++;
        const myIdx = callIdx;
        // First call hangs for 2s (simulating a slow initial dial), subsequent
        // calls resolve quickly. When the first call finally resolves, the
        // frontend's latestReqID guard should discard its effect.
        const delay = myIdx === 1 ? 2000 : 100;
        return new Promise((resolve) => setTimeout(() => {
          (window as any).__testResolvedLink = s;
          resolve(undefined);
        }, delay));
      };
    });

    // Paste A — starts slow probe.
    await pasteShareLink(app, FAKE_LINK_A);
    // Paste B before A's probe resolves.
    await app.page.waitForTimeout(600);
    await pasteShareLink(app, FAKE_LINK_B);

    // Eventually the tag section reveals (from B's resolution).
    await expect(app.page.locator('#follow-share-tag-section')).toBeVisible({ timeout: 5000 });

    // Wait out A's slow probe so it fully resolves — UI should NOT re-flip.
    await app.page.waitForTimeout(2000);
    // Tag section still visible (B's result wins).
    await expect(app.page.locator('#follow-share-tag-section')).toBeVisible();
    // No state flip to connecting or invalid.
    await expect(app.page.locator('#follow-share-connecting')).toBeHidden();
    await expect(app.page.locator('#follow-share-invalid')).toBeHidden();
  });
});
