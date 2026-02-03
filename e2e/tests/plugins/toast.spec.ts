import { test, expect } from '../../fixtures/test-fixtures';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const TEST_PLUGINS_DIR = path.resolve(__dirname, '../../test-plugins');

test.describe('Plugin Toast', () => {
  test('showToast displays info type with default styling', async ({ app }) => {
    // Call showToast directly via evaluate
    await app.page.evaluate(() => {
      // @ts-ignore
      showToast('Test info message', 'info');
    });

    const toast = app.page.locator('#toast');
    await expect(toast).toBeVisible();
    await expect(toast).toHaveText('Test info message');
    // Check it has stone-800 background (default)
    const hasInfoStyle = await toast.evaluate(el => el.classList.contains('bg-stone-800'));
    expect(hasInfoStyle).toBe(true);
  });

  test('showToast displays success type with green styling', async ({ app }) => {
    await app.page.evaluate(() => {
      // @ts-ignore
      showToast('Success message', 'success');
    });

    const toast = app.page.locator('#toast');
    await expect(toast).toBeVisible();
    const hasSuccessStyle = await toast.evaluate(el => el.classList.contains('bg-emerald-600'));
    expect(hasSuccessStyle).toBe(true);
  });

  test('showToast displays error type with red styling', async ({ app }) => {
    await app.page.evaluate(() => {
      // @ts-ignore
      showToast('Error message', 'error');
    });

    const toast = app.page.locator('#toast');
    await expect(toast).toBeVisible();
    const hasErrorStyle = await toast.evaluate(el => el.classList.contains('bg-red-600'));
    expect(hasErrorStyle).toBe(true);
  });

  test('showToast defaults to info type when no type provided', async ({ app }) => {
    await app.page.evaluate(() => {
      // @ts-ignore
      showToast('Default message');
    });

    const toast = app.page.locator('#toast');
    const hasInfoStyle = await toast.evaluate(el => el.classList.contains('bg-stone-800'));
    expect(hasInfoStyle).toBe(true);
  });
});

test.describe('Plugin Toast API', () => {
  test('frontend receives plugin:toast events from backend', async ({ app }) => {
    // Test that the frontend event listener is correctly wired up
    // by directly emitting an event from the page context
    await app.page.evaluate(() => {
      // Simulate what the backend would do - emit a plugin:toast event
      if (window.runtime && window.runtime.EventsEmit) {
        window.runtime.EventsEmit('plugin:toast', {
          message: 'Test toast from event',
          type: 'success'
        });
      }
    });

    // The event listener should call showToast
    const toast = app.page.locator('#toast');
    await expect(toast).toBeVisible({ timeout: 5000 });
    await expect(toast).toContainText('Test toast from event');
  });

  test('plugin can trigger toast via toast.show()', async ({ app }) => {
    // Clean up any existing plugins
    await app.deleteAllPlugins();

    // Import a test plugin that calls toast.show on clip:created event
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'toast-test.lua');

    // Import the plugin
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();

    // Create a test file and upload it to trigger clip:created event
    const { createTempFile, generateTestImage } = await import('../../helpers/test-data.js');
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    // Wait for the plugin to receive the event (check storage)
    const receivedEvent = await app.waitForPluginStorage(plugin!.id, 'last_event', 'clip:created', 5000);
    expect(receivedEvent).toBe(true);

    // Verify that toast.show() was called and succeeded
    // The return value 'true' indicates the toast API worked
    // (Note: The actual toast may be overwritten by the upload success toast,
    // but this verifies the plugin->backend->frontend path works)
    const toastResult = await app.getPluginStorage(plugin!.id, 'toast_result');
    expect(toastResult).toBe('true');

    // Cleanup
    await app.removePlugin(plugin!.id);
  });
});
