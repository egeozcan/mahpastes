import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import { createTempFile, generateTestImage, generateTestText } from '../../helpers/test-data';
import * as path from 'path';

/** Upload in-page through upload(), without waiting for any dialog it raises. */
function startUpload(app: any, name: string, contentType: string, data: Buffer) {
  return app.page.evaluate(async ({ name, contentType, data }: any) => {
    // @ts-ignore - global
    await upload([{ name, content_type: contentType, data }]);
    return 'done';
  }, { name, contentType, data: data.toString('base64') });
}

async function cardImageSrc(app: any, filename: string): Promise<string> {
  const img = app.page.locator(`${selectors.gallery.clipCardByName(filename)} img:not(.video-thumb)`).first();
  await expect(img).toHaveAttribute('src', /^data:/);
  return (await img.getAttribute('src')) as string;
}

test.describe('Gallery state across reloads and edits', () => {
  test('overwriting a clip in place refreshes its card image', async ({ app }) => {
    const filePath = await createTempFile(generateTestImage(40, 40, [255, 0, 0]), 'png');
    const name = path.basename(filePath);
    await app.uploadFile(filePath);
    const before = await cardImageSrc(app, name);

    const upload = startUpload(app, name, 'image/png', generateTestImage(40, 40, [0, 0, 255]));
    await expect(app.page.locator(selectors.conflict.dialog)).not.toHaveAttribute('inert', '');
    await app.page.locator(selectors.conflict.overwriteButton).click();
    await upload;

    await expect.poll(() => cardImageSrc(app, name)).not.toBe(before);
  });

  test('a second conflicting upload settles the first instead of wedging it', async ({ app }) => {
    const filePath = await createTempFile(generateTestImage(30, 30, [255, 0, 0]), 'png');
    const name = path.basename(filePath);
    await app.uploadFile(filePath);

    const first = startUpload(app, name, 'image/png', generateTestImage(30, 30, [0, 255, 0]));
    await expect(app.page.locator(selectors.conflict.dialog)).not.toHaveAttribute('inert', '');
    const second = startUpload(app, name, 'image/png', generateTestImage(30, 30, [0, 0, 255]));

    // The first upload is settled (as a skip) once the second takes the dialog.
    await expect(first).resolves.toBe('done');
    await app.page.locator(selectors.conflict.skipButton).click();
    await expect(second).resolves.toBe('done');
    await app.expectClipCount(1);
  });

  test('the file input is cleared so the same file can be picked again', async ({ app }) => {
    const filePath = await createTempFile(generateTestText('again'), 'txt');
    await app.uploadFile(filePath);
    await expect.poll(() => app.page.evaluate(() =>
      (document.getElementById('file-input') as HTMLInputElement).value)).toBe('');
  });

  test('selection survives a reload of the same view', async ({ app }) => {
    const a = await createTempFile(generateTestText('keep-a'), 'txt');
    const b = await createTempFile(generateTestText('keep-b'), 'txt');
    await app.uploadFile(a);
    await app.uploadFile(b);
    await app.selectClip(path.basename(a));
    expect(await app.getSelectedCount()).toBe(1);

    await app.refreshClips();
    await expect(app.page.locator(`${selectors.gallery.clipCardByName(path.basename(a))} ${selectors.gallery.clipCheckbox}`)).toBeChecked();
    expect(await app.getSelectedCount()).toBe(1);
  });

  test('archiving the focused clip keeps keyboard focus in the gallery', async ({ app }) => {
    for (const label of ['f1', 'f2', 'f3']) {
      await app.uploadFile(await createTempFile(generateTestText(label), 'txt'));
    }
    await app.expectClipCount(3);
    const second = app.page.locator(selectors.gallery.clipCard).nth(1);
    await second.focus();
    await app.pressKey('e');
    await app.expectClipCount(2);

    await expect.poll(() => app.page.evaluate(() =>
      !!document.activeElement && document.activeElement.matches('#gallery > li'))).toBe(true);
    // Arrow keys still move through the gallery.
    await app.pressKey('ArrowLeft');
    await expect.poll(() => app.page.evaluate(() =>
      Array.from(document.querySelectorAll('#gallery > li')).indexOf(document.activeElement as Element))).toBe(0);
  });

  test('deleting the focused clip through the confirm keeps keyboard focus in the gallery', async ({ app }) => {
    for (const label of ['d1', 'd2', 'd3']) {
      await app.uploadFile(await createTempFile(generateTestText(label), 'txt'));
    }
    await app.expectClipCount(3);
    const second = app.page.locator(selectors.gallery.clipCard).nth(1);
    await second.focus();
    await app.pressKey('d');
    await expect(app.page.locator(selectors.confirm.dialog)).toHaveClass(/opacity-100/);
    await app.page.locator(selectors.confirm.confirmButton).click();
    await app.expectClipCount(2);

    await expect.poll(() => app.page.evaluate(() =>
      !!document.activeElement && document.activeElement.matches('#gallery > li'))).toBe(true);
  });

  test('a failed reload clears the selection so bulk actions cannot reach unseen clips', async ({ app }) => {
    const file = await createTempFile(generateTestText('sel-err'), 'txt');
    await app.uploadFile(file);
    await app.selectClip(path.basename(file));
    expect(await app.getSelectedCount()).toBe(1);
    await expect(app.page.locator(selectors.bulk.toolbar)).toHaveClass(/pointer-events-auto/);

    await app.page.evaluate(async () => {
      const api = (window as any).go.main.App;
      const original = api.ListClipsPage;
      api.ListClipsPage = () => Promise.reject('database is locked');
      try {
        // @ts-ignore - global
        await loadClips();
      } finally {
        api.ListClipsPage = original;
      }
    });
    await expect(app.page.locator(selectors.gallery.emptyState)).toHaveText(/Error loading clips/);
    // @ts-ignore - app.js top-level let
    expect(await app.page.evaluate(() => selectedIds.size)).toBe(0);
    await expect(app.page.locator(selectors.bulk.toolbar)).not.toHaveClass(/pointer-events-auto/);
    await app.refreshClips();
  });

  test('plain search drops hidden clips from the selection and counts what is visible', async ({ app }) => {
    const alpha = await createTempFile(generateTestText('alpha'), 'txt');
    const beta = await createTempFile(generateTestText('beta'), 'txt');
    await app.uploadFile(alpha);
    await app.uploadFile(beta);
    await app.selectClips([path.basename(alpha), path.basename(beta)]);
    expect(await app.getSelectedCount()).toBe(2);

    // The temp names share a prefix; search on the unique tail of one of them.
    const unique = path.basename(alpha).slice(-12);
    await app.search(`  ${unique}  `);
    await app.expectClipVisible(path.basename(alpha));
    await expect(app.page.locator(selectors.gallery.clipCardByName(path.basename(beta)))).toBeHidden();
    expect(await app.getSelectedCount()).toBe(1);
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toHaveText('1 of 2 clips');
  });

  test('plain search matches folder cards by name', async ({ app }) => {
    await app.createTag('projects');
    await app.createTag('receipts');
    await app.toggleFolderMode();
    await app.expectFolderVisible('projects');
    await app.search('proj');
    await expect(app.page.locator(selectors.tags.folderCard('projects'))).toBeVisible();
    await expect(app.page.locator(selectors.tags.folderCard('receipts'))).toBeHidden();
  });

  test('a clip is not dimmed when its hidden tag is the active filter', async ({ app }) => {
    await app.createTag('secret');
    const file = await createTempFile(generateTestText('hush'), 'txt');
    const name = path.basename(file);
    await app.uploadFile(file);
    await app.addTagToClip(name, 'secret');
    await app.setHiddenTags(['secret']);
    await app.filterByTag('secret');
    await app.expectClipVisible(name);
    await expect(app.page.locator(selectors.gallery.clipCardByName(name))).not.toHaveAttribute('data-hidden', 'true');
  });

  test('a failed bulk expiry keeps the selection', async ({ app }) => {
    const file = await createTempFile(generateTestText('expiry'), 'txt');
    await app.uploadFile(file);
    await app.selectClip(path.basename(file));
    await app.page.evaluate(() => {
      const api = (window as any).go.main.App;
      const original = api.BulkSetExpiration;
      (window as any).__restoreBulkSetExpiration = () => { api.BulkSetExpiration = original; };
      api.BulkSetExpiration = () => Promise.reject('boom');
    });
    try {
      await app.page.locator('#bulk-expiry-btn').click();
      await app.page.locator('.expiration-popover button', { hasText: '1h' }).click();
      await app.expectToast('boom');
      expect(await app.getSelectedCount()).toBe(1);
    } finally {
      await app.page.evaluate(() => {
        const w = window as any;
        w.__restoreBulkSetExpiration?.();
        delete w.__restoreBulkSetExpiration;
      });
    }
  });

  test('the empty state sits beside the gallery in the stone palette', async ({ app }) => {
    const status = app.page.locator(selectors.gallery.emptyState);
    await expect(status).toBeVisible();
    await expect(status).toHaveText(/No active clips/);
    await expect(status).toHaveClass(/text-stone-400/);
    await expect(app.page.locator('#gallery > p')).toHaveCount(0);
  });

  test('uploading content that already exists says so in the summary', async ({ app }) => {
    const data = generateTestImage(20, 20, [10, 20, 30]);
    const first = await createTempFile(data, 'png');
    await app.uploadFile(first);
    await startUpload(app, 'copy-of-' + path.basename(first), 'image/png', data);
    await expect(app.page.locator(selectors.toast.message)).toContainText(/duplicate/i);
  });
});
