import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Rename Clip Validation', () => {
  // BUG: RenameClip accepts filenames containing path separators and path
  // traversal sequences. The backend only checks for empty names but does not
  // validate for characters that are unsafe in filenames. These names end up
  // stored in the database and are later used in ZIP archive creation
  // (BulkDownloadToFile), temp file materialisation, and HTTP content
  // disposition headers, which can cause path traversal vulnerabilities or
  // broken file operations.

  test('should reject filename containing forward slashes', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const originalName = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.expectClipVisible(originalName);

    // Try to rename via the backend API directly (bypasses frontend validation)
    const result = await app.page.evaluate(async (name: string) => {
      // @ts-ignore - Wails runtime
      const clips = await window.go.main.App.GetClips(false, [], [], '', '');
      const clip = clips.find((c: any) => c.filename === name);
      if (!clip) return { success: false, error: 'clip not found' };
      try {
        // @ts-ignore
        await window.go.main.App.RenameClip(clip.id, 'path/to/evil.png');
        return { success: true };
      } catch (err: any) {
        return { success: false, error: err.message || String(err) };
      }
    }, originalName);

    // A filename with slashes is a path, not a filename. The backend should
    // reject it to prevent issues in downstream operations.
    expect(result.success).toBe(false);

    // Original name should be unchanged in the database
    const dbFilename = await app.page.evaluate(async (name: string) => {
      // @ts-ignore
      const clips = await window.go.main.App.GetClips(false, [], [], '', '');
      const clip = clips.find((c: any) => c.filename === name);
      return clip?.filename ?? null;
    }, originalName);
    expect(dbFilename).toBe(originalName);
  });

  test('should reject filename with parent directory traversal', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const originalName = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.expectClipVisible(originalName);

    // Attempt to rename to a path traversal sequence
    const result = await app.page.evaluate(async (name: string) => {
      // @ts-ignore
      const clips = await window.go.main.App.GetClips(false, [], [], '', '');
      const clip = clips.find((c: any) => c.filename === name);
      if (!clip) return { success: false, error: 'clip not found' };
      try {
        // @ts-ignore
        await window.go.main.App.RenameClip(clip.id, '../../../etc/passwd');
        return { success: true };
      } catch (err: any) {
        return { success: false, error: err.message || String(err) };
      }
    }, originalName);

    // '..' is not a valid filename component. The backend should reject it.
    expect(result.success).toBe(false);
  });

  test('should reject filename that is just ".."', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const originalName = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.expectClipVisible(originalName);

    const result = await app.page.evaluate(async (name: string) => {
      // @ts-ignore
      const clips = await window.go.main.App.GetClips(false, [], [], '', '');
      const clip = clips.find((c: any) => c.filename === name);
      if (!clip) return { success: false, error: 'clip not found' };
      try {
        // @ts-ignore
        await window.go.main.App.RenameClip(clip.id, '..');
        return { success: true };
      } catch (err: any) {
        return { success: false, error: err.message || String(err) };
      }
    }, originalName);

    expect(result.success).toBe(false);
  });

  test('should reject filename containing backslashes', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const originalName = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.expectClipVisible(originalName);

    const result = await app.page.evaluate(async (name: string) => {
      // @ts-ignore
      const clips = await window.go.main.App.GetClips(false, [], [], '', '');
      const clip = clips.find((c: any) => c.filename === name);
      if (!clip) return { success: false, error: 'clip not found' };
      try {
        // @ts-ignore
        await window.go.main.App.RenameClip(clip.id, 'path\\to\\evil.png');
        return { success: true };
      } catch (err: any) {
        return { success: false, error: err.message || String(err) };
      }
    }, originalName);

    expect(result.success).toBe(false);
  });

  test('should reject filename containing null bytes', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const originalName = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.expectClipVisible(originalName);

    const result = await app.page.evaluate(async (name: string) => {
      // @ts-ignore
      const clips = await window.go.main.App.GetClips(false, [], [], '', '');
      const clip = clips.find((c: any) => c.filename === name);
      if (!clip) return { success: false, error: 'clip not found' };
      try {
        // @ts-ignore
        await window.go.main.App.RenameClip(clip.id, 'evil\x00name.png');
        return { success: true };
      } catch (err: any) {
        return { success: false, error: err.message || String(err) };
      }
    }, originalName);

    // Null bytes in filenames are never valid and can cause truncation issues
    // in C-based file system APIs.
    expect(result.success).toBe(false);
  });

  test('should reject rename via UI when name contains slashes', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const originalName = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.expectClipVisible(originalName);

    // Open the rename dialog via UI
    await app.openCardMenu(originalName);
    await app.page.locator(selectors.cardMenu.rename).click();

    const input = app.page.locator(selectors.prompt.input);
    await expect(input).toBeVisible();

    // Type a name with path separators
    await input.fill('../../malicious.png');
    await app.page.locator(selectors.prompt.saveButton).click();

    // The rename should fail with an error toast, not succeed
    await expect(app.page.locator(selectors.toast.container)).toContainText(
      /[Ff]ailed|[Ii]nvalid|[Ee]rror|cannot/
    );

    // The original name should still be present in the gallery
    await app.expectClipVisible(originalName);
  });
});
