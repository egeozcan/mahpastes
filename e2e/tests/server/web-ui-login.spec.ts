import { expect, test } from '@playwright/test';
import { spawnServer } from '../../fixtures/server-fixtures';

test('web UI login sets a session cookie and loads the app shell', async ({ page }) => {
  const server = await spawnServer();
  try {
    await page.goto(server.url);
    await expect(page).toHaveURL(/\/login\.html$/);

    await page.locator('#api-key').fill(server.bootstrapKey);
    await page.getByRole('button', { name: /sign in/i }).click();

    // The login POST navigates to the app shell, which loads the whole frontend
    // including the committed text-editor bundle. This step measures ~9s
    // uncontended, so a 10s budget tipped over whenever the main config ran
    // these specs alongside four `wails dev` workers. 30s matches the config's
    // navigationTimeout.
    await page.waitForURL(`${server.url}/`, { timeout: 30000 });
    await expect(page.locator('#gallery')).toBeAttached();
    await expect.poll(() => page.evaluate(() => document.documentElement.classList.contains('server-mode'))).toBe(true);
  } finally {
    await server.stop();
  }
});
