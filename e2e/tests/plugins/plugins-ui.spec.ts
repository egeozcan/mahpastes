import { test, expect } from '../../fixtures/test-fixtures.js';
import * as path from 'path';
import * as fs from 'fs/promises';

test.describe('Plugins UI', () => {
  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
  });

  test.describe('Modal Open/Close', () => {
    test('should open plugins modal', async ({ app }) => {
      await app.openPluginsModal();
      expect(await app.isPluginsModalOpen()).toBe(true);
    });

    test('should close plugins modal via close button', async ({ app }) => {
      await app.openPluginsModal();
      expect(await app.isPluginsModalOpen()).toBe(true);

      await app.closePluginsModal();
      expect(await app.isPluginsModalOpen()).toBe(false);
    });

    test('should close plugins modal by clicking backdrop', async ({ app, page }) => {
      await app.openPluginsModal();
      expect(await app.isPluginsModalOpen()).toBe(true);

      // Click on the backdrop (the modal container itself)
      await page.locator('[data-testid="plugins-modal"]').click({ position: { x: 10, y: 10 } });
      await page.waitForSelector('[data-testid="plugins-modal"].opacity-0', { timeout: 5000 });
      expect(await app.isPluginsModalOpen()).toBe(false);
    });
  });

  test.describe('Empty State', () => {
    test('should show empty state when no plugins installed', async ({ app }) => {
      await app.openPluginsModal();
      await app.expectPluginsEmptyState();
    });

    test('should show zero plugin cards in empty state', async ({ app }) => {
      await app.openPluginsModal();
      const count = await app.getPluginCardCount();
      expect(count).toBe(0);
    });
  });

  test.describe('Plugin List Display', () => {
    test('should display imported plugin in list', async ({ app, tempDir }) => {
      const pluginSource = `
Plugin = {
  name = "Test UI Plugin",
  version = "1.0.0",
  description = "A test plugin for UI",
  author = "Test Author",
  events = {"app:startup"},
}

function on_startup()
  log("Test plugin started")
end
`;
      const pluginPath = path.join(tempDir, 'test-ui-plugin.lua');
      await fs.writeFile(pluginPath, pluginSource);

      const result = await app.importPluginFromPath(pluginPath);
      expect(result).not.toBeNull();

      await app.openPluginsModal();

      // Should show plugin card, not empty state
      const cardCount = await app.getPluginCardCount();
      expect(cardCount).toBe(1);
      await app.expectPluginInList('Test UI Plugin');
    });

    test('should display multiple plugins', async ({ app, tempDir }) => {
      // Create first plugin
      const plugin1Source = `
Plugin = {
  name = "Plugin One",
  version = "1.0.0",
  events = {"app:startup"},
}
function on_startup() end
`;
      const plugin1Path = path.join(tempDir, 'plugin-one.lua');
      await fs.writeFile(plugin1Path, plugin1Source);
      await app.importPluginFromPath(plugin1Path);

      // Create second plugin
      const plugin2Source = `
Plugin = {
  name = "Plugin Two",
  version = "2.0.0",
  events = {"clip:created"},
}
function on_clip_created() end
`;
      const plugin2Path = path.join(tempDir, 'plugin-two.lua');
      await fs.writeFile(plugin2Path, plugin2Source);
      await app.importPluginFromPath(plugin2Path);

      await app.openPluginsModal();

      const cardCount = await app.getPluginCardCount();
      expect(cardCount).toBe(2);
      await app.expectPluginInList('Plugin One');
      await app.expectPluginInList('Plugin Two');
    });
  });

  test.describe('Enable/Disable Toggle', () => {
    test('should toggle plugin enabled state via UI', async ({ app, tempDir }) => {
      const pluginSource = `
Plugin = {
  name = "Toggle Test Plugin",
  version = "1.0.0",
  events = {"app:startup"},
}
function on_startup() end
`;
      const pluginPath = path.join(tempDir, 'toggle-test.lua');
      await fs.writeFile(pluginPath, pluginSource);

      const result = await app.importPluginFromPath(pluginPath);
      expect(result).not.toBeNull();
      const pluginId = result!.id;

      await app.openPluginsModal();

      // Disable via UI
      await app.togglePluginViaUI(pluginId, false);
      await app.expectPluginDisabled('Toggle Test Plugin');

      // Enable via UI
      await app.togglePluginViaUI(pluginId, true);
      await app.expectPluginEnabled('Toggle Test Plugin');
    });
  });

  test.describe('Remove Plugin', () => {
    test('should remove plugin via UI', async ({ app, tempDir }) => {
      const pluginSource = `
Plugin = {
  name = "Remove Test Plugin",
  version = "1.0.0",
  events = {"app:startup"},
}
function on_startup() end
`;
      const pluginPath = path.join(tempDir, 'remove-test.lua');
      await fs.writeFile(pluginPath, pluginSource);

      const result = await app.importPluginFromPath(pluginPath);
      expect(result).not.toBeNull();
      const pluginId = result!.id;

      await app.openPluginsModal();
      expect(await app.getPluginCardCount()).toBe(1);

      // Remove via UI
      await app.removePluginViaUI(pluginId);

      // Should show empty state
      await app.expectPluginsEmptyState();
      expect(await app.getPluginCardCount()).toBe(0);
    });
  });

  test.describe('Plugin Details', () => {
    test('should expand plugin card to show details', async ({ app, tempDir, page }) => {
      const pluginSource = `
Plugin = {
  name = "Details Test Plugin",
  version = "1.2.3",
  description = "This is a test description",
  author = "Test Author",
  events = {"app:startup", "clip:created"},
}
function on_startup() end
function on_clip_created() end
`;
      const pluginPath = path.join(tempDir, 'details-test.lua');
      await fs.writeFile(pluginPath, pluginSource);

      const result = await app.importPluginFromPath(pluginPath);
      expect(result).not.toBeNull();

      await app.openPluginsModal();

      // Click to expand
      const card = page.locator(`[data-testid="plugin-card-${result!.id}"]`);
      await card.locator('[data-action="toggle-expand"]').click();

      // Should show description
      await expect(card.locator('text=This is a test description')).toBeVisible();

      // Should show events
      await expect(card.locator('text=app:startup')).toBeVisible();
      await expect(card.locator('text=clip:created')).toBeVisible();
    });
  });
});
