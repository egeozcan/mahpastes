import { test, expect } from '../../fixtures/test-fixtures';

/**
 * A refocus that finds the library unchanged must still re-render the
 * remaining-time badges of the cards that survive it: they were rendered when
 * the listing was, so a window hidden for five minutes otherwise comes back
 * reading 'Temp · 10m' instead of 'Temp · 5m'.
 */

test.describe('Refocus expiry badges', () => {
  test.afterEach(async ({ app }) => {
    await app.page.evaluate(() => {
      const w = window as any;
      if (w.__origDate) w.Date = w.__origDate;
    });
  });

  test('an unchanged-library refocus refreshes surviving expiration badges', async ({ app }) => {
    const name = 'refocus-badge.txt';
    await app.page.evaluate(async (name: string) => {
      // @ts-ignore - Wails runtime
      await window.go.main.App.UploadFiles([{ name, content_type: 'text/plain', data: btoa('badge') }], 10, 0);
      // @ts-ignore - app global
      await loadClips();
    }, name);

    const badge = app.page.locator(`#gallery > li[data-filename="${name}"] .clip-expiration-badge`);
    await expect(badge).toContainText('10m');
    // Let reloads queued by the upload settle so the refocus sees an
    // unchanged library version.
    await app.page.waitForTimeout(700);

    // Five minutes pass while the window is hidden; no library write happens.
    await app.page.evaluate(() => {
      const w = window as any;
      const Orig = w.__origDate || w.Date;
      w.__origDate = Orig;
      const skew = 5 * 60 * 1000;
      class Skewed extends Orig {
        constructor(...args: any[]) {
          if (args.length === 0) super(Orig.now() + skew);
          else super(...(args as []));
        }
        static now() { return Orig.now() + skew; }
      }
      w.Date = Skewed;
    });

    const outcome = await app.page.evaluate(() => (window as any).refreshGalleryOnRefocus());
    expect(outcome).toBe('none');
    await expect(badge).toContainText('5m');
    await expect(badge).not.toContainText('10m');
  });
});
