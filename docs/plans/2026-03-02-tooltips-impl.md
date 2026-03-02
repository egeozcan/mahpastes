# Tooltips Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add custom styled tooltips to all interactive elements in the app with smart positioning and a settings toggle to disable them.

**Architecture:** CSS `::after`/`::before` pseudo-elements driven by `data-tooltip` attributes, with a ~60-line JS module for viewport-edge detection. Tooltips disabled via `body.tooltips-disabled` class toggle, stored in SQLite settings table.

**Tech Stack:** Vanilla CSS + JS, Go (existing `GetSetting`/`SetSetting` in `app.go`)

**Design doc:** `docs/plans/2026-03-02-tooltips-design.md`

---

### Task 1: CSS Tooltip Styles

Add all tooltip CSS to `frontend/css/main.css`.

**Files:**
- Modify: `frontend/css/main.css` (append after line 260, after `.clip-focused`)

**Step 1: Add tooltip CSS**

Append the following CSS at the end of `frontend/css/main.css`:

```css
/* Tooltip system */
[data-tooltip] {
    position: relative;
}

[data-tooltip]::before,
[data-tooltip]::after {
    position: absolute;
    opacity: 0;
    pointer-events: none;
    transition: opacity 150ms;
    transition-delay: 0ms;
    z-index: 270;
}

/* Arrow */
[data-tooltip]::before {
    content: '';
    border: 4px solid transparent;
}

/* Tooltip box */
[data-tooltip]::after {
    content: attr(data-tooltip);
    font-family: 'IBM Plex Mono', monospace;
    font-size: 10px;
    font-weight: 500;
    color: #fff;
    background: #1c1917;
    padding: 4px 8px;
    border-radius: 4px;
    white-space: nowrap;
    box-shadow: 0 1px 3px rgba(0,0,0,0.12);
    line-height: 1.4;
}

/* Show on hover with delay */
[data-tooltip]:hover::before,
[data-tooltip]:hover::after {
    opacity: 1;
    transition-delay: 300ms;
}

/* Default position: below */
[data-tooltip]::before {
    top: 100%;
    left: 50%;
    transform: translateX(-50%);
    margin-top: 2px;
    border-bottom-color: #1c1917;
}

[data-tooltip]::after {
    top: 100%;
    left: 50%;
    transform: translateX(-50%);
    margin-top: 8px;
}

/* Position: above */
[data-tooltip-pos="above"]::before {
    top: auto;
    bottom: 100%;
    margin-top: 0;
    margin-bottom: 2px;
    border-bottom-color: transparent;
    border-top-color: #1c1917;
}

[data-tooltip-pos="above"]::after {
    top: auto;
    bottom: 100%;
    margin-top: 0;
    margin-bottom: 8px;
}

/* Position: left */
[data-tooltip-pos="left"]::before {
    top: 50%;
    left: auto;
    right: 100%;
    transform: translateY(-50%);
    margin-top: 0;
    margin-right: 2px;
    border-bottom-color: transparent;
    border-left-color: #1c1917;
}

[data-tooltip-pos="left"]::after {
    top: 50%;
    left: auto;
    right: 100%;
    transform: translateY(-50%);
    margin-top: 0;
    margin-right: 8px;
}

/* Position: right */
[data-tooltip-pos="right"]::before {
    top: 50%;
    left: 100%;
    transform: translateY(-50%);
    margin-top: 0;
    margin-left: 2px;
    border-bottom-color: transparent;
    border-right-color: #1c1917;
}

[data-tooltip-pos="right"]::after {
    top: 50%;
    left: 100%;
    transform: translateY(-50%);
    margin-top: 0;
    margin-left: 8px;
}

/* Disable tooltips globally */
body.tooltips-disabled [data-tooltip]::before,
body.tooltips-disabled [data-tooltip]::after {
    display: none !important;
}
```

**Step 2: Commit**

```bash
git add frontend/css/main.css
git commit -m "feat: add tooltip CSS with position variants and disable toggle"
```

---

### Task 2: Tooltip JS Module

Create the smart-positioning JS module.

**Files:**
- Create: `frontend/js/tooltips.js`
- Modify: `frontend/index.html` (add script tag at line 1214, before `utils.js`)

**Step 1: Create `frontend/js/tooltips.js`**

