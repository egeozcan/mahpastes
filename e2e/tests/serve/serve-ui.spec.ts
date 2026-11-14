import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

test.describe('Serve - UI', () => {
  test.afterEach(async ({ app }) => {
    await app.stopAllServers();
  });

  test('should show 3-way view toggle in drawer', async ({ app }) => {
    await app.openDrawer();
    await expect(app.page.locator(selectors.viewTabs.clips)).toBeVisible();
    await expect(app.page.locator(selectors.viewTabs.watch)).toBeVisible();
    await expect(app.page.locator(selectors.viewTabs.serve)).toBeVisible();
  });

  test('should switch to serve view via tab', async ({ app }) => {
    await app.switchToServeView();
    await expect(app.page.locator(selectors.serve.view)).toBeVisible();
    await expect(app.page.locator('section:has(#gallery)')).not.toBeVisible();
  });

  test('should switch back to clips via back button', async ({ app }) => {
    await app.switchToServeView();
    await app.page.click(selectors.serve.backBtn);
    await expect(app.page.locator('section:has(#gallery)')).toBeVisible();
    await expect(app.page.locator(selectors.serve.view)).not.toBeVisible();
  });

  // The list, each entry's choices and its port lived only in memory: after a
  // restart every tag had to be added and configured again, and each start
  // took a new random port, so a bookmarked URL died with every stop.
  test('the serve list, its choices and the port survive a reload; nothing starts by itself', async ({ app }) => {
    const { tagID } = await app.createTag('site');
    await app.switchToServeView();
    await app.page.evaluate(async (id) => {
      // @ts-ignore - app global
      await addConfiguredServeEntry(id, 'site');
    }, tagID);
    const row = app.page.locator(`${selectors.serve.list} > li`).first();
    await row.locator('.serve-bind-toggle').click();
    await row.locator('.serve-api-access').selectOption('read');

    await row.locator(selectors.serve.toggleBtn).click();
    await expect.poll(async () => (await app.getServeStatus()).length).toBe(1);
    const firstPort = (await app.getServeStatus())[0].port;
    await app.stopAllServers();

    await app.page.reload();
    await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 15000 });
    await app.switchToServeView();

    const restored = app.page.locator(`${selectors.serve.list} > li`).first();
    await expect(restored).toContainText('site');
    await expect(restored.locator('.serve-bind-toggle')).toHaveText('Network');
    await expect(restored.locator('.serve-api-access')).toHaveValue('read');
    expect(await app.getServeStatus()).toEqual([]);

    // Starting it again brings it up on the port it had.
    await restored.locator(selectors.serve.toggleBtn).click();
    await expect.poll(async () => (await app.getServeStatus()).length).toBe(1);
    const status = (await app.getServeStatus())[0];
    expect(status.port).toBe(firstPort);
    expect(status.bind_all).toBe(true);
    expect(status.api_access).toBe('read');
  });

  test('a remembered entry does not follow its id to a tag with another name', async ({ app }) => {
    const { tagID } = await app.createTag('public-site');
    await app.page.evaluate(async (id) => {
      // What a restore that reassigned this id would leave behind: choices
      // made for a tag of another name.
      // @ts-ignore - Wails runtime
      await window.go.main.App.SetSetting('serve_entries', JSON.stringify([
        { tag_id: id, tag_name: 'private-notes', bind_all: true, api_access: 'readwrite', last_port: 45123 },
      ]));
      (window as any).__testHelpers.resetServeEntries();
    }, tagID);
    await app.switchToServeView();
    await expect(app.page.locator(selectors.serve.list)).toContainText('No tags being served');
  });

  test('a removed entry stays removed after a reload', async ({ app }) => {
    const { tagID } = await app.createTag('gone');
    await app.switchToServeView();
    await app.page.evaluate(async (id) => {
      // @ts-ignore - app global
      await addConfiguredServeEntry(id, 'gone');
    }, tagID);
    await app.page.locator(`${selectors.serve.list} .serve-remove-btn`).first().click();
    await expect(app.page.locator(selectors.serve.list)).toContainText('No tags being served');

    await app.page.reload();
    await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 15000 });
    await app.switchToServeView();
    await expect(app.page.locator(selectors.serve.list)).toContainText('No tags being served');
  });
});
