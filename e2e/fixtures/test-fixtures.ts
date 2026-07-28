import { test as base, Page, Locator, expect, BrowserContext } from '@playwright/test';
import { selectors } from '../helpers/selectors.js';
import { getBaseURL, spawnSecondaryInstance, restartWailsInstance } from '../helpers/wails-manager.js';
import { createTempDir, cleanup, Point } from '../helpers/test-data.js';
import * as fs from 'fs/promises';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// State file path
const STATE_FILE = path.resolve(__dirname, '../.test-state.json');

interface TestState {
  instances: Array<{
    workerIndex: number;
    port: number;
    dataDir: string;
    baseURL: string;
  }>;
}

// Worker-scoped fixtures shared across all tests in the same worker
type WorkerFixtures = {
  workerContext: BrowserContext;
  workerPage: Page;
};

// Helper class for interacting with the mahpastes app
export class AppHelper {
  constructor(
    public page: Page,
    public baseURL: string,
    public dataDir: string = ''
  ) {}

  // ==================== Navigation ====================

  async goto(): Promise<void> {
    await this.page.goto(this.baseURL);
  }

  async waitForReady(): Promise<void> {
    // Wait for the app to be fully loaded
    await this.page.waitForSelector(selectors.header.root);
    // Only wait for the gallery to be ATTACHED to the DOM, not visible — after
    // share-view-context restarts the page may boot back into the share view,
    // which hides #gallery. The rest of the readiness waits (below) still
    // ensure the JS runtime is fully initialized.
    await this.page.waitForSelector(selectors.gallery.container, { state: 'attached' });
    // Wait for Wails runtime to be available (indicates JS is fully initialized).
    // After an app restart the wails dev proxy + vite + page-load chain can take
    // up to 30s on a contested machine before window.go.main.App is populated.
    await this.page.waitForFunction(() => {
      // @ts-ignore - Wails runtime
      return typeof window.go?.main?.App?.GetClips === 'function';
    }, { timeout: 30000 });
    // Wait for app to be fully initialized (loadTags and loadClips complete).
    // Post-restart initialization is slower because plugins reload, watchers
    // re-seed, and any network-facing systems (ShareManager DHT bootstrap,
    // tag serve, watch folder restart) each add setup latency.
    await this.page.waitForFunction(() => {
      // @ts-ignore
      return window.__appReady === true;
    }, { timeout: 60000 });
    // Workaround: Wails dev mode has a timing issue where the first API calls
    // during page load may return empty. Re-fetch tags and clips to ensure state is fresh.
    await this.page.evaluate(async () => {
      // @ts-ignore
      const tags = await window.go.main.App.GetTags();
      // @ts-ignore
      if (window.__testHelpers) {
        // @ts-ignore
        window.__testHelpers.setAllTags(tags);
      }
      // Re-render tag filter dropdown with fresh data
      // @ts-ignore
      if (typeof renderTagFilterDropdown === 'function') {
        // @ts-ignore
        renderTagFilterDropdown();
      }
    });
  }

  // ==================== Drawer ====================

  async openDrawer(): Promise<void> {
    const panel = this.page.locator(selectors.drawer.panel);
    const isOpen = await panel.evaluate((el) => !el.classList.contains('translate-x-full'));
    if (!isOpen) {
      await this.page.locator(selectors.header.drawerToggle).click();
      await this.page.waitForFunction(
        (sel) => !document.querySelector(sel)?.classList.contains('translate-x-full'),
        selectors.drawer.panel,
        { timeout: 5000 }
      );
    }
  }

  async closeDrawer(): Promise<void> {
    const panel = this.page.locator(selectors.drawer.panel);
    const isOpen = await panel.evaluate((el) => !el.classList.contains('translate-x-full'));
    if (isOpen) {
      await this.page.locator(selectors.drawer.closeButton).click();
    }
  }

  // ==================== Clip Operations ====================

