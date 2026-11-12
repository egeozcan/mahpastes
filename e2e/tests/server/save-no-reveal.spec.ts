import { expect, test } from '@playwright/test';
import { spawnServer } from '../../fixtures/server-fixtures';

// In server mode Save is a browser download: the REST glue resolves
// SaveClipToFile to the download's name, and the host file manager cannot
// show a file in the remote browser's Downloads folder. So the desktop
// "Show in folder" toast action must not be offered, and the binding itself
// refuses as a second layer.
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

test('saving a clip offers no Show in folder action in server mode', async ({ page }) => {
  const server = await spawnServer();
  try {
    await login(page, server);

    await page.evaluate(async () => {
      const w = window as any;
      // Stand in for the browser download so no real file is fetched.
      w.go.main.App.SaveClipToFile = async () => 'clip.txt';
      // @ts-ignore - global from wails-api.js
      await saveClipToFile(1);
    });
    await page.waitForTimeout(300);
    await expect(page.locator('#toast [data-testid="toast-action"]')).toHaveCount(0);

    const rejected = await page.evaluate(async () => {
      try {
        // @ts-ignore - REST glue binding surface
        await window.go.main.App.RevealInFileManager('/etc/passwd');
        return false;
      } catch {
        return true;
      }
    });
    expect(rejected, 'RevealInFileManager should reject in server mode').toBe(true);
  } finally {
    await server.stop();
  }
});
