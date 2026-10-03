import { test, expect } from '../../fixtures/test-fixtures';
import type { Page } from '@playwright/test';
import * as fs from 'fs/promises';
import * as path from 'path';

/**
 * Strings that arrive from outside the user's own hands — a backup file
 * someone sent, a plugin manifest, a watch folder or tag written through the
 * REST API or restored from a backup — reach views that build markup with
 * innerHTML. Each one must render as text: one raw interpolation runs script
 * in a webview that exposes every bound Go method.
 */
test.describe('Untrusted strings in markup', () => {
  async function expectInert(page: Page, id: string): Promise<void> {
    expect(await page.locator(`#${id}`).count()).toBe(0);
    expect(await page.locator('[data-pwned]').count()).toBe(0);
  }

  test('a watch folder card renders its regex and its auto-tag color as text', async ({ app, tempDir }) => {
    const dir = path.join(tempDir, 'watched');
    await fs.mkdir(dir, { recursive: true });
    const regex = '<img id=pwn-regex src=x>';
    await app.addWatchFolder(dir, { filterMode: 'custom', filterRegex: regex });
    await expectInert(app.page, 'pwn-regex');
    await expect(app.page.locator('#watch-folder-list > li').first()).toContainText(`Regex: ${regex}`);

    // The backend no longer stores a color like this, so render the card from
    // a tag list carrying one — what a database from before validation holds.
    await app.page.evaluate(() => {
      // @ts-ignore - test helper
      window.__testHelpers.setAllTags([
        { id: 4242, name: 'auto', color: 'red" data-pwned="1"><img id=pwn-color src=x>', count: 0 },
      ]);
      // @ts-ignore - global from watch.js
      const li = createWatchFolderCard({
        id: 9999, path: '/probe', filter_mode: 'all', filter_presets: [], filter_regex: '',
        auto_tag_id: 4242, exists: true, is_paused: false, auto_archive: false,
      });
      li.id = 'probe-watch-card';
      document.body.appendChild(li);
    });
    try {
      await expectInert(app.page, 'pwn-color');
      const style = await app.page.locator('#probe-watch-card span[style]').getAttribute('style');
      expect(style).toBe('background-color: #78716C');
    } finally {
      await app.page.evaluate(async () => {
        document.getElementById('probe-watch-card')?.remove();
        // @ts-ignore - Wails runtime / test helper
        window.__testHelpers.setAllTags(await window.go.main.App.GetTags());
      });
    }
  });

  test('the remove-empty-tags preview never chains CSS from a stored color', async ({ app }) => {
    try {
    await app.page.evaluate(() => {
      // @ts-ignore - Wails runtime
      const api = window.go.main.App;
      // @ts-ignore
      window.__origGetRemovableEmptyTags = api.GetRemovableEmptyTags;
      api.GetRemovableEmptyTags = async () => [
        { id: 1, name: 'beacon', color: 'red; background-image: url(https://example.invalid/beacon)' },
        { id: 2, name: 'breakout', color: 'red" data-pwned="1"><img id=pwn-empty src=x>' },
      ];
      // @ts-ignore - global from maintenance.js
      runRemoveEmptyTags();
    });
      await app.page.waitForSelector('[data-testid="confirm-dialog"].opacity-100, #confirm-dialog.opacity-100', { timeout: 5000 });
      await expectInert(app.page, 'pwn-empty');
      const styles = await app.page.locator('#confirm-message span[style]').evaluateAll(
        (els) => els.map((el) => el.getAttribute('style') || ''),
      );
      expect(styles.length).toBe(2);
      for (const style of styles) {
        expect(style).not.toContain('background-image');
      }
      await app.cancelDialog();
    } finally {
      await app.page.evaluate(() => {
        // @ts-ignore
        if (window.__origGetRemovableEmptyTags) window.go.main.App.GetRemovableEmptyTags = window.__origGetRemovableEmptyTags;
      });
    }
  });

  test('a plugin name from its manifest renders as text in the remove dialog', async ({ app, tempDir }) => {
    const hostileName = '<img id=pwn-plugin src=x>';
    const pluginPath = path.join(tempDir, 'hostile-name.lua');
    await fs.writeFile(pluginPath, `
Plugin = {
  name = "${hostileName}",
  version = "1.0.0",
  events = {"app:startup"},
}
function on_startup() end
`);
    const result = await app.importPluginFromPath(pluginPath);
    expect(result).not.toBeNull();
    const pluginId = result!.id;

    await app.openPluginsModal();
    const card = app.page.locator(`[data-testid="plugin-card-${pluginId}"]`);
    await card.locator('[data-action="toggle-expand"]').click();
    await app.page.locator(`[data-testid="remove-plugin-${pluginId}"]`).click();
    await app.page.waitForSelector('#confirm-dialog.opacity-100', { timeout: 5000 });

    await expectInert(app.page, 'pwn-plugin');
    await expect(app.page.locator('#confirm-message')).toContainText(`Remove "${hostileName}"?`);
    await app.confirmDialog();
  });

  test('a stored plugin permission renders as text', async ({ app }) => {
    // A restored backup's plugin_permissions rows reach this list as stored,
    // and restore marks every one pending — so the user opens it to reconfirm.
    try {
      await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        const svc = window.go.main.PluginService;
        // @ts-ignore
        window.__origGetPluginPermissions = svc.GetPluginPermissions;
        svc.GetPluginPermissions = async () => [{
          type: '<img id=pwn-perm src=x>" data-pwned="1',
          path: '/tmp',
          granted_at: '2024-01-01',
          pending: '1',
        }];
        const card = document.createElement('div');
        card.id = 'probe-plugin-card';
        card.innerHTML = '<div data-permissions-list></div>';
        document.body.appendChild(card);
        // @ts-ignore - global from plugins.js
        await loadPluginPermissions(1, card);
      });
      await expectInert(app.page, 'pwn-perm');
      await expect(app.page.locator('#probe-plugin-card')).toContainText('<img id=pwn-perm src=x>');
    } finally {
      await app.page.evaluate(() => {
        // @ts-ignore
        if (window.__origGetPluginPermissions) window.go.main.PluginService.GetPluginPermissions = window.__origGetPluginPermissions;
        document.getElementById('probe-plugin-card')?.remove();
      });
    }
  });

  test('a backup manifest renders as text when the file is picked', async ({ app }) => {
    try {
    await app.page.evaluate(() => {
      // @ts-ignore - Wails runtime
      const api = window.go.main.App;
      // @ts-ignore
      window.__origShowRestoreBackupDialog = api.ShowRestoreBackupDialog;
      api.ShowRestoreBackupDialog = async () => ({ manifest: {
        format_version: 1,
        app_version: '<img id=pwn-backup src=x>',
        created_at: new Date().toISOString(),
        platform: 'darwin',
        summary: { clips: 1, tags: 0, plugins: 0, watch_folders: 0 },
        excluded: [],
      }, path: '/nonexistent/crafted-backup.zip' });
      // @ts-ignore - global from settings.js
      return selectRestoreBackup();
    });
      await expect(app.page.locator('#restore-backup-info')).toContainText('<img id=pwn-backup src=x>');
      await expectInert(app.page, 'pwn-backup');
    } finally {
      await app.page.evaluate(() => {
        // @ts-ignore
        if (window.__origShowRestoreBackupDialog) window.go.main.App.ShowRestoreBackupDialog = window.__origShowRestoreBackupDialog;
        // @ts-ignore - global from settings.js
        hideRestoreConfirmDialog();
      });
    }
  });
});
