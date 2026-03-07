import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage, generateTestText } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

/**
 * HTML integrity tests — validate that dynamically generated HTML is
 * structurally correct. Catches unclosed tags, missing attributes,
 * empty containers that should have children, etc.
 *
 * These run a generic DOMParser-based validator inside page.evaluate()
 * so they catch issues that are invisible to normal functional tests.
 */

/** Validate an HTML string returns no parser errors and has no text-node artifacts from broken tags. */
const HTML_VALIDATOR_FN = `
  function validateHTML(html) {
    const parser = new DOMParser();
    const doc = parser.parseFromString('<div>' + html + '</div>', 'text/html');
    const errors = [];

    // Check for parser error elements
    const parserError = doc.querySelector('parsererror');
    if (parserError) {
      errors.push('DOMParser error: ' + parserError.textContent.substring(0, 200));
    }

    return errors;
  }
`;

/**
 * Validate a live DOM element: check that expected children exist, no
 * broken tags leaked as text, and required attributes are present.
 */
const DOM_STRUCTURE_CHECKER_FN = `
  function checkElement(el, checks) {
    const errors = [];
    if (!el) {
      errors.push('Element not found');
      return errors;
    }

    // 1. Every child selector must resolve to at least one element
    if (checks.requiredChildren) {
      for (const sel of checks.requiredChildren) {
        const found = el.querySelectorAll(sel);
        if (found.length === 0) {
          errors.push('Missing required child: ' + sel);
        }
      }
    }

    // 2. Attributes that must exist on the root element
    if (checks.requiredAttributes) {
      for (const attr of checks.requiredAttributes) {
        if (!el.hasAttribute(attr)) {
          errors.push('Missing attribute on root: ' + attr);
        }
      }
    }

    // 3. Attribute checks on child elements: [{selector, attrs: [attr, ...]}]
    if (checks.childAttributes) {
      for (const { selector, attrs } of checks.childAttributes) {
        const targets = el.querySelectorAll(selector);
        if (targets.length === 0) {
          errors.push('childAttributes: no element matches ' + selector);
        }
        targets.forEach((target, i) => {
          for (const attr of attrs) {
            if (!target.hasAttribute(attr)) {
              errors.push(selector + '[' + i + '] missing attribute: ' + attr);
            }
          }
        });
      }
    }

    // 4. Every <button> and <a> should have a closing tag (i.e. they
    //    should contain at least one child node).
    const interactives = el.querySelectorAll('button, a');
    interactives.forEach((node) => {
      if (node.childNodes.length === 0) {
        const id = node.getAttribute('data-action') || node.className.substring(0, 30);
        errors.push('Empty interactive element (likely unclosed tag): ' + id);
      }
    });

    // 5. Every <svg> should have at least one child (path, circle, etc.)
    const svgs = el.querySelectorAll('svg');
    svgs.forEach((svg, i) => {
      if (svg.children.length === 0) {
        errors.push('Empty SVG[' + i + '] (likely broken tag)');
      }
    });

    // 6. No raw angle bracket artifacts in text content (sign of unclosed tag)
    const textContent = el.textContent || '';
    if (/<[a-z]|<\\/[a-z]/i.test(textContent)) {
      // Only flag if it looks like a tag, not legitimate < in content
      const suspicious = textContent.match(/<\\/?[a-z][a-z0-9-]*[\\s>]/gi);
      if (suspicious) {
        errors.push('Suspicious tag-like text in content: ' + suspicious.slice(0, 3).join(', '));
      }
    }

    return errors;
  }
`;

