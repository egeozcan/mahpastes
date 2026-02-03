import { test, expect } from '../../fixtures/test-fixtures';

test.describe('Tag CRUD Operations', () => {
  test.afterEach(async ({ app }) => {
    // Clean up tags after each test
    await app.deleteAllTags();
  });

  test.describe('Create Tags', () => {
    test('should create a new tag', async ({ app }) => {
      await app.createTag('important');

      const tags = await app.getAllTags();
      expect(tags.length).toBe(1);
      expect(tags[0].name).toBe('important');
    });

    test('should auto-assign color to new tag', async ({ app }) => {
      await app.createTag('work');

      const tags = await app.getAllTags();
      expect(tags[0].color).toBeTruthy();
      expect(tags[0].color).toMatch(/^#[0-9A-Fa-f]{6}$/);
    });

    test('should create multiple tags with different colors', async ({ app }) => {
      await app.createTag('tag1');
      await app.createTag('tag2');
      await app.createTag('tag3');

      const tags = await app.getAllTags();
      expect(tags.length).toBe(3);

      // Colors should be different
      const colors = tags.map(t => t.color);
      const uniqueColors = new Set(colors);
      expect(uniqueColors.size).toBe(3);
    });

    test('should show tag in filter dropdown after creation', async ({ app }) => {
      await app.createTag('visible');

      // Reload page to ensure UI is updated with new tag
      await app.page.reload();
      await app.waitForReady();

      await app.openTagFilterDropdown();
      const checkbox = app.page.locator('[data-testid="tag-checkbox-visible"]');
      await expect(checkbox).toBeVisible();
      await app.closeTagFilterDropdown();
    });
  });

  test.describe('Delete Tags', () => {
    test('should delete a tag', async ({ app }) => {
      await app.createTag('temporary');
      await app.expectTagCount(1);

      await app.deleteTag('temporary');
      await app.expectTagCount(0);
    });

    test('should remove tag from filter dropdown after deletion', async ({ app }) => {
      await app.createTag('deleted');
      await app.deleteTag('deleted');

      // Refresh to ensure UI is updated
      await app.page.reload();
      await app.waitForReady();

      await app.openTagFilterDropdown();
      const checkbox = app.page.locator('[data-testid="tag-checkbox-deleted"]');
      await expect(checkbox).not.toBeVisible();
    });
  });

  test.describe('Tag Persistence', () => {
    test('should persist tags after page reload', async ({ app }) => {
      await app.createTag('persistent');

      await app.page.reload();
      await app.waitForReady();

      const tags = await app.getAllTags();
      expect(tags.some(t => t.name === 'persistent')).toBe(true);
    });
  });
});
