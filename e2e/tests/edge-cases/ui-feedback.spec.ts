import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

/**
 * Feedback and double-submit guards: Wails rejects with plain strings (so
 * `error.message` printed "undefined"), and several buttons acted twice on a
 * double click.
 */
test.describe('UI feedback and double-submit guards', () => {
  test.afterEach(async ({ app }) => {
    await app.page.evaluate(() => {
      const w = window as any;
      // Settle anything a test left pending, then put the real bindings back
      // exactly once: a stale restore run by a later test would undo its stub.
      for (const release of ['__releaseAdd', '__releaseImport', '__releaseTags', '__releaseDelete']) {
        w[release]?.();
        delete w[release];
      }
      w.__restoreStub?.();
      delete w.__restoreStub;
    });
  });

  test('the confirm dialog shows it is working and cannot be confirmed twice or closed mid-action', async ({ app }) => {
    await app.page.evaluate(async () => {
      const w = window as any;
      const api = w.go.main.App;
      await api.UploadFiles([{ name: 'held.txt', content_type: 'text/plain', data: btoa('held') }], 0, 0);
      await w.loadClips();
      // Hold BulkDelete: every intercepted call is queued and released
      // together, and calls after the release pass straight through.
      const original = api.BulkDelete;
      const waiting: Array<() => void> = [];
      let released = false;
      w.__bulkDeleteCalls = 0;
      api.BulkDelete = (...args: unknown[]) => {
        w.__bulkDeleteCalls++;
        if (released) return original(...args);
        return new Promise((resolve, reject) => {
          waiting.push(() => original(...args).then(resolve, reject));
        });
      };
      w.__releaseDelete = () => { released = true; waiting.splice(0).forEach(run => run()); };
      w.__restoreStub = () => { api.BulkDelete = original; };
    });
    await app.expectClipCount(1);
    await app.selectClips(['held.txt']);
    await app.bulkDelete();

    const dialog = app.page.locator(selectors.confirm.dialog);
    const yes = app.page.locator(selectors.confirm.confirmButton);
    await expect(dialog).toHaveClass(/opacity-100/);
    await yes.click();

    await expect(yes).toHaveAttribute('aria-busy', 'true');
    await expect(yes).toHaveText('Working…');
    // A second confirm, Cancel and Escape all do nothing while it runs.
    await yes.click({ force: true });
    await app.page.locator(selectors.confirm.cancelButton).click({ force: true });
    await app.page.keyboard.press('Escape');
    await expect(dialog).toHaveClass(/opacity-100/);
    expect(await app.page.evaluate(() => (window as any).__bulkDeleteCalls)).toBe(1);

    await app.page.evaluate(() => (window as any).__releaseDelete());
    await expect(dialog).toHaveClass(/opacity-0/);
    await app.expectClipCount(0);
    expect(await app.page.evaluate(() => (window as any).__bulkDeleteCalls)).toBe(1);

    // The next dialog starts clean.
    await app.page.evaluate(() => (window as any).showConfirmDialog('Again', 'Sure?', () => {}));
    await expect(yes).not.toHaveAttribute('aria-busy', 'true');
    await expect(yes).not.toHaveText('Working…');
    await app.page.locator(selectors.confirm.cancelButton).click();
  });

  test('the delete dialog names the clip, escaped, and a failed rename says why', async ({ app }) => {
    await app.page.evaluate(async () => {
      const w = window as any;
      await w.go.main.App.UploadFiles([{ name: '<b>bold</b>.txt', content_type: 'text/plain', data: btoa('x') }], 0, 0);
      await w.loadClips();
      const card = document.querySelector('#gallery > li[data-id]') as HTMLElement;
      w.deleteClip(Number(card.dataset.id));
    });
    const message = app.page.locator(selectors.confirm.message);
    await expect(message).toHaveText('Delete <b>bold</b>.txt permanently?');
    await expect(message.locator('b')).toHaveCount(0);
    await app.page.locator(selectors.confirm.cancelButton).click();

    // The backend's validation message reaches the toast.
    await app.page.evaluate(async () => {
      const w = window as any;
      const card = document.querySelector('#gallery > li[data-id]') as HTMLElement;
      try {
        await w.go.main.App.RenameClip(Number(card.dataset.id), 'a/b.txt');
      } catch (error) {
        w.toastError('Failed to rename clip', error);
      }
    });
    await app.expectToast('Failed to rename clip: filename contains path separator');
  });

  test('bulk messages count one clip as one clip', async ({ app }) => {
    await app.page.evaluate(async () => {
      const w = window as any;
      await w.go.main.App.UploadFiles([{ name: 'single.txt', content_type: 'text/plain', data: btoa('1') }], 0, 0);
      await w.loadClips();
    });
    await app.selectClips(['single.txt']);
    await app.bulkDelete();
    await expect(app.page.locator(selectors.confirm.message)).toHaveText('Delete 1 clip permanently?');
    await app.page.locator(selectors.confirm.confirmButton).click();
    await app.expectToast('Deleted 1 clip.');
  });

  test('the remove-folder dialog shows the path and says clips are kept', async ({ app }) => {
    await app.page.evaluate(() => (window as any).removeWatchFolder(999999, '/tmp/<i>inbox</i>'));
    const message = app.page.locator(selectors.confirm.message);
    await expect(message).toContainText('Stop watching /tmp/<i>inbox</i>?');
    await expect(message).toContainText('Clips already imported stay in your library');
    await expect(message.locator('i')).toHaveCount(0);
    await app.page.locator(selectors.confirm.cancelButton).click();
  });

  test('a failure rejected as a plain string reads as an error toast with its message', async ({ app }) => {
    await app.page.evaluate(() => {
      const api = (window as any).go.main.App;
      const original = api.ShowCreateBackupDialog;
      (window as any).__restoreStub = () => { api.ShowCreateBackupDialog = original; };
      api.ShowCreateBackupDialog = () => Promise.reject('disk full');
    });
    await app.openSettingsModal();
    await app.page.locator(selectors.backup.createButton).click();
    const toast = app.page.locator(selectors.toast.message);
    await expect(toast).toHaveText('Failed to create backup: disk full');
    await expect(toast).toHaveClass(/bg-red-600/);
    await app.closeSettingsModal();
  });

  test('a created backup is named, and the toast offers to show it', async ({ app }) => {
    await app.page.evaluate(() => {
      const w = window as any;
      const api = w.go.main.App;
      const originalCreate = api.ShowCreateBackupDialog;
      const originalReveal = api.RevealInFileManager;
      w.__revealed = [];
      api.ShowCreateBackupDialog = async () => '/Users/me/Backups/mahpastes-2026.zip';
      api.RevealInFileManager = async (p: string) => { w.__revealed.push(p); };
      w.__restoreStub = () => { api.ShowCreateBackupDialog = originalCreate; api.RevealInFileManager = originalReveal; };
    });
    await app.openSettingsModal();
    await app.page.locator(selectors.backup.createButton).click();
    await app.expectToast('Backup created: mahpastes-2026.zip');
    const action = app.page.locator(selectors.toast.action);
    await expect(action).toHaveText('Show in folder');
    await action.click();
    expect(await app.page.evaluate(() => (window as any).__revealed)).toEqual(['/Users/me/Backups/mahpastes-2026.zip']);
    await app.closeSettingsModal();
  });

  test('API keys show when they were last used in local terms, and Enter creates a key', async ({ app }) => {
    const parsed = await app.page.evaluate(() => {
      const w = window as any;
      const fiveMinutesAgo = new Date(Date.now() - 5 * 60000).toISOString().replace('T', ' ').slice(0, 19);
      return {
        sqlite: w.apiTimeAgo(w.parseAPITimestamp(fiveMinutesAgo)),
        rfc: w.apiTimeAgo(w.parseAPITimestamp(new Date(Date.now() - 3 * 3600000).toISOString())),
        junk: w.parseAPITimestamp('not a time'),
      };
    });
    expect(parsed).toEqual({ sqlite: '5 min ago', rfc: '3 h ago', junk: null });

    const name = `enter-${Date.now()}`;
    await app.openDrawer();
    await app.page.locator('#open-api-btn').click();
    await app.page.waitForSelector('[data-testid="api-modal"].opacity-100');
    await app.page.locator('#api-show-create-btn').click();
    await app.page.locator('#api-key-name').fill(name);
    await app.page.locator('#api-key-name').press('Enter');
    await app.page.waitForSelector('#api-key-reveal:not(.hidden)');
    // The reveal says how the key is used: nothing else in the app names mp.
    const snippet = app.page.locator('[data-testid="api-key-usage-snippet"]');
    await expect(snippet).toContainText('export MP_API_KEY=mp_');
    await expect(snippet).toContainText(/export MP_API_URL=http:\/\/(localhost|127\.0\.0\.1):\d+/);
    await expect(snippet).toContainText('mp clip list');
    const running = await app.page.evaluate(async () => (await (window as any).go.main.APIService.GetAPIStatus()).running);
    await expect(app.page.locator('[data-testid="api-key-usage-stopped"]'))[running ? 'toBeHidden' : 'toBeVisible']();
    await app.page.locator('#api-key-reveal-close').click();
    const card = app.page.locator('[data-testid^="api-key-card-"]', { hasText: name });
    await expect(card).toHaveCount(1);
    await expect(card).toContainText('Never used');
    await app.page.locator('#api-modal-close').click();
  });

  test('double-clicking Create Key makes one key', async ({ app }) => {
    const name = `once-${Date.now()}`;
    await app.openDrawer();
    await app.page.locator('#open-api-btn').click();
    await app.page.waitForSelector('[data-testid="api-modal"].opacity-100');
    await app.page.locator('#api-show-create-btn').click();
    await app.page.locator('#api-key-name').fill(name);
    await app.page.evaluate(() => {
      const btn = document.getElementById('api-create-key-btn') as HTMLButtonElement;
      btn.click();
      btn.click();
    });
    await app.page.waitForSelector('#api-key-reveal:not(.hidden)');
    await app.page.locator('#api-key-reveal-close').click();
    await expect(app.page.locator('[data-testid^="api-key-card-"]', { hasText: name })).toHaveCount(1);
    const count = await app.page.evaluate(async (n) => {
      const keys = await (window as any).go.main.APIService.ListAPIKeys();
      return keys.filter((k: any) => k.name === n).length;
    }, name);
    expect(count).toBe(1);
    await app.page.locator('#api-modal-close').click();
  });

  test('adding a watch folder shows progress, adds once and closes when added', async ({ app }) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'mp-watch-add-'));
    await app.openWatchView();
    await app.page.evaluate(() => {
      const api = (window as any).go.main.App;
      const original = api.AddWatchedFolder;
      (window as any).__addCalls = 0;
      (window as any).__restoreStub = () => { api.AddWatchedFolder = original; };
      api.AddWatchedFolder = (cfg: any) => {
        (window as any).__addCalls++;
        return new Promise(resolve => { (window as any).__releaseAdd = () => resolve(original(cfg)); });
      };
    });
    try {
      await app.page.evaluate((p) => (window as any).openFolderModal(p), dir);
      await app.page.locator(selectors.watchEdit.filterAll).check();
      await app.page.locator(selectors.watchEdit.processExisting).check();
      const save = app.page.locator(selectors.watchEdit.saveButton);
      await expect(save).toBeEnabled();
      await app.page.evaluate(() => {
        const btn = document.getElementById('folder-modal-save') as HTMLButtonElement;
        btn.click();
        btn.click();
      });
      await expect(save).toBeDisabled();
      await expect(save).toHaveText('Adding…');
      await app.page.evaluate(() => (window as any).__releaseAdd());
      await expect(app.page.locator(selectors.watchEdit.modal)).toHaveAttribute('inert', '');
      expect(await app.page.evaluate(() => (window as any).__addCalls)).toBe(1);
    } finally {
      await app.page.evaluate(() => {
        const w = window as any;
        w.__releaseAdd?.();
        delete w.__releaseAdd;
        delete w.__addCalls;
      });
      await app.closeWatchView();
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });

  test('a second folder can be added while the first is still importing its files', async ({ app }) => {
    const first = fs.mkdtempSync(path.join(os.tmpdir(), 'mp-watch-a-'));
    const second = fs.mkdtempSync(path.join(os.tmpdir(), 'mp-watch-b-'));
    await app.openWatchView();
    await app.page.evaluate(() => {
      const api = (window as any).go.main.App;
      const original = api.ProcessExistingFilesInFolder;
      (window as any).__restoreStub = () => { api.ProcessExistingFilesInFolder = original; };
      api.ProcessExistingFilesInFolder = (id: number) =>
        new Promise(resolve => { (window as any).__releaseImport = () => resolve(original(id)); });
    });
    try {
      await app.page.evaluate((p) => (window as any).openFolderModal(p), first);
      await app.page.locator(selectors.watchEdit.filterAll).check();
      await app.page.locator(selectors.watchEdit.processExisting).check();
      await app.page.locator(selectors.watchEdit.saveButton).click();
      await expect(app.page.locator(selectors.watchEdit.modal)).toHaveAttribute('inert', '');

      // The first import is still running.
      await app.page.evaluate((p) => (window as any).openFolderModal(p), second);
      await app.page.locator(selectors.watchEdit.filterAll).check();
      const save = app.page.locator(selectors.watchEdit.saveButton);
      await expect(save).toBeEnabled();
      await expect(save).toHaveText('Add Folder');
      await save.click();
      await expect(app.page.locator(selectors.watchEdit.modal)).toHaveAttribute('inert', '');
      await expect.poll(() => app.page.evaluate(async () =>
        (await (window as any).go.main.App.GetWatchedFolders()).length)).toBe(2);
    } finally {
      await app.page.evaluate(() => (window as any).__releaseImport?.());
      await app.closeWatchView();
      fs.rmSync(first, { recursive: true, force: true });
      fs.rmSync(second, { recursive: true, force: true });
    }
  });

  test('a refresh failure after the folder was added is not reported as a failed save', async ({ app }) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'mp-watch-refresh-'));
    await app.openWatchView();
    await app.page.evaluate(() => {
      const api = (window as any).go.main.App;
      const original = api.RefreshWatches;
      (window as any).__restoreStub = () => { api.RefreshWatches = original; };
      api.RefreshWatches = () => Promise.reject('watcher busy');
    });
    try {
      await app.page.evaluate((p) => (window as any).openFolderModal(p), dir);
      await app.page.locator(selectors.watchEdit.filterAll).check();
      await app.page.locator(selectors.watchEdit.saveButton).click();
      await expect(app.page.locator(selectors.watchEdit.modal)).toHaveAttribute('inert', '');
      const toast = app.page.locator(selectors.toast.message);
      await expect(toast).toContainText('Folder added');
      await expect(toast).toContainText('watcher busy');
      await expect(toast).not.toContainText('Failed to save folder');
    } finally {
      await app.closeWatchView();
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });

  test('the serve list is not rebuilt on every poll and double Start starts once', async ({ app }) => {
    await app.createTag('served-once');
    await app.switchToServeView();
    const tagID = await app.page.evaluate(async () => {
      const tags = await (window as any).go.main.App.GetTags();
      const tag = tags.find((t: any) => t.name === 'served-once');
      await (window as any).openServeViewForTag(tag.id);
      return tag.id;
    });
    const row = app.page.locator(`#serve-list > li[data-tag-id="${tagID}"]`);
    await expect(row).toBeVisible();
    // Highlighted by openServeViewForTag (it used to look for an attribute no row had).
    await expect(row).toHaveClass(/ring-2/);

    try {
      // Count completed polls: a poll re-renders in the continuation of its
      // GetServeStatus, which runs before this counter can be read.
      await app.page.evaluate(() => {
        const w = window as any;
        const svc = w.go.main.ServeService;
        const original = svc.GetServeStatus;
        w.__statusPolls = 0;
        w.__restoreStatusStub = () => { svc.GetServeStatus = original; };
        svc.GetServeStatus = (...args: any[]) =>
          original(...args).then((r: any) => { w.__statusPolls++; return r; });
      });
      try {
        // Mark and zero the count together, so only polls that end after the mark count.
        await row.evaluate((el: any) => { el.__marker = true; (window as any).__statusPolls = 0; });
        await expect.poll(() => app.page.evaluate(() => (window as any).__statusPolls), { timeout: 5000 })
          .toBeGreaterThanOrEqual(1);
        expect(await row.evaluate((el: any) => el.__marker === true)).toBe(true);
      } finally {
        await app.page.evaluate(() => {
          const w = window as any;
          w.__restoreStatusStub?.();
          delete w.__restoreStatusStub;
          delete w.__statusPolls;
        });
      }

      await app.page.evaluate(() => {
        const svc = (window as any).go.main.ServeService;
        const original = svc.StartServing;
        (window as any).__startCalls = 0;
        (window as any).__restoreStub = () => { svc.StartServing = original; };
        svc.StartServing = (...args: any[]) => { (window as any).__startCalls++; return original(...args); };
      });
      await row.locator('.serve-toggle-btn').evaluate((btn: HTMLButtonElement) => { btn.click(); btn.click(); });
      await expect(app.page.locator(`#serve-list > li[data-tag-id="${tagID}"] .serve-toggle-btn`)).toHaveText('Stop');
      expect(await app.page.evaluate(() => (window as any).__startCalls)).toBe(1);
    } finally {
      await app.stopAllServers();
    }
  });

  test('navigating to a folder newer than the cached tag list still opens it', async ({ app }) => {
    const { tagID } = await app.createTag('fresh-folder');
    await app.toggleFolderMode();
    await app.page.evaluate((id) => {
      // Simulate a tag the cache has not caught up with yet.
      // @ts-ignore - app global (top-level let)
      const list = allTags as any[];
      const i = list.findIndex(t => t.id === id);
      if (i >= 0) list.splice(i, 1);
      (window as any).navigateToFolder(id);
    }, tagID);
    await app.expectFolderHeader('fresh-folder');
  });

  test('a folder navigation superseded while the tag list refreshes does not win', async ({ app }) => {
    const { tagID: slow } = await app.createTag('slow-folder');
    const { tagID: fast } = await app.createTag('fast-folder');
    await app.toggleFolderMode();
    await app.page.evaluate(({ slow, fast }) => {
      const w = window as any;
      // @ts-ignore - app global (top-level let)
      const list = allTags as any[];
      const i = list.findIndex(t => t.id === slow);
      if (i >= 0) list.splice(i, 1);
      const api = w.go.main.App;
      const original = api.GetTags;
      w.__restoreStub = () => { api.GetTags = original; };
      // Hold every GetTags call until released: a pending tag:created
      // refresh can call it too, so a single resolver slot would be
      // overwritten and the slow navigation's call left hanging forever.
      const held: Array<() => void> = [];
      let released = false;
      api.GetTags = () => released
        ? original()
        : new Promise(resolve => { held.push(() => resolve(original())); });
      w.__releaseTags = () => { released = true; held.splice(0).forEach(r => r()); };
      w.__slowNav = w.navigateToFolder(slow);
      w.navigateToFolder(fast);
    }, { slow, fast });
    await app.expectFolderHeader('fast-folder');

    await app.page.evaluate(async () => {
      const w = window as any;
      w.__releaseTags();
      delete w.__releaseTags;
      await w.__slowNav;
      delete w.__slowNav;
    });
    // @ts-ignore - app global (top-level let)
    expect(await app.page.evaluate(() => activeTagFilters[activeTagFilters.length - 1])).toBe(fast);
    await app.expectFolderHeader('fast-folder');
  });
});
