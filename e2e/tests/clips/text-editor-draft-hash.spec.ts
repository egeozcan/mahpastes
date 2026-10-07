import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';
import type { Page } from '@playwright/test';

// A save that lands while the user keeps typing rebaselines the editor onto the
// saved bytes but leaves it dirty. The draft that protects those newer edits must
// be keyed to the *new* baseline: a draft naming the pre-save original (or, for a
// >256K document, the new length with the old memoized hash) is rejected and
// deleted on reopen, silently losing the edits.

const DRAFT_PREFIX = 'mahpastes:text-editor-draft:v2:';

async function holdNextWrite(page: Page): Promise<void> {
  await page.evaluate(() => {
    const App = (window as any).go.main.App;
    (window as any).__realUpdate = App.UpdateClipData;
    (window as any).__release = null;
    App.UpdateClipData = (id: number, contentType: string, data: string, name: string) =>
      new Promise((resolve, reject) => {
        (window as any).__release = () => {
          App.UpdateClipData = (window as any).__realUpdate;
          (window as any).__realUpdate.call(App, id, contentType, data, name).then(resolve, reject);
        };
      });
  });
}

async function restoreWrite(page: Page): Promise<void> {
  await page.evaluate(() => {
    const App = (window as any).go.main.App;
    if ((window as any).__realUpdate) App.UpdateClipData = (window as any).__realUpdate;
  });
}

async function draftTextStartsWith(page: Page, prefix: string): Promise<void> {
  await page.waitForFunction(
    ({ draftPrefix, want }) => {
      const key = Object.keys(localStorage).find((k) => k.startsWith(draftPrefix));
      if (!key) return false;
      try {
        return JSON.parse(localStorage.getItem(key)!).text.startsWith(want);
      } catch (_) {
        return false;
      }
    },
    { draftPrefix: DRAFT_PREFIX, want: prefix },
    { timeout: 10000 },
  );
}

async function saveWhileTyping(page: Page, typedDuringSave: string): Promise<void> {
  await holdNextWrite(page);
  try {
    await page.locator(selectors.textEditor.saveButton).click();
    await page.waitForFunction(() => (window as any).__release !== null, { timeout: 10000 });

    await page.locator(selectors.textEditor.editor).click();
    await page.keyboard.press('ControlOrMeta+Home');
    await page.keyboard.type(typedDuringSave);
    // The draft for these edits is written against the pre-save baseline.
    await draftTextStartsWith(page, typedDuringSave);

    await page.evaluate(() => (window as any).__release());
  } finally {
    await restoreWrite(page);
  }
}

async function reloadAndReopen(app: any, filename: string): Promise<void> {
  await app.page.reload();
  await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });
  await app.expectClipVisible(filename);
  await app.openTextEditor(filename);
}

test.describe('Text editor draft after a save that raced typing', () => {
  test('a large clip keeps edits typed after the save across a reload', async ({ app }) => {
    const original = 'line of original text\n'.repeat(20000); // ~440 KB, hashed draft path
    const textPath = await createTempFile(original, 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);
    await app.openTextEditor(filename);

    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+Home');
    await app.page.keyboard.type('SAVED ');

    await saveWhileTyping(app.page, 'DURING ');
    await app.waitForToast(/Edits made while saving are still unsaved/);

    // Keep typing after the rebaseline: this draft must carry the new baseline's
    // hash, not the memoized hash of the text the editor opened with.
    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+Home');
    await app.page.keyboard.type('AFTER ');
    await draftTextStartsWith(app.page, 'AFTER DURING SAVED ');

    await reloadAndReopen(app, filename);
    await expect(app.page.locator(selectors.textEditor.draftStatus)).toHaveText('Recovered draft');
    const head = await app.page.evaluate('TextClipEditor.getValue().slice(0, 19)');
    expect(head).toBe('AFTER DURING SAVED ');
    await app.cancelTextEditor();
  });

  test('a large clip keeps edits typed during the save across a reload without further typing', async ({ app }) => {
    const original = 'line of original text\n'.repeat(20000);
    const textPath = await createTempFile(original, 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);
    await app.openTextEditor(filename);

    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+Home');
    await app.page.keyboard.type('SAVED ');

    await saveWhileTyping(app.page, 'DURING ');
    await app.waitForToast(/Edits made while saving are still unsaved/);

    await reloadAndReopen(app, filename);
    await expect(app.page.locator(selectors.textEditor.draftStatus)).toHaveText('Recovered draft');
    const head = await app.page.evaluate('TextClipEditor.getValue().slice(0, 13)');
    expect(head).toBe('DURING SAVED ');
    await app.cancelTextEditor();
  });

  test('a small clip keeps edits typed during the save across a reload without further typing', async ({ app }) => {
    const textPath = await createTempFile('small original', 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);
    await app.openTextEditor(filename);
    await app.setTextEditorContent('saved text');

    await saveWhileTyping(app.page, 'DURING ');
    await app.waitForToast(/Edits made while saving are still unsaved/);

    // The stored draft is re-keyed at once, not only by the beforeunload
    // persist (which a crash or force quit never runs).
    await app.page.waitForFunction(
      (draftPrefix) => {
        const key = Object.keys(localStorage).find((k) => k.startsWith(draftPrefix));
        if (!key) return false;
        try {
          return JSON.parse(localStorage.getItem(key)!).originalText === 'saved text';
        } catch (_) {
          return false;
        }
      },
      DRAFT_PREFIX,
      { timeout: 5000 },
    );

    await reloadAndReopen(app, filename);
    await expect(app.page.locator(selectors.textEditor.draftStatus)).toHaveText('Recovered draft');
    await app.expectTextEditorContent('DURING saved text');
    await app.cancelTextEditor();
  });
});
