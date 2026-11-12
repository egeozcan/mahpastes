import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import { generateTestText } from '../../helpers/test-data';
import * as fs from 'fs/promises';
import * as path from 'path';

// These tests poll the database directly instead of using waitForWatchImport:
// its fallback runs ProcessExistingFilesInFolder, which would import the file
// even when the live watch is dead — exactly what these tests must catch.
async function expectLiveImport(app: any, expected: number): Promise<void> {
  await expect.poll(async () => app.getClipCountFromDB(), { timeout: 20000, intervals: [250, 500, 1000] })
    .toBe(expected);
}

async function exists(p: string): Promise<boolean> {
  try {
    await fs.stat(p);
    return true;
  } catch {
    return false;
  }
}

test.describe('Watch Folder Robustness', () => {
  test('keeps watching after a backup is restored', async ({ app, tempDir }) => {
    const watched = path.join(tempDir, 'watched');
    await fs.mkdir(watched);
    await app.openWatchView();
    await app.addWatchFolder(watched);
    await app.toggleGlobalWatch(true);
    await app.closeWatchView();

    const backupPath = path.join(tempDir, `restore-${Date.now()}.zip`);
    await app.page.evaluate(async (dest) => {
      // @ts-ignore - Wails runtime
      await window.go.main.App.CreateBackup(dest);
    }, backupPath);
    await app.page.evaluate(async (file) => {
      // @ts-ignore - Wails runtime
      await window.go.main.App.ConfirmRestoreBackup(file, 'none');
    }, backupPath);

    // Restored folders come back paused; resuming one must watch it again.
    await app.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const App = window.go.main.App;
      for (const f of await App.GetWatchedFolders()) {
        await App.SetFolderPaused(f.id, false);
      }
      await App.RefreshWatches();
    });

    const file = path.join(watched, `after-restore-${Date.now()}.txt`);
    await fs.writeFile(file, generateTestText('after-restore'));
    await expectLiveImport(app, 1);
    await expect.poll(() => exists(file)).toBe(false);
  });

  test('leaves downloads in progress and empty placeholders alone', async ({ app, tempDir }) => {
    await app.openWatchView();
    await app.addWatchFolder(tempDir);
    await app.toggleGlobalWatch(true);
    await app.closeWatchView();

    const stamp = Date.now();
    const finalPath = path.join(tempDir, `download-${stamp}.txt`);
    const partPath = `${finalPath}.part`;
    const chromePath = path.join(tempDir, `other-${stamp}.txt.crdownload`);
    await fs.writeFile(finalPath, ''); // Firefox's empty placeholder
    await fs.writeFile(partPath, generateTestText('half'));
    await fs.writeFile(chromePath, generateTestText('half'));

    await app.page.waitForTimeout(2000);
    expect(await app.getClipCountFromDB()).toBe(0);
    expect(await exists(finalPath)).toBe(true);
    expect(await exists(partPath)).toBe(true);
    expect(await exists(chromePath)).toBe(true);

    // The download completes: the .part file is renamed over the placeholder.
    await fs.rename(partPath, finalPath);
    await expectLiveImport(app, 1);
    await expect.poll(() => exists(finalPath)).toBe(false);
    expect(await exists(chromePath)).toBe(true);
  });

  test('shows a deleted folder as not found and watches it again when it returns', async ({ app, tempDir }) => {
    const watched = path.join(tempDir, 'inbox');
    await fs.mkdir(watched);
    await app.openWatchView();
    await app.addWatchFolder(watched);
    await app.toggleGlobalWatch(true);

    await fs.rm(watched, { recursive: true });
    const card = app.page.locator(selectors.watch.folderCard).filter({ hasText: watched });
    // Reported at once where the OS sends the folder's own removal event,
    // otherwise by the periodic re-arm (30 s).
    await expect(card.locator(selectors.watchFolder.missingBadge)).toBeVisible({ timeout: 40000 });
    await expect(app.page.locator(selectors.drawer.watchIndicator)).toHaveAttribute('data-state', 'unavailable');
    // The menu button's dot (visible with the drawer closed) turns amber too.
    await expect(app.page.locator(selectors.drawer.activityIndicator)).toHaveAttribute('data-state', 'unavailable');

    // The folder comes back; a refresh (or the periodic re-arm) watches it again.
    await fs.mkdir(watched);
    await app.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      await window.go.main.App.RefreshWatches();
    });
    await expect(card.locator(selectors.watchFolder.missingBadge)).toHaveCount(0, { timeout: 10000 });
    await app.closeWatchView();

    const file = path.join(watched, `returned-${Date.now()}.txt`);
    await fs.writeFile(file, generateTestText('returned'));
    await expectLiveImport(app, 1);
  });
});
