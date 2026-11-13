import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import * as path from 'path';

// Reading and uploading showed nothing until the final toast, and one file
// that could not be read dropped the whole batch without a word.
test.describe('Upload feedback', () => {
  test('a slow upload shows its stage in the bottom bar and clears it afterwards', async ({ app }) => {
    await app.page.evaluate(() => {
      const w = window as any;
      const api = w.go.main.App;
      const original = api.UploadFiles;
      const waiting: Array<() => void> = [];
      let released = false;
      api.UploadFiles = (...args: unknown[]) => {
        if (released) return original(...args);
        return new Promise((resolve, reject) => { waiting.push(() => original(...args).then(resolve, reject)); });
      };
      w.__releaseUpload = () => { released = true; waiting.splice(0).forEach(run => run()); };
      w.__restoreUpload = () => { api.UploadFiles = original; };
    });
    const status = app.page.locator('[data-testid="upload-status"]');
    try {
      await expect(status).toBeHidden();
      const filePath = await createTempFile(generateTestImage(), 'png');
      await app.page.locator('#file-input').setInputFiles([filePath]);
      await expect(status).toBeVisible();
      await expect(status).toHaveText('Uploading…');

      await app.page.evaluate(() => (window as any).__releaseUpload());
      await app.expectClipVisible(path.basename(filePath));
      await expect(status).toBeHidden();
    } finally {
      await app.page.evaluate(() => { (window as any).__releaseUpload(); (window as any).__restoreUpload(); });
    }
  });

  test('an unreadable file is named and the rest of the batch is uploaded', async ({ app }) => {
    const good = await createTempFile(generateTestImage(), 'png');
    const bad = await createTempFile(generateTestImage(20, 20, [0, 0, 255]), 'png');
    const badName = path.basename(bad);
    await app.page.evaluate((name) => {
      const w = window as any;
      const original = w.fileToFileData;
      w.fileToFileData = (file: File) => file.name === name ? Promise.reject(new Error('NotReadableError')) : original(file);
      w.__restoreRead = () => { w.fileToFileData = original; };
    }, badName);
    try {
      await app.page.locator('#file-input').setInputFiles([good, bad]);
      await app.expectClipVisible(path.basename(good));
      await app.expectToast(`1 uploaded. Could not read ${badName}.`);
      await app.expectClipCount(1);
    } finally {
      await app.page.evaluate(() => (window as any).__restoreRead());
    }
  });

  test('a quick upload shows no status at all', async ({ app }) => {
    await app.page.evaluate(() => {
      const el = document.getElementById('upload-status')!;
      (window as any).__statusShown = false;
      new MutationObserver(() => { if (!el.hidden) (window as any).__statusShown = true; })
        .observe(el, { attributes: true, attributeFilter: ['hidden'] });
    });
    const filePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(filePath);
    await app.page.waitForTimeout(500);
    expect(await app.page.evaluate(() => (window as any).__statusShown)).toBe(false);
  });
});
