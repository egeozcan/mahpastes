import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import path from 'path';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

test.describe('Folder context menu: Share toggle', () => {
    async function setup(app: any) {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const tag = `shr-${Date.now()}`;
        await app.createTag(tag);
        await app.addTagToClip(path.basename(imagePath), tag);
        await app.enterFolderMode();
        return tag;
    }

    test('Share jumps to Share view with the tag pre-selected', async ({ app }) => {
        const tag = await setup(app);
        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('share'));

        // Poll until share view is active (handleAction is async; click() returns before awaits complete).
        await app.page.waitForFunction(
            () => (window as any).__testHelpers?.getCurrentView?.() === 'share',
            { timeout: 5000 }
        );

        const view = await app.page.evaluate(
            () => (window as any).__testHelpers?.getCurrentView?.()
        );
        expect(view).toBe('share');

        const modal = app.page.locator('#create-share-modal');
        await expect(modal).toBeVisible();

        const selectValue = await app.page.locator('#create-share-tag-select').inputValue();
        const tagID = await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            return tags.find((t: any) => t.name === n)?.id;
        }, tag);
        expect(selectValue).toBe(String(tagID));
    });

    test('When sharing, menu flips to Stop sharing and stops on click', async ({ app }) => {
        const tag = await setup(app);
        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            await window.go.main.ShareService.StartShare(t.id);
        }, tag);
        await expect(app.page.locator(selectors.folderBadgeSharedActive(tag))).toHaveCount(1, { timeout: 5000 });

        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        const item = app.page.locator(`${selectors.folderContextMenu} [data-action="stop-share"]`);
        await expect(item).toContainText(/Stop sharing/);
        await item.click();

        await expect(app.page.locator(selectors.folderBadgeSharedActive(tag))).toHaveCount(0, { timeout: 5000 });
    });
});
