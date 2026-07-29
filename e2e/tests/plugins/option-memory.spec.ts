import { test, expect } from '../../fixtures/test-fixtures.js';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const TEST_PLUGIN_PATH = path.resolve(__dirname, '../../fixtures/test-plugin.lua');

// The options dialog remembers the last submitted select/checkbox/range values
// per plugin action, so repeat runs (e.g. the fal.ai model pickers) reopen with
// the previous choice instead of the manifest default.
test.describe('Plugin Option Memory', () => {
  async function installAndReload(app: any) {
    const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    await app.enablePlugin(plugin!.id);
    await app.page.reload();
    await app.waitForReady();
    return plugin!;
  }

  const fieldValue = (app: any, id: string) =>
    app.page.locator(`#plugin-options-form [name="${id}"]`).inputValue();

  const fieldChecked = (app: any, id: string) =>
    app.page.locator(`#plugin-options-form [name="${id}"]`).isChecked();

  test('should open with manifest defaults on first use', async ({ app }) => {
    const plugin = await installAndReload(app);

    await app.clickDrawerPluginAction(plugin.id, 'test_global_options');
    await expect.poll(() => app.isPluginOptionsModalOpen(), { timeout: 5000 }).toBe(true);

    expect(await fieldValue(app, 'flavor')).toBe('plain');
    expect(await fieldChecked(app, 'loud')).toBe(false);
  });

  test('should restore last chosen select and checkbox values', async ({ app }) => {
    const plugin = await installAndReload(app);

    await app.clickDrawerPluginAction(plugin.id, 'test_global_options');
    await app.fillPluginOptionsForm({ name: 'first', flavor: 'fancy', loud: true });
    await app.submitPluginOptionsForm();
    await app.expectClipCount(1);

    await app.clickDrawerPluginAction(plugin.id, 'test_global_options');
    await expect.poll(() => app.isPluginOptionsModalOpen(), { timeout: 5000 }).toBe(true);

    expect(await fieldValue(app, 'flavor')).toBe('fancy');
    expect(await fieldChecked(app, 'loud')).toBe(true);
  });

  test('should not remember free-text fields', async ({ app }) => {
    const plugin = await installAndReload(app);

    await app.clickDrawerPluginAction(plugin.id, 'test_global_options');
    await app.fillPluginOptionsForm({ name: 'first', flavor: 'fancy' });
    await app.submitPluginOptionsForm();
    await app.expectClipCount(1);

    await app.clickDrawerPluginAction(plugin.id, 'test_global_options');
    await expect.poll(() => app.isPluginOptionsModalOpen(), { timeout: 5000 }).toBe(true);

    expect(await fieldValue(app, 'name')).toBe('test');
  });

  test('should persist remembered values across a reload', async ({ app }) => {
    const plugin = await installAndReload(app);

    await app.clickDrawerPluginAction(plugin.id, 'test_global_options');
    await app.fillPluginOptionsForm({ name: 'first', flavor: 'fancy', loud: true });
    await app.submitPluginOptionsForm();
    await app.expectClipCount(1);

    await app.page.reload();
    await app.waitForReady();

    await app.clickDrawerPluginAction(plugin.id, 'test_global_options');
    await expect.poll(() => app.isPluginOptionsModalOpen(), { timeout: 5000 }).toBe(true);

    expect(await fieldValue(app, 'flavor')).toBe('fancy');
    expect(await fieldChecked(app, 'loud')).toBe(true);
  });
});