```javascript
// --- Tooltip Smart Positioning ---
// Monitors [data-tooltip] elements and flips position when near viewport edges.

(function() {
    const MARGIN = 8; // px from viewport edge

    function computePosition(el) {
        const rect = el.getBoundingClientRect();
        const vh = window.innerHeight;
        const vw = window.innerWidth;

        const spaceBelow = vh - rect.bottom;
        const spaceAbove = rect.top;
        const spaceRight = vw - rect.right;
        const spaceLeft = rect.left;

        // Default: below. Flip if not enough room.
        if (spaceBelow < 40 && spaceAbove > spaceBelow) return 'above';
        if (spaceBelow >= 40) return 'below';
        if (spaceRight < 40 && spaceLeft > 40) return 'left';
        if (spaceLeft < 40 && spaceRight > 40) return 'right';
        return 'below';
    }

    document.addEventListener('mouseenter', function(e) {
        const el = e.target.closest?.('[data-tooltip]');
        if (!el) return;

        const pos = computePosition(el);
        if (pos !== 'below') {
            el.setAttribute('data-tooltip-pos', pos);
        }
    }, true);

    document.addEventListener('mouseleave', function(e) {
        const el = e.target.closest?.('[data-tooltip]');
        if (!el) return;

        el.removeAttribute('data-tooltip-pos');
    }, true);

    // --- Settings: tooltips enabled/disabled ---
    async function loadTooltipSetting() {
        try {
            const val = await window.go.main.App.GetSetting('tooltips_enabled');
            if (val === 'false') {
                document.body.classList.add('tooltips-disabled');
            }
        } catch (e) {
            // Setting doesn't exist yet — tooltips enabled by default
        }
    }

    // Expose toggle for settings UI
    window.toggleTooltips = async function(enabled) {
        if (enabled) {
            document.body.classList.remove('tooltips-disabled');
        } else {
            document.body.classList.add('tooltips-disabled');
        }
        try {
            await window.go.main.App.SetSetting('tooltips_enabled', enabled ? 'true' : 'false');
        } catch (e) {
            console.error('Failed to save tooltip setting:', e);
        }
    };

    // Load on startup
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', loadTooltipSetting);
    } else {
        loadTooltipSetting();
    }
})();
```

**Step 2: Add script tag in `frontend/index.html`**

Insert before the `utils.js` script tag (currently at line 1214). Add at line 1214:

```html
    <script src="js/tooltips.js"></script>
```

This makes all other scripts load after the tooltip module, so `window.toggleTooltips` is available.

**Step 3: Commit**

```bash
git add frontend/js/tooltips.js frontend/index.html
git commit -m "feat: add tooltip JS module with smart positioning and settings integration"
```

---

### Task 3: Settings Toggle UI

Add a "Show tooltips" toggle to the Settings modal.

**Files:**
- Modify: `frontend/index.html` (settings modal, after line 971 — after the Keyboard Shortcuts section closing `</div>`)
- Modify: `frontend/js/settings.js` (add tooltip toggle logic)

**Step 1: Add tooltip toggle HTML in settings modal**

Insert after the Keyboard Shortcuts section closing `</div>` (line 971 in `frontend/index.html`), before the modal footer (line 972 `</div>`):

```html
                <!-- Tooltips -->
                <div class="pt-4 border-t border-stone-100">
                    <h3 class="text-xs font-semibold text-stone-600 uppercase tracking-wider mb-3 flex items-center gap-2">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/>
                        </svg>
                        Tooltips
                    </h3>
                    <p class="text-[11px] text-stone-500 mb-3">
                        Show helpful descriptions when hovering over buttons and actions.
                    </p>
                    <label class="flex items-center gap-3 cursor-pointer" data-testid="tooltips-toggle-label">
                        <div class="relative inline-flex items-center cursor-pointer">
                            <input type="checkbox" id="tooltips-toggle" data-testid="tooltips-toggle" class="sr-only peer" checked>
                            <div class="w-9 h-5 bg-stone-300 peer-focus:ring-2 peer-focus:ring-stone-400/30 rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:bg-stone-800"></div>
                        </div>
                        <span class="text-xs font-medium text-stone-700">Show tooltips on hover</span>
                    </label>
                </div>
```

**Step 2: Add JS logic in `frontend/js/settings.js`**

Add at the end of `settings.js` (after line 419):

