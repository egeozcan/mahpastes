import { test, expect } from '../../fixtures/test-fixtures.js';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const TEST_PLUGINS_DIR = path.resolve(__dirname, '../../test-plugins');

test.describe('Plugin Settings', () => {
  let settingsPluginId: number | null = null;

  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    settingsPluginId = null;
  });

  test.afterEach(async ({ app }) => {
    if (settingsPluginId) {
      try {
        await app.removePlugin(settingsPluginId);
      } catch {}
    }
  });

  test('settings section renders when plugin has settings', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'settings-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    settingsPluginId = plugin?.id ?? null;

    // Open plugins modal and expand plugin card
    await app.openPluginsModal();
    const card = app.page.locator(`[data-testid="plugin-card-${plugin!.id}"]`);
    await card.locator('[data-action="toggle-expand"]').click();
    await app.page.waitForTimeout(500);

    // Check settings section exists
    const settingsSection = card.locator('[data-settings-section]');
    await expect(settingsSection).toBeVisible();

    // Check all 4 settings fields are present
    await expect(card.locator('[data-setting-key="api_key"]')).toBeVisible();
    await expect(card.locator('[data-setting-key="endpoint"]')).toBeVisible();
    await expect(card.locator('[data-setting-key="enabled"]')).toBeVisible();
    await expect(card.locator('[data-setting-key="mode"]')).toBeVisible();
  });

  test('text input saves value to storage', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'settings-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    await app.openPluginsModal();
    const card = app.page.locator(`[data-testid="plugin-card-${plugin!.id}"]`);
    await card.locator('[data-action="toggle-expand"]').click();
    await app.page.waitForTimeout(500);

    // Fill in text input
    const endpointInput = card.locator('[data-setting-key="endpoint"]');
    await endpointInput.fill('https://custom.api.com');
    await app.page.waitForTimeout(500); // Wait for debounce

    // Verify storage was updated
    const value = await app.getPluginStorage(plugin!.id, 'endpoint');
    expect(value).toBe('https://custom.api.com');
  });

  test('checkbox saves value to storage', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'settings-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    await app.openPluginsModal();
    const card = app.page.locator(`[data-testid="plugin-card-${plugin!.id}"]`);
    await card.locator('[data-action="toggle-expand"]').click();
    await app.page.waitForTimeout(500);

    // Uncheck the checkbox (default is true)
    const checkbox = card.locator('[data-setting-key="enabled"]');
    await checkbox.uncheck();
    await app.page.waitForTimeout(300);

    // Verify storage was updated
    const value = await app.getPluginStorage(plugin!.id, 'enabled');
    expect(value).toBe('false');
  });

  test('select saves value to storage', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'settings-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    await app.openPluginsModal();
    const card = app.page.locator(`[data-testid="plugin-card-${plugin!.id}"]`);
    await card.locator('[data-action="toggle-expand"]').click();
    await app.page.waitForTimeout(500);

    // Change select value
    const select = card.locator('[data-setting-key="mode"]');
    await select.selectOption('thorough');
    await app.page.waitForTimeout(300);

    // Verify storage was updated
    const value = await app.getPluginStorage(plugin!.id, 'mode');
    expect(value).toBe('thorough');
  });

  test('password input saves value to storage', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'settings-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    await app.openPluginsModal();
    const card = app.page.locator(`[data-testid="plugin-card-${plugin!.id}"]`);
    await card.locator('[data-action="toggle-expand"]').click();
    await app.page.waitForTimeout(500);

    // Fill in password input
    const apiKeyInput = card.locator('[data-setting-key="api_key"]');
    await apiKeyInput.fill('secret-api-key-123');
    await app.page.waitForTimeout(500); // Wait for debounce

    // Verify storage was updated
    const value = await app.getPluginStorage(plugin!.id, 'api_key');
    expect(value).toBe('secret-api-key-123');
  });

  test('settings persist after closing and reopening modal', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'settings-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    // Open modal and set a value
    await app.openPluginsModal();
    let card = app.page.locator(`[data-testid="plugin-card-${plugin!.id}"]`);
    await card.locator('[data-action="toggle-expand"]').click();
    await app.page.waitForTimeout(500);

    const endpointInput = card.locator('[data-setting-key="endpoint"]');
    await endpointInput.fill('https://persistent.api.com');
    await app.page.waitForTimeout(500);

    // Close and reopen modal
    await app.closePluginsModal();
    await app.page.waitForTimeout(300);
    await app.openPluginsModal();

    // Expand plugin card again
    card = app.page.locator(`[data-testid="plugin-card-${plugin!.id}"]`);
    await card.locator('[data-action="toggle-expand"]').click();
    await app.page.waitForTimeout(500);

    // Verify the value persisted
    const newEndpointInput = card.locator('[data-setting-key="endpoint"]');
    await expect(newEndpointInput).toHaveValue('https://persistent.api.com');
  });

  test('settings section not shown for plugin without settings', async ({ app }) => {
    // Use existing event-tracker plugin which has no settings
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'event-tracker.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    await app.openPluginsModal();
    const card = app.page.locator(`[data-testid="plugin-card-${plugin!.id}"]`);
    await card.locator('[data-action="toggle-expand"]').click();
    await app.page.waitForTimeout(500);

    // Settings section should not exist
    const settingsSection = card.locator('[data-settings-section]');
    await expect(settingsSection).not.toBeVisible();
  });
});
