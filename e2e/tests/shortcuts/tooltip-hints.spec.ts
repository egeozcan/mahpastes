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
});
