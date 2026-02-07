import { test, expect } from '../../fixtures/test-fixtures';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Path to test plugins directory
const TEST_PLUGINS_DIR = path.resolve(__dirname, '../../test-plugins');

test.describe('Plugin Scheduler', () => {
  let schedulerPluginId: number | null = null;

  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    schedulerPluginId = null;
  });

  test.afterEach(async ({ app }) => {
    if (schedulerPluginId) {
      try {
        await app.removePlugin(schedulerPluginId);
      } catch {
        // Ignore cleanup errors
      }
    }
  });

  test('should execute scheduled task at specified interval', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'scheduler-test.lua');

    // Import plugin
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    expect(plugin?.name).toBe('Scheduler Test');
    schedulerPluginId = plugin?.id ?? null;

    // Check initial state (poll until plugin initializes)
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'tick_count'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('0');

    // Wait for first tick
    await expect.poll(
      async () => parseInt(await app.getPluginStorage(plugin!.id, 'tick_count') || '0'),
      { timeout: 5000, intervals: [200, 500, 1000], message: 'Waiting for first tick' }
    ).toBeGreaterThanOrEqual(1);

    const count1 = parseInt(await app.getPluginStorage(plugin!.id, 'tick_count'));

    // Wait for another tick
    await expect.poll(
      async () => parseInt(await app.getPluginStorage(plugin!.id, 'tick_count') || '0'),
      { timeout: 5000, intervals: [200, 500, 1000], message: 'Waiting for second tick' }
    ).toBeGreaterThan(count1);
  });

  test('should record last tick timestamp', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'scheduler-test.lua');

    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    schedulerPluginId = plugin?.id ?? null;

    // Wait for a tick
    await expect.poll(
      async () => parseInt(await app.getPluginStorage(plugin!.id, 'last_tick') || '0'),
      { timeout: 5000, intervals: [200, 500, 1000], message: 'Waiting for last_tick' }
    ).toBeGreaterThan(0);
  });

  test('should stop scheduled tasks when plugin is disabled', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'scheduler-test.lua');

    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    schedulerPluginId = plugin?.id ?? null;

    // Wait for some ticks
    await expect.poll(
      async () => parseInt(await app.getPluginStorage(plugin!.id, 'tick_count') || '0'),
      { timeout: 5000, intervals: [200, 500, 1000] }
    ).toBeGreaterThanOrEqual(1);

    const countBeforeDisable = await app.getPluginStorage(plugin!.id, 'tick_count');
    const ticksBefore = parseInt(countBeforeDisable);

    // Disable plugin
    await app.disablePlugin(plugin!.id);

    // Wait for what would be another tick cycle
    await app.page.waitForTimeout(3000);

    // Tick count should not have increased (or only slightly if timing edge case)
    const countAfterDisable = await app.getPluginStorage(plugin!.id, 'tick_count');
    const ticksAfter = parseInt(countAfterDisable);

    // Allow for at most 1 additional tick due to timing
    expect(ticksAfter).toBeLessThanOrEqual(ticksBefore + 1);
  });

  test('should resume scheduled tasks when plugin is re-enabled', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'scheduler-test.lua');

    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    schedulerPluginId = plugin?.id ?? null;

    // Disable immediately
    await app.disablePlugin(plugin!.id);

    // Get count while disabled
    const countWhileDisabled = await app.getPluginStorage(plugin!.id, 'tick_count');
    const disabledTicks = parseInt(countWhileDisabled);

    // Re-enable
    await app.enablePlugin(plugin!.id);

    // Wait for ticks to resume
    await expect.poll(
      async () => parseInt(await app.getPluginStorage(plugin!.id, 'tick_count') || '0'),
      { timeout: 5000, intervals: [200, 500, 1000], message: 'Waiting for ticks to resume' }
    ).toBeGreaterThan(disabledTicks);
  });
});
