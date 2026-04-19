import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import path from 'path';

test.describe('Folder view — rename', () => {
  test.afterEach(async ({ app }) => {
    // Exit folder mode if active
    const btn = app.page.locator('[data-testid="folder-mode-button"]');
    const pressed = await btn.getAttribute('aria-pressed').catch(() => 'false');
    if (pressed === 'true') {
      await btn.click();
    }
    await app.clearTagFilters();
    await app.deleteAllTags();
  });

  test('renames current folder and updates the view', async ({ app }) => {
    const img = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(img);
    await app.createTag('work/clientA');
    await app.tagClipByIndex(0, 'work/clientA');
    await app.enterFolder('work/clientA');

    // Confirm we are inside work/clientA.
    await app.expectFolderHeader('work/clientA');

    // Rename the tag — this fires a tag:updated Wails event which should
    // cause the folder view to re-navigate to the new path.
    await app.renameTag('work/clientA', 'work/clientB');

    // Folder view should have re-navigated to the new path.
    await app.expectFolderHeader('work/clientB');
    // The clip is still inside the renamed folder.
    await app.expectClipCount(1);
  });
});
