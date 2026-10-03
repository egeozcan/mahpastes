import { expect, request, test } from '@playwright/test';
import { authedRequestContext, spawnServer } from '../../fixtures/server-fixtures';

// The web UI pages its gallery through the REST shim (ListClipsPage →
// GET /api/v1/clips?offset=…), which used to slice a listing already capped at 50.
test('server-mode gallery shows "50 of N" and loads the rest', async ({ page }) => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(request, server);
    try {
      for (let i = 0; i < 55; i++) {
        const res = await ctx.post(`/api/v1/clips?filename=s-${i}.txt`, {
          multipart: { file: { name: `s-${i}.txt`, mimeType: 'text/plain', buffer: Buffer.from(`server clip ${i}`) } },
        });
        expect(res.status()).toBe(201);
      }
    } finally {
      await ctx.dispose();
    }

    await page.goto(server.url);
    await page.locator('#api-key').fill(server.bootstrapKey);
    await page.getByRole('button', { name: /sign in/i }).click();
    await page.waitForURL(`${server.url}/`, { timeout: 30000 });

    const cards = page.locator('#gallery > li[data-id]');
    await expect(cards).toHaveCount(50, { timeout: 30000 });
    await expect(page.locator('#clip-count')).toHaveText('50 of 55 clips');
    await page.locator('#gallery-load-more-btn').click();
    await expect(cards).toHaveCount(55);
    await expect(page.locator('#clip-count')).toHaveText('55 clips');
  } finally {
    await server.stop();
  }
});
