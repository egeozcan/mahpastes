import { expect, request, test } from '@playwright/test';
import { authedRequestContext, spawnServer } from '../../fixtures/server-fixtures';

// "Copy > Public Link" minted a permanent, unauthenticated download link and
// said only "Copied to clipboard!". The tooltip called it revocable, but the
// web UI had no way to revoke one, or even to see which exist.
test('a public link says what it is, can be revoked from its toast, and is listed with Revoke', async ({ page }) => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(request, server);
    try {
      const res = await ctx.post('/api/v1/clips?filename=handout.txt', {
        multipart: { file: { name: 'handout.txt', mimeType: 'text/plain', buffer: Buffer.from('public body') } },
      });
      expect(res.status()).toBe(201);
    } finally {
      await ctx.dispose();
    }

    await page.goto(server.url);
    await page.locator('#api-key').fill(server.bootstrapKey);
    await page.getByRole('button', { name: /sign in/i }).click();
    await page.waitForURL(`${server.url}/`, { timeout: 30000 });
    const card = page.locator('#gallery > li[data-filename="handout.txt"]');
    await expect(card).toBeVisible({ timeout: 30000 });
    const id = Number(await card.getAttribute('data-id'));

    const mint = async () => {
      const response = page.waitForResponse((r) => r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/v1/links');
      // @ts-ignore - app global
      await page.evaluate((clipId) => createAndCopyPublicLink(clipId), id);
      return (await response).json();
    };
    const anon = await request.newContext();
    try {
      // First link: revoked from its own toast.
      const first = await mint();
      await expect(page.locator('#toast')).toContainText(/Public link (copied\. Anyone with it can download this clip\.|created, but it could not be copied)/);
      expect((await anon.get(`${server.url}/s/${first.token}`)).status()).toBe(200);
      await page.locator('#toast [data-testid="toast-action"]').click();
      await expect(page.locator('#toast')).toContainText('Public link revoked.');
      expect((await anon.get(`${server.url}/s/${first.token}`)).status()).toBe(404);

      // Second link: found, named after its clip, and revoked in the list.
      const second = await mint();
      await page.evaluate(() => (window as any).openApiModal());
      const row = page.locator(`[data-testid="public-link-${second.info.id}"]`);
      await expect(row).toContainText('handout.txt');
      await expect(row).toContainText('no expiry');
      // The revoked one is not listed.
      await expect(page.locator(`[data-testid="public-link-${first.info.id}"]`)).toHaveCount(0);
      await page.locator(`[data-testid="revoke-public-link-${second.info.id}"]`).click();
      await expect(page.locator('[data-testid="public-links-list"]')).toContainText('No public links.');
      expect((await anon.get(`${server.url}/s/${second.token}`)).status()).toBe(404);
    } finally {
      await anon.dispose();
    }
  } finally {
    await server.stop();
  }
});