  async uploadFile(filePath: string): Promise<void> {
    // Wait in two phases so sequential uploads are reliable AND the DOM is
    // ready for immediate assertions.
    //  1) Backend count must increase (filter-independent, proves the upload
    //     actually completed — the old "first clip visible" check returned
    //     immediately from upload #2+ because the first row was already there).
    //  2) The specific clip's DOM row should appear. If an active filter hides
    //     it, the wait times out harmlessly — callers that care about post-
    //     upload DOM state are also uploading unfiltered clips.
    const before = await this.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const clips = await window.go.main.App.GetClips(false, [], [], '', '');
      return Array.isArray(clips) ? clips.length : 0;
    });
    const fileInput = this.page.locator(selectors.upload.fileInput);
    await fileInput.setInputFiles(filePath);
    await this.page.waitForFunction(
      async (expected) => {
        // @ts-ignore - Wails runtime
        const clips = await window.go.main.App.GetClips(false, [], [], '', '');
        return Array.isArray(clips) && clips.length >= expected;
      },
      before + 1,
      { timeout: 10000 }
    );
    const filename = path.basename(filePath).toLowerCase();
    await this.page.locator(selectors.gallery.clipCardByName(filename))
      .waitFor({ state: 'attached', timeout: 2000 })
      .catch(() => { /* clip is filtered out of current view; backend ack above is sufficient */ });
  }

  async uploadFiles(filePaths: string[]): Promise<void> {
    const fileInput = this.page.locator(selectors.upload.fileInput);
    await fileInput.setInputFiles(filePaths);

    // Wait for clip to appear in gallery
    await this.page.locator('#gallery > li').first().waitFor({ state: 'visible', timeout: 10000 });
  }

  async pasteText(text: string): Promise<void> {
    // Focus the body and paste
    await this.page.locator('body').focus();
    await this.page.evaluate((t) => {
      const event = new ClipboardEvent('paste', {
        clipboardData: new DataTransfer(),
      });
      (event.clipboardData as DataTransfer).setData('text/plain', t);
      document.dispatchEvent(event);
    }, text);

    // Wait for clip to appear in gallery
    await this.page.locator('#gallery > li').first().waitFor({ state: 'visible', timeout: 10000 });
  }

  async getClipCount(): Promise<number> {
    const clips = this.page.locator(selectors.gallery.clipCard);
    return clips.count();
  }

  async refreshClips(): Promise<void> {
    // Refresh the gallery from the database without a full page reload.
    // Uses loadClips() to re-render the gallery from backend data.
    await this.page.evaluate(async () => {
      // @ts-ignore
      if (typeof loadClips === 'function') await loadClips();
    });
    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });
  }

  async getClipCountFromDB(archived: boolean = false): Promise<number> {
    // Query the database directly via Wails API to get accurate count
    return this.page.evaluate(async (isArchived) => {
      // @ts-ignore - Wails runtime
      const clips = await window.go.main.App.GetClips(isArchived, [], [], "", "");
      return clips?.length || 0;
    }, archived);
  }

  /**
   * Wait for a watch folder import to complete. Polls the DB first (giving fsnotify
   * a chance), then falls back to ProcessExistingFilesInFolder if the event was missed.
   * This handles macOS kqueue unreliability under parallel test load.
   */
  async waitForWatchImport(expectedCount: number = 1, archived: boolean = false): Promise<void> {
    let forcedScan = false;
    await expect.poll(
      async () => {
        const count = await this.getClipCountFromDB(archived);
        if (count >= expectedCount) return count;

        // After first poll, force a directory scan as fallback
        if (!forcedScan) {
          forcedScan = true;
          await this.page.evaluate(async () => {
            // @ts-ignore - Wails runtime
            const App = window.go?.main?.App;
            if (!App?.GetWatchedFolders) return;
            const folders = await App.GetWatchedFolders();
            for (const f of folders) {
              try {
                // @ts-ignore
                await App.ProcessExistingFilesInFolder(f.id);
              } catch {}
            }
          });
        }
        return count;
      },
      { timeout: 15000, intervals: [1000, 1000, 2000, 2000], message: `Expected ${expectedCount} imported clip(s)` }
    ).toBeGreaterThanOrEqual(expectedCount);
  }

  async getClipByFilename(filename: string): Promise<Locator> {
    return this.page.locator(selectors.gallery.clipCardByName(filename));
  }

  async getClipById(id: string): Promise<Locator> {
    return this.page.locator(selectors.gallery.clipCardById(id));
  }

  async getAllClips(): Promise<Locator> {
    return this.page.locator(selectors.gallery.clipCard);
  }

  async deleteClip(filename: string): Promise<void> {
    await this.clickDeleteInCardMenu(filename);
    await this.confirmDialog();
  }

  // Open the card menu and click delete, but don't confirm the dialog
  // This is useful for tests that need to check the dialog behavior
  async clickDeleteInCardMenu(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    // Open the card menu
    await clip.locator(selectors.clipActions.menuTrigger).click();
    // Wait for menu to appear
    await this.page.waitForSelector(selectors.cardMenu.dropdown);
    // Click delete
    await this.page.locator(selectors.cardMenu.delete).click();
  }

  async archiveClip(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    // Open the card menu
    await clip.locator(selectors.clipActions.menuTrigger).click();
    // Wait for menu to appear
    await this.page.waitForSelector(selectors.cardMenu.dropdown);
    // Click archive
    await this.page.locator(selectors.cardMenu.archive).click();
  }

  async editClip(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    // Open the card menu
    await clip.locator(selectors.clipActions.menuTrigger).click();
    // Wait for menu to appear
    await this.page.waitForSelector(selectors.cardMenu.dropdown);
    // Click edit
    await this.page.locator(selectors.cardMenu.edit).click();
  }

  async viewClip(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    await clip.locator(selectors.clipActions.view).click();
  }

  async copyClipPath(filename: string): Promise<void> {
    // Open card menu and hover copy submenu to reveal copy actions
    await this.hoverCopySubmenu(filename);
    // Click copy path
    await this.page.locator(selectors.cardMenu.copyPath).click();
  }

  async saveClipToFile(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    // Open the card menu
    await clip.locator(selectors.clipActions.menuTrigger).click();
    // Wait for menu to appear
    await this.page.waitForSelector(selectors.cardMenu.dropdown);
    // Click save
    await this.page.locator(selectors.cardMenu.save).click();
  }

  // Delete all clips (for cleanup) - uses API directly for reliability
  async deleteAllClips(): Promise<void> {
    await this.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const clips = await window.go.main.App.GetClips(false, [], [], "", "");
      // @ts-ignore
      const archivedClips = await window.go.main.App.GetClips(true, [], [], "", "");
      const allClips = [...clips, ...archivedClips];

      for (const clip of allClips) {
        try {
          // @ts-ignore
          await window.go.main.App.DeleteClip(clip.id);
        } catch {
          // Ignore individual delete errors
        }
      }
    });
    // Refresh the page to update the UI
    await this.page.reload();
    await this.waitForReady();
  }

  // ==================== Test Isolation ====================

  /**
   * Reset all app state to ensure test isolation.
   * Called both before and after each test.
   */
  async resetAppState(): Promise<void> {
    // 1. Close any open modals (prevents interaction issues)
    await this.closeAllModalsSafe();

    // 2. Reset data state (order: plugins -> watch folders -> clips -> tags)
    await this.deleteAllPluginsSafe();
    await this.deleteAllWatchFoldersSafe();
    await this.deleteAllClipsSafe();
    await this.deleteAllTagsSafe();

    // 3. Reset UI state
    await this.resetUIState();
  }

  private async closeAllModalsSafe(): Promise<void> {
    // Check and close each modal type
    // Lightbox uses .active class
    try {
      if (await this.isLightboxOpen()) {
        await this.closeLightbox();
      }
    } catch {
      // Ignore - modal may not exist or already closed
    }

    // Editor uses .active class
    try {
      if (await this.isEditorOpen()) {
        await this.closeImageEditor();
      }
    } catch {
      // Ignore
    }

    // Comparison uses .active class
    try {
      const comparisonOpen = await this.page.evaluate((selector) => {
        const el = document.querySelector(selector);
        return el ? el.classList.contains('active') : false;
      }, selectors.comparison.modal);
      if (comparisonOpen) {
        await this.closeComparison();
      }
    } catch {
      // Ignore
    }

    // Reset watch/serve views back to clips via switchView
    try {
      await this.page.evaluate(() => {
        // @ts-ignore
        if (window.__testHelpers?.switchView) {
          // @ts-ignore
          window.__testHelpers.switchView('clips');
        }
      });
    } catch {
      // Ignore
    }

    // Plugins modal uses opacity classes
    try {
      if (await this.isPluginsModalOpen()) {
        await this.closePluginsModal();
      }
    } catch {
      // Ignore
    }

    // Text editor modal
    try {
      const textEditorVisible = await this.page.locator(selectors.textEditor.modal).isVisible();
      if (textEditorVisible) {
        await this.page.locator(selectors.textEditor.cancelButton).click();
      }
    } catch {
      // Ignore
    }

    // Cheat sheet
    try {
      const cheatsheetOpen = await this.page.evaluate(() => {
        const el = document.getElementById('shortcuts-cheatsheet');
        return el ? el.classList.contains('opacity-100') : false;
      });
      if (cheatsheetOpen) {
        await this.page.evaluate(() => {
          const el = document.getElementById('shortcuts-cheatsheet');
          if (el) {
            el.classList.remove('opacity-100');
            el.classList.add('opacity-0', 'pointer-events-none');
          }
        });
      }
    } catch {
      // Ignore
    }

    // Plugin options modal uses .active class
    try {
      if (await this.isPluginOptionsModalOpen()) {
        await this.cancelPluginOptionsForm();
      }
    } catch {
      // Ignore
    }

    // Folder modal (watch folder add/edit)
    try {
      const folderModalVisible = await this.page.locator('#folder-modal').isVisible();
      if (folderModalVisible) {
        await this.page.locator('#folder-modal-cancel').click();
      }
    } catch {
      // Ignore
    }

    // Context menu (card menu / lightbox file menu)
    try {
      await this.page.evaluate(() => {
        // @ts-ignore - ContextMenu is global
        if (typeof ContextMenu !== 'undefined' && ContextMenu.isOpen()) {
          ContextMenu.close();
        }
      });
    } catch {
      // Ignore
    }
  }

  private async deleteAllPluginsSafe(): Promise<void> {
    try {
      await this.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        if (typeof window.go?.main?.PluginService?.GetPlugins !== 'function') {
          return; // Plugin API not available
        }
        // @ts-ignore
        const plugins = await window.go.main.PluginService.GetPlugins();
        for (const plugin of plugins) {
          try {
            // @ts-ignore
            await window.go.main.PluginService.RemovePlugin(plugin.id);
          } catch {
            // Ignore individual delete errors
          }
        }
      });
    } catch {
      // Plugin API may not be available
    }
  }

  private async deleteAllWatchFoldersSafe(): Promise<void> {
    try {
      await this.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        const folders = await window.go.main.App.GetWatchedFolders();
        for (const folder of folders) {
          try {
            // @ts-ignore
            await window.go.main.App.RemoveWatchedFolder(folder.id);
          } catch {
            // Ignore individual delete errors
          }
        }
      });
    } catch {
      // Ignore errors
    }
  }

  private async deleteAllClipsSafe(): Promise<void> {
    try {
      // Inline version without page reload (faster for cleanup)
      await this.page.evaluate(async () => {
        // @ts-ignore
        const clips = await window.go.main.App.GetClips(false, [], [], "", "");
        // @ts-ignore
        const archivedClips = await window.go.main.App.GetClips(true, [], [], "", "");
        for (const clip of [...clips, ...archivedClips]) {
          try {
            // @ts-ignore
            await window.go.main.App.DeleteClip(clip.id);
          } catch {
            // Ignore
          }
        }
      });
    } catch {
      // Ignore errors
    }
  }

  private async deleteAllTagsSafe(): Promise<void> {
    try {
      await this.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        const tags = await window.go.main.App.GetTags();
        for (const tag of tags) {
          try {
            // @ts-ignore
            await window.go.main.App.DeleteTag(tag.id);
          } catch {
            // Ignore individual delete errors
          }
        }
      });
    } catch {
      // Ignore errors
    }
  }

  private async resetUIState(): Promise<void> {
    // Clear search
    try {
      const searchInput = this.page.locator(selectors.header.searchInput);
      const searchValue = await searchInput.inputValue();
      if (searchValue) {
        await searchInput.clear();
      }
    } catch {
      // Ignore
    }

    // Clear tag filters
    try {
      await this.page.evaluate(() => {
        // @ts-ignore
        if (window.__testHelpers) {
          // @ts-ignore
          window.__testHelpers.setActiveTagFilters([]);
        }
      });
    } catch {
      // Ignore
    }

    // Switch to active view if in archive
    try {
      if (await this.isArchiveViewActive()) {
        await this.toggleArchiveView();
      }
    } catch {
      // Ignore
    }

    // Clear selected clips via frontend state
    try {
      await this.page.evaluate(() => {
        // @ts-ignore - Global state
        if (typeof window.selectedIds !== 'undefined' && window.selectedIds.size > 0) {
          // @ts-ignore
          window.selectedIds.clear();
          // @ts-ignore
          if (typeof window.renderGallery === 'function') {
            // @ts-ignore
            window.renderGallery();
          }
        }
      });
    } catch {
      // Ignore
    }
  }

  /**
   * Fast state reset using a single page.evaluate call.
   * Used with worker-scoped page to avoid per-test page navigation.
   * Eliminates ~15 browser round-trips per reset.
   */
  async fastReset(): Promise<void> {
    await this.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const App = window.go?.main?.App;
      // @ts-ignore
      const PS = window.go?.main?.PluginService;

      // --- 1. Delete all backend data ---

      try {
        if (PS?.GetPlugins) {
          const plugins = await PS.GetPlugins();
          await Promise.all(plugins.map((p: any) => PS.RemovePlugin(p.id).catch(() => {})));
        }
      } catch {}

      try {
        if (App?.GetWatchedFolders) {
          const folders = await App.GetWatchedFolders();
          await Promise.all(folders.map((f: any) => App.RemoveWatchedFolder(f.id).catch(() => {})));
        }
      } catch {}

      try {
        if (App?.GetClips) {
          const [clips, archived] = await Promise.all([
            App.GetClips(false, [], [], "", ""),
            App.GetClips(true, [], [], "", ""),
          ]);
          const all = [...(clips || []), ...(archived || [])];
          await Promise.all(all.map((c: any) => App.DeleteClip(c.id).catch(() => {})));
        }
      } catch {}

      try {
        if (App?.GetTags) {
          const tags = await App.GetTags();
          await Promise.all(tags.map((t: any) => App.DeleteTag(t.id).catch(() => {})));
        }
      } catch {}

      // Clear hidden tags setting
      try {
        if (App?.SetHiddenTags) {
          await App.SetHiddenTags([]);
        }
      } catch {}

      // Reset sort preferences to defaults
      try {
        if (App?.SetSetting) {
          await App.SetSetting('sort_field', 'date');
          await App.SetSetting('sort_dir', 'desc');
        }
      } catch {}

      // --- 2. Close all modals via DOM ---

      // Use the real editor teardown so worker-scoped pages do not retain
      // canvas listeners, history, selections, or animation frames.
      const editorModal = document.getElementById('editor-modal');
      const closeEditorFn = (window as any).closeEditor;
      if (typeof closeEditorFn === 'function') {
        closeEditorFn({ force: true, discardDraft: true });
      } else if (editorModal) {
        editorModal.classList.remove('active');
        editorModal.setAttribute('inert', '');
      }

      // All modals use opacity-0/pointer-events-none when closed
      const modalIds = [
        'confirm-dialog', 'restore-confirm-dialog', 'folder-modal',
        'settings-modal', 'maintenance-modal', 'plugin-options-modal',
        'plugin-result-modal', 'plugin-review-modal',
      ];
      for (const id of modalIds) {
        const el = document.getElementById(id);
        if (el) {
          el.classList.remove('opacity-100');
          el.classList.add('opacity-0', 'pointer-events-none');
        }
      }

      // Plugins modal uses data-testid
      const pluginsModal = document.querySelector('[data-testid="plugins-modal"]');
      if (pluginsModal) {
        pluginsModal.classList.remove('opacity-100');
        pluginsModal.classList.add('opacity-0', 'pointer-events-none');
      }

      // Close through the controller so its focus trap and background inert state are released.
      // @ts-ignore classic-script controller
      if (window.LightboxController) window.LightboxController.close();
      else document.querySelector('#lightbox')?.classList.remove('active');
      document.querySelector('#editor-modal')?.classList.remove('active');
      document.querySelector('#comparison-modal')?.classList.remove('active');

      // Watch view uses .hidden class
      document.querySelector('#watch-view')?.classList.add('hidden');

      // Folder move modal (dynamically created; open = flex, closed = hidden)
      const folderMoveModal = document.querySelector('[data-testid="folder-move-modal"]');
      if (folderMoveModal) {
        folderMoveModal.classList.add('hidden');
        folderMoveModal.classList.remove('flex');
      }

      // Share modals (all use .hidden class)
      const shareModalIds = [
        'create-share-modal',
        'follow-share-modal',
        'edit-follow-modal',
        'share-logs-modal',
      ];
      for (const id of shareModalIds) {
        const el = document.getElementById(id);
        if (el) el.classList.add('hidden');
      }

      // Shortcuts cheatsheet
      const cheatsheet = document.getElementById('shortcuts-cheatsheet');
      if (cheatsheet) {
        cheatsheet.classList.remove('opacity-100');
        cheatsheet.classList.add('opacity-0', 'pointer-events-none');
      }

      // Card menu dropdown (dynamically created)
      document.querySelector('.card-menu-dropdown')?.remove();

      // Sort popover (dynamically created)
      document.querySelector('.sort-popover')?.remove();

      // Tag popover
      const tagPopover = document.getElementById('tag-popover');
      if (tagPopover) tagPopover.classList.add('hidden');

      // Tag filter dropdown
      const tagFilterDropdown = document.getElementById('tag-filter-dropdown');
      if (tagFilterDropdown) tagFilterDropdown.classList.add('hidden');

      // Close plugin URL input container
      const urlContainer = document.getElementById('plugin-url-container');
      if (urlContainer) urlContainer.classList.add('hidden');

      // Lightbox plugin menu
      document.getElementById('lightbox-plugin-menu')?.classList.add('hidden');

      // Clear stale plugin list content (prevents waitForFunction from resolving with old data)
      const pluginListEl = document.querySelector('[data-testid="plugins-list"]');
      if (pluginListEl) pluginListEl.innerHTML = '';
      const pluginsEmptyEl = document.getElementById('plugins-empty-state');
      if (pluginsEmptyEl) pluginsEmptyEl.classList.add('hidden');

      // --- 3. Reset JS state ---

      // @ts-ignore
      const helpers = window.__testHelpers;
      if (helpers) {
        helpers.setActiveTagFilters([]);
        if (helpers.setHiddenTags) helpers.setHiddenTags([]);
        if (helpers.setViewingArchive) helpers.setViewingArchive(false);
        if (helpers.setViewingWatch) helpers.setViewingWatch(false);
        if (helpers.setFolderMode) helpers.setFolderMode(false);
        if (helpers.setSort) helpers.setSort('date', 'desc');

        // Clear currentFolderTagID — it's tags.js module state not wired into
        // setFolderMode. Stale IDs from prior tests cause the tag:updated
        // handler to misinterpret rename events as deletions and exit folder mode.
        // @ts-ignore
        if (typeof window.rememberCurrentFolder === 'function') {
          // @ts-ignore
          window.rememberCurrentFolder(null);
        }

        // Stop folderStatusPoller — it's only evaluated from toggleFolderMode(),
        // which setFolderMode bypasses. Leaving it running means every 2s across
        // the entire suite we fire GetServeStatus + GetShareStatus for nothing.
        // @ts-ignore
        if (window.folderStatusPoller?.stop) {
          // @ts-ignore
          window.folderStatusPoller.stop();
        }

        // Reset keyboard shortcut overrides to defaults
        const sm = helpers.getShortcutManager ? helpers.getShortcutManager() : null;
        if (sm) {
          sm.resetAllToDefaults();
          sm.clearFocus();
        }
      }

      // Close nav drawer if open and restore inert
      const navDrawer = document.getElementById('nav-drawer');
      if (navDrawer) {
        navDrawer.classList.add('translate-x-full');
        navDrawer.setAttribute('inert', '');
      }
      const drawerOverlay = document.getElementById('drawer-overlay');
      if (drawerOverlay) {
        drawerOverlay.classList.add('opacity-0', 'pointer-events-none');
        drawerOverlay.classList.remove('opacity-100');
      }
      const drawerToggle = document.getElementById('drawer-toggle-btn');
      if (drawerToggle) {
        drawerToggle.setAttribute('aria-expanded', 'false');
      }

      // Reset folder mode button UI
      const folderModeBtn = document.getElementById('folder-mode-btn');
      if (folderModeBtn) {
        folderModeBtn.setAttribute('aria-pressed', 'false');
        folderModeBtn.classList.remove('bg-stone-800', 'text-white', 'border-stone-800');
        folderModeBtn.classList.add('border-stone-200', 'text-stone-500', 'hover:border-stone-300', 'hover:bg-stone-100');
      }

      // Reset archive button UI (ID: toggle-archive-view-btn)
      const archiveBtn = document.getElementById('toggle-archive-view-btn');
      if (archiveBtn) {
        archiveBtn.setAttribute('aria-pressed', 'false');
        const btnText = document.getElementById('archive-btn-text');
        if (btnText) btnText.textContent = 'Archive';
        archiveBtn.classList.remove('bg-stone-800', 'text-white', 'border-stone-800');
        archiveBtn.classList.add('border-stone-200', 'text-stone-600', 'hover:border-stone-300', 'hover:bg-stone-100');
      }

      // Reset header archive button UI
      const headerArchiveBtn = document.getElementById('header-archive-btn');
      if (headerArchiveBtn) {
        headerArchiveBtn.setAttribute('aria-pressed', 'false');
        headerArchiveBtn.classList.remove('bg-stone-800', 'text-white', 'border-stone-800');
        headerArchiveBtn.classList.add('border-stone-200', 'text-stone-500', 'hover:border-stone-300', 'hover:bg-stone-100');
      }

      // Reset view tabs to clips view
      // @ts-ignore
      if (window.__testHelpers?.switchView) {
        // @ts-ignore
        window.__testHelpers.switchView('clips');
      } else {
        // Fallback: click the clips tab directly
        const clipsTab = document.getElementById('view-tab-clips');
        if (clipsTab) clipsTab.click();
      }

      // Reset drop overlay
      const dropOverlay = document.getElementById('drop-overlay');
      if (dropOverlay) {
        dropOverlay.classList.add('opacity-0');
        dropOverlay.classList.remove('opacity-100');
      }

      // Make sure gallery parent is visible
      const gallery = document.getElementById('gallery');
      if (gallery?.parentElement) gallery.parentElement.classList.remove('hidden');

      // Clear search
      const searchInput = document.getElementById('search-input') as HTMLInputElement;
      if (searchInput) searchInput.value = '';

      // Reset upload expiration dropdown to "No expiration"
      const expirySelect = document.getElementById('expiry-select') as HTMLSelectElement;
      if (expirySelect) expirySelect.value = '0';

      // --- 4. Reload gallery and caches with clean state ---

      // @ts-ignore
      window.__appReady = false;

      // Refresh plugin UI actions cache (so card menus/lightbox reflect deleted plugins).
      // Every plugin was just removed, so the empty result is final — skip the
      // startup-race backoff, which would otherwise sleep ~1.85s per reset.
      // @ts-ignore - loadPluginUIActions is a global function from ui.js
      if (typeof loadPluginUIActions === 'function') await loadPluginUIActions({ retry: false });

      // @ts-ignore - loadClips is a global function from wails-api.js
      if (typeof loadClips === 'function') await loadClips();

      // Re-fetch tags
      if (App?.GetTags) {
        const tags = await App.GetTags();
        if (helpers?.setAllTags) helpers.setAllTags(tags);
        // @ts-ignore
        if (typeof renderTagFilterDropdown === 'function') renderTagFilterDropdown();
      }

      // Clear lingering DOM focus so next test's keyboard shortcuts aren't
      // swallowed by the input guard (previously-focused INPUT/SELECT survives
      // a body.click).
      (document.activeElement as HTMLElement | null)?.blur();

      // @ts-ignore
      window.__appReady = true;
    });

    // Verify app is ready
    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });
  }

  // ==================== Bulk Operations ====================

  async selectClip(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    const checkbox = clip.locator(selectors.gallery.clipCheckbox);
    await checkbox.check();
  }

  async deselectClip(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    const checkbox = clip.locator(selectors.gallery.clipCheckbox);
    await checkbox.uncheck();
  }

  async selectClips(filenames: string[]): Promise<void> {
    for (const filename of filenames) {
      await this.selectClip(filename);
    }
  }

  async shiftSelectClip(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    const checkbox = clip.locator(selectors.gallery.clipCheckbox);
    await checkbox.click({ modifiers: ['Shift'] });
  }

  async selectAll(): Promise<void> {
    // First select one clip to make bulk toolbar visible
    const firstClip = this.page.locator(selectors.gallery.clipCard).first();
    await firstClip.locator(selectors.gallery.clipCheckbox).check();
    // Now the toolbar is visible, click select all
    const checkbox = this.page.locator(selectors.bulk.selectAllCheckbox);
    await checkbox.check();
  }

  async deselectAll(): Promise<void> {
    const checkbox = this.page.locator(selectors.bulk.selectAllCheckbox);
    await checkbox.uncheck();
  }

  async getSelectedCount(): Promise<number> {
    // Check if toolbar is visible first (has pointer-events-auto class)
    const isVisible = await this.page.evaluate((selector) => {
      const toolbar = document.querySelector(selector);
      return toolbar?.classList.contains('pointer-events-auto') ?? false;
    }, selectors.bulk.toolbar);
    if (!isVisible) return 0;

    const text = await this.page.locator(selectors.bulk.selectedCount).textContent();
    const match = text?.match(/(\d+)/);
    return match ? parseInt(match[1]) : 0;
  }

  async bulkDelete(): Promise<void> {
    await this.page.locator(selectors.bulk.deleteButton).click();
  }

  async bulkArchive(): Promise<void> {
    await this.page.locator(selectors.bulk.archiveButton).click();
  }

  async bulkDownload(): Promise<void> {
    await this.page.locator(selectors.bulk.downloadButton).click();
  }

  async bulkCompare(): Promise<void> {
    await this.page.locator(selectors.bulk.moreButton).click();
    await this.page.locator('.card-menu-dropdown [data-action="compare"]').click();
  }

  isBulkToolbarVisible(): Promise<boolean> {
    // Toolbar uses opacity and pointer-events classes, not display
    return this.page.evaluate((selector) => {
      const toolbar = document.querySelector(selector);
      return toolbar?.classList.contains('pointer-events-auto') ?? false;
    }, selectors.bulk.toolbar);
  }

  // ==================== Lightbox ====================

  async openLightbox(filename: string): Promise<void> {
    await this.viewClip(filename);
    await this.page.locator(`${selectors.lightbox.overlay}.active`).waitFor({ state: 'visible' });
    await expect(this.page.locator(selectors.lightbox.caption)).toContainText(filename);
    await expect.poll(async () => {
      const image = this.page.locator(selectors.lightbox.image);
      return image.evaluate((element: HTMLImageElement) => element.complete && element.naturalWidth > 0);
    }).toBe(true);
  }

  async closeLightbox(): Promise<void> {
    await this.page.locator(selectors.lightbox.closeButton).click();
    await expect(this.page.locator(selectors.lightbox.overlay)).not.toHaveClass(/active/);
  }

  async lightboxNext(): Promise<void> {
    await this.page.locator(selectors.lightbox.nextButton).click();
  }

  async lightboxPrev(): Promise<void> {
    await this.page.locator(selectors.lightbox.prevButton).click();
  }

  isLightboxOpen(): Promise<boolean> {
    // Lightbox uses active class with opacity, not display:none
    return this.page.evaluate((selector) => {
      const el = document.querySelector(selector);
      return el ? el.classList.contains('active') : false;
    }, selectors.lightbox.overlay);
  }

  // ==================== Image Editor ====================

  async openImageEditor(filename: string): Promise<void> {
    await this.editClip(filename);
    await this.page.waitForSelector(`${selectors.editor.modal}.active`);
  }

  async closeImageEditor(): Promise<void> {
    // Use JavaScript click for potentially off-viewport button
    await this.page.evaluate((selector) => {
      const btn = document.querySelector(selector);
      if (btn) (btn as HTMLElement).click();
    }, selectors.editor.cancelButton);

    const needsDiscardConfirmation = await this.page.locator(selectors.confirm.dialog)
      .evaluate((dialog) => dialog.classList.contains('opacity-100'));
    if (needsDiscardConfirmation) {
      await this.page.locator(selectors.confirm.confirmButton).click();
    }

    await this.page.waitForSelector(`${selectors.editor.modal}:not(.active)`);
  }

  async selectTool(tool: string): Promise<void> {
    await this.page.locator(`[data-tool="${tool}"]`).click();
  }

  async setEditorColor(color: string): Promise<void> {
    await this.page.locator(selectors.editor.colorPicker).fill(color);
  }

  async setBrushSize(size: number): Promise<void> {
    await this.page.locator(selectors.editor.brushSize).fill(String(size));
  }

  async drawOnCanvas(from: Point, to: Point): Promise<void> {
    const canvas = this.page.locator(selectors.editor.canvas);
    const box = await canvas.boundingBox();
    if (!box) throw new Error('Canvas not visible');

    await this.page.mouse.move(box.x + from.x, box.y + from.y);
    await this.page.mouse.down();
    await this.page.mouse.move(box.x + to.x, box.y + to.y);
    await this.page.mouse.up();
  }

  async editorUndo(): Promise<void> {
    await this.page.locator(selectors.editor.undoButton).click();
  }

  async editorRedo(): Promise<void> {
    await this.page.locator(selectors.editor.redoButton).click();
  }

  async saveEditorAsNewClip(filename?: string): Promise<void> {
    await this.page.locator(selectors.editor.saveButton).click();
    const filenameInput = this.page.locator('#editor-filename');
    await expect(filenameInput).toBeVisible();
    if (filename) await filenameInput.fill(filename);
    await this.page.locator(selectors.editor.saveButton).click();
    await this.page.waitForSelector(`${selectors.editor.modal}:not(.active)`);
  }

  isEditorOpen(): Promise<boolean> {
    return this.page.evaluate((selector) => {
      const el = document.querySelector(selector);
      return el ? el.classList.contains('active') : false;
    }, selectors.editor.modal);
  }

  async setZoom(action: 'fit' | '100' | 'in' | 'out'): Promise<void> {
    const map: Record<string, string> = {
      fit: selectors.editor.zoomFit,
      '100': selectors.editor.zoom100,
      'in': selectors.editor.zoomIn,
      out: selectors.editor.zoomOut,
    };
    await this.page.locator(map[action]).click();
  }

  async getZoomLevel(): Promise<string> {
    return await this.page.locator(selectors.editor.zoomDisplay).textContent() || '';
  }

  async cropImage(from: Point, to: Point): Promise<void> {
    await this.selectTool('crop');
    await this.drawOnCanvas(from, to);
    await this.page.locator(selectors.editor.cropConfirm).click();
  }

  async anonymizeRegion(from: Point, to: Point, mode: 'brush' | 'rect' = 'rect'): Promise<void> {
    await this.selectTool('anonymize');
    if (mode === 'rect') {
      await this.page.locator(selectors.editor.anonRect).click();
    } else {
      await this.page.locator(selectors.editor.anonBrush).click();
    }
    await this.drawOnCanvas(from, to);
  }

  async getCanvasDimensions(): Promise<{ width: number; height: number }> {
    return this.page.evaluate((sel) => {
      const canvas = document.querySelector(sel) as HTMLCanvasElement;
      return canvas ? { width: canvas.width, height: canvas.height } : { width: 0, height: 0 };
    }, selectors.editor.canvas);
  }

  async isToolActive(tool: string): Promise<boolean> {
    const btn = this.page.locator(`[data-tool="${tool}"]`);
    return btn.evaluate((el) => el.classList.contains('active'));
  }

  async isUndoEnabled(): Promise<boolean> {
    return this.page.evaluate((sel) => {
      const btn = document.querySelector(sel) as HTMLButtonElement;
      return btn ? !btn.disabled : false;
    }, selectors.editor.undoButton);
  }

  async setFontSize(size: number): Promise<void> {
    await this.page.locator(selectors.editor.fontSize).fill(String(size));
  }

  // ==================== Image Comparison ====================

  async openComparison(): Promise<void> {
    await this.bulkCompare();
    await this.page.waitForSelector(`${selectors.comparison.modal}.active`);
  }

  async closeComparison(): Promise<void> {
    await this.page.evaluate((selector) => {
      const btn = document.querySelector(selector);
      if (btn) (btn as HTMLElement).click();
    }, selectors.comparison.closeButton);
    await this.page.waitForSelector(`${selectors.comparison.modal}:not(.active)`);
  }

  async setComparisonMode(mode: 'fade' | 'slider' | 'diff'): Promise<void> {
    if (mode === 'fade') {
      await this.page.locator(selectors.comparison.modeFade).click();
    } else if (mode === 'slider') {
      await this.page.locator(selectors.comparison.modeSlider).click();
    } else {
      await this.page.locator(selectors.comparison.modeDiff).click();
    }
  }

  async setFadeLevel(level: number): Promise<void> {
    await this.page.locator(selectors.comparison.rangeSlider).fill(String(level));
  }

  async setSliderPosition(position: number): Promise<void> {
    await this.page.locator(selectors.comparison.rangeSlider).fill(String(position));
  }

  isComparisonOpen(): Promise<boolean> {
    return this.page.evaluate((selector) => {
      const el = document.querySelector(selector);
      return el ? el.classList.contains('active') : false;
    }, selectors.comparison.modal);
  }

  async swapComparisonImages(): Promise<void> {
    await this.page.locator(selectors.comparison.swapButton).click();
  }

  async getComparisonSimilarity(): Promise<string> {
    return await this.page.locator(selectors.comparison.similarity).textContent() ?? '';
  }

  async getComparisonImageInfo(): Promise<string> {
    return await this.page.locator(selectors.comparison.imageInfo).textContent() ?? '';
  }

  async isComparisonDiffVisible(): Promise<boolean> {
    const img = this.page.locator(selectors.comparison.diffImage);
    return !(await img.evaluate(el => el.classList.contains('hidden')));
  }

  async getComparisonRangeLabel(): Promise<string> {
    return await this.page.locator(selectors.comparison.rangeLabel).textContent() ?? '';
  }

  // ==================== Watch Folders ====================

  async openWatchView(): Promise<void> {
    // Check if already open
    const isOpen = await this.page.locator(selectors.watch.view).isVisible();
    if (!isOpen) {
      await this.openDrawer();
      await this.page.locator(selectors.drawer.watchButton).click();
      await this.page.waitForSelector(`${selectors.watch.view}:not(.hidden)`, { timeout: 5000 });
    }
  }

  async closeWatchView(): Promise<void> {
    // Check if already closed
    const isOpen = await this.page.locator(selectors.watch.view).isVisible();
    if (isOpen) {
      await this.openDrawer();
      await this.page.locator(selectors.viewTabs.clips).click();
      await this.page.waitForFunction((selector) => {
        const el = document.querySelector(selector);
        return el?.classList.contains('hidden');
      }, selectors.watch.view, { timeout: 5000 });
    }
  }

  async isWatchViewOpen(): Promise<boolean> {
    return this.page.locator(selectors.watch.view).isVisible();
  }

  async toggleGlobalWatch(enabled: boolean): Promise<void> {
    // Use the Wails API directly to set global pause state
    // enabled=true means NOT paused, enabled=false means paused
    const paused = !enabled;
    await this.page.evaluate(async (isPaused) => {
      // @ts-ignore - Wails runtime
      await window.go.main.App.SetGlobalWatchPaused(isPaused);
      // @ts-ignore - Refresh watches to update state
      await window.go.main.App.RefreshWatches();
      // @ts-ignore - Update the watch indicator in the UI
      if (typeof updateWatchIndicator === 'function') await updateWatchIndicator();
    }, paused);

    // Update the UI checkbox to match
    await this.page.evaluate(({ selector, newState }) => {
      const toggle = document.querySelector(selector) as HTMLInputElement;
      if (toggle) {
        toggle.checked = newState;
      }
      // Update label text
      const label = document.getElementById('global-watch-label');
      if (label) {
        label.textContent = newState ? 'Watching active' : 'Watching paused';
      }
    }, { selector: selectors.watch.globalToggle, newState: enabled });

    // Wait for label to reflect state
    await expect(this.page.locator('#global-watch-label')).toContainText(
      enabled ? /active/i : /paused/i, { timeout: 5000 }
    );
  }

  async getWatchFolderCount(): Promise<number> {
    const text = await this.page.locator(selectors.watch.folderCount).textContent();
    const match = text?.match(/(\d+)/);
    return match ? parseInt(match[1]) : 0;
  }

  async addWatchFolder(folderPath: string, options: {
    filterMode?: 'all' | 'presets' | 'custom';
    filterPresets?: string[];
    filterRegex?: string;
    processExisting?: boolean;
    autoArchive?: boolean;
  } = {}): Promise<void> {
    // This would typically trigger a native dialog
    // For testing, we'll use the Wails API directly via page.evaluate
    await this.page.evaluate(async ({ path, opts }) => {
      // @ts-ignore - Wails runtime
      await window.go.main.App.AddWatchedFolder({
        path,
        filter_mode: opts.filterMode || 'all',
        filter_presets: opts.filterPresets || [],
        filter_regex: opts.filterRegex || '',
        process_existing: opts.processExisting || false,
        auto_archive: opts.autoArchive || false,
      });
      // @ts-ignore - Wails runtime
      await window.go.main.App.RefreshWatches();
    }, { path: folderPath, opts: options });
    // Refresh UI by toggling watch view
    await this.closeWatchView();
    await this.openWatchView();
    const cards = this.page.locator('#watch-folder-list > li');
    await expect(cards).not.toHaveCount(0, { timeout: 5000 });
    await expect(cards.first().locator('.truncate')).toBeVisible({ timeout: 5000 });
  }

  async removeWatchFolder(folderPath: string): Promise<void> {
    const folderCard = this.page.locator(selectors.watch.folderCard).filter({ hasText: folderPath });
    await folderCard.locator(selectors.watchFolder.deleteButton).click();
    await this.confirmDialog();
  }

  async pauseWatchFolder(folderPath: string): Promise<void> {
    const folderCard = this.page.locator(selectors.watch.folderCard).filter({ hasText: folderPath });
    await folderCard.locator(selectors.watchFolder.pauseToggle).click();
  }

  async deleteAllWatchFolders(): Promise<void> {
    await this.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const folders = await window.go.main.App.GetWatchedFolders();
      for (const folder of folders) {
        try {
          // @ts-ignore
          await window.go.main.App.RemoveWatchedFolder(folder.id);
        } catch {
          // Ignore individual delete errors
        }
      }
    });
  }

  async enableGlobalWatch(): Promise<void> {
    await this.toggleGlobalWatch(true);
  }

  async disableGlobalWatch(): Promise<void> {
    await this.toggleGlobalWatch(false);
  }

  async openAddFolderModal(): Promise<void> {
    // Click the add folder button
    await this.page.locator(selectors.watch.addFolderButton).click();
    // Wait for the modal to open
    await this.page.waitForSelector(selectors.watchEdit.modal, { state: 'visible' });
  }

  // ==================== Search & Filter ====================

  async search(query: string): Promise<void> {
    await this.page.locator(selectors.header.searchInput).fill(query);
    // Wait for gallery to re-render after search filter
    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });
  }

  async clearSearch(): Promise<void> {
    await this.page.locator(selectors.header.searchInput).clear();
    // Wait for gallery to re-render after search filter
    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });
  }

  async toggleArchiveView(): Promise<void> {
    await this.page.locator(selectors.header.archiveButton).click();
    // Wait for gallery to re-render after view toggle
    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });
  }

  async isArchiveViewActive(): Promise<boolean> {
    const btn = this.page.locator(selectors.drawer.archiveButton);
    const pressed = await btn.getAttribute('aria-pressed');
    return pressed === 'true';
  }

  // ==================== Dialogs & Toasts ====================

  async confirmDialog(): Promise<void> {
    // Wait for dialog to become active (has opacity-100 class)
    await this.page.waitForSelector(`${selectors.confirm.dialog}.opacity-100`, { timeout: 5000 });
    await this.page.locator(selectors.confirm.confirmButton).click();
    // Wait for dialog to be hidden (opacity-0 class)
    await this.page.waitForSelector(`${selectors.confirm.dialog}.opacity-0`);
  }

  async cancelDialog(): Promise<void> {
    // Wait for dialog to become active (has opacity-100 class)
    await this.page.waitForSelector(`${selectors.confirm.dialog}.opacity-100`, { timeout: 5000 });
    await this.page.locator(selectors.confirm.cancelButton).click();
    // Wait for dialog to be hidden (opacity-0 class)
    await this.page.waitForSelector(`${selectors.confirm.dialog}.opacity-0`);
  }

  async expectToast(message: string): Promise<void> {
    const toast = this.page.locator(selectors.toast.message).filter({ hasText: message });
    await expect(toast).toBeVisible({ timeout: 5000 });
  }

  async waitForToast(pattern: RegExp): Promise<string> {
    const toastLocator = this.page.locator(selectors.toast.message);
    let toastText = '';
    let attempts = 0;
    const maxAttempts = 50; // ~5s with 100ms polling

    while (attempts < maxAttempts) {
      toastText = await toastLocator.textContent() || '';
      if (toastText && pattern.test(toastText)) {
        return toastText;
      }
      await this.page.waitForTimeout(100);
      attempts++;
    }

    throw new Error(`Toast matching pattern ${pattern} not found. Last text: "${toastText}"`);
  }

  // ==================== Text Editor ====================

  async openTextEditor(filename: string): Promise<void> {
    await this.editClip(filename);
    await this.page.waitForSelector(`${selectors.textEditor.modal}.active`);
    await this.page.waitForSelector('#text-editor-view:not(.hidden)');
  }

  async getTextEditorContent(): Promise<string> {
    return this.page.locator(selectors.textEditor.textarea).inputValue();
  }

  async setTextEditorContent(content: string): Promise<void> {
    await this.page.locator(selectors.textEditor.textarea).fill(content);
  }

  async saveTextEditor(): Promise<void> {
    await this.page.locator(selectors.textEditor.saveButton).click();
    await this.page.waitForSelector(`${selectors.textEditor.modal}:not(.active)`);
  }

  async cancelTextEditor(): Promise<void> {
    await this.page.locator(selectors.textEditor.cancelButton).click();

    const needsDiscardConfirmation = await this.page.locator(selectors.confirm.dialog)
      .evaluate((dialog) => dialog.classList.contains('opacity-100'));
    if (needsDiscardConfirmation) {
      await this.page.locator(selectors.confirm.confirmButton).click();
    }

    await this.page.waitForSelector(`${selectors.textEditor.modal}:not(.active)`);
  }

  // ==================== Assertions ====================

  async expectClipVisible(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await expect(clip).toBeVisible();
  }

  async expectClipNotVisible(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await expect(clip).not.toBeVisible();
  }

  async expectClipCount(count: number, options?: { timeout?: number }): Promise<void> {
    const clips = this.page.locator(selectors.gallery.clipCard);
    await expect(clips).toHaveCount(count, { timeout: options?.timeout });
  }

  /**
   * Wait for clip count with a longer timeout - useful for watch folder imports
   * which can be slower under load.
   */
  async waitForClipCount(count: number, timeout: number = 30000): Promise<void> {
    const clips = this.page.locator(selectors.gallery.clipCard);
    await expect(clips).toHaveCount(count, { timeout });
  }

  async expectEmptyState(): Promise<void> {
    const emptyState = this.page.locator(selectors.gallery.emptyState);
    await expect(emptyState).toBeVisible();
  }

  // ==================== Tags ====================

  async createTag(name: string): Promise<{ tagID: number }> {
    // Create tag via API and update frontend state.
    // Returns the numeric ID of the leaf tag created (the last segment in a path like "a/b/c").
    const tagID = await this.page.evaluate(async (tagName) => {
      // @ts-ignore - Wails runtime
      await window.go.main.App.CreateTag(tagName);

      // Fetch updated tags from backend
      // @ts-ignore - Wails runtime
      const tags = await window.go.main.App.GetTags();

      // Update frontend state via test helper
      // @ts-ignore
      if (window.__testHelpers) {
        // @ts-ignore
        window.__testHelpers.setAllTags(tags);
      }

      // Return the id of the leaf tag that was just created.
      const created = tags.find((t: any) => t.name === tagName);
      return created ? created.id : 0;
    }, name);
    return { tagID: tagID as number };
  }

  async deleteTag(name: string): Promise<void> {
    // Delete tag via API
    await this.page.evaluate(async (tagName) => {
      // @ts-ignore - Wails runtime
      const tags = await window.go.main.App.GetTags();
      const tag = tags.find((t: any) => t.name === tagName);
      if (tag) {
        // @ts-ignore
        await window.go.main.App.DeleteTag(tag.id);
      }
    }, name);
    // Give the frontend a moment to process the tag:deleted event.
    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });
  }

  /** Rename a tag. Triggers the tag:updated Wails event which causes folder-view re-navigation. */
  async renameTag(oldName: string, newName: string): Promise<void> {
    await this.page.evaluate(async ({ oldTagName, newTagName }) => {
      // @ts-ignore - Wails runtime
      const tags = await window.go.main.App.GetTags();
      const tag = tags.find((t: any) => t.name === oldTagName);
      if (!tag) throw new Error(`Tag not found: ${oldTagName}`);
      // @ts-ignore
      await window.go.main.App.UpdateTag(tag.id, newTagName, tag.color || '');
    }, { oldTagName: oldName, newTagName: newName });
    // Wait for the tag:updated event to finish processing.
    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });
  }

  /**
   * Add a tag to the clip at the given 0-based gallery index.
   * Requires the clip to be visible in the current gallery view.
   */
  async tagClipByIndex(index: number, tagName: string): Promise<void> {
    await this.page.evaluate(async ({ idx, tag }) => {
      // @ts-ignore - Wails runtime
      const clips = await window.go.main.App.GetClips(false, [], [], '', '');
      if (!clips || idx >= clips.length) throw new Error(`No clip at index ${idx}`);
      const clip = clips[idx];

      // @ts-ignore
      const tags = await window.go.main.App.GetTags();
      const tagObj = tags.find((t: any) => t.name === tag);
      if (!tagObj) throw new Error(`Tag not found: ${tag}`);

      // @ts-ignore
      await window.go.main.App.AddTagToClip(clip.id, tagObj.id);

      // @ts-ignore
      if (window.__testHelpers?.loadClips) window.__testHelpers.loadClips();
    }, { idx: index, tag: tagName });
    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });
  }

  /**
   * Enter folder view: enable folder mode (if not already on) and navigate into the named tag.
   */
  async enterFolder(tagName: string): Promise<void> {
    // Enable folder mode if not already active.
    const isFolderActive = await this.page.evaluate(() => {
      // @ts-ignore
      return window.__testHelpers?.isFolderMode?.() === true;
    });
    if (!isFolderActive) {
      await this.toggleFolderMode();
    }
    // Navigate to the folder.
    await this.page.evaluate(async (name) => {
      // @ts-ignore - Wails runtime
      const tags = await window.go.main.App.GetTags();
      const tag = tags.find((t: any) => t.name === name);
      if (!tag) throw new Error(`Tag not found for enterFolder: ${name}`);
      // @ts-ignore
      if (typeof navigateToFolder === 'function') {
        // @ts-ignore
        navigateToFolder(tag.id);
      } else if (window.__testHelpers?.setFolderMode) {
        // Fallback: set active tag filter
        // @ts-ignore
        window.__testHelpers.setActiveTagFilters([tag.id]);
        // @ts-ignore
        window.__testHelpers.loadClips();
      }
    }, tagName);
    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });
  }

  /**
   * Assert that the folder header (breadcrumb pill) shows the given tag name.
   * In folder mode, the active-tags-container shows tag pills for the current path.
   * Scoped to #active-tags-container to avoid matching clip-card tag pills.
   */
  async expectFolderHeader(name: string): Promise<void> {
    const pill = this.page.locator(`#active-tags-container [data-testid="tag-pill-${name}"]`);
    await expect(pill).toBeVisible({ timeout: 5000 });
  }

  /** Assert that folder mode is NOT active (the folder-mode button is not pressed). */
  async expectNotInFolderMode(): Promise<void> {
    const btn = this.page.locator(selectors.tags.folderModeButton);
    await expect(btn).toHaveAttribute('aria-pressed', 'false', { timeout: 5000 });
  }

  /**
   * Toggle the tag filter chip for the named tag (non-folder mode).
   * Equivalent to filterByTag but named for clarity in filter-clear tests.
   */
  async selectTagFilter(name: string): Promise<void> {
    await this.filterByTag(name);
  }

  /** Assert that the named tag is NOT an active filter (no pill visible in active-tags-container). */
  async expectTagFilterInactive(name: string): Promise<void> {
    const pill = this.page.locator(`#active-tags-container [data-testid="tag-pill-${name}"]`);
    await expect(pill).not.toBeVisible({ timeout: 5000 });
  }

  async getAllTags(): Promise<Array<{ id: number; name: string; color: string }>> {
    return this.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      return await window.go.main.App.GetTags();
    });
  }

  async addTagToClip(clipFilename: string, tagName: string): Promise<void> {
    // Add tag via API directly
    await this.page.evaluate(async ({ filename, tag }) => {
      // Get clip ID by filename
      // @ts-ignore
      const clips = await window.go.main.App.GetClips(false, [], [], "", "");
      const clip = clips.find((c: any) =>
        c.filename?.toLowerCase().includes(filename.replace('.png', '').toLowerCase())
      );
      if (!clip) {
        throw new Error(`Clip not found: ${filename}`);
      }

      // Get tag ID by name
      // @ts-ignore
      const tags = await window.go.main.App.GetTags();
      const tagObj = tags.find((t: any) => t.name === tag);
      if (!tagObj) {
        throw new Error(`Tag not found: ${tag}`);
      }

      // Add tag to clip
      // @ts-ignore
      await window.go.main.App.AddTagToClip(clip.id, tagObj.id);

      // Refresh clips via test helper
      // @ts-ignore
      if (window.__testHelpers && window.__testHelpers.loadClips) {
        // @ts-ignore
        window.__testHelpers.loadClips();
      }
    }, { filename: clipFilename, tag: tagName });

    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });
  }

  async removeTagFromClip(clipFilename: string, tagName: string): Promise<void> {
    // Remove tag via API directly
    await this.page.evaluate(async ({ filename, tag }) => {
      // Get clip ID by filename
      // @ts-ignore
      const clips = await window.go.main.App.GetClips(false, [], [], "", "");
      const clip = clips.find((c: any) =>
        c.filename?.toLowerCase().includes(filename.replace('.png', '').toLowerCase())
      );
      if (!clip) {
        throw new Error(`Clip not found: ${filename}`);
      }

      // Get tag ID by name
      // @ts-ignore
      const tags = await window.go.main.App.GetTags();
      const tagObj = tags.find((t: any) => t.name === tag);
      if (!tagObj) {
        throw new Error(`Tag not found: ${tag}`);
      }

      // Remove tag from clip
      // @ts-ignore
      await window.go.main.App.RemoveTagFromClip(clip.id, tagObj.id);

      // Refresh clips via test helper
      // @ts-ignore
      if (window.__testHelpers && window.__testHelpers.loadClips) {
        // @ts-ignore
        window.__testHelpers.loadClips();
      }
    }, { filename: clipFilename, tag: tagName });

    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });
  }

  async enterFolderMode(): Promise<void> {
    const btn = this.page.locator('[data-testid="folder-mode-button"]');
    const pressed = await btn.getAttribute('aria-pressed');
    if (pressed !== 'true') {
      await btn.click();
    }
    await this.page.waitForFunction(() => {
      return document.querySelectorAll('[data-folder]').length > 0
          || !!document.querySelector('[data-testid="empty-state"]');
    }, null, { timeout: 2000 }).catch(() => { /* ok if no folders */ });
  }

  async openTagFilterDropdown(): Promise<void> {
    await this.page.evaluate(async () => {
      // Keep frontend test state in sync when tags were created directly via backend APIs.
      // @ts-ignore - Wails runtime
      const tags = await window.go.main.App.GetTags();
      // @ts-ignore
      if (window.__testHelpers?.setAllTags) {
        // @ts-ignore
        window.__testHelpers.setAllTags(tags);
      }
      // @ts-ignore
      if (typeof renderTagFilterDropdown === 'function') {
        // @ts-ignore
        renderTagFilterDropdown();
      }
    });

    const dropdown = this.page.locator(selectors.tags.filterDropdown);
    const isVisible = await dropdown.evaluate(el => !el.classList.contains('hidden'));
    if (!isVisible) {
      await this.page.locator(selectors.tags.filterButton).click();
      await this.page.waitForSelector(`${selectors.tags.filterDropdown}:not(.hidden)`);
    }
  }

  async closeTagFilterDropdown(): Promise<void> {
    const dropdown = this.page.locator(selectors.tags.filterDropdown);
    const isVisible = await dropdown.evaluate(el => !el.classList.contains('hidden'));
    if (isVisible) {
      await this.page.locator('body').click({ position: { x: 10, y: 10 } });
      // Wait for the dropdown to have the hidden class
      await this.page.waitForFunction((selector) => {
        const el = document.querySelector(selector);
        return el?.classList.contains('hidden');
      }, selectors.tags.filterDropdown, { timeout: 5000 });
    }
  }

  async filterByTag(tagName: string): Promise<void> {
    // Get tag ID and set filter via API
    await this.page.evaluate(async (tag) => {
      // Get tag ID by name
      // @ts-ignore
      const tags = await window.go.main.App.GetTags();
      const tagObj = tags.find((t: any) => t.name === tag);
      if (!tagObj) {
        throw new Error(`Tag not found: ${tag}`);
      }

      // Update active tag filters
      // @ts-ignore
      if (window.__testHelpers) {
        // @ts-ignore
        const currentFilters = window.__testHelpers.getActiveTagFilters();
        if (!currentFilters.includes(tagObj.id)) {
          currentFilters.push(tagObj.id);
        }
        // Update active tags display (pill bar) to reflect new filter
        // @ts-ignore
        if (typeof updateActiveTagsDisplay === 'function') updateActiveTagsDisplay();
        // @ts-ignore
        window.__testHelpers.loadClips();
      }
    }, tagName);

    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });
  }

  async filterByTags(tagNames: string[]): Promise<void> {
    // Get tag IDs and set filters via API
    await this.page.evaluate(async (tags) => {
      // Get all tags
      // @ts-ignore
      const allTags = await window.go.main.App.GetTags();

      const tagIds: number[] = [];
      for (const tagName of tags) {
        const tagObj = allTags.find((t: any) => t.name === tagName);
        if (!tagObj) {
          throw new Error(`Tag not found: ${tagName}`);
        }
        tagIds.push(tagObj.id);
      }

      // Update active tag filters
      // @ts-ignore
      if (window.__testHelpers) {
        // @ts-ignore
        window.__testHelpers.setActiveTagFilters(tagIds);
        // @ts-ignore
        window.__testHelpers.loadClips();
      }
    }, tagNames);

    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });
  }

  async clearTagFilters(): Promise<void> {
    // Clear filters via API
    await this.page.evaluate(() => {
      // @ts-ignore
      if (window.__testHelpers) {
        // @ts-ignore
        window.__testHelpers.setActiveTagFilters([]);
        // @ts-ignore
        window.__testHelpers.loadClips();
      }
    });

    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });
  }

  async expectClipHasTag(clipFilename: string, tagName: string): Promise<void> {
    const clip = await this.getClipByFilename(clipFilename);
    const tagPill = clip.locator(selectors.tags.tagPill(tagName));
    await expect(tagPill).toBeVisible();
  }

  async expectClipDoesNotHaveTag(clipFilename: string, tagName: string): Promise<void> {
    const clip = await this.getClipByFilename(clipFilename);
    const tagPill = clip.locator(selectors.tags.tagPill(tagName));
    await expect(tagPill).not.toBeVisible();
  }

  async expectTagCount(count: number): Promise<void> {
    const tags = await this.getAllTags();
    expect(tags.length).toBe(count);
  }

  async expectTagFilterActive(tagName: string): Promise<void> {
    // Check if the tag is in active filters (visible as pill in active-tags-container)
    const activeContainer = this.page.locator(selectors.tags.activeTagsContainer);
    const tagPill = activeContainer.locator(`text="${tagName}"`);
    await expect(tagPill).toBeVisible();
  }

  async bulkAddTag(tagName: string): Promise<void> {
    // Get selected clip IDs
    const selectedClipIds = await this.page.evaluate(() => {
      // @ts-ignore
      return Array.from(selectedIds || []);
    });

    if (selectedClipIds.length === 0) {
      throw new Error('No clips selected for bulk tag operation');
    }

    // Add tag to all selected clips via API
    await this.page.evaluate(async ({ clipIds, tag }) => {
      // Get tag ID by name
      // @ts-ignore
      const tags = await window.go.main.App.GetTags();
      const tagObj = tags.find((t: any) => t.name === tag);
      if (!tagObj) {
        throw new Error(`Tag not found: ${tag}`);
      }

      // Bulk add tag
      // @ts-ignore
      await window.go.main.App.BulkAddTag(clipIds, tagObj.id);

      // Refresh clips via test helper
      // @ts-ignore
      if (window.__testHelpers && window.__testHelpers.loadClips) {
        // @ts-ignore
        window.__testHelpers.loadClips();
      }
    }, { clipIds: selectedClipIds, tag: tagName });

    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });
  }

  async bulkRemoveTag(tagName: string): Promise<void> {
    // Get selected clip IDs
    const selectedClipIds = await this.page.evaluate(() => {
      // @ts-ignore
      return Array.from(selectedIds || []);
    });

    if (selectedClipIds.length === 0) {
      throw new Error('No clips selected for bulk tag operation');
    }

    // Remove tag from all selected clips via API
    await this.page.evaluate(async ({ clipIds, tag }) => {
      // Get tag ID by name
      // @ts-ignore
      const tags = await window.go.main.App.GetTags();
      const tagObj = tags.find((t: any) => t.name === tag);
      if (!tagObj) {
        throw new Error(`Tag not found: ${tag}`);
      }

      // Bulk remove tag
      // @ts-ignore
      await window.go.main.App.BulkRemoveTag(clipIds, tagObj.id);

      // Refresh clips via test helper
      // @ts-ignore
      if (window.__testHelpers && window.__testHelpers.loadClips) {
        // @ts-ignore
        window.__testHelpers.loadClips();
      }
    }, { clipIds: selectedClipIds, tag: tagName });

    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });
  }

  async deleteAllTags(): Promise<void> {
    await this.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const tags = await window.go.main.App.GetTags();
      for (const tag of tags) {
        try {
          // @ts-ignore
          await window.go.main.App.DeleteTag(tag.id);
        } catch {
          // Ignore individual delete errors
        }
      }
    });
  }

  // ==================== Folder Mode ====================

  async toggleFolderMode(): Promise<void> {
    await this.page.locator(selectors.tags.folderModeButton).click();
    await this.page.waitForFunction(() => (window as any).__appReady === true, null, { timeout: 10000 });
  }

  async expectFolderVisible(name: string): Promise<void> {
    await expect(this.page.locator(selectors.tags.folderCard(name))).toBeVisible();
  }

  async expectFolderNotVisible(name: string): Promise<void> {
    await expect(this.page.locator(selectors.tags.folderCard(name))).not.toBeVisible();
  }

  async clickFolder(name: string): Promise<void> {
    const folder = this.page.locator(selectors.tags.folderCard(name));
    await folder.waitFor({ state: 'visible', timeout: 10000 });
    await folder.click();
    await this.page.waitForFunction(() => (window as any).__appReady === true, null, { timeout: 10000 });
  }

  async setFolderMode(enabled: boolean): Promise<void> {
    await this.page.evaluate((val) => {
      // @ts-ignore
      if (window.__testHelpers?.setFolderMode) {
        // @ts-ignore
        window.__testHelpers.setFolderMode(val);
      }
    }, enabled);
  }

  // ==================== Hidden Tags ====================

  async setHiddenTags(tagNames: string[]): Promise<void> {
    await this.page.evaluate(async (names) => {
      // @ts-ignore - Wails runtime
      const allTags = await window.go.main.App.GetTags();
      const ids: number[] = [];
      for (const name of names) {
        const tag = allTags.find((t: any) => t.name === name);
        if (tag) ids.push(tag.id);
      }
      // @ts-ignore
      await window.go.main.App.SetHiddenTags(ids);
      // Update frontend state
      // @ts-ignore
      if (window.__testHelpers?.setHiddenTags) {
        // @ts-ignore
        window.__testHelpers.setHiddenTags(ids);
      }
    }, tagNames);
  }

  async getHiddenTagNames(): Promise<string[]> {
    return this.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const hiddenIds = await window.go.main.App.GetHiddenTags();
      // @ts-ignore
      const allTags = await window.go.main.App.GetTags();
      return hiddenIds
        .map((id: number) => allTags.find((t: any) => t.id === id)?.name)
        .filter(Boolean);
    });
  }

  async toggleHiddenTagInSettings(tagName: string): Promise<void> {
    // The checkbox is sr-only, so click the parent label row instead
    const row = this.page.locator(`[data-testid="hidden-tag-row-${tagName}"]`);
    await row.scrollIntoViewIfNeeded();
    await row.click();
  }

  // ==================== Plugins ====================

  async getPlugins(): Promise<Array<{ id: number; name: string; version: string; enabled: boolean; status: string }>> {
    return this.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      if (typeof window.go?.main?.PluginService?.GetPlugins !== 'function') {
        return []; // API not available (app needs rebuild)
      }
      // @ts-ignore
      return await window.go.main.PluginService.GetPlugins();
    });
  }

  async isPluginApiAvailable(): Promise<boolean> {
    return this.page.evaluate(() => {
      // @ts-ignore - Wails runtime
      return typeof window.go?.main?.PluginService?.GetPlugins === 'function';
    });
  }

  async importPlugin(pluginSource: string, filename: string): Promise<{ id: number; name: string }> {
    // Write plugin to a temp file and import via API
    // Since we can't trigger native file dialog, we'll write directly to plugins dir
    const result = await this.page.evaluate(async ({ source, fname }) => {
      // Create a Blob and trigger import via workaround
      // We need to use the backend API directly

      // First, get the data dir path
      // @ts-ignore
      const dataDir = await window.go.main.App.GetDataDir?.() || '';

      // For testing, we'll insert directly into the database and copy the file
      // This simulates what ImportPlugin does but without the file dialog

      // Parse the plugin source to extract name/version
      const nameMatch = source.match(/name\s*=\s*["']([^"']+)["']/);
      const versionMatch = source.match(/version\s*=\s*["']([^"']+)["']/);
      const name = nameMatch ? nameMatch[1] : 'Test Plugin';
      const version = versionMatch ? versionMatch[1] : '1.0.0';

      // We'll use a workaround: write to localStorage and have backend pick it up
      // Actually, let's just call the internal registration
      // For e2e testing, we expose a test helper

      // @ts-ignore - Test helper for plugin import
      if (window.__testPluginImport) {
        // @ts-ignore
        return await window.__testPluginImport(source, fname);
      }

      // Fallback: return mock data (plugin system may not be fully testable via e2e)
      return { id: 0, name, version };
    }, { source: pluginSource, fname: filename });

    return result;
  }

  async importPluginFromPath(pluginPath: string): Promise<{ id: number; name: string; version: string; enabled: boolean } | null> {
    // Import a plugin directly from a file path using the new API
    return this.page.evaluate(async (path) => {
      // @ts-ignore - Wails runtime
      if (typeof window.go?.main?.PluginService?.ImportPluginFromPath !== 'function') {
        console.error('ImportPluginFromPath not available');
        return null;
      }
      try {
        // @ts-ignore
        const result = await window.go.main.PluginService.ImportPluginFromPath(path);
        return result;
      } catch (e) {
        console.error('Failed to import plugin:', e);
        return null;
      }
    }, pluginPath);
  }

  async getPluginStorage(pluginId: number, key: string): Promise<string> {
    return this.page.evaluate(async ({ id, k }) => {
      // @ts-ignore - Wails runtime
      if (typeof window.go?.main?.PluginService?.GetPluginStorage !== 'function') {
        return '';
      }
      try {
        // @ts-ignore
        return await window.go.main.PluginService.GetPluginStorage(id, k) || '';
      } catch {
        return '';
      }
    }, { id: pluginId, k: key });
  }

  async waitForPluginStorage(pluginId: number, key: string, expectedValue: string, timeout = 5000): Promise<boolean> {
    try {
      await expect.poll(
        async () => this.getPluginStorage(pluginId, key),
        { timeout, intervals: [100, 200, 500] }
      ).toBe(expectedValue);
      return true;
    } catch {
      return false;
    }
  }

  async waitForPluginStorageContains(pluginId: number, key: string, substring: string, timeout = 5000): Promise<boolean> {
    try {
      await expect.poll(
        async () => this.getPluginStorage(pluginId, key),
        { timeout, intervals: [100, 200, 500] }
      ).toContain(substring);
      return true;
    } catch {
      return false;
    }
  }

  async enablePlugin(pluginId: number): Promise<void> {
    await this.page.evaluate(async (id) => {
      // @ts-ignore - Wails runtime
      if (typeof window.go?.main?.PluginService?.EnablePlugin !== 'function') {
        return; // API not available
      }
      // @ts-ignore
      await window.go.main.PluginService.EnablePlugin(id);
    }, pluginId);
  }

  async disablePlugin(pluginId: number): Promise<void> {
    await this.page.evaluate(async (id) => {
      // @ts-ignore - Wails runtime
      if (typeof window.go?.main?.PluginService?.DisablePlugin !== 'function') {
        return; // API not available
      }
      // @ts-ignore
      await window.go.main.PluginService.DisablePlugin(id);
    }, pluginId);
  }

  async removePlugin(pluginId: number): Promise<void> {
    await this.page.evaluate(async (id) => {
      // @ts-ignore - Wails runtime
      if (typeof window.go?.main?.PluginService?.RemovePlugin !== 'function') {
        return; // API not available
      }
      // @ts-ignore
      await window.go.main.PluginService.RemovePlugin(id);
    }, pluginId);
  }

  async getPluginPermissions(pluginId: number): Promise<Array<{ type: string; path: string }>> {
    return this.page.evaluate(async (id) => {
      // @ts-ignore - Wails runtime
      if (typeof window.go?.main?.PluginService?.GetPluginPermissions !== 'function') {
        return []; // API not available
      }
      // @ts-ignore
      return await window.go.main.PluginService.GetPluginPermissions(id);
    }, pluginId);
  }

  async deleteAllPlugins(): Promise<void> {
    await this.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      if (typeof window.go?.main?.PluginService?.GetPlugins !== 'function') {
        return; // API not available
      }
      // @ts-ignore
      const plugins = await window.go.main.PluginService.GetPlugins();
      for (const plugin of plugins) {
        try {
          // @ts-ignore
          await window.go.main.PluginService.RemovePlugin(plugin.id);
        } catch {
          // Ignore individual delete errors
        }
      }
    });
  }

  async expectPluginCount(count: number): Promise<void> {
    const plugins = await this.getPlugins();
    expect(plugins.length).toBe(count);
  }

  async expectPluginEnabled(pluginName: string): Promise<void> {
    const plugins = await this.getPlugins();
    const plugin = plugins.find(p => p.name === pluginName);
    expect(plugin).toBeDefined();
    expect(plugin?.enabled).toBe(true);
  }

  async expectPluginDisabled(pluginName: string): Promise<void> {
    const plugins = await this.getPlugins();
    const plugin = plugins.find(p => p.name === pluginName);
    expect(plugin).toBeDefined();
    expect(plugin?.enabled).toBe(false);
  }

  // ==================== Plugins UI ====================

  async openPluginsModal(): Promise<void> {
    await this.openDrawer();
    await this.page.locator(selectors.plugins.modalButton).click();
    await this.page.waitForSelector(`${selectors.plugins.modal}.opacity-100`, { timeout: 5000 });
    // Wait for plugin list to finish rendering (either list items appear or empty state becomes visible)
    await this.page.waitForFunction(() => {
      const list = document.querySelector('[data-testid="plugins-list"]');
      const emptyState = document.getElementById('plugins-empty-state');
      const hasItems = list && list.querySelectorAll(':scope > li').length > 0;
      const showsEmpty = emptyState && !emptyState.classList.contains('hidden');
      return hasItems || showsEmpty;
    }, { timeout: 5000 });
  }

  async closePluginsModal(): Promise<void> {
    await this.page.locator(selectors.plugins.closeButton).click();
    await this.page.waitForSelector(`${selectors.plugins.modal}.opacity-0`, { timeout: 5000 });
  }

  async isPluginsModalOpen(): Promise<boolean> {
    return this.page.evaluate((selector) => {
      const el = document.querySelector(selector);
      return el ? el.classList.contains('opacity-100') : false;
    }, selectors.plugins.modal);
  }

  async importPluginViaUI(): Promise<void> {
    // Note: This triggers native file dialog, may need special handling
    await this.page.locator(selectors.plugins.importButton).click();
  }

  async togglePluginViaUI(pluginId: number, enable: boolean): Promise<void> {
    // The change handler performs an async Wails call after the checkbox click.
    // Wait for the backend state so callers cannot race that update.
    const toggleLabel = this.page.locator(`[data-testid="plugin-card-${pluginId}"] [data-action="toggle-enable"]`);
    await toggleLabel.click();
    await expect.poll(async () => {
      const plugins = await this.getPlugins();
      return plugins.find(plugin => plugin.id === pluginId)?.enabled;
    }, { timeout: 5000 }).toBe(enable);
  }

  async removePluginViaUI(pluginId: number): Promise<void> {
    // First expand the plugin card to reveal the remove button
    const card = this.page.locator(`[data-testid="plugin-card-${pluginId}"]`);
    await card.locator('[data-action="toggle-expand"]').click();
    await expect(this.page.locator(`[data-testid="remove-plugin-${pluginId}"]`)).toBeVisible({ timeout: 5000 });
    await this.page.locator(selectors.plugins.pluginRemove(pluginId)).click();
    await this.confirmDialog();
  }

  async getPluginCardCount(): Promise<number> {
    const cards = this.page.locator(`${selectors.plugins.list} > li`);
    return cards.count();
  }

  async expectPluginsEmptyState(): Promise<void> {
    const emptyState = this.page.locator(selectors.plugins.emptyState);
    await expect(emptyState).toBeVisible();
  }

  async expectPluginInList(pluginName: string): Promise<void> {
    const list = this.page.locator(selectors.plugins.list);
    await expect(list.locator(`text=${pluginName}`)).toBeVisible();
  }

  // ==================== Plugin UI Extensions ====================

  async getPluginUIActions(): Promise<{ lightbox_buttons: any[]; card_actions: any[]; bulk_actions: any[]; global_actions: any[] }> {
    return this.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      if (typeof window.go?.main?.PluginService?.GetPluginUIActions !== 'function') {
        return { lightbox_buttons: [], card_actions: [], bulk_actions: [], global_actions: [] };
      }
      try {
        // @ts-ignore
        const result = await window.go.main.PluginService.GetPluginUIActions();
        return result || { lightbox_buttons: [], card_actions: [], bulk_actions: [], global_actions: [] };
      } catch {
        return { lightbox_buttons: [], card_actions: [], bulk_actions: [], global_actions: [] };
      }
    });
  }

  async executePluginActionViaAPI(
    pluginId: number,
    actionId: string,
    clipIds: number[],
    options: Record<string, any> = {},
    context: Record<string, any> = {}
  ): Promise<{ success: boolean; error?: string; result_clip_id?: number }> {
    return this.page.evaluate(async ({ pid, aid, cids, opts, ctx }) => {
      // @ts-ignore - Wails runtime
      if (typeof window.go?.main?.PluginService?.ExecutePluginAction !== 'function') {
        return { success: false, error: 'API not available' };
      }
      try {
        // @ts-ignore
        const result = await window.go.main.PluginService.ExecutePluginAction(pid, aid, cids, opts, ctx);
        return result || { success: false, error: 'No result returned' };
      } catch (e: any) {
        return { success: false, error: e.message || String(e) };
      }
    }, { pid: pluginId, aid: actionId, cids: clipIds, opts: options, ctx: context });
  }

  async openCardMenu(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    await clip.locator(selectors.clipActions.menuTrigger).click();
    await this.page.waitForSelector(selectors.cardMenu.dropdown);
  }

  async closeCardMenu(): Promise<void> {
    // Click away to close menu
    await this.page.locator('body').click({ position: { x: 10, y: 10 } });
    await this.page.locator('.card-menu-dropdown').waitFor({ state: 'hidden', timeout: 5000 });
  }

  async hoverCopySubmenu(filename: string): Promise<void> {
    await this.openCardMenu(filename);
    const trigger = this.page.locator(selectors.cardMenu.copyTrigger);
    await trigger.waitFor({ state: 'visible', timeout: 5000 });
    await trigger.hover();
    await this.page.locator(selectors.cardMenu.submenu).waitFor({ state: 'visible', timeout: 3000 });
  }

  async hoverPluginsSubmenu(filename: string): Promise<void> {
    await this.openCardMenu(filename);
    const trigger = this.page.locator(selectors.cardMenu.pluginsTrigger);
    await trigger.waitFor({ state: 'visible', timeout: 5000 });
    await trigger.hover();
    await this.page.locator(selectors.cardMenu.submenu).waitFor({ state: 'visible', timeout: 3000 });
  }

  async clickMergeDuplicatesInCardMenu(filename: string): Promise<void> {
    await this.openCardMenu(filename);
    await this.page.locator(selectors.cardMenu.mergeDuplicates).click();
  }

  async getDuplicateBadgeText(filename: string): Promise<string | null> {
    const clip = await this.getClipByFilename(filename);
    const badge = clip.locator(selectors.dedup.badge);
    if (await badge.count() === 0) return null;
    return badge.textContent();
  }

  async clickDeduplicateAll(): Promise<void> {
    await this.openMaintenanceModal();
    await this.page.locator(selectors.maintenance.deduplicateButton).waitFor({ state: 'visible', timeout: 5000 });
    await this.page.locator(selectors.maintenance.deduplicateButton).click();
  }

  async openMaintenanceModal(): Promise<void> {
    await this.openDrawer();
    await this.page.locator(selectors.maintenance.openButton).click();
    await this.page.waitForSelector(`${selectors.maintenance.modal}.opacity-100`, { timeout: 5000 });
  }

  async closeMaintenanceModal(): Promise<void> {
    await this.page.locator(selectors.maintenance.closeButton).click();
    await this.page.waitForSelector(`${selectors.maintenance.modal}.opacity-0`, { timeout: 5000 });
  }

  async clickRemoveEmptyTags(): Promise<void> {
    await this.openMaintenanceModal();
    await this.page.locator(selectors.maintenance.removeEmptyTagsButton).waitFor({ state: 'visible', timeout: 5000 });
    await this.page.locator(selectors.maintenance.removeEmptyTagsButton).click();
  }

  /**
   * Seed a stale temp file into the app's clip_temp_files directory with a
   * backdated mtime (2 hours ago) so the stale-file sweep will detect it.
   * Requires `dataDir` to be set on this AppHelper instance (it is, when the
   * app fixture creates it from the test state file).
   */
  async seedStaleTempFile(name: string, sizeBytes: number = 16): Promise<void> {
    if (!this.dataDir) throw new Error('seedStaleTempFile: dataDir not available on this AppHelper');
    const nodePath = await import('path');
    const nodeFs = await import('fs');
    const filePath = nodePath.default.join(this.dataDir, 'clip_temp_files', name);
    nodeFs.default.mkdirSync(nodePath.default.dirname(filePath), { recursive: true });
    nodeFs.default.writeFileSync(filePath, Buffer.alloc(sizeBytes));
    // Back-date mtime to 2 hours ago — well past the 60-min lease window.
    const pastMs = Date.now() - 2 * 60 * 60 * 1000;
    const pastDate = new Date(pastMs);
    nodeFs.default.utimesSync(filePath, pastDate, pastDate);
  }

  /**
   * Seed an orphan plugin_storage row into the app's SQLite DB using the
   * system sqlite3 CLI (no extra npm dependency needed).  The plugin_id value
   * intentionally has no matching row in the plugins table so the orphan-rows
   * sweep will detect and delete it.
   *
   * Requires `dataDir` to be set on this AppHelper instance (it is, when the
   * app fixture creates it from the test state file).
   */
  async seedOrphanPluginStorage(pluginId: number, key: string, value: string): Promise<void> {
    if (!this.dataDir) throw new Error('seedOrphanPluginStorage: dataDir not available on this AppHelper');
    const nodePath = await import('path');
    const nodeChildProcess = await import('child_process');
    const dbPath = nodePath.default.join(this.dataDir, 'clips.db');
    const sql = `INSERT INTO plugin_storage (plugin_id, key, value) VALUES (${pluginId}, '${key.replace(/'/g, "''")}', '${value.replace(/'/g, "''")}');`;
    nodeChildProcess.default.execSync(`sqlite3 "${dbPath}" "${sql}"`);
  }

  async clickCardMenuPluginAction(pluginId: number, actionId: string): Promise<void> {
    const actionBtn = this.page.locator(
      `.card-menu-submenu [data-action="plugin"][data-plugin-id="${pluginId}"][data-action-id="${actionId}"]`
    );
    await actionBtn.click();
  }

  async expectCardMenuPluginActionsVisible(): Promise<void> {
    const pluginActions = this.page.locator(selectors.cardMenu.pluginAction);
    await expect(pluginActions.first()).toBeVisible();
  }

  async expectCardMenuPluginActionsCount(count: number): Promise<void> {
    const pluginActions = this.page.locator(selectors.cardMenu.pluginAction);
    await expect(pluginActions).toHaveCount(count);
  }

  async expectDrawerPluginActionsVisible(): Promise<void> {
    await this.openDrawer();
    const container = this.page.locator(selectors.drawer.pluginActionsContainer);
    await expect(container).toBeVisible();
  }

  async expectDrawerPluginActionsHidden(): Promise<void> {
    await this.openDrawer();
    const container = this.page.locator(selectors.drawer.pluginActionsContainer);
    await expect(container).toBeHidden();
  }

  async expectDrawerPluginActionsCount(count: number): Promise<void> {
    await this.openDrawer();
    const actions = this.page.locator(selectors.drawer.pluginAction);
    await expect(actions).toHaveCount(count);
  }

  async clickDrawerPluginAction(pluginId: number, actionId: string): Promise<void> {
    await this.openDrawer();
    const btn = this.page.locator(
      `${selectors.drawer.pluginAction}[data-plugin-id="${pluginId}"][data-action-id="${actionId}"]`
    );
    await btn.click();
  }

  async openLightboxPluginActions(): Promise<void> {
    const container = this.page.locator(selectors.lightbox.pluginActions);
    await expect(container).toBeVisible();
  }

  async clickLightboxPluginAction(pluginId: number, actionId: string): Promise<void> {
    // Open the plugin menu first
    const trigger = this.page.locator(selectors.lightbox.pluginTrigger);
    await trigger.click();
    await this.page.locator('#lightbox-plugin-menu').waitFor({ state: 'visible', timeout: 5000 });

    // Click the menu item
    const item = this.page.locator(
      `${selectors.lightbox.pluginMenuItem}[data-plugin-id="${pluginId}"][data-action-id="${actionId}"]`
    );
    await item.click();
  }

  async expectLightboxPluginTriggerVisible(): Promise<void> {
    const trigger = this.page.locator(selectors.lightbox.pluginTrigger);
    await expect(trigger).toBeVisible();
  }

  async expectLightboxPluginMenuItemsCount(count: number): Promise<void> {
    // Open the plugin menu to count items
    const trigger = this.page.locator(selectors.lightbox.pluginTrigger);
    await trigger.click();
    await this.page.locator('#lightbox-plugin-menu').waitFor({ state: 'visible', timeout: 5000 });

    const items = this.page.locator(selectors.lightbox.pluginMenuItem);
    await expect(items).toHaveCount(count);

    // Close menu
    await this.page.keyboard.press('Escape');
  }

  async isPluginOptionsModalOpen(): Promise<boolean> {
    return this.page.evaluate((selector) => {
      const el = document.querySelector(selector);
      // Modal is open when it has opacity-100 class (not opacity-0)
      return el ? el.classList.contains('opacity-100') : false;
    }, selectors.pluginOptions.modal);
  }

  async fillPluginOptionsForm(values: Record<string, any>): Promise<void> {
    for (const [name, value] of Object.entries(values)) {
      const field = this.page.locator(`#plugin-options-form [name="${name}"]`);
      const fieldType = await field.getAttribute('type');

      if (fieldType === 'checkbox') {
        if (value) {
          await field.check();
        } else {
          await field.uncheck();
        }
      } else {
        await field.fill(String(value));
      }
    }
  }

  async submitPluginOptionsForm(): Promise<void> {
    await this.page.locator(selectors.pluginOptions.submitButton).click();
    // Wait for modal to close
    await this.page.waitForFunction((selector) => {
      const el = document.querySelector(selector);
      return !el || !el.classList.contains('active');
    }, selectors.pluginOptions.modal, { timeout: 5000 });
  }

  async cancelPluginOptionsForm(): Promise<void> {
    await this.page.locator(selectors.pluginOptions.cancelButton).click();
    await this.page.waitForFunction((selector) => {
      const el = document.querySelector(selector);
      return !el || !el.classList.contains('active');
    }, selectors.pluginOptions.modal, { timeout: 5000 });
  }

  // ==================== Plugin Review & URL Install ====================

  async isReviewModalOpen(): Promise<boolean> {
    return this.page.evaluate(() => {
      const el = document.querySelector('[data-testid="plugin-review-modal"]');
      return el ? el.classList.contains('opacity-100') : false;
    });
  }

  async approvePluginReview(): Promise<void> {
    await this.page.locator('[data-testid="plugin-review-approve"]').click();
    // Wait for modal to close
    await this.page.waitForFunction(() => {
      const el = document.querySelector('[data-testid="plugin-review-modal"]');
      return el ? el.classList.contains('opacity-0') : true;
    }, { timeout: 5000 });
  }

  async cancelPluginReview(): Promise<void> {
    await this.page.locator('[data-testid="plugin-review-cancel"]').click();
    await this.page.waitForFunction(() => {
      const el = document.querySelector('[data-testid="plugin-review-modal"]');
      return el ? el.classList.contains('opacity-0') : true;
    }, { timeout: 5000 });
  }

  async getReviewPluginName(): Promise<string> {
    return await this.page.locator('#plugin-review-name').textContent() ?? '';
  }

  async installPluginFromURL(url: string): Promise<void> {
    await this.openPluginsModal();
    await this.page.locator('[data-testid="install-url-btn"]').click();
    await this.page.locator('[data-testid="plugin-url-input"]').waitFor({ state: 'visible', timeout: 5000 });
    await this.page.locator('[data-testid="plugin-url-input"]').fill(url);
    await this.page.locator('[data-testid="plugin-url-install-btn"]').click();
  }

  async previewPluginFromURL(url: string): Promise<any> {
    return this.page.evaluate(async (u) => {
      // @ts-ignore
      return window.go.main.PluginService.PreviewPluginFromURL(u);
    }, url);
  }

  async confirmPluginInstall(source: string): Promise<any> {
    return this.page.evaluate(async (s) => {
      // @ts-ignore
      return window.go.main.PluginService.ConfirmPluginInstall(s);
    }, source);
  }

  async getUpdateCheckInterval(): Promise<string> {
    return this.page.evaluate(async () => {
      // @ts-ignore
      return window.go.main.PluginService.GetUpdateCheckInterval();
    });
  }

  async setUpdateCheckInterval(interval: string): Promise<void> {
    await this.page.evaluate(async (i) => {
      // @ts-ignore
      await window.go.main.PluginService.SetUpdateCheckInterval(i);
    }, interval);
  }

  // ==================== Backup & Restore ====================

  async openSettingsModal(): Promise<void> {
    await this.openDrawer();
    await this.page.locator(selectors.drawer.settingsButton).click();
    await this.page.waitForSelector(`${selectors.settings.modal}.opacity-100`, { timeout: 5000 });
  }

  async closeSettingsModal(): Promise<void> {
    await this.page.locator(selectors.settings.closeButton).click();
    await this.page.waitForSelector(`${selectors.settings.modal}.opacity-0`, { timeout: 5000 });
  }

  async createBackupViaAPI(): Promise<string> {
    // Create backup programmatically and return the path
    const tempDir = await this.page.evaluate(() => {
      // @ts-ignore
      return window.__testTempDir || '/tmp';
    });

    const backupPath = `${tempDir}/test-backup-${Date.now()}.zip`;

    await this.page.evaluate(async (path) => {
      // @ts-ignore
      await window.go.main.App.CreateBackup(path);
    }, backupPath);

    return backupPath;
  }

  async restoreBackupViaAPI(backupPath: string): Promise<void> {
    await this.page.evaluate(async (path) => {
      // @ts-ignore
      await window.go.main.App.ConfirmRestoreBackup(path, 'none');
    }, backupPath);

    await this.page.reload();
    await this.waitForReady();
  }

  async getBackupManifest(backupPath: string): Promise<any> {
    return this.page.evaluate(async (path) => {
      // @ts-ignore
      return await window.go.main.ValidateBackup(path);
    }, backupPath);
  }

  // ==================== Expiration ====================

  async setExpirationViaMenu(filename: string, preset: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    await clip.locator(selectors.clipActions.menuTrigger).click();
    await this.page.waitForSelector(selectors.cardMenu.dropdown);
    await this.page.locator(selectors.cardMenu.setExpiration).click();
    await this.page.waitForSelector(selectors.expiration.popover);
    await this.page.locator(selectors.expiration.popover).locator('button', { hasText: preset }).click();
  }

  async cancelExpirationViaMenu(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    await clip.locator(selectors.clipActions.menuTrigger).click();
    await this.page.waitForSelector(selectors.cardMenu.dropdown);
    await this.page.locator(selectors.cardMenu.cancelExpiration).click();
  }

  async expectClipHasExpirationBadge(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await expect(clip.locator(selectors.expiration.badge)).toBeVisible();
    await expect(clip.locator(selectors.expiration.badge)).toContainText('Temp');
  }

  async expectClipHasNoExpirationBadge(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await expect(clip.locator(selectors.expiration.badge)).not.toBeVisible();
  }

  // ==================== Keyboard Shortcuts ====================

  async pressKey(key: string, modifiers?: { meta?: boolean; ctrl?: boolean; shift?: boolean; alt?: boolean }): Promise<void> {
    const mods: string[] = [];
    if (modifiers?.meta) mods.push('Meta');
    if (modifiers?.ctrl) mods.push('Control');
    if (modifiers?.shift) mods.push('Shift');
    if (modifiers?.alt) mods.push('Alt');

    const combo = [...mods, key].join('+');
    await this.page.keyboard.press(combo);
  }

  async isCheatSheetOpen(): Promise<boolean> {
    return this.page.evaluate(() => {
      const el = document.getElementById('shortcuts-cheatsheet');
      return el ? el.classList.contains('opacity-100') : false;
    });
  }

  async openCheatSheet(): Promise<void> {
    await this.page.keyboard.press('Shift+/');
    await this.page.waitForSelector('[data-testid="shortcuts-cheatsheet"].opacity-100', { timeout: 5000 });
  }

  async closeCheatSheet(): Promise<void> {
    await this.page.keyboard.press('Escape');
    await this.page.waitForSelector('[data-testid="shortcuts-cheatsheet"].opacity-0', { timeout: 5000 });
  }

  async getFocusedClipIndex(): Promise<number> {
    return this.page.evaluate(() => {
      const focused = document.activeElement;
      if (!focused || !focused.matches('#gallery > li')) return -1;
      const gallery = document.getElementById('gallery');
      if (!gallery) return -1;
      const clips = Array.from(gallery.querySelectorAll(':scope > li'));
      return clips.indexOf(focused);
    });
  }

  async isFocusedClipVisible(): Promise<boolean> {
    return this.page.evaluate(() => {
      const focused = document.activeElement;
      return !!focused && focused.matches('#gallery > li');
    });
  }

  /** Assert that the currently focused element matches the given selector. */
  async expectFocusOn(selector: string): Promise<void> {
    await expect(this.page.locator(selector)).toBeFocused();
  }

  /**
   * Press Tab repeatedly until the focused element matches `selector`.
   * Throws after `maxTabs` presses to prevent infinite loops.
   * If `shift` is true, presses Shift+Tab instead.
   */
  async tabTo(selector: string, options?: { shift?: boolean; maxTabs?: number }): Promise<void> {
    const max = options?.maxTabs ?? 30;
    const key = options?.shift ? 'Shift+Tab' : 'Tab';
    for (let i = 0; i < max; i++) {
      await this.page.keyboard.press(key);
      const focused = await this.page.evaluate(
        (sel) => document.activeElement?.matches(sel) ?? false,
        selector
      );
      if (focused) return;
    }
    throw new Error(`Could not tab to "${selector}" within ${max} presses`);
  }

  // ==================== Metadata ====================

  async openMetadataModal(clipFilename: string): Promise<void> {
    const card = this.page.locator(selectors.gallery.clipCardByName(clipFilename));
    await card.locator(selectors.clipActions.menuTrigger).click();
    await this.page.locator(selectors.cardMenu.metadata).click();
    await expect(this.page.locator(selectors.metadata.modal)).not.toHaveClass(/pointer-events-none/);
    // Wait for metadata content to load (either rows or empty state)
    await this.page.locator(`${selectors.metadata.row}, ${selectors.metadata.emptyState}`).first().waitFor({ state: 'attached', timeout: 5000 });
  }

  async closeMetadataModal(): Promise<void> {
    await this.page.locator(selectors.metadata.closeButton).click();
    await expect(this.page.locator(selectors.metadata.modal)).toHaveClass(/pointer-events-none/);
  }

  async addMetadataField(key: string, value: string): Promise<void> {
    await this.page.locator(selectors.metadata.addButton).click();
    const rows = this.page.locator(selectors.metadata.row);
    const lastRow = rows.last();
    await lastRow.locator(selectors.metadata.keyInput).fill(key);
    await lastRow.locator(selectors.metadata.valueInput).fill(value);
  }

  async saveMetadata(): Promise<void> {
    await this.page.locator(selectors.metadata.saveButton).click();
    await expect(this.page.locator(selectors.metadata.modal)).toHaveClass(/pointer-events-none/);
  }

  async expectMetadataRow(key: string, value: string): Promise<void> {
    await expect.poll(async () => {
      const rows = this.page.locator(selectors.metadata.row);
      const count = await rows.count();
      for (let i = 0; i < count; i++) {
        const rowKey = await rows.nth(i).locator(selectors.metadata.keyInput).inputValue();
        const rowValue = await rows.nth(i).locator(selectors.metadata.valueInput).inputValue();
        if (rowKey === key && rowValue === value) {
          return true;
        }
      }
      return false;
    }, { timeout: 5000 }).toBe(true);
  }

  async expectMetadataEmpty(): Promise<void> {
    await expect(this.page.locator(selectors.metadata.emptyState)).toBeVisible();
  }

  async expectMetadataRowCount(count: number): Promise<void> {
    await expect(this.page.locator(selectors.metadata.row)).toHaveCount(count);
  }

  async deleteMetadataRow(index: number): Promise<void> {
    const rows = this.page.locator(selectors.metadata.row);
    await rows.nth(index).locator(selectors.metadata.deleteRowButton).click();
  }

  // ==================== Sort ====================

  async openSortPopover(): Promise<void> {
    // Close if already open (button toggles)
    const popover = this.page.locator(selectors.sort.popover);
    if (await popover.count() > 0) {
      await this.page.locator(selectors.sort.button).click();
      await expect(popover).toHaveCount(0);
    }
    await this.page.locator(selectors.sort.button).click();
    await this.page.waitForSelector(selectors.sort.popover);
  }

  async closeSortPopover(): Promise<void> {
    await this.page.locator('body').click({ position: { x: 0, y: 0 } });
    await expect(this.page.locator(selectors.sort.popover)).toHaveCount(0);
  }

  async selectSort(field: string): Promise<void> {
    await this.page.locator(selectors.sort.option(field)).click();
    // Popover closes and clips reload
    await expect(this.page.locator(selectors.sort.popover)).toHaveCount(0);
  }

  async getClipFilenames(): Promise<string[]> {
    const cards = this.page.locator(selectors.gallery.clipCard);
    const count = await cards.count();
    const names: string[] = [];
    for (let i = 0; i < count; i++) {
      const title = await cards.nth(i).locator('.p-2\\.5 p').getAttribute('title');
      names.push(title || '');
    }
    return names;
  }

  // ==================== System Metadata ====================

  async expectSystemMetadataVisible(): Promise<void> {
    await expect(this.page.locator(selectors.metadata.systemInfo)).toBeVisible();
  }

  async expectSystemMetadataRowCount(count: number): Promise<void> {
    await expect(this.page.locator(selectors.metadata.systemRow)).toHaveCount(count);
  }

  async getSystemMetadataValue(label: string): Promise<string> {
    const rows = this.page.locator(selectors.metadata.systemRow);
    const count = await rows.count();
    for (let i = 0; i < count; i++) {
      const rowLabel = await rows.nth(i).locator('span').first().textContent();
      if (rowLabel?.trim() === label) {
        return (await rows.nth(i).locator('span').nth(1).textContent()) || '';
      }
    }
    return '';
  }

  // ==================== Serve ====================

  async switchToServeView(): Promise<void> {
    await this.openDrawer();
    await this.page.click(selectors.viewTabs.serve);
    await this.page.waitForSelector(selectors.serve.view, { state: 'visible', timeout: 5000 });
  }

  async startServingTag(tagName: string, apiAccess: string = 'none'): Promise<{ port: number; url: string }> {
    return this.page.evaluate(async ({ name, access }: { name: string; access: string }) => {
      // @ts-ignore - Wails runtime
      const tags = await window.go.main.App.GetTags();
      const tag = tags.find((t: any) => t.name === name);
      if (!tag) throw new Error(`Tag "${name}" not found`);
      // @ts-ignore - Wails runtime
      const port = await window.go.main.ServeService.GetRandomPort();
      // @ts-ignore - Wails runtime
      const info = await window.go.main.ServeService.StartServing(tag.id, port, false, access);
      return { port: info.port, url: info.url };
    }, { name: tagName, access: apiAccess });
  }

  async stopServingTag(tagName: string): Promise<void> {
    await this.page.evaluate(async (name: string) => {
      // @ts-ignore - Wails runtime
      const tags = await window.go.main.App.GetTags();
      const tag = tags.find((t: any) => t.name === name);
      if (!tag) throw new Error(`Tag "${name}" not found`);
      // @ts-ignore - Wails runtime
      await window.go.main.ServeService.StopServing(tag.id);
    }, tagName);
  }

  async getServeStatus(): Promise<any[]> {
    return this.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const result = await window.go.main.ServeService.GetServeStatus();
      return result || [];
    });
  }

  async stopAllServers(): Promise<void> {
    const statuses = await this.getServeStatus();
    for (const s of statuses) {
      await this.page.evaluate(async (tagId: number) => {
        // @ts-ignore - Wails runtime
        await window.go.main.ServeService.StopServing(tagId);
      }, s.tag_id);
    }
  }

  // ==================== Share ====================

  /**
   * Open the share view, create a share for the given tag, close the modal,
   * and return the share string and the tag's numeric ID.
   * The tag must already exist before calling this.
   */
  async startShare(tagName: string): Promise<{ shareString: string; tagID: number }> {
    await this.openDrawer();
    await this.page.click('#view-tab-share');
    await this.page.waitForFunction(
      () => !(document.getElementById('share-view')?.classList.contains('hidden') ?? true),
      { timeout: 5000 },
    );
    // Before opening the modal, verify Go actually has this tag. If it
    // doesn't, the create-share modal will never populate the dropdown and
    // we'd wait a full 10 s for nothing. Surface a crisp error instead.
    const goTagNames: string[] = await this.page.evaluate(async () =>
      ((await (window as any).go.main.App.GetTags()) || []).map((t: any) => t.name),
    );
    if (!goTagNames.includes(tagName)) {
      throw new Error(
        `startShare("${tagName}"): tag is not in App.GetTags() — caller forgot to createTag first (found: [${goTagNames.join(', ')}])`,
      );
    }

    await this.page.click('#add-share-btn');
    // The click handler fetches tags asynchronously (App.GetTags) and then
    // populates the dropdown — selectOption races that async work and fails
    // with "did not find some options" if it wins. Wait for the option we're
    // about to pick to actually exist before selecting.
    await this.page.waitForFunction(
      (tn) => {
        const sel = document.getElementById('create-share-tag-select') as HTMLSelectElement | null;
        if (!sel) return false;
        return Array.from(sel.options).some((o) => o.textContent === tn);
      },
      tagName,
      { timeout: 10000 },
    );
    await this.page.selectOption('#create-share-tag-select', { label: tagName });
    await this.page.click('#create-share-confirm-btn');
    // Wait for the result section to appear (StartShare RPC completes).
    await this.page.waitForFunction(
      () => !(document.getElementById('create-share-result-section')?.classList.contains('hidden') ?? true),
      { timeout: 15000 },
    );
    const shareString = (await this.page.textContent('#create-share-string-box') || '').trim();
    await this.page.click('.create-share-close');
    const tagID = await this.page.evaluate((n) =>
      (window as any).go.main.App.GetTags().then((tags: any[]) => (tags.find((t: any) => t.name === n)?.id) || 0),
    tagName);
    return { shareString, tagID };
  }

  /**
   * Stop an active share for the given tag ID via the ShareService API directly.
   * Used for cleanup; does not interact with the UI.
   */
  async stopShare(tagID: number): Promise<void> {
    await this.page.evaluate((id) => (window as any).go.main.ShareService.StopShare(id), tagID);
  }

  /**
   * Open the follow share modal, paste the share string, fill the local tag name,
   * confirm, and wait for the modal to close.
   *
   * Note: The tag section is only revealed after the textarea receives a valid
   * share string via its `input` event. Playwright's `fill()` dispatches that event,
   * so we wait for the section to become visible before filling the tag name.
   */
  async followShare(shareString: string, localTagName: string): Promise<void> {
    await this.openDrawer();
    await this.page.click('#view-tab-share');
    await this.page.waitForFunction(
      () => !(document.getElementById('share-view')?.classList.contains('hidden') ?? true),
      { timeout: 5000 },
    );
    await this.page.click('#add-follow-btn');
    // The follow modal uses the Tailwind `hidden` class (display:none) to show/hide.
    // Wait for the class to be removed (modal opens).
    await this.page.waitForFunction(
      () => !(document.getElementById('follow-share-modal')?.classList.contains('hidden') ?? true),
      { timeout: 5000 },
    );
    // Use locator fill to reliably dispatch input event, which reveals the tag section.
    await this.page.locator('#follow-share-string').fill(shareString);
    // Explicitly dispatch the input event in case the textarea listener missed it.
    await this.page.evaluate(() => {
      const el = document.getElementById('follow-share-string');
      if (el) el.dispatchEvent(new Event('input', { bubbles: true }));
    });
    // Wait for the tag-name section to be revealed by the textarea input event handler.
    await this.page.waitForFunction(
      () => !(document.getElementById('follow-share-tag-section')?.classList.contains('hidden') ?? true),
      { timeout: 10000 },
    );
    await this.page.locator('#follow-share-local-tag').fill(localTagName);
    // Dismiss the tag autocomplete dropdown so it doesn't overlap the
    // Confirm button and intercept the click.
    await this.page.locator('#follow-share-local-tag').evaluate((el: HTMLElement) => el.blur());
    await this.page.waitForFunction(
      () => {
        const d = document.querySelector('#follow-share-tag-section [role="listbox"]');
        return !d || (d as HTMLElement).classList.contains('hidden');
      },
      { timeout: 2000 },
    );
    await this.page.click('#follow-share-confirm-btn');
    // Wait for the modal to close: it uses the Tailwind `hidden` class (display:none).
    // We poll for the class rather than using waitForSelector (which waits for visibility).
    // If the Follow RPC fails, check the error text for diagnosis.
    await this.page.waitForFunction(
      () => {
        const modal = document.getElementById('follow-share-modal');
        if (!modal) return true; // gone from DOM
        if (modal.classList.contains('hidden')) return true; // closed
        const err = document.getElementById('follow-share-error');
        if (err && !err.classList.contains('hidden') && err.textContent) {
          throw new Error('Follow failed: ' + err.textContent);
        }
        return false; // still open, no error yet
      },
      { timeout: 20000 },
    );
  }

  /**
   * Return the raw ShareStatus object from ShareService.GetShareStatus().
   * Contains `shares` (publications) and `follows` arrays.
   */
  async getShareStatus(): Promise<any> {
    return await this.page.evaluate(() => (window as any).go.main.ShareService.GetShareStatus());
  }

  // ==================== Restart (persistence tests) ====================

  /**
   * Restart the Wails instance bound to this AppHelper, preserving its data
   * directory (SQLite DB, share identity, etc.).
   *
   * After restart the page is reloaded and waitForReady() is called so the
   * caller can immediately interact with the app.
   *
   * IMPORTANT: This method is only valid when called on the primary worker
   * instance (workerIndex = testInfo.parallelIndex). It must NOT be called on
   * a secondary (spawnSecondary) instance — those use a different index space.
   *
   * Usage (persistence tests):
   *   await app.restart(testInfo.parallelIndex);
   *   const status = await app.getShareStatus();
   */
  async restart(workerIndex: number): Promise<void> {
    // Close the current page BEFORE killing wails — otherwise the websocket
    // to the vite dev server (hosted by wails dev) tears down mid-flight and
    // leaves `this.page` in a closed state that subsequent goto cannot recover.
    const context = this.page.context();
    try { await this.page.close(); } catch { /* already closed */ }
    await restartWailsInstance(workerIndex);
    this.page = await context.newPage();
    await this.page.goto(this.baseURL);
    // waitForReady times out occasionally when the page's first JS load
    // doesn't complete the full `window.load` chain within the timeout —
    // often because vite took a beat to serve assets after wails dev
    // restarted. A single page.reload() almost always recovers it.
    try {
      await this.waitForReady();
    } catch {
      await this.page.reload();
      await this.waitForReady();
    }
  }

  // ==================== Secondary Instance (two-app tests) ====================

  /**
   * Spawn a second Wails app instance for this test worker.
   * Returns an AppHelper bound to the secondary instance's page, and a cleanup()
   * function that MUST be called (ideally in a finally block) to kill the process.
   *
   * Port: BASE_PORT + 1000 + workerIndex (e.g. 35115 for worker 0), which does
   * not collide with the primary pool even at max workers.
   *
   * Usage:
   *   const secondary = await app.spawnSecondary(testInfo.parallelIndex);
   *   try {
   *     // ... test using app (publisher) and secondary.app (follower)
   *   } finally {
   *     await secondary.cleanup();
   *   }
   */
  async spawnSecondary(
    workerIndex: number,
  ): Promise<{ app: AppHelper; cleanup: () => Promise<void> }> {
    const context = this.page.context();
    const { instance, cleanup } = await spawnSecondaryInstance(workerIndex);

    const secondaryPage = await context.newPage();
    await secondaryPage.goto(instance.baseURL);

    // Wait for the secondary app to be fully ready. Use state:'attached' for
    // #gallery so a bootup that lands in a non-clips view (rare but possible)
    // doesn't hang the whole spawn on visibility.
    const waitReady = async () => {
      await secondaryPage.waitForSelector('[data-testid="gallery"], #gallery', { state: 'attached', timeout: 60000 });
      await secondaryPage.waitForFunction(
        () => typeof (window as any).go?.main?.App?.GetClips === 'function',
        { timeout: 60000 },
      );
      await secondaryPage.waitForFunction(
        () => (window as any).__appReady === true,
        { timeout: 60000 },
      );
    };
    // Same recovery pattern as restart(): a single reload routinely fixes a
    // flaky first-load where goto lands before vite finishes serving assets.
    try {
      await waitReady();
    } catch {
      await secondaryPage.reload();
      await waitReady();
    }

    const secondaryApp = new AppHelper(secondaryPage, instance.baseURL);

    const fullCleanup = async () => {
      try {
        await secondaryPage.close().catch(() => {});
      } finally {
        await cleanup();
      }
    };

    return { app: secondaryApp, cleanup: fullCleanup };
  }

  // ==================== Merge Tags ====================

  /**
   * Open the merge-tag modal for the named source tag by right-clicking its
   * row in the tag filter dropdown.  Returns when the modal is visible.
   */
  async openMergeModal(sourceName: string): Promise<void> {
    await this.openTagFilterDropdown();

    // Right-click the label element that contains the checkbox for this tag.
    const checkbox = this.page.locator(`[data-testid="tag-checkbox-${sourceName}"]`);
    await checkbox.waitFor({ state: 'visible', timeout: 5000 });
    // The contextmenu listener is on the parent <label> element.
    await checkbox.locator('..').click({ button: 'right' });

    // Wait for the context menu to appear and click "Merge into…".
    await this.page.waitForSelector('.context-menu-item, [role="menuitem"]', { timeout: 3000 })
      .catch(() => {
        // Fallback: context menu may use a different selector.
      });
    // Click the Merge into… option (contains the text).
    await this.page.getByText(/Merge into/i).first().click();

    // Wait for the modal to become visible (inert removed).
    await this.page.waitForFunction(
      () => !document.getElementById('merge-tag-modal')?.hasAttribute('inert'),
      { timeout: 5000 },
    );
  }

  /**
   * Type the destination tag name into the merge modal's input and wait for
   * the 200ms preview debounce to complete.
   */
  async enterMergeDestination(destName: string): Promise<void> {
    await this.page.fill('#merge-tag-dest-input', destName);
    // Dispatch input event to trigger debounce timer.
    await this.page.evaluate(() => {
      const el = document.getElementById('merge-tag-dest-input');
      if (el) el.dispatchEvent(new Event('input', { bubbles: true }));
    });
    // Wait for the 200ms debounce + async GetTags / PreviewMergeTag round-trip.
    await this.page.waitForTimeout(400);
  }

  /**
   * Return the text content of all `.block.text-red-500` spans inside
   * #merge-tag-preview (the blocker messages rendered by merge-tag-modal.js).
   */
  async getMergeModalBlockers(): Promise<string[]> {
    return this.page.evaluate(() => {
      const spans = document.querySelectorAll('#merge-tag-preview span.block.text-red-500');
      return Array.from(spans).map((s) => s.textContent || '');
    });
  }

  /**
   * Full merge workflow: open the modal, fill destination, confirm.
   * Returns when the modal has closed (inert re-applied).
   */
  async mergeTag(sourceName: string, destName: string): Promise<void> {
    await this.openMergeModal(sourceName);
    await this.enterMergeDestination(destName);

    // Wait for the confirm button to be enabled.
    await this.page.waitForFunction(
      () => !(document.getElementById('merge-tag-confirm') as HTMLButtonElement | null)?.disabled,
      { timeout: 5000 },
    );

    await this.page.click('#merge-tag-confirm');

    // Wait for the modal to close.
    await this.page.waitForFunction(
      () => document.getElementById('merge-tag-modal')?.hasAttribute('inert'),
      { timeout: 10000 },
    );

    // Wait for the tag:merged event to finish processing frontend state.
    await this.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });
  }

  /**
   * Assert that the named tag does NOT appear in the tag filter dropdown.
   */
  async expectTagMissing(tagName: string): Promise<void> {
    await this.openTagFilterDropdown();
    const checkbox = this.page.locator(`[data-testid="tag-checkbox-${tagName}"]`);
    await expect(checkbox).not.toBeAttached({ timeout: 3000 });
  }

  /**
   * Assert that the named tag IS present in the tag filter dropdown.
   */
  async expectTagExists(tagName: string): Promise<void> {
    await this.openTagFilterDropdown();
    const checkbox = this.page.locator(`[data-testid="tag-checkbox-${tagName}"]`);
    await expect(checkbox).toBeAttached({ timeout: 5000 });
  }

  async rightClickFolder(name: string): Promise<void> {
    await this.page.click(`[data-testid="folder-card-${name}"]`, { button: 'right' });
    await this.page.locator('.card-menu-dropdown[data-source="folder"]').waitFor({ state: 'visible' });
  }

  getFolderContextMenuItem(action: string) {
    return this.page.locator(`.card-menu-dropdown[data-source="folder"] [data-action="${action}"]`);
  }

  async expectFolderBadge(name: string, type: 'served' | 'shared' | 'served-paused' | 'shared-paused'): Promise<void> {
    const map: Record<string, string> = {
      'served':        `[data-testid="folder-card-${name}"] .folder-badge-serve`,
      'shared':        `[data-testid="folder-card-${name}"] .folder-badge-share`,
      'served-paused': `[data-testid="folder-card-${name}"] .folder-badge-paused[data-kind="serve"]`,
      'shared-paused': `[data-testid="folder-card-${name}"] .folder-badge-paused[data-kind="share"]`,
    };
    await this.page.locator(map[type]).waitFor({ state: 'visible', timeout: 5000 });
  }
}

