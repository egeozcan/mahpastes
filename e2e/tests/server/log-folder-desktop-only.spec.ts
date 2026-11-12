import { expect, test } from '@playwright/test';
import { spawnServer } from '../../fixtures/server-fixtures';

// The log folder lives on the server's disk, so the Maintenance entry for it
// is hidden by `.server-mode .desktop-only`, and the REST glue stub refuses.
test('open log folder is hidden and inert in server mode', async ({ page }) => {
  const server = await spawnServer();
  try {
    await page.goto(server.url);
    await expect(page).toHaveURL(/\/login\.html$/);
    await page.locator('#api-key').fill(server.bootstrapKey);
    await page.getByRole('button', { name: /sign in/i }).click();
    await page.waitForURL(`${server.url}/`, { timeout: 30000 });
    await expect.poll(() =>
      page.evaluate(() => document.documentElement.classList.contains('server-mode'))
    ).toBe(true);

    await expect(page.locator('[data-testid="maintenance-log-section"]')).toHaveCSS('display', 'none');

    const rejected = await page.evaluate(async () => {
      try {
        // @ts-ignore - REST glue binding surface
        await window.go.main.App.OpenLogFolder();
        return false;
      } catch {
        return true;
      }
    });
    expect(rejected).toBe(true);
  } finally {
    await server.stop();
  }
});
