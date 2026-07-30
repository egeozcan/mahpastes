import { test, expect } from '../../fixtures/test-fixtures';
import { createTempDir, createTempFile, generateTestImage } from '../../helpers/test-data';
import { writeFile } from 'fs/promises';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

// Behavior introduced or preserved by the CodeMirror migration. The existing
// text-editor and markdown suites cover the behavior that must not change; this
// file covers what the adapter adds and the traps the migration had to avoid.

test.describe('CodeMirror editor migration', () => {
  test('the retired textarea and mirrored highlight layer are gone', async ({ app }) => {
    const textPath = await createTempFile('anything', 'txt');
    await app.uploadFile(textPath);
    await app.openTextEditor(path.basename(textPath));

    // The highlight layer existed only because a textarea stops painting its
    // selection when focus moves into the find input. drawSelection replaces it.
    await expect(app.page.locator('#text-editor-textarea')).toHaveCount(0);
    await expect(app.page.locator('#text-editor-highlight-layer')).toHaveCount(0);
    await expect(app.page.locator(selectors.textEditor.editor)).toBeVisible();
    await expect(app.page.locator(selectors.textEditor.lineNumbers)).toBeVisible();
    await app.cancelTextEditor();
  });

  test('the stock CodeMirror search panel never appears', async ({ app }) => {
    const textPath = await createTempFile('alpha beta alpha', 'txt');
    await app.uploadFile(textPath);
    await app.openTextEditor(path.basename(textPath));

    // Every stock search command falls back to openSearchPanel() when no valid
    // query is set, so each of these is a chance for it to leak in.
    await app.page.locator(selectors.textEditor.findToggle).click();
    await app.page.locator(selectors.textEditor.findNextButton).click();
    await app.page.locator(selectors.textEditor.replaceButton).click();
    await app.page.locator(selectors.textEditor.replaceAllButton).click();
    await expect(app.page.locator(selectors.textEditor.stockSearchPanel)).toHaveCount(0);

    // Cmd/Ctrl+F inside the editor belongs to the app panel too.
    await app.page.locator(selectors.textEditor.findInput).fill('alpha');
    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+f');
    await expect(app.page.locator(selectors.textEditor.findPanel)).toBeVisible();
    await expect(app.page.locator(selectors.textEditor.findInput)).toBeFocused();
    await expect(app.page.locator(selectors.textEditor.stockSearchPanel)).toHaveCount(0);
    await app.cancelTextEditor();
  });

  test('undo and redo work through the editor', async ({ app }) => {
    const textPath = await createTempFile('base', 'txt');
    await app.uploadFile(textPath);
    await app.openTextEditor(path.basename(textPath));

    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+End');
    await app.page.locator(selectors.textEditor.editor).pressSequentially(' more');
    await app.expectTextEditorContent('base more');

    await app.page.keyboard.press('ControlOrMeta+z');
    await app.expectTextEditorContent('base');
    await expect(app.page.locator(selectors.textEditor.saveButton)).toBeDisabled();

    await app.page.keyboard.press('ControlOrMeta+Shift+z');
    await app.expectTextEditorContent('base more');
    await expect(app.page.locator(selectors.textEditor.saveButton)).toBeEnabled();
    await app.cancelTextEditor();
  });

  test('a recovered draft is not undoable back out of', async ({ app }) => {
    // Recovering a draft loads a document; it is not an edit the user made in
    // this session, so undo must not reach behind it.
    const textPath = await createTempFile('stored', 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);
    await app.openTextEditor(filename);
    await app.setTextEditorContent('drafted');
    await expect(app.page.locator(selectors.textEditor.draftStatus)).toHaveText('Draft saved');

    await app.page.reload();
    await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });
    await app.expectClipVisible(filename);
    await app.openTextEditor(filename);
    await app.expectTextEditorContent('drafted');

    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+z');
    await app.expectTextEditorContent('drafted');
    await app.cancelTextEditor();
  });

  test('the editor is inside the modal focus trap', async ({ app }) => {
    // CodeMirror's content element is focusable through contenteditable, with no
    // tabindex — the old focusable-element query would have dropped it.
    const textPath = await createTempFile('trapped', 'txt');
    await app.uploadFile(textPath);
    await app.openTextEditor(path.basename(textPath));

    const inTrap = await app.page.evaluate(() => {
      const modal = document.getElementById('editor-modal')!;
      const focusable = Array.from(modal.querySelectorAll(
        'button:not([disabled]):not([tabindex="-1"]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [contenteditable="true"]:not([tabindex="-1"]), [tabindex]:not([tabindex="-1"])',
      ));
      const content = modal.querySelector('#text-editor-mount .cm-content');
      return focusable.includes(content as Element);
    });
    expect(inTrap).toBe(true);
    await app.cancelTextEditor();
  });
});

test.describe('Find panel case and whole-word toggles', () => {
  test('both default off, so an untouched panel matches as before', async ({ app }) => {
    const textPath = await createTempFile('Alpha alpha alphabet', 'txt');
    await app.uploadFile(textPath);
    await app.openTextEditor(path.basename(textPath));
    await app.page.locator(selectors.textEditor.findToggle).click();

    await expect(app.page.locator(selectors.textEditor.matchCaseToggle)).toHaveAttribute('aria-pressed', 'false');
    await expect(app.page.locator(selectors.textEditor.wholeWordToggle)).toHaveAttribute('aria-pressed', 'false');

    await app.page.locator(selectors.textEditor.findInput).fill('alpha');
    // Case-insensitive, substring: Alpha, alpha, alphabet.
    await expect(app.page.locator(selectors.textEditor.searchStatus)).toHaveText('1 of 3');
    await app.cancelTextEditor();
  });

  test('match case narrows the result set and the N of M status', async ({ app }) => {
    const textPath = await createTempFile('Alpha alpha alphabet', 'txt');
    await app.uploadFile(textPath);
    await app.openTextEditor(path.basename(textPath));
    await app.page.locator(selectors.textEditor.findToggle).click();
    await app.page.locator(selectors.textEditor.findInput).fill('alpha');
    await expect(app.page.locator(selectors.textEditor.searchStatus)).toHaveText('1 of 3');

    await app.page.locator(selectors.textEditor.matchCaseToggle).click();
    await expect(app.page.locator(selectors.textEditor.matchCaseToggle)).toHaveAttribute('aria-pressed', 'true');
    // 'Alpha' no longer counts.
    await expect(app.page.locator(selectors.textEditor.searchStatus)).toHaveText('1 of 2');
    await expect(app.page.locator(selectors.textEditor.searchMatches)).toHaveCount(2);

    await app.page.locator(selectors.textEditor.matchCaseToggle).click();
    await expect(app.page.locator(selectors.textEditor.searchStatus)).toHaveText('1 of 3');
    await app.cancelTextEditor();
  });

  test('whole word narrows the result set and the N of M status', async ({ app }) => {
    const textPath = await createTempFile('Alpha alpha alphabet', 'txt');
    await app.uploadFile(textPath);
    await app.openTextEditor(path.basename(textPath));
    await app.page.locator(selectors.textEditor.findToggle).click();
    await app.page.locator(selectors.textEditor.findInput).fill('alpha');
    await expect(app.page.locator(selectors.textEditor.searchStatus)).toHaveText('1 of 3');

    await app.page.locator(selectors.textEditor.wholeWordToggle).click();
    await expect(app.page.locator(selectors.textEditor.wholeWordToggle)).toHaveAttribute('aria-pressed', 'true');
    // 'alphabet' no longer counts; 'Alpha' still does while case is ignored.
    await expect(app.page.locator(selectors.textEditor.searchStatus)).toHaveText('1 of 2');
    await app.cancelTextEditor();
  });

  test('the toggles are not persisted across opens', async ({ app }) => {
    const textPath = await createTempFile('Alpha alpha', 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);
    await app.openTextEditor(filename);
    await app.page.locator(selectors.textEditor.findToggle).click();
    await app.page.locator(selectors.textEditor.matchCaseToggle).click();
    await app.page.locator(selectors.textEditor.wholeWordToggle).click();
    await expect(app.page.locator(selectors.textEditor.matchCaseToggle)).toHaveAttribute('aria-pressed', 'true');
    await app.cancelTextEditor();

    await app.openTextEditor(filename);
    await app.page.locator(selectors.textEditor.findToggle).click();
    await expect(app.page.locator(selectors.textEditor.matchCaseToggle)).toHaveAttribute('aria-pressed', 'false');
    await expect(app.page.locator(selectors.textEditor.wholeWordToggle)).toHaveAttribute('aria-pressed', 'false');
    await app.cancelTextEditor();
  });

  test('the active match stays visible while focus is in the query input', async ({ app }) => {
    // The drawSelection requirement. Losing this without a replacement would be a
    // visible find/replace regression: a textarea stops painting its selection as
    // soon as focus leaves it.
    const textPath = await createTempFile(`alpha\n${'line\n'.repeat(40)}alpha`, 'txt');
    await app.uploadFile(textPath);
    await app.openTextEditor(path.basename(textPath));
    await app.page.locator(selectors.textEditor.findToggle).click();
    await app.page.locator(selectors.textEditor.findInput).fill('alpha');

    await expect(app.page.locator(selectors.textEditor.findInput)).toBeFocused();
    const active = app.page.locator(selectors.textEditor.activeSearchMatch);
    await expect(active).toHaveCount(1);
    await expect(active).toBeVisible();
    await expect(active).toHaveText('alpha');
    await expect(active).not.toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
    await app.cancelTextEditor();
  });
});

test.describe('Byte fidelity through a real save', () => {
  test('a BOM and CRLF survive an ordinary save', async ({ app }) => {
    // Asserted on bytes. A decoded-string comparison passes happily while the BOM
    // or the CR silently vanishes — which is exactly what the previous save path,
    // encoding the raw editor string, did.
    const source = Buffer.from([0xef, 0xbb, 0xbf, 0x61, 0x0d, 0x0a, 0x62, 0x0d, 0x0a]);
    const textPath = await createTempFile(source, 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);
    await app.openTextEditor(filename);

    // The editor value excludes the BOM and uses LF, matching CodeMirror's model.
    await app.expectTextEditorContent('a\nb\n');

    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+End');
    await app.page.locator(selectors.textEditor.editor).pressSequentially('c');
    await app.expectTextEditorContent('a\nb\nc');
    await app.saveTextEditor();

    const bytes = await app.page.evaluate(async (name) => {
      const clips = await (window as any).go.main.App.GetClips(false, [], [], 'created_at', 'desc');
      const clip = clips.find((c: any) => c.filename === name);
      const data = await (window as any).go.main.App.GetClipData(clip.id);
      // Valid text crosses the desktop bridge as a plain string, everything else
      // as base64; both are handled rather than inferred from the content type.
      if (data.data_encoding === 'base64') {
        return Array.from(Uint8Array.from(atob(data.data), (ch) => ch.charCodeAt(0)));
      }
      return Array.from(new TextEncoder().encode(data.data));
    }, filename);

    expect(bytes).toEqual([0xef, 0xbb, 0xbf, 0x61, 0x0d, 0x0a, 0x62, 0x0d, 0x0a, 0x63]);
  });
});

