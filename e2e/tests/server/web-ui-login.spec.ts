import { expect, test } from '@playwright/test';
import { spawnServer } from '../../fixtures/server-fixtures';

test('web UI login sets a session cookie and loads the app shell', async ({ page }) => {
  const server = await spawnServer();
  try {
    await page.goto(server.url);
    await expect(page).toHaveURL(/\/login\.html$/);

    await page.locator('#api-key').fill(server.bootstrapKey);
    await page.getByRole('button', { name: /sign in/i }).click();

    await page.waitForURL(`${server.url}/`, { timeout: 10000 });
    await expect(page.locator('#gallery')).toBeAttached();
    await expect.poll(() => page.evaluate(() => document.documentElement.classList.contains('server-mode'))).toBe(true);
  } finally {
    await server.stop();
  }
});
