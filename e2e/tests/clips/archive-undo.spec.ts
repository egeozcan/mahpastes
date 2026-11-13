import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

// Archive is one unconfirmed key or click, so its toast offers the way back.
test.describe('Archive undo', () => {
  const undo = (app: any) => app.page.locator(selectors.toast.action);

  test('Undo on the archive toast brings the clip back', async ({ app }) => {
    const filePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);

    await app.archiveClip(filename);
    await app.expectClipCount(0);
    await app.expectToast('Clip archived.');
    await expect(undo(app)).toHaveText('Undo');
    await undo(app).click();

    await app.expectClipVisible(filename);
    await app.expectClipCount(1);
    await app.toggleArchiveView();
    await app.expectClipCount(0);
  });

  test('Undo on the restore toast archives the clip again', async ({ app }) => {
    const filePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);
    await app.archiveClip(filename);
    await app.toggleArchiveView();
    await app.expectClipVisible(filename);

    await app.archiveClip(filename); // the same control restores in this view
    await app.expectClipCount(0);
    await app.expectToast('Clip restored.');
    await undo(app).click();
    await app.expectClipVisible(filename);
  });

  test('Undo after a bulk archive restores every clip of the selection', async ({ app }) => {
    const names: string[] = [];
    for (const color of [[255, 0, 0], [0, 255, 0]] as [number, number, number][]) {
      const filePath = await createTempFile(generateTestImage(20, 20, color), 'png');
      names.push(path.basename(filePath));
      await app.uploadFile(filePath);
    }
    await app.selectClips(names);
    await app.bulkArchive();
    await app.expectClipCount(0);
    await app.waitForToast(/Archived 2 clips\./);
    await undo(app).click();
    await app.expectClipCount(2);
  });

  test('a bulk archive of one clip says "1 clip"', async ({ app }) => {
    const filePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);
    await app.selectClips([filename]);
    await app.bulkArchive();
    await app.waitForToast(/Archived 1 clip\./);
  });

  test('Undo clicked from another view still restores the clip', async ({ app }) => {
    const filePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);
    await app.archiveClip(filename);
    await expect(undo(app)).toBeVisible();

    // Straight to the click: the toast is on a timer.
    await app.toggleArchiveView();
    await undo(app).click();
    await app.expectClipCount(0);
    await app.toggleArchiveView();
    await app.expectClipVisible(filename);
  });
});
