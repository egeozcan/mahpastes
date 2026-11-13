import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

/**
 * Rich text paste: a paste that carries both text/plain and text/html keeps
 * the plain text by default, and an .html clip when Settings → Rich Text
 * Paste asks for the formatting.
 */
test.describe('Rich Text Paste', () => {
  // Same entry point the paste event uses, with both flavours it read.
  const paste = (app: any, text: string, html: string) =>
    app.page.evaluate(([t, h]: [string, string]) => (window as any).__testHelpers.handlePastedText(t, h), [text, html]);

  const choose = async (app: any, value: string) => {
    await app.openSettingsModal();
    await app.page.locator(selectors.settings.pasteHtmlSelect).selectOption(value);
    await app.closeSettingsModal();
    expect(await app.page.evaluate(() => (window as any).__testHelpers.getPasteHtmlBehavior())).toBe(value);
  };

  const clipContent = async (app: any, filenamePrefix: string): Promise<string> => {
    const clip = app.page.locator(`#gallery > li[data-filename^="${filenamePrefix}"]`);
    await expect(clip).toHaveCount(1);
    const id = Number(await clip.getAttribute('data-id'));
    return app.page.evaluate(async (clipId: number) => {
      // ClipData carries the bytes in `data`: the text itself for valid
      // UTF-8 text clips, base64 otherwise.
      const clip = await (window as any).go.main.App.GetClipData(clipId);
      if (clip.data_encoding !== 'base64') return clip.data;
      const bytes = Uint8Array.from(atob(clip.data), c => c.charCodeAt(0));
      return new TextDecoder().decode(bytes);
    }, id);
  };

  test('defaults to keeping the plain text', async ({ app }) => {
    await app.openSettingsModal();
    await expect(app.page.locator(selectors.settings.pasteHtmlSection)).toBeVisible();
    await expect(app.page.locator(selectors.settings.pasteHtmlSelect)).toHaveValue('text');
    await app.closeSettingsModal();

    await paste(app, 'Hello world', '<meta charset="utf-8"><b>Hello</b> <a href="https://example.com">world</a>');

    await app.refreshClips();
    await app.expectClipCount(1);
    await expect(app.page.locator('#gallery > li[data-filename^="pasted_html_"]')).toHaveCount(0);
    expect(await clipContent(app, 'pasted_text_')).toBe('Hello world');
  });

  test('keeps the HTML flavour as an .html clip when asked', async ({ app }) => {
    await choose(app, 'html');

    await paste(app, 'Grüße world', '<b>Grüße</b> <a href="https://example.com">world</a>');

    await app.refreshClips();
    await app.expectClipCount(1);
    const clip = app.page.locator('#gallery > li[data-filename^="pasted_html_"]');
    await expect(clip).toHaveCount(1);
    await expect(clip).toHaveAttribute('data-filename', /\.html$/);

    const content = await clipContent(app, 'pasted_html_');
    expect(content.startsWith('<!DOCTYPE html>')).toBe(true);
    expect(content).toContain('<meta charset="utf-8">');
    expect(content).toContain('<b>Grüße</b> <a href="https://example.com">world</a>');

    const contentType = await app.page.evaluate(async () => {
      const clips = await (window as any).go.main.App.GetClips(false, [], [], '', '');
      return clips[0].content_type;
    });
    expect(contentType).toBe('text/html');
  });

  test('falls back to the plain text when there is no HTML', async ({ app }) => {
    await choose(app, 'html');

    await paste(app, 'only plain', '');

    await app.refreshClips();
    await app.expectClipCount(1);
    expect(await clipContent(app, 'pasted_text_')).toBe('only plain');
  });

  test('the preference survives a reload of the stored value', async ({ app }) => {
    await choose(app, 'html');
    const stored = await app.page.evaluate(() => (window as any).go.main.App.GetSetting('paste_html_behavior'));
    expect(stored).toBe('html');
    expect(await app.page.evaluate(() => (window as any).loadPasteHtmlBehavior())).toBe('html');
  });

  test.describe('document wrapping', () => {
    const wrap = (app: any, html: string) =>
      app.page.evaluate((h: string) => (window as any).__testHelpers.pastedHtmlDocument(h), html);

    test('wraps a bare fragment in a UTF-8 document', async ({ app }) => {
      const doc = await wrap(app, '  <p>hi</p>  ');
      expect(doc).toBe('<!DOCTYPE html>\n<html>\n<head>\n<meta charset="utf-8">\n</head>\n<body>\n<p>hi</p>\n</body>\n</html>\n');
    });

    test('adds a charset to a Chromium-style document without one', async ({ app }) => {
      const doc = await wrap(app, '<html><body>\n<!--StartFragment--><i>x</i><!--EndFragment-->\n</body>\n</html>');
      expect(doc.startsWith('<!DOCTYPE html>\n<html>\n<meta charset="utf-8">')).toBe(true);
      expect(doc).toContain('<i>x</i>');
    });

    test('leaves a complete document with a charset alone', async ({ app }) => {
      const full = '<!DOCTYPE html><html><head><meta charset="utf-8"></head><body>x</body></html>';
      expect(await wrap(app, full)).toBe(full + '\n');
    });

    test('adds a charset when the document only mentions the word', async ({ app }) => {
      const meta = '<html><head><meta name="description" content="about charset"></head><body>é</body></html>';
      expect(await wrap(app, meta)).toBe(
        '<!DOCTYPE html>\n<html><head>\n<meta charset="utf-8"><meta name="description" content="about charset"></head><body>é</body></html>\n');

      const commented = await wrap(app, '<html><head><!-- <meta charset="utf-8"> --></head><body>é</body></html>');
      expect(commented).toContain('<head>\n<meta charset="utf-8"><!--');
    });

    test('leaves a document with an http-equiv charset alone', async ({ app }) => {
      const full = '<!DOCTYPE html><html><head><meta http-equiv="Content-Type" content="text/html; charset=utf-8"></head><body>x</body></html>';
      expect(await wrap(app, full)).toBe(full + '\n');

      const spaced = '<!DOCTYPE html><html><head><meta content="text/html; charset = utf-8" http-equiv="content-type"></head><body>x</body></html>';
      expect(await wrap(app, spaced)).toBe(spaced + '\n');
    });

    test('picks the flavour from the preference', async ({ app }) => {
      const flavor = (text: string, html: string, behavior: string) =>
        app.page.evaluate(([t, h, b]: string[]) => (window as any).__testHelpers.pasteFlavor(t, h, b), [text, html, behavior]);
      expect(await flavor('t', '<b>t</b>', 'text')).toBe('text');
      expect(await flavor('t', '<b>t</b>', 'html')).toBe('html');
      expect(await flavor('t', '   ', 'html')).toBe('text');
      expect(await flavor('', '<b>t</b>', 'text')).toBeNull();
      expect(await flavor('', '', 'html')).toBeNull();
    });
  });
});
