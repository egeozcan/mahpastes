import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestText,
  generateTestJSON,
  generateTestHTML,
  uniqueFilename,
} from '../../helpers/test-data';
import * as path from 'path';

test.describe('Clip Upload', () => {
  test.describe('File Upload', () => {
    test('should upload a single image file', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);

      await app.expectClipVisible(filename);
      await app.expectClipCount(1);
    });

    test('should upload multiple files at once', async ({ app }) => {
      const file1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const file2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const file3 = await createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png');

      await app.uploadFiles([file1, file2, file3]);

      await app.expectClipCount(3);
    });

    test('should upload a text file', async ({ app }) => {
      const textPath = await createTempFile(generateTestText('upload-test'), 'txt');
      const filename = path.basename(textPath);

      await app.uploadFile(textPath);

      await app.expectClipVisible(filename);
    });

    test('should upload a JSON file', async ({ app }) => {
      const jsonPath = await createTempFile(generateTestJSON(), 'json');
      const filename = path.basename(jsonPath);

      await app.uploadFile(jsonPath);

      await app.expectClipVisible(filename);
    });

    test('should upload an HTML file', async ({ app }) => {
      const htmlPath = await createTempFile(generateTestHTML(), 'html');
      const filename = path.basename(htmlPath);

      await app.uploadFile(htmlPath);

      await app.expectClipVisible(filename);
    });

    test('should upload different image formats', async ({ app }) => {
      // Test with PNG (we can only easily generate PNG without external deps)
      const png1 = await createTempFile(generateTestImage(100, 100, [255, 128, 0]), 'png');
      const png2 = await createTempFile(generateTestImage(100, 100, [128, 0, 255]), 'png');

      await app.uploadFiles([png1, png2]);

      await app.expectClipCount(2);
    });
  });

  test.describe('Expiration Timer', () => {
    test('should upload file with 5 minute expiration', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath, 5);

      await app.expectClipVisible(filename);
      // Check for expiration badge (shows "Temp" text)
      const clip = await app.getClipByFilename(filename);
      const badge = clip.locator('.absolute.top-2.left-2').first();
      await expect(badge).toBeVisible();
    });

    test('should upload file with no expiration (default)', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath, 0);

      await app.expectClipVisible(filename);
    });

    test('should upload file with 30 minute expiration', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');

      await app.uploadFile(imagePath, 30);

      await app.expectClipCount(1);
    });

    test('should upload file with 2 hour expiration', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');

      await app.uploadFile(imagePath, 120);

      await app.expectClipCount(1);
    });
  });

  test.describe('Paste from Clipboard', () => {
    test('should paste text content', async ({ app }) => {
      const text = generateTestText('paste-test');

      await app.pasteText(text);

      await app.expectClipCount(1);
    });

    test('should paste multiple text clips', async ({ app }) => {
      const text1 = generateTestText('paste-1');
      const text2 = generateTestText('paste-2');

      await app.pasteText(text1);
      await app.pasteText(text2);

      await app.expectClipCount(2);
    });

    test('should paste JSON content', async ({ app }) => {
      const json = generateTestJSON();

      await app.pasteText(json);

      await app.expectClipCount(1);
    });
  });

  test.describe('Upload Validation', () => {
    test('should handle empty file gracefully', async ({ app }) => {
      const emptyFile = await createTempFile('', 'txt');

      await app.uploadFile(emptyFile);

      // App should either reject or accept empty files
      // This test verifies no crash occurs
    });

    test('should upload large text content', async ({ app }) => {
      const largeText = 'x'.repeat(10000);
      const filePath = await createTempFile(largeText, 'txt');

      await app.uploadFile(filePath);

      await app.expectClipCount(1);
    });
  });
});
