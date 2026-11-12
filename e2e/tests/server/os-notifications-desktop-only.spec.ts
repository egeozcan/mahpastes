import { expect, test } from '@playwright/test';
import { spawnServer } from '../../fixtures/server-fixtures';

// System notifications are shown by the desktop app's OS, so the Settings
// section is hidden by `.server-mode .desktop-only` and the REST glue stub
// refuses to switch them on.
test('system notifications are hidden and inert in server mode', async ({ page }) => {
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

    await expect(page.locator('[data-testid="settings-os-notifications-section"]')).toHaveCSS('display', 'none');

    const result = await page.evaluate(async () => {
      // @ts-ignore - REST glue binding surface
      const svc = window.go.main.NotificationService;
      const status = await svc.GetNotificationStatus();
      let rejected = false;
      try {
        await svc.SetNotificationsEnabled(true);
      } catch {
        rejected = true;
      }
      return { supported: status.supported, rejected };
    });
    expect(result).toEqual({ supported: false, rejected: true });
  } finally {
    await server.stop();
  }
});
