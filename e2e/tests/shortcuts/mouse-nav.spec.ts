import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import path from 'path';

/**
 * The mouse "back" (button 3) and "forward" (button 4) buttons mirror browser
 * history navigation: back closes the topmost overlay or walks up the folder
 * tree, forward retraces folder steps. Playwright's mouse API can't press the
 * physical side buttons, so we dispatch the same mouseup events the webview
 * delivers when they're clicked.
 */
async function mouseBack(page: import('@playwright/test').Page): Promise<void> {
  await page.evaluate(() => {
    document.dispatchEvent(new MouseEvent('mouseup', { button: 3, bubbles: true, cancelable: true }));
  });
}

async function mouseForward(page: import('@playwright/test').Page): Promise<void> {
  await page.evaluate(() => {
    document.dispatchEvent(new MouseEvent('mouseup', { button: 4, bubbles: true, cancelable: true }));
  });
}

test.describe('Mouse back/forward navigation', () => {
  test.afterEach(async ({ app }) => {
    const btn = app.page.locator(selectors.tags.folderModeButton);
    const pressed = await btn.getAttribute('aria-pressed');
    if (pressed === 'true') {
      await btn.click();
    }
    await app.clearTagFilters();
    await app.deleteAllTags();
  });

  test('back button closes the lightbox', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openLightbox(filename);
    expect(await app.isLightboxOpen()).toBe(true);

    await mouseBack(app.page);
    await app.page.waitForSelector(`${selectors.lightbox.overlay}:not(.active)`);
    expect(await app.isLightboxOpen()).toBe(false);
  });

  test('back button walks up the folder tree', async ({ app }) => {
    await app.createTag('work/client1');
    await app.toggleFolderMode();
    await app.clickFolder('work');
    await app.clickFolder('client1');

    // Inside work/client1 — both breadcrumb pills present.
    await expect(app.page.locator(selectors.tags.tagPill('work/client1'))).toBeVisible();

    // Back → up to work (client1 folder card visible again, no deeper pill).
    await mouseBack(app.page);
    await app.expectFolderVisible('client1');
    await expect(app.page.locator(selectors.tags.tagPill('work/client1'))).not.toBeVisible();
    await expect(app.page.locator(selectors.tags.tagPill('work'))).toBeVisible();

    // Back → up to the folder-mode root (top-level "work" folder card).
    await mouseBack(app.page);
    await app.expectFolderVisible('work');
    await expect(app.page.locator(selectors.tags.tagPill('work'))).not.toBeVisible();
  });

  test('forward button retraces folder steps after going back', async ({ app }) => {
    await app.createTag('work/client1');
    await app.toggleFolderMode();
    await app.clickFolder('work');
    await app.clickFolder('client1');

    await mouseBack(app.page); // → work
    await mouseBack(app.page); // → root

    await app.expectFolderVisible('work');

    await mouseForward(app.page); // → work
    await app.expectFolderVisible('client1');
    await expect(app.page.locator(selectors.tags.tagPill('work'))).toBeVisible();

    await mouseForward(app.page); // → work/client1
    await expect(app.page.locator(selectors.tags.tagPill('work/client1'))).toBeVisible();
  });

  test('entering a new folder clears the forward history', async ({ app }) => {
    await app.createTag('work/client1');
    await app.createTag('personal');
    await app.toggleFolderMode();
    await app.clickFolder('work');
    await app.clickFolder('client1');

    await mouseBack(app.page); // → work
    await mouseBack(app.page); // → root
    await app.expectFolderVisible('work');

    // A fresh navigation invalidates the forward stack.
    await app.clickFolder('personal');
    await expect(app.page.locator(selectors.tags.tagPill('personal'))).toBeVisible();

    // Forward should now be a no-op (still showing personal).
    await mouseForward(app.page);
    await expect(app.page.locator(selectors.tags.tagPill('personal'))).toBeVisible();
  });

  test('back button at folder-mode root does nothing', async ({ app }) => {
    await app.createTag('work');
    await app.toggleFolderMode();
    await app.expectFolderVisible('work');

    await mouseBack(app.page);
    // Still at the root folder view.
    await app.expectFolderVisible('work');
    await expect(app.page.locator(selectors.tags.tagPill('work'))).not.toBeVisible();
  });
});
