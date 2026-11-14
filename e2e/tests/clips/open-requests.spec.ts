import { test, expect } from '../../fixtures/test-fixtures';
import { createTempDir, createTempFile, generateTestImage, generateTestText } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
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

type OpenBatch = {
  files?: string[];
  file_sizes?: number[];
  skipped?: string[];
  too_large?: string[];
  links?: { kind: string; clip_id?: number; url: string }[];
  unsupported_links?: string[];
  paste_clipboard?: boolean;
};

test.describe('Open request handling in the window', () => {
  const handle = (app: any, batch: OpenBatch) =>
    app.page.evaluate((b: any) => (window as any).__testHelpers.handleOpenRequests(b), batch);

  test('imports opened files and shows them', async ({ app }) => {
    const filePath = await createTempFile(generateTestImage(24, 24, 'red'), 'png');

    await handle(app, { files: [filePath] });

    await app.expectClipCount(1);
    await app.expectClipVisible(path.basename(filePath));
  });

  test('switches from the archive to the active list to show opened files', async ({ app }) => {
    const filePath = await createTempFile(generateTestText('from-archive'), 'txt');
    await app.toggleArchiveView();
    expect(await app.isArchiveViewActive()).toBe(true);

    await handle(app, { files: [filePath] });

    expect(await app.isArchiveViewActive()).toBe(false);
    await app.expectClipVisible(path.basename(filePath));
  });

  test('reports items that are not files', async ({ app }) => {
    await handle(app, { skipped: ['/somewhere/Folder', 'C:\\data\\gone.txt'] });

    await app.expectToast("Skipped 2 items that can't be imported: Folder, gone.txt");
    await app.expectClipCount(0);
  });

  test('reports files too large to import and still imports the rest', async ({ app }) => {
    const filePath = await createTempFile(generateTestText('beside-a-big-one'), 'txt');

    await handle(app, { files: [filePath], file_sizes: [16], too_large: ['/videos/huge.mov'] });

    await app.expectToast('Skipped 1 file too large to import (over 1 GB): huge.mov');
    await app.expectClipCount(1);
    await app.expectClipVisible(path.basename(filePath));
  });

  test('reports links it has no action for', async ({ app }) => {
    await handle(app, { unsupported_links: ['mahpastes://nothing-here'] });

    await app.expectToast('Unsupported mahpastes link: mahpastes://nothing-here');
  });

  test('shows an unsupported link as shortened text, never as markup', async ({ app }) => {
    const link = 'mahpastes:<img id=pwn-link src=x>' + 'a'.repeat(200);

    await handle(app, { unsupported_links: [link] });

    await app.expectToast(`Unsupported mahpastes link: ${link.slice(0, 80)}…`);
    await expect(app.page.locator('#pwn-link')).toHaveCount(0);
  });

  test('a clip link opens an image clip in the lightbox', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(32, 32, 'blue'), 'png');
    await app.uploadFile(imagePath);
    const card = await app.getClipByFilename(path.basename(imagePath));
    const id = Number(await card.getAttribute('data-id'));

    await handle(app, { links: [{ kind: 'clip', clip_id: id, url: `mahpastes://clip/${id}` }] });

    await expect.poll(() => app.isLightboxOpen()).toBe(true);
  });

  test('a clip link opens a text clip in the editor', async ({ app }) => {
    const textPath = await createTempFile(generateTestText('linked-text'), 'txt');
    await app.uploadFile(textPath);
    const card = await app.getClipByFilename(path.basename(textPath));
    const id = Number(await card.getAttribute('data-id'));

    await handle(app, { links: [{ kind: 'clip', clip_id: id, url: `mahpastes://clip/${id}` }] });

    await expect.poll(() => app.isEditorOpen()).toBe(true);
  });

  test('a link to a missing clip is reported', async ({ app }) => {
    await handle(app, { links: [{ kind: 'clip', clip_id: 999999, url: 'mahpastes://clip/999999' }] });

    await app.expectToast('Clip 999999 was not found.');
  });

  test('a link never opens another application', async ({ app }) => {
    const pdfPath = await createTempFile(Buffer.from('%PDF-1.4\n%%EOF\n'), 'pdf');
    await app.uploadFile(pdfPath);
    const card = await app.getClipByFilename(path.basename(pdfPath));
    const id = Number(await card.getAttribute('data-id'));
    await app.page.evaluate(() => {
      (window as any).__openedWithDefaultApp = 0;
      const api = (window as any).go.main.App;
      api.OpenClipWithDefaultApp = async () => { (window as any).__openedWithDefaultApp++; };
    });

    await handle(app, { links: [{ kind: 'clip', clip_id: id, url: `mahpastes://clip/${id}` }] });

    // A clip without an in-app viewer is focused in the gallery instead.
    await expect(card).toBeFocused();
    expect(await app.page.evaluate(() => (window as any).__openedWithDefaultApp)).toBe(0);
  });

  test('the paste action saves the system clipboard as a clip', async ({ app }) => {
    const marker = `pasted-from-shortcut-${Date.now()}`;
    await app.page.evaluate((text) => {
      const api = (window as any).go.main.App;
      api.GetClipboardImage = async () => { throw new Error('no image in clipboard'); };
      api.GetClipboardText = async () => text;
    }, marker);

    await handle(app, { paste_clipboard: true });

    await expect.poll(() => app.getClipCountFromDB(), { timeout: 10000 }).toBe(1);
  });
});

test.describe('Clip links', () => {
  test('Copy > Link puts the clip\'s mahpastes link on the clipboard', async ({ app }) => {
    const textPath = await createTempFile(generateTestText('link-me'), 'txt');
    await app.uploadFile(textPath);
    const filename = path.basename(textPath);
    const card = await app.getClipByFilename(filename);
    const id = Number(await card.getAttribute('data-id'));
    await app.page.evaluate(() => {
      const api = (window as any).go.main.App;
      (window as any).__copiedText = null;
      api.CopyToClipboard = async (text: string) => { (window as any).__copiedText = text; };
    });

    await app.hoverCopySubmenu(filename);
    await app.page.locator(selectors.cardMenu.copyLink).click();

    await app.expectToast('Link copied to clipboard.');
    expect(await app.page.evaluate(() => (window as any).__copiedText)).toBe(`mahpastes://clip/${id}`);
  });

  // The lightbox Actions menu shows the same items as the card menu; Link
  // used to close the menu and copy nothing.
  test('lightbox Actions > Copy > Link copies the same link', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(32, 32, 'blue'), 'png');
    await app.uploadFile(imagePath);
    const filename = path.basename(imagePath);
    const card = await app.getClipByFilename(filename);
    const id = Number(await card.getAttribute('data-id'));
    await app.page.evaluate(() => {
      const api = (window as any).go.main.App;
      (window as any).__copiedText = null;
      api.CopyToClipboard = async (text: string) => { (window as any).__copiedText = text; };
    });

    await app.openLightbox(filename);
    await app.page.click(selectors.lightbox.fileTrigger);
    await app.page.locator(selectors.cardMenu.copyTrigger).hover();
    await app.page.locator(selectors.cardMenu.copyLink).click();

    await app.expectToast('Link copied to clipboard.');
    expect(await app.page.evaluate(() => (window as any).__copiedText)).toBe(`mahpastes://clip/${id}`);
    await expect(app.page.locator(selectors.lightbox.overlay)).toHaveClass(/active/);
  });
});
