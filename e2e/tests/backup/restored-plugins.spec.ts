import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

const pluginSource = `
Plugin = {
    name = "Restore Review Plugin",
    version = "1.0.0",
    description = "Checks that restored plugins wait for review",
    author = "Test Author",
    events = {"clip:created"},
}

function on_clip_created(data) end
`;

async function createBackup(app: any, dest: string) {
  await app.page.evaluate(async (p: string) => {
    // @ts-ignore - Wails runtime
    await window.go.main.App.CreateBackup(p);
  }, dest);
}

async function restoreBackup(app: any, src: string) {
  await app.page.evaluate(async (p: string) => {
    // @ts-ignore - Wails runtime
    await window.go.main.App.ConfirmRestoreBackup(p, 'none');
  }, src);
}

async function restoredPlugin(app: any) {
  const plugins = await app.getPlugins();
  const p = plugins.find((x: any) => x.name === 'Restore Review Plugin');
  expect(p).toBeTruthy();
  return p;
}

test.describe('Backup & Restore - Restored plugins', () => {
  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
  });

  test('a plugin not installed here comes back disabled and held for review', async ({ app, tempDir }) => {
    const pluginPath = await createTempFile(Buffer.from(pluginSource), 'lua');
    expect(await app.importPluginFromPath(pluginPath)).toBeTruthy();
    const backupPath = path.join(tempDir, 'plugin-review.zip');
    await createBackup(app, backupPath);

    await app.deleteAllPlugins();
    await restoreBackup(app, backupPath);

    const p = await restoredPlugin(app);
    expect(p.enabled).toBe(false);
    expect(p.status).toBe('needs_review');
  });

  test('a restored plugin already installed here, unchanged, keeps running', async ({ app, tempDir }) => {
    const pluginPath = await createTempFile(Buffer.from(pluginSource), 'lua');
    expect(await app.importPluginFromPath(pluginPath)).toBeTruthy();
    const backupPath = path.join(tempDir, 'plugin-same.zip');
    await createBackup(app, backupPath);

    await restoreBackup(app, backupPath);

    const p = await restoredPlugin(app);
    expect(p.enabled).toBe(true);
    expect(p.status).toBe('enabled');
  });

  test('enabling a held plugin shows the review, and only approval enables it', async ({ app, tempDir }) => {
    const pluginPath = await createTempFile(Buffer.from(pluginSource), 'lua');
    expect(await app.importPluginFromPath(pluginPath)).toBeTruthy();
    const backupPath = path.join(tempDir, 'plugin-approve.zip');
    await createBackup(app, backupPath);
    await app.deleteAllPlugins();
    await restoreBackup(app, backupPath);
    const p = await restoredPlugin(app);

    await app.openPluginsModal();
    await expect(app.page.locator(selectors.plugins.needsReviewBadge(p.id))).toBeVisible();

    const toggle = app.page.locator(`${selectors.plugins.pluginCard(p.id)} [data-action="toggle-enable"]`);
    const reviewModal = app.page.locator(`${selectors.pluginReview.modal}.opacity-100`);

    // Cancelling leaves it disabled.
    await toggle.click();
    await expect(reviewModal).toBeVisible();
    await expect(app.page.locator(selectors.pluginReview.title)).toHaveText('Review Restored Plugin');
    await expect(app.page.locator(selectors.pluginReview.name)).toHaveText('Restore Review Plugin');
    await app.page.locator(selectors.pluginReview.cancelButton).click();
    await expect(reviewModal).toBeHidden();
    expect((await restoredPlugin(app)).enabled).toBe(false);
    await expect(app.page.locator(selectors.plugins.pluginToggle(p.id))).not.toBeChecked();

    // Approving enables it.
    await toggle.click();
    await expect(reviewModal).toBeVisible();
    await app.page.locator(selectors.pluginReview.approveButton).click();
    await expect.poll(async () => (await restoredPlugin(app)).enabled, { timeout: 5000 }).toBe(true);
    expect((await restoredPlugin(app)).status).toBe('enabled');
    await expect(app.page.locator(selectors.plugins.needsReviewBadge(p.id))).toHaveCount(0);
  });
});
