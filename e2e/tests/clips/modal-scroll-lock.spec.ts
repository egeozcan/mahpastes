import { test, expect } from '../../fixtures/test-fixtures.js';

test.describe('Modal scroll lock', () => {
  test('locks scrolling without changing the background layout', async ({ app, page }) => {
    await page.evaluate(() => {
      document.body.style.minHeight = '300vh';
      window.scrollTo(0, 100);
    });

    const galleryColumns = () => page.evaluate(
      () => getComputedStyle(document.getElementById('gallery')!).gridTemplateColumns,
    );
    const rootOverflow = () => page.evaluate(
      () => getComputedStyle(document.documentElement).overflowY,
    );
    const columnsBeforeOpen = await galleryColumns();
    const overflowBeforeOpen = await rootOverflow();

    await app.openSettingsModal();

    // Keep the real scrollbar rather than replacing it with a differently
    // painted reserved gutter while the document is locked.
    await expect.poll(rootOverflow).toBe(overflowBeforeOpen);

    const lockedScrollY = await page.evaluate(() => window.scrollY);
    await page.mouse.move(10, 10);
    await page.mouse.wheel(0, 500);
    await page.waitForTimeout(100);
    expect(await page.evaluate(() => window.scrollY)).toBe(lockedScrollY);
    expect(await galleryColumns()).toBe(columnsBeforeOpen);

    const settingsScroller = page.locator('#settings-modal .overflow-y-auto').first();
    await settingsScroller.hover();
    await page.mouse.wheel(0, 300);
    await expect.poll(() => settingsScroller.evaluate((element) => element.scrollTop))
      .toBeGreaterThan(0);
    expect(await page.evaluate(() => window.scrollY)).toBe(lockedScrollY);

    await page.evaluate(() => {
      // @ts-ignore - classic scripts expose the shared dialog helpers globally.
      showConfirmDialog('Nested dialog', 'Keep the underlying modal open.', () => {});
    });
    await page.locator('#confirm-dialog.opacity-100').waitFor();
    await page.locator('#confirm-no-btn').click();

    // Closing a stacked dialog must not release the settings modal's lock.
    const stackedScrollY = await page.evaluate(() => window.scrollY);
    await page.evaluate(() => window.scrollBy(0, 500));
    await page.waitForTimeout(100);
    expect(await page.evaluate(() => window.scrollY)).toBe(stackedScrollY);

    await app.closeSettingsModal();
    await page.waitForFunction(
      () => document.querySelector('[aria-modal="true"]:not([inert])') === null,
    );

    await page.evaluate(() => window.scrollBy(0, 500));
    await page.waitForTimeout(100);
    expect(await page.evaluate(() => window.scrollY)).toBeGreaterThan(stackedScrollY);

    await page.evaluate(() => {
      document.body.style.minHeight = '';
      window.scrollTo(0, 0);
    });
  });
});
