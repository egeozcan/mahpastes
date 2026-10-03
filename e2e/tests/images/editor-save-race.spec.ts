import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import * as path from 'path';

/** Hold every UpdateClipData call until window.__releaseSave() is called. */
async function gateSaves(page: any): Promise<void> {
  await page.evaluate(() => {
    const app = (window as any).go.main.App;
    const original = app.UpdateClipData;
    (window as any).__restoreUpdateClipData = () => { app.UpdateClipData = original; };
    app.UpdateClipData = (...args: any[]) => new Promise((resolve, reject) => {
      (window as any).__releaseSave = () => original(...args).then(resolve, reject);
    });
  });
}

async function releaseSave(page: any): Promise<void> {
  await page.waitForFunction(() => typeof (window as any).__releaseSave === 'function');
  await page.evaluate(() => {
    (window as any).__releaseSave();
    (window as any).__releaseSave = null;
  });
}

test.describe('Image editor save races', () => {
  test.afterEach(async ({ app }) => {
    // Settle a save a failed test left gated before unhooking the stub.
    await app.page.evaluate(() => {
      const w = window as any;
      w.__releaseSave?.();
      w.__restoreUpdateClipData?.();
      delete w.__releaseSave;
      delete w.__restoreUpdateClipData;
    });
  });

  test('drawing while an in-place save is in flight keeps the editor open and dirty', async ({ app }) => {
    const file = await createTempFile(generateTestImage(200, 200, [255, 255, 255]), 'png');
    await app.uploadFile(file);
    await app.openImageEditor(path.basename(file));
    await app.selectTool('brush');
    await app.drawOnCanvas({ x: 20, y: 20 }, { x: 80, y: 80 });

    await gateSaves(app.page);
    await app.page.locator('#editor-save-in-place').click();
    await app.page.waitForFunction(() => typeof (window as any).__releaseSave === 'function');
    await app.drawOnCanvas({ x: 120, y: 20 }, { x: 160, y: 90 });
    await releaseSave(app.page);

    await app.expectToast('still unsaved');
    await expect(app.page.locator(`${selectors.editor.modal}.active`)).toHaveCount(1);
    await expect(app.page.locator('#editor-save-in-place')).toBeEnabled();
    await app.closeImageEditor();
  });

  test('a late save does not close an editor opened on another clip', async ({ app }) => {
    const first = await createTempFile(generateTestImage(120, 120, [255, 0, 0]), 'png');
    const second = await createTempFile(generateTestImage(120, 120, [0, 0, 255]), 'png');
    await app.uploadFile(first);
    await app.uploadFile(second);

    await app.openImageEditor(path.basename(first));
    await app.selectTool('brush');
    await app.drawOnCanvas({ x: 10, y: 10 }, { x: 60, y: 60 });
    await gateSaves(app.page);
    await app.page.locator('#editor-save-in-place').click();
    await app.page.waitForFunction(() => typeof (window as any).__releaseSave === 'function');

    // Leave that editor and open the other clip while the write is pending.
    await app.page.evaluate(() => (window as any).closeEditor({ force: true }));
    await app.page.waitForSelector(`${selectors.editor.modal}:not(.active)`);
    await app.openImageEditor(path.basename(second));

    // The save ends with loadClips() on every path, after any closeEditor(),
    // so a render past this point means the late save has fully settled.
    const renderSeq = await app.page.evaluate(() => (window as any).__galleryRenderSeq || 0);
    await releaseSave(app.page);
    await app.expectToast('Saved');
    await app.page.waitForFunction(
      (prev) => ((window as any).__galleryRenderSeq || 0) > prev,
      renderSeq,
    );
    await expect(app.page.locator(`${selectors.editor.modal}.active`)).toHaveCount(1);
    await app.closeImageEditor();
  });
});
