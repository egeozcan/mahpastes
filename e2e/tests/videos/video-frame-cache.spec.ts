import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestVideo } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

/**
 * A captured video-card frame is cached per clip revision, so a card rebuilt
 * for an unchanged clip shows it without leasing the file, fetching, seeking
 * and re-encoding. It is still shown through img.video-thumb.
 */
test.describe('Video card frame cache', () => {
  test.afterEach(async ({ app }) => {
    await app.page.evaluate(() => {
      const w = window as any;
      if (w.__originalPrepareClipForTransfer) {
        // @ts-ignore - Wails runtime
        window.go.main.TransferService.PrepareClipForTransfer = w.__originalPrepareClipForTransfer;
        delete w.__originalPrepareClipForTransfer;
      }
    });
  });

  test('a rebuilt card reuses the captured frame', async ({ app }) => {
    const videoPath = await createTempFile(generateTestVideo(), 'mp4');
    const filename = path.basename(videoPath);
    await app.uploadFile(videoPath);

    const card = app.page.locator(selectors.gallery.clipCardByName(filename));
    const thumb = card.locator('img.video-thumb');
    await expect(thumb).toHaveAttribute('src', /^blob:/);
    const firstSrc = await thumb.getAttribute('src');

    await app.page.evaluate(() => {
      const w = window as any;
      // @ts-ignore - Wails runtime
      const service = window.go.main.TransferService;
      w.__originalPrepareClipForTransfer = service.PrepareClipForTransfer;
      w.__prepareCalls = 0;
      service.PrepareClipForTransfer = async (...args: any[]) => {
        w.__prepareCalls += 1;
        return w.__originalPrepareClipForTransfer(...args);
      };
    });

    // The archive view drops the card; coming back builds a new one.
    await app.page.evaluate(() => {
      document.querySelectorAll('#gallery > li[data-id]').forEach((li: any) => { li.__frameMark = true; });
    });
    await app.toggleArchiveView();
    await expect(card).toHaveCount(0);
    await app.toggleArchiveView();

    await expect(thumb).toBeVisible();
    expect(await card.evaluate((el: any) => !!el.__frameMark)).toBe(false);
    await expect(thumb).toHaveAttribute('src', firstSrc!);
    await expect(thumb).toHaveAttribute('data-frame-cached', 'true');
    await expect(card.locator('video')).toHaveCount(0);
    await expect(card.locator('.video-play-badge')).toBeVisible();
    expect(await app.page.evaluate(() => (window as any).__prepareCalls)).toBe(0);
  });

  test('replacing the clip bytes drops its cached frame', async ({ app }) => {
    const videoPath = await createTempFile(generateTestVideo(), 'mp4');
    const filename = path.basename(videoPath);
    await app.uploadFile(videoPath);
    const thumb = app.page.locator(selectors.gallery.clipCardByName(filename)).locator('img.video-thumb');
    await expect(thumb).toHaveAttribute('src', /^blob:/);
    const firstSrc = await thumb.getAttribute('src');

    const revoked = await app.page.evaluate(async (src) => {
      const card = document.querySelector('#gallery > li[data-id]') as HTMLElement;
      // @ts-ignore
      invalidateClipMedia(Number(card.dataset.id));
      try {
        await fetch(src!);
        return false;
      } catch {
        return true;
      }
    }, firstSrc);
    expect(revoked).toBe(true);
    expect(await app.page.evaluate(() => (window as any).__videoFrameCacheStats().entries)).toBe(0);
  });
});
