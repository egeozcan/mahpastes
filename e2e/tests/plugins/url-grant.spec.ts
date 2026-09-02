import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
import * as path from 'path';
import * as http from 'http';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const PLUGIN_PATH = path.resolve(__dirname, '../../test-plugins/url-grant.lua');

// The url setting grant model: saving a url setting grants the plugin
// network access to its host, revocably; storage.set refuses the key from
// Lua; a denied host's failure surfaces as the plugin's own message.
test.describe('Plugin URL Grant', () => {
  let pluginId: number | null = null;

  // One server reachable as both "localhost" and "127.0.0.1" — two distinct
  // grant hosts, so a retarget test can flip between them.
  let server: http.Server;
  let serverPort = 0;
  let localhostURL = '';
  let loopbackURL = '';

  test.beforeAll(async () => {
    server = http.createServer((req, res) => {
      res.writeHead(200, { 'Content-Type': 'text/plain' });
      res.end('ok');
    });
    await new Promise<void>((resolve) => {
      server.listen(0, '127.0.0.1', () => {
        serverPort = (server.address() as any).port;
        localhostURL = `http://localhost:${serverPort}`;
        loopbackURL = `http://127.0.0.1:${serverPort}`;
        resolve();
      });
    });
  });

  test.afterAll(async () => {
    if (server) {
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  });

  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    pluginId = null;
  });

  test.afterEach(async ({ app }) => {
    if (pluginId) {
      try {
        await app.removePlugin(pluginId);
      } catch {}
    }
  });

  async function install(app: any): Promise<any> {
    const plugin = await app.importPluginFromPath(PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    pluginId = plugin!.id;
    await app.enablePlugin(plugin!.id);
    await app.page.reload();
    await app.waitForReady();
    return plugin!;
  }

  async function openSettings(app: any): Promise<any> {
    await app.openPluginsModal();
    const card = app.page.locator(selectors.plugins.pluginCard(pluginId!));
    await card.locator(selectors.plugins.expandToggle).click();
    await expect(card.locator(selectors.pluginSettings.section)).toBeVisible({ timeout: 5000 });
    return card;
  }

  // Waits for the debounced url-setting save to produce a network grant.
  async function expectGranted(card: any, host: string) {
    await expect(
      card.locator(selectors.plugins.permissionsList),
      `expected a network grant for ${host}`
    ).toContainText(host, { timeout: 5000 });
  }

  async function search(app: any, source: string, query: string) {
    return app.page.evaluate(async ({ pid, src, q }: { pid: number; src: string; q: string }) => {
      // @ts-ignore - Wails runtime
      return await window.go.main.PluginService.SearchPluginOptions(pid, src, q);
    }, { pid: pluginId, src: source, q: query });
  }

  // The Wails bridge rejects with a plain string, not an Error, so capture
  // the message instead of using expect(...).rejects.
  async function searchError(app: any, source: string, query: string): Promise<string> {
    try {
      await search(app, source, query);
      return '';
    } catch (e: any) {
      return String(e && e.message ? e.message : e);
    }
  }

  test('saving a url setting grants the host and http.get succeeds', async ({ app }) => {
    await install(app);
    const card = await openSettings(app);

    const input = card.locator(selectors.pluginSettings.urlInput);
    await input.fill(localhostURL);
    await expectGranted(card, 'localhost');

    // The grant row is visible on the card with its network badge and Revoke.
    const row = card.locator(selectors.plugins.permissionsList).locator('div', { hasText: 'localhost' }).first();
    await expect(row).toContainText('network');
    await expect(row.locator(selectors.plugins.permissionRevoke)).toBeVisible();

    // The plugin can now reach the granted host for real.
    const rows = await search(app, 'fetch', `${localhostURL}/ping`);
    expect(rows).toEqual([{ value: '200', label: 'HTTP 200' }]);
  });

  test('retargeting the URL revokes the old host and grants the new one', async ({ app }) => {
    await install(app);
    const card = await openSettings(app);

    const input = card.locator(selectors.pluginSettings.urlInput);
    await input.fill(localhostURL);
    await expectGranted(card, 'localhost');

    await input.fill(loopbackURL);
    await expectGranted(card, '127.0.0.1');

    // Old host denied, new host allowed.
    expect(await searchError(app, 'fetch', `${localhostURL}/ping`)).toMatch(
      /domain not in allowlist: localhost/
    );
    const rows = await search(app, 'fetch', `${loopbackURL}/ping`);
    expect(rows).toEqual([{ value: '200', label: 'HTTP 200' }]);
  });

  test('revoking from the status line denies the plugin until re-granted', async ({ app }) => {
    await install(app);
    const card = await openSettings(app);

    const input = card.locator(selectors.pluginSettings.urlInput);
    await input.fill(localhostURL);
    await expectGranted(card, 'localhost');

    // Status line reports the grant with a Revoke link.
    const status = card.locator(selectors.pluginSettings.urlStatus);
    await expect(status).toContainText('Network access granted to');
    await status.locator(selectors.pluginSettings.revokeGrant).click();
    await expect(status).toContainText('Grant access to localhost');

    expect(await searchError(app, 'fetch', `${localhostURL}/ping`)).toMatch(
      /domain not in allowlist/
    );

    // The Grant button re-grants without touching the input.
    await status.locator(selectors.pluginSettings.grantButton).click();
    await expect(status).toContainText('Network access granted to');
    const rows = await search(app, 'fetch', `${localhostURL}/ping`);
    expect(rows).toEqual([{ value: '200', label: 'HTTP 200' }]);
  });

  test('revoking from the permissions list denies the plugin again', async ({ app }) => {
    await install(app);
    const card = await openSettings(app);

    const input = card.locator(selectors.pluginSettings.urlInput);
    await input.fill(localhostURL);
    await expectGranted(card, 'localhost');

    await card.locator(selectors.plugins.permissionRevoke).first().click();
    await expect(
      card.locator(selectors.plugins.permissionsList)
    ).toContainText('No permissions granted');

    // The permissions-list revoke also refreshes the url setting's status
    // line: it must flip from "granted" back to the Grant button.
    await expect(card.locator(selectors.pluginSettings.urlStatus)).toContainText(
      'Grant access to localhost'
    );

    expect(await searchError(app, 'fetch', `${localhostURL}/ping`)).toMatch(
      /domain not in allowlist/
    );
  });

  test('storage.set on the url key fails from Lua', async ({ app }) => {
    await install(app);

    expect(
      await searchError(app, 'write', 'https://evil.example')
    ).toMatch(/WRITE-BLOCKED: storage\.set: key 'server_url' is a url setting managed by the user/);
  });

  test('a denied host renders the plugin message, not "No results"', async ({ app }) => {
    await install(app);
    const card = await openSettings(app);

    // No grant exists yet, so the probe's http.get is denied by policy and
    // on_search returns nil, "domain not in allowlist: localhost".
    const input = card.locator(selectors.pluginSearch.settingInput('probe'));
    await input.fill(`${localhostURL}/ping`);

    // Scope to the probe field: the plugin has a second search setting whose
    // (closed) listbox would otherwise trip strict mode.
    const dropdown = card
      .locator('.setting-field[data-key="probe"]')
      .locator(selectors.pluginSearch.dropdown);
    await expect(dropdown).toBeVisible({ timeout: 5000 });
    await expect(dropdown).toContainText('domain not in allowlist: localhost');
    await expect(dropdown).not.toContainText('No results');
    await expect(dropdown).not.toContainText('Search failed');
  });
});
