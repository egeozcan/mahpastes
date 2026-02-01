import { test as base, Page, Locator, expect } from '@playwright/test';
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

    // Wait for clip to appear
    await this.page.waitForTimeout(1000);
  }

  async uploadFiles(filePaths: string[], expiration = 0): Promise<void> {
    if (expiration > 0) {
      await this.page.selectOption(selectors.upload.expirationSelect, String(expiration));
    }

    const fileInput = this.page.locator(selectors.upload.fileInput);
    await fileInput.setInputFiles(filePaths);

    await this.page.waitForTimeout(1000);
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

    await this.page.waitForTimeout(1000);
  }

  async getClipCount(): Promise<number> {
    const clips = this.page.locator(selectors.gallery.clipCard);
    return clips.count();
  }

  async refreshClips(): Promise<void> {
    // Force a refresh of the clips list from the database
    // by reloading the page which reinitializes the clip list
    await this.page.reload();
    await this.page.waitForLoadState('domcontentloaded');
    await this.page.waitForTimeout(500);
  }

  async getClipCountFromDB(archived: boolean = false): Promise<number> {
    // Query the database directly via Wails API to get accurate count
    return this.page.evaluate(async (isArchived) => {
      // @ts-ignore - Wails runtime
      const clips = await window.go.main.App.GetClips(isArchived);
      return clips?.length || 0;
    }, archived);
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
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    await clip.locator(selectors.clipActions.delete).click();
    await this.confirmDialog();
  }

  async archiveClip(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    await clip.locator(selectors.clipActions.archive).click();
  }

  async editClip(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    await clip.locator(selectors.clipActions.edit).click();
  }

  async viewClip(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    await clip.locator(selectors.clipActions.view).click();
  }

  async copyClipPath(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    await clip.locator(selectors.clipActions.copyPath).click();
  }

  async saveClipToFile(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    await clip.locator(selectors.clipActions.save).click();
  }

  // Delete all clips (for cleanup) - uses API directly for reliability
  async deleteAllClips(): Promise<void> {
    await this.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const clips = await window.go.main.App.GetClips(false);
      // @ts-ignore
      const archivedClips = await window.go.main.App.GetClips(true);
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
    await this.page.waitForTimeout(500);
    // Refresh the page to update the UI
    await this.page.reload();
    await this.waitForReady();
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
    await this.page.locator(selectors.header.watchButton).click();
    await this.page.waitForSelector(selectors.watch.view, { state: 'visible' });
  }

  async closeWatchView(): Promise<void> {
    await this.page.locator(selectors.header.watchButton).click();
    await this.page.waitForSelector(selectors.watch.view, { state: 'hidden' });
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

    // Wait for state to settle
    await this.page.waitForTimeout(300);
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
        filterMode: opts.filterMode || 'all',
        filterPresets: opts.filterPresets || [],
        filterRegex: opts.filterRegex || '',
        processExisting: opts.processExisting || false,
        autoArchive: opts.autoArchive || false,
      });
      // @ts-ignore - Wails runtime
      await window.go.main.App.RefreshWatches();
    }, { path: folderPath, opts: options });
    // Refresh UI by toggling watch view
    await this.closeWatchView();
    await this.openWatchView();
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

  // ==================== Search & Filter ====================

  async search(query: string): Promise<void> {
    await this.page.locator(selectors.header.searchInput).fill(query);
    // Wait for search to apply
    await this.page.waitForTimeout(300);
  }

  async clearSearch(): Promise<void> {
    await this.page.locator(selectors.header.searchInput).clear();
    await this.page.waitForTimeout(300);
  }

  async toggleArchiveView(): Promise<void> {
    await this.page.locator(selectors.header.archiveButton).click();
    await this.page.waitForTimeout(300);
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

  async expectClipCount(count: number): Promise<void> {
    const clips = this.page.locator(selectors.gallery.clipCard);
    await expect(clips).toHaveCount(count);
  }

  async expectEmptyState(): Promise<void> {
    const emptyState = this.page.locator(selectors.gallery.emptyState);
    await expect(emptyState).toBeVisible();
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

export const test = base.extend<TestFixtures>({
  app: async ({ page }, use, testInfo) => {
    // Get worker index for this test
    const workerIndex = testInfo.parallelIndex;
    const baseURL = getBaseURL(workerIndex);

    const app = new AppHelper(page, baseURL);
    await app.goto();
    await app.waitForReady();

    await use(app);

    // Cleanup: delete all clips created during test
    try {
      await app.deleteAllClips();
    } catch {
      // Ignore cleanup errors
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
