import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
import {
  generateTestImage,
  generateTestJSON,
  generateTestText,
  createTempFile,
} from '../../helpers/test-data.js';
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

  const imageFiles: string[] = [];
  const colors: [number, number, number][] = [
    [41, 128, 185],   // blue
    [231, 76, 60],    // red
    [46, 204, 113],   // green
    [155, 89, 182],   // purple
    [241, 196, 15],   // yellow
    [52, 73, 94],     // dark slate
  ];

  for (const color of colors) {
    const imgPath = await createTempFile(generateTestImage(640, 480, color), 'png');
    await app.uploadFile(imgPath);
    imageFiles.push(path.basename(imgPath));
  }

  // Upload text and JSON files
  const textPath = await createTempFile(generateTestText('mahpastes-demo'), 'txt');
  await app.uploadFile(textPath);

  const jsonPath = await createTempFile(generateTestJSON(), 'json');
  await app.uploadFile(jsonPath);

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
  // Text and image editing share the same #editor-modal; text shows #text-editor-view
  await app.refreshClips();
  const clips = app.page.locator(selectors.gallery.clipCard);
  const clipCount = await clips.count();
  for (let i = 0; i < clipCount; i++) {
    const filename = await clips.nth(i).getAttribute('data-filename');
    if (filename && (filename.endsWith('.json') || filename.endsWith('.txt'))) {
      await app.editClip(filename);
      await app.page.waitForSelector(`${selectors.editor.modal}.active`);
      await app.page.waitForTimeout(300);
      await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'text-editor.png') });
      await app.closeImageEditor();
      break;
    }
  }

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

  // === 11. Settings screenshot ===
  await app.openSettingsModal();
  await app.page.waitForTimeout(300);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'settings.png') });
  await app.closeSettingsModal();

  // === 12. Plugins screenshot ===
  await app.openPluginsModal();
  await app.page.waitForTimeout(300);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'plugins.png') });
  await app.closePluginsModal();

  // === 13. Archive screenshot ===
  await app.archiveClip(imageFiles[0]);
  await app.refreshClips();
  await app.archiveClip(imageFiles[1]);
  await app.toggleArchiveView();
  await app.page.waitForTimeout(500);
  await app.page.screenshot({ path: path.join(SCREENSHOTS_DIR, 'archive.png') });
  await app.toggleArchiveView();
});
