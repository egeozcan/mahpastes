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

  test('overwriting a video through upload captures a new frame and revokes the old one', async ({ app }) => {
    const original = generateTestVideo();
    const videoPath = await createTempFile(original, 'mp4');
    const filename = path.basename(videoPath);
    await app.uploadFile(videoPath);
    const thumb = app.page.locator(selectors.gallery.clipCardByName(filename)).locator('img.video-thumb');
    await expect(thumb).toHaveAttribute('src', /^blob:/);
    const firstSrc = await thumb.getAttribute('src');

    // New bytes, same picture: a trailing `free` box changes the content hash.
    const free = Buffer.from([0, 0, 0, 8, 0x66, 0x72, 0x65, 0x65]);
    const replaced = Buffer.concat([original, free]).toString('base64');
    const uploading = app.page.evaluate(async ({ name, data }: { name: string; data: string }) => {
      // @ts-ignore
      await upload([{ name, content_type: 'video/mp4', data }]);
    }, { name: filename, data: replaced });
    await expect(app.page.locator(selectors.conflict.dialog)).not.toHaveAttribute('inert', '', { timeout: 5000 });
    await app.page.locator(selectors.conflict.overwriteButton).click();
    await uploading;

    await expect(thumb).toHaveAttribute('src', /^blob:/);
    await expect.poll(() => thumb.getAttribute('src')).not.toBe(firstSrc);
    const oldRevoked = await app.page.evaluate(async (src) => {
      try { await fetch(src!); return false; } catch { return true; }
    }, firstSrc);
    expect(oldRevoked).toBe(true);
  });

  test('deleting a video frees its cached frame', async ({ app }) => {
    const videoPath = await createTempFile(generateTestVideo(), 'mp4');
    const filename = path.basename(videoPath);
    await app.uploadFile(videoPath);
    const thumb = app.page.locator(selectors.gallery.clipCardByName(filename)).locator('img.video-thumb');
    await expect(thumb).toHaveAttribute('src', /^blob:/);
    const src = await thumb.getAttribute('src');
    const id = Number(await app.page.locator(selectors.gallery.clipCardByName(filename)).getAttribute('data-id'));

    await app.deleteClip(filename);
    await app.expectClipCount(0);
    // @ts-ignore - ui.js global; other tests on this worker may hold entries
    expect(await app.page.evaluate((clipId) => videoFrameCache.has(clipId), id)).toBe(false);
    const revoked = await app.page.evaluate(async (s) => {
      try { await fetch(s!); return false; } catch { return true; }
    }, src);
    expect(revoked).toBe(true);
  });
});