test.describe('Draft v1 migration', () => {
  test('a BOM + CRLF v1 draft migrates to v2 and still matches its clip', async ({ app }) => {
    // The trap: a v1 record stored originalText exactly as the backend handed it
    // over, so a BOM'd CRLF document's v1 originalText begins with U+FEFF and
    // contains CR LF. The v2 editor value has neither, and recovery matches a
    // draft to its clip by comparing those strings — so without normalizing both
    // sides, every such draft silently fails to match and is discarded.
    const source = Buffer.from([0xef, 0xbb, 0xbf, 0x6f, 0x6e, 0x65, 0x0d, 0x0a, 0x74, 0x77, 0x6f, 0x0d, 0x0a]);
    const textPath = await createTempFile(source, 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);

    const clipID = await app.page.evaluate(async (name) => {
      const clips = await (window as any).go.main.App.GetClips(false, [], [], 'created_at', 'desc');
      return clips.find((c: any) => c.filename === name).id;
    }, filename);

    // Write the v1 record the old implementation would have written.
    await app.page.evaluate(({ id, name }) => {
      localStorage.setItem(`mahpastes:text-editor-draft:v1:${id}`, JSON.stringify({
        filename: name,
        contentType: 'text/plain',
        originalText: '﻿one\r\ntwo\r\n',
        text: '﻿one\r\ntwo\r\nthree\r\n',
        updatedAt: Date.now(),
      }));
    }, { id: clipID, name: filename });

    await app.openTextEditor(filename);
    await expect(app.page.locator(selectors.textEditor.draftStatus)).toHaveText('Recovered draft');
    await app.expectTextEditorContent('one\ntwo\nthree\n');

    const keys = await app.page.evaluate((id) => ({
      v1: localStorage.getItem(`mahpastes:text-editor-draft:v1:${id}`),
      v2: JSON.parse(localStorage.getItem(`mahpastes:text-editor-draft:v2:${id}`) || 'null'),
    }), clipID);

    // Migrated once, then the legacy entry is gone.
    expect(keys.v1).toBeNull();
    expect(keys.v2.profile).toEqual({ bom: true, newline: 'crlf', finalNewline: true });
    expect(keys.v2.originalText).toBe('one\ntwo\n');
    expect(keys.v2.text).toBe('one\ntwo\nthree\n');

    await app.cancelTextEditor();
  });

  test('a successful save clears both draft keys', async ({ app }) => {
    const textPath = await createTempFile('stored', 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);

    const clipID = await app.page.evaluate(async (name) => {
      const clips = await (window as any).go.main.App.GetClips(false, [], [], 'created_at', 'desc');
      return clips.find((c: any) => c.filename === name).id;
    }, filename);

    // A stale v1 record must not survive a save to be "recovered" later.
    await app.page.evaluate((id) => {
      localStorage.setItem(`mahpastes:text-editor-draft:v1:${id}`, JSON.stringify({
        filename: 'other.txt', contentType: 'text/plain', originalText: 'x', text: 'y', updatedAt: 1,
      }));
    }, clipID);

    await app.openTextEditor(filename);
    await app.setTextEditorContent('edited and saved');
    await app.saveTextEditor();

    const keys = await app.page.evaluate((id) => ({
      v1: localStorage.getItem(`mahpastes:text-editor-draft:v1:${id}`),
      v2: localStorage.getItem(`mahpastes:text-editor-draft:v2:${id}`),
    }), clipID);
    expect(keys.v1).toBeNull();
    expect(keys.v2).toBeNull();
  });
});

test.describe('Descriptor-driven open modes', () => {
  test('a generic open starts Markdown and CSV in Preview', async ({ app }) => {
    // Their Preview is a materially different artifact — rendered GFM or a parsed
    // table — so landing there is worth a click to get back to source.
    for (const [content, extension] of [['# Heading', 'md'], ['a,b\n1,2', 'csv']] as const) {
      const filePath = await createTempFile(content, extension);
      await app.uploadFile(filePath);
      await app.page.locator(selectors.gallery.clipCardByName(path.basename(filePath)))
        .locator(selectors.clipActions.view).click();
      await expect(app.page.locator(selectors.textEditor.previewTab)).toHaveAttribute('aria-selected', 'true');
      await expect(app.page.locator(selectors.textEditor.modeLabel)).toHaveText('Preview');
      await app.cancelTextEditor();
    }
  });

  test('a generic open starts every source-preview format in Edit', async ({ app }) => {
    // Preview for these is read-only highlighted source that looks nearly
    // identical to the editor, so defaulting to it would cost a click and invite
    // "why can't I type here".
    const cases: Array<[string, string]> = [
      ['plain text', 'txt'],
      ['{"a":1}', 'json'],
      ['a: 1', 'yaml'],
      ['<p>hi</p>', 'html'],
    ];
    for (const [content, extension] of cases) {
      const filePath = await createTempFile(content, extension);
      await app.uploadFile(filePath);
      await app.page.locator(selectors.gallery.clipCardByName(path.basename(filePath)))
        .locator(selectors.clipActions.view).click();
      await expect(app.page.locator(selectors.textEditor.editTab)).toHaveAttribute('aria-selected', 'true');
      await expect(app.page.locator(selectors.textEditor.modeLabel)).toHaveText('Edit');
      // Both modes are still reachable for every registered text clip.
      await expect(app.page.locator(selectors.textEditor.previewTab)).toBeVisible();
      await app.cancelTextEditor();
    }
  });

  test('the card menu Edit action opens Markdown and CSV in Edit, with no tab click', async ({ app }) => {
    for (const [content, extension] of [['# Heading', 'md'], ['a,b\n1,2', 'csv']] as const) {
      const filePath = await createTempFile(content, extension);
      const filename = path.basename(filePath);
      await app.uploadFile(filePath);
      // openTextEditor goes through the card menu's Edit item.
      await app.openTextEditor(filename);
      await expect(app.page.locator(selectors.textEditor.editTab)).toHaveAttribute('aria-selected', 'true');
      await expect(app.page.locator(selectors.textEditor.mount)).toBeVisible();
      await expect(app.page.locator(selectors.textEditor.previewPanel)).toBeHidden();
      await app.expectTextEditorContent(content);
      await app.cancelTextEditor();
    }
  });

  test('the two lightbox Edit entry points request Edit rather than clicking a tab', async ({ app }) => {
    // The lightbox only opens for image clips, so these entry points cannot be
    // driven end-to-end with a text clip. What matters is that each expresses the
    // mode in the call — the design forbids simulating a tab click after opening —
    // so the call each one makes is what gets asserted.
    const imagePath = await createTempFile(generateTestImage(40, 40, 'red'), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.page.locator(selectors.gallery.clipCardByName(filename))
      .locator(selectors.clipActions.view).click();

    const calls = await app.page.evaluate(async (name) => {
      const original = (window as any).openEditor;
      const seen: any[] = [];
      (window as any).openEditor = (_id: number, options: any) => { seen.push(options || null); };
      try {
        const clips = await (window as any).go.main.App.GetClips(false, [], [], 'created_at', 'desc');
        const clip = clips.find((c: any) => c.filename === name);
        // The lightbox controller's editClip callback, wired in app.js.
        (window as any).LightboxController.command('edit');
        // The lightbox file menu's Edit item, handled in modals.js.
        await (window as any).handleLightboxFileAction('edit', clip);
        return seen;
      } finally {
        (window as any).openEditor = original;
      }
    }, filename);

    expect(calls).toEqual([{ initialMode: 'edit' }, { initialMode: 'edit' }]);
  });

  test('a recovered draft starts in Edit even for a Preview-default format', async ({ app }) => {
    // A recovered draft is an edit in progress; hiding it behind a Preview tab
    // would be the one case where landing in Preview loses information.
    const csvPath = await createTempFile('a,b\n1,2', 'csv');
    const filename = path.basename(csvPath);
    await app.uploadFile(csvPath);
    await app.openTextEditor(filename);
    await app.setTextEditorContent('a,b\n1,3');
    await expect(app.page.locator(selectors.textEditor.draftStatus)).toHaveText('Draft saved');

    await app.page.reload();
    await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });
    // A *generic* open, which would otherwise land in Preview for CSV.
    await app.page.locator(selectors.gallery.clipCardByName(filename))
      .locator(selectors.clipActions.view).click();

    await expect(app.page.locator(selectors.textEditor.editTab)).toHaveAttribute('aria-selected', 'true');
    await expect(app.page.locator(selectors.textEditor.draftStatus)).toHaveText('Recovered draft');
    await app.expectTextEditorContent('a,b\n1,3');
    await app.cancelTextEditor();
  });

  test('image clips keep their card-menu Edit item', async ({ app }) => {
    // The eligibility helper this replaced also returned true for image/*. A
    // text-only check here would silently remove image editing from the menu.
    const imagePath = await createTempFile(generateTestImage(40, 40, 'blue'), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.openCardMenu(filename);
    await expect(app.page.locator(selectors.cardMenu.edit)).toBeVisible();
    await app.page.keyboard.press('Escape');
  });
});

test.describe('Source Preview', () => {
  test('reflects unsaved source and keeps HTML inert', async ({ app }) => {
    const htmlPath = await createTempFile('<script>window.__pwned = true;</script>', 'html');
    await app.uploadFile(htmlPath);
    await app.openTextEditor(path.basename(htmlPath));

    await app.setTextEditorContent('<img src=x onerror="window.__pwned = true">\nsecond line');
    await app.page.locator(selectors.textEditor.previewTab).click();

    const preview = app.page.locator(selectors.textEditor.sourcePreview);
    await expect(preview).toBeVisible();
    // Preview always renders the current unsaved editor value.
    await expect(preview).toContainText('onerror="window.__pwned = true"');
    // Line numbers come from the source renderer's own gutter.
    await expect(app.page.locator(selectors.textEditor.sourcePreviewLines)).toHaveCount(2);
    // Source stays text: no element was created from it, and nothing ran.
    await expect(app.page.locator('#source-preview-content img')).toHaveCount(0);
    await expect(app.page.locator('#source-preview-content a')).toHaveCount(0);
    expect(await app.page.evaluate(() => (window as any).__pwned)).toBeUndefined();
    await app.cancelTextEditor();
  });

  test('each mode keeps its own scroll position, and closing resets both', async ({ app }) => {
    const long = Array.from({ length: 400 }, (_, i) => `line ${i + 1}`).join('\n');
    const textPath = await createTempFile(long, 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);
    await app.openTextEditor(filename);

    // Opening and every mode switch restore their mode's saved scroll position in
    // a requestAnimationFrame callback. Writing a scroll position before that frame
    // has run means the restore clobbers it, which under load looks exactly like a
    // failure to remember the position. Wait the frame out first.
    const settleFrames = () => app.page.evaluate(
      () => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))),
    );

    // Scroll Edit, switch to Preview (which starts at the top), scroll Preview.
    await settleFrames();
    await app.page.locator(selectors.textEditor.scroller).evaluate((el: HTMLElement) => { el.scrollTop = 900; });
    await app.page.locator(selectors.textEditor.previewTab).click();
    const previewPanel = app.page.locator(selectors.textEditor.previewPanel);
    await expect.poll(() => previewPanel.evaluate((el: HTMLElement) => el.scrollTop)).toBe(0);
    await settleFrames();
    await previewPanel.evaluate((el: HTMLElement) => { el.scrollTop = 500; });

    // Back to Edit: its own position is restored, not Preview's 500.
    await app.page.locator(selectors.textEditor.editTab).click();
    await expect.poll(() => app.page.locator(selectors.textEditor.scroller)
      .evaluate((el: HTMLElement) => el.scrollTop)).toBeGreaterThan(800);

    // And Preview keeps its own.
    await app.page.locator(selectors.textEditor.previewTab).click();
    await expect.poll(() => previewPanel.evaluate((el: HTMLElement) => el.scrollTop)).toBe(500);

    // Closing resets both.
    await app.cancelTextEditor();
    await app.openTextEditor(filename);
    await expect.poll(() => app.page.locator(selectors.textEditor.scroller)
      .evaluate((el: HTMLElement) => el.scrollTop)).toBe(0);
    await app.page.locator(selectors.textEditor.previewTab).click();
    await expect.poll(() => previewPanel.evaluate((el: HTMLElement) => el.scrollTop)).toBe(0);
    await app.cancelTextEditor();
  });

  test('the wrap preference applies to Preview as well as Edit', async ({ app }) => {
    const textPath = await createTempFile('a line of source', 'txt');
    await app.uploadFile(textPath);
    await app.openTextEditor(path.basename(textPath));
    await app.page.locator(selectors.textEditor.previewTab).click();

    const preview = app.page.locator(selectors.textEditor.sourcePreview);
    await expect(preview).toHaveAttribute('data-wrap', 'on');
    await app.page.locator(selectors.textEditor.wrapToggle).click();
    await expect(preview).toHaveAttribute('data-wrap', 'off');
    await app.page.locator(selectors.textEditor.wrapToggle).click();
    await expect(preview).toHaveAttribute('data-wrap', 'on');
    await app.cancelTextEditor();
  });
});

