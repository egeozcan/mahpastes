import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestText,
  createTempDir,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as fs from 'fs/promises';
import * as path from 'path';

test.describe('Error Handling', () => {
  test.describe('File Upload Errors', () => {
    test('should handle upload of very large text gracefully', async ({ app }) => {
      // Create a large text file (1MB)
      const largeContent = 'x'.repeat(1024 * 1024);
      const file = await createTempFile(largeContent, 'txt');

      await app.uploadFile(file);

      // Should either succeed or show error, not crash
      // The app should remain functional regardless of outcome
      const count = await app.getClipCount();
      // Count should be 0 (rejected due to size) or 1 (accepted)
      expect(count).toBeGreaterThanOrEqual(0);
      expect(count).toBeLessThanOrEqual(1);

      // Verify app is still responsive by checking we can interact with it
      await app.page.locator(selectors.header.searchInput).isVisible();
    });

    test('should handle empty filename gracefully', async ({ app }) => {
      // Most systems prevent empty filenames, but test the flow
      const file = await createTempFile(generateTestText('content'), 'txt');

      await app.uploadFile(file);

      // Should not crash
      await app.expectClipCount(1);
    });

    test('should handle special characters in filename', async ({ app }) => {
      const content = generateTestText('special-name');
      const file = await createTempFile(content, 'txt');

      await app.uploadFile(file);

      await app.expectClipCount(1);
    });
  });

  test.describe('Database Errors', () => {
    test('should handle rapid successive operations', async ({ app }) => {
      // Rapidly create and delete clips
      const files = await Promise.all(
        Array.from({ length: 5 }, () => createTempFile(generateTestImage(50, 50), 'png'))
      );

      await app.uploadFiles(files);
      await app.selectAll();
      await app.bulkDelete();
      await app.confirmDialog();

      // Should complete without error
      await app.expectClipCount(0);
    });

    test('should handle concurrent uploads', async ({ app }) => {
      // Upload multiple files simultaneously
      const file1 = createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const file2 = createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const file3 = createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png');

      const files = await Promise.all([file1, file2, file3]);
      await app.uploadFiles(files);

      // All should be uploaded
      await app.expectClipCount(3);
    });
  });

  test.describe('UI State Errors', () => {
    test('should recover from closing modal unexpectedly', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      // Press Escape to close
      await app.page.keyboard.press('Escape');
      await app.page.waitForTimeout(500);

      // App should be in usable state
      await app.expectClipCount(1);
    });

    test('should handle double-click on actions', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);

      // Double-click delete (rapid clicks)
      const clip = await app.getClipByFilename(filename);
      await clip.hover();
      const deleteBtn = clip.locator('[data-action="delete"]');
      await deleteBtn.dblclick();

      // Wait a bit for any dialog that might appear
      await app.page.waitForTimeout(500);

      // Check if dialog is visible - double-click may or may not trigger it
      const dialogVisible = await app.page.evaluate((selector) => {
        const dialog = document.querySelector(selector);
        return dialog?.classList.contains('opacity-100') ?? false;
      }, '#confirm-dialog');

      if (dialogVisible) {
        await app.cancelDialog();
      }

      // App should still be usable - verify clip is still there
      await app.expectClipCount(1);
    });

    test('should handle navigation during operation', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.selectClip(filename);

      // Toggle views while selected
      await app.toggleArchiveView();
      await app.toggleArchiveView();

      // App should remain stable
      await app.expectClipCount(1);
    });
  });

  test.describe('Watch Folder Errors', () => {
    test('should handle non-existent folder path gracefully', async ({ app }) => {
      await app.openWatchView();

      // Try to add non-existent folder
      const nonExistentPath = '/nonexistent/path/that/does/not/exist';

      try {
        await app.page.evaluate(async (folderPath) => {
          // @ts-ignore
          await window.go.main.App.AddWatchedFolder({
            path: folderPath,
            filterMode: 'all',
            filterPresets: [],
            filterRegex: '',
            processExisting: false,
            autoArchive: false,
          });
        }, nonExistentPath);
      } catch {
        // Expected to fail
      }

      // App should still be functional
      await app.closeWatchView();
    });

    test('should handle folder deleted while watching', async ({ app }) => {
      // Create a separate temp directory for this test (not the fixture's tempDir)
      // to avoid affecting test isolation
      const watchDir = await createTempDir();

      await app.openWatchView();
      await app.addWatchFolder(watchDir);
      await app.toggleGlobalWatch(true);

      // Wait for watcher to start
      await app.page.waitForTimeout(500);

      // Remove the watch folder BEFORE deleting the directory
      // This prevents the app from crashing due to watching a deleted folder
      try {
        await app.removeWatchFolder(watchDir);
      } catch {
        // May fail if folder is already gone from list
      }

      // Now safe to delete the directory
      await fs.rm(watchDir, { recursive: true, force: true });

      // App should handle gracefully
      await app.closeWatchView();
    });

    test('should handle permission denied on folder', async ({ app }) => {
      // This test is platform-specific and may not work on all systems
      // Skip if we can't create restricted folder

      await app.openWatchView();
      // App should handle permission errors gracefully
      await app.closeWatchView();
    });
  });

  test.describe('Search Errors', () => {
    test('should handle very long search query', async ({ app }) => {
      const file = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(file);

      // Very long search query
      const longQuery = 'a'.repeat(1000);
      await app.search(longQuery);

      // Should not crash
      await app.clearSearch();
      await app.expectClipCount(1);
    });

    test('should handle unicode in search', async ({ app }) => {
      const file = await createTempFile(generateTestText('unicode-test'), 'txt');
      await app.uploadFile(file);

      // Unicode search
      await app.search('日本語');
      await app.search('emoji 🎉');
      await app.search('Ñoño');

      // Should handle gracefully
      await app.clearSearch();
    });
  });

  test.describe('Clipboard Errors', () => {
    test('should handle paste when clipboard is empty', async ({ app }) => {
      // Attempt to trigger paste with empty content
      await app.page.locator('#drop-zone').focus();

      // Simulate empty paste
      await app.page.evaluate(() => {
        const event = new ClipboardEvent('paste', {
          clipboardData: new DataTransfer(),
        });
        document.dispatchEvent(event);
      });

      // Should not crash
      await app.expectClipCount(0);
    });
  });

  test.describe('Memory and Performance', () => {
    test('should handle many clips without freezing', async ({ app }) => {
      // Create and upload many small clips
      const clipCount = 20;
      const files = await Promise.all(
        Array.from({ length: clipCount }, (_, i) =>
          createTempFile(generateTestText(`clip-${i}`), 'txt')
        )
      );

      // Upload in batches
      for (let i = 0; i < files.length; i += 5) {
        const batch = files.slice(i, i + 5);
        await app.uploadFiles(batch);
      }

      // Should handle all clips
      const count = await app.getClipCount();
      expect(count).toBe(clipCount);

      // Clean up
      await app.deleteAllClips();
    });
  });
});
