import { test, expect } from '../../fixtures/test-fixtures.js';
import { createTempFile, generateTestImage } from '../../helpers/test-data.js';
import { selectors } from '../../helpers/selectors.js';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const MODAL_TEST_PLUGIN_PATH = path.resolve(__dirname, '../../test-plugins/modal-test.lua');

test.describe('Plugin Result Modal', () => {

  test.describe('API-level modal data', () => {
    test('should return modal data from plugin action', async ({ app }) => {
      const plugin = await app.importPluginFromPath(MODAL_TEST_PLUGIN_PATH);
      expect(plugin).not.toBeNull();
      await app.enablePlugin(plugin!.id);

      // Wait for plugin to load
      await app.waitForPluginStorage(plugin!.id, 'loaded', 'true');

      // Upload a clip to get a valid clip ID
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      const clips = await app.page.evaluate(async () => {
        // @ts-ignore
        return await window.go.main.App.GetClips(false, [], [], "", "");
      });
      const clipId = clips[0].id;

      const result = await app.executePluginActionViaAPI(plugin!.id, 'show_markdown', [clipId]);
      expect(result.success).toBe(true);
      expect((result as any).modal).toBeDefined();
      expect((result as any).modal.title).toBe('Markdown Result');
      expect((result as any).modal.format).toBe('markdown');
      expect((result as any).modal.content).toContain('## Test Header');
      expect((result as any).modal.copy_data).toBe('custom copy data');
      expect((result as any).modal.paste_data).toBe('custom paste data');
      expect((result as any).modal.paste_name).toBe('test-result.txt');
      expect((result as any).modal.paste_content_type).toBe('text/plain');
    });

    test('should return no modal field when action does not return modal', async ({ app }) => {
      const plugin = await app.importPluginFromPath(MODAL_TEST_PLUGIN_PATH);
      expect(plugin).not.toBeNull();
      await app.enablePlugin(plugin!.id);
      await app.waitForPluginStorage(plugin!.id, 'loaded', 'true');

      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      const clips = await app.page.evaluate(async () => {
        // @ts-ignore
        return await window.go.main.App.GetClips(false, [], [], "", "");
      });
      const clipId = clips[0].id;

      const result = await app.executePluginActionViaAPI(plugin!.id, 'show_no_modal', [clipId]);
      expect(result.success).toBe(true);
      expect((result as any).modal).toBeUndefined();
    });

    test('should return text modal data with correct fields', async ({ app }) => {
      const plugin = await app.importPluginFromPath(MODAL_TEST_PLUGIN_PATH);
      expect(plugin).not.toBeNull();
      await app.enablePlugin(plugin!.id);
      await app.waitForPluginStorage(plugin!.id, 'loaded', 'true');

      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      const clips = await app.page.evaluate(async () => {
        // @ts-ignore
        return await window.go.main.App.GetClips(false, [], [], "", "");
      });
      const clipId = clips[0].id;

      const result = await app.executePluginActionViaAPI(plugin!.id, 'show_text', [clipId]);
      expect(result.success).toBe(true);
      expect((result as any).modal).toBeDefined();
      expect((result as any).modal.title).toBe('Text Result');
      expect((result as any).modal.format).toBe('text');
      expect((result as any).modal.content).toContain('Line 1');
      expect((result as any).modal.paste_name).toBe('text-output.txt');
    });
  });

  test.describe('Async action modal', () => {
    test('should show modal via Wails event for async action', async ({ app }) => {
      const plugin = await app.importPluginFromPath(MODAL_TEST_PLUGIN_PATH);
      expect(plugin).not.toBeNull();
      await app.enablePlugin(plugin!.id);
      await app.waitForPluginStorage(plugin!.id, 'loaded', 'true');

      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      const clips = await app.page.evaluate(async () => {
        // @ts-ignore
        return await window.go.main.App.GetClips(false, [], [], "", "");
      });
      const clipId = clips[0].id;

      // Execute async action — returns immediately, modal arrives via event
      const result = await app.executePluginActionViaAPI(plugin!.id, 'async_markdown', [clipId]);
      expect(result.success).toBe(true);
      // Async actions don't return modal in the result
      expect((result as any).modal).toBeUndefined();

      // Wait for the modal to appear via the plugin:modal Wails event
      await expect(app.page.locator(selectors.pluginResultModal.modal)).toHaveClass(/opacity-100/, { timeout: 10000 });

      const title = await app.page.locator(selectors.pluginResultModal.title).textContent();
      expect(title).toBe('Async Markdown Result');

      // Verify markdown was rendered
      const hasH2 = await app.page.evaluate(() => {
        const body = document.querySelector('#plugin-result-body');
        return body?.querySelector('h2')?.textContent ?? '';
      });
      expect(hasH2).toBe('Async Header');

      // Close and verify
      await app.page.locator(selectors.pluginResultModal.closeButton).click();
      await expect(app.page.locator(selectors.pluginResultModal.modal)).not.toHaveClass(/opacity-100/);
    });

    test('should not show modal for async action without modal data', async ({ app }) => {
      const plugin = await app.importPluginFromPath(MODAL_TEST_PLUGIN_PATH);
      expect(plugin).not.toBeNull();
      await app.enablePlugin(plugin!.id);
      await app.waitForPluginStorage(plugin!.id, 'loaded', 'true');

      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      const clips = await app.page.evaluate(async () => {
        // @ts-ignore
        return await window.go.main.App.GetClips(false, [], [], "", "");
      });
      const clipId = clips[0].id;

      const result = await app.executePluginActionViaAPI(plugin!.id, 'async_no_modal', [clipId]);
      expect(result.success).toBe(true);

      // Give time for any event to arrive — modal should NOT appear
      await app.page.waitForTimeout(2000);
      const isVisible = await app.page.evaluate(() => {
        const el = document.querySelector('#plugin-result-modal');
        return el?.classList.contains('opacity-100') ?? false;
      });
      expect(isVisible).toBe(false);
    });
  });

  test.describe('Modal UI', () => {
    test('should open modal with showPluginResultModal', async ({ app }) => {
      await app.page.evaluate((data) => {
        // @ts-ignore
        showPluginResultModal(data);
      }, {
        title: 'Test Modal',
        content: '## Hello World',
        format: 'markdown',
        copy_data: 'hello',
      });

      const isVisible = await app.page.evaluate(() => {
        const el = document.querySelector('#plugin-result-modal');
        return el?.classList.contains('opacity-100') ?? false;
      });
      expect(isVisible).toBe(true);
    });

    test('should display correct title', async ({ app }) => {
      await app.page.evaluate((data) => {
        // @ts-ignore
        showPluginResultModal(data);
      }, {
        title: 'My Custom Title',
        content: 'some content',
        format: 'text',
      });

      const title = await app.page.locator(selectors.pluginResultModal.title).textContent();
      expect(title).toBe('My Custom Title');
    });

    test('should render markdown as HTML with table and heading', async ({ app }) => {
      await app.page.evaluate((data) => {
        // @ts-ignore
        showPluginResultModal(data);
      }, {
        title: 'Markdown Test',
        content: '## Test Header\n\n| Col A | Col B |\n|---|---|\n| val1 | val2 |',
        format: 'markdown',
      });

      // Check that the body contains rendered HTML elements
      const hasH2 = await app.page.evaluate(() => {
        const body = document.querySelector('#plugin-result-body');
        return body?.querySelector('h2') !== null;
      });
      expect(hasH2).toBe(true);

      const hasTable = await app.page.evaluate(() => {
        const body = document.querySelector('#plugin-result-body');
        return body?.querySelector('table') !== null;
      });
      expect(hasTable).toBe(true);
    });

    test('should render text format in pre element', async ({ app }) => {
      await app.page.evaluate((data) => {
        // @ts-ignore
        showPluginResultModal(data);
      }, {
        title: 'Text Test',
        content: 'Line 1\nLine 2\nLine 3',
        format: 'text',
      });

      const hasPre = await app.page.evaluate(() => {
        const body = document.querySelector('#plugin-result-body');
        return body?.querySelector('pre') !== null;
      });
      expect(hasPre).toBe(true);

      const preText = await app.page.evaluate(() => {
        const pre = document.querySelector('#plugin-result-body pre');
        return pre?.textContent ?? '';
      });
      expect(preText).toContain('Line 1');
      expect(preText).toContain('Line 2');
      expect(preText).toContain('Line 3');
    });

    test('should render image format with img element', async ({ app }) => {
      const imgData = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==';

      await app.page.evaluate((data) => {
        // @ts-ignore
        showPluginResultModal(data);
      }, {
        title: 'Image Test',
        content: imgData,
        format: 'image',
      });

      const hasImg = await app.page.evaluate(() => {
        const body = document.querySelector('#plugin-result-body');
        return body?.querySelector('img') !== null;
      });
      expect(hasImg).toBe(true);
    });

    test('should close modal via close button', async ({ app }) => {
      await app.page.evaluate((data) => {
        // @ts-ignore
        showPluginResultModal(data);
      }, {
        title: 'Close Test',
        content: 'will close',
        format: 'text',
      });

      // Verify open
      let isVisible = await app.page.evaluate(() => {
        const el = document.querySelector('#plugin-result-modal');
        return el?.classList.contains('opacity-100') ?? false;
      });
      expect(isVisible).toBe(true);

      // Click close
      await app.page.locator(selectors.pluginResultModal.closeButton).click();

      // Verify closed
      isVisible = await app.page.evaluate(() => {
        const el = document.querySelector('#plugin-result-modal');
        return el?.classList.contains('opacity-100') ?? false;
      });
      expect(isVisible).toBe(false);
    });

    test('should close modal via Escape key', async ({ app }) => {
      await app.page.evaluate((data) => {
        // @ts-ignore
        showPluginResultModal(data);
      }, {
        title: 'Escape Test',
        content: 'press escape',
        format: 'text',
      });

      // Verify open
      let isVisible = await app.page.evaluate(() => {
        const el = document.querySelector('#plugin-result-modal');
        return el?.classList.contains('opacity-100') ?? false;
      });
      expect(isVisible).toBe(true);

      // Press Escape
      await app.page.keyboard.press('Escape');

      // Verify closed
      isVisible = await app.page.evaluate(() => {
        const el = document.querySelector('#plugin-result-modal');
        return el?.classList.contains('opacity-100') ?? false;
      });
      expect(isVisible).toBe(false);
    });

    test('should close modal via backdrop click', async ({ app }) => {
      await app.page.evaluate((data) => {
        // @ts-ignore
        showPluginResultModal(data);
      }, {
        title: 'Backdrop Test',
        content: 'click outside',
        format: 'text',
      });

      // Verify open
      let isVisible = await app.page.evaluate(() => {
        const el = document.querySelector('#plugin-result-modal');
        return el?.classList.contains('opacity-100') ?? false;
      });
      expect(isVisible).toBe(true);

      // Click on the backdrop (the modal overlay itself, not the content)
      await app.page.evaluate(() => {
        const modal = document.querySelector('#plugin-result-modal');
        if (modal) {
          // Simulate a click directly on the backdrop element
          const event = new MouseEvent('click', { bubbles: true });
          Object.defineProperty(event, 'target', { value: modal });
          modal.dispatchEvent(event);
        }
      });

      // Verify closed
      isVisible = await app.page.evaluate(() => {
        const el = document.querySelector('#plugin-result-modal');
        return el?.classList.contains('opacity-100') ?? false;
      });
      expect(isVisible).toBe(false);
    });

    test('should copy data to clipboard via copy button', async ({ app }) => {
      // Override clipboard API to capture copied data
      await app.page.evaluate(() => {
        // @ts-ignore
        window.__clipboardData = null;
        // Override the CopyToClipboard Wails binding
        // @ts-ignore
        window.__origCopyToClipboard = window.go.main.App.CopyToClipboard;
        // @ts-ignore
        window.go.main.App.CopyToClipboard = async (text: string) => {
          // @ts-ignore
          window.__clipboardData = text;
        };
      });

      await app.page.evaluate((data) => {
        // @ts-ignore
        showPluginResultModal(data);
      }, {
        title: 'Copy Test',
        content: 'some content',
        format: 'text',
        copy_data: 'custom copy data',
      });

      // Click copy button
      await app.page.locator(selectors.pluginResultModal.copyButton).click();

      // Check that the clipboard got the copy_data value
      const copied = await app.page.evaluate(() => (window as any).__clipboardData);
      expect(copied).toBe('custom copy data');

      // Restore original
      await app.page.evaluate(() => {
        // @ts-ignore
        if (window.__origCopyToClipboard) {
          // @ts-ignore
          window.go.main.App.CopyToClipboard = window.__origCopyToClipboard;
          // @ts-ignore
          delete window.__origCopyToClipboard;
        }
      });
    });

    test('should add paste as new clip via paste button', async ({ app }) => {
      // Verify no clips initially
      const initialCount = await app.getClipCountFromDB();
      expect(initialCount).toBe(0);

      await app.page.evaluate((data) => {
        // @ts-ignore
        showPluginResultModal(data);
      }, {
        title: 'Paste Test',
        content: 'paste content',
        format: 'text',
        paste_data: 'custom paste data',
        paste_name: 'test-result.txt',
        paste_content_type: 'text/plain',
      });

      // Click paste button
      await app.page.locator(selectors.pluginResultModal.pasteButton).click();

      // Wait for the clip to be created
      await expect.poll(
        async () => app.getClipCountFromDB(),
        { timeout: 10000, intervals: [500, 1000, 2000] }
      ).toBe(1);

      // Modal should close after paste
      const isVisible = await app.page.evaluate(() => {
        const el = document.querySelector('#plugin-result-modal');
        return el?.classList.contains('opacity-100') ?? false;
      });
      expect(isVisible).toBe(false);
    });
  });
});
