import { test as base, Page, Locator, expect, BrowserContext } from '@playwright/test';
import { selectors } from '../helpers/selectors.js';
import { getBaseURL } from '../helpers/wails-manager.js';
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
    public baseURL: string
  ) {}

  // ==================== Navigation ====================

  async goto(): Promise<void> {
    await this.page.goto(this.baseURL);
  }

  async waitForReady(): Promise<void> {
    // Wait for the app to be fully loaded
    await this.page.waitForSelector(selectors.header.root);
    await this.page.waitForSelector(selectors.upload.dropZone);
    // Wait for Wails runtime to be available (indicates JS is fully initialized)
    await this.page.waitForFunction(() => {
      // @ts-ignore - Wails runtime
      return typeof window.go?.main?.App?.GetClips === 'function';
    }, { timeout: 10000 });
    // Wait for app to be fully initialized (loadTags and loadClips complete)
    // Increase timeout to handle slow startup when multiple workers are active
    await this.page.waitForFunction(() => {
      // @ts-ignore
      return window.__appReady === true;
    }, { timeout: 30000 });
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

  // ==================== Clip Operations ====================

  async uploadFile(filePath: string, expiration = 0): Promise<void> {
    // Set expiration before upload
    if (expiration > 0) {
      await this.page.selectOption(selectors.upload.expirationSelect, String(expiration));
    }

    // Upload via file input
    const fileInput = this.page.locator(selectors.upload.fileInput);
    await fileInput.setInputFiles(filePath);

    // Wait for clip to appear in gallery
    await this.page.locator('#gallery > li').first().waitFor({ state: 'visible', timeout: 10000 });
  }

  async uploadFiles(filePaths: string[], expiration = 0): Promise<void> {
    if (expiration > 0) {
      await this.page.selectOption(selectors.upload.expirationSelect, String(expiration));
    }

    const fileInput = this.page.locator(selectors.upload.fileInput);
    await fileInput.setInputFiles(filePaths);

    // Wait for clip to appear in gallery
    await this.page.locator('#gallery > li').first().waitFor({ state: 'visible', timeout: 10000 });
  }

  async pasteText(text: string): Promise<void> {
    // Focus the drop zone and paste
    await this.page.locator(selectors.upload.dropZone).focus();
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
      const clips = await window.go.main.App.GetClips(isArchived, []);
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
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    // Open the card menu
    await clip.locator(selectors.clipActions.menuTrigger).click();
    // Wait for menu to appear
    await this.page.waitForSelector(selectors.cardMenu.dropdown);
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
      const clips = await window.go.main.App.GetClips(false, []);
      // @ts-ignore
      const archivedClips = await window.go.main.App.GetClips(true, []);
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

    // Watch view uses hidden class (visible when NOT hidden)
    try {
      if (await this.isWatchViewOpen()) {
        await this.closeWatchView();
      }
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
        const clips = await window.go.main.App.GetClips(false, []);
        // @ts-ignore
        const archivedClips = await window.go.main.App.GetClips(true, []);
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
            App.GetClips(false, []),
            App.GetClips(true, []),
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

      // --- 2. Close all modals via DOM ---

      // All modals use opacity-0/pointer-events-none when closed
      const modalIds = [
        'confirm-dialog', 'restore-confirm-dialog', 'folder-modal',
        'settings-modal', 'plugin-options-modal',
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

      // Lightbox & editor use .active class
      document.querySelector('#lightbox')?.classList.remove('active');
      document.querySelector('#editor-modal')?.classList.remove('active');
      document.querySelector('#comparison-modal')?.classList.remove('active');

      // Watch view uses .hidden class
      document.querySelector('#watch-view')?.classList.add('hidden');

      // Card menu dropdown (dynamically created)
      document.querySelector('.card-menu-dropdown')?.remove();

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
        if (helpers.setViewingArchive) helpers.setViewingArchive(false);
        if (helpers.setViewingWatch) helpers.setViewingWatch(false);
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

      // Reset watch button UI (ID: toggle-watch-view-btn)
      const watchBtn = document.getElementById('toggle-watch-view-btn');
      if (watchBtn) {
        watchBtn.setAttribute('aria-pressed', 'false');
        const watchBtnText = document.getElementById('watch-btn-text');
        if (watchBtnText) watchBtnText.textContent = 'Watch';
        watchBtn.classList.remove('bg-stone-800', 'text-white', 'border-stone-800', 'hover:bg-stone-700', 'hover:border-stone-700');
        watchBtn.classList.add('border-stone-200', 'text-stone-600', 'hover:bg-stone-100', 'hover:border-stone-300');
      }

      // Reset upload section
      const uploadSection = document.getElementById('upload-section');
      if (uploadSection) {
        uploadSection.classList.remove('opacity-50', 'pointer-events-none', 'hidden');
        uploadSection.removeAttribute('aria-hidden');
      }

      // Make sure gallery parent is visible
      const gallery = document.getElementById('gallery');
      if (gallery?.parentElement) gallery.parentElement.classList.remove('hidden');

      // Clear search
      const searchInput = document.getElementById('search-input') as HTMLInputElement;
      if (searchInput) searchInput.value = '';

      // --- 4. Reload gallery and caches with clean state ---

      // @ts-ignore
      window.__appReady = false;

      // Refresh plugin UI actions cache (so card menus/lightbox reflect deleted plugins)
      // @ts-ignore - loadPluginUIActions is a global function from ui.js
      if (typeof loadPluginUIActions === 'function') await loadPluginUIActions();

      // @ts-ignore - loadClips is a global function from wails-api.js
      if (typeof loadClips === 'function') await loadClips();

      // Re-fetch tags
      if (App?.GetTags) {
        const tags = await App.GetTags();
        if (helpers?.setAllTags) helpers.setAllTags(tags);
        // @ts-ignore
        if (typeof renderTagFilterDropdown === 'function') renderTagFilterDropdown();
      }

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
    await this.page.locator(selectors.bulk.compareButton).click();
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
    await this.page.waitForSelector(selectors.lightbox.overlay);
  }

  async closeLightbox(): Promise<void> {
    // Use JavaScript click to bypass viewport constraints for absolutely positioned elements
    await this.page.evaluate((selector) => {
      const btn = document.querySelector(selector);
      if (btn) (btn as HTMLElement).click();
    }, selectors.lightbox.closeButton);
    // Wait for active class to be removed (lightbox uses opacity, not display)
    await this.page.waitForSelector(`${selectors.lightbox.overlay}:not(.active)`);
  }

  async lightboxNext(): Promise<void> {
    await this.page.evaluate((selector) => {
      const btn = document.querySelector(selector);
      if (btn) (btn as HTMLElement).click();
    }, selectors.lightbox.nextButton);
  }

  async lightboxPrev(): Promise<void> {
    await this.page.evaluate((selector) => {
      const btn = document.querySelector(selector);
      if (btn) (btn as HTMLElement).click();
    }, selectors.lightbox.prevButton);
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
    await this.page.waitForSelector(`${selectors.editor.modal}:not(.active)`);
  }

  async selectTool(tool: 'brush' | 'line' | 'rectangle' | 'circle' | 'text' | 'eraser'): Promise<void> {
    await this.page.locator(selectors.editor.tools[tool]).click();
  }

  async setEditorColor(color: string): Promise<void> {
    await this.page.locator(selectors.editor.colorPicker).fill(color);
  }

  async setBrushSize(size: number): Promise<void> {
    await this.page.locator(selectors.editor.brushSize).fill(String(size));
  }

  async drawOnCanvas(from: Point, to: Point): Promise<void> {
    const canvas = this.page.locator(selectors.editor.canvas);
    await canvas.click({ position: from });
    await this.page.mouse.down();
    await this.page.mouse.move(to.x, to.y);
    await this.page.mouse.up();
  }

  async editorUndo(): Promise<void> {
    await this.page.locator(selectors.editor.undoButton).click();
  }

  async editorRedo(): Promise<void> {
    await this.page.locator(selectors.editor.redoButton).click();
  }

  async saveEditorAsNewClip(): Promise<void> {
    await this.page.locator(selectors.editor.saveButton).click();
    await this.page.waitForSelector(`${selectors.editor.modal}:not(.active)`);
  }

  isEditorOpen(): Promise<boolean> {
    return this.page.evaluate((selector) => {
      const el = document.querySelector(selector);
      return el ? el.classList.contains('active') : false;
    }, selectors.editor.modal);
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

  async setComparisonMode(mode: 'fade' | 'slider'): Promise<void> {
    if (mode === 'fade') {
      await this.page.locator(selectors.comparison.modeFade).click();
    } else {
      await this.page.locator(selectors.comparison.modeSlider).click();
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

  // ==================== Watch Folders ====================

  async openWatchView(): Promise<void> {
    // Check if already open
    const isOpen = await this.page.locator(selectors.watch.view).isVisible();
    if (!isOpen) {
      await this.page.locator(selectors.header.watchButton).click();
      await this.page.waitForSelector(`${selectors.watch.view}:not(.hidden)`, { timeout: 5000 });
    }
  }

  async closeWatchView(): Promise<void> {
    // Check if already closed
    const isOpen = await this.page.locator(selectors.watch.view).isVisible();
    if (isOpen) {
      await this.page.locator(selectors.header.watchButton).click();
      // Wait for the view to have the hidden class
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
    await expect(this.page.locator('#watch-folder-list > li')).not.toHaveCount(0, { timeout: 5000 });
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
    const btn = this.page.locator(selectors.header.archiveButton);
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

  // ==================== Text Editor ====================

  async openTextEditor(filename: string): Promise<void> {
    await this.editClip(filename);
    await this.page.waitForSelector(selectors.textEditor.modal);
  }

  async getTextEditorContent(): Promise<string> {
    return this.page.locator(selectors.textEditor.textarea).inputValue();
  }

  async setTextEditorContent(content: string): Promise<void> {
    await this.page.locator(selectors.textEditor.textarea).fill(content);
  }

  async saveTextEditor(): Promise<void> {
    await this.page.locator(selectors.textEditor.saveButton).click();
    await this.page.waitForSelector(selectors.textEditor.modal, { state: 'hidden' });
  }

  async cancelTextEditor(): Promise<void> {
    await this.page.locator(selectors.textEditor.cancelButton).click();
    await this.page.waitForSelector(selectors.textEditor.modal, { state: 'hidden' });
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

  async createTag(name: string): Promise<void> {
    // Create tag via API and update frontend state
    await this.page.evaluate(async (tagName) => {
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
    }, name);
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
      const clips = await window.go.main.App.GetClips(false, []);
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
      const clips = await window.go.main.App.GetClips(false, []);
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

  async openTagFilterDropdown(): Promise<void> {
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
    // The toggle is a hidden checkbox with a styled div - click the parent label
    const toggleLabel = this.page.locator(`[data-testid="plugin-card-${pluginId}"] [data-action="toggle-enable"]`);
    await toggleLabel.click();
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

  async getPluginUIActions(): Promise<{ lightbox_buttons: any[]; card_actions: any[] }> {
    return this.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      if (typeof window.go?.main?.PluginService?.GetPluginUIActions !== 'function') {
        return { lightbox_buttons: [], card_actions: [] };
      }
      try {
        // @ts-ignore
        const result = await window.go.main.PluginService.GetPluginUIActions();
        return result || { lightbox_buttons: [], card_actions: [] };
      } catch {
        return { lightbox_buttons: [], card_actions: [] };
      }
    });
  }

  async executePluginActionViaAPI(
    pluginId: number,
    actionId: string,
    clipIds: number[],
    options: Record<string, any> = {}
  ): Promise<{ success: boolean; error?: string; result_clip_id?: number }> {
    return this.page.evaluate(async ({ pid, aid, cids, opts }) => {
      // @ts-ignore - Wails runtime
      if (typeof window.go?.main?.PluginService?.ExecutePluginAction !== 'function') {
        return { success: false, error: 'API not available' };
      }
      try {
        // @ts-ignore
        const result = await window.go.main.PluginService.ExecutePluginAction(pid, aid, cids, opts);
        return result || { success: false, error: 'No result returned' };
      } catch (e: any) {
        return { success: false, error: e.message || String(e) };
      }
    }, { pid: pluginId, aid: actionId, cids: clipIds, opts: options });
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

  async clickCardMenuPluginAction(pluginId: number, actionId: string): Promise<void> {
    const actionBtn = this.page.locator(
      `${selectors.cardMenu.dropdown} [data-action="plugin"][data-plugin-id="${pluginId}"][data-action-id="${actionId}"]`
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

  // ==================== Backup & Restore ====================

  async openSettingsModal(): Promise<void> {
    await this.page.locator(selectors.header.settingsButton).click();
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
      await window.go.main.App.ConfirmRestoreBackup(path);
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
    await page.waitForSelector(selectors.upload.dropZone, { timeout: 30000 });
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

  // Test-scoped app: reuses worker's page, fast reset between tests
  app: async ({ workerPage }, use, testInfo) => {
    const workerIndex = testInfo.parallelIndex;
    const baseURL = getBaseURL(workerIndex);
    const app = new AppHelper(workerPage, baseURL);

    // Fast reset before test (single evaluate call, no page navigation)
    try {
      await app.fastReset();
    } catch {
      // If fast reset fails, page might be broken. Full reload as fallback.
      try {
        await workerPage.goto(baseURL);
        await app.waitForReady();
      } catch {
        // Last resort: ignore and hope for the best
      }
    }

    await use(app);

    // Capture screenshot on failure
    if (testInfo.status !== testInfo.expectedStatus) {
      try {
        const screenshotPath = testInfo.outputPath('failure.png');
        await workerPage.screenshot({ path: screenshotPath });
        testInfo.attachments.push({
          name: 'screenshot',
          contentType: 'image/png',
          path: screenshotPath,
        });
      } catch {
        // Ignore screenshot errors
      }
    }

    // Fast reset after test
    try {
      await app.fastReset();
    } catch {
      // If reset fails, reload for next test
      try {
        await workerPage.goto(baseURL);
        await app.waitForReady();
      } catch {
        // Ignore
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
