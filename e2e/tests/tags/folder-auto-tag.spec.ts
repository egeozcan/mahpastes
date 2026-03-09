import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import path from 'path';

test.describe('Folder Mode Auto-Tagging', () => {
  test.afterEach(async ({ app }) => {
    // Exit folder mode if active
    const btn = app.page.locator('[data-testid="folder-mode-button"]');
    const pressed = await btn.getAttribute('aria-pressed');
    if (pressed === 'true') {
      await btn.click();
    }
    await app.clearTagFilters();
    await app.deleteAllTags();
  });

  test('uploading a clip while inside a folder auto-tags it', async ({ app }) => {
    await app.createTag('projects');
    await app.toggleFolderMode();
    await app.clickFolder('projects');

    // Upload a clip while inside the "projects" folder
    const imagePath = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    // Clip should be visible inside the folder
    await app.expectClipVisible(filename);
    await app.expectClipCount(1);

    // Navigate back to root — clip should NOT appear (it's tagged, not untagged)
    await app.clearTagFilters();
    await app.expectClipNotVisible(filename);
  });

  test('uploading at folder root does not auto-tag', async ({ app }) => {
    await app.createTag('projects');
    await app.toggleFolderMode();

    // Upload at root level (no folder selected)
    const imagePath = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    // Clip should be visible at root as untagged
    await app.expectClipVisible(filename);

    // Navigate into "projects" — clip should NOT appear there
    await app.clickFolder('projects');
    await app.expectClipNotVisible(filename);
  });
});
