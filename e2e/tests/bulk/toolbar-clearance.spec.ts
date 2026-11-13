import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

// The floating bulk toolbar covered the last row of cards (and "Load more")
// at the end of the page, with no way to scroll them clear.
test.describe('Bulk toolbar clearance', () => {
  test('the last card can be scrolled clear of the toolbar, and the room is given back', async ({ app }) => {
    await app.page.evaluate(async () => {
      const files = [];
      for (let i = 0; i < 40; i++) {
        files.push({ name: `row-${String(i).padStart(2, '0')}.txt`, content_type: 'text/plain', data: btoa(`clip ${i}`) });
      }
      // @ts-ignore - Wails runtime
      await window.go.main.App.UploadFiles(files, 0, 0);
      // @ts-ignore
      await loadClips();
    });
    await app.expectClipCount(40);

    const main = app.page.locator('main');
    expect(await main.evaluate((el: HTMLElement) => el.style.paddingBottom)).toBe('');

    await app.page.locator(`${selectors.gallery.clipCard} .clip-checkbox`).first().check({ force: true });
    await expect(app.page.locator(selectors.bulk.toolbar)).toHaveClass(/pointer-events-auto/);
    await expect.poll(() => main.evaluate((el: HTMLElement) => parseFloat(el.style.paddingBottom) || 0)).toBeGreaterThan(80);

    // At the very end of the page the last card sits above the toolbar.
    await app.page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
    const clear = await app.page.evaluate(() => {
      const cards = document.querySelectorAll('#gallery > li[data-id]');
      const last = cards[cards.length - 1].getBoundingClientRect();
      const toolbar = document.getElementById('bulk-toolbar')!.getBoundingClientRect();
      return last.bottom <= toolbar.top;
    });
    expect(clear).toBe(true);

    await app.page.keyboard.press('Escape');
    await expect(app.page.locator(selectors.bulk.toolbar)).not.toHaveClass(/pointer-events-auto/);
    expect(await main.evaluate((el: HTMLElement) => el.style.paddingBottom)).toBe('');
  });
});

// Comparison existed only inside the "more" menu, once exactly two images
// were already selected.
test.describe('Bulk toolbar: Compare', () => {
  test('Compare shows with one image selected, says what it needs, and opens with two', async ({ app }) => {
    const { createTempFile, generateTestImage, generateTestText } = await import('../../helpers/test-data');
    const pathMod = await import('path');
    const a = await createTempFile(generateTestImage(40, 40, [255, 0, 0]), 'png');
    const b = await createTempFile(generateTestImage(40, 40, [0, 255, 0]), 'png');
    const t = await createTempFile(generateTestText('not-an-image'), 'txt');
    for (const f of [a, b, t]) await app.uploadFile(f);
    const compare = app.page.locator('[data-testid="bulk-compare-btn"]');

    // No image selected: no button.
    await app.selectClips([pathMod.basename(t)]);
    await expect(compare).toBeHidden();

    await app.selectClips([pathMod.basename(a)]);
    await expect(compare).toBeVisible();
    await expect(compare).toHaveAttribute('aria-disabled', 'true');
    await expect(compare).toHaveAttribute('data-tooltip', 'Select exactly two images to compare');
    // Playwright treats aria-disabled as not clickable; a person can click it.
    await compare.click({ force: true });
    await app.expectToast('Select exactly two images to compare.');
    await expect(app.page.locator(selectors.comparison.modal)).not.toHaveClass(/active/);

    // The text clip is still selected: a text clip and two images is not a pair.
    await app.selectClips([pathMod.basename(b)]);
    await expect(compare).toHaveAttribute('aria-disabled', 'true');
    await app.deselectClip(pathMod.basename(t));
    await expect(compare).toHaveAttribute('aria-disabled', 'false');
    await compare.click();
    await expect(app.page.locator(selectors.comparison.modal)).toHaveClass(/active/);
  });
});
