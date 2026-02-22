import { test, expect } from '../../fixtures/test-fixtures.js';
import { createTempFile, generateTestImage } from '../../helpers/test-data.js';
import { selectors } from '../../helpers/selectors.js';
import * as path from 'path';

test.describe('Clip Expiration', () => {
  test.beforeEach(async ({ app }) => {
    await app.setUploadExpiration('0');
  });

  test.describe('Upload with Expiration', () => {
    test('should upload a clip with expiration and show Temp badge', async ({ app }) => {
      await app.setUploadExpiration('15');
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);
      await app.expectClipHasExpirationBadge(path.basename(imagePath));
    });

    test('should upload without expiration by default', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);
      await app.expectClipHasNoExpirationBadge(path.basename(imagePath));
    });

    test('should persist expiration dropdown selection across uploads', async ({ app }) => {
      await app.setUploadExpiration('60');
      const img1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      await app.uploadFile(img1);
      const img2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(img2);
      await app.expectClipCount(2);
      await app.expectClipHasExpirationBadge(path.basename(img1));
      await app.expectClipHasExpirationBadge(path.basename(img2));
    });
  });

  test.describe('Context Menu Expiration', () => {
    test('should set expiration via context menu', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      const filename = path.basename(imagePath);
      await app.expectClipHasNoExpirationBadge(filename);

      await app.setExpirationViaMenu(filename, '1h');
      await app.expectClipHasExpirationBadge(filename);
    });

    test('should cancel expiration via context menu', async ({ app }) => {
      await app.setUploadExpiration('15');
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      const filename = path.basename(imagePath);
      await app.expectClipHasExpirationBadge(filename);

      await app.setUploadExpiration('0');

      await app.cancelExpirationViaMenu(filename);
      await app.expectClipHasNoExpirationBadge(filename);
    });

    test('should show Set Expiration for non-expiring clips', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      const clip = await app.getClipByFilename(path.basename(imagePath));
      await clip.hover();
      await clip.locator(selectors.clipActions.menuTrigger).click();
      await app.page.waitForSelector(selectors.cardMenu.dropdown);
      await expect(app.page.locator(selectors.cardMenu.setExpiration)).toBeVisible();
      await expect(app.page.locator(selectors.cardMenu.cancelExpiration)).not.toBeVisible();
    });

    test('should show Cancel Expiration for expiring clips', async ({ app }) => {
      await app.setUploadExpiration('15');
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      const clip = await app.getClipByFilename(path.basename(imagePath));
      await clip.hover();
      await clip.locator(selectors.clipActions.menuTrigger).click();
      await app.page.waitForSelector(selectors.cardMenu.dropdown);
      await expect(app.page.locator(selectors.cardMenu.cancelExpiration)).toBeVisible();
      await expect(app.page.locator(selectors.cardMenu.setExpiration)).not.toBeVisible();
    });
  });

  test.describe('Temp Badge', () => {
    test('should show remaining time in Temp badge', async ({ app }) => {
      await app.setUploadExpiration('360');
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      const clip = await app.getClipByFilename(path.basename(imagePath));
      const badge = clip.locator(selectors.expiration.badge);
      await expect(badge).toContainText(/Temp · \d+h/);
    });
  });

  test.describe('Bulk Expiration', () => {
    test('should set expiration on multiple clips', async ({ app }) => {
      const img1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const img2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFiles([img1, img2]);
      await app.expectClipCount(2);

      await app.selectAll();
      await app.page.locator(selectors.bulk.expiryButton).click();
      await app.page.waitForSelector(selectors.expiration.popover);
      await app.page.locator(selectors.expiration.popover).locator('button', { hasText: '1h' }).click();

      await app.expectClipHasExpirationBadge(path.basename(img1));
      await app.expectClipHasExpirationBadge(path.basename(img2));
    });

    test('should cancel expiration on multiple clips', async ({ app }) => {
      await app.setUploadExpiration('15');
      const img1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const img2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFiles([img1, img2]);
      await app.expectClipCount(2);

      await app.setUploadExpiration('0');

      await app.selectAll();
      await expect(app.page.locator(selectors.bulk.cancelExpiryButton)).toBeVisible();
      await app.page.locator(selectors.bulk.cancelExpiryButton).click();

      await app.expectClipHasNoExpirationBadge(path.basename(img1));
      await app.expectClipHasNoExpirationBadge(path.basename(img2));
    });
  });
});
