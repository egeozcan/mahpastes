import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

async function uploadImages(app: any, count: number): Promise<string[]> {
  const files = await Promise.all(
    Array.from({ length: count }, (_, i) =>
      createTempFile(generateTestImage(50, 50, [10 * i, 255 - 10 * i, 128]), 'png')
    )
  );
  await app.uploadFiles(files);
  await app.expectClipCount(count);
  return files.map((f) => path.basename(f));
}

test.describe('Bulk Context Menu', () => {
  test('should open bulk actions when right-clicking a selected clip in a multi-selection', async ({ app }) => {
    const names = await uploadImages(app, 3);
    await app.selectClips(names);

    await app.openBulkContextMenu(names[0]);

    const menu = app.page.locator(selectors.bulk.contextMenu);
    await expect(menu).toBeVisible();
    await expect(menu).toHaveAttribute('aria-label', 'Bulk actions for selected clips');

    // Labels carry the selection count so it is clear the action is bulk
    await expect(app.getBulkContextMenuItem('bulk-delete')).toContainText('Delete 3 clips');
    await expect(app.getBulkContextMenuItem('bulk-archive')).toContainText('Archive 3 clips');
    await expect(app.getBulkContextMenuItem('bulk-download')).toContainText('Download 3 clips');
    await expect(app.getBulkContextMenuItem('bulk-tag')).toBeVisible();
    await expect(app.getBulkContextMenuItem('bulk-expiry')).toBeVisible();
    await expect(app.getBulkContextMenuItem('bulk-deselect')).toBeVisible();

    // Single-clip actions must not leak into the bulk menu
    await expect(menu.locator('[data-action="open"]')).toHaveCount(0);
    await expect(menu.locator('[data-action="rename"]')).toHaveCount(0);
  });

  test('should open the single-clip menu when only one clip is selected', async ({ app }) => {
    const names = await uploadImages(app, 2);
    await app.selectClip(names[0]);

    const clip = await app.getClipByFilename(names[0]);
    await clip.click({ button: 'right' });

    await expect(app.page.locator(selectors.cardMenu.dropdown)).toBeVisible();
    await expect(app.page.locator(selectors.bulk.contextMenu)).toHaveCount(0);
    await expect(app.page.locator(selectors.cardMenu.rename)).toBeVisible();
  });

  test('should open the single-clip menu when right-clicking an unselected clip', async ({ app }) => {
    const names = await uploadImages(app, 3);
    await app.selectClips([names[0], names[1]]);

    const clip = await app.getClipByFilename(names[2]);
    await clip.click({ button: 'right' });

    await expect(app.page.locator(selectors.cardMenu.dropdown)).toBeVisible();
    await expect(app.page.locator(selectors.bulk.contextMenu)).toHaveCount(0);
  });

  test('should delete every selected clip from the bulk context menu', async ({ app }) => {
    const names = await uploadImages(app, 3);
    await app.selectClips([names[0], names[1]]);

    await app.clickBulkContextMenuItem(names[0], 'bulk-delete');
    await app.confirmDialog();

    await app.expectClipCount(1);
    await app.expectClipVisible(names[2]);
  });

  test('should archive every selected clip from the bulk context menu', async ({ app }) => {
    const names = await uploadImages(app, 3);
    await app.selectClips([names[0], names[1]]);

    await app.clickBulkContextMenuItem(names[0], 'bulk-archive');

    await app.expectClipCount(1);
    await app.expectClipVisible(names[2]);
  });

  test('should clear the selection from the bulk context menu', async ({ app }) => {
    const names = await uploadImages(app, 3);
    await app.selectClips(names);
    expect(await app.getSelectedCount()).toBe(3);

    await app.clickBulkContextMenuItem(names[0], 'bulk-deselect');

    expect(await app.getSelectedCount()).toBe(0);
    await expect(app.page.locator(selectors.bulk.contextMenu)).toHaveCount(0);
  });

  test('should open the bulk tag popover from the bulk context menu', async ({ app }) => {
    const names = await uploadImages(app, 2);
    await app.createTag('bulk-ctx-tag');
    await app.selectClips(names);

    await app.clickBulkContextMenuItem(names[0], 'bulk-tag');

    await expect(app.page.locator(selectors.tags.popover)).toBeVisible();
  });

  test('should open bulk actions with mod+Enter on a focused selected clip', async ({ app }) => {
    const names = await uploadImages(app, 2);
    await app.selectClips(names);

    const clip = await app.getClipByFilename(names[0]);
    await clip.focus();
    await expect(clip).toBeFocused({ timeout: 3000 });

    await app.page.keyboard.press('ControlOrMeta+Enter');

    await expect(app.page.locator(selectors.bulk.contextMenu)).toBeVisible();
  });

  test('should offer Compare only when exactly two images are selected', async ({ app }) => {
    const names = await uploadImages(app, 3);

    await app.selectClips([names[0], names[1]]);
    await app.openBulkContextMenu(names[0]);
    await expect(app.getBulkContextMenuItem('bulk-compare')).toBeVisible();
    await app.closeCardMenu();

    await app.selectClip(names[2]);
    await app.openBulkContextMenu(names[0]);
    await expect(app.getBulkContextMenuItem('bulk-compare')).toHaveCount(0);
  });

  test('should offer Clear Expiration only when a selected clip expires', async ({ app }) => {
    const names = await uploadImages(app, 2);

    await app.selectClips(names);
    await app.openBulkContextMenu(names[0]);
    await expect(app.getBulkContextMenuItem('bulk-cancel-expiry')).toHaveCount(0);
    await app.closeCardMenu();

    await app.deselectClip(names[0]);
    await app.deselectClip(names[1]);
    await app.setExpirationViaMenu(names[0], '15m');
    await app.expectClipHasExpirationBadge(names[0]);

    await app.selectClips(names);
    await app.openBulkContextMenu(names[0]);
    await expect(app.getBulkContextMenuItem('bulk-cancel-expiry')).toBeVisible();
  });
});
