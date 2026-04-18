import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestText,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Copy Actions', () => {
  test.describe('Card Menu - Copy File', () => {
    test('should show Copy File in card menu for image clips', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);

      await app.hoverCopySubmenu(filename);
      const copyFileBtn = app.page.locator(selectors.cardMenu.copyFile);
      await expect(copyFileBtn).toBeVisible();
    });

    test('should show Copy File in card menu for text clips', async ({ app }) => {
      const textPath = await createTempFile(generateTestText('copy-test'), 'txt');
      const filename = path.basename(textPath);
      await app.uploadFile(textPath);

      await app.hoverCopySubmenu(filename);
      const copyFileBtn = app.page.locator(selectors.cardMenu.copyFile);
      await expect(copyFileBtn).toBeVisible();
    });

    test('should show success toast when clicking Copy File', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);

      await app.hoverCopySubmenu(filename);
      await app.page.locator(selectors.cardMenu.copyFile).click();
      await app.expectToast('File copied to clipboard!');
    });

    test('should still copy file after drag preparation', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);

      const clip = await app.getClipByFilename(filename);
      const clipId = Number(await clip.getAttribute('data-id'));
      await app.page.evaluate(async (id) => {
        // @ts-ignore
        if (window.__testHelpers?.prepareDragForTest) {
          // @ts-ignore
          await window.__testHelpers.prepareDragForTest(id);
        }
      }, clipId);

      await app.hoverCopySubmenu(filename);
      await app.page.locator(selectors.cardMenu.copyFile).click();
      const toast = app.page.locator(selectors.toast.message);
      await expect(toast).toBeVisible();
      await expect(toast).toContainText(/File copied to clipboard!|Failed to copy file\./);
    });
  });

  test.describe('Card Menu - Copy Contents', () => {
    test('should show Copy Contents for text clips', async ({ app }) => {
      const textPath = await createTempFile(generateTestText('copy-contents'), 'txt');
      const filename = path.basename(textPath);
      await app.uploadFile(textPath);

      await app.hoverCopySubmenu(filename);
      const copyContentsBtn = app.page.locator(selectors.cardMenu.copyContents);
      await expect(copyContentsBtn).toBeVisible();
    });

    test('should show Copy Contents for image clips', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);

      await app.hoverCopySubmenu(filename);
      const copyContentsBtn = app.page.locator(selectors.cardMenu.copyContents);
      await expect(copyContentsBtn).toBeVisible();
    });

    test('should NOT show Copy Contents for binary clips (PDF)', async ({ app }) => {
      // Create a minimal PDF file
      const pdfContent = Buffer.from('%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n');
      const pdfPath = await createTempFile(pdfContent, 'pdf');
      const filename = path.basename(pdfPath);
      await app.uploadFile(pdfPath);

      await app.hoverCopySubmenu(filename);
      const copyContentsBtn = app.page.locator(selectors.cardMenu.copyContents);
      await expect(copyContentsBtn).not.toBeVisible();
    });

    test('should show success toast when clicking Copy Contents on text', async ({ app }) => {
      const textPath = await createTempFile(generateTestText('copy-toast'), 'txt');
      const filename = path.basename(textPath);
      await app.uploadFile(textPath);

      await app.hoverCopySubmenu(filename);
      await app.page.locator(selectors.cardMenu.copyContents).click();
      await app.expectToast('Contents copied to clipboard!');
    });
  });

  test.describe('Lightbox Menu', () => {
    test('should show Copy File and Copy Contents in lightbox actions for images', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);

      // Open lightbox
      await app.openLightbox(filename);

      // Open the file actions menu (now uses shared ContextMenu)
      const trigger = app.page.locator('#lightbox-file-menu-trigger');
      await trigger.click();
      await app.page.locator(selectors.cardMenu.dropdown).waitFor({ state: 'visible', timeout: 5000 });

      // Open Copy submenu via dispatchEvent since the lightbox backdrop intercepts pointer events
      const copyTrigger = app.page.locator(selectors.cardMenu.copyTrigger);
      await copyTrigger.dispatchEvent('mouseenter');
      await app.page.locator(selectors.cardMenu.submenu).waitFor({ state: 'visible', timeout: 3000 });

      // Verify both actions are present in the submenu
      const copyFileBtn = app.page.locator(selectors.cardMenu.copyFile);
      const copyContentsBtn = app.page.locator(selectors.cardMenu.copyContents);
      await expect(copyFileBtn).toBeVisible();
      await expect(copyContentsBtn).toBeVisible();
    });
  });

  test.describe('Keyboard Shortcuts', () => {
    test('Cmd+C in lightbox should copy image contents', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);

      await app.openLightbox(filename);
      await app.page.keyboard.press('ControlOrMeta+c');
      await app.expectToast('Contents copied to clipboard!');
    });

    test('Cmd+C with clips selected should copy files to clipboard', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      const filename1 = path.basename(imagePath1);
      const filename2 = path.basename(imagePath2);
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);

      await app.selectClips([filename1, filename2]);
      // Blur focus so the shortcut engine doesn't see an active checkbox and
      // suppress the 'c' key.  We use evaluate() to blur directly — clicking
      // the body can land on elements that re-acquire focus or dismiss the
      // selection.  After blur, document.activeElement is null/body, and
      // bulk-context shortcuts fire normally.
      await app.page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
      await app.page.keyboard.press('c');
      await app.expectToast('2 files copied to clipboard!');
    });

    test('Cmd+C with single clip selected should copy file to clipboard', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);

      await app.selectClip(filename);
      // Blur the checkbox so the shortcut engine doesn't suppress the key
      await app.page.locator('body').click({ position: { x: 0, y: 0 } });
      await app.page.keyboard.press('c');
      await app.expectToast('1 file copied to clipboard!');
    });
  });

  test.describe('Bulk Copy Button', () => {
    test('should show Copy button in bulk toolbar when clips selected', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);

      await app.selectClip(filename);
      const copyBtn = app.page.locator(selectors.bulk.copyButton);
      await expect(copyBtn).toBeVisible();
    });

    test('bulk Copy button should copy files to clipboard', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      const filename1 = path.basename(imagePath1);
      const filename2 = path.basename(imagePath2);
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);

      await app.selectClips([filename1, filename2]);
      await app.page.locator(selectors.bulk.copyButton).click();
      await app.expectToast('2 files copied to clipboard!');
    });
  });
});
