import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
import { generateTestImage, createTempFile } from '../../helpers/test-data.js';
import * as fs from 'fs/promises';
import * as path from 'path';
import type { Page } from '@playwright/test';

// How every layered surface (dialogs, popovers, full-screen viewers) shares
// the keyboard: global keys must not open things behind a viewer, Escape must
// close only the topmost layer, and focus must move into a dialog and come
// back to whatever opened it.

/** Open = not inert, not hidden, not faded out — how every modal signals it. */
async function isLayerOpen(page: Page, selector: string): Promise<boolean> {
  return page.evaluate((sel) => {
    const el = document.querySelector(sel);
    return !!el && !el.hasAttribute('inert') && !el.classList.contains('hidden')
      && !el.classList.contains('opacity-0');
  }, selector);
}

async function expectLayerOpen(page: Page, selector: string, open: boolean): Promise<void> {
  await expect.poll(() => isLayerOpen(page, selector), { timeout: 5000 }).toBe(open);
}

async function focusIsInside(page: Page, selector: string): Promise<boolean> {
  return page.evaluate((sel) => !!document.activeElement?.closest(sel), selector);
}

async function expectFocusInside(page: Page, selector: string): Promise<void> {
  await expect.poll(() => focusIsInside(page, selector), { timeout: 5000 }).toBe(true);
}

/** Is the element the one actually painted at its own center? */
async function isTopmostAtCenter(page: Page, selector: string): Promise<boolean> {
  return page.evaluate((sel) => {
    const el = document.querySelector(sel) as HTMLElement | null;
    if (!el) return false;
    const panel = (el.firstElementChild as HTMLElement | null) || el;
    const r = panel.getBoundingClientRect();
    const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    return !!hit && el.contains(hit);
  }, selector);
}

async function uploadImage(app: any): Promise<string> {
  const imagePath = await createTempFile(generateTestImage(64, 64, '#884422'), 'png');
  await app.uploadFile(imagePath);
  await app.expectClipCount(1);
  return path.basename(imagePath);
}

