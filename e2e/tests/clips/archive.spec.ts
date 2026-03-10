import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestText,
} from '../../helpers/test-data';
import * as path from 'path';

test.describe('Clip Archive', () => {
  test.describe('Archive Single Clip', () => {
    test('should archive a clip', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.expectClipVisible(filename);

      await app.archiveClip(filename);

      // Clip should no longer be visible in main view
      await app.expectClipNotVisible(filename);
    });

    test('should show archived clip in archive view', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.archiveClip(filename);

      // Switch to archive view
      await app.toggleArchiveView();

      // Clip should be visible in archive
      await app.expectClipVisible(filename);
    });

    test('should restore archived clip', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      // Upload and archive
      await app.uploadFile(imagePath);
      await app.archiveClip(filename);

      // Switch to archive view and restore
      await app.toggleArchiveView();
      await app.expectClipVisible(filename);
      await app.archiveClip(filename); // Toggle back (restore)

      // Switch back to main view
      await app.toggleArchiveView();

      // Clip should be visible again
      await app.expectClipVisible(filename);
    });

    test('should archive text clip', async ({ app }) => {
      const textPath = await createTempFile(generateTestText('archive-test'), 'txt');
      const filename = path.basename(textPath);

      await app.uploadFile(textPath);
      await app.archiveClip(filename);

      await app.toggleArchiveView();
      await app.expectClipVisible(filename);
    });
  });

  test.describe('Archive View Toggle', () => {
    test('should toggle archive view button state', async ({ app }) => {
      // Initially not in archive view
      const isArchive = await app.isArchiveViewActive();
      expect(isArchive).toBe(false);

      // Toggle to archive
      await app.toggleArchiveView();
      const isArchiveAfter = await app.isArchiveViewActive();
      expect(isArchiveAfter).toBe(true);

      // Toggle back
      await app.toggleArchiveView();
      const isArchiveFinal = await app.isArchiveViewActive();
      expect(isArchiveFinal).toBe(false);
    });

    test('should not show active clips in archive view', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.expectClipVisible(filename);

      // Switch to archive view
      await app.toggleArchiveView();

      // Active clip should not be visible
      await app.expectClipNotVisible(filename);
    });

    test('should not show archived clips in main view', async ({ app }) => {
      const file1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const file2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const filename1 = path.basename(file1);
      const filename2 = path.basename(file2);

      await app.uploadFiles([file1, file2]);

      // Archive only one
      await app.archiveClip(filename1);

      // Main view should only show non-archived
      await app.expectClipNotVisible(filename1);
      await app.expectClipVisible(filename2);
    });
  });

  test.describe('Archive Multiple Clips', () => {
    test('should archive multiple clips sequentially', async ({ app }) => {
      const file1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const file2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const filename1 = path.basename(file1);
      const filename2 = path.basename(file2);

      await app.uploadFiles([file1, file2]);

      await app.archiveClip(filename1);
      await app.archiveClip(filename2);

      // Both should be in archive
      await app.toggleArchiveView();
      await app.expectClipVisible(filename1);
      await app.expectClipVisible(filename2);
    });
  });

  test.describe('Keyboard Archive', () => {
    test('should archive clip via keyboard only', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Tab to gallery, focus clip
      await app.tabTo('#gallery > li', { maxTabs: 50 });
      expect(await app.getFocusedClipIndex()).toBe(0);

      // Press 'e' to archive (clip context shortcut)
      await app.page.keyboard.press('e');

      // Clip should be archived (removed from non-archive view)
      await app.expectClipCount(0);
    });

    test('should restore archived clip via keyboard only', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Archive via keyboard
      await app.tabTo('#gallery > li', { maxTabs: 50 });
      await app.page.keyboard.press('e');
      await app.expectClipCount(0);

      // Switch to archive view to verify it's there
      await app.toggleArchiveView();
      await app.expectClipVisible(filename);

      // Focus the archived clip and restore via keyboard
      await app.tabTo('#gallery > li', { maxTabs: 50 });
      expect(await app.getFocusedClipIndex()).toBe(0);
      await app.page.keyboard.press('e');

      // Should be removed from archive view
      await app.expectClipCount(0);

      // Switch back to main view - clip should be restored
      await app.toggleArchiveView();
      await app.expectClipVisible(filename);
    });
  });

  test.describe('Archive with Expiration', () => {
    test('should archive any clip', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.archiveClip(filename);

      await app.toggleArchiveView();
      await app.expectClipVisible(filename);
    });
  });
});