```javascript
// --- Tooltips Toggle ---
const tooltipsToggle = document.getElementById('tooltips-toggle');

async function loadTooltipToggle() {
    try {
        const val = await window.go.main.App.GetSetting('tooltips_enabled');
        if (val === 'false') {
            tooltipsToggle.checked = false;
        } else {
            tooltipsToggle.checked = true;
        }
    } catch (e) {
        tooltipsToggle.checked = true; // default enabled
    }
}

if (tooltipsToggle) {
    tooltipsToggle.addEventListener('change', () => {
        if (typeof window.toggleTooltips === 'function') {
            window.toggleTooltips(tooltipsToggle.checked);
        }
    });
}
```

**Step 3: Call `loadTooltipToggle()` in `openSettings()`**

In `frontend/js/settings.js`, modify the `openSettings` function (line 9) to call `loadTooltipToggle()`:

Currently:
```javascript
function openSettings() {
    renderHiddenTagsSettings();
    loadUpdateInterval();
    renderShortcutsSettings();
```

Change to:
```javascript
function openSettings() {
    renderHiddenTagsSettings();
    loadUpdateInterval();
    renderShortcutsSettings();
    loadTooltipToggle();
```

**Step 4: Commit**

```bash
git add frontend/index.html frontend/js/settings.js
git commit -m "feat: add tooltips on/off toggle to Settings modal"
```

---

### Task 4: Add Tooltips to Static HTML Elements

Add `data-tooltip` attributes to elements defined in `frontend/index.html`. Remove any existing `title` attributes that are being replaced.

**IMPORTANT:** The implementing agent MUST read the actual source code for each element to verify tooltip descriptions are technically accurate. Cross-reference the tooltip text from the design doc with the actual behavior implemented in the JS handlers.

**Files:**
- Modify: `frontend/index.html`

**Step 1: Header buttons (lines 38-98)**

Add to each header button:
- Tag filter button (`#tag-filter-btn`, line 38): add `data-tooltip="Filter clips by tag (F)"`
- Archive toggle (`#header-archive-btn`, line 66): add `data-tooltip="Show archived clips (A)"`
- Sort button (`#sort-btn`, line 78): add `data-tooltip="Change sort order"`
- Menu button (`#drawer-toggle-btn`, line 90): add `data-tooltip="Open navigation menu"`

**Step 2: Nav drawer buttons (lines 115-177)**

- Close button (`#drawer-close-btn`, line 115): add `data-tooltip="Close menu"`
- Watch button (`#toggle-watch-view-btn`, line 122): add `data-tooltip="Monitor folders and auto-import new files"`
- Archive button (`#toggle-archive-view-btn`, line 134): add `data-tooltip="View archived clips"`
- Clear temp button (`#delete-all-temp-btn`, line 143): add `data-tooltip="Remove all clips that were marked temporary"`
- Deduplicate button (`#deduplicate-btn`, line 152): add `data-tooltip="Scan library for identical files and merge them"`
- Settings button (`#open-settings-btn`, line 161): add `data-tooltip="Configure app preferences"`
- Plugins button (`#open-plugins-btn`, line 170): add `data-tooltip="Manage installed plugins"`

**Step 3: Watch view buttons (lines 205-240)**

- Add Folder button (`#add-folder-btn`, line 237): add `data-tooltip="Pick a folder to monitor for new files"`
- Global watch toggle label area (line 217): add `data-tooltip="Master switch for all folder watchers"` to the `<label>` element

**Step 4: Bulk toolbar buttons (lines 259-328)**

- Compare (`#bulk-compare-btn`, line 259): add `data-tooltip="Open selected images in side-by-side comparison view"`
- Tag (`#bulk-tag-btn`, line 267): add `data-tooltip="Add a tag to selected clips"`
- Expire (`#bulk-expiry-btn`, line 276): add `data-tooltip="Set expiration on selected clips"`
- Clear Expiry (`#bulk-cancel-expiry-btn`, line 284): add `data-tooltip="Remove expiration from selected clips"`
- Copy (`#bulk-copy-btn`, line 292): add `data-tooltip="Copy selected files to clipboard for pasting"`
- Download (`#bulk-download-btn`, line 300): add `data-tooltip="Save selected files to your Downloads folder"`
- Archive (`#bulk-archive-btn`, line 308): add `data-tooltip="Archive selected clips"`
- Delete (`#bulk-delete-btn`, line 317): add `data-tooltip="Permanently delete selected -- cannot be undone"`
- Cancel (`#cancel-selection-btn`, line 326): add `data-tooltip="Deselect all clips"`

