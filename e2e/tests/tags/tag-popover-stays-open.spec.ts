import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

// The card's tag popover closed after every tick, so three tags meant three
// trips through the card menu.
test.describe('Tag popover stays open while tagging one clip', () => {
  const box = (app: any, name: string) => app.page.locator(`#tag-popover [data-testid="tag-checkbox-${name}"]`);

  async function openPopover(app: any, filename: string) {
    const card = await app.getClipByFilename(filename);
    await card.hover();
    await card.locator(selectors.clipActions.menuTrigger).click();
    await app.page.locator('.card-menu-dropdown [data-action="tags"]').click();
    await expect(app.page.locator(selectors.tags.popover)).toBeVisible();
  }

  test('several tags are set in one visit, and the popover shows what the clip now has', async ({ app }) => {
    for (const name of ['red', 'work', 'work/urgent']) await app.createTag(name);
    const filePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);
    await openPopover(app, filename);

    await box(app, 'red').check();
    await expect(app.page.locator(selectors.tags.popover)).toBeVisible();
    await expect(box(app, 'red')).toBeChecked();
    await expect(box(app, 'red')).toBeFocused();

    await box(app, 'work').check();
    await expect(box(app, 'work')).toBeChecked();
    await expect(box(app, 'red')).toBeChecked();

    // Same tree: the deeper tag replaces its parent, and the list says so.
    await box(app, 'work/urgent').check();
    await expect(box(app, 'work/urgent')).toBeChecked();
    await expect(box(app, 'work')).not.toBeChecked();
    await expect(app.page.locator(selectors.tags.popover)).toBeVisible();

    // Untick, still open.
    await box(app, 'red').uncheck();
    await expect(box(app, 'red')).not.toBeChecked();
    await expect(app.page.locator(selectors.tags.popover)).toBeVisible();

    await app.page.keyboard.press('Escape');
    await expect(app.page.locator(selectors.tags.popover)).toBeHidden();
    const card = await app.getClipByFilename(filename);
    await expect(card).toContainText('work/urgent');
    await expect(card).not.toContainText('red');
  });

  test('a tag created in the popover is set and the popover stays for more', async ({ app }) => {
    const filePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);
    await openPopover(app, filename);

    await app.page.locator('#create-tag-input, #tag-popover input[type="text"]').first().fill('fresh');
    await app.page.keyboard.press('Enter');
    await expect(box(app, 'fresh')).toBeChecked();
    await expect(app.page.locator(selectors.tags.popover)).toBeVisible();
  });

  test('the popover closes when the tick takes the clip out of the view', async ({ app }) => {
    await app.createTag('a');
    // A second clip keeps the tag alive (a tag with no clips left is removed,
    // which would lift the filter instead).
    const other = await createTempFile(generateTestImage(30, 30, [0, 0, 255]), 'png');
    await app.uploadFile(other);
    await app.addTagToClip(path.basename(other), 'a');
    const filePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);
    await app.addTagToClip(filename, 'a');
    await app.filterByTag('a');
    await app.expectClipCount(2);
    await openPopover(app, filename);

    await box(app, 'a').uncheck();
    await expect(app.page.locator(selectors.tags.popover)).toBeHidden();
    await app.expectClipCount(1);
  });
});
