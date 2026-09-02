import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestText,
} from '../../helpers/test-data';
import * as path from 'path';
import * as http from 'http';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Path to the mahresources plugin
const PLUGIN_PATH = path.resolve(__dirname, '../../../plugins/mahresources.lua');

// A fake mahresources server. The plugin's network allowlist covers
// localhost/127.0.0.1, so the plugin can talk to it for real.
interface RecordedRequest {
  method: string;
  url: string;
  headers: http.IncomingHttpHeaders;
  body: string;
}

let recorded: RecordedRequest[];
let uploadStatus: number;
let uploadResponseBody: Record<string, unknown>;

let server: http.Server;
let serverPort = 0;
let baseURL = '';

test.describe('mahresources Plugin', () => {
  let pluginId: number | null = null;

  test.beforeAll(async () => {
    uploadStatus = 201;
    uploadResponseBody = [{ id: 1 }];
    server = http.createServer((req, res) => {
      let body = '';
      req.on('data', (chunk) => { body += chunk; });
      req.on('end', () => {
        recorded.push({ method: req.method || '', url: req.url || '', headers: req.headers, body });

        if ((req.url || '').startsWith('/v1/groups')) {
          res.writeHead(200, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify([
            { ID: 11, Name: 'Trips' },
            { ID: 12, Name: 'Work' },
          ]));
          return;
        }

        if ((req.url || '').startsWith('/v1/resource')) {
          // 401 without a token, 403 with an explicitly rejected token,
          // success otherwise — mirrors mahresources' auth failure modes.
          const auth = req.headers['authorization'];
          if (!auth) {
            res.writeHead(401, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: 'unauthorized' }));
            return;
          }
          if (auth === 'Bearer rejected') {
            res.writeHead(403, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: 'insufficient permissions' }));
            return;
          }
          res.writeHead(uploadStatus, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify(uploadResponseBody));
          return;
        }

        res.writeHead(404);
        res.end();
      });
    });
    await new Promise<void>((resolve) => {
      server.listen(0, () => {
        const addr = server.address();
        if (addr && typeof addr !== 'string') {
          serverPort = addr.port;
          baseURL = `http://localhost:${addr.port}`;
        }
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
    recorded = [];
    uploadStatus = 201;
    uploadResponseBody = [{ id: 1 }];
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    await app.deleteAllTags();
    pluginId = null;
  });

  test.afterEach(async ({ app }) => {
    if (pluginId) {
      try {
        await app.removePlugin(pluginId);
      } catch {
        // Ignore cleanup errors
      }
    }
  });

  async function installPlugin(app: any, settings: Record<string, string> = {}) {
    const plugin = await app.importPluginFromPath(PLUGIN_PATH);
    pluginId = plugin?.id ?? null;
    await app.enablePlugin(plugin!.id);
    await app.page.reload();
    await app.waitForReady();
    for (const [key, value] of Object.entries(settings)) {
      await app.setPluginStorage(plugin!.id, key, value);
    }
    return plugin!;
  }

  test('should load plugin and show correct name', async ({ app }) => {
    const plugin = await app.importPluginFromPath(PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    expect(plugin?.name).toBe('mahresources');
    expect(plugin?.enabled).toBe(true);
    pluginId = plugin?.id ?? null;
  });

  test('should show card action when plugin is enabled', async ({ app }) => {
    const plugin = await app.importPluginFromPath(PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;
    await app.enablePlugin(plugin!.id);

    // Reload to refresh UI actions
    await app.page.reload();
    await app.waitForReady();

    const actions = await app.getPluginUIActions();
    const uploadAction = actions.card_actions.find(
      (a: any) => a.id === 'upload' && a.label === 'Upload to mahresources'
    );
    expect(uploadAction).toBeDefined();
    // The upload action carries the owner_id search option for per-upload override.
    expect(
      uploadAction.options.some((o: any) => o.id === 'owner_id' && o.type === 'search')
    ).toBe(true);
  });

  test('should have default settings', async ({ app }) => {
    const plugin = await app.importPluginFromPath(PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    // Check default settings via storage
    const serverUrl = await app.getPluginStorage(plugin!.id, 'server_url');
    // Default might not be set in storage until user saves, so it could be empty.
    // The plugin handles this with fallback defaults in the Lua code.
    expect(serverUrl === '' || serverUrl === 'http://localhost:8181').toBeTruthy();
  });

  test('content_filter select renders with its three options', async ({ app }) => {
    await installPlugin(app);

    await app.openPluginsModal();
    const card = app.page.locator(`[data-testid="plugin-card-${pluginId}"]`);
    await card.locator('[data-action="toggle-expand"]').click();

    const select = card.locator('[data-setting-key="content_filter"][data-setting-type="select"]');
    await expect(select).toBeVisible({ timeout: 5000 });
    const options = await select.locator('option').allTextContents();
    expect(options).toEqual(['all', 'images', 'text']);
  });

  test('auto_upload checkbox writes true/false to storage and back', async ({ app }) => {
    await installPlugin(app);

    await app.openPluginsModal();
    const card = app.page.locator(`[data-testid="plugin-card-${pluginId}"]`);
    await card.locator('[data-action="toggle-expand"]').click();

    const checkbox = card.locator('[data-setting-key="auto_upload"][data-setting-type="checkbox"]');
    await expect(checkbox).toBeVisible({ timeout: 5000 });

    await checkbox.check();
    await expect.poll(async () => app.getPluginStorage(pluginId!, 'auto_upload'), { timeout: 5000 }).toBe('true');

    await checkbox.uncheck();
    await expect.poll(async () => app.getPluginStorage(pluginId!, 'auto_upload'), { timeout: 5000 }).toBe('false');
  });

  test('group search receives URL-encoded query and bearer header only when token is set', async ({ app }) => {
    await installPlugin(app, { server_url: baseURL, api_token: 'tok-123' });

    const rows = await app.page.evaluate(async ({ pid }) => {
      // @ts-ignore - Wails runtime
      return await window.go.main.PluginService.SearchPluginOptions(pid, 'groups', 'a&b');
    }, { pid: pluginId });

    expect(rows).toEqual([
      { value: '11', label: 'Trips' },
      { value: '12', label: 'Work' },
    ]);

    expect(recorded.length).toBe(1);
    expect(recorded[0].url).toBe('/v1/groups?Name=a%26b&page=1');
    expect(recorded[0].headers['authorization']).toBe('Bearer tok-123');
  });

  test('group search sends no auth header when no token is set', async ({ app }) => {
    await installPlugin(app, { server_url: baseURL, api_token: '' });

    await app.page.evaluate(async ({ pid }) => {
      // @ts-ignore - Wails runtime
      await window.go.main.PluginService.SearchPluginOptions(pid, 'groups', 'Trips');
    }, { pid: pluginId });

    expect(recorded.length).toBe(1);
    expect(recorded[0].headers['authorization']).toBeUndefined();
  });

  test('upload posts multipart with resource, ownerId and Accept header (setting default)', async ({ app }) => {
    await installPlugin(app, { server_url: baseURL, owner_id: '11' });

    const imagePath = await createTempFile(generateTestImage(20, 20), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);
    const clips = await app.getClipCount();
    expect(clips).toBe(1);

    const clipId = await app.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const list = await window.go.main.App.GetClips(false, [], [], '', '');
      return list[0].id;
    });

    const result = await app.executePluginActionViaAPI(pluginId!, 'upload', [clipId], {});
    expect(result.success).toBe(true);

    await expect.poll(() => recorded.filter(r => r.url.startsWith('/v1/resource')).length, { timeout: 5000 }).toBe(1);
    const upload = recorded.find(r => r.url.startsWith('/v1/resource'))!;
    expect(upload.headers['accept']).toBe('application/json');
    expect(upload.headers['content-type']).toContain('multipart/form-data');
    expect(upload.body).toContain('name="resource"');
    expect(upload.body).toContain('name="ownerId"');
    expect(upload.body).toContain('11');
  });

  test('owner precedence: dialog option beats setting, setting beats omitted', async ({ app }) => {
    await installPlugin(app, { server_url: baseURL, owner_id: '11' });

    const imagePath = await createTempFile(generateTestImage(20, 20), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);
    const clipId = await app.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const list = await window.go.main.App.GetClips(false, [], [], '', '');
      return list[0].id;
    });

    // Dialog option overrides the setting.
    const viaOption = await app.executePluginActionViaAPI(pluginId!, 'upload', [clipId], { owner_id: '12' });
    expect(viaOption.success).toBe(true);
    await expect.poll(() => recorded.filter(r => r.url.startsWith('/v1/resource')).length, { timeout: 5000 }).toBe(1);
    let upload = recorded.find(r => r.url.startsWith('/v1/resource'))!;
    expect(upload.body).toContain('12');
    expect(upload.body).not.toContain('11');

    // Omitted option falls back to the setting.
    recorded = [];
    const viaSetting = await app.executePluginActionViaAPI(pluginId!, 'upload', [clipId], {});
    expect(viaSetting.success).toBe(true);
    await expect.poll(() => recorded.filter(r => r.url.startsWith('/v1/resource')).length, { timeout: 5000 }).toBe(1);
    upload = recorded.find(r => r.url.startsWith('/v1/resource'))!;
    expect(upload.body).toContain('name="ownerId"');
    expect(upload.body).toContain('11');
  });

  test('upload without any owner omits the ownerId field', async ({ app }) => {
    await installPlugin(app, { server_url: baseURL, owner_id: '' });

    const textPath = await createTempFile(generateTestText('ownerless'), 'txt');
    await app.uploadFile(textPath);
    await app.expectClipCount(1);
    const clipId = await app.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const list = await window.go.main.App.GetClips(false, [], [], '', '');
      return list[0].id;
    });

    const result = await app.executePluginActionViaAPI(pluginId!, 'upload', [clipId], {});
    expect(result.success).toBe(true);
    await expect.poll(() => recorded.filter(r => r.url.startsWith('/v1/resource')).length, { timeout: 5000 }).toBe(1);
    const upload = recorded.find(r => r.url.startsWith('/v1/resource'))!;
    expect(upload.body).not.toContain('name="ownerId"');
  });

  test('401 and 403 produce different messages', async ({ app }) => {
    await installPlugin(app, { server_url: baseURL });

    const imagePath = await createTempFile(generateTestImage(20, 20), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);
    const clipId = await app.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const list = await window.go.main.App.GetClips(false, [], [], '', '');
      return list[0].id;
    });

    // No token -> 401: missing or invalid token message. The upload action is
    // async, so the failure surfaces as an error toast rather than a return
    // value; the toast text is the plugin's mapped message.
    const toast = app.page.locator('#toast');
    await app.executePluginActionViaAPI(pluginId!, 'upload', [clipId], {});
    await expect(toast).toBeVisible({ timeout: 10000 });
    const unauthMessage = await toast.textContent();
    expect(unauthMessage).toContain('401');
    expect(unauthMessage).toContain('token');

    // Token the server rejects -> 403: valid token, cannot write here.
    await app.setPluginStorage(pluginId!, 'api_token', 'rejected');
    recorded = [];
    await app.executePluginActionViaAPI(pluginId!, 'upload', [clipId], {});
    await expect(toast).toBeVisible({ timeout: 10000 });
    const forbiddenMessage = await toast.textContent();
    expect(forbiddenMessage).toContain('403');
    expect(forbiddenMessage).toContain('group');
    expect(forbiddenMessage).not.toBe(unauthMessage);
  });
});
