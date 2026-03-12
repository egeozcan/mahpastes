import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import path from 'path';

/**
 * Helper: perform a full HTML5 drag-and-drop sequence between two elements.
 * We dispatch events in the browser context so we can create real DataTransfer objects.
 * The folder-drag module uses event delegation on #gallery and #active-tags-container,
 * so events must bubble up properly.
 */
async function dragAndDrop(page: import('@playwright/test').Page, sourceSelector: string, targetSelector: string) {
  // Wait for both elements to be visible in the DOM before dispatching events
  await page.waitForSelector(sourceSelector, { state: 'visible', timeout: 5000 });
  await page.waitForSelector(targetSelector, { state: 'visible', timeout: 5000 });

  await page.evaluate(async ({ srcSel, tgtSel }) => {
    const src = document.querySelector(srcSel) as HTMLElement;
    const tgt = document.querySelector(tgtSel) as HTMLElement;
    if (!src) throw new Error(`Source not found: ${srcSel}`);
    if (!tgt) throw new Error(`Target not found: ${tgtSel}`);

    const srcRect = src.getBoundingClientRect();
    const tgtRect = tgt.getBoundingClientRect();
    const srcX = srcRect.x + srcRect.width / 2;
    const srcY = srcRect.y + srcRect.height / 2;
    const tgtX = tgtRect.x + tgtRect.width / 2;
    const tgtY = tgtRect.y + tgtRect.height / 2;

    const dt = new DataTransfer();

    // dragstart on source
    src.dispatchEvent(new DragEvent('dragstart', {
      bubbles: true, cancelable: true, dataTransfer: dt,
      clientX: srcX, clientY: srcY,
    }));

    // Small yield for requestAnimationFrame in dragstart handler
    await new Promise(r => setTimeout(r, 50));

    // dragenter on target
    tgt.dispatchEvent(new DragEvent('dragenter', {
      bubbles: true, cancelable: true, dataTransfer: dt,
      clientX: tgtX, clientY: tgtY,
    }));

    // dragover on target (must be prevented for drop to work)
    tgt.dispatchEvent(new DragEvent('dragover', {
      bubbles: true, cancelable: true, dataTransfer: dt,
      clientX: tgtX, clientY: tgtY,
    }));

    // drop on target
    tgt.dispatchEvent(new DragEvent('drop', {
      bubbles: true, cancelable: true, dataTransfer: dt,
      clientX: tgtX, clientY: tgtY,
    }));

    // dragend on source
    src.dispatchEvent(new DragEvent('dragend', {
      bubbles: true, cancelable: true, dataTransfer: dt,
    }));

    // Wait for async drop execution (BulkAddTag/UpdateTag + loadClips/loadTags)
    await new Promise(r => setTimeout(r, 1000));
  }, { srcSel: sourceSelector, tgtSel: targetSelector });
}

