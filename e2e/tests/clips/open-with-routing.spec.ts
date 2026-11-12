import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

// Open With takes the native chooser where the platform has one (Windows),
// lists the system's applications where it can (macOS), and uses the
// application picker everywhere else. The bound methods and the capability
// are stubbed so every branch can be checked on any host without launching an
// app.
const STUBBED = ['SupportsNativeOpenWith', 'OpenClipWithDialog', 'ChooseApplication', 'OpenClipWithApp', 'ListOpenWithApps'];

type ListedApp = { name: string; path: string; is_default: boolean };

async function stubOpenWith(
  app: any,
  native: boolean,
  opts: { listApps?: boolean; apps?: ListedApp[]; listError?: boolean; holdList?: boolean } = {},
): Promise<void> {
  await app.page.evaluate(({ names, native, opts }: { names: string[]; native: boolean; opts: any }) => {
    const w = window as any;
    const App = w.go.main.App;
    w.__origOpenWith = {};
    for (const name of names) w.__origOpenWith[name] = App[name];
    w.__openWithCalls = [];
    w.__openWithListRelease = [];
    App.SupportsNativeOpenWith = async () => native;
    App.OpenClipWithDialog = async (id: number) => { w.__openWithCalls.push(['dialog', id]); };
    App.ChooseApplication = async () => { w.__openWithCalls.push(['choose']); return ''; };
    App.OpenClipWithApp = async (id: number, appPath: string) => { w.__openWithCalls.push(['app', id, appPath]); };
    App.ListOpenWithApps = async (id: number) => {
      w.__openWithCalls.push(['list', id]);
      // Held lookups queue until released; later ones pass straight through.
      if (opts.holdList && w.__openWithListRelease) {
        await new Promise(resolve => w.__openWithListRelease.push(resolve));
      }
      if (opts.listError) throw new Error('osascript failed');
      return opts.apps || [];
    };
    const caps = w.__testHelpers.getTransferCapabilitiesForTest();
    w.__testHelpers.setTransferCapabilitiesForTest({ ...caps, open_with_apps: !!opts.listApps });
  }, { names: STUBBED, native, opts });
}

async function releaseListLookups(app: any): Promise<void> {
  await app.page.evaluate(() => {
    const w = window as any;
    const pending = w.__openWithListRelease || [];
    w.__openWithListRelease = null;
    for (const resolve of pending) resolve();
  });
}

async function openWithCalls(app: any): Promise<unknown[][]> {
  return app.page.evaluate(() => (window as any).__openWithCalls);
}

async function clipId(app: any, filename: string): Promise<number> {
  const clip = await app.getClipByFilename(filename);
  return Number(await clip.getAttribute('data-id'));
}

