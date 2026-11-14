import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

// The search box had no way to empty it short of selecting the text, and
// Escape did nothing in it: the query went on filtering and single-letter
// shortcuts went on typing into the field.
test.describe('Clearing the search box', () => {
  const input = (app: any) => app.page.locator(selectors.header.searchInput);
  const clear = (app: any) => app.page.locator(selectors.header.searchClearButton);

  test('the clear button appears with a query and empties it', async ({ app }) => {
    const filePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(filePath);
    await expect(clear(app)).toBeHidden();

    await app.search('zzzznomatch');
    await expect(app.page.locator(`${selectors.gallery.clipCard}:visible`)).toHaveCount(0);
    await expect(clear(app)).toBeVisible();

    await clear(app).click();
    await expect(input(app)).toHaveValue('');
    await expect(input(app)).toBeFocused();
    await expect(clear(app)).toBeHidden();
    await app.expectClipVisible(path.basename(filePath));
  });

  test('Escape clears the query, and a second Escape returns focus to the gallery', async ({ app }) => {
    const filePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);

    await input(app).fill('zzzznomatch');
    await app.page.keyboard.press('Escape');
    await expect(input(app)).toHaveValue('');
    await expect(input(app)).toBeFocused();
    await app.expectClipVisible(filename);

    await app.page.keyboard.press('Escape');
    await expect(await app.getClipByFilename(filename)).toBeFocused();
  });

  test('Escape clears the query before it clears a selection', async ({ app }) => {
    const filePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);
    await app.selectClips([filename]);

    await input(app).fill('png');
    await app.page.keyboard.press('Escape');
    await expect(input(app)).toHaveValue('');
    await expect(app.page.locator(selectors.bulk.toolbar)).toBeVisible();
  });

  test('Escape still closes the search options popover first', async ({ app }) => {
    await input(app).fill('query');
    await app.page.click('#search-options-btn');
    await expect(app.page.locator('.search-options-popover')).toBeVisible();
    await app.page.keyboard.press('Escape');
    await expect(app.page.locator('.search-options-popover')).toHaveCount(0);
    await expect(input(app)).toHaveValue('query');
  });

  test('Escape leaves a deep search and restores the plain listing', async ({ app }) => {
    const textPath = await createTempFile('needle in the body', 'txt');
    const other = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(textPath);
    await app.uploadFile(other);
    await app.page.evaluate(async () => {
      // @ts-ignore
      await window.go.main.App.SetSetting('search_in_content', 'true');
    });
    await app.page.click('#search-options-btn');
    const contents = app.page.locator('.search-options-popover input[type="checkbox"]').first();
    if (!(await contents.isChecked())) await contents.check();
    await app.page.keyboard.press('Escape');

    await input(app).fill('needle');
    await app.expectClipCount(1);
    await input(app).focus();
    await app.page.keyboard.press('Escape');
    await expect(input(app)).toHaveValue('');
    await app.expectClipCount(2);
  });
});
