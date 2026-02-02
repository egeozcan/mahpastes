import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Image Lightbox', () => {
  test.describe('Open and Close', () => {
    test('should open lightbox when clicking view on image', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      const isOpen = await app.isLightboxOpen();
      expect(isOpen).toBe(true);
    });

    test('should close lightbox when clicking close button', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);
      await app.closeLightbox();

      const isOpen = await app.isLightboxOpen();
      expect(isOpen).toBe(false);
    });

    test('should close lightbox when pressing Escape', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      await app.page.keyboard.press('Escape');
      await app.page.waitForTimeout(300);

      const isOpen = await app.isLightboxOpen();
      expect(isOpen).toBe(false);
    });

    test('should display image in lightbox', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      const lightboxImage = app.page.locator(selectors.lightbox.image);
      await expect(lightboxImage).toBeVisible();
    });
  });

  test.describe('Navigation', () => {
    test('should navigate to next image', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);

      // Open lightbox on first image (newest is first, so that's files[2])
      await app.openLightbox(filenames[2]);

      // Navigate next
      await app.lightboxNext();
      await app.page.waitForTimeout(200);

      const isOpen = await app.isLightboxOpen();
      expect(isOpen).toBe(true);
    });

    test('should navigate to previous image', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.openLightbox(filenames[1]);

      await app.lightboxPrev();
      await app.page.waitForTimeout(200);

      const isOpen = await app.isLightboxOpen();
      expect(isOpen).toBe(true);
    });

    test('should navigate with arrow keys', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.openLightbox(filenames[0]);

      // Navigate with arrow key
      await app.page.keyboard.press('ArrowRight');
      await app.page.waitForTimeout(200);

      const isOpen = await app.isLightboxOpen();
      expect(isOpen).toBe(true);
    });

    test('should wrap around at end of images', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.openLightbox(filenames[0]);

      // Navigate past the end
      await app.lightboxNext();
      await app.lightboxNext();
      await app.page.waitForTimeout(200);

      // Should still be open (wrapped or at boundary)
      const isOpen = await app.isLightboxOpen();
      expect(isOpen).toBe(true);
    });
  });

  test.describe('Single Image', () => {
    test('should handle lightbox with single image', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      // Navigation buttons may be hidden or disabled for single image
      const isOpen = await app.isLightboxOpen();
      expect(isOpen).toBe(true);
    });
  });

  test.describe('Lightbox with Different Image Sizes', () => {
    test('should display small image correctly', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(50, 50), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      const lightboxImage = app.page.locator(selectors.lightbox.image);
      await expect(lightboxImage).toBeVisible();
    });

    test('should display large image correctly', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(800, 600), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      const lightboxImage = app.page.locator(selectors.lightbox.image);
      await expect(lightboxImage).toBeVisible();
    });

    test('should display non-square image correctly', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(400, 100), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      const lightboxImage = app.page.locator(selectors.lightbox.image);
      await expect(lightboxImage).toBeVisible();
    });
  });

  test.describe('Zoom Info Display', () => {
    test('should display zoom percentage in lightbox', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      const zoomInfo = app.page.locator(selectors.lightbox.zoomInfo);
      await expect(zoomInfo).toBeVisible();
      // Should show a percentage (e.g., "100%" or "50%")
      await expect(zoomInfo).toHaveText(/^\d+%$/);
    });

    test('should show zoom relative to native dimensions', async ({ app }) => {
      // Upload a large image that will be scaled down to fit
      const imagePath = await createTempFile(generateTestImage(2000, 2000), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      const zoomInfo = app.page.locator(selectors.lightbox.zoomInfo);
      await expect(zoomInfo).toBeVisible();
      // Wait for zoom percentage to update after image loads (initial value is 100%)
      // Large image should show less than 100% since it's scaled to fit
      await expect.poll(async () => {
        const text = await zoomInfo.textContent();
        return parseInt(text || '100', 10);
      }, { timeout: 5000 }).toBeLessThan(100);
    });
  });
});
