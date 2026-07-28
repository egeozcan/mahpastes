import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage, generateTestText } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Context Menu Structure', () => {
  test('should show Open and Open With at the top of the menu', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.openCardMenu(path.basename(imagePath));
    const menu = app.page.locator(selectors.cardMenu.dropdown);
    const items = menu.locator(':scope > [role="menuitem"]');

    // First two items should be Open and Open With
    await expect(items.nth(0)).toHaveAttribute('data-action', 'open');
    await expect(items.nth(1)).toHaveAttribute('data-action', 'open-with');
  });

  test('should show Copy submenu trigger with arrow', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.openCardMenu(path.basename(imagePath));
    const copyTrigger = app.page.locator(selectors.cardMenu.copyTrigger);
    await expect(copyTrigger).toBeVisible();
    // Should have the arrow indicator
    const arrow = copyTrigger.locator('.card-menu-arrow');
    await expect(arrow).toBeVisible();
  });

  test('should expand Copy submenu on hover with Path and File items', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.hoverCopySubmenu(path.basename(imagePath));
    await expect(app.page.locator(selectors.cardMenu.copyPath)).toBeVisible();
    await expect(app.page.locator(selectors.cardMenu.copyFile)).toBeVisible();
  });

  test('should show Contents in Copy submenu for text clips', async ({ app }) => {
    const textPath = await createTempFile(generateTestText('ctx-test'), 'txt');
    await app.uploadFile(textPath);

    await app.hoverCopySubmenu(path.basename(textPath));
    await expect(app.page.locator(selectors.cardMenu.copyContents)).toBeVisible();
  });

  test('should NOT show Contents in Copy submenu for binary clips', async ({ app }) => {
    const pdfContent = Buffer.from('%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n');
    const pdfPath = await createTempFile(pdfContent, 'pdf');
    await app.uploadFile(pdfPath);

    await app.hoverCopySubmenu(path.basename(pdfPath));
    const copyContentsBtn = app.page.locator(selectors.cardMenu.copyContents);
    await expect(copyContentsBtn).not.toBeVisible();
  });

  test('should NOT show Plugins submenu when no plugins installed', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.openCardMenu(path.basename(imagePath));
    const pluginsTrigger = app.page.locator(selectors.cardMenu.pluginsTrigger);
    await expect(pluginsTrigger).toHaveCount(0);
  });

  test('should have divider between Open actions and Copy submenu', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.openCardMenu(path.basename(imagePath));
    const menu = app.page.locator(selectors.cardMenu.dropdown);
    // First divider should be between Open With and Copy
    const dividers = menu.locator(':scope > .card-menu-divider');
    await expect(dividers.first()).toBeVisible();
  });

  test('should close submenu when hovering a different item', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.hoverCopySubmenu(path.basename(imagePath));
    const submenu = app.page.locator(selectors.cardMenu.submenu);
    await expect(submenu).toBeVisible();

    // Hover away to Delete item
    await app.page.locator(selectors.cardMenu.delete).hover();
    await submenu.waitFor({ state: 'hidden', timeout: 3000 });
  });

  test('should show same Open items in lightbox menu', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.openLightbox(path.basename(imagePath));
    const trigger = app.page.locator('#lightbox-file-menu-trigger');
    await trigger.click();

    const menu = app.page.locator(selectors.cardMenu.dropdown);
    await expect(menu).toBeVisible();

    const openItem = menu.locator('[data-action="open"]');
    await expect(openItem).toBeVisible();
    const openWithItem = menu.locator('[data-action="open-with"]');
    await expect(openWithItem).toBeVisible();
  });

  test('should open the actions menu when right-clicking the lightbox image', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.openLightbox(path.basename(imagePath));
    await app.page.locator(selectors.lightbox.image).click({ button: 'right' });

    const menu = app.page.locator(selectors.cardMenu.dropdown);
    await expect(menu).toBeVisible();
    await expect(menu.locator('[data-action="open"]')).toBeVisible();
    // Portaled into the lightbox so it stacks above the backdrop
    await expect(app.page.locator(`${selectors.lightbox.overlay} > ${selectors.cardMenu.dropdown}`)).toHaveCount(1);
  });

  test('should anchor the lightbox right-click menu at the pointer', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.openLightbox(path.basename(imagePath));
    const viewport = app.page.locator(selectors.lightbox.viewport);
    const viewportBox = (await viewport.boundingBox())!;
    // Upper area, right of centre: leaves room for the menu to open below-left
    // of the cursor without being clamped to the viewport edges.
    const clickX = viewportBox.x + viewportBox.width * 0.6;
    const clickY = viewportBox.y + viewportBox.height * 0.25;
    await app.page.mouse.click(clickX, clickY, { button: 'right' });

    const menu = app.page.locator(selectors.cardMenu.dropdown);
    await expect(menu).toBeVisible();
    const menuBox = (await menu.boundingBox())!;
    // Menu is placed at the cursor, not at the Actions button in the bottom bar
    expect(Math.abs(menuBox.x + menuBox.width - clickX)).toBeLessThan(20);
    expect(Math.abs(menuBox.y - clickY)).toBeLessThan(20);
  });

  test('should close the lightbox right-click menu with Escape without closing the lightbox', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.openLightbox(path.basename(imagePath));
    await app.page.locator(selectors.lightbox.image).click({ button: 'right' });

    const menu = app.page.locator(selectors.cardMenu.dropdown);
    await expect(menu).toBeVisible();

    await app.page.keyboard.press('Escape');
    await menu.waitFor({ state: 'hidden', timeout: 3000 });
    await expect(app.page.locator(selectors.lightbox.overlay)).toHaveClass(/active/);
  });
});

