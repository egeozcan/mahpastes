import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Markdown clips', () => {
  test('opens a filename-classified Markdown clip in rendered Preview', async ({ app }) => {
    const markdownPath = await createTempFile('# Preview Heading\n\nSource excerpt.', 'MD');
    const filename = path.basename(markdownPath);

    await app.uploadFile(markdownPath);
    const card = app.page.locator(selectors.gallery.clipCardByName(filename));
    await expect(card.locator(selectors.gallery.clipPreview)).toContainText('# Preview Heading');
    await expect(card).toContainText('MD');

    await card.locator(selectors.clipActions.view).click();
    await expect(app.page.locator(selectors.textEditor.modal)).toHaveClass(/active/);
    await expect(app.page.locator(selectors.textEditor.previewTab)).toHaveAttribute('aria-selected', 'true');
    await expect(app.page.locator(`${selectors.textEditor.previewContent} h1`)).toHaveText('Preview Heading');
    await expect(app.page.locator(selectors.textEditor.modeLabel)).toHaveText('Preview');
  });

  test('previews unsaved source and toggles with the editor shortcut', async ({ app }) => {
    const markdownPath = await createTempFile('# Saved', 'md');
    const filename = path.basename(markdownPath);
    await app.uploadFile(markdownPath);

    const card = app.page.locator(selectors.gallery.clipCardByName(filename));
    await card.locator(selectors.clipActions.view).click();
    await app.page.locator(selectors.textEditor.editTab).click();
    await expect(app.page.locator(selectors.textEditor.textarea)).toBeFocused();
    await app.page.locator(selectors.textEditor.textarea).fill('## Unsaved Preview');

    await app.page.locator(selectors.textEditor.previewTab).click();
    await expect(app.page.locator(`${selectors.textEditor.previewContent} h2`)).toHaveText('Unsaved Preview');

    await app.page.keyboard.press('ControlOrMeta+Shift+P');
    await expect(app.page.locator(selectors.textEditor.editTab)).toHaveAttribute('aria-selected', 'true');
    await expect(app.page.locator(selectors.textEditor.textarea)).toHaveValue('## Unsaved Preview');
    await app.cancelTextEditor();
  });

  test('renders sanitized GFM with stable headings and read-only tasks', async ({ app }) => {
    const markdown = [
      '<p class="fixed" id="fake" style="position:fixed" onclick="bad()">Safe HTML</p>',
      '<script>bad()</script>',
      '',
      '## Duplicate',
      '## Duplicate',
      '- [ ] Read only',
      '',
      '| A | B |',
      '|---|---|',
      '| 1 | 2 |',
    ].join('\n');
    const markdownPath = await createTempFile(markdown, 'md');
    const filename = path.basename(markdownPath);
    await app.uploadFile(markdownPath);
    await app.page.locator(selectors.gallery.clipCardByName(filename)).locator(selectors.clipActions.view).click();

    const paragraph = app.page.locator(`${selectors.textEditor.previewContent} p`, { hasText: 'Safe HTML' });
    await expect(paragraph).not.toHaveAttribute('class');
    await expect(paragraph).not.toHaveAttribute('id');
    await expect(paragraph).not.toHaveAttribute('style');
    await expect(app.page.locator(`${selectors.textEditor.previewContent} script`)).toHaveCount(0);
    await expect(app.page.locator(`${selectors.textEditor.previewContent} h2#duplicate`)).toHaveCount(1);
    await expect(app.page.locator(`${selectors.textEditor.previewContent} h2#duplicate-1`)).toHaveCount(1);
    await expect(app.page.locator(`${selectors.textEditor.previewContent} input[type="checkbox"]`)).toBeDisabled();
    await expect(app.page.locator(`${selectors.textEditor.previewContent} table`)).toBeVisible();
  });

  test('routes safe external links to the OS and blocks unsafe schemes', async ({ app }) => {
    const markdownPath = await createTempFile(
      '[Safe](https://example.com/docs) [Mail](mailto:test@example.com) [Unsafe](javascript:alert(1)) [File](file:///tmp/x)',
      'md',
    );
    const filename = path.basename(markdownPath);
    await app.uploadFile(markdownPath);
    await app.page.evaluate(() => {
      (window as any).__openedMarkdownURL = '';
      (window as any).runtime.BrowserOpenURL = (url: string) => { (window as any).__openedMarkdownURL = url; };
    });
    await app.page.locator(selectors.gallery.clipCardByName(filename)).locator(selectors.clipActions.view).click();

    await app.page.getByRole('link', { name: 'Safe' }).click();
    await expect.poll(() => app.page.evaluate(() => (window as any).__openedMarkdownURL)).toBe('https://example.com/docs');
    await expect(app.page.getByText('Unsafe', { exact: true })).not.toHaveAttribute('href');
    await expect(app.page.getByText('File', { exact: true })).not.toHaveAttribute('href');
  });

  test('enables and disables Markdown behavior when a clip is renamed', async ({ app }) => {
    const textPath = await createTempFile('# Rename Heading', 'txt');
    const originalName = path.basename(textPath);
    await app.uploadFile(textPath);

    await app.openCardMenu(originalName);
    await app.page.locator(selectors.cardMenu.rename).click();
    await app.page.locator(selectors.prompt.input).fill('renamed.md');
    await app.page.locator(selectors.prompt.saveButton).click();
    const markdownCard = app.page.locator(selectors.gallery.clipCardByName('renamed.md'));
    await markdownCard.locator(selectors.clipActions.view).click();
    await expect(app.page.locator(`${selectors.textEditor.previewContent} h1`)).toHaveText('Rename Heading');
    await app.page.locator(selectors.textEditor.cancelButton).click();

    await app.openCardMenu('renamed.md');
    await app.page.locator(selectors.cardMenu.rename).click();
    await app.page.locator(selectors.prompt.input).fill('renamed.txt');
    await app.page.locator(selectors.prompt.saveButton).click();
    const textCard = app.page.locator(selectors.gallery.clipCardByName('renamed.txt'));
    await expect(textCard).toContainText('TXT');
    await textCard.locator(selectors.clipActions.view).click();
    await expect(app.page.locator(selectors.textEditor.previewTab)).toBeHidden();
    await expect(app.page.locator(selectors.textEditor.textarea)).toBeVisible();
  });

  test('keeps invalid UTF-8 Markdown unavailable for preview and editing', async ({ app }) => {
    const markdownPath = await createTempFile(Buffer.from([0xff, 0xfe, 0x23, 0x78]), 'md');
    const filename = path.basename(markdownPath);
    await app.uploadFile(markdownPath);

    const card = app.page.locator(selectors.gallery.clipCardByName(filename));
    await card.locator(selectors.clipActions.view).click();

    await expect(app.page.locator(selectors.textEditor.previewContent)).toContainText(
      'Markdown preview unavailable—file is not valid UTF-8.',
    );
    await expect(app.page.locator(selectors.textEditor.previewTab)).toBeHidden();
    await expect(app.page.locator(selectors.textEditor.textarea)).toBeHidden();
  });

  test('resolves relative links through shared tag paths', async ({ app }) => {
    const targetPath = await createTempFile('# Target\n\n## Details\n\nReached.', 'md');
    const targetName = path.basename(targetPath);
    const sourcePath = await createTempFile(`[Open target](${targetName}#details)`, 'md');
    const sourceName = path.basename(sourcePath);
    await app.uploadFile(sourcePath);
    await app.uploadFile(targetPath);
    await app.createTag('docs');
    await app.addTagToClip(sourceName, 'docs');
    await app.addTagToClip(targetName, 'docs');

    const sourceCard = app.page.locator(selectors.gallery.clipCardByName(sourceName));
    await sourceCard.locator(selectors.clipActions.view).click();
    const link = app.page.locator(`${selectors.textEditor.previewContent} a`, { hasText: 'Open target' });
    await expect(link).toHaveAttribute('data-markdown-reference-status', 'unique');
    await link.click();

    await expect(app.page.locator(selectors.textEditor.currentFilename)).toHaveText(targetName);
    await expect(app.page.locator(`${selectors.textEditor.previewContent} h2#details`)).toHaveText('Details');
  });

  test('renders a validated local image from a shared tag path', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(8, 6), 'png');
    const imageName = path.basename(imagePath);
    const markdownPath = await createTempFile(`![Local chart](${imageName})`, 'md');
    const markdownName = path.basename(markdownPath);
    await app.uploadFile(imagePath);
    await app.uploadFile(markdownPath);
    await app.createTag('docs');
    await app.addTagToClip(imageName, 'docs');
    await app.addTagToClip(markdownName, 'docs');

    const card = app.page.locator(selectors.gallery.clipCardByName(markdownName));
    await card.locator(selectors.clipActions.view).click();
    const image = app.page.locator(`${selectors.textEditor.previewContent} img[alt="Local chart"]`);
    await expect(image).toBeVisible();
    await expect(image).toHaveAttribute('src', /^data:image\/png;base64,/);
  });

  test('blocks remote images behind explicit HTTPS controls', async ({ app }) => {
    const markdownPath = await createTempFile(
      '![Secure image](https://example.com/image.png)\n\n![Insecure image](http://example.com/image.png)',
      'md',
    );
    const filename = path.basename(markdownPath);
    await app.uploadFile(markdownPath);
    const card = app.page.locator(selectors.gallery.clipCardByName(filename));
    await card.locator(selectors.clipActions.view).click();

    const secure = app.page.locator('[data-markdown-image-placeholder]', { hasText: 'https://example.com/image.png' });
    await expect(secure.locator('a')).toHaveText('https://example.com/image.png');
    await expect(secure.getByRole('button', { name: 'Load Image' })).toBeVisible();
    const insecure = app.page.locator('[data-markdown-image-placeholder]', { hasText: 'http://example.com/image.png' });
    await expect(insecure.locator('a')).toHaveText('http://example.com/image.png');
    await expect(insecure.getByRole('button', { name: 'Load Image' })).toHaveCount(0);
    await expect(app.page.locator(`${selectors.textEditor.previewContent} img`)).toHaveCount(0);
  });

  test('loads remote images with progress and displays cache hits automatically', async ({ app }) => {
    const pngBase64 = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=';
    const markdownPath = await createTempFile(
      '![Cached](https://example.com/cached.png)\n\n![Download](https://example.com/download.png)',
      'md',
    );
    const filename = path.basename(markdownPath);
    await app.uploadFile(markdownPath);
    await app.page.evaluate((data) => {
      const service = (window as any).go.main.MarkdownService;
      service.GetCachedRemoteImage = async (url: string) => url.includes('cached')
        ? { hit: true, content_type: 'image/png', data, size: 68, width: 1, height: 1 }
        : { hit: false };
      service.LoadRemoteImage = async (requestID: string) => {
        await new Promise(resolve => setTimeout(resolve, 300));
        return { request_id: requestID, content_type: 'image/png', data, size: 68, width: 1, height: 1, cached: false };
      };
    }, pngBase64);

    const card = app.page.locator(selectors.gallery.clipCardByName(filename));
    await card.locator(selectors.clipActions.view).click();
    await expect(app.page.locator(`${selectors.textEditor.previewContent} img[alt="Cached"]`)).toBeVisible();

    const download = app.page.locator('[data-markdown-image-placeholder]', { hasText: 'download.png' });
    await download.getByRole('button', { name: 'Load Image' }).click();
    await expect(download.locator('progress')).toBeVisible();
    await expect(app.page.locator(`${selectors.textEditor.previewContent} img[alt="Download"]`)).toBeVisible();
  });

  test('opens a recovered Markdown draft in Edit mode', async ({ app }) => {
    const markdownPath = await createTempFile('# Saved', 'markdown');
    const filename = path.basename(markdownPath);
    await app.uploadFile(markdownPath);

    const card = app.page.locator(selectors.gallery.clipCardByName(filename));
    await card.locator(selectors.clipActions.view).click();
    await app.page.locator(selectors.textEditor.editTab).click();
    await app.page.locator(selectors.textEditor.textarea).fill('# Recovered');
    await expect(app.page.locator(selectors.textEditor.draftStatus)).toHaveText('Draft saved');

    await app.page.reload();
    await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });
    const reloadedCard = app.page.locator(selectors.gallery.clipCardByName(filename));
    await reloadedCard.locator(selectors.clipActions.view).click();

    await expect(app.page.locator(selectors.textEditor.editTab)).toHaveAttribute('aria-selected', 'true');
    await expect(app.page.locator(selectors.textEditor.textarea)).toHaveValue('# Recovered');
    await expect(app.page.locator(selectors.textEditor.draftStatus)).toHaveText('Recovered draft');
    await app.cancelTextEditor();
  });
});
