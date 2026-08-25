import { expect, test } from '@playwright/test';
import { spawnServer } from '../../fixtures/server-fixtures';

// Folder import walks the machine's own filesystem, which is meaningless for a
// remote browser and would be a remote directory-listing / arbitrary-read /
// move-to-trash primitive if it worked. Two independent layers keep it off in
// server mode: the `.desktop-only` CSS rule on the drawer entry, and throwing
// stubs in rest-glue.js. This pins both.
async function login(page: import('@playwright/test').Page, server: Awaited<ReturnType<typeof spawnServer>>) {
  await page.goto(server.url);
  await expect(page).toHaveURL(/\/login\.html$/);
  await page.locator('#api-key').fill(server.bootstrapKey);
  await page.getByRole('button', { name: /sign in/i }).click();
  await page.waitForURL(`${server.url}/`, { timeout: 30000 });
  await expect.poll(() =>
    page.evaluate(() => document.documentElement.classList.contains('server-mode'))
  ).toBe(true);
}

test('folder import is hidden and inert in server mode', async ({ page }) => {
  const server = await spawnServer();
  try {
    await login(page, server);

    // Layer 1: the drawer entry is hidden by `.server-mode .desktop-only`.
    await page.locator('#drawer-toggle-btn').click();
    await expect(page.locator('[data-testid="open-import-btn"]')).toBeHidden();

    // Layer 2: the bindings themselves refuse, so nothing that reaches them
    // another way can scan the server's disk.
    for (const method of ['BeginImportSession', 'StartImportSession', 'ImportInspect', 'ImportApply']) {
      const rejected = await page.evaluate(async (name) => {
        try {
          // @ts-ignore - Wails/REST glue binding surface
          await window.go.main.App[name]('/etc', false);
          return false;
        } catch {
          return true;
        }
      }, method);
      expect(rejected, `${method} should reject in server mode`).toBe(true);
    }
  } finally {
    await server.stop();
  }
});
