import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestText } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

/**
 * Saving a clip to a file offers "Show in folder" in the confirmation toast,
 * which reveals the written file in Finder, Explorer or the Linux file
 * manager. The native save dialog and the file manager are stubbed so the
 * test exercises only the frontend wiring: the path SaveClipToFile resolves
 * to is the one handed to RevealInFileManager.
 */
test.describe('Save clip: Show in folder', () => {
  test.afterEach(async ({ app }) => {
    await app.page.evaluate(() => {
      const w = window as any;
      if (w.__origSaveClipToFile) w.go.main.App.SaveClipToFile = w.__origSaveClipToFile;
      if (w.__origRevealInFileManager) w.go.main.App.RevealInFileManager = w.__origRevealInFileManager;
    });
  });

  async function stubSave(app: any, savedPath: string): Promise<void> {
    await app.page.evaluate((savedPath: string) => {
      const w = window as any;
      w.__origSaveClipToFile = w.go.main.App.SaveClipToFile;
      w.__origRevealInFileManager = w.go.main.App.RevealInFileManager;
      w.__revealed = [];
      w.go.main.App.SaveClipToFile = async () => savedPath;
      w.go.main.App.RevealInFileManager = async (p: string) => { w.__revealed.push(p); };
    }, savedPath);
  }

  test('save toast reveals the saved file', async ({ app }) => {
    const textPath = await createTempFile(generateTestText('save-reveal'), 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);

    const savedPath = '/tmp/mahpastes e2e/saved, copy.txt';
    await stubSave(app, savedPath);
    await app.saveClipToFile(filename);

    await app.expectToast('Saved clip to file');
    const action = app.page.locator(selectors.toast.action);
    await expect(action).toHaveText('Show in folder');
    await action.click();

    await expect.poll(() => app.page.evaluate(() => (window as any).__revealed)).toEqual([savedPath]);
    // Clicking the action dismisses the toast and drops the button with it.
    await expect(action).toHaveCount(0);
  });

  test('cancelled save shows no toast', async ({ app }) => {
    const textPath = await createTempFile(generateTestText('save-cancel'), 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);

    await stubSave(app, '');
    await app.saveClipToFile(filename);

    // Give the async handler a moment; nothing may appear.
    await app.page.waitForTimeout(300);
    await expect(app.page.locator(selectors.toast.action)).toHaveCount(0);
    await expect(app.page.locator(selectors.toast.container)).not.toContainText('Saved clip to file');
  });

  test('a failed reveal is reported', async ({ app }) => {
    const textPath = await createTempFile(generateTestText('save-reveal-fail'), 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);

    await stubSave(app, '/tmp/gone.txt');
    await app.page.evaluate(() => {
      (window as any).go.main.App.RevealInFileManager = async () => { throw new Error('file not found'); };
    });
    await app.saveClipToFile(filename);
    await app.page.locator(selectors.toast.action).click();
    await app.expectToast('Failed to show file');
  });
});
