import { expect, request, test } from '@playwright/test';
import { authedRequestContext, spawnServer } from '../../fixtures/server-fixtures';

// The web UI patches a card in place after a tag change, re-reading the clip's
// tags through the REST shim's GetClipTags (GET /api/v1/clips/{id}). Deleting
// a clip removes its card without rebuilding the others.
test('server-mode gallery patches tag and delete changes in place', async ({ page }) => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(request, server);
    try {
      for (const name of ['srv-a.txt', 'srv-b.txt']) {
        const res = await ctx.post(`/api/v1/clips?filename=${name}`, {
          multipart: { file: { name, mimeType: 'text/plain', buffer: Buffer.from(`server ${name}`) } },
        });
        expect(res.status()).toBe(201);
      }
      const tag = await ctx.post('/api/v1/tags', { data: { name: 'srv-tag' } });
      expect(tag.ok()).toBe(true);
    } finally {
      await ctx.dispose();
    }

    await page.goto(server.url);
    await page.locator('#api-key').fill(server.bootstrapKey);
    await page.getByRole('button', { name: /sign in/i }).click();
    await page.waitForURL(`${server.url}/`, { timeout: 30000 });

    const cards = page.locator('#gallery > li[data-id]');
    await expect(cards).toHaveCount(2, { timeout: 30000 });
    await page.evaluate(() => {
      document.querySelectorAll('#gallery > li[data-id]').forEach((li: any) => { li.__mark = true; });
    });

    // Neither change may refetch the listing.
    const listings: string[] = [];
    page.on('request', (req) => {
      const url = new URL(req.url());
      if (req.method() === 'GET' && url.pathname === '/api/v1/clips') listings.push(req.url());
    });

    const cardA = page.locator('#gallery > li[data-filename="srv-a.txt"]');
    await cardA.hover();
    await cardA.locator('[data-action="menu"]').click();
    await page.locator('.card-menu-dropdown [data-action="tags"]').click();
    await page.locator('[data-testid="tag-popover"] [data-testid="tag-checkbox-srv-tag"]').check();
    await expect(cardA.locator('.clip-tags button')).toHaveText(['srv-tag']);

    const cardB = page.locator('#gallery > li[data-filename="srv-b.txt"]');
    await cardB.hover();
    await cardB.locator('[data-action="menu"]').click();
    await page.locator('.card-menu-dropdown [data-action="delete"]').click();
    await page.locator('#confirm-yes-btn').click();
    await expect(cards).toHaveCount(1);
    await expect(page.locator('#clip-count')).toHaveText('1 clip');
    expect(await cardA.evaluate((el: any) => !!el.__mark)).toBe(true);
    expect(listings).toEqual([]);
  } finally {
    await server.stop();
  }
});