test.describe('Byte-safety and size limits', () => {
  test('an invalid UTF-8 text clip is read-only for every format, not just Markdown', async ({ app }) => {
    // A deliberate behavior change: this clip used to be lossily editable, so a
    // save would have replaced bytes the user never saw. The loss happens in Go —
    // string(data) on invalid bytes — so the only safe answer is to refuse.
    const latin1Path = await createTempFile(Buffer.from([0x63, 0x61, 0x66, 0xe9]), 'txt');
    await app.uploadFile(latin1Path);
    await app.openTextEditor(path.basename(latin1Path));

    await expect(app.page.locator(selectors.textEditor.unavailablePanel)).toBeVisible();
    await expect(app.page.locator(selectors.textEditor.mount)).toBeHidden();
    await expect(app.page.locator(selectors.textEditor.previewPanel)).toBeHidden();
    await expect(app.page.locator(selectors.textEditor.modeTabs)).toBeHidden();
    await expect(app.page.locator(selectors.textEditor.modeLabel)).toHaveText('Unavailable');

    // Format-neutral wording, and it names the follow-up so the state reads as
    // deliberate rather than broken.
    const reason = app.page.locator(selectors.textEditor.unavailableReason);
    await expect(reason).toContainText('not valid UTF-8');
    await expect(reason).not.toContainText('Markdown');
    await expect(app.page.locator(selectors.textEditor.unavailablePanel)).toContainText('Windows-1252');

    // Nothing can be written back over the original bytes.
    await expect(app.page.locator(selectors.textEditor.saveButton)).toBeDisabled();
    await expect(app.page.locator(selectors.textEditor.saveAsButton)).toBeDisabled();
    expect(await app.page.evaluate('TextClipEditor.getValue()')).toBe('');

    // The keyboard path has to be closed too: mod+s is bound to Save As, which
    // would otherwise reveal the filename field and then write an empty clip from
    // an editor that was deliberately never given a value.
    await app.page.keyboard.press('ControlOrMeta+s');
    await app.waitForToast(/Saving is unavailable/);
    await expect(app.page.locator(selectors.textEditor.saveAsInput)).toBeHidden();
    await expect(app.page.locator(selectors.textEditor.modal)).toHaveClass(/active/);
    await app.cancelTextEditor();
    await app.expectClipCount(1);
  });

  test('a text clip over the 16 MiB cap declines to open at all', async ({ app }) => {
    // Materializing a document this size is the point of the cap, so the payload is
    // injected at the retrieval seam rather than uploaded: the editor path would
    // otherwise hold it as a base64 string, a decoded string, a CodeMirror document
    // and a preview at once, which is exactly what must not happen.
    const textPath = await createTempFile('small', 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);

    const clipID = await app.page.evaluate(async (name) => {
      const clips = await (window as any).go.main.App.GetClips(false, [], [], 'created_at', 'desc');
      return clips.find((c: any) => c.filename === name).id;
    }, filename);

    await app.page.evaluate(({ id, name }) => {
      const App = (window as any).go.main.App;
      const oversized = {
        id,
        filename: name,
        content_type: 'text/plain',
        data: 'x'.repeat(17 * 1024 * 1024),
        data_encoding: 'utf8',
        valid_utf8: true,
      };
      // Desktop retrieval is GetClipData; server mode adds GetClipText. Patch
      // whichever exists so the test does not encode an assumption about which
      // surface it is running on.
      for (const method of ['GetClipText', 'GetClipData']) {
        const original = App[method];
        if (typeof original !== 'function') continue;
        App[method] = async (requested: number) =>
          (requested === id ? oversized : original.call(App, requested));
      }
    }, { id: clipID, name: filename });

    await app.openCardMenu(filename);
    await app.page.locator(selectors.cardMenu.edit).click();

    await app.waitForToast(/too large to edit \(17 MB\)/);
    // Declines to open, rather than opening into a dead end.
    await expect(app.page.locator(selectors.textEditor.modal)).not.toHaveClass(/active/);
  });

  test('a document over 2 MiB degrades to plain preview and editing', async ({ app }) => {
    // The existing 2 MiB Markdown source threshold is now the common
    // enhanced-assistance threshold for every format.
    // Just over 2 MiB, in moderately long lines rather than one enormous one.
    const big = Array.from({ length: 11500 }, (_, i) => `line ${i} ${'x'.repeat(180)}`).join('\n');
    const textPath = await createTempFile(big, 'txt');
    await app.uploadFile(textPath);
    await app.openTextEditor(path.basename(textPath));

    await expect(app.page.locator(selectors.textEditor.degradedNotice)).toBeVisible();
    await expect(app.page.locator(selectors.textEditor.degradedNotice)).toContainText('2 MB');

    // Editing stays available.
    await expect(app.page.locator(selectors.textEditor.editor)).toBeVisible();
    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+End');
    await app.page.locator(selectors.textEditor.editor).pressSequentially('!');
    await expect(app.page.locator(selectors.textEditor.saveButton)).toBeEnabled();

    // Preview stays available, as plain inert source with no per-line highlighting.
    await app.page.locator(selectors.textEditor.previewTab).click();
    await expect(app.page.locator(selectors.textEditor.sourcePreviewPlain)).toBeVisible();
    await expect(app.page.locator(selectors.textEditor.sourcePreviewLines)).toHaveCount(0);
    await app.cancelTextEditor();
  });
});

test.describe('Preview toggle shortcut migration', () => {
  test('the generalized action replaces the Markdown-only one', async ({ app }) => {
    const registered = await app.page.evaluate(`(() => {
      const actions = ShortcutManager.actions;
      const action = actions.get('editor.preview_toggle');
      return {
        legacyRegistered: actions.has('editor.markdown_preview'),
        label: action ? action.label : null,
        defaultKey: action ? action.defaultKey : null,
        context: action ? action.context : null,
      };
    })()`) as any;

    expect(registered.legacyRegistered).toBe(false);
    expect(registered.label).toBe('Toggle Preview/Edit');
    // The default binding is unchanged by the rename.
    expect(registered.defaultKey).toBe('mod+shift+p');
    expect(registered.context).toBe('text-editor');
  });

  test('a stored override on the retired ID migrates without changing the binding', async ({ app }) => {
    const result = await app.page.evaluate(`(() => {
      const manager = ShortcutManager;
      const overrides = manager.userOverrides;
      const before = { ...overrides };
      for (const key of Object.keys(overrides)) delete overrides[key];

      overrides['editor.markdown_preview'] = 'mod+shift+m';
      const changed = manager.migrateRenamedOverrides();
      const after = { legacy: overrides['editor.markdown_preview'], next: overrides['editor.preview_toggle'] };

      // A deliberate rebinding of the new ID must win over the legacy value.
      for (const key of Object.keys(overrides)) delete overrides[key];
      overrides['editor.markdown_preview'] = 'mod+shift+m';
      overrides['editor.preview_toggle'] = 'mod+shift+k';
      manager.migrateRenamedOverrides();
      const preferNew = { legacy: overrides['editor.markdown_preview'], next: overrides['editor.preview_toggle'] };

      for (const key of Object.keys(overrides)) delete overrides[key];
      Object.assign(overrides, before);
      manager.rebuildBindings();
      return { changed, after, preferNew };
    })()`) as any;

    expect(result.changed).toBe(true);
    expect(result.after.next).toBe('mod+shift+m');
    expect(result.after.legacy).toBeUndefined();
    expect(result.preferNew.next).toBe('mod+shift+k');
    expect(result.preferNew.legacy).toBeUndefined();
  });
});

// --- Validation, diagnostics, and the change-based save policy ----------------

// Invalid by construction and never becomes valid: a Helm template is the fixture
// the change-based save policy exists for.
const HELM_DEPLOYMENT = [
  '{{- if .Values.enabled }}',
  'apiVersion: apps/v1',
  'kind: Deployment',
  'metadata:',
  '  labels:',
  '    {{- include "chart.labels" . | nindent 4 }}',
  '  name: app',
  '{{- end }}',
].join('\n');

async function expectNoConfirmDialog(app: any) {
  // The dialog is always in the DOM; opacity-100 is what makes it visible.
  await expect(app.page.locator(selectors.confirm.dialog)).not.toHaveClass(/opacity-100/);
}

