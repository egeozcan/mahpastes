import { test, expect } from '../../fixtures/test-fixtures';

// Tooltip key hints were written into the markup: "(Ctrl+Z)" on macOS, the
// old key after a rebinding, and "(F)" on the tag filter, where F toggles
// folder mode. They are now filled in from the registered shortcuts.
test.describe('Tooltip key hints', () => {
  const tip = (app: any, selector: string) => app.page.locator(selector);

  test('hints come from the bindings and use the platform’s modifier', async ({ app }) => {
    await expect(tip(app, '#toggle-folder-mode-btn, [aria-label="Toggle folder mode"]').first())
      .toHaveAttribute('data-tooltip', 'Toggle folder view (F)');
    await expect(tip(app, '[aria-label="Toggle archive view"]').first())
      .toHaveAttribute('data-tooltip', 'Show archived clips (A)');
    await expect(tip(app, '#lightbox-prev')).toHaveAttribute('data-tooltip', 'Previous clip (←)');
    await expect(tip(app, '#lightbox-close')).toHaveAttribute('data-tooltip', 'Close viewer (Esc)');
    await expect(tip(app, '.editor-tool-btn[data-tool="arrow"]')).toHaveAttribute('data-tooltip', 'Draw arrow (W)');
    // The modifier is the one ShortcutManager shows everywhere else.
    const expected = await app.page.evaluate(() =>
      // @ts-ignore - app global
      `Undo last action (${ShortcutManager.comboToDisplay('mod+z')})`);
    await expect(tip(app, '[aria-label="Undo last action"]')).toHaveAttribute('data-tooltip', expected);
    expect(expected).toMatch(/\((⌘Z|Ctrl\+Z)\)$/);
  });

  test('the tag filter no longer claims a key it does not have', async ({ app }) => {
    await expect(tip(app, '[aria-label="Filter by tags"]')).toHaveAttribute('data-tooltip', 'Filter clips by tag');
  });

  test('a rebinding shows in the tooltip, and an unbound action shows no key', async ({ app }) => {
    try {
      await app.page.evaluate(() => {
        // @ts-ignore - app global
        ShortcutManager.setOverride('editor.arrow', 'k');
        // @ts-ignore
        ShortcutManager.removeBinding('toggle-folder');
      });
      await expect(tip(app, '.editor-tool-btn[data-tool="arrow"]')).toHaveAttribute('data-tooltip', 'Draw arrow (K)');
      await expect(tip(app, '[aria-label="Toggle folder mode"]').first())
        .toHaveAttribute('data-tooltip', 'Toggle folder view');
    } finally {
      await app.page.evaluate(() => {
        // @ts-ignore
        ShortcutManager.resetAllToDefaults();
      });
    }
  });

  test('menus name the key of each action that has one, per context', async ({ app }) => {
    const { createTempFile, generateTestImage } = await import('../../helpers/test-data');
    const pathMod = await import('path');
    const filePath = await createTempFile(generateTestImage(), 'png');
    const filename = pathMod.basename(filePath);
    await app.uploadFile(filePath);
    const key = (action: string) => app.page.locator(`.card-menu-dropdown [data-action="${action}"] .card-menu-shortcut`);

    await app.openCardMenu(filename);
    await expect(key('delete')).toHaveText('D');
    await expect(key('archive')).toHaveText('E');
    await expect(key('tags')).toHaveText('T');
    // Open has no key of its own (Enter opens the viewer, not another app).
    await expect(key('open')).toHaveCount(0);
    await app.page.keyboard.press('Escape');

    // The bulk menu names no keys: with a card focused the same keys act on
    // that one card, not on the selection.
    await app.selectClips([filename]);
    const second = await createTempFile(generateTestImage(20, 20, [0, 0, 255]), 'png');
    await app.uploadFile(second);
    await app.selectClips([pathMod.basename(second)]);
    await (await app.getClipByFilename(filename)).click({ button: 'right' });
    await expect(app.page.locator('.card-menu-dropdown [data-action="bulk-delete"]')).toBeVisible();
    await expect(app.page.locator('.card-menu-dropdown .card-menu-shortcut')).toHaveCount(0);
    await app.page.keyboard.press('Escape');
    await app.page.keyboard.press('Escape');

    // In the viewer the same menu shows the viewer's keys: archive is A there.
    await app.openLightbox(filename);
    await app.page.locator('#lightbox-file-menu-trigger').click();
    await expect(app.page.locator('[data-action="archive"] .card-menu-shortcut').last()).toHaveText('A');
    await expect(app.page.locator('[data-action="delete"] .card-menu-shortcut').last()).toHaveText('D');
    await app.page.keyboard.press('Escape');
    await app.closeLightbox();
  });

  test('a tooltip shown for keyboard focus near the bottom edge opens upwards', async ({ app }) => {
    const add = app.page.locator('#add-btn');
    await add.focus();
    // focus() from script is not :focus-visible everywhere; Tab away and back is.
    await app.page.keyboard.press('Shift+Tab');
    await app.page.keyboard.press('Tab');
    await expect(add).toBeFocused();
    await expect(add).toHaveAttribute('data-tooltip-pos', 'above');
    await app.page.keyboard.press('Tab');
    await expect(add).not.toHaveAttribute('data-tooltip-pos', 'above');
  });
});
