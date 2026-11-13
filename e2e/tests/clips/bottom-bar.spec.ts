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

  // The expiry select stays set for the whole session; a set value has to
  // look set, and the upload toast has to say the clip will delete itself.
  test('a set expiry is highlighted and named in the upload toast', async ({ app }) => {
    const select = app.page.locator(selectors.bottomBar.expirySelect);
    await expect(select).toHaveAttribute('data-active', 'false');
    await expect(select).toHaveClass(/text-stone-500/);

    await select.selectOption('15');
    await expect(select).toHaveAttribute('data-active', 'true');
    await expect(select).toHaveClass(/bg-amber-50/);
    await expect(select).not.toHaveClass(/text-stone-500/);

    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.expectToast('1 uploaded, expiring in 15m.');

    await select.selectOption('0');
    await expect(select).toHaveAttribute('data-active', 'false');
    await expect(select).not.toHaveClass(/bg-amber-50/);

    const second = await createTempFile(generateTestImage(30, 30, [0, 0, 255]), 'png');
    await app.uploadFile(second);
    await app.waitForToast(/^1 uploaded\.$/);
  });
});
