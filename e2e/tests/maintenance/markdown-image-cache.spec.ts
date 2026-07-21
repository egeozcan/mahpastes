import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

test.describe('Maintenance: Markdown image cache', () => {
  test('reports and clears the temporary image cache', async ({ app }) => {
    await app.openMaintenanceModal();
    await expect(app.page.locator(selectors.maintenance.markdownCacheSize)).toContainText('0 B');
    await app.page.locator(selectors.maintenance.clearMarkdownCacheButton).click();

    await expect(app.page.locator(selectors.confirm.title)).toHaveText('Clear Markdown Image Cache?');
    await expect(app.page.locator(selectors.confirm.confirmButton)).toHaveText('Clear Cache');
    await app.confirmDialog();
    await app.expectToast('Markdown image cache cleared');
  });
});
