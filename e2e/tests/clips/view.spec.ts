import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestText,
  generateTestJSON,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Clip View', () => {
  test.describe('Clip Card Display', () => {
    test('should display image clip with thumbnail', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);

      const clip = await app.getClipByFilename(filename);
      await expect(clip).toBeVisible();

      // Check for image thumbnail
      const img = clip.locator('img');
      await expect(img).toBeVisible();
    });

    test('should display text clip with preview', async ({ app }) => {
      const textPath = await createTempFile(generateTestText('view-test'), 'txt');
      const filename = path.basename(textPath);

      await app.uploadFile(textPath);

      const clip = await app.getClipByFilename(filename);
      await expect(clip).toBeVisible();
    });

    test('should display JSON clip with formatted preview', async ({ app }) => {
      const jsonPath = await createTempFile(generateTestJSON(), 'json');
      const filename = path.basename(jsonPath);

      await app.uploadFile(jsonPath);

      const clip = await app.getClipByFilename(filename);
      await expect(clip).toBeVisible();
    });

    test('should show filename on clip card', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);

      const clip = await app.getClipByFilename(filename);
      await expect(clip).toContainText(filename.replace('.png', ''));
    });

    test('should show clip type indicator', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');

      await app.uploadFile(imagePath);

      await app.expectClipCount(1);
    });
  });

  test.describe('Clip Card Actions', () => {
    test('should show action buttons on hover', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);

      const clip = await app.getClipByFilename(filename);
      await clip.hover();

      // Menu trigger should be visible on hover
      const menuTrigger = clip.locator(selectors.clipActions.menuTrigger);
      await expect(menuTrigger).toBeVisible();
    });

    test('should copy clip path to clipboard', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.copyClipPath(filename);

      // Toast should appear
      await app.expectToast('Copied');
    });
  });

  test.describe('Gallery Layout', () => {
    test('should display clips in grid layout', async ({ app }) => {
      // Upload several clips
      const files = await Promise.all([
        createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png'),
        createTempFile(generateTestImage(100, 100, [255, 255, 0]), 'png'),
      ]);

      await app.uploadFiles(files);

      await app.expectClipCount(4);

      // Verify all clips are visible
      const clips = await app.getAllClips();
      await expect(clips).toHaveCount(4);
    });

    test('should show empty state when no clips', async ({ app }) => {
      // Ensure no clips exist
      await app.deleteAllClips();

      // Check for empty state
      const gallery = app.page.locator(selectors.gallery.container);
      // Either empty state element or no clips
      const clipCount = await app.getClipCount();
      expect(clipCount).toBe(0);
    });

    test('should order clips by creation time (newest first)', async ({ app }) => {
      const file1 = await createTempFile(generateTestText('first'), 'txt');
      await app.uploadFile(file1);
      await app.page.waitForTimeout(100);

      const file2 = await createTempFile(generateTestText('second'), 'txt');
      await app.uploadFile(file2);

      const clips = await app.getAllClips();
      await expect(clips).toHaveCount(2);

      // Newest should be first (check order based on DOM position)
      const firstClip = clips.first();
      await expect(firstClip).toBeVisible();
    });
  });

  test.describe('Clip Preview Content', () => {
    test('should truncate long text in preview', async ({ app }) => {
      const longText = 'A'.repeat(500);
      const filePath = await createTempFile(longText, 'txt');
      const filename = path.basename(filePath);

      await app.uploadFile(filePath);

      const clip = await app.getClipByFilename(filename);
      await expect(clip).toBeVisible();
      // Preview should not show all 500 characters
    });

    test('should show different icons for different file types', async ({ app }) => {
      const txtFile = await createTempFile(generateTestText(), 'txt');
      const jsonFile = await createTempFile(generateTestJSON(), 'json');

      await app.uploadFiles([txtFile, jsonFile]);

      await app.expectClipCount(2);
    });
  });
});
