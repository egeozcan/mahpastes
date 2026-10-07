import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import { createTempFile, generateTestImage, generateTestVideo } from '../../helpers/test-data';
import * as path from 'path';

// Card media failures must settle the card (error text + _mediaFailed, so the
// next reload rebuilds it rather than reusing a spinner), and a stalled video
// load must release its source before giving its scheduler slot back.

const STALL_URL = '/__card-media-stall/clip.mp4';

test.describe('Card media failure handling', () => {
  test.afterEach(async ({ app }) => {
    await app.page.unroute('**/thumb/**').catch(() => {});
    await app.page.unroute(`**${STALL_URL}`).catch(() => {});
    await app.page.evaluate(() => {
      const w = window as any;
      if (w.__cardMediaOriginalGetClipData) {
        w.go.main.App.GetClipData = w.__cardMediaOriginalGetClipData;
        delete w.__cardMediaOriginalGetClipData;
      }
      if (w.__cardMediaOriginalPrepare) {
        w.go.main.TransferService.PrepareClipForTransfer = w.__cardMediaOriginalPrepare;
        delete w.__cardMediaOriginalPrepare;
      }
      delete w.__testVideoCardSlotTimeoutMs;
    });
  });

  test('a failed full-image fallback shows an error and marks the card failed', async ({ app }) => {
    const file = await createTempFile(generateTestImage(900, 600, [10, 150, 60]), 'png');
    const filename = path.basename(file);
    await app.page.route('**/thumb/**', route => route.abort());
    await app.page.evaluate(() => {
      const w = window as any;
      w.__cardMediaOriginalGetClipData = w.go.main.App.GetClipData;
      w.go.main.App.GetClipData = async () => { throw new Error('simulated GetClipData failure'); };
    });

    await app.uploadFile(file);
    const card = app.page.locator(selectors.gallery.clipCardByName(filename));
    await expect(card.locator('.loading-spinner')).toContainText('Failed to load');
    expect(await card.evaluate((el: any) => el._mediaFailed === true)).toBe(true);
    await expect.poll(() => app.page.evaluate(() => (window as any).__cardMediaStats().active)).toBe(0);
  });

  test('a stalled video load is released and marked failed when its slot times out', async ({ app }) => {
    const videoPath = await createTempFile(generateTestVideo(), 'mp4');
    const filename = path.basename(videoPath);

    // Every request for the stalled URL hangs forever.
    await app.page.route(`**${STALL_URL}`, () => { /* never fulfilled */ });
    await app.page.evaluate((stallUrl) => {
      const w = window as any;
      w.__testVideoCardSlotTimeoutMs = 1500;
      const service = w.go.main.TransferService;
      w.__cardMediaOriginalPrepare = service.PrepareClipForTransfer;
      service.PrepareClipForTransfer = async () => ({
        transfer_url: stallUrl,
        lease_expires_at: new Date(Date.now() + 60 * 60 * 1000).toISOString(),
      });
    }, STALL_URL);

    await app.uploadFile(videoPath);
    const card = app.page.locator(selectors.gallery.clipCardByName(filename));
    await expect(card.locator('.loading-spinner')).toContainText('Failed to load', { timeout: 10000 });
    // The source was released, not left fetching after the slot was freed.
    await expect(card.locator('video')).toHaveCount(0);
    expect(await card.evaluate((el: any) => el._mediaFailed === true)).toBe(true);
    await expect.poll(() => app.page.evaluate(() => (window as any).__cardMediaStats().active)).toBe(0);
  });
});
