import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

// Both tag lists were a short scroll box over every tag, with no way to
// narrow them by typing.
test.describe('Type to narrow a tag list', () => {
  test('the filter dropdown narrows as you type, keeping the parents of a match', async ({ app }) => {
    for (const name of ['work', 'work/client', 'work/client/alpha', 'home', 'travel']) await app.createTag(name);
    await app.openTagFilterDropdown();
    const row = (name: string) => app.page.locator(`#tag-filter-list [data-tag-name="${name}"]`);
    const search = app.page.locator('[data-testid="tag-filter-search"]');

    await search.fill('ALPHA');
    await expect(row('work/client/alpha')).toBeVisible();
    await expect(row('work/client')).toBeVisible();
    await expect(row('work')).toBeVisible();
    await expect(row('home')).toBeHidden();
    await expect(row('travel')).toBeHidden();

    // Down from the field lands on the first row left.
    await app.page.keyboard.press('ArrowDown');
    await expect(row('work')).toBeFocused();

    await search.fill('zzz');
    await expect(app.page.locator('#tag-filter-list')).toContainText('No tag matches.');
    await search.fill('');
    await expect(row('home')).toBeVisible();
    await expect(app.page.locator('#tag-filter-list')).not.toContainText('No tag matches.');
  });

  test('the query does not outlive the dropdown', async ({ app }) => {
    await app.createTag('home');
    await app.createTag('travel');
    await app.openTagFilterDropdown();
    await app.page.locator('[data-testid="tag-filter-search"]').fill('home');
    await app.page.keyboard.press('Escape');
    await app.openTagFilterDropdown();
    await expect(app.page.locator('[data-testid="tag-filter-search"]')).toHaveValue('');
    await expect(app.page.locator('#tag-filter-list [data-tag-name="travel"]')).toBeVisible();
  });

  test('the card popover narrows from its field, and Enter on an existing name sets that tag', async ({ app }) => {
    for (const name of ['invoices', 'receipts', 'travel']) await app.createTag(name);
    const filePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);
    const card = await app.getClipByFilename(filename);
    await card.hover();
    await card.locator(selectors.clipActions.menuTrigger).click();
    await app.page.locator('.card-menu-dropdown [data-action="tags"]').click();

    const field = app.page.locator('[data-testid="create-tag-input"]');
    const row = (name: string) => app.page.locator(`#tag-popover-list [data-tag-name="${name}"]`);
    await field.fill('rec');
    await expect(row('receipts')).toBeVisible();
    await expect(row('invoices')).toBeHidden();

    await field.fill('receipts');
    await app.page.keyboard.press('Enter');
    await expect(app.page.locator('#tag-popover [data-testid="tag-checkbox-receipts"]')).toBeChecked();
    await expect(field).toHaveValue('');
    await expect(row('invoices')).toBeVisible();
    await expect(card).toContainText('receipts');
  });
});
