import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  uniqueId,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';
import * as fs from 'fs/promises';

/**
 * The search box has two opt-in widenings, both of which have to be answered by
 * the database rather than by filtering the cards on screen: the gallery holds
 * only a preview of each clip, and none of the clips a hidden tag filtered out.
 */
test.describe('Search options', () => {
  test('finds a clip by words inside it only when content search is on', async ({ app }) => {
    const needle = `swordfish${uniqueId()}`;
    // The needle appears in the body only — the filename is random.
    const withNeedle = await createTempFile(`nothing to see here\n${needle}\nlast line`, 'txt');
    const without = await createTempFile('an unrelated body of text', 'txt');

    await app.uploadFiles([withNeedle, without]);
    await app.expectClipCount(2);

    // Default: filenames and content types only, so the needle finds nothing.
    await app.search(needle);
    await app.expectClipNotVisible(path.basename(withNeedle));

    await app.setSearchOption('content', true);
    await app.searchAndWaitForResults(needle);

    await app.expectClipVisible(path.basename(withNeedle));
    await app.expectClipNotVisible(path.basename(without));
  });

  test('content search is remembered as a setting', async ({ app }) => {
    await app.setSearchOption('content', true);

    const saved = await app.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      return window.go.main.App.GetSetting('search_in_content');
    });
    expect(saved).toBe('true');
  });

  test('clearing the query restores the plain listing', async ({ app }) => {
    const needle = `haystack${uniqueId()}`;
    const withNeedle = await createTempFile(`body containing ${needle}`, 'txt');
    const other = await createTempFile(generateTestImage(20, 20, [0, 0, 255]), 'png');

    await app.uploadFiles([withNeedle, other]);
    await app.setSearchOption('content', true);

    await app.searchAndWaitForResults(needle);
    await app.expectClipCount(1);

    await app.searchAndWaitForResults('');
    await app.expectClipCount(2);
  });

  test('surfaces clips behind a hidden tag when asked, dimmed', async ({ app }) => {
    const marker = `buried${uniqueId()}`;
    const source = await createTempFile(`content of ${marker}`, 'txt');
    const named = path.join(path.dirname(source), `${marker}.txt`);
    await fs.rename(source, named);

    await app.uploadFile(named);
    await app.createTag('private-search');
    await app.addTagToClip(`${marker}.txt`, 'private-search');
    await app.setHiddenTags(['private-search']);
    await app.refreshClips();

    // Hidden means hidden: the clip is not in the gallery to be filtered at all.
    await app.searchAndWaitForResults(marker);
    await app.expectClipNotVisible(`${marker}.txt`);

    await app.setSearchOption('hidden', true);
    await app.searchAndWaitForResults(marker);

    await app.expectClipVisible(`${marker}.txt`);
    const card = app.page.locator(`#gallery > li[data-filename="${marker}.txt"]`);
    await expect(card).toHaveAttribute('data-hidden', 'true');

    // Turning it back off puts the clip out of sight again.
    await app.setSearchOption('hidden', false);
    await app.searchAndWaitForResults(marker);
    await app.expectClipNotVisible(`${marker}.txt`);
  });

  test('the options popover opens from the search box and toggles content search', async ({ app }) => {
    const needle = `popover${uniqueId()}`;
    const withNeedle = await createTempFile(`hidden away: ${needle}`, 'txt');
    await app.uploadFile(withNeedle);

    await app.page.locator(selectors.header.searchOptionsButton).click();
    await expect(app.page.locator(selectors.header.searchOptionsPopover)).toBeVisible();

    // With an empty box there is nothing to re-run, so ticking the option is
    // silent; the search that follows is what exercises it.
    await app.page.locator(selectors.header.searchOptionContent).check();

    await app.searchAndWaitForResults(needle);
    await app.expectClipVisible(path.basename(withNeedle));
  });

  test('only one header menu is open at a time', async ({ app }) => {
    const optionsBtn = app.page.locator(selectors.header.searchOptionsButton);
    const optionsPopover = app.page.locator(selectors.header.searchOptionsPopover);
    const sortPopover = app.page.locator(selectors.sort.popover);
    const tagDropdown = app.page.locator(selectors.tags.filterDropdown);

    // Opening sort or the tag filter closes the search options...
    await optionsBtn.click();
    await expect(optionsPopover).toBeVisible();
    await app.page.locator(selectors.sort.button).click();
    await expect(sortPopover).toBeVisible();
    await expect(optionsPopover).toHaveCount(0);

    await optionsBtn.click();
    await expect(optionsPopover).toBeVisible();
    await app.page.locator(selectors.tags.filterButton).click();
    await expect(tagDropdown).toBeVisible();
    await expect(optionsPopover).toHaveCount(0);

    // ...and opening the search options closes them.
    await optionsBtn.click();
    await expect(optionsPopover).toBeVisible();
    await expect(tagDropdown).toBeHidden();
    await expect(sortPopover).toHaveCount(0);
  });

  test('the options are unavailable in folder view', async ({ app }) => {
    await app.toggleFolderMode();

    await app.page.locator(selectors.header.searchOptionsButton).click();
    await expect(app.page.locator(selectors.header.searchOptionsFolderNote)).toBeVisible();
    await expect(app.page.locator(selectors.header.searchOptionContent)).toBeDisabled();
    await expect(app.page.locator(selectors.header.searchOptionHidden)).toBeDisabled();
  });
});

test.describe.serial('Search option isolation', () => {
  test('enables both options in module state', async ({ app }) => {
    await app.setSearchOption('content', true);
    await app.setSearchOption('hidden', true);
    const options = await app.page.evaluate(() => (window as any).__testHelpers.getSearchOptions());
    expect(options).toEqual({ inContent: true, includeHidden: true });
  });

  test('fast reset clears both options from module state', async ({ app }) => {
    const options = await app.page.evaluate(() => (window as any).__testHelpers.getSearchOptions());
    expect(options).toEqual({ inContent: false, includeHidden: false });
  });
});
