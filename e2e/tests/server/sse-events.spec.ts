import { expect, request, test } from '@playwright/test';
import { authedRequestContext, spawnServer } from '../../fixtures/server-fixtures';

test('REST uploads emit an SSE event for the server web UI', async ({ page }) => {
  const server = await spawnServer();
  try {
    await page.goto(`${server.url}/login.html`);
    await page.locator('#api-key').fill(server.bootstrapKey);
    await page.getByRole('button', { name: /sign in/i }).click();
    await page.waitForURL(`${server.url}/`, { timeout: 10000 });

    await page.evaluate(() => new Promise<void>((resolve, reject) => {
      const source = new EventSource('/api/v1/events');
      (window as any).__serverEventSource = source;
      const timer = window.setTimeout(() => reject(new Error('timed out opening SSE connection')), 5000);
      source.onopen = () => {
        window.clearTimeout(timer);
        resolve();
      };
      source.onerror = () => {
        window.clearTimeout(timer);
        reject(new Error('failed to open SSE connection'));
      };
    }));

    const waitForImport = page.evaluate(() => new Promise<any>((resolve) => {
      const source = (window as any).__serverEventSource as EventSource;
      const timer = window.setTimeout(() => resolve(null), 10000);
      source.addEventListener('watch:import', (event) => {
        window.clearTimeout(timer);
        resolve(JSON.parse((event as MessageEvent).data));
      });
    }));

    const ctx = await authedRequestContext(request, server);
    try {
      const upload = await ctx.post('/api/v1/clips?filename=sse.txt', {
        multipart: {
          file: {
            name: 'sse.txt',
            mimeType: 'text/plain',
            buffer: Buffer.from('sse event'),
          },
        },
      });
      expect(upload.status()).toBe(201);
    } finally {
      await ctx.dispose();
    }

    const event = await waitForImport;
    expect(event).toMatchObject({ filename: 'sse.txt', content_type: 'text/plain' });
  } finally {
    await page.evaluate(() => (window as any).__serverEventSource?.close()).catch(() => {});
    await server.stop();
  }
});
