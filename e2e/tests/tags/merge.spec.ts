import { test } from '../../fixtures/test-fixtures';
import { expect } from '@playwright/test';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

test.describe('Merge tags', () => {
  test.afterEach(async ({ app }) => {
    // Stop any tag servers created during the test.
    await app.stopAllServers();
    // Exit folder mode if active.
    const btn = app.page.locator('[data-testid="folder-mode-button"]');
    const pressed = await btn.getAttribute('aria-pressed').catch(() => 'false');
    if (pressed === 'true') {
      await btn.click();
    }
    await app.clearTagFilters();
    await app.deleteAllTags();
  });

  test('merges source into destination (flat case)', async ({ app }) => {
    const img = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(img);
    await app.createTag('src');
    await app.createTag('dst');
    await app.tagClipByIndex(0, 'src');

    await app.mergeTag('src', 'dst');

    await app.expectTagMissing('src');
    await app.selectTagFilter('dst');
    await app.expectClipCount(1);
  });

  test('subtree moves with source', async ({ app }) => {
    await app.createTag('a/x');
    await app.createTag('a/x/foo');
    await app.createTag('b/y');

    await app.mergeTag('a/x', 'b/y');

    await app.expectTagMissing('a/x');
    await app.expectTagExists('b/y/foo');
  });

  test('blocked when source tag is served', async ({ app }) => {
    await app.createTag('served-src');
    await app.createTag('dst-blocked');
    await app.startServingTag('served-src');

    await app.openMergeModal('served-src');
    await app.enterMergeDestination('dst-blocked');
    const blockers = await app.getMergeModalBlockers();
    expect(blockers.some((b: string) => /serv/i.test(b))).toBe(true);
    // Close the modal.
    await app.page.click('#merge-tag-cancel');
    await app.page.waitForFunction(
      () => document.getElementById('merge-tag-modal')?.hasAttribute('inert'),
      { timeout: 3000 },
    );
  });

  test('re-navigates when current folder is the merge source', async ({ app }) => {
    const img = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(img);
    await app.createTag('oldfolder');
    await app.createTag('newfolder');
    await app.tagClipByIndex(0, 'oldfolder');
    await app.enterFolder('oldfolder');

    // Confirm we are in oldfolder before merging.
    await app.expectFolderHeader('oldfolder');

    await app.mergeTag('oldfolder', 'newfolder');

    // After the merge, the tag:merged event should re-navigate to newfolder.
    await app.expectFolderHeader('newfolder');
  });
});
