import { expect, test } from '@playwright/test';
import { spawnServer } from '../../fixtures/server-fixtures';
import * as fs from 'fs/promises';
import * as os from 'os';
import * as path from 'path';

// The web UI showed the upload expiry select but dropped its value: a clip
// uploaded with "15m" selected was permanent. The upload route takes no
// expiry, so the shim sets it with a second request, and only on a clip the
// upload created — identical content returns the clip already stored, which
// must not be given an expiry.
test('server-mode upload applies the selected expiry to new clips only', async ({ page }) => {
  const server = await spawnServer();
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'mp-expiry-'));
  try {
    await page.goto(server.url);
    await page.locator('#api-key').fill(server.bootstrapKey);
    await page.getByRole('button', { name: /sign in/i }).click();
    await page.waitForURL(`${server.url}/`, { timeout: 30000 });
    await expect(page.locator('#expiry-select')).toBeVisible({ timeout: 30000 });

    const expirations: { url: string; body: any }[] = [];
    page.on('request', (req) => {
      if (req.method() === 'PUT' && /\/api\/v1\/clips\/\d+\/expiration$/.test(new URL(req.url()).pathname)) {
        expirations.push({ url: req.url(), body: req.postDataJSON() });
      }
    });

    const file = path.join(dir, 'temp-note.txt');
    await fs.writeFile(file, 'expires soon');
    await page.locator('#expiry-select').selectOption('15');
    await page.locator('#file-input').setInputFiles([file]);
    await expect(page.locator('#gallery > li[data-id]')).toHaveCount(1, { timeout: 30000 });
    await expect(page.locator('#toast')).toContainText('1 uploaded, expiring in 15m.');
    await expect.poll(() => expirations.length).toBe(1);
    expect(expirations[0].body).toEqual({ minutes: 15 });

    // The same content again: the server answers with the stored clip, and
    // no expiry is put on it.
    const copy = path.join(dir, 'temp-note-copy.txt');
    await fs.writeFile(copy, 'expires soon');
    await page.locator('#file-input').setInputFiles([copy]);
    await expect(page.locator('#toast')).toContainText('uploaded');
    await page.waitForTimeout(500);
    expect(expirations.length).toBe(1);
  } finally {
    await fs.rm(dir, { recursive: true, force: true });
    await server.stop();
  }
});
