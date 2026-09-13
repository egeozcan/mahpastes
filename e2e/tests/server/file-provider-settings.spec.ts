import { expect, test } from '@playwright/test';
import { spawnServer } from '../../fixtures/server-fixtures';

test('Finder access stays hidden and inert in headless mode', async ({ page }) => {
  const server = await spawnServer();
  try {
    await page.goto(server.url);
    await page.locator('#api-key').fill(server.bootstrapKey);
    await page.getByRole('button', { name: /sign in/i }).click();
    await page.waitForURL(`${server.url}/`, { timeout: 30000 });
    await page.evaluate(() => (window as any).openSettings());
    await expect(page.locator('#settings-modal')).toHaveClass(/opacity-100/);
    await expect(page.locator('#file-provider-settings')).toBeHidden();
    expect(await page.evaluate(() => typeof (window as any).go.main.FileProviderService)).toBe('undefined');
  } finally { await server.stop(); }
});

test('Finder controls use capability status, surface failures and allow retry', async ({ page }) => {
  const server = await spawnServer();
  try {
    await page.goto(server.url);
    await page.locator('#api-key').fill(server.bootstrapKey);
    await page.getByRole('button', { name: /sign in/i }).click();
    await page.waitForURL(`${server.url}/`, { timeout: 30000 });
    // Exercise the real settings module with the native boundary replaced.
    // This is UI contract coverage, not evidence of File Provider registration.
    await page.evaluate(() => {
      const w = window as any;
      let first = true;
      let status = { supported: true, enabled: false, running: false, message: '', recoveryPath: '' };
      w.go.main.FileProviderService = {
        Status: async () => status,
        Enable: async () => {
          status.enabled = true;
          if (first) { first = false; status.message = 'Registration failed'; throw new Error(status.message); }
          status.running = true; status.message = ''; return status;
        },
        Disable: async () => {
          status = { supported: true, enabled: false, running: false, message: '', recoveryPath: '/Users/test/Recovered files' };
          return status;
        },
        Reveal: async () => {},
      };
      w.openSettings();
    });
    await expect(page.locator('#file-provider-settings')).toBeVisible();
    await page.locator('#file-provider-toggle').click();
    await expect(page.locator('#file-provider-status')).toHaveText('Registration failed');
    await page.locator('#file-provider-retry').click();
    await expect(page.locator('#file-provider-reveal')).toBeVisible();
    await page.locator('#file-provider-toggle').click();
    await expect(page.locator('#file-provider-status')).toContainText('/Users/test/Recovered files');
    await expect(page.locator('#file-provider-reveal')).toBeHidden();
  } finally { await server.stop(); }
});