**Step 5: Bottom bar (lines 347-377)**

- Add button (`#add-btn`, line 347): add `data-tooltip="Upload files to your library"`
- Expiry select (`#expiry-select`, line 353): replace `title="Set expiration for new uploads"` with `data-tooltip="New uploads will auto-delete after this duration"`

**Step 6: Lightbox (lines 475-513)**

- Close (`#lightbox-close`, line 475): add `data-tooltip="Close viewer (Esc)"`
- Previous (`#lightbox-prev`, line 482): add `data-tooltip="Previous clip (Left)"`
- Next (`#lightbox-next`, line 494): add `data-tooltip="Next clip (Right)"`
- Zoom slider (`#lightbox-zoom-slider`, line 510): add `data-tooltip="Adjust zoom level"` to the parent `div.lightbox-zoom-control`

**Step 7: Comparison modal (lines 524-623)**

- Swap (`#comparison-swap`, line 524): replace `title="Swap images (S)"` with `data-tooltip="Swap left and right images (S)"`
- Close (`#comparison-close`, line 531): add `data-tooltip="Close comparison (Esc)"`
- Fade mode (`#mode-fade`, line 566): add `data-tooltip="Crossfade opacity between both images (1)"`
- Slider mode (`#mode-slider`, line 568): add `data-tooltip="Drag a divider to reveal each image (2)"`
- Diff mode (`#mode-diff`, line 570): replace `title="Diff mode (3)"` with `data-tooltip="Compute and highlight pixel-level differences (3)"`
- Stretch toggle (`#toggle-stretch`, line 598): add `data-tooltip="Scale images to fill the view area"`
- Zoom out (`#zoom-out`, line 608): add `data-tooltip="Zoom out (-)"`
- Zoom in (`#zoom-in`, line 614): add `data-tooltip="Zoom in (+)"`
- Zoom fit (`#zoom-fit`, line 619): add `data-tooltip="Fit image to view (0)"`

**Step 8: Editor toolbar (lines 667-743)**

Replace all existing `title` attributes with `data-tooltip`:
- Brush (line 667): `title="Brush (B)"` → `data-tooltip="Freehand brush (B)"`
- Line (line 674): `title="Line (L)"` → `data-tooltip="Draw straight line (L)"`
- Rectangle (line 680): `title="Rectangle (R)"` → `data-tooltip="Draw rectangle (R)"`
- Circle (line 686): `title="Circle (C)"` → `data-tooltip="Draw circle (C)"`
- Text (line 691): `title="Text (T)"` → `data-tooltip="Add text annotation (T)"`
- Eraser (line 697): `title="Eraser (E)"` → `data-tooltip="Erase drawn content (E)"`
- Color input (line 711): add `data-tooltip="Pick drawing color"` to the wrapping `<div>` (line 709)
- Opacity range (line 716): add `data-tooltip="Adjust brush opacity"` to the wrapping `<div>` (line 714)
- Size range (line 721): add `data-tooltip="Adjust brush size"` to the wrapping `<div>` (line 719)
- Undo (line 729): replace `title="Undo (Ctrl+Z)"` with `data-tooltip="Undo last action (Ctrl+Z)"`
- Redo (line 736): replace `title="Redo (Ctrl+Y)"` with `data-tooltip="Redo last action (Ctrl+Y)"`
- Save (`#editor-save`, line 637): add `data-tooltip="Save edits as a new clip in your library"`
- Close (`#editor-close`, line 645): add `data-tooltip="Discard all edits and close editor (Esc)"`

**Step 9: Modal close buttons**

- Settings close (`#settings-close`, line 887): add `data-tooltip="Close"`
- Plugins close (`#plugins-close`, line 1005): add `data-tooltip="Close"`
- Shortcuts cheatsheet close (`#shortcuts-cheatsheet-close`): add `data-tooltip="Close"` — find this element with grep

**Step 10: Commit**

```bash
git add frontend/index.html
git commit -m "feat: add data-tooltip attributes to all static HTML elements"
```

---

### Task 5: Add Tooltips to Dynamically Created Elements

Add `data-tooltip` attributes to elements created by JavaScript.

**IMPORTANT:** The implementing agent MUST read each source file and verify the actual behavior of each action handler before applying tooltip text. Check what `handleCardMenuAction`, `handleLightboxFileAction`, and similar handlers actually do.

