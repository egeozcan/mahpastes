import { test, expect } from '../../fixtures/test-fixtures';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

/**
 * A watch import used to be prepended to the gallery by hand, so it showed up
 * in views it does not belong to (folder mode, descendant filters, search,
 * sort). It now reloads the listing on screen.
 */
test.describe('Watch import and the current view', () => {
  test('an auto-tagged import does not appear at the folder-mode root', async ({ app }) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'mp-watch-view-'));
    try {
      const { tagID } = await app.createTag('inbox');
      await app.toggleFolderMode();

      const folderId = await app.page.evaluate(async ({ dir, tagID }) => {
        const api = (window as any).go.main.App;
        const folder = await api.AddWatchedFolder({
          path: dir, filter_mode: 'all', filter_presets: [], filter_regex: '',
          process_existing: false, auto_archive: false, auto_tag_id: tagID,
        });
        return folder.id;
      }, { dir, tagID });

      fs.writeFileSync(path.join(dir, 'dropped.txt'), 'imported by the watcher');
      const renderSeq = await app.page.evaluate(() => (window as any).__galleryRenderSeq || 0);
      await app.page.evaluate((id) => (window as any).go.main.App.ProcessExistingFilesInFolder(id), folderId);
      await app.expectToast('Imported: dropped.txt');

      // The reload is debounced; wait for it to render, then check the root.
      await app.page.waitForFunction(
        (prev) => ((window as any).__galleryRenderSeq || 0) > prev,
        renderSeq,
      );
      await app.expectClipNotVisible('dropped.txt');
      await expect(app.page.locator('[data-testid="folder-card-inbox"]')).toContainText('1 clip');
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });
});
