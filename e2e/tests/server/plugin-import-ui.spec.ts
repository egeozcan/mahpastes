import { expect, test } from '@playwright/test';
import { spawnServer } from '../../fixtures/server-fixtures';

const MINIMAL_PLUGIN = `Plugin = {
    name = "E2E UI Import Plugin",
    version = "0.1.0",
    description = "Imported through the server web UI in an e2e test.",
    author = "e2e",
}
`;

// Logs the web UI in and returns once the app shell is loaded in server mode.
async function login(page: import('@playwright/test').Page, server: Awaited<ReturnType<typeof spawnServer>>) {
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
  await expect.poll(() => page.evaluate(() => document.documentElement.classList.contains('server-mode'))).toBe(true);
}

async function openPluginsModal(page: import('@playwright/test').Page) {
  await page.locator('#drawer-toggle-btn').click();
  await page.locator('#open-plugins-btn').click();
  await page.waitForSelector('[data-testid="plugins-modal"].opacity-100', { timeout: 5000 });
}

test('the Import button is visible in server mode and runs the upload→review→install flow', async ({ page }) => {
  const server = await spawnServer();
  try {
    await login(page, server);
    await openPluginsModal(page);

    // The button must not be hidden by the server-mode `.desktop-only` rule —
    // local import works on the headless server via browser upload.
    const importBtn = page.locator('[data-testid="import-plugin-btn"]');
    await expect(importBtn).toBeVisible();

    // Clicking it opens a file chooser (a hidden <input type=file>); feed it our
    // plugin instead of a native dialog.
    page.once('filechooser', async (chooser) => {
      await chooser.setFiles({
        name: 'e2e-ui-import-plugin.lua',
        mimeType: 'text/x-lua',
        buffer: Buffer.from(MINIMAL_PLUGIN),
      });
    });
    await importBtn.click();

    // The permission review modal should appear with the parsed manifest.
    const review = page.locator('[data-testid="plugin-review-modal"]');
    await expect(review).toBeVisible({ timeout: 5000 });
    await expect(page.locator('#plugin-review-name')).toContainText('E2E UI Import Plugin');

    // Approve -> confirm install.
    await page.locator('[data-testid="plugin-review-approve"]').click();

    // The plugin shows up in the list.
    await expect(page.locator('[data-testid="plugins-list"]')).toContainText('E2E UI Import Plugin', { timeout: 5000 });
  } finally {
    await server.stop();
  }
});
