import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import { generateTestImage, createTempFile } from '../../helpers/test-data';
import * as path from 'path';

// Keyboard users keep their place: focus survives re-renders, a disabled
// control is not left looking live, and keys stay with the popover they
// were pressed in.

test.describe('Focus and key ownership polish', () => {
  test('restore Cancel is disabled while a restore is running', async ({ app }) => {
    await app.page.evaluate(() => {
      const w = window as any;
      const api = w.go.main.App;
      const original = api.ConfirmRestoreBackup;
      w.__polishRestoreStub = () => { api.ConfirmRestoreBackup = original; };
      api.ConfirmRestoreBackup = () => new Promise((_, reject) => { w.__polishFailRestore = () => reject('stopped by test'); });
    });
    try {
      await app.openSettingsModal();
      await app.page.evaluate(() => {
        // @ts-ignore - settings.js top-level let
        pendingRestorePath = '/tmp/not-a-real-backup.zip';
        (window as any).showRestoreConfirmDialog();
      });
      await app.page.locator('#restore-confirm-yes').click();
      await expect(app.page.locator('#restore-confirm-yes')).toHaveText('Restoring...');
      await expect(app.page.locator('#restore-confirm-cancel')).toBeDisabled();

      await app.page.evaluate(() => (window as any).__polishFailRestore());
      await expect(app.page.locator('#restore-confirm-cancel')).toBeEnabled();
    } finally {
      await app.page.evaluate(() => {
        const w = window as any;
        w.__polishFailRestore?.();
        w.__polishRestoreStub?.();
        delete w.__polishFailRestore;
        delete w.__polishRestoreStub;
      });
    }
  });

  test('closing a layer after a gallery reload returns focus to the same clip card', async ({ app }) => {
    const file = await createTempFile(generateTestImage(32, 32, '#884422'), 'png');
    await app.uploadFile(file);
    const name = path.basename(file);
    await app.expectClipCount(1);

    const card = await app.getClipByFilename(name);
    const id = await card.getAttribute('data-id');
    await card.focus();
    await app.page.evaluate(() => (window as any).showPluginResultModal({ title: 'R', content: 'x', format: 'text' }));
    await expect.poll(() => app.page.evaluate(() => !!document.activeElement?.closest('#plugin-result-modal'))).toBe(true);

    // A reload (a clip:created from a plugin, say) replaces every card.
    await app.page.evaluate(async () => {
      // @ts-ignore - wails-api.js global
      await loadClips();
    });
    await app.page.keyboard.press('Escape');

    await expect.poll(() => app.page.evaluate(() =>
      (document.activeElement as HTMLElement | null)?.closest('#gallery > li')?.getAttribute('data-id') ?? null)).toBe(id);
  });

  test('a tag created elsewhere keeps focus on the tag filter row', async ({ app }) => {
    await app.createTag('alpha');
    await app.createTag('beta');
    await app.openTagFilterDropdown();
    const beta = app.page.locator('[data-testid="tag-checkbox-beta"]');
    await beta.focus();

    // Created through the backend directly, as a plugin or REST client would.
    await app.page.evaluate(async () => { await (window as any).go.main.App.CreateTag('gamma'); });
    await expect(app.page.locator('[data-testid="tag-checkbox-gamma"]')).toHaveCount(1);
    await expect(beta).toBeFocused();
  });

  test('an older tag load never overwrites a newer one', async ({ app }) => {
    await app.createTag('fresh');
    const names = await app.page.evaluate(async () => {
      const w = window as any;
      const api = w.go.main.App;
      const original = api.GetTags;
      let calls = 0;
      api.GetTags = async () => {
        const n = ++calls;
        const tags = await original();
        // The first request answers last, with a stale (empty) list.
        if (n === 1) {
          await new Promise(r => setTimeout(r, 300));
          return [];
        }
        return tags;
      };
      try {
        // @ts-ignore - app.js globals
        await Promise.all([loadTags(), loadTags()]);
        // @ts-ignore
        return allTags.map((t: any) => t.name);
      } finally {
        api.GetTags = original;
      }
    });
    expect(names).toContain('fresh');
  });

  test.describe('serve view', () => {
    test.afterEach(async ({ app }) => {
      await app.stopAllServers();
      await app.page.evaluate(() => { (window as any).closeServeTagPicker?.({ restoreFocus: false }); });
    });

    test('Start and Stop keep focus on the toggle button', async ({ app }) => {
      await app.createTag('served');
      await app.switchToServeView();
      await app.page.click(selectors.serve.addBtn);
      await app.page.locator(`${selectors.serve.tagPicker} .serve-tag-option`).first().click();
      const toggle = app.page.locator(selectors.serve.toggleBtn).first();
      await expect(toggle).toBeVisible();

      await toggle.focus();
      await app.page.keyboard.press('Enter');
      await expect(app.page.locator(selectors.serve.toggleBtn).first()).toHaveAttribute('data-running', 'true');
      await expect(app.page.locator(selectors.serve.toggleBtn).first()).toBeFocused();

      await app.page.keyboard.press('Enter');
      await expect(app.page.locator(selectors.serve.toggleBtn).first()).toHaveAttribute('data-running', 'false');
      await expect(app.page.locator(selectors.serve.toggleBtn).first()).toBeFocused();
    });

    test('Escape closes the tag picker even with focus outside it', async ({ app }) => {
      await app.createTag('pickme');
      await app.switchToServeView();
      await app.page.click(selectors.serve.addBtn);
      await expect(app.page.locator(selectors.serve.tagPicker)).toBeVisible();

      await app.page.locator(selectors.serve.backBtn).focus();
      await app.page.keyboard.press('Escape');
      await expect(app.page.locator(selectors.serve.tagPicker)).toHaveCount(0);
      await expect(app.page.locator(selectors.serve.addBtn)).toBeFocused();
    });
  });

  test('a global key equal to an import wizard key is reported as a conflict', async ({ app }) => {
    const conflict = await app.page.evaluate(() => {
      // @ts-ignore - shortcuts.js top-level const
      const sm = ShortcutManager;
      const all = Array.from(sm.actions.values()) as any[];
      const wizard = all.find((a: any) => a.context === 'import-wizard');
      const global = all.find((a: any) => a.context === 'global' && sm.getEffectiveCombo(a.id));
      if (!wizard || !global) return 'missing actions';
      return sm.findConflict(wizard.id, sm.getEffectiveCombo(global.id))?.id ?? null;
    });
    expect(conflict).not.toBeNull();
    expect(conflict).not.toBe('missing actions');
  });
});
