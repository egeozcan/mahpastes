import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
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
    const card = app.page.locator(selectors.plugins.pluginCard(plugin!.id));
    await card.locator(selectors.plugins.expandToggle).click();

    // Check settings section exists
    const settingsSection = card.locator(selectors.pluginSettings.section);
    await expect(settingsSection).toBeVisible({ timeout: 5000 });

    // Check all 4 settings fields are present
    await expect(card.locator(selectors.pluginSettings.settingField('api_key'))).toBeVisible();
    await expect(card.locator(selectors.pluginSettings.settingField('endpoint'))).toBeVisible();
    await expect(card.locator(selectors.pluginSettings.settingField('enabled'))).toBeVisible();
    await expect(card.locator(selectors.pluginSettings.settingField('mode'))).toBeVisible();
  });

  test('text input saves value to storage', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'settings-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    await app.openPluginsModal();
    const card = app.page.locator(selectors.plugins.pluginCard(plugin!.id));
    await card.locator(selectors.plugins.expandToggle).click();

    // Wait for settings section to be visible after expand
    const settingsSection = card.locator(selectors.pluginSettings.section);
    await expect(settingsSection).toBeVisible({ timeout: 5000 });

    // Fill in text input
    const endpointInput = card.locator(selectors.pluginSettings.settingField('endpoint'));
    await endpointInput.fill('https://custom.api.com');

    // Wait for debounce to save value to storage
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'endpoint'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('https://custom.api.com');
  });

  test('checkbox saves value to storage', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'settings-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    await app.openPluginsModal();
    const card = app.page.locator(selectors.plugins.pluginCard(plugin!.id));
    await card.locator(selectors.plugins.expandToggle).click();

    // Wait for settings section to be visible after expand
    const settingsSection = card.locator(selectors.pluginSettings.section);
    await expect(settingsSection).toBeVisible({ timeout: 5000 });

    // Uncheck the checkbox (default is true)
    const checkbox = card.locator(selectors.pluginSettings.settingField('enabled'));
    await checkbox.uncheck();

    // Wait for storage to reflect the unchecked state
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'enabled'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('false');
  });

  test('select saves value to storage', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'settings-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    await app.openPluginsModal();
    const card = app.page.locator(selectors.plugins.pluginCard(plugin!.id));
    await card.locator(selectors.plugins.expandToggle).click();

    // Wait for settings section to be visible after expand
    const settingsSection = card.locator(selectors.pluginSettings.section);
    await expect(settingsSection).toBeVisible({ timeout: 5000 });

    // Change select value
    const select = card.locator(selectors.pluginSettings.settingField('mode'));
    await select.selectOption('thorough');

    // Wait for storage to reflect the selected value
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'mode'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('thorough');
  });

  test('password input saves value to storage', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'settings-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    await app.openPluginsModal();
    const card = app.page.locator(selectors.plugins.pluginCard(plugin!.id));
    await card.locator(selectors.plugins.expandToggle).click();

    // Wait for settings section to be visible after expand
    const settingsSection = card.locator(selectors.pluginSettings.section);
    await expect(settingsSection).toBeVisible({ timeout: 5000 });

    // Fill in password input
    const apiKeyInput = card.locator(selectors.pluginSettings.settingField('api_key'));
    await apiKeyInput.fill('secret-api-key-123');

    // Wait for debounce to save value to storage
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'api_key'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('secret-api-key-123');
  });

  test('settings persist after closing and reopening modal', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'settings-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    // Open modal and set a value
    await app.openPluginsModal();
    let card = app.page.locator(selectors.plugins.pluginCard(plugin!.id));
    await card.locator(selectors.plugins.expandToggle).click();

    // Wait for settings section to be visible after expand
    let settingsSection = card.locator(selectors.pluginSettings.section);
    await expect(settingsSection).toBeVisible({ timeout: 5000 });

    const endpointInput = card.locator(selectors.pluginSettings.settingField('endpoint'));
    await endpointInput.fill('https://persistent.api.com');

    // Wait for debounce to save value to storage
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'endpoint'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('https://persistent.api.com');

    // Close and reopen modal
    await app.closePluginsModal();
    await app.openPluginsModal();

    // Expand plugin card again
    card = app.page.locator(selectors.plugins.pluginCard(plugin!.id));
    await card.locator(selectors.plugins.expandToggle).click();

    // Wait for settings section to be visible after expand
    settingsSection = card.locator(selectors.pluginSettings.section);
    await expect(settingsSection).toBeVisible({ timeout: 5000 });

    // Verify the value persisted
    const newEndpointInput = card.locator(selectors.pluginSettings.settingField('endpoint'));
    await expect(newEndpointInput).toHaveValue('https://persistent.api.com');
  });

  test('settings section not shown for plugin without settings', async ({ app }) => {
    // Use existing event-tracker plugin which has no settings
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'event-tracker.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    await app.openPluginsModal();
    const card = app.page.locator(selectors.plugins.pluginCard(plugin!.id));
    await card.locator(selectors.plugins.expandToggle).click();

    // Wait for the expanded card content to be visible (e.g., description or events section)
    // Since there are no settings, we wait for the card to finish expanding by checking
    // that the expand toggle has the expected state, then verify settings section is absent
    await expect(card.locator(selectors.plugins.expandToggle)).toBeVisible({ timeout: 5000 });

    // Settings section should not exist
    const settingsSection = card.locator(selectors.pluginSettings.section);
    await expect(settingsSection).not.toBeVisible();
  });
});
