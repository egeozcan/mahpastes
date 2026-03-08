import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import path from 'path';

test.describe('Folder Mode', () => {
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

  test('folder mode button toggles state', async ({ app }) => {
    const btn = app.page.locator('[data-testid="folder-mode-button"]');
    await expect(btn).toHaveAttribute('aria-pressed', 'false');
    await btn.click();
    await expect(btn).toHaveAttribute('aria-pressed', 'true');
    await btn.click();
    await expect(btn).toHaveAttribute('aria-pressed', 'false');
  });

  test('folder mode shows tag folders at root', async ({ app }) => {
    await app.createTag('work');
    await app.createTag('personal');
    await app.toggleFolderMode();
    await app.expectFolderVisible('work');
    await app.expectFolderVisible('personal');
  });

  test('clicking folder navigates into subtag', async ({ app }) => {
    await app.createTag('work/client1');
    await app.createTag('work/client2');
    await app.toggleFolderMode();
    await app.clickFolder('work');
    await app.expectFolderVisible('client1');
    await app.expectFolderVisible('client2');
  });

  test('folder mode shows directly-tagged clips alongside folders', async ({ app }) => {
    await app.createTag('work/client1');
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.addTagToClip(path.basename(imagePath), 'work');
    await app.toggleFolderMode();
    await app.clickFolder('work');
    // Should see client1 folder AND the clip
    await app.expectFolderVisible('client1');
    await app.expectClipCount(1);
  });

  test('exiting folder mode preserves tag filters', async ({ app }) => {
    await app.createTag('work/client1');
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.addTagToClip(path.basename(imagePath), 'work/client1');
    await app.toggleFolderMode();
    await app.clickFolder('work');
    // Exit folder mode — filter should remain on "work"
    await app.toggleFolderMode();
    // Clip should still be visible (filtered by work, which includes descendants)
    await app.expectClipCount(1);
  });
});
