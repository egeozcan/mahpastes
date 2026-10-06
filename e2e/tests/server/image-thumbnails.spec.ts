import { expect, request, test } from '@playwright/test';
import { authedRequestContext, spawnServer } from '../../fixtures/server-fixtures';
import { generateTestImage } from '../../helpers/test-data';

// Server mode: image cards load GET /api/v1/clips/{id}/thumb?h=<hash> straight
// into <img>, and the lightbox loads the same-origin data URL — no Blob ->
// FileReader -> base64 -> data: URL round trip.
test('server-mode image cards use thumbnail URLs and the lightbox the full image', async ({ page }) => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(request, server);
    let clipID = 0;
    try {
      const res = await ctx.post('/api/v1/clips?filename=big.png', {
        multipart: { file: { name: 'big.png', mimeType: 'image/png', buffer: generateTestImage(1600, 1200, [30, 90, 160]) } },
      });
      expect(res.status()).toBe(201);
      clipID = (await res.json()).id;

      const thumb = await ctx.get(`/api/v1/clips/${clipID}/thumb`);
      expect(thumb.status()).toBe(200);
      expect(thumb.headers()['content-type']).toBe('image/jpeg');
      expect(thumb.headers()['x-content-type-options']).toBe('nosniff');
      expect(thumb.headers()['cache-control']).toContain('private');
    } finally {
      await ctx.dispose();
    }

    // Unauthenticated: no thumbnail.
    const anon = await request.newContext({ baseURL: server.url });
    try {
      expect((await anon.get(`/api/v1/clips/${clipID}/thumb`)).status()).toBe(401);
    } finally {
      await anon.dispose();
    }

    await page.addInitScript(() => {
      const w = window as any;
      w.__fileReaderReads = 0;
      const original = FileReader.prototype.readAsDataURL;
      FileReader.prototype.readAsDataURL = function (...args: any[]) {
        w.__fileReaderReads++;
        return original.apply(this, args as any);
      };
    });
    await page.goto(server.url);
    await page.locator('#api-key').fill(server.bootstrapKey);
    await page.getByRole('button', { name: /sign in/i }).click();
    await page.waitForURL(`${server.url}/`, { timeout: 30000 });

    const image = page.locator(`#gallery > li[data-id="${clipID}"] img[data-clip-id]:not(.video-thumb)`);
    await expect(image).toHaveAttribute('src', new RegExp(`^/api/v1/clips/${clipID}/thumb\\?h=[0-9a-f]{64}$`), { timeout: 30000 });
    await expect.poll(() => image.evaluate((el: HTMLImageElement) => el.complete ? el.naturalWidth : 0)).toBe(512);

    await page.locator(`#gallery > li[data-id="${clipID}"] [data-action="open-lightbox"]`).click();
    const full = page.locator('#lightbox-img');
    await expect(full).toHaveAttribute('src', `/api/v1/clips/${clipID}/data`);
    await expect.poll(() => full.evaluate((el: HTMLImageElement) => el.complete ? el.naturalWidth : 0)).toBe(1600);

    expect(await page.evaluate(() => (window as any).__fileReaderReads)).toBe(0);
  } finally {
    await server.stop();
  }
});
