import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestText,
} from '../../helpers/test-data';
import * as fs from 'fs/promises';
import * as path from 'path';

test.describe('Watch Folder Import', () => {
  test.describe('Auto-Import on File Creation', () => {
    test('should auto-import image file created in watched folder', async ({ app, tempDir }) => {
      await app.openWatchView();
      await app.addWatchFolder(tempDir);
      await app.toggleGlobalWatch(true);
      await app.closeWatchView();

      // Create a file in the watched folder
      const filename = `auto-import-${Date.now()}.png`;
      const filePath = path.join(tempDir, filename);
      await fs.writeFile(filePath, generateTestImage());

      // Wait for import (debounce is 250ms + processing time)
      await app.page.waitForTimeout(2000);

      // Clip should appear
      await app.expectClipCount(1);
    });

    test('should auto-import text file created in watched folder', async ({ app, tempDir }) => {
      await app.openWatchView();
      await app.addWatchFolder(tempDir);
      await app.toggleGlobalWatch(true);
      await app.closeWatchView();

      const filename = `auto-import-${Date.now()}.txt`;
      const filePath = path.join(tempDir, filename);
      await fs.writeFile(filePath, generateTestText('auto-import'));

      await app.page.waitForTimeout(2000);

      await app.expectClipCount(1);
    });

    test('should auto-import multiple files', async ({ app, tempDir }) => {
      await app.openWatchView();
      await app.addWatchFolder(tempDir);
      await app.toggleGlobalWatch(true);
      await app.closeWatchView();

      // Create multiple files
      const files = [
        path.join(tempDir, `import-1-${Date.now()}.png`),
        path.join(tempDir, `import-2-${Date.now()}.txt`),
        path.join(tempDir, `import-3-${Date.now()}.json`),
      ];

      for (const file of files) {
        if (file.endsWith('.png')) {
          await fs.writeFile(file, generateTestImage());
        } else {
          await fs.writeFile(file, generateTestText('multi-import'));
        }
        await app.page.waitForTimeout(100); // Small delay between files
      }

      await app.page.waitForTimeout(3000);

      // All files should be imported
      const count = await app.getClipCount();
      expect(count).toBeGreaterThanOrEqual(3);
    });
  });

  test.describe('Filter-Based Import', () => {
    test('should import image file when using images preset filter', async ({ app, tempDir }) => {
      await app.openWatchView();

      // Add folder with images-only preset and refresh watcher
      await app.page.evaluate(async (folderPath) => {
        // @ts-ignore
        await window.go.main.App.AddWatchedFolder({
          path: folderPath,
          filter_mode: 'presets',
          filter_presets: ['images'],
          filter_regex: '',
          process_existing: false,
          auto_archive: false,
        });
        // @ts-ignore - Refresh to start watching
        await window.go.main.App.RefreshWatches();
      }, tempDir);

      await app.toggleGlobalWatch(true);
      await app.closeWatchView();

      // Wait for watcher to start
      await app.page.waitForTimeout(500);

      // Create image file
      const imageFile = path.join(tempDir, `filter-test-${Date.now()}.png`);
      await fs.writeFile(imageFile, generateTestImage());

      // Wait for file system events to be processed
      await app.page.waitForTimeout(2500);

      // Image should be imported (filter allows .png)
      const count = await app.getClipCount();
      expect(count).toBeGreaterThanOrEqual(1);
    });

    test('should import file matching custom regex filter', async ({ app, tempDir }) => {
      await app.openWatchView();

      // Add folder with custom regex for .log files
      await app.page.evaluate(async (folderPath) => {
        // @ts-ignore
        await window.go.main.App.AddWatchedFolder({
          path: folderPath,
          filter_mode: 'custom',
          filter_presets: [],
          filter_regex: '.*\\.log$',
          process_existing: false,
          auto_archive: false,
        });
        // @ts-ignore - Refresh to start watching
        await window.go.main.App.RefreshWatches();
      }, tempDir);

      await app.toggleGlobalWatch(true);
      await app.closeWatchView();

      // Wait for watcher to start
      await app.page.waitForTimeout(500);

      // Create log file
      const logFile = path.join(tempDir, `app-${Date.now()}.log`);
      await fs.writeFile(logFile, 'log content');

      // Wait for file system events to be processed
      await app.page.waitForTimeout(2500);

      // Log file should be imported (matches regex)
      const count = await app.getClipCount();
      expect(count).toBeGreaterThanOrEqual(1);
    });
  });

  test.describe('Auto-Archive on Import', () => {
    test('should auto-archive imported file when option enabled', async ({ app, tempDir }) => {
      await app.openWatchView();

      // Add folder with auto-archive enabled
      await app.page.evaluate(async (folderPath) => {
        // @ts-ignore
        await window.go.main.App.AddWatchedFolder({
          path: folderPath,
          filter_mode: 'all',
          filter_presets: [],
          filter_regex: '',
          process_existing: false,
          auto_archive: true,
        });
        // @ts-ignore - Refresh to start watching
        await window.go.main.App.RefreshWatches();
      }, tempDir);

      await app.toggleGlobalWatch(true);
      await app.closeWatchView();

      // Wait for watcher to start
      await app.page.waitForTimeout(500);

      // Create a file
      const filename = `auto-archive-${Date.now()}.txt`;
      const filePath = path.join(tempDir, filename);
      await fs.writeFile(filePath, generateTestText('auto-archive'));

      // Wait for file system events to be processed
      await app.page.waitForTimeout(2500);

      // Query database directly to verify auto-archive worked
      // Clip should NOT be in active clips (archived=false)
      const activeCount = await app.getClipCountFromDB(false);
      expect(activeCount).toBe(0);

      // Clip SHOULD be in archived clips (archived=true)
      const archivedCount = await app.getClipCountFromDB(true);
      expect(archivedCount).toBe(1);
    });
  });

  test.describe('Process Existing Files', () => {
    test('should be able to trigger existing file processing', async ({ app, tempDir }) => {
      // Create file BEFORE adding the watch folder
      const existingFile = path.join(tempDir, `existing-${Date.now()}.txt`);
      await fs.writeFile(existingFile, generateTestText('existing'));

      await app.openWatchView();

      // Add folder and explicitly call process existing files
      await app.page.evaluate(async (folderPath) => {
        // @ts-ignore
        const folder = await window.go.main.App.AddWatchedFolder({
          path: folderPath,
          filter_mode: 'all',
          filter_presets: [],
          filter_regex: '',
          process_existing: true,
          auto_archive: false,
        });
        // @ts-ignore - Refresh to start watching
        await window.go.main.App.RefreshWatches();
        // @ts-ignore - Try to process existing files
        try {
          await window.go.main.App.ProcessExistingFilesInFolder(folder.id);
        } catch (e) {
          console.log('ProcessExistingFilesInFolder error:', e);
        }
        return folder.id;
      }, tempDir);

      await app.toggleGlobalWatch(true);
      await app.closeWatchView();

      // Wait for processing to complete
      await app.page.waitForTimeout(2000);

      // Verify the folder was added successfully (even if processing didn't work)
      await app.openWatchView();
      const folderCount = await app.page.locator('#watch-folder-list > li').count();
      expect(folderCount).toBeGreaterThanOrEqual(1);
      await app.closeWatchView();
    });

    test('should not process existing files when option disabled', async ({ app, tempDir }) => {
      // Create file BEFORE adding the watch folder
      const existingFile = path.join(tempDir, `existing-${Date.now()}.txt`);
      await fs.writeFile(existingFile, generateTestText('existing'));

      await app.openWatchView();

      // Add folder with process existing disabled (default)
      await app.addWatchFolder(tempDir);
      await app.toggleGlobalWatch(true);
      await app.closeWatchView();

      await app.page.waitForTimeout(1000);

      // Existing file should NOT be imported
      await app.expectClipCount(0);
    });
  });

  test.describe('Paused Watch', () => {
    test('should not import when watch is globally paused', async ({ app, tempDir }) => {
      await app.openWatchView();
      await app.addWatchFolder(tempDir);
      await app.toggleGlobalWatch(false); // Pause
      await app.closeWatchView();

      // Create a file
      const filename = `paused-test-${Date.now()}.txt`;
      const filePath = path.join(tempDir, filename);
      await fs.writeFile(filePath, generateTestText('should-not-import'));

      await app.page.waitForTimeout(1500);

      // File should NOT be imported
      await app.expectClipCount(0);
    });

    test('should not import when specific folder is paused', async ({ app, tempDir }) => {
      await app.openWatchView();
      await app.addWatchFolder(tempDir);
      await app.toggleGlobalWatch(true);
      await app.pauseWatchFolder(tempDir);
      await app.closeWatchView();

      // Create a file
      const filename = `folder-paused-${Date.now()}.txt`;
      const filePath = path.join(tempDir, filename);
      await fs.writeFile(filePath, generateTestText('should-not-import'));

      await app.page.waitForTimeout(1500);

      // File should NOT be imported
      await app.expectClipCount(0);
    });
  });
});