test.describe('Per-format validation status', () => {
  const cases: Array<{ extension: string; label: string; valid: string; invalid: string; at: string }> = [
    { extension: 'json', label: 'JSON', valid: '{"a": 1}', invalid: '{"a": }', at: 'Ln 1, Col 7' },
    { extension: 'jsonl', label: 'JSON Lines', valid: '{"a":1}\n{"b":2}', invalid: '{"a":1}\n{"b":}', at: 'Ln 2, Col 6' },
    { extension: 'yaml', label: 'YAML', valid: 'a: 1\nb: 2', invalid: 'a: 1\na: 2', at: 'Ln 2, Col 1' },
    { extension: 'toml', label: 'TOML', valid: 'a = 1', invalid: 'a = = 1', at: 'Ln 1, Col 5' },
    { extension: 'xml', label: 'XML', valid: '<r><a/></r>', invalid: '<r><a></r>', at: 'Ln 1, Col 10' },
  ];

  for (const item of cases) {
    test(`${item.label}: valid, then invalid with a positioned drawer row`, async ({ app }) => {
      const filePath = await createTempFile(item.valid, item.extension);
      await app.uploadFile(filePath);
      await app.openTextEditor(path.basename(filePath));

      const status = app.page.locator(selectors.textEditor.validationStatus);
      await expect(status).toHaveText(`Valid ${item.label}`);
      // The drawer is a permanent strip and starts collapsed.
      await expect(app.page.locator(selectors.textEditor.diagnostics)).toBeVisible();
      await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toHaveText('No issues');
      await expect(app.page.locator(selectors.textEditor.diagnosticsBody)).toBeHidden();

      await app.setTextEditorContent(item.invalid);
      await expect(status).toContainText(`Invalid ${item.label}`);
      await expect(status).toContainText(item.at);
      await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toHaveText('1 error');
      // New errors do NOT expand the drawer while the user types.
      await expect(app.page.locator(selectors.textEditor.diagnosticsBody)).toBeHidden();

      // Activating the count is one of the three things that does expand it.
      await app.page.locator(selectors.textEditor.diagnosticsToggle).click();
      await expect(app.page.locator(selectors.textEditor.diagnosticsBody)).toBeVisible();
      await expect(app.page.locator(selectors.textEditor.diagnosticsRow)).toHaveCount(1);
      await expect(app.page.locator(selectors.textEditor.diagnosticsRowLocation)).toHaveText(item.at);
      await expect(app.page.locator(selectors.textEditor.diagnosticsRow))
        .toHaveAttribute('aria-label', new RegExp(`^Error at line \\d+, column \\d+: `));

      // Source stays visible while the diagnostics are shown.
      await app.expectTextEditorContent(item.invalid);
      await app.cancelTextEditor();
    });
  }

  test('clips with no validator produce no diagnostics at all', async ({ app }) => {
    // Shell, .env, INI/CFG/CONF, plain text and logs get highlighting only. Each
    // source below would be a syntax error in some *other* grammar, which is
    // exactly the false positive this must not produce.
    const cases: Array<[string, string]> = [
      ['#!/bin/sh\nif [ -f x ]; then\n  echo "{" \n', 'sh'],
      ['[section]\nkey = value\nbroken = [\n', 'ini'],
      ['[a]\nb = = 1\n', 'cfg'],
      ['[a]\nb: {\n', 'conf'],
      ['2026-07-30 ERROR {"unclosed": \n', 'log'],
      ['just text {[(\n', 'txt'],
    ];
    // `.env` is matched as a whole basename, so it needs a file named exactly that
    // rather than one of createTempFile's generated names.
    const envDir = await createTempDir();
    const envPath = path.join(envDir, '.env');
    await writeFile(envPath, 'SECRET=hunter2\nBROKEN="unterminated\n');

    for (const filePath of [...await Promise.all(cases.map(([content, extension]) => createTempFile(content, extension))), envPath]) {
      await app.uploadFile(filePath);
      await app.openTextEditor(path.basename(filePath));

      await expect(app.page.locator(selectors.textEditor.validationStatus)).toHaveText('');
      await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toHaveText('No issues');
      // Editing does not conjure one either.
      await app.page.locator(selectors.textEditor.editor).click();
      await app.page.keyboard.press('ControlOrMeta+End');
      await app.page.locator(selectors.textEditor.editor).pressSequentially('}');
      await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toHaveText('No issues');
      await expect(app.page.locator(selectors.textEditor.errorMarker)).toHaveCount(0);
      await app.cancelTextEditor();
    }
  });

  test('an astral character does not shift the reported position', async ({ app }) => {
    // The canonical fixture: byte, code-point and UTF-16 columns all differ, so an
    // unconverted offset puts the cursor in the wrong place rather than failing.
    const filePath = await createTempFile('{"a": 1}', 'json');
    await app.uploadFile(filePath);
    await app.openTextEditor(path.basename(filePath));
    await app.setTextEditorContent('{"😀": }');

    await expect(app.page.locator(selectors.textEditor.validationStatus)).toContainText('Ln 1, Col 8');
    await app.page.locator(selectors.textEditor.diagnosticsToggle).click();
    await expect(app.page.locator(selectors.textEditor.diagnosticsRowLocation)).toHaveText('Ln 1, Col 8');

    // Activating the row places the cursor there, which is the assertion that a
    // wrong unit would actually break.
    await app.page.locator(selectors.textEditor.diagnosticsRow).click();
    await expect(app.page.locator(selectors.textEditor.cursorStatus)).toHaveText('Ln 1, Col 8');
    await app.cancelTextEditor();
  });
});

test.describe('Diagnostics drawer', () => {
  test('a row switches to Edit, selects the range, and focuses the editor', async ({ app }) => {
    const filePath = await createTempFile('{\n  "a": 1,\n  "b": ,\n  "c": 3\n}', 'json');
    await app.uploadFile(filePath);
    await app.openTextEditor(path.basename(filePath));
    await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toHaveText('1 error');

    // From Preview, so the switch back to Edit is exercised.
    await app.page.locator(selectors.textEditor.previewTab).click();
    await app.page.locator(selectors.textEditor.diagnosticsToggle).click();
    await app.page.locator(selectors.textEditor.diagnosticsRow).click();

    await expect(app.page.locator(selectors.textEditor.editTab)).toHaveAttribute('aria-selected', 'true');
    await expect(app.page.locator(selectors.textEditor.editor)).toBeFocused();
    await expect(app.page.locator(selectors.textEditor.cursorStatus)).toHaveText('Ln 3, Col 8');
    await app.cancelTextEditor();
  });

  test('rows are keyboard-activatable', async ({ app }) => {
    const filePath = await createTempFile('{"a": }', 'json');
    await app.uploadFile(filePath);
    await app.openTextEditor(path.basename(filePath));
    await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toHaveText('1 error');

    await app.page.locator(selectors.textEditor.diagnosticsToggle).focus();
    await app.page.keyboard.press('Enter');
    await expect(app.page.locator(selectors.textEditor.diagnosticsBody)).toBeVisible();

    await app.page.locator(selectors.textEditor.diagnosticsRow).focus();
    await app.page.keyboard.press('Enter');
    await expect(app.page.locator(selectors.textEditor.editor)).toBeFocused();
    await expect(app.page.locator(selectors.textEditor.cursorStatus)).toHaveText('Ln 1, Col 7');
    await app.cancelTextEditor();
  });

  test('inline markers appear in the editor and activating one expands the drawer', async ({ app }) => {
    // Short document on purpose: CodeMirror virtualizes off-screen lines, so a DOM
    // count of markers is only meaningful when every line is rendered.
    const filePath = await createTempFile('a: 1\na: 2\n', 'yaml');
    await app.uploadFile(filePath);
    await app.openTextEditor(path.basename(filePath));
    await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toHaveText('1 error');

    const marker = app.page.locator(selectors.textEditor.errorMarker);
    await expect(marker).toHaveCount(1);
    await expect(app.page.locator(selectors.textEditor.diagnosticsBody)).toBeHidden();

    await marker.click();
    await expect(app.page.locator(selectors.textEditor.diagnosticsBody)).toBeVisible();
    await app.cancelTextEditor();
  });

  test('at most 100 diagnostics are shown and the remainder is reported', async ({ app }) => {
    const broken = `${Array.from({ length: 140 }, () => '{').join('\n')}\n`;
    const filePath = await createTempFile(broken, 'jsonl');
    await app.uploadFile(filePath);
    await app.openTextEditor(path.basename(filePath));

    await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toHaveText('140 errors');
    await app.page.locator(selectors.textEditor.diagnosticsToggle).click();
    await expect(app.page.locator(selectors.textEditor.diagnosticsRow)).toHaveCount(100);
    const notice = app.page.locator(selectors.textEditor.diagnosticsNotice);
    await expect(notice).toBeVisible();
    await expect(notice).toContainText('first 100 of 140');
    await expect(notice).toContainText('40 more');
    await app.cancelTextEditor();
  });

  test('a recovered draft with authoritative errors starts expanded', async ({ app }) => {
    // The one exception to "starts collapsed": a recovered invalid state must not
    // be hidden behind a collapsed strip.
    const filePath = await createTempFile('{"a": 1}', 'json');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);
    await app.openTextEditor(filename);
    await app.setTextEditorContent('{"a": }');
    await expect(app.page.locator(selectors.textEditor.draftStatus)).toHaveText('Draft saved');

    await app.page.reload();
    await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });
    await app.expectClipVisible(filename);
    await app.openTextEditor(filename);

    await expect(app.page.locator(selectors.textEditor.draftStatus)).toHaveText('Recovered draft');
    await expect(app.page.locator(selectors.textEditor.diagnosticsBody)).toBeVisible();
    await expect(app.page.locator(selectors.textEditor.diagnosticsRow)).toHaveCount(1);
    await app.cancelTextEditor();
  });

  test('an ordinary open of an already-invalid document starts collapsed', async ({ app }) => {
    const filePath = await createTempFile('{"a": }', 'json');
    await app.uploadFile(filePath);
    await app.openTextEditor(path.basename(filePath));

    await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toHaveText('1 error');
    await expect(app.page.locator(selectors.textEditor.diagnosticsBody)).toBeHidden();
    await app.cancelTextEditor();
  });
});

