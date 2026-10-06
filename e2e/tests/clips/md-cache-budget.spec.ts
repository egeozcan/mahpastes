import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

// The Markdown preview's session image cache must not outlive the preview
// image budget: an image the budget refuses to display may not stay cached
// (its object URL holding the blob alive) just because it was fetched.
test.describe('Markdown preview: session cache respects the image budget', () => {
  test('drops and revokes images the budget refused to display', async ({ app }) => {
    const count = 5;
    const names: string[] = [];
    for (let i = 0; i < count; i++) {
      const imagePath = await createTempFile(generateTestImage(4 + i, 4), 'png');
      names.push(path.basename(imagePath));
      await app.uploadFile(imagePath);
    }
    const markdown = names.map((name, i) => `![Image ${i}](${name})`).join('\n\n');
    const markdownPath = await createTempFile(markdown, 'md');
    const markdownName = path.basename(markdownPath);
    await app.uploadFile(markdownPath);
    await app.createTag('budget');
    for (const name of [...names, markdownName]) await app.addTagToClip(name, 'budget');

    await app.page.evaluate(() => {
      const w = window as any;
      const created: string[] = [];
      const revoked: string[] = [];
      w.__mdCreated = created;
      w.__mdRevoked = revoked;
      w.__mdLocalCalls = 0;
      const create = URL.createObjectURL.bind(URL);
      const revoke = URL.revokeObjectURL.bind(URL);
      URL.createObjectURL = (obj: any) => {
        const url = create(obj);
        if (w.__mdTrack) created.push(url);
        return url;
      };
      URL.revokeObjectURL = (url: string) => { revoked.push(url); revoke(url); };
      const service = w.go.main.MarkdownService;
      const original = service.GetLocalImage;
      // Each image claims a 4096x4096 decode (64 MiB): only one fits the
      // 100 MiB decoded budget, the rest are refused at display time.
      service.GetLocalImage = async (clipID: number) => {
        w.__mdLocalCalls++;
        const result = await original.call(service, clipID);
        return { ...result, width: 4096, height: 4096, decoded_size: 4096 * 4096 * 4 };
      };
      w.__mdTrack = true;
    });

    const card = app.page.locator(selectors.gallery.clipCardByName(markdownName));
    await card.locator(selectors.clipActions.view).click();
    const preview = app.page.locator(selectors.textEditor.previewContent);
    await expect(preview.locator('img')).toHaveCount(1);
    await expect(preview.locator('.markdown-image-note', { hasText: 'budget exceeded' })).toHaveCount(count - 1);
    expect(await app.page.evaluate(() => (window as any).__mdLocalCalls)).toBe(count);

    const shownURL = await preview.locator('img').getAttribute('src');
    // Every fetched blob except the displayed one is released.
    await expect.poll(() => app.page.evaluate(() => {
      const w = window as any;
      return (w.__mdCreated as string[]).filter(url => !(w.__mdRevoked as string[]).includes(url));
    })).toEqual([shownURL]);

    await app.cancelTextEditor();
  });
});