**Files:**
- Modify: `frontend/js/ui.js` (card menu items, drag handle, three-dot menu)
- Modify: `frontend/js/modals.js` (lightbox file menu, lightbox plugin menu trigger)
- Modify: `frontend/js/watch.js` (watch folder action buttons)

**Step 1: Card menu items — `frontend/js/ui.js`**

In `renderCardMenu()` (line 100), after creating each menu item button (line 148), add `data-tooltip` based on `action.id`. Add a tooltip map before the function and apply during rendering.

Add before `renderCardMenu` (around line 99):

```javascript
const cardMenuTooltips = {
    'copy-path': 'Create a temp file and copy its path to clipboard',
    'copy-file': 'Place file on clipboard for pasting into other apps',
    'copy-contents': 'Copy the raw text or data to clipboard',
    'save-file': 'Save a copy to your Downloads folder',
    'edit': 'Open in the built-in image editor for annotation',
    'tags': 'Add or remove tags',
    'metadata': 'View and edit file metadata',
    'set-expiration': 'Schedule auto-deletion after a time period',
    'cancel-expiration': 'Cancel the scheduled auto-deletion',
    'archive': 'Move to archive without deleting',
    'restore': 'Move back from archive',
    'merge-duplicates': 'Find clips with identical content and merge them',
    'delete': 'Permanently delete -- this cannot be undone',
};
```

Then in the `builtInActions.forEach` loop (line 142-150), after setting `item.innerHTML`, add:

```javascript
        const tooltip = cardMenuTooltips[action.id];
        if (tooltip) item.setAttribute('data-tooltip', tooltip);
```

Note: The archive action uses `action.id === 'archive'` with label "Restore" when viewing archive. The tooltip map needs both `'archive'` and `'restore'` entries, but the action id is always `'archive'` — the label changes. Check the actual `action.id` value in the code:
- Line 135: `{ id: 'archive', label: isViewingArchive ? 'Restore' : 'Archive', ... }`

So the tooltip should be dynamic. Instead of using the map for archive, set it conditionally:

```javascript
        let tooltip = cardMenuTooltips[action.id];
        if (action.id === 'archive' && action.label === 'Restore') {
            tooltip = 'Move back from archive';
        }
        if (tooltip) item.setAttribute('data-tooltip', tooltip);
```

**Step 2: Card three-dot menu trigger — `frontend/js/ui.js`**

In `createClipCard()` (line 717), the menu trigger is created in the HTML template (line 798). Add `data-tooltip="More actions for this clip"` to the button:

Change line 798 from:
```html
                    <button class="card-menu-trigger p-1 text-stone-400 hover:text-stone-600 hover:bg-stone-100 rounded transition-colors"
                            data-action="menu"
```
to:
```html
                    <button class="card-menu-trigger p-1 text-stone-400 hover:text-stone-600 hover:bg-stone-100 rounded transition-colors"
                            data-action="menu"
                            data-tooltip="More actions for this clip"
```

**Step 3: Card drag handle — `frontend/js/ui.js`**

Find the `renderDragHandle` function and replace its `title` attribute with `data-tooltip`. Search for `title="Drag to another app"` in `ui.js` and replace with `data-tooltip="Creates a temp file -- drag into another app to export"`.

**Step 4: Lightbox file menu items — `frontend/js/modals.js`**

In `openLightboxFileMenu()` (line 748), add tooltips to file menu items. Add a tooltip map near the function:

```javascript
const lightboxFileMenuTooltips = {
    'copy-path': 'Create a temp file and copy its path to clipboard',
    'copy-file': 'Place file on clipboard for pasting into other apps',
    'copy-contents': 'Copy the raw text or data to clipboard',
    'save-file': 'Save a copy to your Downloads folder',
    'edit': 'Open in the built-in image editor for annotation',
    'tags': 'Add or remove tags',
    'archive': 'Move to archive without deleting',
    'delete': 'Permanently delete -- this cannot be undone',
};
```

Apply in the item creation loop (around line 782-795):

```javascript
        const tooltip = lightboxFileMenuTooltips[action.id];
        if (tooltip) item.setAttribute('data-tooltip', tooltip);
```

And for the delete item (around line 802):

```javascript
    deleteItem.setAttribute('data-tooltip', lightboxFileMenuTooltips['delete']);
```

Also handle the archive tooltip for restore state: check `isViewingArchive` and set `'Move back from archive'` if true.

