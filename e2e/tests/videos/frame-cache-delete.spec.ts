import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestVideo } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

/**
 * A video card's frame is encoded asynchronously (canvas.toBlob). Deleting the
 * clip while that encode is still pending must invalidate it: when the
 * callback finally runs it may not install a cache entry (and object URL) for
 * a clip that no longer exists.
 */
test.describe('Video frame cache: delete during capture', () => {
  test.afterEach(async ({ app }) => {
    await app.page.evaluate(() => {
      const w = window as any;
      if (w.__originalToBlob) {
        HTMLCanvasElement.prototype.toBlob = w.__originalToBlob;
        delete w.__originalToBlob;
      }
      (w.__heldToBlob || []).forEach((release: () => void) => release());
      delete w.__heldToBlob;
    });
  });

  test('a capture still encoding when its clip is deleted publishes no entry', async ({ app }) => {
    const before = await app.page.evaluate(() => (window as any).__videoFrameCacheStats().entries);

    // Hold every toBlob callback until the test releases it.
    await app.page.evaluate(() => {
      const w = window as any;
      w.__originalToBlob = HTMLCanvasElement.prototype.toBlob;
      w.__heldToBlob = [];
      HTMLCanvasElement.prototype.toBlob = function (this: HTMLCanvasElement, cb: BlobCallback, ...rest: any[]) {
        w.__heldToBlob.push(() => w.__originalToBlob.call(this, cb, ...rest));
      } as any;
    });

    const videoPath = await createTempFile(generateTestVideo(), 'mp4');
    const filename = path.basename(videoPath);
    await app.uploadFile(videoPath);
    await expect(app.page.locator(selectors.gallery.clipCardByName(filename))).toHaveCount(1);

    // The frame was drawn and its encode is pending.
    await expect.poll(() => app.page.evaluate(() => (window as any).__heldToBlob.length)).toBeGreaterThan(0);

    await app.deleteClip(filename);
    await expect(app.page.locator(selectors.gallery.clipCardByName(filename))).toHaveCount(0);

    // Let the encode finish after the delete.
    await app.page.evaluate(() => {
      const w = window as any;
      const held = w.__heldToBlob;
      w.__heldToBlob = [];
      held.forEach((release: () => void) => release());
    });
    // toBlob is asynchronous; give its callback time to run.
    await app.page.waitForTimeout(500);

    const after = await app.page.evaluate(() => (window as any).__videoFrameCacheStats().entries);
    expect(after).toBe(before);
  });
});
