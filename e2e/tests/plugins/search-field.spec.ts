import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const PLUGIN_PATH = path.resolve(__dirname, '../../test-plugins/search-field-test.lua');

// The search field type: an async combobox over the plugin's on_search hook,
// rendered in the plugin settings panel and in the action options dialog.
test.describe('Plugin Search Field', () => {
  let pluginId: number | null = null;

  async function installAndOpenSettings(app: any) {
    const plugin = await app.importPluginFromPath(PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    pluginId = plugin!.id;
    await app.enablePlugin(plugin!.id);
    await app.page.reload();
    await app.waitForReady();

    await app.openPluginsModal();
    const card = app.page.locator(selectors.plugins.pluginCard(plugin!.id));
    await card.locator(selectors.plugins.expandToggle).click();
    await expect(card.locator(selectors.pluginSettings.section)).toBeVisible({ timeout: 5000 });
    return { card, input: card.locator(selectors.pluginSearch.settingInput('entity')) };
  }


  // An option's accessible name is "<label> <value>"; matching on both avoids
  // substring collisions (e.g. Alpha vs Alphabet).
  const row = (scope: any, label: string, value: string) =>
    scope.getByRole('option', { name: `${label} ${value}` });

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

  test('picker renders in settings and lists rows from on_search', async ({ app }) => {
    const { card, input } = await installAndOpenSettings(app);

    await input.fill('al');
    const dropdown = card.locator(selectors.pluginSearch.dropdown);
    await expect(dropdown).toBeVisible({ timeout: 5000 });
    // 'al' matches Alpha and Alphabet, not Beta/Gamma.
    await expect(row(dropdown, 'Alpha', '1')).toBeVisible();
    await expect(row(dropdown, 'Alphabet', '12')).toBeVisible();
    await expect(row(dropdown, 'Beta', '2')).toHaveCount(0);
  });

  test('selecting writes only the id to plugin storage while the label round-trips', async ({ app }) => {
    const { card, input } = await installAndOpenSettings(app);

    await input.fill('al');
    const dropdown = card.locator(selectors.pluginSearch.dropdown);
    await expect(dropdown).toBeVisible({ timeout: 5000 });
    await row(dropdown, 'Alpha', '1').click();

    // Only the id goes to plugin storage — no __label twin.
    await expect.poll(async () => app.getPluginStorage(pluginId!, 'entity'), { timeout: 5000 }).toBe('1');
    expect(await app.getPluginStorage(pluginId!, 'entity__label')).toBe('');

    // The visible input shows the label.
    await expect(input).toHaveValue('Alpha');

    // The label persists in the plugin_setting_labels app setting.
    const labels = await app.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      return await window.go.main.App.GetSetting('plugin_setting_labels');
    });
    const parsed = JSON.parse(labels || '{}');
    expect(parsed['Search Field Test::entity']).toEqual({ value: '1', label: 'Alpha' });
  });

  test('label survives closing and reopening the plugins modal', async ({ app }) => {
    const { card, input } = await installAndOpenSettings(app);

    await input.fill('be');
    const dropdown = card.locator(selectors.pluginSearch.dropdown);
    await expect(dropdown).toBeVisible({ timeout: 5000 });
    await row(dropdown, 'Beta', '2').click();
    await expect.poll(async () => app.getPluginStorage(pluginId!, 'entity'), { timeout: 5000 }).toBe('2');

    await app.closePluginsModal();
    await app.openPluginsModal();
    const reopenedCard = app.page.locator(selectors.plugins.pluginCard(pluginId!));
    await reopenedCard.locator(selectors.plugins.expandToggle).click();
    const reopenedInput = reopenedCard.locator(selectors.pluginSearch.settingInput('entity'));
    await expect(reopenedInput).toBeVisible({ timeout: 5000 });
    await expect(reopenedInput).toHaveValue('Beta');
  });

  test('a value the plugin rewrites behind the panel displays as the raw id', async ({ app }) => {
    const { card, input } = await installAndOpenSettings(app);

    await input.fill('al');
    const dropdown = card.locator(selectors.pluginSearch.dropdown);
    await expect(dropdown).toBeVisible({ timeout: 5000 });
    await row(dropdown, 'Alpha', '1').click();
    await expect.poll(async () => app.getPluginStorage(pluginId!, 'entity'), { timeout: 5000 }).toBe('1');

    // The plugin rewrites the stored value behind the panel's back.
    await app.page.evaluate(async ({ id }) => {
      // @ts-ignore - Wails runtime
      await window.go.main.PluginService.SetPluginStorage(id, 'entity', '3');
    }, { id: pluginId });

    // Collapse and re-expand to force a fresh render from storage.
    const freshCard = app.page.locator(selectors.plugins.pluginCard(pluginId!));
    await freshCard.locator(selectors.plugins.expandToggle).click();
    await freshCard.locator(selectors.plugins.expandToggle).click();
    const freshInput = freshCard.locator(selectors.pluginSearch.settingInput('entity'));
    await expect(freshInput).toBeVisible({ timeout: 5000 });
    // The remembered label ("Alpha") must NOT be shown over the new value.
    await expect(freshInput).toHaveValue('3');
  });

  test('the clear button empties the stored value', async ({ app }) => {
    const { card, input } = await installAndOpenSettings(app);

    await input.fill('al');
    const dropdown = card.locator(selectors.pluginSearch.dropdown);
    await expect(dropdown).toBeVisible({ timeout: 5000 });
    await row(dropdown, 'Alpha', '1').click();
    await expect.poll(async () => app.getPluginStorage(pluginId!, 'entity'), { timeout: 5000 }).toBe('1');

    await card.locator(selectors.pluginSearch.clearButton).click();
    await expect.poll(async () => app.getPluginStorage(pluginId!, 'entity'), { timeout: 5000 }).toBe('');
    await expect(input).toHaveValue('');
  });

  test('the action dialog picker delivers the chosen value into on_ui_action options', async ({ app }) => {
    const plugin = await app.importPluginFromPath(PLUGIN_PATH);
    pluginId = plugin!.id;
    await app.enablePlugin(plugin!.id);
    await app.page.reload();
    await app.waitForReady();

    await app.clickDrawerPluginAction(plugin!.id, 'pick');
    await expect.poll(() => app.isPluginOptionsModalOpen(), { timeout: 5000 }).toBe(true);

    // The options dialog search field: visible input + hidden value input.
    const visible = app.page.locator('#plugin-options-form [id="plugin-opt-entity"]');
    await visible.fill('gam');
    const dropdown = app.page.locator('#plugin-options-form [role="listbox"]');
    await expect(dropdown).toBeVisible({ timeout: 5000 });
    await row(dropdown, 'Gamma', '3').click();

    const hidden = app.page.locator('#plugin-options-form [name="entity"]');
    await expect(hidden).toHaveValue('3');

    await app.submitPluginOptionsForm();

    // The plugin echoes options.entity into the result modal.
    await expect(app.page.locator(selectors.pluginResultModal.modal)).toHaveClass(/opacity-100/, { timeout: 10000 });
    await expect(app.page.locator('#plugin-result-body')).toContainText('entity=3');
    await app.page.locator(selectors.pluginResultModal.closeButton).click();
  });

  test('a search while the sandbox is busy reports busy instead of hanging', async ({ app }) => {
    const plugin = await app.importPluginFromPath(PLUGIN_PATH);
    pluginId = plugin!.id;
    await app.enablePlugin(plugin!.id);
    await app.page.reload();
    await app.waitForReady();

    await app.openPluginsModal();
    const card = app.page.locator(selectors.plugins.pluginCard(pluginId!));
    await card.locator(selectors.plugins.expandToggle).click();
    const input = card.locator(selectors.pluginSearch.settingInput('entity'));
    await expect(input).toBeVisible({ timeout: 5000 });

    // Fire the async action that holds the sandbox for ~6 seconds.
    await app.executePluginActionViaAPI(pluginId!, 'slow', [], {});
    await app.page.waitForTimeout(1500); // let the goroutine take the sandbox lock

    await input.fill('al');
    const dropdown = card.locator(selectors.pluginSearch.dropdown);
    await expect(dropdown).toBeVisible({ timeout: 5000 });
    await expect(dropdown.locator(':scope > div', { hasText: 'Plugin is busy' })).toBeVisible({ timeout: 5000 });
  });
});
