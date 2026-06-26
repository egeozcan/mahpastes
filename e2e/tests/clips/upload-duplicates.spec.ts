import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import { generateTestImage, createTempFile } from '../../helpers/test-data';
import * as fs from 'fs';
import * as path from 'path';

/**
 * Helper: upload file data via the app's upload() function directly,
 * bypassing the file input element. This avoids browser change-event quirks
 * when re-selecting the same file path.
 */
async function uploadViaJS(app: any, filePath: string) {
  const content = fs.readFileSync(filePath);
  const base64 = content.toString('base64');
  const filename = path.basename(filePath);
  const ext = path.extname(filePath).slice(1);
  const mimeMap: Record<string, string> = { png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif' };
  const contentType = mimeMap[ext] || 'application/octet-stream';

  await app.page.evaluate(async ({ name, ct, data }: { name: string; ct: string; data: string }) => {
    // @ts-ignore - upload is a global function
    await upload([{ name, content_type: ct, data }]);
  }, { name: filename, ct: contentType, data: base64 });
}

test.describe('Upload Duplicate Detection', () => {
  test('identical content files are silently skipped', async ({ app }) => {
    // Upload a file
    const img = generateTestImage(50, 50, [255, 0, 0]);
    const filePath = await createTempFile(img, 'png');
    await app.uploadFile(filePath);
    await app.expectClipCount(1);

    // Upload the exact same file again (same filename, same content)
    await uploadViaJS(app, filePath);

    // Should show toast with "skipped"
    await app.expectToast('skipped');

    // Count should remain at 1
    await app.expectClipCount(1);
  });

  test('different content same name shows conflict dialog', async ({ app }) => {
    // Upload first file
    const img1 = generateTestImage(50, 50, [255, 0, 0]);
    const filePath = await createTempFile(img1, 'png');
    await app.uploadFile(filePath);
    await app.expectClipCount(1);

    // Overwrite the temp file with different content
    const img2 = generateTestImage(50, 50, [0, 255, 0]);
    fs.writeFileSync(filePath, img2);

    // Upload again via JS — should trigger conflict dialog
    // Note: upload() is async and will block on the conflict dialog promise,
    // so we fire it without awaiting and interact with the dialog separately.
    const filename = path.basename(filePath);
    const base64 = img2.toString('base64');
    const uploadPromise = app.page.evaluate(async ({ name, data }: { name: string; data: string }) => {
      // @ts-ignore
      await upload([{ name, content_type: 'image/png', data }]);
    }, { name: filename, data: base64 });

    // Wait for the conflict dialog to appear (inert attribute removed)
    const dialog = app.page.locator(selectors.conflict.dialog);
    await expect(dialog).not.toHaveAttribute('inert', '', { timeout: 5000 });
    await expect(dialog).toBeVisible();

    // Verify the dialog shows the filename in the file list
    const fileList = app.page.locator(selectors.conflict.fileList);
    await expect(fileList).toBeVisible();

    // Close dialog via Skip to clean up
    await app.page.locator(selectors.conflict.skipButton).click();
    await uploadPromise;
  });

  test('overwrite replaces existing clip', async ({ app }) => {
    // Upload first file
    const img1 = generateTestImage(50, 50, [255, 0, 0]);
    const filePath = await createTempFile(img1, 'png');
    await app.uploadFile(filePath);
    await app.expectClipCount(1);

    // Overwrite with different content
    const img2 = generateTestImage(50, 50, [0, 0, 255]);
    fs.writeFileSync(filePath, img2);
    const filename = path.basename(filePath);
    const base64 = img2.toString('base64');

    // Upload again — trigger conflict dialog
    const uploadPromise = app.page.evaluate(async ({ name, data }: { name: string; data: string }) => {
      // @ts-ignore
      await upload([{ name, content_type: 'image/png', data }]);
    }, { name: filename, data: base64 });

    // Wait for conflict dialog
    const dialog = app.page.locator(selectors.conflict.dialog);
    await expect(dialog).not.toHaveAttribute('inert', '', { timeout: 5000 });

    // Click Overwrite
    await app.page.locator(selectors.conflict.overwriteButton).click();
    await uploadPromise;

    // Wait for dialog to close
    await expect(dialog).toHaveAttribute('inert', '', { timeout: 5000 });

    // Should still have 1 clip (overwritten, not duplicated)
    await app.expectClipCount(1);
  });

  test('keep both creates duplicate', async ({ app }) => {
    // Upload first file
    const img1 = generateTestImage(50, 50, [255, 0, 0]);
    const filePath = await createTempFile(img1, 'png');
    await app.uploadFile(filePath);
    await app.expectClipCount(1);

    // Overwrite with different content
    const img2 = generateTestImage(60, 60, [0, 255, 0]);
    fs.writeFileSync(filePath, img2);
    const filename = path.basename(filePath);
    const base64 = img2.toString('base64');

    // Upload again — trigger conflict dialog
    const uploadPromise = app.page.evaluate(async ({ name, data }: { name: string; data: string }) => {
      // @ts-ignore
      await upload([{ name, content_type: 'image/png', data }]);
    }, { name: filename, data: base64 });

    // Wait for conflict dialog
    const dialog = app.page.locator(selectors.conflict.dialog);
    await expect(dialog).not.toHaveAttribute('inert', '', { timeout: 5000 });

    // Click Keep Both
    await app.page.locator(selectors.conflict.keepButton).click();
    await uploadPromise;

    // Wait for dialog to close
    await expect(dialog).toHaveAttribute('inert', '', { timeout: 5000 });

    // Should now have 2 clips
    await app.expectClipCount(2);
  });

  test('skip does not upload', async ({ app }) => {
    // Upload first file
    const img1 = generateTestImage(50, 50, [255, 0, 0]);
    const filePath = await createTempFile(img1, 'png');
    await app.uploadFile(filePath);
    await app.expectClipCount(1);

    // Overwrite with different content
    const img2 = generateTestImage(70, 70, [0, 0, 255]);
    fs.writeFileSync(filePath, img2);
    const filename = path.basename(filePath);
    const base64 = img2.toString('base64');

    // Upload again — trigger conflict dialog
    const uploadPromise = app.page.evaluate(async ({ name, data }: { name: string; data: string }) => {
      // @ts-ignore
      await upload([{ name, content_type: 'image/png', data }]);
    }, { name: filename, data: base64 });

    // Wait for conflict dialog
    const dialog = app.page.locator(selectors.conflict.dialog);
    await expect(dialog).not.toHaveAttribute('inert', '', { timeout: 5000 });

    // Click Skip
    await app.page.locator(selectors.conflict.skipButton).click();
    await uploadPromise;

    // Wait for dialog to close
    await expect(dialog).toHaveAttribute('inert', '', { timeout: 5000 });

    // Should still have 1 clip
    await app.expectClipCount(1);
  });

  test('folder drop with duplicate loose file shows conflict dialog', async ({ app }) => {
    // Enable folder mode
    await app.toggleFolderMode();

    // First drop: a folder "assets" with one file
    const img1Base64 = generateTestImage(50, 50, [255, 0, 0]).toString('base64');

    const firstEntries = [
      { relativePath: 'assets/icon.png', name: 'icon.png', data: img1Base64, contentType: 'image/png' },
    ];

    await app.page.evaluate(async (entries) => {
      const folderFiles = entries.map(e => {
        const binary = atob(e.data);
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
        const file = new File([bytes], e.name, { type: e.contentType });
        return { relativePath: e.relativePath, file };
      });
      const dirPaths = new Set(
        entries.map(e => e.relativePath.split('/').slice(0, -1).join('/'))
          .filter(p => p.length > 0)
      );
      // @ts-ignore
      await window.__testHelpers.handleFolderDrop(folderFiles, [], dirPaths);
    }, firstEntries);

    await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });

    // Navigate into assets folder and verify 1 clip
    await app.clickFolder('assets');
    await app.expectClipCount(1);

    // Second drop: a loose file with the same name but different content,
    // dropped while inside the "assets" folder. This tests conflict detection
    // for loose files in handleFolderDrop (they get the current folder's tag).
    const img2Base64 = generateTestImage(50, 50, [0, 255, 0]).toString('base64');

    // handleFolderDrop will block on the conflict dialog, so fire without awaiting
    const dropPromise = app.page.evaluate(async (data: { data: string }) => {
      const binary = atob(data.data);
      const bytes = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
      const file = new File([bytes], 'icon.png', { type: 'image/png' });
      // @ts-ignore
      await window.__testHelpers.handleFolderDrop([], [file], new Set());
    }, { data: img2Base64 });

    // Wait for conflict dialog to appear
    const dialog = app.page.locator(selectors.conflict.dialog);
    await expect(dialog).not.toHaveAttribute('inert', '', { timeout: 10000 });

    // Click Overwrite to resolve
    await app.page.locator(selectors.conflict.overwriteButton).click();

    // Wait for the drop promise to complete
    await dropPromise;

    // Wait for dialog to close
    await expect(dialog).toHaveAttribute('inert', '', { timeout: 5000 });

    await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });

    // Still in assets folder — should still have 1 clip (overwritten)
    await app.refreshClips();
    await app.expectClipCount(1);
  });

  // The WebView names clipboard bitmap data "image.png" on every paste.
  const paste = async (app: any, rgb: [number, number, number]) => {
    const base64 = generateTestImage(50, 50, rgb).toString('base64');
    await app.page.evaluate(async (data: string) => {
      const binary = atob(data);
      const bytes = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
      // Generic clipboard name, exactly what the WebView assigns to a screen capture.
      const file = new File([bytes], 'image.png', { type: 'image/png' });
      // @ts-ignore - handlePastedFiles awaits the upload, so this resolves once committed
      await window.__testHelpers.handlePastedFiles([file]);
    }, base64);
  };

  test('different screen captures land as distinct clips without a conflict prompt', async ({ app }) => {
    await paste(app, [255, 0, 0]);
    await app.expectClipCount(1);

    // Second paste: different bytes, same generic "image.png" name.
    await paste(app, [0, 255, 0]);

    // No prompt — both land as distinct clips. (If the dialog had appeared, the
    // upload would block and the count would never reach 2.)
    await app.expectClipCount(2);
    const dialog = app.page.locator(selectors.conflict.dialog);
    await expect(dialog).toHaveAttribute('inert', '');

    // Both clips were renamed to content-addressed pasted_image_* names.
    const renamed = app.page.locator('#gallery > li[data-filename^="pasted_image_"]');
    await expect(renamed).toHaveCount(2);
  });

  test('re-pasting the same capture is deduped, not duplicated', async ({ app }) => {
    await paste(app, [0, 0, 255]);
    await app.expectClipCount(1);

    // Accidental second Cmd+V of the identical capture — same bytes, same generic name.
    await paste(app, [0, 0, 255]);

    // Content-addressed naming collapses it into the identical-content skip:
    // still one clip, and no conflict prompt.
    await app.expectToast('skipped');
    await app.expectClipCount(1);
    const dialog = app.page.locator(selectors.conflict.dialog);
    await expect(dialog).toHaveAttribute('inert', '');
  });

  test('pastedImageName is content-addressed for generic names and a no-op otherwise', async ({ app }) => {
    const results = await app.page.evaluate(async () => {
      // @ts-ignore
      const fn = window.__testHelpers.pastedImageName;
      const dataA = btoa('screen-capture-A');
      const dataB = btoa('screen-capture-B');
      return {
        png: await fn({ name: 'image.png', data: dataA }),
        jpeg: await fn({ name: 'image.jpeg', data: dataA }),
        jpgUpper: await fn({ name: 'image.JPG', data: dataA }),
        sameAgain: await fn({ name: 'image.png', data: dataA }),
        different: await fn({ name: 'image.png', data: dataB }),
        realName: await fn({ name: 'quarterly-report.png', data: dataA }),
        pdf: await fn({ name: 'document.pdf', data: dataA }),
      };
    });

    expect(results.png).toMatch(/^pasted_image_[0-9a-f]{16}\.png$/);
    expect(results.jpeg).toMatch(/^pasted_image_[0-9a-f]{16}\.jpeg$/);
    expect(results.jpgUpper).toMatch(/^pasted_image_[0-9a-f]{16}\.jpg$/); // extension lowercased
    // Identical bytes → identical name (the dedup key); different bytes → different name.
    expect(results.sameAgain).toBe(results.png);
    expect(results.different).not.toBe(results.png);
    // Meaningful names are left untouched so they keep conflict detection.
    expect(results.realName).toBe('quarterly-report.png');
    expect(results.pdf).toBe('document.pdf');
  });

  test.afterEach(async ({ app }) => {
    // Exit folder mode if active
    const btn = app.page.locator('[data-testid="folder-mode-button"]');
    const pressed = await btn.getAttribute('aria-pressed');
    if (pressed === 'true') {
      await btn.click();
    }
    await app.clearTagFilters();
    await app.deleteAllTags();
  });
});
