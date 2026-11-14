import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

// A clip with no in-app viewer (a PDF, an archive) used to ignore a click on
// its preview and Enter on its card. Now both say there is no preview and
// offer Open; neither launches another application without that second step.
test.describe('Cards without an in-app viewer', () => {
  async function uploadPdf(app: any): Promise<string> {
    const pdfPath = await createTempFile(Buffer.from('%PDF-1.4\n%%EOF\n'), 'pdf');
    await app.uploadFile(pdfPath);
    await app.page.evaluate(() => {
      (window as any).__openedWithDefaultApp = [];
      const api = (window as any).go.main.App;
      api.OpenClipWithDefaultApp = async (id: number) => { (window as any).__openedWithDefaultApp.push(id); };
    });
    return path.basename(pdfPath);
  }
  const opened = (app: any) => app.page.evaluate(() => (window as any).__openedWithDefaultApp);

  test('a click on the preview offers Open, which opens the clip', async ({ app }) => {
    const filename = await uploadPdf(app);
    const card = await app.getClipByFilename(filename);
    const id = Number(await card.getAttribute('data-id'));

    await card.locator('[data-action="open-lightbox"]').click();
    await app.expectToast('No preview for this file type.');
    expect(await opened(app)).toEqual([]);

    await app.page.locator(selectors.toast.action).click();
    await expect.poll(() => opened(app)).toEqual([id]);
  });

  test('Enter on the focused card does the same', async ({ app }) => {
    const filename = await uploadPdf(app);
    const card = await app.getClipByFilename(filename);
    await card.focus();
    await app.page.keyboard.press('Enter');
    await app.expectToast('No preview for this file type.');
    await expect(app.page.locator(selectors.toast.action)).toHaveText('Open');
    expect(await opened(app)).toEqual([]);
  });
});