**Step 5: Lightbox plugin menu trigger — `frontend/js/modals.js`**

Find where `lightbox-plugin-menu-trigger` is created (line 529). Add `data-tooltip="Run plugin actions on this clip"`.

**Step 6: Lightbox file actions trigger — `frontend/js/modals.js`**

Find where `lightbox-file-menu-trigger` is created (line 727). Add `data-tooltip="File operations and management"`.

**Step 7: Watch folder action buttons — `frontend/js/watch.js`**

Find where pause/resume and remove buttons are created. Replace existing `title` attributes with `data-tooltip`:
- Pause button: replace `title="Pause"` with `data-tooltip="Pause watching this folder"`
- Resume button: replace `title="Resume"` with `data-tooltip="Resume watching this folder"`
- Remove button: replace `title="Remove"` with `data-tooltip="Stop watching this folder and remove it from the list"`

**Step 8: Commit**

```bash
git add frontend/js/ui.js frontend/js/modals.js frontend/js/watch.js
git commit -m "feat: add data-tooltip attributes to all dynamically created elements"
```

---

### Task 6: E2E Tests for Tooltips

Add e2e tests to verify tooltip functionality.

**Files:**
- Create: `e2e/tests/clips/tooltips.spec.ts`
- Modify: `e2e/helpers/selectors.ts` (add tooltip selectors)

**Step 1: Add tooltip helpers to selectors**

In `e2e/helpers/selectors.ts`, add a `tooltips` section:

```typescript
  tooltips: {
    anyTooltip: '[data-tooltip]',
    headerTagFilter: '#tag-filter-btn[data-tooltip]',
    headerArchive: '#header-archive-btn[data-tooltip]',
    headerSort: '#sort-btn[data-tooltip]',
    headerMenu: '#drawer-toggle-btn[data-tooltip]',
    settingsToggle: '[data-testid="tooltips-toggle"]',
  },
```

**Step 2: Create tooltip test file**

Create `e2e/tests/clips/tooltips.spec.ts`:

