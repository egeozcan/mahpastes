import { test, expect } from '../../fixtures/test-fixtures';

// The task panel printed a failure's reason twice, said "Click to expand"
// under every error, and its failure indicator could only be cleared with a
// button labelled "Clear completed".
test.describe('Background task panel', () => {
  async function seed(app: any, tasks: any[]) {
    await app.page.evaluate((list) => {
      const w = window as any;
      // @ts-ignore - app globals
      taskQueue.tasks.clear();
      // @ts-ignore
      for (const t of list) taskQueue.tasks.set(t.id, t);
      // @ts-ignore
      updateQueueBar();
      // @ts-ignore
      openQueueModal();
      // @ts-ignore
      renderQueueModal();
    }, tasks);
  }
  const now = () => new Date().toISOString();

  test.afterEach(async ({ app }) => {
    await app.page.evaluate(() => {
      // @ts-ignore
      taskQueue.tasks.clear(); updateQueueBar(); closeQueueModal();
    });
  });

  test('a short error is shown once, with nothing to expand', async ({ app }) => {
    await seed(app, [{ id: 't1', task_name: 'Upscale', status: 'failed', error: 'model not found', progress: 0, total: 1, created_at: now() }]);
    const card = app.page.locator('[data-task-id="t1"]');
    await expect(card.locator('[data-testid="task-error"]')).toHaveText('model not found');
    expect((await card.innerText()).split('model not found').length - 1).toBe(1);
    await expect(card.locator('[data-task-action="toggle-error"]')).toHaveCount(0);
    await expect(card).not.toContainText('Click to expand');
  });

  test('a long error expands with a real button, and stays expanded across redraws', async ({ app }) => {
    const long = 'request failed: ' + 'x'.repeat(400);
    await seed(app, [{ id: 't2', task_name: 'Upscale', status: 'failed', error: long, progress: 0, total: 1, created_at: now() }]);
    const card = app.page.locator('[data-task-id="t2"]');
    const toggle = card.locator('[data-task-action="toggle-error"]');
    await expect(toggle).toHaveText('Show all');
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await expect(card.locator('[data-testid="task-error"]')).toHaveClass(/line-clamp-2/);

    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await expect(toggle).toHaveText('Show less');
    // A progress event redraws the list; the choice survives it.
    // @ts-ignore
    await app.page.evaluate(() => renderQueueModal());
    await expect(card.locator('[data-testid="task-error"]')).not.toHaveClass(/line-clamp-2/);
  });

  test('a finished task can be dismissed on its own, and the footer button says what it clears', async ({ app }) => {
    await seed(app, [
      { id: 'a', task_name: 'Failed one', status: 'failed', error: 'boom', progress: 0, total: 1, created_at: now() },
      { id: 'b', task_name: 'Running one', status: 'running', progress: 1, total: 4, created_at: now() },
    ]);
    await expect(app.page.locator('#clear-completed-btn')).toHaveText('Clear finished');
    // A running task has no dismiss button.
    await expect(app.page.locator('[data-task-id="b"] [data-task-action="dismiss"]')).toHaveCount(0);

    await app.page.locator('[data-task-id="a"] [data-task-action="dismiss"]').click();
    await expect(app.page.locator('[data-task-id="a"]')).toHaveCount(0);
    await expect(app.page.locator('[data-task-id="b"]')).toHaveCount(1);
  });
});

// Update badges only ever appeared if the scheduled check's event arrived
// while the page was loaded; there was no way to ask.
test.describe('Plugins: Check for updates', () => {
  test('the button asks, reports the count and marks the plugins', async ({ app }) => {
    await app.deleteAllPlugins();
    const pathMod = await import('path');
    const { fileURLToPath } = await import('url');
    const dir = pathMod.resolve(pathMod.dirname(fileURLToPath(import.meta.url)), '../../test-plugins');
    const plugin = await app.importPluginFromPath(pathMod.join(dir, 'settings-test.lua'));
    expect(plugin).not.toBeNull();
    try {
      await app.page.evaluate((id) => {
        const w = window as any;
        const svc = w.go.main.PluginService;
        const original = svc.CheckForUpdates;
        w.__restoreCheck = () => { svc.CheckForUpdates = original; };
        w.__checks = 0;
        svc.CheckForUpdates = async () => {
          w.__checks++;
          return w.__checks === 1 ? [{ plugin_id: id, current_version: '1.0.0', latest_version: '2.0.0' }] : [];
        };
      }, plugin!.id);
      await app.openPluginsModal();
      const button = app.page.locator('[data-testid="check-plugin-updates-btn"]');
      const card = app.page.locator(`[data-testid="plugin-card-${plugin!.id}"]`);
      await expect(card).not.toContainText('Update available');

      await button.click();
      await app.expectToast('1 plugin update available.');
      await expect(card).toContainText('Update available');
      await expect(button).toBeEnabled();

      // The next answer is the truth: a plugin no longer behind loses its badge.
      await button.click();
      await app.expectToast('No plugin updates found.');
      await expect(card).not.toContainText('Update available');
    } finally {
      await app.page.evaluate(() => (window as any).__restoreCheck?.());
      await app.removePlugin(plugin!.id).catch(() => undefined);
    }
  });

});
