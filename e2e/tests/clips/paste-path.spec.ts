import { test, expect } from '../../fixtures/test-fixtures';
import { createTempDir, createTempFile, generateTestImage, generateTestText } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as fs from 'fs/promises';
import * as path from 'path';

/**
 * Pasting text that names a file on disk is ambiguous — the user may want the
 * path as a text clip, or the file it points at. The app asks, but only when
 * the path actually resolves.
 */
test.describe('Pasted File Paths', () => {
  // Fires the paste through the same entry point the paste event uses. The
  // promise stays pending while the dialog is open, so callers resolve the
  // dialog first and await it after.
  const pasteText = (app: any, text: string) =>
    app.page.evaluate((t: string) => (window as any).__testHelpers.handleText(t), text);

  const dialog = (app: any) => app.page.locator(selectors.pathPaste.dialog);

  test('offers the file behind a pasted path and imports it', async ({ app }) => {
    const filePath = await createTempFile(generateTestImage(40, 40, 'blue'), 'png');
    const filename = path.basename(filePath);

    const pastePromise = pasteText(app, filePath);

    await expect(dialog(app)).toHaveClass(/opacity-100/);
    await expect(app.page.locator(selectors.pathPaste.message))
      .toHaveText(/path to a file on this computer/);
    await expect(app.page.locator(selectors.pathPaste.fileList)).toContainText(filename);
    await expect(app.page.locator(selectors.pathPaste.fileButton)).toHaveText('Add File');

    await app.page.locator(selectors.pathPaste.fileButton).click();
    await pastePromise;

    // The clip is the file itself, not the path text.
    await app.refreshClips();
    await app.expectClipCount(1);
    await app.expectClipVisible(filename);
  });

  test('adds the path as a text clip when asked', async ({ app }) => {
    const filePath = await createTempFile(generateTestText('path-as-text'), 'txt');

    const pastePromise = pasteText(app, filePath);

    await expect(dialog(app)).toHaveClass(/opacity-100/);
    await app.page.locator(selectors.pathPaste.textButton).click();
    await pastePromise;

    await app.refreshClips();
    await app.expectClipCount(1);
    const textClip = app.page.locator('#gallery > li[data-filename^="pasted_text_"]');
    await expect(textClip).toHaveCount(1);
  });

  test('cancelling adds nothing at all', async ({ app }) => {
    const filePath = await createTempFile(generateTestText('cancelled'), 'txt');

    const pastePromise = pasteText(app, filePath);

    await expect(dialog(app)).toHaveClass(/opacity-100/);
    await app.page.locator(selectors.pathPaste.cancelButton).click();
    await pastePromise;

    await app.refreshClips();
    await app.expectClipCount(0);
  });

  test('Escape cancels the prompt', async ({ app }) => {
    const filePath = await createTempFile(generateTestText('escaped'), 'txt');

    const pastePromise = pasteText(app, filePath);

    await expect(dialog(app)).toHaveClass(/opacity-100/);
    await app.page.keyboard.press('Escape');
    await pastePromise;

    await expect(dialog(app)).toHaveAttribute('inert', '');
    await app.refreshClips();
    await app.expectClipCount(0);
  });

  test('imports every file when several paths are pasted at once', async ({ app }) => {
    const fileA = await createTempFile(generateTestImage(30, 30, 'red'), 'png');
    const fileB = await createTempFile(generateTestImage(30, 30, 'green'), 'png');

    const pastePromise = pasteText(app, `${fileA}\n${fileB}\n`);

    await expect(dialog(app)).toHaveClass(/opacity-100/);
    await expect(app.page.locator(selectors.pathPaste.message)).toHaveText(/2 paths/);
    await expect(app.page.locator(selectors.pathPaste.fileButton)).toHaveText('Add 2 Files');

    await app.page.locator(selectors.pathPaste.fileButton).click();
    await pastePromise;

    await app.refreshClips();
    await app.expectClipCount(2);
    await app.expectClipVisible(path.basename(fileA));
    await app.expectClipVisible(path.basename(fileB));
  });

  test('resolves paths escaped the way the platform escapes them', async ({ app }) => {
    const dir = await createTempDir();
    const name = 'my report (final).png';
    const filePath = path.join(dir, name);
    await fs.writeFile(filePath, generateTestImage(20, 20, 'purple'));

    // A terminal drag backslash-escapes shell metacharacters on POSIX. Windows
    // has no such convention — normalizePastedPath skips unescaping there
    // because a backslash is a separator — so quoting is the equivalent form.
    const pasted = process.platform === 'win32'
      ? `"${filePath}"`
      : filePath.replace(/[ ()]/g, c => `\\${c}`);

    const pastePromise = pasteText(app, pasted);

    await expect(dialog(app)).toHaveClass(/opacity-100/);
    await app.page.locator(selectors.pathPaste.fileButton).click();
    await pastePromise;

    await app.refreshClips();
    await app.expectClipVisible(name);
  });

  test('does not prompt for a path that does not exist', async ({ app }) => {
    await pasteText(app, '/tmp/definitely-not-a-real-file-9f3a2b.png');

    await expect(dialog(app)).toHaveAttribute('inert', '');
    await app.refreshClips();
    await app.expectClipCount(1);
    await expect(app.page.locator('#gallery > li[data-filename^="pasted_text_"]')).toHaveCount(1);
  });

  test('does not prompt for a directory', async ({ app }) => {
    const dir = await createTempDir();

    await pasteText(app, dir);

    await expect(dialog(app)).toHaveAttribute('inert', '');
    await app.refreshClips();
    await app.expectClipCount(1);
    await expect(app.page.locator('#gallery > li[data-filename^="pasted_text_"]')).toHaveCount(1);
  });

  test('does not prompt when only some pasted lines are real files', async ({ app }) => {
    const filePath = await createTempFile(generateTestText('mixed'), 'txt');

    await pasteText(app, `${filePath}\n/tmp/definitely-not-a-real-file-9f3a2b.txt`);

    await expect(dialog(app)).toHaveAttribute('inert', '');
    await app.refreshClips();
    await app.expectClipCount(1);
    await expect(app.page.locator('#gallery > li[data-filename^="pasted_text_"]')).toHaveCount(1);
  });

  test.describe('Settings preference', () => {
    // Picks the preference through the real Settings UI.
    const choose = async (app: any, value: string) => {
      await app.openSettingsModal();
      await app.page.locator(selectors.settings.pastePathSelect).selectOption(value);
      await app.closeSettingsModal();
      expect(await app.page.evaluate(() => (window as any).__testHelpers.getPastePathBehavior()))
        .toBe(value);
    };

    test('defaults to asking', async ({ app }) => {
      await app.openSettingsModal();
      await expect(app.page.locator(selectors.settings.pastePathSelect)).toHaveValue('ask');
      await app.closeSettingsModal();
    });

    test('"keep the path as text" skips the prompt entirely', async ({ app }) => {
      const filePath = await createTempFile(generateTestImage(30, 30, 'red'), 'png');
      await choose(app, 'text');

      await pasteText(app, filePath);

      await expect(dialog(app)).toHaveAttribute('inert', '');
      await app.refreshClips();
      await app.expectClipCount(1);
      await expect(app.page.locator('#gallery > li[data-filename^="pasted_text_"]')).toHaveCount(1);
    });

    test('"add the file it points to" imports without prompting', async ({ app }) => {
      const filePath = await createTempFile(generateTestImage(30, 30, 'cyan'), 'png');
      await choose(app, 'file');

      await pasteText(app, filePath);

      await expect(dialog(app)).toHaveAttribute('inert', '');
      await app.refreshClips();
      await app.expectClipCount(1);
      await app.expectClipVisible(path.basename(filePath));
    });

    // Two fast Cmd+V of the same path must not both clear the duplicate check
    // before either clip is committed.
    test('a double paste of the same path lands as one clip', async ({ app }) => {
      const filePath = await createTempFile(generateTestImage(30, 30, 'yellow'), 'png');
      await choose(app, 'file');

      await Promise.all([pasteText(app, filePath), pasteText(app, filePath)]);

      await app.refreshClips();
      await app.expectClipCount(1);
      await app.expectClipVisible(path.basename(filePath));
    });

    test('"add the file it points to" still pastes text when the path is not a file', async ({ app }) => {
      await choose(app, 'file');

      await pasteText(app, '/tmp/definitely-not-a-real-file-9f3a2b.png');

      await expect(dialog(app)).toHaveAttribute('inert', '');
      await app.refreshClips();
      await app.expectClipCount(1);
      await expect(app.page.locator('#gallery > li[data-filename^="pasted_text_"]')).toHaveCount(1);
    });

    // The paste listener is live before the stored preference arrives, so a
    // paste during startup must wait for it rather than fall back to `ask`.
    test('a paste racing startup still waits for the stored preference', async ({ app }) => {
      const filePath = await createTempFile(generateTestImage(30, 30, 'green'), 'png');
      await choose(app, 'text');

      const result = await app.page.evaluate(async (p: string) => {
        const w = window as any;
        // Re-arm the readiness barrier the way a cold start leaves it: unsettled,
        // with the stored value not yet applied.
        const behaviorDuringPaste: string[] = [];
        const originalWhenReady = w.whenPastePathBehaviorReady;
        let release: () => void;
        const gate = new Promise<void>(r => { release = r; });
        w.whenPastePathBehaviorReady = async () => {
          await gate;
          return originalWhenReady();
        };

        const pastePromise = w.__testHelpers.handleText(p);
        // Nothing may have happened yet — the paste is parked on the barrier.
        behaviorDuringPaste.push(document.getElementById('path-paste-dialog')!.getAttribute('inert') ?? 'not-inert');
        release!();
        await pastePromise;
        w.whenPastePathBehaviorReady = originalWhenReady;
        return behaviorDuringPaste;
      }, filePath);

      expect(result).toEqual(['']); // dialog still inert while parked

      // Once released it honoured `text`: no prompt, path stored as text.
      await expect(dialog(app)).toHaveAttribute('inert', '');
      await app.refreshClips();
      await app.expectClipCount(1);
      await expect(app.page.locator('#gallery > li[data-filename^="pasted_text_"]')).toHaveCount(1);
    });

    test('the choice is persisted and shown again when Settings reopens', async ({ app }) => {
      await choose(app, 'file');

      // Re-read from the backend, the way a fresh launch would.
      const stored = await app.page.evaluate(
        () => (window as any).go.main.App.GetSetting('paste_path_behavior'));
      expect(stored).toBe('file');

      await app.openSettingsModal();
      await expect(app.page.locator(selectors.settings.pastePathSelect)).toHaveValue('file');
      await app.closeSettingsModal();
    });
  });

  test('pathCandidatesFromText only accepts absolute path shapes', async ({ app }) => {
    const results = await app.page.evaluate(() => {
      const fn = (window as any).__testHelpers.pathCandidatesFromText;
      return {
        absolute: fn('/Users/me/photo.png'),
        quoted: fn('"/Users/me/my photo.png"'),
        tilde: fn('~/photo.png'),
        tildeBackslash: fn('~\\Documents\\photo.png'), // how Windows writes it
        fileUrl: fn('file:///Users/me/photo.png'),
        windows: fn('C:\\Users\\me\\photo.png'),
        multiline: fn('/a/one.png\n/a/two.png\n'),
        padded: fn('   /Users/me/photo.png  '),

        relative: fn('photo.png'),
        dotted: fn('./photo.png'),
        httpUrl: fn('https://example.com/photo.png'),
        prose: fn('Check /etc/hosts for the entry'),
        mixed: fn('/a/one.png\nsome prose'),
        empty: fn(''),
        blank: fn('   \n  '),
        tooManyLines: fn(Array.from({ length: 17 }, (_, i) => `/a/${i}.png`).join('\n')),
        tooLong: fn('/a/' + 'x'.repeat(5000)),
        notAString: fn(null),
      };
    });

    expect(results.absolute).toEqual(['/Users/me/photo.png']);
    expect(results.quoted).toEqual(['"/Users/me/my photo.png"']);
    expect(results.tilde).toEqual(['~/photo.png']);
    expect(results.tildeBackslash).toEqual(['~\\Documents\\photo.png']);
    expect(results.fileUrl).toEqual(['file:///Users/me/photo.png']);
    expect(results.windows).toEqual(['C:\\Users\\me\\photo.png']);
    expect(results.multiline).toEqual(['/a/one.png', '/a/two.png']);
    expect(results.padded).toEqual(['/Users/me/photo.png']);

    expect(results.relative).toBeNull();
    expect(results.dotted).toBeNull();
    expect(results.httpUrl).toBeNull();
    expect(results.prose).toBeNull();
    expect(results.mixed).toBeNull();
    expect(results.empty).toBeNull();
    expect(results.blank).toBeNull();
    expect(results.tooManyLines).toBeNull();
    expect(results.tooLong).toBeNull();
    expect(results.notAString).toBeNull();
  });
});
