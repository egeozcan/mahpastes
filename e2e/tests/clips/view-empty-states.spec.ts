import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

// Small dead ends in the secondary views: a Save button disabled with no
// reason, pickers that said nothing useful with no tags, blank share lists,
// and copy buttons that failed without a word.
test.describe('Secondary views say what to do next', () => {
  test('a new watched folder starts on "All files", and an empty filter says why Save is off', async ({ app }) => {
    await app.page.evaluate(() => (window as any).openFolderModal('/tmp/somewhere'));
    const save = app.page.locator(selectors.watchEdit.saveButton);
    const hint = app.page.locator('[data-testid="filter-empty-hint"]');
    await expect(app.page.locator(selectors.watchEdit.filterAll)).toBeChecked();
    await expect(save).toBeEnabled();
    await expect(hint).toBeHidden();

    await app.page.locator(selectors.watchEdit.filterAll).uncheck();
    await expect(save).toBeDisabled();
    await expect(hint).toBeVisible();
    await expect(hint).toHaveText('Pick at least one file type, or enter a custom filter, to enable saving.');
    await app.page.locator(selectors.watchEdit.cancelButton).click();
  });

  test('with no tags, the tag filter, Serve and Share say how a tag is made', async ({ app }) => {
    await app.openTagFilterDropdown();
    await expect(app.page.locator('#tag-filter-list')).toContainText('Add one from a clip\'s menu (Tags), or create a folder in folder mode.');
    await app.page.keyboard.press('Escape');

    await app.switchToServeView();
    await app.page.locator(selectors.serve.addBtn).click();
    await app.expectToast('No tags yet. Tag a clip from its menu (Tags), or create a folder in folder mode, then serve it here.');

    // Share: no empty picker to dead-end in.
    await app.page.evaluate(() => (window as any).openShareFlowForTag?.());
    await expect(app.page.locator('#create-share-modal')).not.toHaveClass(/opacity-100/);
  });

  test('when every tag is already listed, Serve says so', async ({ app }) => {
    const { tagID } = await app.createTag('only');
    await app.switchToServeView();
    await app.page.evaluate(async (id) => {
      // @ts-ignore - app global
      await addConfiguredServeEntry(id, 'only');
    }, tagID);
    await app.page.locator(selectors.serve.addBtn).click();
    await app.expectToast('Every tag is already in this list.');
  });

  test('copying a link falls back when the async clipboard is missing, and says so when it fails', async ({ app }) => {
    const results = await app.page.evaluate(async () => {
      const w = window as any;
      const originalClipboard = Object.getOwnPropertyDescriptor(Navigator.prototype, 'clipboard');
      const originalExec = document.execCommand;
      const calls: string[] = [];
      // As on a plain-http page: no navigator.clipboard at all.
      Object.defineProperty(Navigator.prototype, 'clipboard', { configurable: true, get: () => undefined });
      try {
        document.execCommand = ((cmd: string) => { calls.push(cmd); return true; }) as any;
        const viaFallback = await w.copyTextQuietly('http://127.0.0.1:1234');
        document.execCommand = (() => false) as any;
        const refused = await w.copyTextQuietly('http://127.0.0.1:1234');
        return { viaFallback, refused, calls };
      } finally {
        document.execCommand = originalExec;
        if (originalClipboard) Object.defineProperty(Navigator.prototype, 'clipboard', originalClipboard);
      }
    });
    expect(results).toEqual({ viaFallback: true, refused: false, calls: ['copy'] });
  });

  test('the share lists say what they would hold', async ({ app }) => {
    await app.page.evaluate(() => (window as any).__testHelpers.switchView('share'));
    await expect(app.page.locator('#share-publications-list')).toContainText('Nothing shared yet.');
    await expect(app.page.locator('#share-follows-list')).toContainText('Not following anyone.');
  });
});
