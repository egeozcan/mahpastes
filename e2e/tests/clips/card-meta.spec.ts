import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile } from '../../helpers/test-data';
import * as path from 'path';

// Cards showed neither size nor date, though the gallery sorts by both.
test.describe('Card size and age', () => {
  test('the footer shows the size and how long ago the clip was added', async ({ app }) => {
    const filePath = await createTempFile('x'.repeat(2048), 'txt');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);

    const card = await app.getClipByFilename(filename);
    const meta = card.locator('[data-testid="clip-meta"]');
    await expect(meta).toHaveText('2 KB · now');
    await expect(meta).toHaveAttribute('title', /2,048 bytes, added /);
    // The filename's own title is untouched: the Metadata action reads it.
    await expect(card.locator('.clip-filename')).toHaveAttribute('title', filename);
  });

  test('the age is rewritten on a card that is kept, and on refocus', async ({ app }) => {
    const filePath = await createTempFile('aged', 'txt');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);
    const card = await app.getClipByFilename(filename);
    const meta = card.locator('[data-testid="clip-meta"]');

    // Pretend three hours have passed.
    await app.page.evaluate(() => {
      const real = Date.now;
      (window as any).__realNow = real;
      Date.now = () => real() + 3 * 3600 * 1000;
    });
    try {
      await app.page.evaluate(() => (window as any).refreshExpiryBadges());
      await expect(meta).toHaveText(/ · 3h$/);

      // A reload keeps the card (same signature) and still refreshes its age.
      await app.page.evaluate(() => { Date.now = () => (window as any).__realNow() + 2 * 86400 * 1000; });
      await app.page.evaluate(() => (window as any).loadClips());
      await expect(meta).toHaveText(/ · 2d$/);
    } finally {
      await app.page.evaluate(() => { Date.now = (window as any).__realNow; });
    }
  });
});
