import { test, expect } from '../../fixtures/test-fixtures';
import { createTempDir, createTempFile, generateTestImage, generateTestText } from '../../helpers/test-data';
import * as path from 'path';

/**
 * Launching mahpastes while it is already running hands the new launch's
 * arguments (files from Send To, a drop on the exe or launcher, Open With) to
 * the running window instead of failing on the instance lock. The second
 * process talks to the first over the instance socket; these tests speak that
 * protocol directly, the way the second process does.
 *
 * The backend queues requests and every attached frontend may drain them (in
 * `wails dev` that is the hidden native window as well as this page), so the
 * socket tests assert on the library, not on toasts in this page.
 */
test.describe('Open requests from a second launch', () => {
  test.skip(process.platform === 'win32', "Node's net module cannot speak AF_UNIX sockets on Windows");

  test('imports files forwarded by a second launch', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(32, 32, 'green'), 'png');
    const textPath = await createTempFile(generateTestText('second-launch'), 'txt');

    const reply = await app.sendOpenRequest({ files: [imagePath, textPath] });
    expect(reply.ok).toBe(true);

    await expect.poll(() => app.getClipCountFromDB(), { timeout: 10000 }).toBe(2);
    await app.refreshClips();
    await app.expectClipVisible(path.basename(imagePath));
    await app.expectClipVisible(path.basename(textPath));
  });

  test('imports each forwarded file once', async ({ app }) => {
    const filePath = await createTempFile(generateTestText('once'), 'txt');

    // A selection can name the same file twice (and macOS reports a file once
    // per open event); the queue keeps one copy of each pending file.
    expect((await app.sendOpenRequest({ files: [filePath, filePath] })).ok).toBe(true);

    await expect.poll(() => app.getClipCountFromDB(), { timeout: 10000 }).toBe(1);
    // Give a stray second import time to show up before asserting it did not.
    await app.page.waitForTimeout(1000);
    expect(await app.getClipCountFromDB()).toBe(1);
  });

  test('ignores folders and missing paths', async ({ app }) => {
    const dir = await createTempDir();
    const missing = path.join(dir, 'does-not-exist.txt');

    const reply = await app.sendOpenRequest({ files: [dir, missing] });
    expect(reply.ok).toBe(true);

    await app.page.waitForTimeout(1500);
    expect(await app.getClipCountFromDB()).toBe(0);
  });

  test('refuses a malformed request', async ({ app }) => {
    const reply = await app.sendOpenRequest({ files: Array(10001).fill('/x') });
    expect(reply.ok).toBe(false);
    expect(reply.error).toContain('too many');
  });
});

test.describe('Open request handling in the window', () => {
  const handle = (app: any, batch: { files: string[]; skipped: string[]; urls: string[] }) =>
    app.page.evaluate((b: any) => (window as any).__testHelpers.handleOpenRequests(b), batch);

  test('imports opened files and shows them', async ({ app }) => {
    const filePath = await createTempFile(generateTestImage(24, 24, 'red'), 'png');

    await handle(app, { files: [filePath], skipped: [], urls: [] });

    await app.expectClipCount(1);
    await app.expectClipVisible(path.basename(filePath));
  });

  test('switches from the archive to the active list to show opened files', async ({ app }) => {
    const filePath = await createTempFile(generateTestText('from-archive'), 'txt');
    await app.toggleArchiveView();
    expect(await app.isArchiveViewActive()).toBe(true);

    await handle(app, { files: [filePath], skipped: [], urls: [] });

    expect(await app.isArchiveViewActive()).toBe(false);
    await app.expectClipVisible(path.basename(filePath));
  });

  test('reports items that are not files', async ({ app }) => {
    await handle(app, { files: [], skipped: ['/somewhere/Folder', 'C:\\data\\gone.txt'], urls: [] });

    await app.expectToast("Skipped 2 items that can't be imported: Folder, gone.txt");
    await app.expectClipCount(0);
  });

  test('reports links it has no action for', async ({ app }) => {
    await handle(app, { files: [], skipped: [], urls: ['mahpastes://nothing-here'] });

    await app.expectToast('Unsupported mahpastes link: mahpastes://nothing-here');
  });
});