test.describe('Context Menu Keyboard Navigation', () => {
  test('should open menu with Ctrl/Cmd+Enter on focused card', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    // Dismiss any lingering overlay from a previous test, then ensure page
    // keyboard focus is live (body click routes Playwright keyboard dispatch
    // to this frame).  Use Playwright's native locator.focus() rather than
    // evaluate()-based focus or tabTo: the CDP-backed focus properly registers
    // with Playwright's keyboard dispatch layer so subsequent keyboard.press()
    // calls actually reach the focused element.  tabTo with maxTabs:50 is
    // unreliable under parallel load because there may be >50 focusable
    // elements before the gallery cards in the tab order.
    await app.page.keyboard.press('Escape');
    await app.page.locator('body').click({ position: { x: 400, y: 400 } });
    await app.page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());

    const firstCard = app.page.locator('#gallery > li').first();
    await firstCard.waitFor({ state: 'visible', timeout: 5000 });
    await firstCard.focus();
    await expect(firstCard).toBeFocused({ timeout: 3000 });

    // Press Ctrl/Cmd+Enter to open context menu
    await app.page.keyboard.press('ControlOrMeta+Enter');

    const menu = app.page.locator(selectors.cardMenu.dropdown);
    await expect(menu).toBeVisible();
  });

  test('should navigate menu items with arrow keys', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.openCardMenu(path.basename(imagePath));

    // First focusable item should be focused (Open)
    const openItem = app.page.locator(selectors.cardMenu.open);
    await expect(openItem).toBeFocused();

    // Arrow down to Open With
    await app.page.keyboard.press('ArrowDown');
    const openWithItem = app.page.locator(selectors.cardMenu.openWith);
    await expect(openWithItem).toBeFocused();
  });

  test('should open Copy submenu with ArrowRight and close with ArrowLeft', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.openCardMenu(path.basename(imagePath));

    // Focus the Copy submenu trigger
    const copyTrigger = app.page.locator(selectors.cardMenu.copyTrigger);
    await copyTrigger.focus();

    // Open submenu
    await app.page.keyboard.press('ArrowRight');
    const submenu = app.page.locator(selectors.cardMenu.submenu);
    await expect(submenu).toBeVisible();

    // First submenu item should be focused
    const firstSubmenuItem = submenu.locator('[role="menuitem"]').first();
    await expect(firstSubmenuItem).toBeFocused();

    // Close submenu with ArrowLeft
    await app.page.keyboard.press('ArrowLeft');
    await submenu.waitFor({ state: 'hidden', timeout: 3000 });
    // Focus returns to the trigger
    await expect(copyTrigger).toBeFocused();
  });

  test('should close menu with Escape', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.openCardMenu(path.basename(imagePath));
    const menu = app.page.locator(selectors.cardMenu.dropdown);
    await expect(menu).toBeVisible();

    await app.page.keyboard.press('Escape');
    await menu.waitFor({ state: 'hidden', timeout: 3000 });
  });

  test('should close only submenu with Escape when submenu is open', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.openCardMenu(path.basename(imagePath));
    const copyTrigger = app.page.locator(selectors.cardMenu.copyTrigger);
    await copyTrigger.focus();
    await app.page.keyboard.press('ArrowRight');

    const submenu = app.page.locator(selectors.cardMenu.submenu);
    await expect(submenu).toBeVisible();

    // Escape closes submenu but keeps main menu
    await app.page.keyboard.press('Escape');
    await submenu.waitFor({ state: 'hidden', timeout: 3000 });

    const menu = app.page.locator(selectors.cardMenu.dropdown);
    await expect(menu).toBeVisible();
  });
});

test.describe('Open Actions', () => {
  test('should show Open item in card menu', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.openCardMenu(path.basename(imagePath));
    const openItem = app.page.locator(selectors.cardMenu.open);
    await expect(openItem).toBeVisible();
    await expect(openItem).toContainText('Open');
  });

  test('should show Open With item in card menu', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.openCardMenu(path.basename(imagePath));
    const openWithItem = app.page.locator(selectors.cardMenu.openWith);
    await expect(openWithItem).toBeVisible();
    await expect(openWithItem).toContainText('Open With');
  });

  test('should have correct tooltips on Open items', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);

    await app.openCardMenu(path.basename(imagePath));
    const openItem = app.page.locator(selectors.cardMenu.open);
    await expect(openItem).toHaveAttribute('title', 'Open with your default application');

    const openWithItem = app.page.locator(selectors.cardMenu.openWith);
    await expect(openWithItem).toHaveAttribute('title', 'Choose an application to open this clip');
  });
});
