import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Drag Out', () => {
  test('should show drag handle when drag-out capability is enabled', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    const clip = await app.getClipByFilename(filename);
    const caps = await app.page.evaluate(() => {
      // @ts-ignore
      return window.__testHelpers.getTransferCapabilitiesForTest();
    });

    const dragHandle = clip.locator(selectors.clipActions.dragOut);
    if (caps?.drag_out?.enabled) {
      await expect(dragHandle).toBeVisible();
    } else {
      await expect(dragHandle).toHaveCount(0);
    }
  });

  test('should hide drag handle when drag-out capability is disabled', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.page.evaluate(async () => {
      // @ts-ignore
      window.__testHelpers.setTransferCapabilitiesForTest({
        drag_out: {
          enabled: false,
          strategy: '',
          reason: 'disabled in test',
        },
        clipboard_file: true,
      });
      // @ts-ignore
      await loadClips();
    });

    const clip = await app.getClipByFilename(filename);
    await expect(clip.locator(selectors.clipActions.dragOut)).toHaveCount(0);

    await app.page.evaluate(async () => {
      // @ts-ignore
      window.__testHelpers.clearTransferCapabilitiesForTest();
      // @ts-ignore
      if (typeof initTransferCapabilities === 'function') {
        // @ts-ignore
        await initTransferCapabilities();
      }
      // @ts-ignore
      await loadClips();
    });
  });

  test('hover should arm, prepare, then become ready', async ({ app }) => {
    const caps = await app.page.evaluate(() => {
      // @ts-ignore
      return window.__testHelpers.getTransferCapabilitiesForTest();
    });
    test.skip(!caps?.drag_out?.enabled, 'Drag-out is disabled on this platform');

    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    const clip = await app.getClipByFilename(filename);
    const dragHandle = clip.locator(selectors.clipActions.dragOut);
    await expect(dragHandle).toBeVisible();

    const handle = await dragHandle.elementHandle();
    if (!handle) {
      throw new Error('drag handle not found');
    }

    const prepState = await app.page.evaluate(async (el) => {
      // Slow prep to make spinner phase observable in test.
      // @ts-ignore
      const originalPrepare = window.prepareDrag;
      if (typeof originalPrepare === 'function') {
        // @ts-ignore
        window.prepareDrag = async (...args) => {
          await new Promise((resolve) => setTimeout(resolve, 250));
          return originalPrepare(...args);
        };
      }

      el.dispatchEvent(new PointerEvent('pointerenter', { bubbles: true }));
      await new Promise((resolve) => setTimeout(resolve, 75));
      const startedPreparing = el.classList.contains('is-preparing');
      const startedArming = el.classList.contains('is-hover-arming');
      const startedReady = el.classList.contains('is-ready');
      const progressVisible = !el.querySelector('.clip-drag-icon-progress')?.classList.contains('hidden');

      await new Promise((resolve) => setTimeout(resolve, 1050));
      const switchedToPreparing = el.classList.contains('is-preparing');
      const spinnerVisible = !el.querySelector('.clip-drag-icon-spinner')?.classList.contains('hidden');

      const deadline = Date.now() + 5000;
      while (Date.now() < deadline && el.classList.contains('is-preparing')) {
        await new Promise((resolve) => setTimeout(resolve, 25));
      }

      const finishedPreparing = !el.classList.contains('is-preparing');
      // @ts-ignore
      const prepared = !!window.__testHelpers.getPreparedDragItemForTest(Number(el.dataset.id));
      const ready = el.classList.contains('is-ready');

      if (typeof originalPrepare === 'function') {
        // @ts-ignore
        window.prepareDrag = originalPrepare;
      }

      return { startedPreparing, startedArming, startedReady, progressVisible, switchedToPreparing, spinnerVisible, finishedPreparing, prepared, ready };
    }, handle);

    expect(prepState.startedPreparing).toBeFalsy();
    expect(prepState.startedArming || prepState.startedReady).toBeTruthy();
    if (prepState.startedArming) {
      expect(prepState.progressVisible).toBeTruthy();
      expect(prepState.switchedToPreparing).toBeTruthy();
      expect(prepState.spinnerVisible).toBeTruthy();
    }
    expect(prepState.finishedPreparing).toBeTruthy();
    expect(prepState.prepared).toBeTruthy();
    expect(prepState.ready).toBeTruthy();
  });

  test('prepared drag should set file transfer payload', async ({ app }) => {
    const caps = await app.page.evaluate(() => {
      // @ts-ignore
      return window.__testHelpers.getTransferCapabilitiesForTest();
    });
    test.skip(!caps?.drag_out?.enabled, 'Drag-out is disabled on this platform');

    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    const clip = await app.getClipByFilename(filename);
    const clipId = Number(await clip.getAttribute('data-id'));

    await app.page.evaluate(async (id) => {
      // @ts-ignore
      await window.__testHelpers.prepareDragForTest(id);
    }, clipId);

    const dragHandle = clip.locator(selectors.clipActions.dragOut);
    const handle = await dragHandle.elementHandle();
    if (!handle) {
      throw new Error('drag handle not found');
    }

    const result = await app.page.evaluate((el) => {
      const dt = new DataTransfer();
      const dragEvent = new DragEvent('dragstart', {
        bubbles: true,
        cancelable: true,
        dataTransfer: dt,
      });
      const allowed = el.dispatchEvent(dragEvent);
      return {
        allowed,
        uriList: dt.getData('text/uri-list'),
        downloadURL: dt.getData('DownloadURL'),
        plain: dt.getData('text/plain'),
      };
    }, handle);

    expect(result.allowed).toBe(true);
    expect(result.uriList.startsWith('file://')).toBeTruthy();
    expect(result.downloadURL).toContain(result.uriList);
    expect(result.plain).toContain('clip_temp_files');
  });

  test('dragging one selected clip should export only the dragged clip', async ({ app }) => {
    const caps = await app.page.evaluate(() => {
      // @ts-ignore
      return window.__testHelpers.getTransferCapabilitiesForTest();
    });
    test.skip(!caps?.drag_out?.enabled, 'Drag-out is disabled on this platform');

    const imagePath1 = await createTempFile(generateTestImage(120, 120, [255, 0, 0]), 'png');
    const imagePath2 = await createTempFile(generateTestImage(120, 120, [0, 0, 255]), 'png');
    const filename1 = path.basename(imagePath1);
    const filename2 = path.basename(imagePath2);

    await app.uploadFile(imagePath1);
    await app.uploadFile(imagePath2);

    await app.selectClips([filename1, filename2]);

    const firstClip = await app.getClipByFilename(filename1);
    const firstId = Number(await firstClip.getAttribute('data-id'));
    const secondClip = await app.getClipByFilename(filename2);
    const secondId = Number(await secondClip.getAttribute('data-id'));

    await app.page.evaluate(async (id) => {
      // @ts-ignore
      await window.__testHelpers.prepareDragForTest(id);
    }, firstId);

    const dragHandle = firstClip.locator(selectors.clipActions.dragOut);
    const handle = await dragHandle.elementHandle();
    if (!handle) {
      throw new Error('drag handle not found');
    }

    const result = await app.page.evaluate((el) => {
      const dt = new DataTransfer();
      const dragEvent = new DragEvent('dragstart', {
        bubbles: true,
        cancelable: true,
        dataTransfer: dt,
      });
      el.dispatchEvent(dragEvent);
      const plain = dt.getData('text/plain');
      const filename = plain.split('/').pop() || '';
      const match = filename.match(/^(\d+)(?:_|\.|$)/);
      return {
        filename,
        draggedId: match ? Number(match[1]) : -1,
      };
    }, handle);

    expect(result.draggedId).toBe(firstId);
    expect(result.draggedId).not.toBe(secondId);
  });
});
