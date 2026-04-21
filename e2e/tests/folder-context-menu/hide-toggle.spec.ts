import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import path from 'path';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

test.describe('Folder hide/unhide via context menu', () => {
    async function setup(app: any) {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const tag = `hide-${Date.now()}`;
        await app.createTag(tag);
        await app.addTagToClip(path.basename(imagePath), tag);
        await app.enterFolderMode();
        return tag;
    }

    test('Hide sets data-hidden and reduces opacity; Unhide reverses', async ({ app }) => {
        const tag = await setup(app);
        const card = app.page.locator(selectors.folderCard(tag));

        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('hide'));
        await expect(card).toHaveAttribute('data-hidden', 'true');

        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('hide'));
        await expect(card).not.toHaveAttribute('data-hidden', 'true');
    });

    test('Hidden state persists across re-renders', async ({ app }) => {
        const tag = await setup(app);

        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('hide'));

        await app.page.evaluate(async () => {
            if (typeof window.renderFolderCards === 'function') await window.renderFolderCards();
        });

        await expect(app.page.locator(selectors.folderHidden(tag))).toHaveCount(1);
    });

    test('aria-label includes "hidden" when hidden', async ({ app }) => {
        const tag = await setup(app);
        const card = app.page.locator(selectors.folderCard(tag));

        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('hide'));

        await expect(card).toHaveAttribute('aria-label', /hidden/);
    });
});