// Custom test fixtures
type TestFixtures = {
  app: AppHelper;
  tempDir: string;
};

// Read state file to get instance info
async function getTestState(): Promise<TestState> {
  const content = await fs.readFile(STATE_FILE, 'utf-8');
  return JSON.parse(content);
}

export const test = base.extend<TestFixtures, WorkerFixtures>({
  // Worker-scoped browser context: one per worker, shared across all tests
  workerContext: [async ({ browser }, use) => {
    const context = await browser.newContext({
      viewport: { width: 1280, height: 800 },
    });
    await use(context);
    await context.close();
  }, { scope: 'worker' }],

  // Worker-scoped page: navigated once, reused across all tests in the worker
  workerPage: [async ({ workerContext }, use, testInfo) => {
    const page = await workerContext.newPage();
    const workerIndex = testInfo.parallelIndex;
    const baseURL = getBaseURL(workerIndex);

    await page.goto(baseURL);

    // Wait for full app initialization (only once per worker)
    await page.waitForSelector(selectors.header.root, { timeout: 30000 });
    await page.waitForSelector(selectors.gallery.container, { timeout: 30000 });
    await page.waitForFunction(
      () => typeof (window as any).go?.main?.App?.GetClips === 'function',
      { timeout: 30000 }
    );
    await page.waitForFunction(
      () => (window as any).__appReady === true,
      { timeout: 30000 }
    );

    await use(page);
  }, { scope: 'worker' }],

  // Override built-in page fixture to return the worker-scoped page.
  // Tests that destructure { page } will get the shared worker page, not a fresh one.
  page: async ({ workerPage }, use) => {
    await use(workerPage);
  },

  // Test-scoped app: reuses worker's page, fast reset between tests.
  //
  // Healing note: a previous test may have called AppHelper.restart(), which
  // closes the worker's page before killing the wails dev process (the page's
  // vite websocket is tied to that process). The worker-scoped `workerPage`
  // reference is frozen for the lifetime of the worker — we can't reassign
  // it — so if it's closed we build a fresh page in the same BrowserContext
  // and hand THAT to the AppHelper for this test. Next test sees the same
  // dead `workerPage`, heals again; cost is ~1 goto per subsequent test.
  app: async ({ workerPage, workerContext }, use, testInfo) => {
    const workerIndex = testInfo.parallelIndex;
    const baseURL = getBaseURL(workerIndex);

    let activePage: Page = workerPage;
    if (activePage.isClosed()) {
      activePage = await workerContext.newPage();
      await activePage.goto(baseURL);
      await activePage.waitForFunction(
        () => typeof (window as any).go?.main?.App?.GetClips === 'function',
        { timeout: 30000 },
      );
      await activePage.waitForFunction(
        () => (window as any).__appReady === true,
        { timeout: 30000 },
      );
    }

    // Resolve dataDir for this worker from the test state file so Node-side
    // helpers (e.g. seedStaleTempFile) can interact with the app's data dir.
    let dataDir = '';
    try {
      const state = await getTestState();
      const entry = state.instances.find((i) => i.workerIndex === workerIndex);
      dataDir = entry?.dataDir ?? '';
    } catch {
      // State file may not exist in some test modes; dataDir stays empty.
    }

    const app = new AppHelper(activePage, baseURL, dataDir);

    // Fast reset before test (single evaluate call, no page navigation)
    try {
      await app.fastReset();
    } catch {
      // If fast reset fails, page might be broken. Full reload as fallback.
      try {
        await activePage.goto(baseURL);
        await app.waitForReady();
      } catch {
        // Last resort: ignore and hope for the best
      }
    }

    await use(app);

    // Capture screenshot on failure. Use the CURRENT page (app.page) since
    // the test may have swapped it via restart(); the healed `activePage`
    // above is also valid because app.page starts there.
    if (testInfo.status !== testInfo.expectedStatus) {
      try {
        const screenshotPath = testInfo.outputPath('failure.png');
        const shotPage = app.page.isClosed() ? activePage : app.page;
        if (!shotPage.isClosed()) {
          await shotPage.screenshot({ path: screenshotPath });
          testInfo.attachments.push({
            name: 'screenshot',
            contentType: 'image/png',
            path: screenshotPath,
          });
        }
      } catch {
        // Ignore screenshot errors
      }
    }

    // Fast reset after test. Skip if page is closed — next test will heal.
    if (!app.page.isClosed()) {
      try {
        await app.fastReset();
      } catch {
        try {
          await app.page.goto(baseURL);
          await app.waitForReady();
        } catch {
          // Ignore
        }
      }
    }
  },

  tempDir: async ({}, use) => {
    // Create a temporary directory for the test
    const dir = await createTempDir();
    await use(dir);
    // Cleanup after test
    await cleanup(dir);
  },
});

export { expect } from '@playwright/test';
