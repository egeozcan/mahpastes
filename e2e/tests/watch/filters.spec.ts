import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Watch Folder Filters', () => {
  test.describe('Filter Mode: All', () => {
    test('should add folder with "all" filter mode by default', async ({ app, tempDir }) => {
      await app.openWatchView();

      // Add folder via API (default is 'all')
      await app.addWatchFolder(tempDir);

      // Wait for UI to update
      await app.page.waitForTimeout(500);

      // Folder should be added - check for folder card in list
      const folderCards = app.page.locator(selectors.watch.folderCard);
      const cardCount = await folderCards.count();
      expect(cardCount).toBeGreaterThanOrEqual(1);
    });
  });

  test.describe('Filter Mode: Presets', () => {
    test('should add folder with images preset', async ({ app, tempDir }) => {
      await app.openWatchView();

      // Add folder with presets filter via direct API call
      await app.page.evaluate(async (folderPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.AddWatchedFolder({
          path: folderPath,
          filter_mode: 'presets',
          filter_presets: ['images'],
          filter_regex: '',
          process_existing: false,
          auto_archive: false,
        });
        // @ts-ignore - Refresh watches to update UI
        await window.go.main.App.RefreshWatches();
      }, tempDir);

      // Toggle view to refresh UI
      await app.closeWatchView();
      await app.openWatchView();

      const count = await app.getWatchFolderCount();
      expect(count).toBeGreaterThanOrEqual(1);
    });

    test('should add folder with documents preset', async ({ app, tempDir }) => {
      await app.openWatchView();

      await app.page.evaluate(async (folderPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.AddWatchedFolder({
          path: folderPath,
          filter_mode: 'presets',
          filter_presets: ['documents'],
          filter_regex: '',
          process_existing: false,
          auto_archive: false,
        });
        // @ts-ignore - Refresh watches to update UI
        await window.go.main.App.RefreshWatches();
      }, tempDir);

      // Toggle view to refresh UI
      await app.closeWatchView();
      await app.openWatchView();

      const count = await app.getWatchFolderCount();
      expect(count).toBeGreaterThanOrEqual(1);
    });

    test('should add folder with multiple presets', async ({ app, tempDir }) => {
      await app.openWatchView();

      await app.page.evaluate(async (folderPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.AddWatchedFolder({
          path: folderPath,
          filter_mode: 'presets',
          filter_presets: ['images', 'documents', 'videos'],
          filter_regex: '',
          process_existing: false,
          auto_archive: false,
        });
        // @ts-ignore - Refresh watches to update UI
        await window.go.main.App.RefreshWatches();
      }, tempDir);

      // Toggle view to refresh UI
      await app.closeWatchView();
      await app.openWatchView();

      const count = await app.getWatchFolderCount();
      expect(count).toBeGreaterThanOrEqual(1);
    });
  });

  test.describe('Filter Mode: Custom Regex', () => {
    test('should add folder with custom regex filter', async ({ app, tempDir }) => {
      await app.openWatchView();

      await app.page.evaluate(async (folderPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.AddWatchedFolder({
          path: folderPath,
          filter_mode: 'custom',
          filter_presets: [],
          filter_regex: '.*\\.txt$',
          process_existing: false,
          auto_archive: false,
        });
        // @ts-ignore - Refresh watches to update UI
        await window.go.main.App.RefreshWatches();
      }, tempDir);

      // Toggle view to refresh UI
      await app.closeWatchView();
      await app.openWatchView();

      const count = await app.getWatchFolderCount();
      expect(count).toBeGreaterThanOrEqual(1);
    });

    test('should add folder with complex regex pattern', async ({ app, tempDir }) => {
      await app.openWatchView();

      await app.page.evaluate(async (folderPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.AddWatchedFolder({
          path: folderPath,
          filter_mode: 'custom',
          filter_presets: [],
          filter_regex: '^(report|summary).*\\.(pdf|docx)$',
          process_existing: false,
          auto_archive: false,
        });
        // @ts-ignore - Refresh watches to update UI
        await window.go.main.App.RefreshWatches();
      }, tempDir);

      // Toggle view to refresh UI
      await app.closeWatchView();
      await app.openWatchView();

      const count = await app.getWatchFolderCount();
      expect(count).toBeGreaterThanOrEqual(1);
    });
  });

  test.describe('Filter Options', () => {
    test('should add folder with process existing files option', async ({ app, tempDir }) => {
      await app.openWatchView();

      await app.page.evaluate(async (folderPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.AddWatchedFolder({
          path: folderPath,
          filter_mode: 'all',
          filter_presets: [],
          filter_regex: '',
          process_existing: true,
          auto_archive: false,
        });
        // @ts-ignore - Refresh watches to update UI
        await window.go.main.App.RefreshWatches();
      }, tempDir);

      // Toggle view to refresh UI
      await app.closeWatchView();
      await app.openWatchView();

      const count = await app.getWatchFolderCount();
      expect(count).toBeGreaterThanOrEqual(1);
    });

    test('should add folder with auto-archive option', async ({ app, tempDir }) => {
      await app.openWatchView();

      await app.page.evaluate(async (folderPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.AddWatchedFolder({
          path: folderPath,
          filter_mode: 'all',
          filter_presets: [],
          filter_regex: '',
          process_existing: false,
          auto_archive: true,
        });
        // @ts-ignore - Refresh watches to update UI
        await window.go.main.App.RefreshWatches();
      }, tempDir);

      // Toggle view to refresh UI
      await app.closeWatchView();
      await app.openWatchView();

      const count = await app.getWatchFolderCount();
      expect(count).toBeGreaterThanOrEqual(1);
    });

    test('should add folder with both options enabled', async ({ app, tempDir }) => {
      await app.openWatchView();

      await app.page.evaluate(async (folderPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.AddWatchedFolder({
          path: folderPath,
          filter_mode: 'all',
          filter_presets: [],
          filter_regex: '',
          process_existing: true,
          auto_archive: true,
        });
        // @ts-ignore - Refresh watches to update UI
        await window.go.main.App.RefreshWatches();
      }, tempDir);

      // Toggle view to refresh UI
      await app.closeWatchView();
      await app.openWatchView();

      const count = await app.getWatchFolderCount();
      expect(count).toBeGreaterThanOrEqual(1);
    });
  });

  test.describe('Update Filter Settings', () => {
    test('should update folder filter mode', async ({ app, tempDir }) => {
      await app.openWatchView();

      // Add folder first
      await app.addWatchFolder(tempDir);

      // Get the folder ID and update it
      const folders = await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        return await window.go.main.App.GetWatchedFolders();
      });

      if (folders && folders.length > 0) {
        const folderId = folders[0].id;

        await app.page.evaluate(async ({ id, folderPath }) => {
          // @ts-ignore - Wails runtime
          await window.go.main.App.UpdateWatchedFolder(id, {
            path: folderPath,
            filter_mode: 'presets',
            filter_presets: ['images'],
            filter_regex: '',
            process_existing: false,
            auto_archive: false,
          });
        }, { id: folderId, folderPath: tempDir });

        await app.page.waitForTimeout(500);
      }

      // Verify folder still exists
      const count = await app.getWatchFolderCount();
      expect(count).toBeGreaterThanOrEqual(1);
    });
  });
});
