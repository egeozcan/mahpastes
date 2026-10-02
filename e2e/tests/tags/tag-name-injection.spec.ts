import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import type { Page } from '@playwright/test';

/**
 * Tag names are not trusted input: a server-mode editor key can create any
 * tag, a plugin can, and a followed P2P share creates the tag the publisher
 * named. The tag views build markup with innerHTML, so a name that reaches an
 * attribute unescaped — a data-testid, an aria-label — closes it and injects
 * elements into the webview, which exposes every bound Go method.
 */
test.describe('Tag name injection', () => {
  const hostile = 'x" data-pwned="1"><img id=pwn-tag src=x>';

  async function expectInert(page: Page): Promise<void> {
    expect(await page.locator('#pwn-tag').count()).toBe(0);
    expect(await page.locator('[data-pwned]').count()).toBe(0);
  }

  async function attributeValues(page: Page, selector: string, attr: string): Promise<string[]> {
    return page.locator(selector).evaluateAll(
      (els, a) => els.map((el) => el.getAttribute(a as string) || ''),
      attr,
    );
  }

  test('a hostile tag name renders as text in every tag view', async ({ app }) => {
    const { tagID } = await app.createTag(hostile);
    expect(tagID).toBeGreaterThan(0);

    // Filter dropdown: the name lands in each checkbox's data-testid.
    await app.openTagFilterDropdown();
    await expectInert(app.page);
    expect(await attributeValues(app.page, `${selectors.tags.filterList} input[type="checkbox"]`, 'data-testid'))
      .toContain(`tag-checkbox-${hostile}`);
    await app.closeTagFilterDropdown();

    // Active filter pill: the name lands in the remove button's aria-label.
    await app.filterByTag(hostile);
    await expectInert(app.page);
    expect(await attributeValues(app.page, `${selectors.tags.activeTagsContainer} button`, 'aria-label'))
      .toContain(`Remove ${hostile} filter`);

    // Tag popover list (the per-clip tagging menu).
    await app.page.evaluate(async () => {
      // @ts-ignore - global from tags.js
      await renderTagPopoverList(null);
    });
    await expectInert(app.page);
    expect(await attributeValues(app.page, `${selectors.tags.popoverList} input[type="checkbox"]`, 'data-testid'))
      .toContain(`tag-checkbox-${hostile}`);

    // Settings → hidden tags list.
    await app.page.evaluate(() => {
      // @ts-ignore - global from settings.js
      renderHiddenTagsSettings();
    });
    await expectInert(app.page);

    // Folder mode breadcrumb: the name lands in the segment pill's aria-label.
    await app.enterFolderMode();
    await app.page.evaluate((id) => {
      // @ts-ignore - global from ui.js
      navigateToFolder(id);
    }, tagID);
    await expect(app.page.locator(`${selectors.tags.activeTagsContainer} [data-drop-target="${tagID}"]`)).toBeVisible();
    await expectInert(app.page);
    expect(await attributeValues(app.page, `${selectors.tags.activeTagsContainer} button`, 'aria-label'))
      .toContain(`Remove ${hostile} filter`);
  });

  test('tag colors that are not a hex color or keyword never reach markup', async ({ app }) => {
    const results = await app.page.evaluate(() => {
      const inputs = [
        '#3B82F6',
        'red',
        'red" onmouseover="alert(1)',
        'red; background-image: url(https://example.com/x)',
        '',
        null,
      ];
      // @ts-ignore - global from utils.js
      return inputs.map((c) => safeTagColor(c));
    });
    expect(results).toEqual(['#3B82F6', 'red', '#78716C', '#78716C', '#78716C', '#78716C']);
  });

  test('the backend refuses a color that is not a hex color or keyword', async ({ app }) => {
    const { tagID } = await app.createTag('colorful');
    const error = await app.page.evaluate(async (id) => {
      try {
        // @ts-ignore - Wails runtime
        await window.go.main.App.UpdateTag(id, 'colorful', 'red" onmouseover="alert(1)');
        return '';
      } catch (e: any) {
        return String(e?.message || e);
      }
    }, tagID);
    expect(error).toContain('invalid tag color');

    const tags = await app.getAllTags();
    const tag = tags.find((t) => t.id === tagID);
    expect(tag?.color).toMatch(/^#[0-9a-fA-F]{6}$/);
  });
});
