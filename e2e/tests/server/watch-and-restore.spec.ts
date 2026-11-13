import { expect, test } from '@playwright/test';
import { spawnServer } from '../../fixtures/server-fixtures';
import * as fs from 'fs/promises';
import * as os from 'os';
import * as path from 'path';

// The web UI's Watch view had no way to add a folder (the picker is native),
// and Backup & Restore said "restoring will replace all data" beside no
// Restore button at all.
test('the web UI can add a watch folder by its server path, and says where restoring is done', async ({ page }) => {
  const server = await spawnServer();
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'mp-server-watch-'));
  try {
    await page.goto(server.url);
    await page.locator('#api-key').fill(server.bootstrapKey);
    await page.getByRole('button', { name: /sign in/i }).click();
    await page.waitForURL(`${server.url}/`, { timeout: 30000 });
    await expect(page.locator('#expiry-select')).toBeVisible({ timeout: 30000 });

    await page.evaluate(() => (window as any).__testHelpers?.switchView ? (window as any).__testHelpers.switchView('watch') : (window as any).switchView('watch'));
    // The native picker is hidden; the typed path takes its place.
    await expect(page.locator('#add-folder-zone')).toBeHidden();
    await expect(page.locator('[data-testid="add-folder-server"]')).toBeVisible();

    await page.locator('[data-testid="add-folder-server-path"]').fill(dir);
    await page.locator('[data-testid="add-folder-server-btn"]').click();
    await expect(page.locator('#folder-modal-path')).toHaveText(dir);
    await page.locator('#folder-modal-save').click();
    await expect(page.locator('#watch-folder-list > li')).toHaveCount(1, { timeout: 15000 });
    await expect(page.locator('#watch-folder-list > li')).toContainText(dir);

    await page.evaluate(() => (window as any).openSettings());
    await expect(page.locator('[data-testid="restore-server-note"]')).toBeVisible();
    await expect(page.locator('[data-testid="restore-server-note"]')).toContainText('mp backup restore');
    await expect(page.locator('#restore-backup-btn')).toBeHidden();
  } finally {
    await fs.rm(dir, { recursive: true, force: true });
    await server.stop();
  }
});
