import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const TEST_PLUGIN_PATH = path.resolve(__dirname, '../../fixtures/test-plugin.lua');

test.describe('Plugin Global Actions', () => {
  test('should not show plugin section in drawer when no plugins installed', async ({ app }) => {
    await app.expectDrawerPluginActionsHidden();
  });

  test('should show global actions in drawer after plugin install', async ({ app }) => {
    const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    await app.enablePlugin(plugin!.id);

    // Reload to refresh plugin UI actions
    await app.page.reload();
    await app.waitForReady();

    await app.expectDrawerPluginActionsVisible();
    await app.expectDrawerPluginActionsCount(3); // simple, options, async
  });

  test('should not show global actions when plugin disabled', async ({ app }) => {
    const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    await app.disablePlugin(plugin!.id);

    await app.page.reload();
    await app.waitForReady();

    await app.expectDrawerPluginActionsHidden();
  });

  test('should execute simple global action and create clip', async ({ app }) => {
    const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    await app.enablePlugin(plugin!.id);

    await app.page.reload();
    await app.waitForReady();

    await app.clickDrawerPluginAction(plugin!.id, 'test_global_simple');

    // Should create a new clip
    await app.expectClipCount(1);
    await app.expectClipVisible('global_simple_result.txt');
  });

  test('should show options dialog for global action with options', async ({ app }) => {
    const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    await app.enablePlugin(plugin!.id);

    await app.page.reload();
    await app.waitForReady();

    await app.clickDrawerPluginAction(plugin!.id, 'test_global_options');

    // Options dialog should open
    const isOpen = await app.isPluginOptionsModalOpen();
    expect(isOpen).toBe(true);

    // Title should show action name only (no clip count)
    const title = await app.page.locator('#plugin-options-title').textContent();
    expect(title).toBe('Test Global Options');
  });

  test('should execute global action with options and create clip', async ({ app }) => {
    const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    await app.enablePlugin(plugin!.id);

    await app.page.reload();
    await app.waitForReady();

    await app.clickDrawerPluginAction(plugin!.id, 'test_global_options');

    // Fill in options and submit
    await app.fillPluginOptionsForm({ name: 'my_creation' });
    await app.submitPluginOptionsForm();

    // Should create a new clip with the provided name
    await app.expectClipCount(1);
    await app.expectClipVisible('my_creation.txt');
  });

  test('should include global_actions in API response', async ({ app }) => {
    const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    await app.enablePlugin(plugin!.id);

    const actions = await app.getPluginUIActions();
    expect(actions.global_actions).toBeDefined();
    expect(actions.global_actions.length).toBe(3);

    const simpleAction = actions.global_actions.find((a: any) => a.id === 'test_global_simple');
    expect(simpleAction).toBeDefined();
    expect(simpleAction.plugin_id).toBe(plugin!.id);
    expect(simpleAction.label).toBe('Test Global Simple');
  });
});
