import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage, createTempDir, cleanup } from '../../helpers/test-data';
import * as path from 'path';
import * as fs from 'fs/promises';

test.describe('Watch Folder Auto-Tagging', () => {
  let watchDir: string;

  test.beforeEach(async () => {
    watchDir = await createTempDir();
  });

  test.afterEach(async ({ app }) => {
    await app.deleteAllTags();
    await app.deleteAllWatchFolders();
    if (watchDir) {
      await cleanup(watchDir);
    }
  });

  test.describe('Auto-Tag Configuration', () => {
    test('should show auto-tag dropdown in watch folder modal', async ({ app }) => {
      // Open watch view and add folder modal
      const watchBtn = app.page.locator('#toggle-watch-view-btn');
      await watchBtn.click();
      await app.page.waitForSelector('#watch-view:not(.hidden)');

      const addFolderBtn = app.page.locator('#add-folder-btn');
      await addFolderBtn.click();
      await app.page.waitForSelector('#folder-modal', { state: 'visible' });

      const autoTagSelect = app.page.locator('[data-testid="watch-folder-auto-tag"]');
      await expect(autoTagSelect).toBeVisible();
    });

    test('should list available tags in auto-tag dropdown', async ({ app }) => {
      // Create tags first
      await app.createTag('auto-tag-1');
      await app.createTag('auto-tag-2');

      // Reload to ensure tags are loaded in frontend
      await app.page.reload();
      await app.waitForReady();

      // Open watch view
      const watchBtn = app.page.locator('#toggle-watch-view-btn');
      await watchBtn.click();
      await app.page.waitForSelector('#watch-view:not(.hidden)');

      // Directly open the folder modal with a test path (bypassing native dialog)
      await app.page.evaluate((testPath) => {
        // @ts-ignore - call the modal open function directly
        if (typeof openFolderModal === 'function') {
          openFolderModal(testPath);
        }
      }, watchDir);

      await app.page.waitForSelector('#folder-modal', { state: 'visible' });

      const autoTagSelect = app.page.locator('[data-testid="watch-folder-auto-tag"]');

      // Check options exist
      const options = autoTagSelect.locator('option');
      const optionCount = await options.count();

      // Should have "None" + 2 tags
      expect(optionCount).toBeGreaterThanOrEqual(3);
    });

    test('should have None option as default', async ({ app }) => {
      // Open watch view and add folder modal
      const watchBtn = app.page.locator('#toggle-watch-view-btn');
      await watchBtn.click();
      await app.page.waitForSelector('#watch-view:not(.hidden)');

      const addFolderBtn = app.page.locator('#add-folder-btn');
      await addFolderBtn.click();
      await app.page.waitForSelector('#folder-modal', { state: 'visible' });

      const autoTagSelect = app.page.locator('[data-testid="watch-folder-auto-tag"]');
      const noneOption = autoTagSelect.locator('option[value=""]');
      await expect(noneOption).toHaveText('None');
    });
  });

  test.describe('Auto-Tag on Import', () => {
    test('should auto-tag files imported from watch folder', async ({ app }) => {
      // Create a tag
      await app.createTag('watched');

      // Get tag ID
      const tags = await app.getAllTags();
      const watchedTag = tags.find(t => t.name === 'watched');

      // Use the API directly to add folder with auto-tag
      await app.page.evaluate(async ({ folderPath, tagId }) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.AddWatchedFolder({
          path: folderPath,
          filter_mode: 'all',
          filter_presets: [],
          filter_regex: '',
          process_existing: false,
          auto_archive: false,
          auto_tag_id: tagId
        });
        // @ts-ignore
        await window.go.main.App.RefreshWatches();
      }, { folderPath: watchDir, tagId: watchedTag!.id });

      await app.page.waitForTimeout(500);

      // Enable global watch via API
      await app.page.evaluate(async () => {
        // @ts-ignore
        await window.go.main.App.SetGlobalWatchPaused(false);
        // @ts-ignore
        await window.go.main.App.RefreshWatches();
      });
      await app.page.waitForTimeout(1000);

      // Drop a file into the watched folder
      const imageContent = generateTestImage(100, 100, [255, 128, 0]);
      const testFilePath = path.join(watchDir, 'auto-tagged-image.png');
      await fs.writeFile(testFilePath, imageContent);

      // Wait for the file to be imported
      await app.page.waitForTimeout(3000);

      // Refresh clips via API
      await app.page.evaluate(async () => {
        // @ts-ignore
        if (window.__testHelpers) window.__testHelpers.loadClips();
      });
      await app.page.waitForTimeout(500);

      // Verify clip was imported with the tag
      await app.expectClipCount(1);
      await app.expectClipHasTag('auto-tagged-image.png', 'watched');
    });

    test('should not auto-tag when no auto-tag is configured', async ({ app }) => {
      // Create a tag but don't assign it to watch folder
      await app.createTag('unused');

      // Add watch folder without auto-tag via API
      await app.page.evaluate(async (folderPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.AddWatchedFolder({
          path: folderPath,
          filter_mode: 'all',
          filter_presets: [],
          filter_regex: '',
          process_existing: false,
          auto_archive: false,
          auto_tag_id: null
        });
        // @ts-ignore
        await window.go.main.App.RefreshWatches();
      }, watchDir);

      // Enable watching via API
      await app.page.evaluate(async () => {
        // @ts-ignore
        await window.go.main.App.SetGlobalWatchPaused(false);
        // @ts-ignore
        await window.go.main.App.RefreshWatches();
      });
      await app.page.waitForTimeout(1000);

      // Drop a file
      const imageContent = generateTestImage(100, 100, [0, 255, 128]);
      const testFilePath = path.join(watchDir, 'no-tag-image.png');
      await fs.writeFile(testFilePath, imageContent);

      // Wait for import
      await app.page.waitForTimeout(3000);

      // Refresh clips via API
      await app.page.evaluate(async () => {
        // @ts-ignore
        if (window.__testHelpers) window.__testHelpers.loadClips();
      });
      await app.page.waitForTimeout(500);

      // Verify clip exists but has no tags
      await app.expectClipCount(1);
      await app.expectClipDoesNotHaveTag('no-tag-image.png', 'unused');
    });
  });

  test.describe('Edit Auto-Tag', () => {
    test('should update auto-tag for existing watch folder', async ({ app }) => {
      // Create tags
      await app.createTag('original');
      await app.createTag('updated');

      // Add watch folder via API
      await app.page.evaluate(async (folderPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.AddWatchedFolder({
          path: folderPath,
          filter_mode: 'all',
          filter_presets: [],
          filter_regex: '',
          process_existing: false,
          auto_archive: false,
          auto_tag_id: null
        });
        // @ts-ignore
        await window.go.main.App.RefreshWatches();
      }, watchDir);

      // Get folder and update via API
      const tags = await app.getAllTags();
      const updatedTag = tags.find(t => t.name === 'updated');

      await app.page.evaluate(async ({ folderPath, tagId }) => {
        // @ts-ignore
        const folders = await window.go.main.App.GetWatchedFolders();
        const folder = folders.find((f: any) => f.path === folderPath);
        if (folder) {
          // @ts-ignore
          await window.go.main.App.UpdateWatchedFolder(folder.id, {
            filter_mode: folder.filter_mode || 'all',
            filter_presets: folder.filter_presets || [],
            filter_regex: folder.filter_regex || '',
            process_existing: folder.process_existing || false,
            auto_archive: folder.auto_archive || false,
            auto_tag_id: tagId
          });
        }
        // @ts-ignore
        await window.go.main.App.RefreshWatches();
      }, { folderPath: watchDir, tagId: updatedTag!.id });

      // Enable watching via API
      await app.page.evaluate(async () => {
        // @ts-ignore
        await window.go.main.App.SetGlobalWatchPaused(false);
        // @ts-ignore
        await window.go.main.App.RefreshWatches();
      });
      await app.page.waitForTimeout(1000);

      const imageContent = generateTestImage(100, 100, [128, 0, 255]);
      const testFilePath = path.join(watchDir, 'updated-tag-image.png');
      await fs.writeFile(testFilePath, imageContent);

      await app.page.waitForTimeout(3000);

      // Refresh clips via API
      await app.page.evaluate(async () => {
        // @ts-ignore
        if (window.__testHelpers) window.__testHelpers.loadClips();
      });
      await app.page.waitForTimeout(500);

      // Should have the updated tag
      await app.expectClipCount(1);
      await app.expectClipHasTag('updated-tag-image.png', 'updated');
    });
  });
});
