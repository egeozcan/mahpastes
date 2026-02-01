import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Image Comparison', () => {
  test.describe('Open and Close', () => {
    test('should open comparison modal with two selected images', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      const isOpen = await app.isComparisonOpen();
      expect(isOpen).toBe(true);
    });

    test('should close comparison modal', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();
      await app.closeComparison();

      const isOpen = await app.isComparisonOpen();
      expect(isOpen).toBe(false);
    });
  });

  test.describe('Comparison Modes', () => {
    test('should switch to fade mode', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      await app.setComparisonMode('fade');

      // Fade slider should be visible
      const fadeSlider = app.page.locator(selectors.comparison.rangeSlider);
      await expect(fadeSlider).toBeVisible();
    });

    test('should switch to slider mode', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      await app.setComparisonMode('slider');

      // Position slider should be visible
      const positionSlider = app.page.locator(selectors.comparison.rangeSlider);
      await expect(positionSlider).toBeVisible();
    });
  });

  test.describe('Fade Mode Controls', () => {
    test('should adjust fade level', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();
      await app.setComparisonMode('fade');

      await app.setFadeLevel(75);

      const fadeSlider = app.page.locator(selectors.comparison.rangeSlider);
      await expect(fadeSlider).toHaveValue('75');
    });

    test('should set fade to 0 (show first image only)', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();
      await app.setComparisonMode('fade');

      await app.setFadeLevel(0);

      const fadeSlider = app.page.locator(selectors.comparison.rangeSlider);
      await expect(fadeSlider).toHaveValue('0');
    });

    test('should set fade to 100 (show second image only)', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();
      await app.setComparisonMode('fade');

      await app.setFadeLevel(100);

      const fadeSlider = app.page.locator(selectors.comparison.rangeSlider);
      await expect(fadeSlider).toHaveValue('100');
    });
  });

  test.describe('Slider Mode Controls', () => {
    test('should adjust slider position', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();
      await app.setComparisonMode('slider');

      await app.setSliderPosition(30);

      const positionSlider = app.page.locator(selectors.comparison.rangeSlider);
      await expect(positionSlider).toHaveValue('30');
    });
  });

  test.describe('Zoom Controls', () => {
    test('should have zoom in button', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      const zoomInBtn = app.page.locator(selectors.comparison.zoomInButton);
      await expect(zoomInBtn).toBeVisible();
    });

    test('should have zoom out button', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      const zoomOutBtn = app.page.locator(selectors.comparison.zoomOutButton);
      await expect(zoomOutBtn).toBeVisible();
    });

    test('should have fit button', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      const fitBtn = app.page.locator(selectors.comparison.fitButton);
      await expect(fitBtn).toBeVisible();
    });
  });

  test.describe('Different Image Sizes', () => {
    test('should compare images of same size', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      const isOpen = await app.isComparisonOpen();
      expect(isOpen).toBe(true);
    });

    test('should compare images of different sizes', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(300, 150, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      const isOpen = await app.isComparisonOpen();
      expect(isOpen).toBe(true);
    });
  });
});
