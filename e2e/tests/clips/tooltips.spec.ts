import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
import { generateTestImage, createTempFile } from '../../helpers/test-data.js';
import * as path from 'path';

test.describe('Tooltips', () => {
  test('header buttons have data-tooltip attributes', async ({ app }) => {
    const tagFilterBtn = app.page.locator('#tag-filter-btn');
    await expect(tagFilterBtn).toHaveAttribute('data-tooltip', /filter/i);

    const archiveBtn = app.page.locator('#header-archive-btn');
    await expect(archiveBtn).toHaveAttribute('data-tooltip', /archive/i);

    const sortBtn = app.page.locator('#sort-btn');
    await expect(sortBtn).toHaveAttribute('data-tooltip', /sort/i);

    const menuBtn = app.page.locator('#drawer-toggle-btn');
    await expect(menuBtn).toHaveAttribute('data-tooltip', /menu/i);
  });

  test('tooltip CSS renders on hover via pseudo-element', async ({ app }) => {
    const btn = app.page.locator('#tag-filter-btn');
    await btn.hover();
    // CSS transition-delay: 300ms + transition: opacity 150ms = 450ms total
    await app.page.waitForTimeout(500);

    const opacity = await btn.evaluate((el) => {
      return window.getComputedStyle(el, '::after').opacity;
    });
    expect(opacity).toBe('1');
  });

  test('tooltip disappears when mouse leaves', async ({ app }) => {
    const btn = app.page.locator('#tag-filter-btn');
    await btn.hover();
    await app.page.waitForTimeout(400);

    // Move mouse away
    await app.page.mouse.move(0, 0);
    await app.page.waitForTimeout(200);

    const opacity = await btn.evaluate((el) => {
      return window.getComputedStyle(el, '::after').opacity;
    });
    expect(opacity).toBe('0');
  });

  test('card menu items have tooltips', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    // Check top-level menu item tooltip (delete is still top-level)
    await app.openCardMenu(filename);
    const deleteItem = app.page.locator('.card-menu-item[data-action="delete"]');
    await expect(deleteItem).toHaveAttribute('title', /permanently delete/i);

    // Hover copy submenu to check submenu item tooltip
    const copyTrigger = app.page.locator(selectors.cardMenu.copyTrigger);
    await copyTrigger.hover();
    await app.page.locator(selectors.cardMenu.submenu).waitFor({ state: 'visible', timeout: 3000 });

    const copyPathItem = app.page.locator('.card-menu-item[data-action="copy-path"]');
    await expect(copyPathItem).toHaveAttribute('title');
  });

  test('tooltips can be disabled via settings toggle', async ({ app }) => {
    // Open settings
    await app.page.locator('#drawer-toggle-btn').click();
    await app.page.locator('#open-settings-btn').click();

    const checkbox = app.page.locator('[data-testid="tooltips-toggle"]');
    await expect(checkbox).toBeChecked();

    // Disable tooltips by clicking the label (checkbox is sr-only with a visual overlay)
    const label = app.page.locator('[data-testid="tooltips-toggle-label"]');
    await label.click();
    await expect(checkbox).not.toBeChecked();

    // Close settings
    await app.page.locator('#settings-close').click();

    // Verify body has disabled class
    await expect(app.page.locator('body')).toHaveClass(/tooltips-disabled/);

    // Hover over a button — tooltip should not render
    const btn = app.page.locator('#tag-filter-btn');
    await btn.hover();
    await app.page.waitForTimeout(400);

    const display = await btn.evaluate((el) => {
      return window.getComputedStyle(el, '::after').display;
    });
    expect(display).toBe('none');
  });

  test('tooltip setting persists across page reload', async ({ app }) => {
    // Open settings and disable tooltips
    await app.page.locator('#drawer-toggle-btn').click();
    await app.page.locator('#open-settings-btn').click();
    const label = app.page.locator('[data-testid="tooltips-toggle-label"]');
    await label.click();
    await app.page.locator('#settings-close').click();
    await app.page.waitForTimeout(200);

    // Reload
    await app.page.reload();
    await app.waitForReady();

    // Verify body still has disabled class
    await expect(app.page.locator('body')).toHaveClass(/tooltips-disabled/);

    // Re-enable for cleanup
    await app.page.locator('#drawer-toggle-btn').click();
    await app.page.locator('#open-settings-btn').click();
    const label2 = app.page.locator('[data-testid="tooltips-toggle-label"]');
    await label2.click();
    await app.page.locator('#settings-close').click();
  });

  test('bottom bar elements have tooltips', async ({ app }) => {
    const addBtn = app.page.locator('#add-btn');
    await expect(addBtn).toHaveAttribute('data-tooltip', /upload/i);

    const expirySelect = app.page.locator('#expiry-select');
    await expect(expirySelect).toHaveAttribute('data-tooltip', /auto-delete/i);
  });

  test('bulk toolbar buttons have tooltips when visible', async ({ app }) => {
    const img1 = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
    const img2 = await createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png');
    await app.uploadFile(img1);
    await app.uploadFile(img2);
    await app.expectClipCount(2);

    // Select both clips
    const checkboxes = app.page.locator('.clip-checkbox');
    await checkboxes.first().click();
    await checkboxes.nth(1).click();

    const deleteBtn = app.page.locator('#bulk-delete-btn');
    await expect(deleteBtn).toHaveAttribute('data-tooltip', /permanently delete/i);

    const copyBtn = app.page.locator('#bulk-copy-btn');
    await expect(copyBtn).toHaveAttribute('data-tooltip', /clipboard/i);
  });
});
