import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const TEST_PLUGINS_DIR = path.resolve(__dirname, '../../test-plugins');

// Plugin cards and watch-folder rows were keyboard dead ends: arrows moved
// between rows, but every control inside a row was out of the tab order and
// Enter on a row did nothing.
test.describe('Rows whose controls have no shortcuts', () => {
  test('a plugin card expands with Enter and Tab reaches its controls', async ({ app }) => {
    await app.deleteAllPlugins();
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'settings-test.lua'));
    expect(plugin).not.toBeNull();
    try {
      await app.openPluginsModal();
      const card = app.page.locator(selectors.plugins.pluginCard(plugin!.id));
      await card.focus();
      await expect(card).toHaveAttribute('aria-expanded', 'false');

      await app.page.keyboard.press('Enter');
      await expect(card).toHaveAttribute('aria-expanded', 'true');
      await expect(card.locator(selectors.pluginSettings.section)).toBeVisible({ timeout: 5000 });
      // The list was rebuilt; the same card still has the keyboard.
      await expect(card).toBeFocused();

      // Tab walks the card's own controls: the enable toggle first.
      await app.page.keyboard.press('Tab');
      await expect(card.locator('[data-action="toggle-enable"] input')).toBeFocused();
      // And on into the settings that loaded after the card was drawn.
      const reached = await app.page.evaluate(async (testid) => {
        const cardEl = document.querySelector(`[data-testid="${testid}"]`)!;
        const field = cardEl.querySelector('[data-settings-section] input, [data-settings-section] select, [data-testid^="plugin-setting"] input') as HTMLElement | null;
        return field ? field.tabIndex : null;
      }, `plugin-card-${plugin!.id}`);
      expect(reached).not.toBe(-1);

      await card.focus();
      await app.page.keyboard.press('Enter');
      await expect(card).toHaveAttribute('aria-expanded', 'false');
    } finally {
      await app.removePlugin(plugin!.id).catch(() => undefined);
    }
  });

  test('a watch folder opens for editing with Enter and Tab reaches Pause and Remove', async ({ app, tempDir }) => {
    await app.addWatchFolder(tempDir);
    await app.openWatchView();
    const row = app.page.locator(selectors.watch.folderCard).first();
    await expect(row).toBeVisible();

    await row.focus();
    await app.page.keyboard.press('Tab');
    await expect(row.locator('[data-action="toggle-pause"]')).toBeFocused();
    await app.page.keyboard.press('Tab');
    await expect(row.locator('[data-action="remove"]')).toBeFocused();

    await row.focus();
    await app.page.keyboard.press('Enter');
    await expect(app.page.locator('#folder-modal')).toBeVisible();
  });
});
