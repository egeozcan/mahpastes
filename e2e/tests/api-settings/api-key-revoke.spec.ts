import { test, expect } from '../../fixtures/test-fixtures';
import type { AppHelper } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

/**
 * Revoking an API key from the REST API modal.
 *
 * Revocation goes through the in-app confirm dialog: native confirm() is a
 * silent no-op inside the Wails webview on macOS, which made the button dead.
 */
test.describe('API Settings - key revocation', () => {
  // The fixture's page is worker-scoped, so an open modal would leak into the
  // next test and swallow its clicks.
  test.afterEach(async ({ app }) => {
    const dialog = app.page.locator(selectors.confirm.dialog);
    if (await dialog.evaluate((el) => el.classList.contains('opacity-100'))) {
      await app.page.locator(selectors.confirm.cancelButton).click();
      await expect(dialog).toHaveClass(/opacity-0/);
    }
    await app.page.locator('#api-modal-close').click();
    await app.page.waitForSelector('[data-testid="api-modal"].opacity-0', { timeout: 5000 });
  });

  async function openApiModal(app: AppHelper) {
    await app.openDrawer();
    await app.page.locator('#open-api-btn').click();
    await app.page.waitForSelector('[data-testid="api-modal"].opacity-100', { timeout: 5000 });
  }

  /** Creates a key and returns its id, resolved from the card bearing its name. */
  async function createKey(app: AppHelper, name: string): Promise<number> {
    await app.page.locator('#api-show-create-btn').click();
    await app.page.locator('#api-key-name').fill(name);
    await app.page.locator('#api-create-key-btn').click();
    await app.page.waitForSelector('#api-key-reveal:not(.hidden)', { timeout: 5000 });
    await app.page.locator('#api-key-reveal-close').click();

    const card = app.page.locator('[data-testid^="api-key-card-"]', { hasText: name });
    await expect(card).toBeVisible();
    const testid = await card.getAttribute('data-testid');
    return parseInt(testid!.replace('api-key-card-', ''), 10);
  }

  test('revokes a key after confirming', async ({ app }) => {
    await openApiModal(app);
    const id = await createKey(app, 'revoke-me');

    await app.page.locator(`[data-testid="revoke-key-${id}"]`).click();

    const dialog = app.page.locator(selectors.confirm.dialog);
    await expect(dialog).toHaveClass(/opacity-100/);
    await expect(app.page.locator(selectors.confirm.title)).toHaveText('Revoke API Key');
    await expect(app.page.locator(selectors.confirm.message)).toContainText('revoke-me');

    await app.page.locator(selectors.confirm.confirmButton).click();

    await expect(app.page.locator(`[data-testid="api-key-card-${id}"]`)).toContainText('Revoked');
    await expect(app.page.locator(`[data-testid="revoke-key-${id}"]`)).toHaveCount(0);
  });

  test('revoked badge records when the retention clock started', async ({ app }) => {
    await openApiModal(app);
    const id = await createKey(app, 'stamped');

    await app.page.locator(`[data-testid="revoke-key-${id}"]`).click();
    await expect(app.page.locator(selectors.confirm.dialog)).toHaveClass(/opacity-100/);
    await app.page.locator(selectors.confirm.confirmButton).click();

    // revoked_at is what the cleanup sweep ages out against — a revoked key
    // without it would sit in this list forever, so assert it round-trips.
    const badge = app.page.locator(`[data-testid="api-key-card-${id}"] span[title]`);
    await expect(badge).toHaveText('Revoked');
    await expect(badge).toHaveAttribute(
      'title',
      /^Revoked \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:Z|[+-]\d{2}:\d{2}) — removed from this list 7 days after revoking$/
    );
  });

  test('leaves the key alone when the dialog is cancelled', async ({ app }) => {
    await openApiModal(app);
    const id = await createKey(app, 'keep-me');

    await app.page.locator(`[data-testid="revoke-key-${id}"]`).click();
    await expect(app.page.locator(selectors.confirm.dialog)).toHaveClass(/opacity-100/);
    await app.page.locator(selectors.confirm.cancelButton).click();
    await expect(app.page.locator(selectors.confirm.dialog)).toHaveClass(/opacity-0/);

    await expect(app.page.locator(`[data-testid="revoke-key-${id}"]`)).toBeVisible();
    await expect(app.page.locator(`[data-testid="api-key-card-${id}"]`)).not.toContainText('Revoked');
  });

  test('key names cannot break out of the revoke button attribute', async ({ app }) => {
    await openApiModal(app);
    const id = await createKey(app, 'evil" onmouseover="x');

    // The name must survive as attribute data, not as markup.
    const btn = app.page.locator(`[data-testid="revoke-key-${id}"]`);
    await expect(btn).toHaveAttribute('data-revoke-name', 'evil" onmouseover="x');
    await expect(btn).not.toHaveAttribute('onmouseover', /.*/);

    await btn.click();
    await expect(app.page.locator(selectors.confirm.message)).toContainText('evil" onmouseover="x');
  });
});