test.describe('HTML Integrity', () => {
  test.describe('Clip Card (image)', () => {
    test('should have well-formed structure', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);

      const errors = await app.page.evaluate(
        ({ sel, checkerFn }) => {
          eval(checkerFn);
          const card = document.querySelector(sel);
          // @ts-ignore
          return checkElement(card, {
            requiredChildren: [
              '.preview-container',            // image preview area
              'p.truncate',                     // filename
              '[data-action="menu"]',           // menu trigger button
              '[data-action="menu"] svg',       // menu icon SVG
              '.clip-checkbox',                 // selection checkbox
              '[data-action="open-lightbox"]',  // clickable preview
            ],
            requiredAttributes: ['data-id', 'data-filename', 'aria-label'],
            childAttributes: [
              { selector: '[data-action="menu"]', attrs: ['aria-label', 'aria-haspopup', 'aria-expanded', 'title'] },
              { selector: '.clip-checkbox', attrs: ['data-id', 'aria-label'] },
            ],
          });
        },
        { sel: selectors.gallery.clipCard, checkerFn: DOM_STRUCTURE_CHECKER_FN }
      );

      expect(errors, `Card HTML errors: ${errors.join('; ')}`).toHaveLength(0);
    });
  });

  test.describe('Clip Card (text)', () => {
    test('should have well-formed structure', async ({ app }) => {
      const textPath = await createTempFile(generateTestText('html-test'), 'txt');
      await app.uploadFile(textPath);

      const errors = await app.page.evaluate(
        ({ sel, checkerFn }) => {
          eval(checkerFn);
          const card = document.querySelector(sel);
          // @ts-ignore
          return checkElement(card, {
            requiredChildren: [
              '.preview-container',
              '.preview-container pre code',    // text clips show code preview
              '[data-action="menu"]',
              '[data-action="menu"] svg',
            ],
            requiredAttributes: ['data-id', 'data-filename'],
          });
        },
        { sel: selectors.gallery.clipCard, checkerFn: DOM_STRUCTURE_CHECKER_FN }
      );

      expect(errors, `Card HTML errors: ${errors.join('; ')}`).toHaveLength(0);
    });
  });

  test.describe('Clip Card (binary)', () => {
    test('should have well-formed structure for unknown file types', async ({ app }) => {
      const pdfContent = Buffer.from('%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n');
      const pdfPath = await createTempFile(pdfContent, 'pdf');
      await app.uploadFile(pdfPath);

      const errors = await app.page.evaluate(
        ({ sel, checkerFn }) => {
          eval(checkerFn);
          const card = document.querySelector(sel);
          // @ts-ignore
          return checkElement(card, {
            requiredChildren: [
              '.preview-container',
              '.preview-container svg',          // file icon for binary types
              '[data-action="menu"]',
              '[data-action="menu"] svg',
            ],
            requiredAttributes: ['data-id', 'data-filename'],
          });
        },
        { sel: selectors.gallery.clipCard, checkerFn: DOM_STRUCTURE_CHECKER_FN }
      );

      expect(errors, `Card HTML errors: ${errors.join('; ')}`).toHaveLength(0);
    });
  });

  test.describe('Context Menu', () => {
    test('should have well-formed structure with all expected items', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);

      await app.openCardMenu(path.basename(imagePath));

      const errors = await app.page.evaluate(
        ({ sel, checkerFn }) => {
          eval(checkerFn);
          const menu = document.querySelector(sel);
          // @ts-ignore
          return checkElement(menu, {
            requiredChildren: [
              '[data-action="open"]',            // Open
              '[data-action="open-with"]',       // Open With
              '[data-submenu="copy"]',           // Copy submenu trigger
              '[data-action="save-file"]',       // Save
              '[data-action="tags"]',            // Tags
              '[data-action="metadata"]',        // Metadata
              '[data-action="archive"]',         // Archive
              '[data-action="delete"]',          // Delete
              '.card-menu-divider',              // At least one divider
            ],
            requiredAttributes: ['role'],
            childAttributes: [
              { selector: '[role="menuitem"]', attrs: ['role', 'tabindex'] },
              { selector: '[data-submenu="copy"]', attrs: ['aria-haspopup', 'aria-expanded'] },
            ],
          });
        },
        { sel: selectors.cardMenu.dropdown, checkerFn: DOM_STRUCTURE_CHECKER_FN }
      );

      expect(errors, `Context menu errors: ${errors.join('; ')}`).toHaveLength(0);
    });

    test('Copy submenu should have well-formed items', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);

      await app.hoverCopySubmenu(path.basename(imagePath));

      const errors = await app.page.evaluate(
        ({ sel, checkerFn }) => {
          eval(checkerFn);
          const submenu = document.querySelector(sel);
          // @ts-ignore
          return checkElement(submenu, {
            requiredChildren: [
              '[data-action="copy-path"]',
              '[data-action="copy-file"]',
            ],
            requiredAttributes: ['role'],
            childAttributes: [
              { selector: '[role="menuitem"]', attrs: ['role', 'tabindex'] },
            ],
          });
        },
        { sel: selectors.cardMenu.submenu, checkerFn: DOM_STRUCTURE_CHECKER_FN }
      );

      expect(errors, `Copy submenu errors: ${errors.join('; ')}`).toHaveLength(0);
    });
  });

  test.describe('Lightbox', () => {
    test('should have well-formed structure when open', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);

      await app.openLightbox(path.basename(imagePath));

      const errors = await app.page.evaluate(
        ({ sel, checkerFn }) => {
          eval(checkerFn);
          const lightbox = document.querySelector(sel);
          // @ts-ignore
          return checkElement(lightbox, {
            requiredChildren: [
              '#lightbox-img',
              '#lightbox-close',
              '#lightbox-prev',
              '#lightbox-next',
              '#lightbox-file-menu-trigger',     // Actions button
              '#lightbox-zoom-slider',
              '#lightbox-zoom-info',
            ],
            requiredAttributes: ['role', 'aria-modal', 'aria-label'],
            childAttributes: [
              { selector: '#lightbox-close', attrs: ['aria-label'] },
              { selector: '#lightbox-prev', attrs: ['aria-label'] },
              { selector: '#lightbox-next', attrs: ['aria-label'] },
            ],
          });
        },
        { sel: selectors.lightbox.overlay, checkerFn: DOM_STRUCTURE_CHECKER_FN }
      );

      expect(errors, `Lightbox HTML errors: ${errors.join('; ')}`).toHaveLength(0);
    });

    test('Actions menu should open with well-formed items', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);

      await app.openLightbox(path.basename(imagePath));
      const trigger = app.page.locator('#lightbox-file-menu-trigger');
      await trigger.click();
      await app.page.locator(selectors.cardMenu.dropdown).waitFor({ state: 'visible', timeout: 5000 });

      const errors = await app.page.evaluate(
        ({ sel, checkerFn }) => {
          eval(checkerFn);
          const menu = document.querySelector(sel);
          // @ts-ignore
          return checkElement(menu, {
            requiredChildren: [
              '[data-action="open"]',
              '[data-action="open-with"]',
              '[data-submenu="copy"]',
              '[data-action="save-file"]',
              '[data-action="delete"]',
            ],
            requiredAttributes: ['role'],
          });
        },
        { sel: selectors.cardMenu.dropdown, checkerFn: DOM_STRUCTURE_CHECKER_FN }
      );

      expect(errors, `Lightbox menu errors: ${errors.join('; ')}`).toHaveLength(0);
    });
  });

  test.describe('Header and Navigation', () => {
    test('header should have well-formed structure', async ({ app }) => {
      const errors = await app.page.evaluate(
        ({ checkerFn }) => {
          eval(checkerFn);
          const header = document.querySelector('header');
          // @ts-ignore
          return checkElement(header, {
            requiredChildren: [
              '#search-input',
              '#tag-filter-btn',
              '#header-archive-btn',
              '#sort-btn',
              '#drawer-toggle-btn',
            ],
            childAttributes: [
              { selector: '#search-input', attrs: ['placeholder'] },
              { selector: '#drawer-toggle-btn', attrs: ['aria-label'] },
            ],
          });
        },
        { checkerFn: DOM_STRUCTURE_CHECKER_FN }
      );

      expect(errors, `Header HTML errors: ${errors.join('; ')}`).toHaveLength(0);
    });

    test('bottom bar should have well-formed structure', async ({ app }) => {
      const errors = await app.page.evaluate(
        ({ sel, checkerFn }) => {
          eval(checkerFn);
          const bar = document.querySelector(sel);
          // @ts-ignore
          return checkElement(bar, {
            requiredChildren: [
              '#add-btn',
              '#expiry-select',
              '#clip-count',
            ],
            childAttributes: [
              { selector: '#add-btn', attrs: ['aria-label'] },
            ],
          });
        },
        { sel: selectors.bottomBar.root, checkerFn: DOM_STRUCTURE_CHECKER_FN }
      );

      expect(errors, `Bottom bar HTML errors: ${errors.join('; ')}`).toHaveLength(0);
    });

    test('nav drawer should have well-formed structure', async ({ app }) => {
      await app.openDrawer();

      const errors = await app.page.evaluate(
        ({ sel, checkerFn }) => {
          eval(checkerFn);
          const drawer = document.querySelector(sel);
          // @ts-ignore
          return checkElement(drawer, {
            requiredChildren: [
              '#drawer-close-btn',
              '#toggle-watch-view-btn',
              '#toggle-archive-view-btn',
              '#open-settings-btn',
              '#open-plugins-btn',
            ],
            requiredAttributes: ['role', 'aria-label'],
            childAttributes: [
              { selector: '#drawer-close-btn', attrs: ['aria-label'] },
            ],
          });
        },
        { sel: selectors.drawer.panel, checkerFn: DOM_STRUCTURE_CHECKER_FN }
      );

      expect(errors, `Nav drawer HTML errors: ${errors.join('; ')}`).toHaveLength(0);
    });
  });

  test.describe('Settings Modal', () => {
    test('should have well-formed structure', async ({ app }) => {
      await app.openSettingsModal();

      const errors = await app.page.evaluate(
        ({ sel, checkerFn }) => {
          eval(checkerFn);
          const modal = document.querySelector(sel);
          // @ts-ignore
          return checkElement(modal, {
            requiredChildren: [
              '#settings-close',
              '#settings-save',
              '[data-testid="tooltips-toggle"]',
              '[data-testid="settings-shortcuts-section"]',
            ],
            requiredAttributes: ['role', 'aria-modal'],
          });
        },
        { sel: selectors.settings.modal, checkerFn: DOM_STRUCTURE_CHECKER_FN }
      );

      expect(errors, `Settings modal errors: ${errors.join('; ')}`).toHaveLength(0);
    });
  });

  test.describe('Tag System', () => {
    test('tag filter dropdown should have well-formed structure', async ({ app }) => {
      // Create a tag first so the list has content
      await app.createTag('integrity-test');
      await app.openTagFilterDropdown();

      const errors = await app.page.evaluate(
        ({ sel, checkerFn }) => {
          eval(checkerFn);
          const dropdown = document.querySelector(sel);
          // @ts-ignore
          return checkElement(dropdown, {
            requiredChildren: [
              '#tag-filter-list',
              '#clear-tag-filters',
            ],
          });
        },
        { sel: selectors.tags.filterDropdown, checkerFn: DOM_STRUCTURE_CHECKER_FN }
      );

      expect(errors, `Tag filter errors: ${errors.join('; ')}`).toHaveLength(0);
    });

    test('tag popover should have well-formed structure', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.createTag('pop-test');

      await app.openCardMenu(path.basename(imagePath));
      await app.page.locator(selectors.cardMenu.tags).click();
      await app.page.locator(selectors.tags.popover).waitFor({ state: 'visible', timeout: 5000 });

      const errors = await app.page.evaluate(
        ({ sel, checkerFn }) => {
          eval(checkerFn);
          const popover = document.querySelector(sel);
          // @ts-ignore
          return checkElement(popover, {
            requiredChildren: [
              '#tag-popover-list',
              '#create-tag-btn',
            ],
          });
        },
        { sel: selectors.tags.popover, checkerFn: DOM_STRUCTURE_CHECKER_FN }
      );

      expect(errors, `Tag popover errors: ${errors.join('; ')}`).toHaveLength(0);
    });
  });

  test.describe('Watch Folders View', () => {
    test('should have well-formed structure', async ({ app, tempDir }) => {
      await app.openWatchView();
      await app.addWatchFolder(tempDir);

      const errors = await app.page.evaluate(
        ({ sel, checkerFn }) => {
          eval(checkerFn);
          const card = document.querySelector(sel);
          // @ts-ignore
          return checkElement(card, {
            requiredChildren: [
              '.truncate',                        // folder path
              'button[data-action="toggle-pause"]',     // pause button
              'button[data-action="toggle-pause"] svg', // pause icon
              'button[data-action="remove"]',           // remove button
              'button[data-action="remove"] svg',       // remove icon
            ],
          });
        },
        { sel: selectors.watch.folderCard, checkerFn: DOM_STRUCTURE_CHECKER_FN }
      );

      expect(errors, `Watch folder card errors: ${errors.join('; ')}`).toHaveLength(0);
    });
  });

  test.describe('Confirmation Dialog', () => {
    test('should have well-formed structure', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);

      // Trigger delete to show confirmation dialog
      await app.openCardMenu(path.basename(imagePath));
      await app.page.locator(selectors.cardMenu.delete).click();
      await app.page.locator(selectors.confirm.dialog).waitFor({ state: 'visible', timeout: 5000 });

      const errors = await app.page.evaluate(
        ({ sel, checkerFn }) => {
          eval(checkerFn);
          const dialog = document.querySelector(sel);
          // @ts-ignore
          return checkElement(dialog, {
            requiredChildren: [
              '#confirm-title',
              '#confirm-message',
              '#confirm-yes-btn',
              '#confirm-no-btn',
            ],
          });
        },
        { sel: selectors.confirm.dialog, checkerFn: DOM_STRUCTURE_CHECKER_FN }
      );

      expect(errors, `Confirm dialog errors: ${errors.join('; ')}`).toHaveLength(0);
    });
  });
});
