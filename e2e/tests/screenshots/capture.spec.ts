import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
import { generateTestImage } from '../../helpers/test-data.js';
import * as path from 'path';
import * as fs from 'fs/promises';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Output directory for documentation screenshots
const SCREENSHOTS_DIR = path.resolve(__dirname, '../../../docs/static/img/screenshots');

// Single test that captures all screenshots in sequence to preserve app state
test('capture documentation screenshots', async ({ app, tempDir }) => {
  await fs.mkdir(SCREENSHOTS_DIR, { recursive: true });

  // === Setup: populate gallery with clips and tags ===

  const imageFixtures: Array<{ filename: string; color: [number, number, number] }> = [
    { filename: 'homepage-before.png', color: [41, 128, 185] },
    { filename: 'workflow-warning.png', color: [231, 76, 60] },
    { filename: 'plugin-permissions.png', color: [46, 204, 113] },
    { filename: 'comparison-baseline.png', color: [155, 89, 182] },
    { filename: 'comparison-updated.png', color: [241, 196, 15] },
    { filename: 'archive-reference.png', color: [52, 73, 94] },
  ];

  const imageFiles: string[] = [];
  for (const fixture of imageFixtures) {
    const imgPath = path.join(tempDir, fixture.filename);
    await fs.writeFile(imgPath, generateTestImage(640, 480, fixture.color));
    await app.uploadFile(imgPath);
    imageFiles.push(fixture.filename);
  }

  // Upload text and JSON files with deterministic names and meaningful content
  const releaseNotesPath = path.join(tempDir, 'release-notes.txt');
  const releaseNotesContent = [
    'mahpastes release notes',
    '',
    '- Header actions moved to the menu drawer',
    '- Keyboard shortcuts can now be customized',
    '- Plugin install flow supports URL sources',
  ].join('\n');
  await fs.writeFile(releaseNotesPath, releaseNotesContent);
  await app.uploadFile(releaseNotesPath);

  const apiResponsePath = path.join(tempDir, 'api-response.json');
  const apiResponseContent = JSON.stringify({
    status: 'ok',
    version: '2.1.0',
    features: ['drawer_navigation', 'shortcut_customization', 'plugin_updates'],
    updated_at: '2026-02-22T12:00:00Z',
  }, null, 2);
  await fs.writeFile(apiResponsePath, apiResponseContent);
  await app.uploadFile(apiResponsePath);

  // Create tags and assign to clips
  await app.createTag('screenshots');
  await app.createTag('design');
  await app.createTag('reference');
  await app.createTag('work');

  // Re-render tag filter dropdown so it shows the newly created tags
  await app.page.evaluate(async () => {
    // @ts-ignore
    if (typeof renderTagFilterDropdown === 'function') renderTagFilterDropdown();
  });

  await app.refreshClips();
  await app.expectClipCount(8);

  await app.addTagToClip(imageFiles[0], 'screenshots');
  await app.addTagToClip(imageFiles[1], 'design');
  await app.addTagToClip(imageFiles[2], 'reference');
  await app.addTagToClip(imageFiles[3], 'work');
  await app.addTagToClip(imageFiles[4], 'screenshots');
  await app.addTagToClip(imageFiles[5], 'design');
  await app.refreshClips();

  // === 1. Gallery screenshot ===
  await app.page.waitForTimeout(500);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'gallery.png') });

  // === 1b. Bottom bar screenshot ===
  // The bottom bar is always visible at the bottom of the page with add button, expiry dropdown, clip count
  const bottomBar = app.page.locator(selectors.bottomBar.root);
  await expect(bottomBar).toBeVisible();
  await app.page.waitForTimeout(200);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'bottom-bar.png') });

  // === 1c. Sort popover screenshot ===
  // Click the sort button in the header to open the sort options popover
  await app.page.locator(selectors.sort.button).click();
  await app.page.waitForSelector(selectors.sort.popover);
  await app.page.waitForTimeout(200);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'sort-popover.png') });
  // Close sort popover by clicking elsewhere
  await app.page.locator('body').click({ position: { x: 10, y: 10 } });
  await app.page.waitForTimeout(100);

  // === 2. Search screenshot ===
  await app.search('png');
  await app.page.waitForTimeout(300);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'search.png') });
  await app.clearSearch();

  // === 3. Card menu screenshot ===
  await app.openCardMenu(imageFiles[0]);
  await app.page.waitForTimeout(200);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'card-menu.png') });
  await app.closeCardMenu();

  // === 3b. Metadata modal screenshot ===
  // Set some metadata on the first clip via API, then open the metadata modal
  await app.page.evaluate(async (filename) => {
    // @ts-ignore
    const clips = await window.go.main.App.GetClips(false, [], [], '', '');
    const clip = clips.find((c: any) =>
      c.filename?.toLowerCase().includes(filename.replace('.png', '').toLowerCase())
    );
    if (clip) {
      // @ts-ignore
      await window.go.main.App.SetClipMetadata(clip.id, 'project', 'website-redesign');
      // @ts-ignore
      await window.go.main.App.SetClipMetadata(clip.id, 'author', 'design-team');
      // @ts-ignore
      await window.go.main.App.SetClipMetadata(clip.id, 'status', 'approved');
    }
  }, imageFiles[0]);
  // Open card menu and click metadata
  await app.openCardMenu(imageFiles[0]);
  await app.page.locator(selectors.cardMenu.metadata).click();
  await app.page.waitForSelector(`${selectors.metadata.modal}.opacity-100`);
  await app.page.waitForTimeout(300);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'metadata-modal.png') });
  // Close metadata modal
  await app.page.locator(selectors.metadata.closeButton).click();
  await app.page.waitForTimeout(200);

  // === 4. Tags filter screenshot ===
  await app.openTagFilterDropdown();
  await app.page.waitForTimeout(200);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'tags.png') });
  await app.closeTagFilterDropdown();

  // === 5. Lightbox screenshot ===
  await app.openLightbox(imageFiles[0]);
  await app.page.waitForTimeout(500);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'lightbox.png') });
  await app.closeLightbox();

  // === 6. Image editor screenshot ===
  await app.openImageEditor(imageFiles[0]);
  await app.selectTool('rectangle');
  await app.setEditorColor('#ef4444');
  await app.setBrushSize(3);
  await app.drawOnCanvas({ x: 100, y: 80 }, { x: 300, y: 200 });
  await app.selectTool('line');
  await app.setEditorColor('#3b82f6');
  await app.drawOnCanvas({ x: 350, y: 100 }, { x: 500, y: 250 });
  await app.page.waitForTimeout(300);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'image-editor.png') });
  await app.closeImageEditor();

  // === 7. Image comparison screenshot ===
  await app.selectClip(imageFiles[0]);
  await app.selectClip(imageFiles[1]);
  await app.openComparison();
  await app.setComparisonMode('slider');
  await app.setSliderPosition(50);
  await app.page.waitForTimeout(500);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'image-comparison.png') });
  await app.closeComparison();
  // Clear selection
  await app.page.evaluate(() => {
    // @ts-ignore
    if (window.selectedIds) window.selectedIds.clear();
    // @ts-ignore
    if (typeof renderGallery === 'function') renderGallery();
  });

  // === 8. Text editor screenshot ===
  await app.editClip('release-notes.txt');
  await app.page.waitForSelector(`${selectors.editor.modal}.active`);
  await expect(app.page.locator('#text-editor-view')).toBeVisible();
  await app.page.waitForTimeout(300);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'text-editor.png') });
  await app.closeImageEditor();

  // === 9. Bulk actions screenshot ===
  for (let i = 0; i < Math.min(4, imageFiles.length); i++) {
    await app.selectClip(imageFiles[i]);
  }
  await app.page.waitForTimeout(300);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'bulk-actions.png') });
  await app.page.evaluate(() => {
    // @ts-ignore
    if (window.selectedIds) window.selectedIds.clear();
    // @ts-ignore
    if (typeof renderGallery === 'function') renderGallery();
  });

  // === 10. Watch folders screenshot ===
  await app.openWatchView();
  await app.addWatchFolder(tempDir, {
    filterMode: 'presets',
    filterPresets: ['images'],
  });
  await app.page.waitForTimeout(300);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'watch-folders.png') });
  await app.closeWatchView();

  // === 10b. Serve view screenshot ===
  await app.switchToServeView();
  // Add a tag to the serve list so the view isn't empty
  await app.page.evaluate(async () => {
    // @ts-ignore - Wails runtime
    const tags = await window.go.main.App.GetTags();
    if (tags && tags.length > 0) {
      // @ts-ignore - access configuredEntries from serve.js
      if (typeof configuredEntries !== 'undefined') {
        const t = tags[0];
        configuredEntries.set(t.id, { tag_id: t.id, tag_name: t.name, bind_all: false });
        if (tags.length > 1) {
          const t2 = tags[1];
          configuredEntries.set(t2.id, { tag_id: t2.id, tag_name: t2.name, bind_all: false });
        }
      }
      // @ts-ignore
      if (typeof loadServeStatus === 'function') await loadServeStatus();
    }
  });
  await app.page.waitForTimeout(300);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'serve-view.png') });
  // Switch back to clips view
  await app.page.evaluate(() => {
    // @ts-ignore
    if (window.__testHelpers?.switchView) window.__testHelpers.switchView('clips');
  });
  await app.page.waitForTimeout(200);

  // === 10c. API settings modal screenshot ===
  await app.openDrawer();
  await app.page.locator('#open-api-btn').click();
  await app.page.waitForSelector('[data-testid="api-modal"].opacity-100', { timeout: 5000 });
  await app.page.waitForTimeout(300);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'api-settings.png') });
  // Close API modal
  await app.page.locator('#api-modal-close').click();
  await app.page.waitForTimeout(200);

  // === 11. Settings screenshot ===
  await app.openSettingsModal();
  await expect(app.page.locator('[data-testid="create-backup-btn"]')).toBeVisible();
  await app.page.waitForTimeout(300);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'settings.png') });

  // === 11b. Shortcuts settings screenshot ===
  const shortcutsSection = app.page.locator('#settings-shortcuts-section');
  await shortcutsSection.scrollIntoViewIfNeeded();
  await expect(app.page.locator('#shortcuts-settings-list .shortcut-key-badge').first()).toBeVisible();
  await app.page.waitForTimeout(200);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'shortcuts-settings.png') });
  await app.closeSettingsModal();

  // === 11c. Shortcuts cheatsheet screenshot ===
  await app.openCheatSheet();
  expect(await app.isCheatSheetOpen()).toBe(true);
  await app.page.waitForSelector('[data-testid="shortcuts-cheatsheet"].opacity-100');
  await app.page.waitForTimeout(200);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'shortcuts-cheatsheet.png') });
  await app.closeCheatSheet();

  // === 12. Plugins screenshot ===
  const pluginPath = path.resolve(__dirname, '../../fixtures/test-plugin.lua');
  const importedPlugin = await app.importPluginFromPath(pluginPath);
  expect(importedPlugin).not.toBeNull();

  await app.openPluginsModal();
  const pluginCardCount = await app.getPluginCardCount();
  expect(pluginCardCount).toBeGreaterThan(0);
  await app.page.waitForTimeout(300);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'plugins.png') });

  // === 12b. Expanded plugin details screenshot ===
  const firstPluginCard = app.page.locator('[data-testid^="plugin-card-"]').first();
  await firstPluginCard.locator('[data-action="toggle-expand"]').click();
  await expect(firstPluginCard.locator('[data-section="details"]')).toBeVisible();
  await app.page.waitForTimeout(200);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'plugins-details.png') });
  await app.closePluginsModal();

  // === 13. Menu drawer screenshot ===
  await app.openDrawer();
  await app.page.waitForTimeout(300);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'menu-drawer.png') });
  await app.closeDrawer();

  // === 14. Deduplication screenshot ===
  // Upload a duplicate of the first image to trigger duplicate detection
  const duplicatePath = path.join(tempDir, 'homepage-before-copy.png');
  await fs.copyFile(path.join(tempDir, imageFiles[0]), duplicatePath);
  await app.uploadFile(duplicatePath);
  await app.refreshClips();
  // Wait for duplicate detection to run (checkDuplicatesExist is called during loadClips)
  await app.page.waitForTimeout(500);
  // Verify the duplicate badge appears
  await app.page.waitForSelector(selectors.dedup.badge, { timeout: 5000 });
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'deduplication.png') });

  // === 15. Archive screenshot ===
  await app.archiveClip(imageFiles[0]);
  await app.refreshClips();
  await app.archiveClip(imageFiles[1]);
  await app.toggleArchiveView();
  await app.page.waitForTimeout(500);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'archive.png') });
  await app.toggleArchiveView();
});