test.describe('Change-based Save Anyway policy', () => {
  test('a document already invalid at open saves without a prompt, and prompts once its errors change', async ({ app }) => {
    const filePath = await createTempFile(HELM_DEPLOYMENT, 'yaml');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);
    await app.openTextEditor(filename);

    const summary = app.page.locator(selectors.textEditor.diagnosticsSummary);
    await expect(summary).toContainText('error');

    // An edit that leaves the error set alone: append a comment line.
    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+End');
    await app.page.locator(selectors.textEditor.editor).pressSequentially('\n# note');
    await expect(app.page.locator(selectors.textEditor.saveButton)).toBeEnabled();

    // Unchanged pre-existing errors expand nothing and block nothing.
    await app.page.locator(selectors.textEditor.saveButton).click();
    await app.page.waitForSelector(`${selectors.textEditor.modal}:not(.active)`);

    // Reopening re-measures against what is now on disk.
    await app.openTextEditor(filename);
    await expect(summary).toContainText('error');
    const before = await summary.textContent();

    // Now change the error set with a stray token at column 1. Deliberately not an
    // indented line: pressSequentially's newline copies the previous line's leading
    // whitespace, so typed indentation would not land as written.
    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+End');
    await app.page.locator(selectors.textEditor.editor).pressSequentially('\n]');
    await expect(summary).not.toHaveText(before || '');

    await app.page.locator(selectors.textEditor.saveButton).click();
    await expect(app.page.locator(selectors.confirm.title)).toHaveText('Save invalid YAML?');
    await expect(app.page.locator(selectors.confirm.message)).toContainText('changed');
    // A save that triggers confirmation expands the drawer.
    await expect(app.page.locator(selectors.textEditor.diagnosticsBody)).toBeVisible();

    await app.page.locator(selectors.confirm.cancelButton).click();
    // Cancelling leaves the editor open, dirty, and out of the Saving… state.
    await expect(app.page.locator(selectors.textEditor.modal)).toHaveClass(/active/);
    await expect(app.page.locator(selectors.textEditor.saveButton)).toBeEnabled();
    await app.cancelTextEditor();
  });

  test('a save that proceeds without confirmation leaves the drawer as the user left it', async ({ app }) => {
    const filePath = await createTempFile(HELM_DEPLOYMENT, 'yaml');
    await app.uploadFile(filePath);
    await app.openTextEditor(path.basename(filePath));
    await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toContainText('error');
    await expect(app.page.locator(selectors.textEditor.diagnosticsBody)).toBeHidden();

    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+End');
    await app.page.locator(selectors.textEditor.editor).pressSequentially('\n# note');
    await app.page.locator(selectors.textEditor.saveButton).click();

    // No dialog, and the drawer was never opened on the user's behalf.
    await expectNoConfirmDialog(app);
    await app.page.waitForSelector(`${selectors.textEditor.modal}:not(.active)`);
  });

  test('the baseline comes from the persisted source, not the recovered draft', async ({ app }) => {
    // Stored `{}` plus a recovered draft of `{"x":}` must still prompt. Capturing
    // the baseline after recovery would classify the user's own error as
    // pre-existing and let it silently overwrite valid stored bytes.
    const filePath = await createTempFile('{}', 'json');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);

    const clip = await app.page.evaluate(async (name) => {
      const clips = await (window as any).go.main.App.GetClips(false, [], [], 'created_at', 'desc');
      const found = clips.find((c: any) => c.filename === name);
      return { id: found.id, contentType: found.content_type };
    }, filename);
    const clipID = clip.id;

    // The draft has to match its clip on filename, content type and original text,
    // and the backend may have promoted the stored type by sniffing.
    await app.page.evaluate(({ id, name, contentType }) => {
      localStorage.setItem(`mahpastes:text-editor-draft:v2:${id}`, JSON.stringify({
        filename: name,
        contentType,
        originalText: '{}',
        text: '{"x":}',
        profile: { bom: false, newline: 'lf', finalNewline: false },
        updatedAt: Date.now(),
      }));
    }, { id: clipID, name: filename, contentType: clip.contentType });

    await app.openTextEditor(filename);
    await expect(app.page.locator(selectors.textEditor.draftStatus)).toHaveText('Recovered draft');
    await app.expectTextEditorContent('{"x":}');

    await app.page.locator(selectors.textEditor.saveButton).click();
    await expect(app.page.locator(selectors.confirm.title)).toHaveText('Save invalid JSON?');
    // The error is the user's own, so it reads as introduced rather than changed.
    await expect(app.page.locator(selectors.confirm.message)).toContainText('introduced');

    await app.page.locator(selectors.confirm.cancelButton).click();
    await expect(app.page.locator(selectors.textEditor.modal)).toHaveClass(/active/);

    // The stored bytes were not touched.
    const stored = await app.page.evaluate(async (id) => {
      const data = await (window as any).go.main.App.GetClipData(id);
      return data.data_encoding === 'base64' ? atob(data.data) : data.data;
    }, clipID);
    expect(stored).toBe('{}');
    await app.cancelTextEditor();
  });

  test('possible issues never trigger confirmation', async ({ app }) => {
    // A Markdown renderer failure is a possible issue. It is counted and navigable
    // but must never gate a save.
    const filePath = await createTempFile('# Heading\n', 'md');
    await app.uploadFile(filePath);
    await app.openTextEditor(path.basename(filePath));

    try {
      // MarkdownRenderer is a top-level `const` in a classic script: a global
      // lexical binding, reachable by name but never a property of window. The
      // expression form of evaluate runs in global scope, where the name resolves.
      await app.page.evaluate(`(() => {
        window.__realMarkdownRender = MarkdownRenderer.render;
        MarkdownRenderer.render = () => { throw new Error('renderer exploded'); };
      })()`);
      await app.page.locator(selectors.textEditor.previewTab).click();
      await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toHaveText('1 possible issue');
      // Never counted as an error, and the status line makes no validity claim.
      await expect(app.page.locator(selectors.textEditor.validationStatus)).toHaveText('');

      await app.page.locator(selectors.textEditor.editTab).click();
      await app.page.locator(selectors.textEditor.editor).click();
      await app.page.keyboard.press('ControlOrMeta+End');
      await app.page.locator(selectors.textEditor.editor).pressSequentially('!');
      await app.page.locator(selectors.textEditor.saveButton).click();

      await expectNoConfirmDialog(app);
      await app.page.waitForSelector(`${selectors.textEditor.modal}:not(.active)`);
    } finally {
      await app.page.evaluate(`(() => {
        if (window.__realMarkdownRender) MarkdownRenderer.render = window.__realMarkdownRender;
      })()`);
    }
  });

  test('a validation timeout allows the save without claiming validity', async ({ app }) => {
    const filePath = await createTempFile('{"a": }', 'json');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);
    await app.openTextEditor(filename);
    await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toHaveText('1 error');

    try {
      // Force every subsequent validation to blow its deadline. The notice must not
      // read as "valid", and the save must still proceed because no authoritative
      // syntax error was established.
      //
      // The shared executor instance is patched rather than getExecutor itself: the
      // bundle's exports are getter-only, so assigning to them silently does nothing.
      await app.page.evaluate(async () => {
        const executor = await (window as any).MahpastesTextEditor.getExecutor();
        (window as any).__realRun = executor.run;
        executor.run = () => Promise.reject(Object.assign(new Error('no response within 1500ms'), { code: 'timeout' }));
      });

      await app.page.locator(selectors.textEditor.editor).click();
      await app.page.keyboard.press('ControlOrMeta+End');
      await app.page.locator(selectors.textEditor.editor).pressSequentially(' ');

      const status = app.page.locator(selectors.textEditor.validationStatus);
      await expect(status).toHaveText('Validation unavailable within safety limits');
      // It makes no validity claim in either direction: no "Valid JSON", and not
      // "Invalid JSON" either — no authoritative answer was established.
      await expect(status).not.toContainText('Valid JSON');
      await expect(status).not.toContainText('Invalid JSON');
      await expect(app.page.locator(selectors.textEditor.diagnosticsSummary))
        .toHaveText('Validation unavailable within safety limits');

      await app.page.locator(selectors.textEditor.saveButton).click();
      await expectNoConfirmDialog(app);
      await app.page.waitForSelector(`${selectors.textEditor.modal}:not(.active)`);
    } finally {
      await app.page.evaluate(async () => {
        const executor = await (window as any).MahpastesTextEditor.getExecutor();
        if ((window as any).__realRun) executor.run = (window as any).__realRun;
      });
    }
  });

  test('Save As reclassifies against the proposed target extension', async ({ app }) => {
    // A .txt clip is plain text and has no validator, so nothing is invalid about
    // it. Proposing a .json target makes strict JSON the grammar it is judged by.
    const filePath = await createTempFile('not json at all', 'txt');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);
    await app.openTextEditor(filename);
    await expect(app.page.locator(selectors.textEditor.validationStatus)).toHaveText('');

    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+End');
    await app.page.locator(selectors.textEditor.editor).pressSequentially('!');

    await app.page.locator(selectors.textEditor.saveAsButton).click();
    await app.page.locator(selectors.textEditor.saveAsInput).fill('copy.json');
    await app.page.locator(selectors.textEditor.saveAsButton).click();

    await expect(app.page.locator(selectors.confirm.title)).toHaveText('Save invalid JSON?');
    await expect(app.page.locator(selectors.confirm.message)).toContainText('introduced');

    // Save Anyway still applies to the current attempted value.
    await app.page.locator(selectors.confirm.confirmButton).click();
    await app.page.waitForSelector(`${selectors.textEditor.modal}:not(.active)`);
    await app.expectClipVisible('copy.json');

    // A .txt target is not reclassified and does not prompt.
    await app.openTextEditor(filename);
    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+End');
    await app.page.locator(selectors.textEditor.editor).pressSequentially('?');
    await app.page.locator(selectors.textEditor.saveAsButton).click();
    await app.page.locator(selectors.textEditor.saveAsInput).fill('copy.txt');
    await app.page.locator(selectors.textEditor.saveAsButton).click();
    await expectNoConfirmDialog(app);
    await app.page.waitForSelector(`${selectors.textEditor.modal}:not(.active)`);
  });
});

test.describe('Save operates on an immutable snapshot', () => {
  test('typing during a pending save does not change the bytes written', async ({ app }) => {
    const filePath = await createTempFile('{"a": 1}', 'json');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);
    await app.openTextEditor(filename);
    await app.setTextEditorContent('{"a": 2}');

    // Hold the write open so an edit can land inside the window, and record the
    // bytes it was actually given.
    await app.page.evaluate(() => {
      const App = (window as any).go.main.App;
      (window as any).__realUpdate = App.UpdateClipData;
      (window as any).__seen = null;
      (window as any).__release = null;
      App.UpdateClipData = (id: number, contentType: string, data: string, name: string) => {
        (window as any).__seen = atob(data);
        return new Promise((resolve) => {
          (window as any).__release = () => resolve((window as any).__realUpdate.call(App, id, contentType, data, name));
        });
      };
    });

    try {
      await app.page.locator(selectors.textEditor.saveButton).click();
      // Saving… is entered immediately, before validation resolves.
      await expect(app.page.locator('#editor-save-label')).toHaveText('Saving…');
      await app.page.waitForFunction(() => (window as any).__release !== null, { timeout: 10000 });

      // Generation N+1: typed after the snapshot was taken.
      await app.page.locator(selectors.textEditor.editor).click();
      await app.page.keyboard.press('ControlOrMeta+End');
      await app.page.locator(selectors.textEditor.editor).pressSequentially('  ');
      await app.expectTextEditorContent('{"a": 2}  ');

      await app.page.evaluate(() => (window as any).__release());

      // Generation N's approval was applied to generation N's bytes.
      expect(await app.page.evaluate(() => (window as any).__seen)).toBe('{"a": 2}');
      await app.waitForToast(/Edits made while saving are still unsaved/);
      // The editor stays open and dirty against the newer value.
      await expect(app.page.locator(selectors.textEditor.modal)).toHaveClass(/active/);
      await expect(app.page.locator(selectors.textEditor.saveButton)).toBeEnabled();
      await app.expectTextEditorContent('{"a": 2}  ');
    } finally {
      await app.page.evaluate(() => {
        const App = (window as any).go.main.App;
        if ((window as any).__realUpdate) App.UpdateClipData = (window as any).__realUpdate;
      });
    }

    // ...and the second save writes the newer value, measured against the
    // re-baselined state.
    await app.saveTextEditor();
    const stored = await app.page.evaluate(async (name) => {
      const clips = await (window as any).go.main.App.GetClips(false, [], [], 'created_at', 'desc');
      const clip = clips.find((c: any) => c.filename === name);
      const data = await (window as any).go.main.App.GetClipData(clip.id);
      return data.data_encoding === 'base64' ? atob(data.data) : data.data;
    }, filename);
    expect(stored).toBe('{"a": 2}  ');
  });
});

