import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestText,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Clip Delete', () => {
  test.describe('Single Clip Delete', () => {
    test('should delete a single clip', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.expectClipVisible(filename);

      await app.deleteClip(filename);

      await app.expectClipNotVisible(filename);
      await app.expectClipCount(0);
    });

    test('should delete one clip while keeping others', async ({ app }) => {
      const file1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const file2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const file3 = await createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png');
      const filename1 = path.basename(file1);
      const filename2 = path.basename(file2);
      const filename3 = path.basename(file3);

      await app.uploadFiles([file1, file2, file3]);
      await app.expectClipCount(3);

      await app.deleteClip(filename2);

      await app.expectClipVisible(filename1);
      await app.expectClipNotVisible(filename2);
      await app.expectClipVisible(filename3);
      await app.expectClipCount(2);
    });

    test('should show confirmation dialog before delete', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);

      // Start delete but check for dialog
      const clip = await app.getClipByFilename(filename);
      await clip.hover();
      await clip.locator('[data-action="delete"]').click();

      // Dialog should be visible (confirmDialog will handle it)
      await app.confirmDialog();

      await app.expectClipNotVisible(filename);
    });

    test('should cancel delete when dialog is cancelled', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);

      // Start delete
      const clip = await app.getClipByFilename(filename);
      await clip.hover();
      await clip.locator('[data-action="delete"]').click();

      // Cancel the dialog
      await app.cancelDialog();

      // Clip should still exist
      await app.expectClipVisible(filename);
    });

    test('should delete text clip', async ({ app }) => {
      const textPath = await createTempFile(generateTestText('delete-test'), 'txt');
      const filename = path.basename(textPath);

      await app.uploadFile(textPath);
      await app.expectClipVisible(filename);

      await app.deleteClip(filename);

      await app.expectClipNotVisible(filename);
    });
  });

  test.describe('Delete All (Clear)', () => {
    test('should delete all clips when using clear all button', async ({ app }) => {
      // Upload multiple clips
      const files = await Promise.all([
        createTempFile(generateTestImage(), 'png'),
        createTempFile(generateTestText('clear-1'), 'txt'),
        createTempFile(generateTestText('clear-2'), 'txt'),
      ]);

      await app.uploadFiles(files);
      await app.expectClipCount(3);

      // Use the app's delete all functionality
      await app.deleteAllClips();

      await app.expectClipCount(0);
    });
  });

  test.describe('Delete with Expiration', () => {
    test('should be able to delete clip with expiration', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath, 30); // 30 minute expiration
      await app.expectClipVisible(filename);

      await app.deleteClip(filename);

      await app.expectClipNotVisible(filename);
    });
  });

  test.describe('Delete from Archive', () => {
    test('should delete archived clip', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      // Upload and archive
      await app.uploadFile(imagePath);
      await app.archiveClip(filename);

      // Switch to archive view
      await app.toggleArchiveView();
      await app.expectClipVisible(filename);

      // Delete from archive
      await app.deleteClip(filename);

      await app.expectClipNotVisible(filename);
    });
  });
});
