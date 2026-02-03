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

    // Wait for plugin to initialize
    await app.page.waitForTimeout(500);

    // Check initial state
    const initialCount = await app.getPluginStorage(plugin!.id, 'tick_count');
    expect(initialCount).toBe('0');

    // Wait for first tick (2 second interval + buffer)
    await app.page.waitForTimeout(2500);

    // Check tick count increased
    const countAfterFirstTick = await app.getPluginStorage(plugin!.id, 'tick_count');
    const count1 = parseInt(countAfterFirstTick);
    expect(count1).toBeGreaterThanOrEqual(1);

    // Wait for another tick
    await app.page.waitForTimeout(2500);

    // Check tick count increased again
    const countAfterSecondTick = await app.getPluginStorage(plugin!.id, 'tick_count');
    const count2 = parseInt(countAfterSecondTick);
    expect(count2).toBeGreaterThan(count1);
  });

  test('should record last tick timestamp', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'scheduler-test.lua');

    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    schedulerPluginId = plugin?.id ?? null;

    // Wait for a tick
    await app.page.waitForTimeout(2500);

    // Check last_tick was set
    const lastTick = await app.getPluginStorage(plugin!.id, 'last_tick');
    expect(lastTick).not.toBe('0');
    expect(parseInt(lastTick)).toBeGreaterThan(0);
  });

  test('should stop scheduled tasks when plugin is disabled', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'scheduler-test.lua');

    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    schedulerPluginId = plugin?.id ?? null;

    // Wait for some ticks
    await app.page.waitForTimeout(3000);

    // Get current tick count
    const countBeforeDisable = await app.getPluginStorage(plugin!.id, 'tick_count');
    const ticksBefore = parseInt(countBeforeDisable);
    expect(ticksBefore).toBeGreaterThanOrEqual(1);

    // Disable plugin
    await app.disablePlugin(plugin!.id);
    await app.page.waitForTimeout(500);

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
    await app.page.waitForTimeout(500);

    // Get count while disabled
    const countWhileDisabled = await app.getPluginStorage(plugin!.id, 'tick_count');

    // Re-enable
    await app.enablePlugin(plugin!.id);

    // Wait for ticks to resume
    await app.page.waitForTimeout(3000);

    // Count should have increased
    const countAfterReEnable = await app.getPluginStorage(plugin!.id, 'tick_count');
    const afterTicks = parseInt(countAfterReEnable);
    const disabledTicks = parseInt(countWhileDisabled);

    expect(afterTicks).toBeGreaterThan(disabledTicks);
  });
});