test.describe('Explicit formatting', () => {
  test('JSON formatting is undoable and preserves the text profile byte-for-byte', async ({ app }) => {
    // BOM + CRLF + final newline, so the profile has something to lose.
    const source = Buffer.concat([
      Buffer.from([0xef, 0xbb, 0xbf]),
      Buffer.from('{"a":1,"b":[2,3]}\r\n', 'utf8'),
    ]);
    const filePath = await createTempFile(source, 'json');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);
    await app.openTextEditor(filename);

    const formatButton = app.page.locator(selectors.textEditor.formatJSONButton);
    await expect(formatButton).toHaveText('Format JSON');
    await expect(formatButton).toBeEnabled();
    await formatButton.click();
    await app.expectTextEditorContent('{\n  "a": 1,\n  "b": [\n    2,\n    3\n  ]\n}\n');

    // A normal undoable transaction, not a document load.
    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+z');
    await app.expectTextEditorContent('{"a":1,"b":[2,3]}\n');
    await app.page.keyboard.press('ControlOrMeta+Shift+z');
    await app.expectTextEditorContent('{\n  "a": 1,\n  "b": [\n    2,\n    3\n  ]\n}\n');

    await app.saveTextEditor();
    const bytes = await app.page.evaluate(async (name) => {
      const clips = await (window as any).go.main.App.GetClips(false, [], [], 'created_at', 'desc');
      const clip = clips.find((c: any) => c.filename === name);
      const data = await (window as any).go.main.App.GetClipData(clip.id);
      if (data.data_encoding === 'base64') {
        return Array.from(Uint8Array.from(atob(data.data), (ch) => ch.charCodeAt(0)));
      }
      return Array.from(new TextEncoder().encode(data.data));
    }, filename);

    // The BOM survived, every separator is CRLF, and the final newline is still there.
    const expected = Array.from(Buffer.concat([
      Buffer.from([0xef, 0xbb, 0xbf]),
      Buffer.from('{\r\n  "a": 1,\r\n  "b": [\r\n    2,\r\n    3\r\n  ]\r\n}\r\n', 'utf8'),
    ]));
    expect(bytes).toEqual(expected);
  });

  test('JSON Lines formatting compacts each value and keeps blank lines', async ({ app }) => {
    const filePath = await createTempFile('{ "a" : 1 }\n\n{  "b":2 }\n', 'jsonl');
    await app.uploadFile(filePath);
    await app.openTextEditor(path.basename(filePath));

    const formatButton = app.page.locator(selectors.textEditor.formatJSONButton);
    await expect(formatButton).toHaveText('Format JSON Lines');
    await formatButton.click();
    await app.expectTextEditorContent('{"a":1}\n\n{"b":2}\n');
    await app.cancelTextEditor();
  });

  test('formatting is disabled while authoritative errors exist', async ({ app }) => {
    const filePath = await createTempFile('{"a": 1}', 'json');
    await app.uploadFile(filePath);
    await app.openTextEditor(path.basename(filePath));
    await expect(app.page.locator(selectors.textEditor.formatJSONButton)).toBeEnabled();

    await app.setTextEditorContent('{"a": }');
    await expect(app.page.locator(selectors.textEditor.formatJSONButton)).toBeDisabled();

    await app.setTextEditorContent('{"a": 1}');
    await expect(app.page.locator(selectors.textEditor.formatJSONButton)).toBeEnabled();
    await app.cancelTextEditor();
  });

  test('formats with no formatter offer no formatting action', async ({ app }) => {
    for (const [content, extension] of [['a: 1\n', 'yaml'], ['a = 1\n', 'toml'], ['<r/>', 'xml'], ['plain\n', 'txt']] as const) {
      const filePath = await createTempFile(content, extension);
      await app.uploadFile(filePath);
      await app.openTextEditor(path.basename(filePath));
      await expect(app.page.locator(selectors.textEditor.formatJSONButton)).toBeHidden();
      await app.cancelTextEditor();
    }
  });

  test('formatting is unavailable above the enhanced-assistance threshold', async ({ app }) => {
    // Just over 2 MiB of valid JSON: highlighting, validation, diagnostics and
    // formatting are all disabled, and plain editing remains.
    const big = `[${Array.from({ length: 110000 }, (_, i) => `"item-${i}-${'x'.repeat(14)}"`).join(',')}]`;
    const filePath = await createTempFile(big, 'json');
    await app.uploadFile(filePath);
    await app.openTextEditor(path.basename(filePath));

    await expect(app.page.locator(selectors.textEditor.degradedNotice)).toBeVisible();
    await expect(app.page.locator(selectors.textEditor.formatJSONButton)).toBeDisabled();
    await expect(app.page.locator(selectors.textEditor.validationStatus)).toHaveText('');
    await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toHaveText('No issues');
    await app.cancelTextEditor();
  });
});

test.describe('Save snapshot integrity', () => {
  test('a save pending validation cannot be written to a different clip', async ({ app }) => {
    // The defect: the snapshot carried no clip identity and the write read mutable
    // module globals, so activating Save on clip A, closing it, and opening clip B
    // wrote A's bytes over B.
    const aPath = await createTempFile('{"a":1}', 'json');
    const bPath = await createTempFile('B ORIGINAL', 'txt');
    const aName = path.basename(aPath);
    const bName = path.basename(bPath);
    await app.uploadFile(aPath);
    await app.uploadFile(bPath);

    // The race window is the validation await inside prepareSave — up to 1.5s with
    // nothing modal on screen, during which Escape and a fresh open are both
    // available. Winning that race by hand would be flaky, so the snapshot is
    // captured at the seam and then replayed after the editor has moved on, which is
    // exactly the state the guard exists to reject.
    await app.openTextEditor(aName);
    await app.setTextEditorContent('{"a":2}');
    // String form: TextClipEditor is a `const` in a classic script, so it is a
    // lexical global rather than a property of `window`.
    const snapshot = await app.page.evaluate(`(async () => {
      const snap = await TextClipEditor.prepareSave({});
      window.__pendingSnapshot = snap;
      return { clipID: snap.clipID, source: snap.source };
    })()`) as { clipID: number; source: string };
    expect(snapshot.source).toBe('{"a":2}');

    await app.page.evaluate(() => (window as any).closeEditor({ force: true, discardDraft: true }));
    await expect(app.page.locator(selectors.textEditor.modal)).not.toHaveClass(/active/);
    await app.openTextEditor(bName);
    await app.expectTextEditorContent('B ORIGINAL');

    // Replay clip A's save while the editor is on clip B.
    await app.page.evaluate(async () => {
      await (window as any).performSaveEditorInPlace((window as any).__pendingSnapshot);
    });
    await app.waitForToast(/moved to a different clip/);

    // B is untouched, and the editor is still on B.
    await expect(app.page.locator(selectors.textEditor.modal)).toHaveClass(/active/);
    await app.expectTextEditorContent('B ORIGINAL');
    await app.cancelTextEditor();
    const bBytes = await app.page.evaluate(async (name) => {
      const App = (window as any).go.main.App;
      const clips = await App.GetClips(false, [], [], 'created_at', 'desc');
      const clip = clips.find((c: any) => c.filename === name);
      // Desktop exposes GetClipData; GetClipText is the server-mode addition.
      const data = await App.GetClipData(clip.id);
      return data.data_encoding === 'base64'
        ? new TextDecoder().decode(Uint8Array.from(atob(data.data), (ch) => ch.charCodeAt(0)))
        : data.data;
    }, bName);
    expect(bBytes).toBe('B ORIGINAL');
  });

  test('formatting is locked out while a save is pending', async ({ app }) => {
    // The defect: Format advanced the shared validation generation, terminating the
    // save's in-flight validation, so the save proceeded on an empty result and
    // skipped Save Anyway entirely. The spec also says formatting never runs during
    // a save.
    const jsonPath = await createTempFile('{"a":1}', 'json');
    await app.uploadFile(jsonPath);
    await app.openTextEditor(path.basename(jsonPath));
    await app.setTextEditorContent('{"a":}');

    await app.page.locator(selectors.textEditor.saveButton).click();
    // The confirmation is up, so the save is mid-flight.
    await expect(app.page.locator(selectors.confirm.dialog)).toHaveClass(/opacity-100/);
    await expect(app.page.locator(selectors.textEditor.formatJSONButton)).toBeDisabled();

    // Cancel: nothing was written, and formatting becomes available again once the
    // document is valid.
    await app.page.locator(selectors.confirm.cancelButton).click();
    await app.setTextEditorContent('{"a":1}');
    await expect(app.page.locator(selectors.textEditor.formatJSONButton)).toBeEnabled();
    await app.cancelTextEditor();
  });
});

test.describe('Diagnostics session isolation', () => {
  test('a previous clip’s diagnostics never land on the next one', async ({ app }) => {
    // The defect: open/close did not advance the validation generation, so a JSON
    // validation still in flight could apply its errors and inline markers to a
    // plain-text clip opened afterwards — a path that starts no validation of its
    // own, so nothing superseded the stale request.
    const jsonPath = await createTempFile('{"a":}', 'json');
    const textPath = await createTempFile('just text', 'txt');
    await app.uploadFile(jsonPath);
    await app.uploadFile(textPath);

    await app.openTextEditor(path.basename(jsonPath));
    // Wait for the invalid JSON to actually report, so there is something that could
    // leak across.
    await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toContainText('error');
    await app.cancelTextEditor();

    await app.openTextEditor(path.basename(textPath));
    // Plain text has no validator at all: the drawer must report nothing, and no
    // inline marker may appear.
    await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toHaveText('No issues');
    // Short document, so a DOM count of markers is meaningful here.
    await expect(app.page.locator(selectors.textEditor.errorMarker)).toHaveCount(0);
    await app.cancelTextEditor();
  });
});

test.describe('Truncated diagnostics and save confirmation', () => {
  test('a document past the finding cap still confirms when its errors change', async ({ app }) => {
    // Both the open-time baseline and the save-time validation stop at 1,000
    // findings, so their first 1,000 can be identical while the document has in fact
    // changed past the cap. Treating that as "unchanged" would let an edit overwrite
    // stored bytes with no Save Anyway prompt, so a truncated comparison confirms.
    const source = `<r>${'&#xZZ;'.repeat(1100)}</r>`;
    const xmlPath = await createTempFile(source, 'xml');
    await app.uploadFile(xmlPath);
    await app.openTextEditor(path.basename(xmlPath));

    // The drawer reports the cap.
    await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toContainText('error');

    // Introduce a structural error beyond the truncation point.
    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+End');
    await app.page.locator(selectors.textEditor.editor).pressSequentially('<a>');

    await app.page.locator(selectors.textEditor.saveButton).click();
    // Must prompt rather than saving silently.
    await expect(app.page.locator(selectors.confirm.dialog)).toHaveClass(/opacity-100/);
    await app.page.locator(selectors.confirm.cancelButton).click();
    await app.cancelTextEditor();
  });
});

test.describe('Save session identity', () => {
  test('reopening the same clip is a new session and rejects the old snapshot', async ({ app }) => {
    // A clip ID alone is not identity. Closing a clip and reopening it reloads the
    // stored bytes, so a snapshot captured before the close describes a document that
    // is no longer on screen and must not be written.
    const jsonPath = await createTempFile('{"a":1}', 'json');
    const filename = path.basename(jsonPath);
    await app.uploadFile(jsonPath);

    await app.openTextEditor(filename);
    await app.setTextEditorContent('{"a":2}');
    await app.page.evaluate(`(async () => {
      window.__staleSnapshot = await TextClipEditor.prepareSave({});
    })()`);

    // Close and reopen the SAME clip: same clipID, new session.
    await app.page.evaluate(() => (window as any).closeEditor({ force: true, discardDraft: true }));
    await expect(app.page.locator(selectors.textEditor.modal)).not.toHaveClass(/active/);
    await app.openTextEditor(filename);
    await app.expectTextEditorContent('{"a":1}');

    const rejected = await app.page.evaluate(`TextClipEditor.snapshotStillTargets(window.__staleSnapshot)`);
    expect(rejected).toBe(false);

    // And replaying it writes nothing.
    await app.page.evaluate(async () => {
      await (window as any).performSaveEditorInPlace((window as any).__staleSnapshot);
    });
    await app.waitForToast(/moved to a different clip/);
    await app.expectTextEditorContent('{"a":1}');
    await app.cancelTextEditor();
  });
});

// --- Bounded CSV/TSV table preview -------------------------------------------