test.describe('Folder Drag-and-Drop', () => {
  // ==================== Clip Drag Tests ====================

  test('single clip dragged onto folder card moves it', async ({ app }) => {
    // Setup: create tags
    await app.createTag('photos');
    await app.createTag('photos/vacation');

    // Upload 2 images and tag both with "photos"
    const img1Path = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
    const img2Path = await createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png');
    const img1Name = path.basename(img1Path);
    const img2Name = path.basename(img2Path);

    await app.uploadFile(img1Path);
    await app.uploadFile(img2Path);
    await app.addTagToClip(img1Name, 'photos');
    await app.addTagToClip(img2Name, 'photos');

    // Enter folder mode and navigate to photos
    await app.toggleFolderMode();
    await app.clickFolder('photos');

    // Should see 2 clips and the vacation folder
    await app.expectClipCount(2);
    await app.expectFolderVisible('vacation');

    // Drag one clip onto the vacation folder
    await dragAndDrop(
      app.page,
      selectors.gallery.clipCardByName(img1Name),
      selectors.tags.folderCard('vacation'),
    );

    // Wait for gallery to update
    await app.page.waitForFunction(() => (window as any).__appReady === true, null, { timeout: 10000 });

    // Clip count should drop by 1 (the dragged clip moved to vacation)
    await app.expectClipCount(1);
  });

  test('single clip dragged to home icon moves to root', async ({ app }) => {
    // Setup: create tag and upload clip
    await app.createTag('work');
    const imgPath = await createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png');
    const imgName = path.basename(imgPath);
    await app.uploadFile(imgPath);
    await app.addTagToClip(imgName, 'work');

    // Enter folder mode and navigate to work
    await app.toggleFolderMode();
    await app.clickFolder('work');

    // Should see 1 clip
    await app.expectClipCount(1);

    // Drag clip to home icon
    await dragAndDrop(
      app.page,
      selectors.gallery.clipCardByName(imgName),
      selectors.tags.homeIcon,
    );

    // Wait for gallery to update
    await app.page.waitForFunction(() => (window as any).__appReady === true, null, { timeout: 10000 });

    // Clip should be gone from this folder
    await app.expectClipCount(0);
  });

  test('multi-select drag moves all selected clips', async ({ app }) => {
    // Setup
    await app.createTag('inbox');
    await app.createTag('inbox/done');

    const img1Path = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
    const img2Path = await createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png');
    const img3Path = await createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png');
    const img1Name = path.basename(img1Path);
    const img2Name = path.basename(img2Path);
    const img3Name = path.basename(img3Path);

    await app.uploadFile(img1Path);
    await app.uploadFile(img2Path);
    await app.uploadFile(img3Path);
    await app.addTagToClip(img1Name, 'inbox');
    await app.addTagToClip(img2Name, 'inbox');
    await app.addTagToClip(img3Name, 'inbox');

    // Enter folder mode and navigate to inbox
    await app.toggleFolderMode();
    await app.clickFolder('inbox');

    // Should see 3 clips
    await app.expectClipCount(3);

    // Select 2 clips via checkbox
    await app.selectClip(img1Name);
    await app.selectClip(img2Name);

    // Drag one of the selected clips onto the done folder
    // Smart selection should pick up all selected clips
    await dragAndDrop(
      app.page,
      selectors.gallery.clipCardByName(img1Name),
      selectors.tags.folderCard('done'),
    );

    // Wait for gallery to update
    await app.page.waitForFunction(() => (window as any).__appReady === true, null, { timeout: 10000 });

    // Only the unselected clip should remain
    await app.expectClipCount(1);
  });

  test('dragging unselected clip only moves that clip', async ({ app }) => {
    // Setup
    await app.createTag('inbox');
    await app.createTag('inbox/done');

    const img1Path = await createTempFile(generateTestImage(100, 100, [255, 128, 0]), 'png');
    const img2Path = await createTempFile(generateTestImage(100, 100, [128, 0, 255]), 'png');
    const img1Name = path.basename(img1Path);
    const img2Name = path.basename(img2Path);

    await app.uploadFile(img1Path);
    await app.uploadFile(img2Path);
    await app.addTagToClip(img1Name, 'inbox');
    await app.addTagToClip(img2Name, 'inbox');

    // Enter folder mode and navigate to inbox
    await app.toggleFolderMode();
    await app.clickFolder('inbox');

    await app.expectClipCount(2);

    // Select clip 1
    await app.selectClip(img1Name);

    // Drag clip 2 (unselected) to done folder
    await dragAndDrop(
      app.page,
      selectors.gallery.clipCardByName(img2Name),
      selectors.tags.folderCard('done'),
    );

    // Wait for gallery to update
    await app.page.waitForFunction(() => (window as any).__appReady === true, null, { timeout: 10000 });

    // Only the unselected clip moved; selected clip stays
    await app.expectClipCount(1);
    await app.expectClipVisible(img1Name);
  });

  test('clip dragged to breadcrumb parent moves up', async ({ app }) => {
    // Setup: create nested tag and clip
    await app.createTag('projects');
    await app.createTag('projects/alpha');

    const imgPath = await createTempFile(generateTestImage(100, 100, [128, 128, 0]), 'png');
    const imgName = path.basename(imgPath);
    await app.uploadFile(imgPath);
    await app.addTagToClip(imgName, 'projects/alpha');

    // Enter folder mode, navigate into projects then alpha
    await app.toggleFolderMode();
    await app.clickFolder('projects');
    await app.clickFolder('alpha');

    // Should see 1 clip
    await app.expectClipCount(1);

    // Drag clip to the "projects" breadcrumb pill (which has data-drop-target attribute)
    await dragAndDrop(
      app.page,
      selectors.gallery.clipCardByName(imgName),
      selectors.tags.tagPill('projects'),
    );

    // Wait for gallery to update
    await app.page.waitForFunction(() => (window as any).__appReady === true, null, { timeout: 10000 });

    // Clip should be gone from alpha (moved up to projects)
    await app.expectClipCount(0);
  });

  // ==================== Folder Drag Tests ====================

  test('folder dragged onto another folder reparents it', async ({ app }) => {
    // Create two top-level tags
    await app.createTag('src');
    await app.createTag('dest');

    // Enter folder mode
    await app.toggleFolderMode();

    // Both folders should be visible at root
    await app.expectFolderVisible('src');
    await app.expectFolderVisible('dest');

    // Drag src onto dest
    await dragAndDrop(
      app.page,
      selectors.tags.folderCard('src'),
      selectors.tags.folderCard('dest'),
    );

    // Wait for gallery to update
    await app.page.waitForFunction(() => (window as any).__appReady === true, null, { timeout: 10000 });

    // src should disappear from root
    await app.expectFolderNotVisible('src');
    // dest should still be at root
    await app.expectFolderVisible('dest');

    // Navigate into dest to verify src is now inside
    await app.clickFolder('dest');
    await app.expectFolderVisible('src');
  });

  test('folder with descendants reparented preserves hierarchy', async ({ app }) => {
    // Create hierarchy: team/frontend
    await app.createTag('team');
    await app.createTag('team/frontend');
    await app.createTag('archive');

    // Enter folder mode
    await app.toggleFolderMode();

    // Drag team onto archive
    await dragAndDrop(
      app.page,
      selectors.tags.folderCard('team'),
      selectors.tags.folderCard('archive'),
    );

    // Wait for gallery to update
    await app.page.waitForFunction(() => (window as any).__appReady === true, null, { timeout: 10000 });

    // team should disappear from root
    await app.expectFolderNotVisible('team');

    // Navigate into archive -> team -> frontend to verify hierarchy preserved
    await app.clickFolder('archive');
    await app.expectFolderVisible('team');
    await app.clickFolder('team');
    await app.expectFolderVisible('frontend');
  });

  test('folder dragged to home icon moves to root', async ({ app }) => {
    // Create container/inner
    await app.createTag('container');
    await app.createTag('container/inner');

    // Enter folder mode and navigate to container
    await app.toggleFolderMode();
    await app.clickFolder('container');
    await app.expectFolderVisible('inner');

    // Drag inner to home icon
    await dragAndDrop(
      app.page,
      selectors.tags.folderCard('inner'),
      selectors.tags.homeIcon,
    );

    // Wait for gallery to update
    await app.page.waitForFunction(() => (window as any).__appReady === true, null, { timeout: 10000 });

    // inner should be gone from container
    await app.expectFolderNotVisible('inner');

    // Go back to root and verify inner is there
    await app.page.locator(selectors.tags.homeIcon).click();
    await app.page.waitForFunction(() => (window as any).__appReady === true, null, { timeout: 10000 });
    await app.expectFolderVisible('inner');
    await app.expectFolderVisible('container');
  });

  test('name conflict on folder reparent shows error toast', async ({ app }) => {
    // Create "shared" at root and "a/shared" (a child "shared" under "a")
    await app.createTag('shared');
    await app.createTag('a');
    await app.createTag('a/shared');

    // Enter folder mode, navigate into "a"
    await app.toggleFolderMode();
    await app.clickFolder('a');
    await app.expectFolderVisible('shared');

    // Try to drag "shared" (which is "a/shared") to home icon to make it root "shared"
    // This should conflict with the existing root "shared"
    await dragAndDrop(
      app.page,
      selectors.tags.folderCard('shared'),
      selectors.tags.homeIcon,
    );

    // Wait a bit for the error toast
    await app.page.waitForTimeout(500);

    // The folder should still be visible (move failed)
    await app.expectFolderVisible('shared');

    // An error toast should appear
    const toast = app.page.locator(selectors.toast.container);
    await expect(toast).toBeVisible({ timeout: 5000 });
  });

  // ==================== Edge Cases ====================

  test('dropping folder on itself does nothing', async ({ app }) => {
    await app.createTag('noop');

    // Enter folder mode
    await app.toggleFolderMode();
    await app.expectFolderVisible('noop');

    // Drag folder onto itself - _isValidDrop returns false so drop won't execute
    await dragAndDrop(
      app.page,
      selectors.tags.folderCard('noop'),
      selectors.tags.folderCard('noop'),
    );

    // Wait a moment
    await app.page.waitForTimeout(300);

    // Folder should still be visible at root, no change
    await app.expectFolderVisible('noop');

    // Verify the tag name hasn't changed
    const tags = await app.getAllTags();
    const noopTag = tags.find(t => t.name === 'noop');
    expect(noopTag).toBeTruthy();
  });
});