test.describe('Open With routing', () => {
  test.afterEach(async ({ app }) => {
    await app.page.evaluate((names: string[]) => {
      const w = window as any;
      if (!w.__origOpenWith) return;
      for (const name of names) w.go.main.App[name] = w.__origOpenWith[name];
      delete w.__origOpenWith;
      delete w.__openWithCalls;
      if (w.__openWithListRelease) for (const resolve of w.__openWithListRelease) resolve();
      delete w.__openWithListRelease;
      w.__testHelpers.clearTransferCapabilitiesForTest();
    }, STUBBED);
  });

  test('card menu uses the native chooser when the platform has one', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    const name = path.basename(imagePath);
    const id = await clipId(app, name);
    await stubOpenWith(app, true);

    await app.openCardMenu(name);
    await app.page.locator(selectors.cardMenu.openWith).click();

    await expect.poll(() => openWithCalls(app)).toEqual([['dialog', id]]);
    await expect(app.page.locator(selectors.toast.container)).not.toContainText('Failed to open clip.');
  });

  test('lightbox menu uses the native chooser when the platform has one', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    const name = path.basename(imagePath);
    const id = await clipId(app, name);
    await stubOpenWith(app, true);

    await app.openLightbox(name);
    await app.page.locator('#lightbox-file-menu-trigger').click();
    await app.page.locator(selectors.cardMenu.openWith).click();

    await expect.poll(() => openWithCalls(app)).toEqual([['dialog', id]]);
    await expect(app.page.locator(selectors.toast.container)).not.toContainText('Failed to open clip.');
  });

  test('falls back to the application picker without a native chooser', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    const name = path.basename(imagePath);
    await stubOpenWith(app, false);

    await app.openCardMenu(name);
    await app.page.locator(selectors.cardMenu.openWith).click();

    // A cancelled picker (empty path) opens nothing.
    await expect.poll(() => openWithCalls(app)).toEqual([['choose']]);
  });

  test.describe('application list (macOS)', () => {
    const APPS: ListedApp[] = [
      { name: 'Preview', path: '/System/Applications/Preview.app', is_default: true },
      { name: 'Pixelmator <Pro>', path: '/Applications/Pixelmator Pro.app', is_default: false },
    ];

    test('lists the system applications, default first, and opens the one picked', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      const name = path.basename(imagePath);
      const id = await clipId(app, name);
      await stubOpenWith(app, false, { listApps: true, apps: APPS });

      await app.openCardMenu(name);
      // The plain item is replaced by the submenu.
      await expect(app.page.locator(selectors.cardMenu.openWith)).toHaveCount(0);
      await app.hoverOpenWithSubmenu(name);

      const rows = app.page.locator(selectors.cardMenu.openWithApp);
      await expect(rows).toHaveCount(2);
      await expect(rows.nth(0)).toHaveText('Preview (default)');
      // Names are text, never markup.
      await expect(rows.nth(1)).toHaveText('Pixelmator <Pro>');
      await expect(rows.nth(1)).toHaveAttribute('title', '/Applications/Pixelmator Pro.app');
      await expect(app.page.locator(selectors.cardMenu.openWithOther)).toHaveText('Other\u2026');

      await rows.nth(1).click();
      await expect.poll(() => openWithCalls(app)).toEqual([['list', id], ['app', id, '/Applications/Pixelmator Pro.app']]);
      await expect(app.page.locator(selectors.cardMenu.dropdown)).toHaveCount(0);
    });

    test('"Other…" falls back to the application picker', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      const name = path.basename(imagePath);
      const id = await clipId(app, name);
      await stubOpenWith(app, false, { listApps: true, apps: APPS });

      await app.hoverOpenWithSubmenu(name);
      await app.page.locator(selectors.cardMenu.openWithOther).click();

      await expect.poll(() => openWithCalls(app)).toEqual([['list', id], ['choose']]);
    });

    test('shows a loading row until the list arrives', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      const name = path.basename(imagePath);
      await stubOpenWith(app, false, { listApps: true, apps: APPS, holdList: true });

      await app.hoverOpenWithSubmenu(name);
      await expect(app.page.locator(selectors.cardMenu.submenuStatus)).toHaveText('Loading\u2026');
      // Clicking the placeholder does nothing and keeps the menu open.
      await app.page.locator(selectors.cardMenu.submenuStatus).click();
      await expect(app.page.locator(selectors.cardMenu.dropdown)).toBeVisible();

      await releaseListLookups(app);
      await expect(app.page.locator(selectors.cardMenu.openWithApp)).toHaveCount(2);
      await expect(app.page.locator(selectors.cardMenu.submenuStatus)).toHaveCount(0);
    });

    test('a failed lookup leaves only "Other…"', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      const name = path.basename(imagePath);
      await stubOpenWith(app, false, { listApps: true, listError: true });

      await app.hoverOpenWithSubmenu(name);
      await expect(app.page.locator(selectors.cardMenu.openWithOther)).toBeVisible();
      await expect(app.page.locator(selectors.cardMenu.openWithApp)).toHaveCount(0);
      await expect(app.page.locator(`${selectors.cardMenu.submenu} .card-menu-divider`)).toHaveCount(0);
    });

    test('keyboard: ArrowRight opens the list and focuses the first application', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      const name = path.basename(imagePath);
      await stubOpenWith(app, false, { listApps: true, apps: APPS, holdList: true });

      await app.openCardMenu(name);
      await app.page.locator(selectors.cardMenu.openWithTrigger).focus();
      await app.page.keyboard.press('ArrowRight');
      await expect(app.page.locator(selectors.cardMenu.submenuStatus)).toBeFocused();

      await releaseListLookups(app);
      await expect(app.page.locator(selectors.cardMenu.openWithApp).first()).toBeFocused();
    });

    test('lightbox menu lists the applications too', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      const name = path.basename(imagePath);
      const id = await clipId(app, name);
      await stubOpenWith(app, false, { listApps: true, apps: APPS });

      await app.openLightbox(name);
      await app.page.locator('#lightbox-file-menu-trigger').click();
      await app.page.locator(selectors.cardMenu.openWithTrigger).hover();
      await app.page.locator(selectors.cardMenu.openWithApp).first().click();

      await expect.poll(() => openWithCalls(app)).toEqual([['list', id], ['app', id, '/System/Applications/Preview.app']]);
    });
  });
});