// Opens through the gallery card's view action — the generic open path — so the
// descriptor's own default mode decides, which for CSV and TSV is Preview.
async function openTablePreview(app: any, content: string, extension: 'csv' | 'tsv') {
  const filePath = await createTempFile(content, extension);
  const filename = path.basename(filePath);
  await app.uploadFile(filePath);
  await app.page.locator(selectors.gallery.clipCardByName(filename))
    .locator(selectors.clipActions.view).click();
  await expect(app.page.locator(selectors.textEditor.previewTab)).toHaveAttribute('aria-selected', 'true');
  return filename;
}

test.describe('CSV/TSV table preview', () => {
  test('a CSV opens in Preview as a real table with header semantics', async ({ app }) => {
    await openTablePreview(app, 'name,city\nada,london\nbob,paris\n', 'csv');

    const table = app.page.locator(selectors.textEditor.tablePreviewTable);
    await expect(table).toBeVisible();
    await expect(app.page.locator(selectors.textEditor.previewPanel)).toHaveAttribute('data-preview', 'table');
    // The detected header row becomes real column headers, not a first data row.
    await expect(app.page.locator(selectors.textEditor.tablePreviewHeaderCell)).toHaveText(['name', 'city']);
    await expect(app.page.locator(selectors.textEditor.tablePreviewRow)).toHaveCount(2);
    await expect(app.page.locator(selectors.textEditor.tablePreviewCell))
      .toHaveText(['ada', 'london', 'bob', 'paris']);
    // Row-number gutter cells are row headers.
    expect(await table.locator('tbody th[scope="row"]').count()).toBe(2);
    expect(await table.locator('thead th[scope="col"]').count()).toBe(2);
    // Nothing to warn about, so no notice.
    await expect(app.page.locator(selectors.textEditor.tablePreviewNote)).toHaveCount(0);
    await app.cancelTextEditor();
  });

  test('a TSV reads as tab-separated even when its cells contain commas', async ({ app }) => {
    await openTablePreview(app, 'name\ttags\nada\tx,y,z\nbob\tp,q,r\n', 'tsv');

    await expect(app.page.locator(selectors.textEditor.tablePreviewHeaderCell)).toHaveText(['name', 'tags']);
    await expect(app.page.locator(selectors.textEditor.tablePreviewCell))
      .toHaveText(['ada', 'x,y,z', 'bob', 'p,q,r']);
    await app.cancelTextEditor();
  });

  test('the controls belong to Preview and disappear in Edit', async ({ app }) => {
    await openTablePreview(app, 'a,b\n1,2\n', 'csv');
    await expect(app.page.locator(selectors.textEditor.tableControls)).toBeVisible();
    // Labelled with what detection actually chose, so a wrong table is diagnosable.
    await expect(app.page.locator(`${selectors.textEditor.tableDelimiterSelect} option[value=""]`))
      .toHaveText('Detected (comma)');
    await expect(app.page.locator(selectors.textEditor.tableSummary)).toHaveText('1 row × 2 columns · header row');

    await app.page.locator(selectors.textEditor.editTab).click();
    await expect(app.page.locator(selectors.textEditor.tableControls)).toBeHidden();
    await app.page.locator(selectors.textEditor.previewTab).click();
    await expect(app.page.locator(selectors.textEditor.tableControls)).toBeVisible();
    await app.cancelTextEditor();
  });

  test('the delimiter options match the ids the renderer understands', async ({ app }) => {
    // An option value the module does not recognize silently falls back to
    // detection, so the override would look wired up and do nothing.
    const parity = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor;
      const select = document.getElementById('editor-table-delimiter') as HTMLSelectElement;
      return {
        options: Array.from(select.options).map((option) => option.value),
        // '' is the auto option, which is not a delimiter.
        known: ['', ...api.TablePreviewRenderer.DELIMITERS.map((d: any) => d.id)],
      };
    });
    expect(parity.options).toEqual(parity.known);
  });

  test('a non-table format never shows the controls', async ({ app }) => {
    const filePath = await createTempFile('{"a":1}', 'json');
    await app.uploadFile(filePath);
    await app.openTextEditor(path.basename(filePath));
    await app.page.locator(selectors.textEditor.previewTab).click();
    await expect(app.page.locator(selectors.textEditor.sourcePreview)).toBeVisible();
    await expect(app.page.locator(selectors.textEditor.tableControls)).toBeHidden();
    await app.cancelTextEditor();
  });

  test('the table controls are inside the modal focus trap', async ({ app }) => {
    await openTablePreview(app, 'a,b\n1,2\n', 'csv');
    // The trap's own query filters on offsetParent, so it only sees laid-out
    // controls. Waiting for visibility first is what keeps this from racing the
    // modal's open animation.
    await expect(app.page.locator(selectors.textEditor.tableControls)).toBeVisible();
    await expect.poll(() => app.page.evaluate(() => {
      const modal = document.getElementById('editor-modal')!;
      const focusable = Array.from(modal.querySelectorAll(
        'button:not([disabled]):not([tabindex="-1"]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [contenteditable="true"]:not([tabindex="-1"]), [tabindex]:not([tabindex="-1"])',
      )).filter((el) => (el as HTMLElement).offsetParent !== null);
      return {
        delimiter: focusable.includes(document.getElementById('editor-table-delimiter')!),
        header: focusable.includes(document.getElementById('editor-table-header')!),
      };
    })).toEqual({ delimiter: true, header: true });
    await app.cancelTextEditor();
  });

  test('Preview reflects unsaved source', async ({ app }) => {
    await openTablePreview(app, 'a,b\n1,2\n', 'csv');
    await app.page.locator(selectors.textEditor.editTab).click();
    await app.setTextEditorContent('a,b\n1,2\n3,4\n');
    await app.page.locator(selectors.textEditor.previewTab).click();
    await expect(app.page.locator(selectors.textEditor.tablePreviewRow)).toHaveCount(2);
    await expect(app.page.locator(selectors.textEditor.tablePreviewCell)).toHaveText(['1', '2', '3', '4']);
    await app.cancelTextEditor();
  });

  test('a document over 2 MiB gets no table at all', async ({ app }) => {
    // Table rendering is disabled above the enhanced-assistance threshold along
    // with highlighting, validation, diagnostics and formatting.
    const big = `a,b,c\n${'x,y,z\n'.repeat(360000)}`;
    const filePath = await createTempFile(big, 'csv');
    const filename = path.basename(filePath);
    await app.uploadFile(filePath);
    await app.page.locator(selectors.gallery.clipCardByName(filename))
      .locator(selectors.clipActions.view).click();

    await expect(app.page.locator(selectors.textEditor.degradedNotice)).toBeVisible();
    // Plain inert source in both modes, and no interpretation controls to offer.
    await app.page.locator(selectors.textEditor.previewTab).click();
    await expect(app.page.locator(selectors.textEditor.sourcePreviewPlain)).toBeVisible();
    await expect(app.page.locator(selectors.textEditor.tablePreview)).toHaveCount(0);
    await expect(app.page.locator(selectors.textEditor.tableControls)).toBeHidden();
    await app.cancelTextEditor();
  });
});

test.describe('CSV/TSV interpretation overrides', () => {
  // Both delimiters read this document cleanly — two fields per record, no
  // deviations, no errors — so detection falls all the way through to the tie order
  // and picks comma. It is exactly the case the override exists for: the table
  // looks plausible and is not the one the author meant.
  const AMBIGUOUS = 'name,x;tags\nada,1;p\nbob,2;q\n';

  test('choosing a delimiter repaints the table', async ({ app }) => {
    await openTablePreview(app, AMBIGUOUS, 'csv');
    await expect(app.page.locator(selectors.textEditor.tablePreviewHeaderCell)).toHaveText(['name', 'x;tags']);

    await app.page.locator(selectors.textEditor.tableDelimiterSelect).selectOption('semicolon');
    await expect(app.page.locator(selectors.textEditor.tablePreviewHeaderCell)).toHaveText(['name,x', 'tags']);
    await expect(app.page.locator(selectors.textEditor.tablePreviewCell))
      .toHaveText(['ada,1', 'p', 'bob,2', 'q']);
    await expect(app.page.locator(`${selectors.textEditor.tableDelimiterSelect} option[value=""]`))
      .toHaveText('Detected (comma)');
    await app.cancelTextEditor();
  });

  test('the drawer follows the chosen delimiter, not the detected one', async ({ app }) => {
    // Detected as semicolon: a malformed quote on line 2 and a ragged line 3.
    await openTablePreview(app, 'a;b\n"1"x;2\n3\n', 'csv');
    const summary = app.page.locator(selectors.textEditor.diagnosticsSummary);
    await expect(summary).toHaveText('2 possible issues');

    // Read as comma, every record is a single field, so nothing is ragged any more
    // and only the quote finding survives. A drawer that kept reporting the
    // semicolon interpretation would be describing a table the user cannot see.
    await app.page.locator(selectors.textEditor.tableDelimiterSelect).selectOption('comma');
    await expect(summary).toHaveText('1 possible issue');
    await app.cancelTextEditor();
  });

  test('overriding the header changes presentation only, and neither override survives a close', async ({ app }) => {
    const filename = await openTablePreview(app, AMBIGUOUS, 'csv');
    await app.page.locator(selectors.textEditor.tableDelimiterSelect).selectOption('semicolon');
    await app.page.locator(selectors.textEditor.tableHeaderSelect).selectOption('off');

    // The former header row drops into the body; the strip becomes column numbers.
    await expect(app.page.locator(selectors.textEditor.tablePreviewHeaderCell)).toHaveText(['1', '2']);
    await expect(app.page.locator(selectors.textEditor.tablePreviewRow)).toHaveCount(3);
    // The source is untouched, so the clip is not dirty.
    await expect(app.page.locator(selectors.textEditor.saveButton)).toBeDisabled();
    await app.expectTextEditorContent(AMBIGUOUS);

    await app.cancelTextEditor();
    await app.page.locator(selectors.gallery.clipCardByName(filename))
      .locator(selectors.clipActions.view).click();

    // Back to detection: comma, header auto. Nothing persisted the choices.
    await expect(app.page.locator(selectors.textEditor.tableDelimiterSelect)).toHaveValue('');
    await expect(app.page.locator(selectors.textEditor.tableHeaderSelect)).toHaveValue('auto');
    await expect(app.page.locator(selectors.textEditor.tablePreviewHeaderCell)).toHaveText(['name', 'x;tags']);
    await app.cancelTextEditor();
  });

  test('a document with no discoverable delimiter says so', async ({ app }) => {
    await openTablePreview(app, 'just\nsome\nlines\n', 'csv');
    await expect(app.page.locator(selectors.textEditor.tablePreviewNote))
      .toContainText('No delimiter could be determined');
    await expect(app.page.locator(`${selectors.textEditor.tableDelimiterSelect} option[value=""]`))
      .toHaveText('Detected (inconclusive)');
    // Choosing one is the way out, and it works. The auto option keeps saying
    // detection is inconclusive, because it still is — the user answered the
    // question rather than changing the answer.
    await app.page.locator(selectors.textEditor.tableDelimiterSelect).selectOption('pipe');
    await expect(app.page.locator(selectors.textEditor.tablePreviewNote)).toHaveCount(0);
    await expect(app.page.locator(`${selectors.textEditor.tableDelimiterSelect} option[value=""]`))
      .toHaveText('Detected (inconclusive)');
    await app.cancelTextEditor();
  });
});

