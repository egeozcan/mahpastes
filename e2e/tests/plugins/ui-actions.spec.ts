import { test, expect } from '../../fixtures/test-fixtures.js';
import { createTempFile, generateTestImage } from '../../helpers/test-data.js';
import { selectors } from '../../helpers/selectors.js';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Path to the test plugin (in e2e/fixtures to avoid users accidentally enabling it)
const TEST_PLUGIN_PATH = path.resolve(__dirname, '../../fixtures/test-plugin.lua');

test.describe('Plugin UI Extensions', () => {
  test.describe('Plugin UI Actions API', () => {
    test('should return empty actions when no plugins enabled', async ({ app }) => {
      const actions = await app.getPluginUIActions();
      expect(actions.lightbox_buttons).toHaveLength(0);
      expect(actions.card_actions).toHaveLength(0);
    });

    test('should return actions from enabled plugin', async ({ app }) => {
      // Import and enable test plugin
      const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
      expect(plugin).not.toBeNull();

      await app.enablePlugin(plugin!.id);

      const actions = await app.getPluginUIActions();
      expect(actions.lightbox_buttons.length).toBeGreaterThan(0);
      expect(actions.card_actions.length).toBeGreaterThan(0);

      // Verify action structure
      const lightboxBtn = actions.lightbox_buttons[0];
      expect(lightboxBtn.plugin_id).toBe(plugin!.id);
      expect(lightboxBtn.id).toBeDefined();
      expect(lightboxBtn.label).toBeDefined();
    });

    test('should not return actions from disabled plugin', async ({ app }) => {
      // Import but do not enable test plugin
      const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
      expect(plugin).not.toBeNull();

      // Disable the plugin explicitly (plugins start enabled by default)
      await app.disablePlugin(plugin!.id);

      const actions = await app.getPluginUIActions();
      expect(actions.lightbox_buttons).toHaveLength(0);
      expect(actions.card_actions).toHaveLength(0);
    });
  });

  test.describe('Card Menu', () => {
    test('should show dropdown menu on card', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await app.openCardMenu(path.basename(imagePath));

      // Verify menu appears
      const menu = app.page.locator(selectors.cardMenu.dropdown);
      await expect(menu).toBeVisible();

      // Verify built-in actions
      await expect(app.page.locator(selectors.cardMenu.copyPath)).toBeVisible();
      await expect(app.page.locator(selectors.cardMenu.delete)).toBeVisible();
    });

    test('should close menu when clicking away', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);

      await app.openCardMenu(path.basename(imagePath));
      const menu = app.page.locator(selectors.cardMenu.dropdown);
      await expect(menu).toBeVisible();

      // Click away
      await app.closeCardMenu();

      // Menu should be closed
      await expect(menu).not.toBeVisible();
    });

    test('should show plugin actions in menu when plugin enabled', async ({ app }) => {
      // Setup plugin
      const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
      expect(plugin).not.toBeNull();
      await app.enablePlugin(plugin!.id);

      // Need to reload to refresh plugin UI actions
      await app.page.reload();
      await app.waitForReady();

      // Upload clip
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);

      // Open menu
      await app.openCardMenu(path.basename(imagePath));

      // Verify plugin actions appear
      const menu = app.page.locator(selectors.cardMenu.dropdown);
      const pluginItems = menu.locator('[data-action="plugin"]');
      await expect(pluginItems.first()).toBeVisible();

      // Should show divider before plugin actions
      const divider = menu.locator('.card-menu-divider');
      await expect(divider).toBeVisible();
    });

    test('should not show plugin actions in menu when no plugins enabled', async ({ app }) => {
      // Upload clip without plugins
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);

      // Open menu
      await app.openCardMenu(path.basename(imagePath));

      // Verify no plugin actions
      const menu = app.page.locator(selectors.cardMenu.dropdown);
      const pluginItems = menu.locator('[data-action="plugin"]');
      await expect(pluginItems).toHaveCount(0);
    });
  });

  test.describe('Lightbox Plugin Buttons', () => {
    test('should show plugin buttons in lightbox when plugin enabled', async ({ app }) => {
      // Setup plugin
      const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
      expect(plugin).not.toBeNull();
      await app.enablePlugin(plugin!.id);

      // Reload to refresh UI actions
      await app.page.reload();
      await app.waitForReady();

      // Upload and open lightbox
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.openLightbox(path.basename(imagePath));

      // Verify plugin trigger button appears
      const pluginContainer = app.page.locator(selectors.lightbox.pluginActions);
      await expect(pluginContainer).toBeVisible();

      const pluginTrigger = app.page.locator(selectors.lightbox.pluginTrigger);
      await expect(pluginTrigger).toBeVisible();
    });

    test('should hide plugin actions container when no plugins enabled', async ({ app }) => {
      // Upload and open lightbox without plugins
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.openLightbox(path.basename(imagePath));

      // Plugin container should be hidden
      const pluginContainer = app.page.locator(selectors.lightbox.pluginActions);
      await expect(pluginContainer).toHaveClass(/hidden/);
    });
  });

  test.describe('Plugin Action Execution via API', () => {
    test('should execute simple action and create new clip', async ({ app }) => {
      // Setup plugin
      const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
      expect(plugin).not.toBeNull();
      await app.enablePlugin(plugin!.id);

      // Upload clip
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Get clip ID
      const clips = await app.page.evaluate(async () => {
        // @ts-ignore
        return await window.go.main.App.GetClips(false, []);
      });
      const clipId = clips[0].id;

      // Execute action
      const result = await app.executePluginActionViaAPI(plugin!.id, 'test_simple', [clipId]);
      expect(result.success).toBe(true);
      expect(result.result_clip_id).toBeGreaterThan(0);

      // Verify new clip created
      await app.page.reload();
      await app.waitForReady();
      await app.expectClipCount(2);
    });

    test('should execute action with options', async ({ app }) => {
      // Setup plugin
      const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
      expect(plugin).not.toBeNull();
      await app.enablePlugin(plugin!.id);

      // Upload clip
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Get clip ID
      const clips = await app.page.evaluate(async () => {
        // @ts-ignore
        return await window.go.main.App.GetClips(false, []);
      });
      const clipId = clips[0].id;

      // Execute action with options
      const result = await app.executePluginActionViaAPI(plugin!.id, 'test_options', [clipId], {
        suffix: '_custom',
        uppercase: true,
      });
      expect(result.success).toBe(true);

      // Verify new clip created
      await app.page.reload();
      await app.waitForReady();
      await app.expectClipCount(2);
    });

    test('should handle bulk action on multiple clips', async ({ app }) => {
      // Setup plugin
      const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
      expect(plugin).not.toBeNull();
      await app.enablePlugin(plugin!.id);

      // Upload multiple clips
      const imagePath1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const imagePath2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      await app.uploadFiles([imagePath1, imagePath2]);
      await app.expectClipCount(2);

      // Get clip IDs
      const clips = await app.page.evaluate(async () => {
        // @ts-ignore
        return await window.go.main.App.GetClips(false, []);
      });
      const clipIds = clips.map((c: any) => c.id);

      // Execute bulk action
      const result = await app.executePluginActionViaAPI(plugin!.id, 'test_bulk', clipIds);
      expect(result.success).toBe(true);

      // Verify new clips created (one per input clip)
      await app.page.reload();
      await app.waitForReady();
      await app.expectClipCount(4);
    });

    test('should return error for invalid plugin ID', async ({ app }) => {
      const result = await app.executePluginActionViaAPI(99999, 'test_simple', [1]);
      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });

    test('should return error for invalid action ID', async ({ app }) => {
      // Setup plugin
      const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
      expect(plugin).not.toBeNull();
      await app.enablePlugin(plugin!.id);

      const result = await app.executePluginActionViaAPI(plugin!.id, 'nonexistent_action', [1]);
      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });
  });

  test.describe('Plugin Options Dialog', () => {
    test('should open options dialog for action with options', async ({ app }) => {
      // Setup plugin
      const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
      expect(plugin).not.toBeNull();
      await app.enablePlugin(plugin!.id);

      // Reload to refresh UI actions
      await app.page.reload();
      await app.waitForReady();

      // Upload clip
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);

      // Open lightbox
      await app.openLightbox(path.basename(imagePath));

      // Open plugin menu and click the action with options (test_options)
      const pluginTrigger = app.page.locator(selectors.lightbox.pluginTrigger);
      await pluginTrigger.click();
      await app.page.locator(selectors.lightbox.pluginMenu).waitFor({ state: 'visible' });

      const optionsItem = app.page.locator(
        `${selectors.lightbox.pluginMenuItem}[data-action-id="test_options"]`
      );
      await optionsItem.click();
      await expect.poll(() => app.isPluginOptionsModalOpen(), { timeout: 5000 }).toBe(true);

      // Options modal should be visible
      const isOpen = await app.isPluginOptionsModalOpen();
      expect(isOpen).toBe(true);
    });

    test('should close options dialog on cancel', async ({ app }) => {
      // Setup plugin
      const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
      expect(plugin).not.toBeNull();
      await app.enablePlugin(plugin!.id);

      // Reload to refresh UI actions
      await app.page.reload();
      await app.waitForReady();

      // Upload clip
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);

      // Open lightbox
      await app.openLightbox(path.basename(imagePath));

      // Open plugin menu and click the action with options
      const pluginTrigger = app.page.locator(selectors.lightbox.pluginTrigger);
      await pluginTrigger.click();
      await app.page.locator(selectors.lightbox.pluginMenu).waitFor({ state: 'visible' });

      const optionsItem = app.page.locator(
        `${selectors.lightbox.pluginMenuItem}[data-action-id="test_options"]`
      );
      await optionsItem.click();
      await expect.poll(() => app.isPluginOptionsModalOpen(), { timeout: 5000 }).toBe(true);

      // Cancel
      await app.cancelPluginOptionsForm();

      // Modal should be closed
      const isOpen = await app.isPluginOptionsModalOpen();
      expect(isOpen).toBe(false);
    });
  });
});