```typescript
import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
import { generateTestImage, createTempFile } from '../../helpers/test-data.js';
import * as path from 'path';

test.describe('Tooltips', () => {
  test('header buttons have data-tooltip attributes', async ({ app }) => {
    // Verify key header buttons have tooltips
    const tagFilterBtn = app.page.locator('#tag-filter-btn');
    await expect(tagFilterBtn).toHaveAttribute('data-tooltip', /filter/i);

    const archiveBtn = app.page.locator('#header-archive-btn');
    await expect(archiveBtn).toHaveAttribute('data-tooltip', /archive/i);

    const sortBtn = app.page.locator('#sort-btn');
    await expect(sortBtn).toHaveAttribute('data-tooltip', /sort/i);

    const menuBtn = app.page.locator('#drawer-toggle-btn');
    await expect(menuBtn).toHaveAttribute('data-tooltip', /menu/i);
  });

  test('tooltip CSS renders on hover via pseudo-element', async ({ app }) => {
    // Hover over a button and verify the ::after pseudo-element becomes visible
    const btn = app.page.locator('#tag-filter-btn');
    await btn.hover();

    // Wait for the 300ms delay
    await app.page.waitForTimeout(400);

    // Check computed style of ::after pseudo-element
    const opacity = await btn.evaluate((el) => {
      return window.getComputedStyle(el, '::after').opacity;
    });
    expect(opacity).toBe('1');
  });

  test('tooltip disappears when mouse leaves', async ({ app }) => {
    const btn = app.page.locator('#tag-filter-btn');
    await btn.hover();
    await app.page.waitForTimeout(400);

    // Move mouse away
    await app.page.mouse.move(0, 0);
    await app.page.waitForTimeout(200);

    const opacity = await btn.evaluate((el) => {
      return window.getComputedStyle(el, '::after').opacity;
    });
    expect(opacity).toBe('0');
  });

  test('card menu items have tooltips', async ({ app }) => {
    // Upload a file to get a card
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    // Open the card menu
    const menuTrigger = app.page.locator('[data-action="menu"]').first();
    await menuTrigger.click();

    // Check that menu items have data-tooltip attributes
    const copyPathItem = app.page.locator('.card-menu-item[data-action="copy-path"]');
    await expect(copyPathItem).toHaveAttribute('data-tooltip');

    const deleteItem = app.page.locator('.card-menu-item[data-action="delete"]');
    await expect(deleteItem).toHaveAttribute('data-tooltip', /permanently delete/i);
  });

  test('tooltips can be disabled via settings toggle', async ({ app }) => {
    // Open settings
    await app.page.locator('#drawer-toggle-btn').click();
    await app.page.locator('#open-settings-btn').click();

    // Find the tooltips toggle
    const toggle = app.page.locator('[data-testid="tooltips-toggle"]');
    await expect(toggle).toBeChecked();

    // Disable tooltips
    await toggle.click();
    await expect(toggle).not.toBeChecked();

    // Close settings
    await app.page.locator('#settings-close').click();

    // Verify body has disabled class
    await expect(app.page.locator('body')).toHaveClass(/tooltips-disabled/);

    // Hover over a button — tooltip should not render
    const btn = app.page.locator('#tag-filter-btn');
    await btn.hover();
    await app.page.waitForTimeout(400);

    const display = await btn.evaluate((el) => {
      return window.getComputedStyle(el, '::after').display;
    });
    expect(display).toBe('none');
  });

  test('tooltip setting persists across page reload', async ({ app }) => {
    // Open settings and disable tooltips
    await app.page.locator('#drawer-toggle-btn').click();
    await app.page.locator('#open-settings-btn').click();
    const toggle = app.page.locator('[data-testid="tooltips-toggle"]');
    await toggle.click();
    await app.page.locator('#settings-close').click();

    // Wait for setting to be saved
    await app.page.waitForTimeout(200);

    // Reload
    await app.page.reload();
    await app.waitForReady();

    // Verify body still has disabled class
    await expect(app.page.locator('body')).toHaveClass(/tooltips-disabled/);

    // Re-enable for cleanup
    await app.page.locator('#drawer-toggle-btn').click();
    await app.page.locator('#open-settings-btn').click();
    const toggle2 = app.page.locator('[data-testid="tooltips-toggle"]');
    await toggle2.click();
    await app.page.locator('#settings-close').click();
  });

  test('bottom bar elements have tooltips', async ({ app }) => {
    const addBtn = app.page.locator('#add-btn');
    await expect(addBtn).toHaveAttribute('data-tooltip', /upload/i);

    const expirySelect = app.page.locator('#expiry-select');
    await expect(expirySelect).toHaveAttribute('data-tooltip', /auto-delete/i);
  });

  test('bulk toolbar buttons have tooltips when visible', async ({ app }) => {
    // Upload two images to enable bulk mode
    const img1 = await createTempFile(generateTestImage(100, 100, '#ff0000'), 'png');
    const img2 = await createTempFile(generateTestImage(100, 100, '#00ff00'), 'png');
    await app.uploadFile(img1);
    await app.uploadFile(img2);
    await app.expectClipCount(2);

    // Select both clips
    const checkboxes = app.page.locator('.clip-checkbox');
    await checkboxes.first().click();
    await checkboxes.nth(1).click();

    // Verify bulk toolbar buttons have tooltips
    const deleteBtn = app.page.locator('#bulk-delete-btn');
    await expect(deleteBtn).toHaveAttribute('data-tooltip', /permanently delete/i);

    const copyBtn = app.page.locator('#bulk-copy-btn');
    await expect(copyBtn).toHaveAttribute('data-tooltip', /clipboard/i);
  });
});
```

**Step 3: Run tests**

```bash
cd e2e && npm test -- tests/clips/tooltips.spec.ts
```

Expected: All tests pass.

**Step 4: Commit**

```bash
git add e2e/tests/clips/tooltips.spec.ts e2e/helpers/selectors.ts
git commit -m "test: add e2e tests for tooltip display, toggle, and persistence"
```

---

### Task 7: Run Full E2E Test Suite

Run the complete test suite to verify no regressions.

**Step 1: Run all tests**

```bash
cd e2e && npm test
```

Expected: All tests pass. If any fail:
- Read the failure output carefully
- Identify if the failure is related to tooltip changes (e.g., a replaced `title` attribute that a test was checking)
- Fix the issue and re-run

**Step 2: Fix any broken tests**

Common issues to check:
- Tests that assert on `title` attributes that we replaced with `data-tooltip`
- Tests that hover over elements and are affected by tooltip pseudo-elements
- Tests that check element positioning affected by `position: relative` added to `[data-tooltip]`

**Step 3: Commit fixes if any**

```bash
git add -A
git commit -m "fix: resolve tooltip-related test regressions"
```