test.describe('CSV/TSV findings are always nonblocking', () => {
  test('ragged rows and malformed quotes are possible issues that never confirm a save', async ({ app }) => {
    // A ragged table AND a malformed quote, i.e. everything CSV can complain about.
    const filename = await openTablePreview(app, 'a,b,c\n1,"2\n3\n', 'csv');

    const summary = app.page.locator(selectors.textEditor.diagnosticsSummary);
    await expect(summary).toContainText('possible issue');
    // Never an error, in any state: the count says "possible issues" and the
    // per-format validation status stays empty because CSV is not authoritative.
    await expect(summary).not.toContainText('error');
    await expect(app.page.locator(selectors.textEditor.validationStatus)).toHaveText('');

    await app.page.locator(selectors.textEditor.diagnosticsToggle).click();
    const rows = app.page.locator(selectors.textEditor.diagnosticsRow);
    await expect(rows.first()).toHaveAttribute('data-severity', 'possible-issue');

    // Editing keeps them nonblocking, and saving does not prompt.
    await app.page.locator(selectors.textEditor.editTab).click();
    await app.setTextEditorContent('a,b,c\n1,"2\n3\n4\n');
    await expect(summary).toContainText('possible issue');
    await app.page.locator(selectors.textEditor.saveButton).click();
    await expectNoConfirmDialog(app);
    await app.page.waitForSelector(`${selectors.textEditor.modal}:not(.active)`);
    await app.expectClipVisible(filename);
  });

  test('an explicitly chosen delimiter does not promote a finding into a save gate', async ({ app }) => {
    // The cut idea, kept cut: an earlier draft made malformed quoting authoritative
    // once the delimiter was explicit. It never was, and this proves it.
    await openTablePreview(app, 'a;b\n"1"x;2\n3\n', 'csv');
    await app.page.locator(selectors.textEditor.tableDelimiterSelect).selectOption('semicolon');

    const summary = app.page.locator(selectors.textEditor.diagnosticsSummary);
    await expect(summary).toContainText('possible issue');
    await expect(summary).not.toContainText('error');

    await app.page.locator(selectors.textEditor.editTab).click();
    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+End');
    await app.page.locator(selectors.textEditor.editor).pressSequentially('4;5;6\n');
    await app.page.locator(selectors.textEditor.saveButton).click();
    await expectNoConfirmDialog(app);
    await app.page.waitForSelector(`${selectors.textEditor.modal}:not(.active)`);
  });

  test('a truncated table says so and points at Edit', async ({ app }) => {
    // 600 data rows: the row limit binds and nothing else does.
    await openTablePreview(app, `a,b\n${'1,2\n'.repeat(600)}`, 'csv');
    await expect(app.page.locator(selectors.textEditor.tablePreviewRow)).toHaveCount(500);
    const note = app.page.locator(selectors.textEditor.tablePreviewNote);
    await expect(note).toContainText('the first 500 of 600 rows');
    await expect(note).toContainText('Switch to Edit for the complete source');
    // Truncation is presentation: the editor still holds every row.
    await app.page.locator(selectors.textEditor.editTab).click();
    await expect(app.page.locator(selectors.textEditor.characterStatus)).toHaveText('2404 characters');
    await app.cancelTextEditor();
  });
});

test.describe('Language modes in the editor', () => {
  test('highlighting appears in both Edit and Preview, in the same classes', async ({ app }) => {
    // Short documents on purpose: CodeMirror virtualizes off-screen lines, so a
    // count of token spans is only meaningful when the whole document is in view.
    const cases: Array<[string, string, string]> = [
      ['{"a": 1}', 'json', 'source-token-property'],
      ['# c\na: 1\n', 'yaml', 'source-token-comment'],
      ['# c\na = 1\n', 'toml', 'source-token-comment'],
      ['// c\nconst a = 1;\n', 'js', 'source-token-keyword'],
      ['<p class="x">t</p>\n', 'html', 'source-token-tag'],
      ['# c\necho hi\n', 'sh', 'source-token-comment'],
    ];

    for (const [content, extension, tokenClass] of cases) {
      const filePath = await createTempFile(content, extension);
      await app.uploadFile(filePath);
      await app.openTextEditor(path.basename(filePath));

      // Edit: highlighting selected from the descriptor, never from the content.
      await expect(app.page.locator(`${selectors.textEditor.mount} .${tokenClass}`).first())
        .toBeVisible();

      await app.page.locator(selectors.textEditor.previewTab).click();
      // Preview: the same tokens in the same classes, from the same Lezer tree.
      await expect(app.page.locator(`#source-preview-content .${tokenClass}`).first())
        .toBeVisible();
      // Still text nodes and app-owned spans — no element came from the source.
      await expect(app.page.locator('#source-preview-content script')).toHaveCount(0);
      await expect(app.page.locator('#source-preview-content a')).toHaveCount(0);

      await app.cancelTextEditor();
    }
  });

  test('a plain-text clip is highlighted in neither panel', async ({ app }) => {
    // Plain text has no language in the registry. That is a real answer, and it must
    // not mean "highlight it as something".
    const filePath = await createTempFile('just text {[(\nsecond line\n', 'txt');
    await app.uploadFile(filePath);
    await app.openTextEditor(path.basename(filePath));

    await expect(app.page.locator(selectors.textEditor.editorToken)).toHaveCount(0);
    await app.page.locator(selectors.textEditor.previewTab).click();
    // Still line-numbered inert source, just with nothing coloured.
    await expect(app.page.locator(selectors.textEditor.sourcePreviewLines)).toHaveCount(3);
    await expect(app.page.locator(selectors.textEditor.sourcePreviewToken)).toHaveCount(0);
    await app.cancelTextEditor();
  });

  test('language parsing is off above the threshold and returns below it', async ({ app }) => {
    // Just over 2 MiB of valid JSON. Above the threshold there is no language
    // extension at all: plain editing, plain inert Preview, no parsing anywhere.
    const big = `[${Array.from({ length: 110000 }, (_, i) => `"item-${i}-${'x'.repeat(14)}"`).join(',')}]`;
    const filePath = await createTempFile(big, 'json');
    await app.uploadFile(filePath);
    await app.openTextEditor(path.basename(filePath));

    await expect(app.page.locator(selectors.textEditor.degradedNotice)).toBeVisible();
    await expect(app.page.locator(selectors.textEditor.editorToken)).toHaveCount(0);

    await app.page.locator(selectors.textEditor.previewTab).click();
    await expect(app.page.locator(selectors.textEditor.sourcePreviewPlain)).toHaveCount(1);
    await expect(app.page.locator(selectors.textEditor.sourcePreviewToken)).toHaveCount(0);

    // The gate is re-evaluated on every edit rather than fixed at open, so deleting
    // back under the threshold has to restore the language extension. Fixing it at
    // open would leave this document plain for the rest of the session.
    await app.page.locator(selectors.textEditor.editTab).click();
    await app.setTextEditorContent('{"a": 1}');
    await expect(app.page.locator(selectors.textEditor.degradedNotice)).toBeHidden();
    await expect(app.page.locator(`${selectors.textEditor.mount} .source-token-property`).first())
      .toBeVisible();
    await app.cancelTextEditor();
  });

  test('a language mode adds highlighting only, not an editing opinion', async ({ app }) => {
    // The lang-* packages export both a bare Language and a LanguageSupport bundle.
    // The bundles carry completion sources and `autoCloseTags`, and this is the
    // regression that proves only the Language is mounted: with autoCloseTags, typing
    // `<r><a></r>` in an XML clip becomes `<r><a></r></a></r>` — the editor silently
    // rewriting what the user typed.
    const xmlPath = await createTempFile('<r></r>', 'xml');
    await app.uploadFile(xmlPath);
    await app.openTextEditor(path.basename(xmlPath));
    await app.setTextEditorContent('<r><a></r>');
    await app.expectTextEditorContent('<r><a></r>');
    // Highlighting is present all the same, so this is not "no language mode".
    await expect(app.page.locator(`${selectors.textEditor.mount} .source-token-tag`).first())
      .toBeVisible();
    await app.cancelTextEditor();

    // Nor is a completion popup mounted anywhere: no autocompletion extension is
    // registered, so there is no tooltip layer for one to appear in.
    const jsPath = await createTempFile('const alpha = 1;\nconst b = alph', 'js');
    await app.uploadFile(jsPath);
    await app.openTextEditor(path.basename(jsPath));
    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+End');
    await app.page.locator(selectors.textEditor.editor).pressSequentially('a');
    await expect(app.page.locator('#text-editor-mount .cm-tooltip-autocomplete')).toHaveCount(0);
    await expect(app.page.locator(selectors.textEditor.stockSearchPanel)).toHaveCount(0);
    await app.cancelTextEditor();
  });

  test('Lezer possible issues are counted, marked, and never gate a save', async ({ app }) => {
    // JavaScript has no authoritative validator: its findings come from the error
    // recovery already required for highlighting, and they are advisory by design.
    const filePath = await createTempFile('const a = 1;\n', 'js');
    await app.uploadFile(filePath);
    await app.openTextEditor(path.basename(filePath));

    const summary = app.page.locator(selectors.textEditor.diagnosticsSummary);
    await expect(summary).toHaveText('No issues');
    // No "Valid JavaScript" either: a non-authoritative validator makes no claim in
    // either direction.
    await expect(app.page.locator(selectors.textEditor.validationStatus)).toHaveText('');

    await app.setTextEditorContent('const a = ;\n');
    await expect(summary).toContainText('possible issue');
    await expect(summary).not.toContainText('error');
    await expect(app.page.locator(selectors.textEditor.validationStatus)).toHaveText('');
    await expect(app.page.locator(selectors.textEditor.errorMarker)).toHaveCount(0);
    await expect(app.page.locator(selectors.textEditor.possibleMarker).first()).toBeVisible();

    // The document is broken and the save goes through without a prompt, because no
    // authoritative syntax error was ever established.
    await app.page.locator(selectors.textEditor.saveButton).click();
    await expectNoConfirmDialog(app);
    await app.page.waitForSelector(`${selectors.textEditor.modal}:not(.active)`);
  });

  test('shell, .env and INI are highlighted and produce no diagnostics at all', async ({ app }) => {
    // Highlighting is where the value is for these; their dialects are too ambiguous
    // for even an advisory grammar promise, so they get no validator of any kind.
    const shellPath = await createTempFile('# c\nif [ -f x ]; then\n  echo "hi"\nfi\n', 'sh');
    const iniPath = await createTempFile('# c\n[section]\nkey = value\n', 'ini');
    const envDir = await createTempDir();
    const envPath = path.join(envDir, '.env');
    await writeFile(envPath, '# c\nSECRET=hunter2\n');

    for (const filePath of [shellPath, iniPath, envPath]) {
      await app.uploadFile(filePath);
      await app.openTextEditor(path.basename(filePath));

      await expect(app.page.locator(`${selectors.textEditor.mount} .source-token-comment`).first())
        .toBeVisible();
      await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toHaveText('No issues');
      await expect(app.page.locator(selectors.textEditor.validationStatus)).toHaveText('');

      await app.page.locator(selectors.textEditor.previewTab).click();
      await expect(app.page.locator('#source-preview-content .source-token-comment').first())
        .toBeVisible();
      await expect(app.page.locator(selectors.textEditor.diagnosticsSummary)).toHaveText('No issues');
      await app.cancelTextEditor();
    }
  });
});
