import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

// A clip uploaded while a tag filter or a search narrows the gallery used to
// vanish: the toast said "1 uploaded." over a view that did not contain it.
test.describe('Upload under a narrowed view', () => {
  const action = (app: any) => app.page.locator(selectors.toast.action);

  test('under a tag filter the toast says the clip is not shown, and Show clears the filter', async ({ app }) => {
    await app.createTag('work');
    await app.filterByTag('work');

    const filePath = await createTempFile(generateTestImage(), 'png');
    await app.page.locator(selectors.upload.fileInput).setInputFiles([filePath]);

    await app.waitForToast(/1 uploaded\. Not shown in the current view\./);
    await app.expectClipCount(0);
    await action(app).click();
    await app.expectClipVisible(path.basename(filePath));
  });

  test('under a search that does not match, Show clears the search', async ({ app }) => {
    const first = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(first);
    await app.search('zzzznomatch');

    const filePath = await createTempFile(generateTestImage(30, 30, [0, 0, 255]), 'png');
    await app.page.locator(selectors.upload.fileInput).setInputFiles([filePath]);

    await app.waitForToast(/1 uploaded\. Not shown in the current view\./);
    await action(app).click();
    await expect(app.page.locator(selectors.header.searchInput)).toHaveValue('');
    await app.expectClipVisible(path.basename(filePath));
    await app.expectClipCount(2);
  });

  test('a search the new clip matches leaves the plain toast', async ({ app }) => {
    await app.search('png');
    const filePath = await createTempFile(generateTestImage(), 'png');
    await app.page.locator(selectors.upload.fileInput).setInputFiles([filePath]);
    await app.expectClipVisible(path.basename(filePath));
    await app.expectToast('1 uploaded.');
    await expect(action(app)).toHaveCount(0);
  });
});
