import { test } from '../../fixtures/test-fixtures';
import { expect } from '@playwright/test';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

test.describe('Merge tags — filter and modal state', () => {
  test.afterEach(async ({ app }) => {
    await app.clearTagFilters();
    await app.deleteAllTags();
  });

  test('merging the filtered tag leaves one live pill whose X clears the filter', async ({ app }) => {
    const img = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(img);
    await app.createTag('src');
    await app.createTag('dst');
    await app.tagClipByIndex(0, 'src');
    await app.filterByTag('src');
    await app.expectClipCount(1);

    await app.mergeTag('src', 'dst');

    const pills = app.page.locator('#active-tags-container button[aria-label$=" filter"]');
    await expect(pills).toHaveCount(1);
    await expect(pills.first()).toHaveAttribute('aria-label', 'Remove dst filter');
    await pills.first().click();

    await expect(pills).toHaveCount(0);
    await app.expectClipCount(1);
  });

  test('editing the destination disarms confirm until the new value is previewed', async ({ app }) => {
    await app.createTag('from');
    await app.createTag('into');
    await app.openMergeModal('from');
    await app.enterMergeDestination('into');
    const confirm = app.page.locator('#merge-tag-confirm');
    await expect(confirm).toBeEnabled();

    // Type a value that names no tag: the button is disarmed synchronously,
    // before the debounced preview has run.
    const disabledRightAway = await app.page.evaluate(() => {
      const input = document.getElementById('merge-tag-dest-input') as HTMLInputElement;
      input.value = 'int';
      input.dispatchEvent(new Event('input', { bubbles: true }));
      const btn = document.getElementById('merge-tag-confirm') as HTMLButtonElement;
      return btn.disabled && !btn.dataset.destId;
    });
    expect(disabledRightAway).toBe(true);
    // Once the debounced preview has answered for the new value, it must not re-arm.
    await expect(app.page.locator('#merge-tag-preview')).toContainText('"int" does not exist');
    await expect(confirm).toBeDisabled();
    await app.page.click('#merge-tag-cancel');
  });
});