test.describe('Modal layering and keyboard ownership', () => {

  test.describe('global keys behind full-screen viewers', () => {
    test('m, comma and ? do nothing while the lightbox is open', async ({ app }) => {
      const name = await uploadImage(app);
      await app.openLightbox(name);

      await app.page.keyboard.press('m');
      await app.page.keyboard.press(',');
      await app.page.keyboard.press('Shift+/');

      await expect(app.page.locator(selectors.drawer.panel)).toHaveClass(/translate-x-full/);
      await expect(app.page.locator(selectors.settings.modal)).toHaveClass(/opacity-0/);
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-0/);
      await expect(app.page.locator(selectors.lightbox.overlay)).toHaveClass(/active/);

      // Lightbox keys still belong to the lightbox.
      await app.page.keyboard.press('Escape');
      await expect(app.page.locator(selectors.lightbox.overlay)).not.toHaveClass(/active/);
    });

    test('m and comma do nothing while the image editor is open', async ({ app }) => {
      const name = await uploadImage(app);
      await app.openImageEditor(name);

      await app.page.locator('#editor-canvas-container').click({ position: { x: 5, y: 5 } }).catch(() => {});
      await app.page.keyboard.press('m');
      await app.page.keyboard.press(',');

      await expect(app.page.locator(selectors.drawer.panel)).toHaveClass(/translate-x-full/);
      await expect(app.page.locator(selectors.settings.modal)).toHaveClass(/opacity-0/);
    });
  });

  test.describe('Escape reaches components that own it', () => {
    test('Escape in the merge autocomplete closes only the suggestions', async ({ app }) => {
      await app.createTag('merge-src');
      await app.createTag('merge-dst');
      await app.openMergeModal('merge-src');

      const input = app.page.locator('#merge-tag-dest-input');
      await input.fill('merge-d');
      const listbox = app.page.locator('#merge-tag-dest-input + [role="listbox"]');
      await expect(listbox).toBeVisible();

      await app.page.keyboard.press('Escape');
      await expect(listbox).toBeHidden();
      await expectLayerOpen(app.page, '#merge-tag-modal', true);

      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, '#merge-tag-modal', false);
    });

    test('Escape in the import wizard base-tag autocomplete keeps the wizard open', async ({ app, tempDir }) => {
      await fs.writeFile(path.join(tempDir, 'a.txt'), 'alpha');
      await app.createTag('trips');
      await app.openImportWizard(tempDir, { recursive: false });

      const input = app.page.locator(selectors.importWizard.baseTagInput);
      await input.click();
      await input.fill('tri');
      const listbox = app.page.locator(`${selectors.importWizard.baseTagInput} + [role="listbox"]`);
      await expect(listbox).toBeVisible();

      await app.page.keyboard.press('Escape');
      await expect(listbox).toBeHidden();
      await expect(app.page.locator(selectors.importWizard.modal)).toHaveClass(/opacity-100/);
    });

    test('Escape closes the folder Move modal', async ({ app }) => {
      await app.createTag('move-src');
      await app.createTag('move-dst');
      await app.enterFolderMode();
      await app.page.click(selectors.folderCard('move-src'), { button: 'right' });
      await app.page.click(selectors.folderContextMenuItem('move'));
      const modal = app.page.locator('[data-testid="folder-move-modal"]');
      await expect(modal).toBeVisible();
      await expectFocusInside(app.page, '[data-testid="folder-move-modal"]');

      await app.page.keyboard.press('Escape');
      await expect(modal).toBeHidden();
    });

    test('Escape closes the search options popover and returns focus to its button', async ({ app }) => {
      await app.page.click('#search-options-btn');
      await expect(app.page.locator('.search-options-popover')).toBeVisible();

      await app.page.keyboard.press('Escape');
      await expect(app.page.locator('.search-options-popover')).toHaveCount(0);
      await expect(app.page.locator('#search-options-btn')).toBeFocused();
    });
  });

  test.describe('stacked dialogs', () => {
    test('Escape on a confirm stacked over Settings closes only the confirm', async ({ app }) => {
      await app.page.locator('body').click();
      await app.page.keyboard.press(',');
      await expectLayerOpen(app.page, selectors.settings.modal, true);

      await app.page.evaluate(() => {
        (window as any).showConfirmDialog('Delete?', 'Really?', () => {});
      });
      await expectLayerOpen(app.page, selectors.confirm.dialog, true);

      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, selectors.confirm.dialog, false);
      await expectLayerOpen(app.page, selectors.settings.modal, true);
      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, selectors.settings.modal, false);
    });

    test('restore confirm takes focus, traps it, and Escape leaves Settings open', async ({ app }) => {
      await app.page.locator('body').click();
      await app.page.keyboard.press(',');
      await expectLayerOpen(app.page, selectors.settings.modal, true);
      await app.page.locator('#settings-close').focus();

      await app.page.evaluate(() => (window as any).showRestoreConfirmDialog());
      await expectLayerOpen(app.page, '#restore-confirm-dialog', true);
      await expectFocusInside(app.page, '#restore-confirm-dialog');

      for (let i = 0; i < 6; i++) await app.page.keyboard.press('Tab');
      expect(await focusIsInside(app.page, '#restore-confirm-dialog')).toBe(true);

      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, '#restore-confirm-dialog', false);
      await expectLayerOpen(app.page, selectors.settings.modal, true);
      await expect(app.page.locator('#settings-close')).toBeFocused();
      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, selectors.settings.modal, false);
    });

    test('Escape does not dismiss the restore confirm while a restore is running', async ({ app }) => {
      await app.page.evaluate(() => {
        const w = window as any;
        const api = w.go.main.App;
        const original = api.ConfirmRestoreBackup;
        w.__restoreRestoreStub = () => { api.ConfirmRestoreBackup = original; delete w.__restoreRestoreStub; };
        api.ConfirmRestoreBackup = () => new Promise((_, reject) => { w.__failRestore = () => reject('stopped by test'); });
      });
      try {
        await app.openSettingsModal();
        await app.page.evaluate(() => {
          // A path makes Confirm call the backend instead of just closing.
          // @ts-ignore - settings.js top-level let
          pendingRestorePath = '/tmp/not-a-real-backup.zip';
          (window as any).showRestoreConfirmDialog();
        });
        await expectLayerOpen(app.page, '#restore-confirm-dialog', true);
        await app.page.locator('#restore-confirm-yes').click();
        await expect(app.page.locator('#restore-confirm-yes')).toHaveText('Restoring...');

        await app.page.keyboard.press('Escape');
        await app.page.evaluate(() => new Promise(requestAnimationFrame));
        await expectLayerOpen(app.page, '#restore-confirm-dialog', true);
        await app.page.locator('#restore-confirm-cancel').click({ force: true });
        await expectLayerOpen(app.page, '#restore-confirm-dialog', true);

        await app.page.evaluate(() => (window as any).__failRestore());
        await expect(app.page.locator('#restore-confirm-yes')).toHaveText('Delete & Restore');
        await app.page.keyboard.press('Escape');
        await expectLayerOpen(app.page, '#restore-confirm-dialog', false);
      } finally {
        // Settle the pending restore too, or restoreInFlight leaks into the next test.
        await app.page.evaluate(() => {
          const w = window as any;
          w.__failRestore?.();
          delete w.__failRestore;
          w.__restoreRestoreStub?.();
        });
      }
    });

    test('plugin review takes focus, and a second review settles the first as cancelled', async ({ app }) => {
      await app.page.evaluate(() => {
        const w = window as any;
        w.__firstReview = 'pending';
        w.showPluginReview({ name: 'one', version: '1.0.0' }, 'install')
          .then((v: boolean) => { w.__firstReview = v; });
        w.showPluginReview({ name: 'two', version: '1.0.0' }, 'install')
          .then((v: boolean) => { w.__secondReview = v; });
      });
      await expectLayerOpen(app.page, '#plugin-review-modal', true);
      await expectFocusInside(app.page, '#plugin-review-modal');
      await expect.poll(() => app.page.evaluate(() => (window as any).__firstReview)).toBe(false);

      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, '#plugin-review-modal', false);
      await expect.poll(() => app.page.evaluate(() => (window as any).__secondReview)).toBe(false);
    });

    test('a confirm stacked on a prompt returns focus to the prompt, and the prompt to its opener', async ({ app }) => {
      await app.page.locator('#search-options-btn').focus();
      await app.page.evaluate(() => (window as any).showPromptDialog('Name', 'x', () => {}));
      await expect(app.page.locator(selectors.prompt.input)).toBeFocused();

      await app.page.evaluate(() => (window as any).showConfirmDialog('Sure?', 'msg', () => {}));
      await expect(app.page.locator(selectors.confirm.cancelButton)).toBeFocused();

      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, selectors.confirm.dialog, false);
      await expect(app.page.locator(selectors.prompt.input)).toBeFocused();

      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, selectors.prompt.dialog, false);
      await expect(app.page.locator('#search-options-btn')).toBeFocused();
    });
  });

  test.describe('rename prompt from the lightbox', () => {
    test('Escape closes the rename prompt and leaves the lightbox open', async ({ app }) => {
      const name = await uploadImage(app);
      await app.openLightbox(name);
      await app.page.click(selectors.lightbox.fileTrigger);
      await app.page.click(selectors.cardMenu.rename);
      await expectLayerOpen(app.page, selectors.prompt.dialog, true);
      await expect(app.page.locator(selectors.prompt.input)).toBeFocused();

      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, selectors.prompt.dialog, false);
      await expect(app.page.locator(selectors.lightbox.overlay)).toHaveClass(/active/);
    });
  });

  test.describe('clip popovers', () => {
    test('t opens the tag popover with focus inside; Escape closes it and refocuses the card', async ({ app }) => {
      const name = await uploadImage(app);
      await app.createTag('alpha');
      const card = await app.getClipByFilename(name);
      await card.focus();
      await app.page.keyboard.press('t');

      await expect(app.page.locator('#tag-popover')).toBeVisible();
      await expectFocusInside(app.page, '#tag-popover');

      await app.page.keyboard.press('Escape');
      await expect(app.page.locator('#tag-popover')).toBeHidden();
      await expect(card).toBeFocused();
    });

    test('expiration popover takes focus; Escape closes it and returns focus', async ({ app }) => {
      const name = await uploadImage(app);
      await app.openCardMenu(name);
      await app.page.click(selectors.cardMenu.setExpiration);

      const popover = app.page.locator(selectors.expiration.popover);
      await expect(popover).toBeVisible();
      await expectFocusInside(app.page, selectors.expiration.popover);

      await app.page.keyboard.press('Escape');
      await expect(popover).toHaveCount(0);
      const card = await app.getClipByFilename(name);
      await expect.poll(() => card.evaluate((el) => el.contains(document.activeElement))).toBe(true);
    });

    test('Metadata and Set Expiration from the lightbox render above it', async ({ app }) => {
      const name = await uploadImage(app);
      await app.openLightbox(name);

      await app.page.click(selectors.lightbox.fileTrigger);
      await app.page.click(selectors.cardMenu.metadata);
      await expectLayerOpen(app.page, selectors.metadata.modal, true);
      await expect.poll(() => isTopmostAtCenter(app.page, selectors.metadata.modal)).toBe(true);
      await expectFocusInside(app.page, selectors.metadata.modal);
      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, selectors.metadata.modal, false);
      await expect(app.page.locator(selectors.lightbox.overlay)).toHaveClass(/active/);

      await app.page.click(selectors.lightbox.fileTrigger);
      await app.page.click(selectors.cardMenu.setExpiration);
      await expect(app.page.locator(selectors.expiration.popover)).toBeVisible();
      await expect.poll(() => app.page.evaluate((sel) => {
        const el = document.querySelector(sel) as HTMLElement;
        const r = el.getBoundingClientRect();
        const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
        return !!hit && el.contains(hit);
      }, selectors.expiration.popover)).toBe(true);
      await app.page.keyboard.press('Escape');
      await expect(app.page.locator(selectors.expiration.popover)).toHaveCount(0);
    });
  });

  test.describe('gallery keys behind open dialogs', () => {
    const openers: Array<[string, string, string]> = [
      ['API modal', '#api-modal', 'openApiModal()'],
      ['queue modal', '#queue-modal', 'openQueueModal()'],
      ['prompt dialog', '#prompt-dialog', "showPromptDialog('Name', '', () => {})"],
      ['cheat sheet', '[data-testid="shortcuts-cheatsheet"]', 'ShortcutManager.openCheatSheet()'],
    ];
    for (const [label, selector, open] of openers) {
      test(`a does not toggle the archive behind the ${label}`, async ({ app }) => {
        await app.page.locator('body').click();
        await app.page.evaluate((code) => { (0, eval)(code); }, open);
        await expectLayerOpen(app.page, selector, true);
        // Move focus off any text field so single-letter keys reach the manager.
        await app.page.evaluate((sel) => {
          const el = document.querySelector(sel) as HTMLElement;
          const btn = el.querySelector('button') as HTMLElement | null;
          btn?.focus();
        }, selector);

        await app.page.keyboard.press('a');
        expect(await app.isArchiveViewActive()).toBe(false);

        await app.page.keyboard.press('Escape');
        await expectLayerOpen(app.page, selector, false);
      });
    }

    test('Escape with the cheat sheet open and a selection closes the sheet, not the selection', async ({ app }) => {
      const name = await uploadImage(app);
      await app.selectClip(name);
      expect(await app.getSelectedCount()).toBe(1);

      await app.page.evaluate(() => (0, eval)('ShortcutManager.openCheatSheet()'));
      await expectLayerOpen(app.page, selectors.shortcuts.cheatsheet, true);
      await app.page.keyboard.press('Escape');

      await expectLayerOpen(app.page, selectors.shortcuts.cheatsheet, false);
      expect(await app.getSelectedCount()).toBe(1);
    });

    test('gallery keys do not act on the hidden gallery from the share view', async ({ app }) => {
      await app.page.evaluate(() => (window as any).switchView('share'));
      await app.page.waitForFunction(() => !document.getElementById('share-view')?.classList.contains('hidden'));
      await app.page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());

      await app.page.keyboard.press('a');
      expect(await app.isArchiveViewActive()).toBe(false);
      expect(await app.page.evaluate(() => (0, eval)('ShortcutManager.getActiveContexts()'))).not.toContain('gallery');
      await app.page.evaluate(() => (window as any).switchView('clips'));
    });
  });

  test.describe('focus management', () => {
    const dialogs: Array<[string, string, string]> = [
      ['API modal', '#api-modal', 'openApiModal()'],
      ['queue modal', '#queue-modal', 'openQueueModal()'],
      ['plugin result modal', '#plugin-result-modal', "showPluginResultModal({ title: 'R', content: 'hello', format: 'text' })"],
    ];
    for (const [label, selector, open] of dialogs) {
      test(`${label} takes focus, closes on Escape and restores focus`, async ({ app }) => {
        await app.page.locator('#search-options-btn').focus();
        await app.page.evaluate((code) => { (0, eval)(code); }, open);
        await expectLayerOpen(app.page, selector, true);
        await expectFocusInside(app.page, selector);

        await app.page.keyboard.press('Escape');
        await expectLayerOpen(app.page, selector, false);
        await expect(app.page.locator('#search-options-btn')).toBeFocused();
      });
    }

    test('metadata modal from the card menu takes focus and restores it to the card', async ({ app }) => {
      const name = await uploadImage(app);
      await app.openCardMenu(name);
      await app.page.click(selectors.cardMenu.metadata);
      await expectLayerOpen(app.page, selectors.metadata.modal, true);
      await expectFocusInside(app.page, selectors.metadata.modal);

      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, selectors.metadata.modal, false);
      const card = await app.getClipByFilename(name);
      await expect.poll(() => card.evaluate((el) => el.contains(document.activeElement))).toBe(true);
    });

    test('share logs modal takes focus and closes on Escape', async ({ app }) => {
      await app.page.evaluate(() => (window as any).switchView('share'));
      await app.page.evaluate(() => (window as any).ShareView.openLogs());
      await expectLayerOpen(app.page, '#share-logs-modal', true);
      await expectFocusInside(app.page, '#share-logs-modal');

      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, '#share-logs-modal', false);
      await app.page.evaluate(() => (window as any).switchView('clips'));
    });

    test('follow share modal takes focus and closes on Escape', async ({ app }) => {
      await app.page.evaluate(() => (window as any).switchView('share'));
      await app.page.click('#add-follow-btn');
      await expectLayerOpen(app.page, '#follow-share-modal', true);
      await expectFocusInside(app.page, '#follow-share-modal');

      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, '#follow-share-modal', false);
      await expect(app.page.locator('#add-follow-btn')).toBeFocused();
      await app.page.evaluate(() => (window as any).switchView('clips'));
    });
  });

  test.describe('context menu hands focus to what it opened', () => {
    test('Merge into… leaves focus in the merge input', async ({ app }) => {
      await app.createTag('focus-src');
      await app.createTag('focus-dst');
      await app.openMergeModal('focus-src');
      await expect(app.page.locator('#merge-tag-dest-input')).toBeFocused();
    });

    test('Rename… then Cancel returns focus to the card menu button', async ({ app }) => {
      const name = await uploadImage(app);
      await app.openCardMenu(name);
      await app.page.click(selectors.cardMenu.rename);
      await expectLayerOpen(app.page, selectors.prompt.dialog, true);

      await app.page.click(selectors.prompt.cancelButton);
      await expectLayerOpen(app.page, selectors.prompt.dialog, false);
      const card = await app.getClipByFilename(name);
      await expect.poll(() => card.evaluate((el) => el.contains(document.activeElement))).toBe(true);
    });
  });

  // The page is worker-scoped: whatever one test leaves open, the next one
  // inherits unless the fixture's reset really closes it.
  test.describe('reset between tests on the shared page', () => {
    test.describe.configure({ mode: 'serial' });

    test('a test that leaves dialogs open and tooltips off', async ({ app }) => {
      const name = await uploadImage(app);
      await app.openCardMenu(name);
      await app.page.click(selectors.cardMenu.metadata);
      await expectLayerOpen(app.page, selectors.metadata.modal, true);
      await app.page.evaluate(() => {
        (window as any).showPromptDialog('Left open', '', () => {});
        (window as any).openQueueModal();
      });
      await expectLayerOpen(app.page, selectors.prompt.dialog, true);
      await app.page.evaluate(() => (window as any).toggleTooltips(false));
      await expect(app.page.locator('body')).toHaveClass(/tooltips-disabled/);
    });

    test('the next test starts with them closed and focus restoration intact', async ({ app }) => {
      for (const sel of [selectors.metadata.modal, selectors.prompt.dialog, '#queue-modal']) {
        await expect(app.page.locator(sel)).toHaveAttribute('inert', '');
      }
      await expect(app.page.locator('body')).not.toHaveClass(/tooltips-disabled/);
      expect(await app.page.evaluate(() => (window as any).go.main.App.GetSetting('tooltips_enabled'))).not.toBe('false');

      const name = await uploadImage(app);
      await app.openCardMenu(name);
      await app.page.click(selectors.cardMenu.metadata);
      await expectLayerOpen(app.page, selectors.metadata.modal, true);
      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, selectors.metadata.modal, false);
      const card = await app.getClipByFilename(name);
      await expect.poll(() => card.evaluate((el) => el.contains(document.activeElement))).toBe(true);

      // The reset's fallback sets `inert` either way, so prove the prompt and
      // queue were really closed: reopened, they must restore focus to the new
      // opener, not to the previous test's.
      const opened: Array<[string, string]> = [
        ['#queue-modal', 'openQueueModal()'],
        [selectors.prompt.dialog, "showPromptDialog('Name', '', () => {})"],
      ];
      for (const [selector, open] of opened) {
        await app.page.locator('#search-options-btn').focus();
        await app.page.evaluate((code) => { (0, eval)(code); }, open);
        await expectLayerOpen(app.page, selector, true);
        await expectFocusInside(app.page, selector);

        await app.page.keyboard.press('Escape');
        await expectLayerOpen(app.page, selector, false);
        await expect(app.page.locator('#search-options-btn')).toBeFocused();
      }
    });
  });

  test.describe('dialogs over the editor', () => {
    const dialogs: Array<[string, string, string]> = [
      ['prompt', '#prompt-dialog', "showPromptDialog('Name', 'x', () => {})"],
      ['conflict dialog', '#conflict-dialog', "showConflictDialog(['a.png'], () => {})"],
      ['path paste dialog', '#path-paste-dialog', "showPathPasteDialog([{ path: '/tmp/a.png', name: 'a.png', size: 1 }])"],
      ['plugin result modal', '#plugin-result-modal', "showPluginResultModal({ title: 'R', content: 'hello', format: 'text' })"],
      ['plugin options modal', '#plugin-options-modal', "openPluginOptionsDialog({ label: 'Opts', plugin_id: 0, id: 'x', options: [{ id: 'q', label: 'Q', type: 'text' }] }, [])"],
    ];
    for (const [label, selector, open] of dialogs) {
      test(`the ${label} renders above the image editor and Escape closes only it`, async ({ app }) => {
        const name = await uploadImage(app);
        await app.openImageEditor(name);
        await app.page.evaluate((code) => { (0, eval)(code); }, open);
        await expectLayerOpen(app.page, selector, true);
        await expect.poll(() => isTopmostAtCenter(app.page, selector)).toBe(true);
        await expectFocusInside(app.page, selector);

        await app.page.keyboard.press('Escape');
        await expectLayerOpen(app.page, selector, false);
        await expect(app.page.locator(selectors.editor.modal)).toHaveClass(/active/);
        await app.closeImageEditor();
      });
    }

    test('a dialog left open beneath the editor does not take its keys', async ({ app }) => {
      const name = await uploadImage(app);
      await app.openImageEditor(name);
      // Force a dialog under the editor, as an older stacking order did.
      try {
        await app.page.evaluate(() => {
          (window as any).showPromptDialog('Name', 'x', () => {});
          (document.getElementById('prompt-dialog') as HTMLElement).style.zIndex = '120';
        });
        await expectLayerOpen(app.page, selectors.prompt.dialog, true);
        // @ts-ignore - shortcuts.js top-level const
        const contexts = await app.page.evaluate(() => ShortcutManager.getActiveContexts());
        expect(contexts).toContain('editor');
      } finally {
        await app.page.evaluate(() => {
          (document.getElementById('prompt-dialog') as HTMLElement).style.zIndex = '';
          (window as any).closePromptDialog();
        });
      }
      await app.closeImageEditor();
    });
  });

  test.describe('user content is never taken for an app dialog', () => {
    test('markdown with aria-modal in a plugin result does not swallow shortcuts after it closes', async ({ app }) => {
      await app.page.evaluate(() => (window as any).showPluginResultModal({
        title: 'R', format: 'markdown', content: '<p aria-modal="true" role="dialog">trap</p>\n\ntext',
      }));
      await expectLayerOpen(app.page, '#plugin-result-modal', true);
      expect(await app.page.locator('#plugin-result-modal [aria-modal]').count()).toBe(0);
      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, '#plugin-result-modal', false);

      // Even if such an element reaches the page, it is not an app dialog.
      await app.page.evaluate(() => {
        const p = document.createElement('p');
        p.setAttribute('aria-modal', 'true');
        p.id = 'stray-aria-modal';
        document.getElementById('plugin-result-body')?.appendChild(p);
      });
      await app.page.locator('body').click();
      await app.page.keyboard.press('?');
      await expectLayerOpen(app.page, '[data-testid="shortcuts-cheatsheet"]', true);
      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, '[data-testid="shortcuts-cheatsheet"]', false);
      await app.page.evaluate(() => document.getElementById('stray-aria-modal')?.remove());
    });
  });

  test.describe('focus after the opener is gone', () => {
    test('removing a plugin from the Plugins modal keeps focus inside the modal', async ({ app, tempDir }) => {
      const pluginPath = path.join(tempDir, 'focus-remove.lua');
      await fs.writeFile(pluginPath, 'Plugin = { name = "Focus Remove", version = "1.0.0", events = {"app:startup"} }\nfunction on_startup() end\n');
      const result = await app.importPluginFromPath(pluginPath);
      expect(result).not.toBeNull();
      await app.openPluginsModal();
      await app.removePluginViaUI(result!.id);
      await app.expectPluginsEmptyState();
      await expectFocusInside(app.page, selectors.plugins.modal);
      await app.closePluginsModal();
    });
  });

  test.describe('expiration popover from the lightbox', () => {
    test('arrow keys move between presets instead of paging the lightbox', async ({ app }) => {
      const first = await uploadImage(app);
      const second = await createTempFile(generateTestImage(64, 64, '#224488'), 'png');
      await app.uploadFile(second);
      await app.expectClipCount(2);
      await app.openLightbox(first);
      const caption = app.page.locator(selectors.lightbox.caption);
      const before = await caption.textContent();

      await app.page.click(selectors.lightbox.fileTrigger);
      await app.page.click(selectors.cardMenu.setExpiration);
      const presets = app.page.locator(`${selectors.expiration.popover} [role="menuitem"]`);
      await expect(presets.first()).toBeFocused();

      await app.page.keyboard.press('ArrowRight');
      await expect(presets.nth(1)).toBeFocused();
      await app.page.keyboard.press('ArrowLeft');
      await expect(presets.first()).toBeFocused();
      expect(await caption.textContent()).toBe(before);

      await app.page.keyboard.press('Escape');
      await expect(app.page.locator(selectors.expiration.popover)).toHaveCount(0);
      await app.closeLightbox();
    });
  });

  test.describe('dialogs that open beneath a higher layer', () => {
    test('a plugin result arriving under an open prompt leaves focus in the prompt', async ({ app }) => {
      await app.page.locator('#search-options-btn').focus();
      await app.page.evaluate(() => (window as any).showPromptDialog('Name', 'x', () => {}));
      await expect(app.page.locator(selectors.prompt.input)).toBeFocused();

      await app.page.evaluate(() => (window as any).showPluginResultModal({ title: 'R', content: 'late', format: 'text' }));
      await expectLayerOpen(app.page, '#plugin-result-modal', true);
      await expect(app.page.locator(selectors.prompt.input)).toBeFocused();

      // Closing the prompt hands focus to the result still open, not to the page behind it.
      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, selectors.prompt.dialog, false);
      await expectFocusInside(app.page, '#plugin-result-modal');

      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, '#plugin-result-modal', false);
      await expect.poll(() => app.page.evaluate(() => document.activeElement !== document.body)).toBe(true);
    });

    test('a conflict dialog arriving under an open confirm does not take its focus', async ({ app }) => {
      await app.page.evaluate(() => (window as any).showConfirmDialog('Sure?', 'msg', () => {}));
      await expect(app.page.locator(selectors.confirm.cancelButton)).toBeFocused();
      await app.page.evaluate(() => (window as any).showConflictDialog(['a.png'], () => {}));
      await expectLayerOpen(app.page, '#conflict-dialog', true);
      // The conflict dialog focuses its button after a short delay; outlast it.
      await app.page.waitForTimeout(250);
      await expect(app.page.locator(selectors.confirm.cancelButton)).toBeFocused();

      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, selectors.confirm.dialog, false);
      await expectFocusInside(app.page, '#conflict-dialog');
      await app.page.keyboard.press('Escape');
      await expectLayerOpen(app.page, '#conflict-dialog', false);
    });
  });

  test.describe('tag popover from the lightbox', () => {
    test('arrow keys inside the popover do not page the lightbox', async ({ app }) => {
      const first = await uploadImage(app);
      const second = await createTempFile(generateTestImage(64, 64, '#224488'), 'png');
      await app.uploadFile(second);
      await app.expectClipCount(2);
      await app.openLightbox(first);
      const caption = app.page.locator(selectors.lightbox.caption);
      const before = await caption.textContent();

      await app.page.click(selectors.lightbox.fileTrigger);
      await app.page.click(selectors.cardMenu.tags);
      await expect(app.page.locator('#tag-popover')).toBeVisible();
      await app.page.locator('#create-tag-btn').focus();

      await app.page.keyboard.press('ArrowRight');
      await app.page.keyboard.press('ArrowLeft');
      expect(await caption.textContent()).toBe(before);
      await expect(app.page.locator('#tag-popover')).toBeVisible();

      await app.page.keyboard.press('Escape');
      await expect(app.page.locator('#tag-popover')).toBeHidden();
      await app.closeLightbox();
    });
  });

  test.describe('Space in dialogs over the image editor', () => {
    test('Space activates a focused button in a prompt over the editor', async ({ app }) => {
      const name = await uploadImage(app);
      await app.openImageEditor(name);
      await app.page.evaluate(() => (window as any).showPromptDialog('Name', 'x', () => {}));
      await expectLayerOpen(app.page, selectors.prompt.dialog, true);
      await app.page.locator(selectors.prompt.cancelButton).focus();

      await app.page.keyboard.press('Space');
      await expectLayerOpen(app.page, selectors.prompt.dialog, false);
      await expect(app.page.locator(selectors.editor.modal)).toHaveClass(/active/);
      await app.closeImageEditor();
    });
  });

  test.describe('accessible names', () => {
    test('icon-only buttons have accessible names', async ({ app }) => {
      for (const id of ['metadata-close', 'settings-close', 'maintenance-close', 'plugins-close',
        'queue-modal-close', 'api-modal-close', 'api-key-copy-btn', 'zoom-out', 'zoom-in']) {
        await expect(app.page.locator(`#${id}`), id).toHaveAttribute('aria-label', /\S/);
      }
    });

    test('tooltips show on keyboard focus', async ({ app }) => {
      await app.page.locator('body').click();
      await app.tabTo('[data-tooltip]', { maxTabs: 30 });
      // Opacity alone reads 1 with tooltips switched off (display: none hides
      // them), so check both.
      await expect.poll(() => app.page.evaluate(() => {
        const after = getComputedStyle(document.activeElement as Element, '::after');
        return `${after.display}/${after.opacity}`;
      })).not.toMatch(/^none\//);
      await expect.poll(() => app.page.evaluate(() =>
        getComputedStyle(document.activeElement as Element, '::after').opacity)).toBe('1');
    });

    test('watch folder Pause and Remove buttons have accessible names', async ({ app, tempDir }) => {
      await app.openWatchView();
      await app.addWatchFolder(tempDir);
      await expect(app.page.locator('[data-action="toggle-pause"]').first()).toHaveAttribute('aria-label', /\S/);
      await expect(app.page.locator('#watch-view [data-action="remove"]').first()).toHaveAttribute('aria-label', /\S/);
    });
  });
});
