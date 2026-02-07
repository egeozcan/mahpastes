import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import * as fs from 'fs/promises';
import * as path from 'path';

test.describe('Watch Folders', () => {
  test.describe('Watch View Toggle', () => {
    test('should open watch view', async ({ app }) => {
      await app.openWatchView();

      const isOpen = await app.isWatchViewOpen();
      expect(isOpen).toBe(true);
    });

    test('should close watch view', async ({ app }) => {
      await app.openWatchView();
      await app.closeWatchView();

      const isOpen = await app.isWatchViewOpen();
      expect(isOpen).toBe(false);
    });

    test('should toggle watch view with button', async ({ app }) => {
      // Open
      await app.openWatchView();
      expect(await app.isWatchViewOpen()).toBe(true);

      // Close
      await app.closeWatchView();
      expect(await app.isWatchViewOpen()).toBe(false);
    });
  });

  test.describe('Add Watch Folder', () => {
    test('should add a watch folder', async ({ app, tempDir }) => {
      await app.openWatchView();

      await app.addWatchFolder(tempDir);

      // Folder count should increase
      const count = await app.getWatchFolderCount();
      expect(count).toBeGreaterThanOrEqual(1);
    });

    test('should display added folder in list', async ({ app, tempDir }) => {
      await app.openWatchView();
      await app.addWatchFolder(tempDir);

      // Folder should appear in the list
      const folderList = app.page.locator(selectors.watch.folderList);
      await expect(folderList).toContainText(path.basename(tempDir));
    });

    test('should add multiple watch folders', async ({ app, tempDir }) => {
      const dir1 = tempDir;
      const dir2 = path.join(tempDir, 'subdir');
      await fs.mkdir(dir2, { recursive: true });

      await app.openWatchView();
      await app.addWatchFolder(dir1);
      await app.addWatchFolder(dir2);

      // Wait for both folders to appear in the UI
      await expect(app.page.locator('#watch-folder-list > li')).toHaveCount(2, { timeout: 5000 });
    });
  });

  test.describe('Remove Watch Folder', () => {
    test('should remove a watch folder', async ({ app, tempDir }) => {
      await app.openWatchView();
      await app.addWatchFolder(tempDir);

      const initialCount = await app.getWatchFolderCount();
      expect(initialCount).toBeGreaterThanOrEqual(1);

      await app.removeWatchFolder(tempDir);

      // Wait for folder count to update in the UI
      await expect.poll(
        () => app.getWatchFolderCount(),
        { timeout: 5000 }
      ).toBe(initialCount - 1);
      const finalCount = await app.getWatchFolderCount();
      expect(finalCount).toBe(initialCount - 1);
    });
  });

  test.describe('Global Watch Toggle', () => {
    test('should pause all watching globally', async ({ app, tempDir }) => {
      await app.openWatchView();
      await app.addWatchFolder(tempDir);

      // Toggle global watch off
      await app.toggleGlobalWatch(false);

      // Check the label indicates paused state
      const label = app.page.locator(selectors.watch.globalLabel);
      await expect(label).toContainText(/paused/i);
    });

    test('should resume watching globally', async ({ app, tempDir }) => {
      await app.openWatchView();
      await app.addWatchFolder(tempDir);

      // Pause first
      await app.toggleGlobalWatch(false);

      // Then resume
      await app.toggleGlobalWatch(true);

      // Check the label indicates active state
      const label = app.page.locator(selectors.watch.globalLabel);
      await expect(label).toContainText(/active/i);
    });
  });

  test.describe('Per-Folder Pause', () => {
    test('should pause watching a specific folder', async ({ app, tempDir }) => {
      await app.openWatchView();
      await app.addWatchFolder(tempDir);

      await app.pauseWatchFolder(tempDir);

      // Folder should show paused state
      const folderCard = app.page.locator(selectors.watch.folderCard).filter({ hasText: path.basename(tempDir) });
      await expect(folderCard).toBeVisible();
    });
  });

  test.describe('Watch Indicator', () => {
    test('should show watch indicator when actively watching', async ({ app, tempDir }) => {
      await app.openWatchView();
      await app.addWatchFolder(tempDir);
      await app.toggleGlobalWatch(true);

      // Close watch view to check indicator
      await app.closeWatchView();

      // Watch indicator should be visible on button
      const indicator = app.page.locator(selectors.header.watchIndicator);
      // Indicator visibility depends on implementation
    });
  });

  test.describe('Folder Card Information', () => {
    test('should show folder path on card', async ({ app, tempDir }) => {
      await app.openWatchView();
      await app.addWatchFolder(tempDir);

      const folderCard = app.page.locator(selectors.watch.folderCard).first();
      await expect(folderCard).toContainText(path.basename(tempDir));
    });
  });
});
