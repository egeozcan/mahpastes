import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestVideo,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Video thumbnails and lightbox', () => {
  test.afterEach(async ({ app }) => {
    await app.page.evaluate(() => {
      const testWindow = window as any;
      if (testWindow.__originalVideoGetClipData) {
        // @ts-ignore - Wails runtime
        window.go.main.App.GetClipData = testWindow.__originalVideoGetClipData;
        delete testWindow.__originalVideoGetClipData;
      }
    });
  });

  test('streams a decoded video-frame thumbnail without loading clip data', async ({ app }) => {
    const videoPath = await createTempFile(generateTestVideo(), 'mp4');
    const filename = path.basename(videoPath);

    await app.page.evaluate(() => {
      const testWindow = window as any;
      // @ts-ignore - Wails runtime
      const original = window.go.main.App.GetClipData;
      testWindow.__originalVideoGetClipData = original;
      testWindow.__videoGetClipDataCalls = 0;
      // @ts-ignore - Wails runtime
      window.go.main.App.GetClipData = async (...args: any[]) => {
        testWindow.__videoGetClipDataCalls += 1;
        return original(...args);
      };
    });

    await app.uploadFile(videoPath);

    const card = app.page.locator(selectors.gallery.clipCardByName(filename));
    await expect(card).toHaveAttribute('data-type', 'video/mp4');
    // The decoded frame is handed to an <img> and the video element is removed,
    // so a gallery of videos holds no decoders and no open range fetches.
    //
    // It deliberately is not carried by the video's own `poster`: WebKit paints
    // a video whose `src` was removed as a black box and ignores the poster,
    // while Chromium honours it. Playwright drives Chromium, so this test can
    // only prove the <img> path works — it could not have caught the poster
    // bug. That was found by snapshotting a real WKWebView.
    const thumbnail = card.locator('img.video-thumb[data-clip-id]');
    await expect(thumbnail).toBeVisible();
    await expect(thumbnail).toHaveAttribute('src', /^data:image\//);
    await expect(card.locator('video[data-clip-id]')).toHaveCount(0);
    await expect(card.locator('.video-play-badge')).toBeVisible();

    // A real frame has more than one colour; an all-flat thumbnail means the
    // read-back failed and should have fallen back to the live element.
    const colours = await thumbnail.evaluate((image: HTMLImageElement) => {
      const canvas = document.createElement('canvas');
      canvas.width = image.naturalWidth;
      canvas.height = image.naturalHeight;
      const ctx = canvas.getContext('2d')!;
      ctx.drawImage(image, 0, 0);
      const { data } = ctx.getImageData(0, 0, canvas.width, canvas.height);
      const seen = new Set<string>();
      for (let i = 0; i < data.length; i += 4) {
        seen.add(`${data[i]},${data[i + 1]},${data[i + 2]}`);
      }
      return seen.size;
    });
    expect(colours).toBeGreaterThan(1);

    expect(await app.page.evaluate(() => (window as any).__videoGetClipDataCalls)).toBe(0);
  });

  test('plays video in the lightbox and releases it on close', async ({ app }) => {
    const videoPath = await createTempFile(generateTestVideo(), 'mp4');
    const filename = path.basename(videoPath);

    await app.uploadFile(videoPath);
    await app.openLightbox(filename);

    const video = app.page.locator(selectors.lightbox.video);
    await expect(video).toBeVisible();
    await expect(video).toHaveAttribute('controls', '');
    await expect(app.page.locator(selectors.lightbox.zoomControl)).toBeHidden();
    await expect(app.page.locator(selectors.lightbox.imageInfo)).toContainText('160×90');

    // Image-only zoom must stay disabled while a video is current: a
    // ctrl+wheel over the video must not change the zoom scale. (It cannot be
    // asserted via event.defaultPrevented any more: since the modal scroll
    // lock, the document-level lock legitimately preventDefaults wheel events
    // a modal cannot consume.)
    const scaleTransform = () => app.page.evaluate(() => {
      const img = document.querySelector('#lightbox-img');
      return img ? img.style.transform : '';
    });
    const before = await scaleTransform();
    await video.evaluate((element: HTMLVideoElement) => {
      element.dispatchEvent(new WheelEvent('wheel', {
        bubbles: true,
        cancelable: true,
        ctrlKey: true,
        deltaY: 24,
      }));
    });
    expect(await scaleTransform()).toBe(before);

    await video.evaluate(async (element: HTMLVideoElement) => {
      element.loop = true;
      await element.play();
    });
    await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(false);

    await app.closeLightbox();
    await expect(video).toBeHidden();
    await expect.poll(() => video.evaluate(
      (element: HTMLVideoElement) => element.paused && !element.hasAttribute('src'),
    )).toBe(true);
  });

  test('navigates between video and image clips in one media sequence', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(120, 80), 'png');
    const videoPath = await createTempFile(generateTestVideo(), 'mp4');

    await app.uploadFile(imagePath);
    await app.uploadFile(videoPath);
    await app.openLightbox(path.basename(videoPath));

    await app.lightboxNext();
    await expect(app.page.locator(selectors.lightbox.caption)).toContainText(path.basename(imagePath));
    await expect(app.page.locator(selectors.lightbox.image)).toBeVisible();
    await expect(app.page.locator(selectors.lightbox.video)).toBeHidden();
    await expect(app.page.locator(selectors.lightbox.zoomControl)).toBeVisible();
  });
});
