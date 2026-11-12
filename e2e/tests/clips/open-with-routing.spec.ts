import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

// Open With takes the native chooser where the platform has one (Windows) and
// the application picker everywhere else. The bound methods are stubbed so
// both branches can be checked on any host without launching an app.
const STUBBED = ['SupportsNativeOpenWith', 'OpenClipWithDialog', 'ChooseApplication', 'OpenClipWithApp'];

async function stubOpenWith(app: any, native: boolean): Promise<void> {
  await app.page.evaluate(({ names, native }: { names: string[]; native: boolean }) => {
    const w = window as any;
    const App = w.go.main.App;
    w.__origOpenWith = {};
    for (const name of names) w.__origOpenWith[name] = App[name];
    w.__openWithCalls = [];
    App.SupportsNativeOpenWith = async () => native;
    App.OpenClipWithDialog = async (id: number) => { w.__openWithCalls.push(['dialog', id]); };
    App.ChooseApplication = async () => { w.__openWithCalls.push(['choose']); return ''; };
    App.OpenClipWithApp = async (id: number, appPath: string) => { w.__openWithCalls.push(['app', id, appPath]); };
  }, { names: STUBBED, native });
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
});
