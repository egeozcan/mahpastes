import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestText,
} from '../../helpers/test-data';
import * as path from 'path';
import * as fs from 'fs/promises';

test.describe('Backup & Restore', () => {
  test.describe('Backup Creation', () => {
    test('should create backup with clips', async ({ app, tempDir }) => {
      // Upload some test clips
      const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const text = await createTempFile(generateTestText('backup-test'), 'txt');

      await app.uploadFiles([image1, image2, text]);
      await app.expectClipCount(3);

      // Create a tag
      await app.createTag('TestTag');

      // Create backup
      const backupPath = path.join(tempDir, 'test-backup.zip');
      await app.page.evaluate(async (destPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.CreateBackup(destPath);
      }, backupPath);

      // Verify backup file exists
      const stat = await fs.stat(backupPath);
      expect(stat.size).toBeGreaterThan(0);
    });

    test('should create backup file that is a valid ZIP', async ({ app, tempDir }) => {
      // Upload a clip
      const image = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(image);
      await app.expectClipCount(1);

      // Create backup
      const backupPath = path.join(tempDir, 'test-backup.zip');
      await app.page.evaluate(async (destPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.CreateBackup(destPath);
      }, backupPath);

      // Read the file and verify it starts with ZIP magic bytes (PK)
      const fileBuffer = await fs.readFile(backupPath);
      expect(fileBuffer[0]).toBe(0x50); // 'P'
      expect(fileBuffer[1]).toBe(0x4B); // 'K'
    });
  });

  test.describe('Restore', () => {
    test('should restore clips from backup', async ({ app, tempDir }) => {
      // Create initial data
      const image = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
      await app.uploadFile(image);
      await app.createTag('OriginalTag');
      await app.expectClipCount(1);

      // Create backup
      const backupPath = path.join(tempDir, 'restore-test.zip');
      await app.page.evaluate(async (destPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.CreateBackup(destPath);
      }, backupPath);

      // Delete original data
      await app.deleteAllClips();
      await app.deleteAllTags();
      await app.expectClipCount(0);

      // Restore from backup
      const restoreResult = await app.page.evaluate(async (backupFile) => {
        try {
          // @ts-ignore - Wails runtime
          await window.go.main.App.ConfirmRestoreBackup(backupFile);
          return { success: true };
        } catch (e: any) {
          return { success: false, error: e.message || String(e) };
        }
      }, backupPath);

      // Log restore result for debugging
      if (!restoreResult.success) {
        console.log('Restore failed:', restoreResult.error);
      }
      expect(restoreResult.success).toBe(true);

      // Verify data was restored by checking via API (more reliable than UI)
      const clipsAfterRestore = await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        return await window.go.main.App.GetClips(false, []);
      });
      expect(clipsAfterRestore.length).toBe(1);

      const tagsAfterRestore = await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        return await window.go.main.App.GetTags();
      });
      expect(tagsAfterRestore.length).toBe(1);
      expect(tagsAfterRestore[0].name).toBe('OriginalTag');
    });

    test('should replace existing data on restore', async ({ app, tempDir }) => {
      // Create initial data
      const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      await app.uploadFile(image1);
      await app.createTag('Tag1');

      // Create backup
      const backupPath = path.join(tempDir, 'replace-test.zip');
      await app.page.evaluate(async (destPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.CreateBackup(destPath);
      }, backupPath);

      // Add more data (should be replaced on restore)
      const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const image3 = await createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png');
      await app.uploadFiles([image2, image3]);
      await app.createTag('Tag2');
      await app.createTag('Tag3');
      await app.expectClipCount(3);

      // Restore from backup (should replace with original 1 clip)
      await app.page.evaluate(async (backupFile) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.ConfirmRestoreBackup(backupFile);
      }, backupPath);

      // Verify data was restored by checking via API (more reliable than UI)
      const clipsAfterRestore = await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        return await window.go.main.App.GetClips(false, []);
      });
      expect(clipsAfterRestore.length).toBe(1);

      const tagsAfterRestore = await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        return await window.go.main.App.GetTags();
      });
      expect(tagsAfterRestore.length).toBe(1);
      expect(tagsAfterRestore[0].name).toBe('Tag1');
    });

    test('should restore clips with tags attached', async ({ app, tempDir }) => {
      // Create clip with tag
      const image = await createTempFile(generateTestImage(50, 50, [255, 128, 0]), 'png');
      await app.uploadFile(image);
      await app.createTag('ImportantTag');
      await app.addTagToClip(path.basename(image), 'ImportantTag');

      // Verify tag is attached
      await app.expectClipHasTag(path.basename(image), 'ImportantTag');

      // Create backup
      const backupPath = path.join(tempDir, 'tagged-test.zip');
      await app.page.evaluate(async (destPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.CreateBackup(destPath);
      }, backupPath);

      // Delete everything
      await app.deleteAllClips();
      await app.deleteAllTags();
      await app.expectClipCount(0);

      // Restore
      await app.page.evaluate(async (backupFile) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.ConfirmRestoreBackup(backupFile);
      }, backupPath);

      // Verify data was restored by checking via API (more reliable than UI)
      const clipsAfterRestore = await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        return await window.go.main.App.GetClips(false, []);
      });
      expect(clipsAfterRestore.length).toBe(1);

      const tagsAfterRestore = await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        return await window.go.main.App.GetTags();
      });
      expect(tagsAfterRestore.length).toBe(1);
      expect(tagsAfterRestore[0].name).toBe('ImportantTag');
    });
  });

  test.describe('Error Handling', () => {
    test('should fail gracefully with non-existent backup file', async ({ app }) => {
      const result = await app.page.evaluate(async () => {
        try {
          // @ts-ignore - Wails runtime
          await window.go.main.App.ConfirmRestoreBackup('/nonexistent/path/backup.zip');
          return { success: true };
        } catch (e: any) {
          return { success: false, error: e.message || String(e) };
        }
      });

      expect(result.success).toBe(false);
    });

    test('should fail gracefully with invalid backup file', async ({ app, tempDir }) => {
      // Create an invalid ZIP file
      const invalidPath = path.join(tempDir, 'invalid.zip');
      await fs.writeFile(invalidPath, 'not a zip file');

      // Try to restore
      const result = await app.page.evaluate(async (backupFile) => {
        try {
          // @ts-ignore - Wails runtime
          await window.go.main.App.ConfirmRestoreBackup(backupFile);
          return { success: true };
        } catch (e: any) {
          return { success: false, error: e.message || String(e) };
        }
      }, invalidPath);

      expect(result.success).toBe(false);
    });
  });

  test.describe('Data Integrity', () => {
    test('should preserve text clip content through backup/restore', async ({ app, tempDir }) => {
      // Create a text clip with specific content
      const textContent = 'Special content for backup test: 日本語 🎉 "quotes" & symbols';
      const textFile = await createTempFile(textContent, 'txt');
      await app.uploadFile(textFile);
      await app.expectClipCount(1);

      // Get the clip data before backup
      const clipsBefore = await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        return await window.go.main.App.GetClips(false, []);
      });
      const clipIdBefore = clipsBefore[0].id;

      // Get the actual content
      const contentBefore = await app.page.evaluate(async (id) => {
        // @ts-ignore - Wails runtime
        const data = await window.go.main.App.GetClipData(id);
        return data.content;
      }, clipIdBefore);

      // Create backup
      const backupPath = path.join(tempDir, 'text-content-test.zip');
      await app.page.evaluate(async (destPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.CreateBackup(destPath);
      }, backupPath);

      // Delete and restore
      await app.deleteAllClips();
      await app.expectClipCount(0);

      await app.page.evaluate(async (backupFile) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.ConfirmRestoreBackup(backupFile);
      }, backupPath);

      // Get the restored clip content directly via API (no need to reload page)
      const clipsAfter = await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        return await window.go.main.App.GetClips(false, []);
      });
      expect(clipsAfter.length).toBe(1);

      const contentAfter = await app.page.evaluate(async (id) => {
        // @ts-ignore - Wails runtime
        const data = await window.go.main.App.GetClipData(id);
        return data.content;
      }, clipsAfter[0].id);

      expect(contentAfter).toBe(contentBefore);
    });

    test('should preserve image clip data through backup/restore', async ({ app, tempDir }) => {
      // Create an image clip
      const imageBuffer = generateTestImage(100, 100, [128, 64, 255]);
      const imageFile = await createTempFile(imageBuffer, 'png');
      await app.uploadFile(imageFile);
      await app.expectClipCount(1);

      // Get clip info before
      const clipsBefore = await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        return await window.go.main.App.GetClips(false, []);
      });
      const clipBefore = clipsBefore[0];

      // Create backup
      const backupPath = path.join(tempDir, 'image-content-test.zip');
      await app.page.evaluate(async (destPath) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.CreateBackup(destPath);
      }, backupPath);

      // Delete and restore
      await app.deleteAllClips();
      await app.expectClipCount(0);

      await app.page.evaluate(async (backupFile) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.ConfirmRestoreBackup(backupFile);
      }, backupPath);

      // Get the restored clip info directly via API (no need to reload page)
      const clipsAfter = await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        return await window.go.main.App.GetClips(false, []);
      });
      expect(clipsAfter.length).toBe(1);

      const clipAfter = clipsAfter[0];

      // Verify the clip type is preserved
      expect(clipAfter.mime_type).toBe(clipBefore.mime_type);
    });
  });
});
