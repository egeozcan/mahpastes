import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import * as path from 'path';

test.describe('Bottom Bar', () => {
  test('should display the bottom bar with add button and expiry selector', async ({ app }) => {
    const page = app.page;
    await expect(page.locator(selectors.bottomBar.root)).toBeVisible();
    await expect(page.locator(selectors.bottomBar.addButton)).toBeVisible();
    await expect(page.locator(selectors.bottomBar.expirySelect)).toBeVisible();
  });

  test('should show correct clip count after uploading', async ({ app }) => {
    const page = app.page;

    // Initially 0 clips
    await expect(page.locator(selectors.bottomBar.clipCount)).toHaveText('0 clips');

    // Upload one file
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);
    await expect(page.locator(selectors.bottomBar.clipCount)).toHaveText('1 clip');

    // Upload another
    const imagePath2 = await createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png');
    await app.uploadFile(imagePath2);
    await app.expectClipCount(2);
    await expect(page.locator(selectors.bottomBar.clipCount)).toHaveText('2 clips');
  });

  test('should update clip count after deleting', async ({ app }) => {
    const page = app.page;
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);
    await expect(page.locator(selectors.bottomBar.clipCount)).toHaveText('1 clip');

    await app.deleteClip(path.basename(imagePath));
    await app.expectClipCount(0);
    await expect(page.locator(selectors.bottomBar.clipCount)).toHaveText('0 clips');
  });
});
