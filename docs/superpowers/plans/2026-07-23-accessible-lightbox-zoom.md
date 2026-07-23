# Accessible Lightbox Zoom Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Do not use subagents. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the globally coordinated image lightbox with an accessible `LightboxController` that owns lifecycle, focus, navigation, actual-scale zoom, pan, and keyboard/mouse/touchpad/touch input.

**Architecture:** Add `frontend/js/lightbox.js` as the single deep module behind `open`, `openSingle`, `setClips`, `command`, and `close`. Gallery, shortcuts, Markdown references, plugin actions, and file actions call that interface without mutating lightbox state or DOM. The controller uses stable clip IDs, generation-guarded image loads, a persistent viewport, and nested scale/pan layers.

**Tech Stack:** Vanilla JavaScript, HTML, Tailwind-compatible project CSS, Wails v2 bindings through existing wrappers, Playwright E2E tests.

## Global Constraints

- Run `cd e2e && npm test 2>&1 | tail -50` before the first implementation change and after the final change.
- Follow test-driven development: add one failing behavioral test, observe the expected failure, implement the smallest passing behavior, then rerun the focused suite.
- Do not import Wails runtime outside `internal/wailsbridge`; this feature requires no Go or generated binding changes.
- Keep the existing stone palette, IBM Plex Mono typography, compact sizing, inline SVG style, and focus-visible conventions.
- `100%` means one image pixel per screen pixel; Fit never enlarges a small image above 100%; maximum zoom is 800%.
- Wheel/two-finger scrolling navigates only at Fit when the image is not pannable; both vertical and horizontal axes are supported.
- Do not generalize the viewport for the editor or comparison viewer.
- Do not modify `frontend/wailsjs/` generated files. Restore generated test artifacts before each commit.
- Commit after every task using only the paths listed by that task.

## File structure

### Create

- `frontend/js/lightbox.js` — `createLightboxController(dependencies)` factory and the complete controller implementation.
- `e2e/tests/images/lightbox-controller.spec.ts` — deterministic controller tests using injected image loaders and a detached lightbox fixture.

### Modify

- `frontend/index.html` — persistent viewport/pan layer, loading/error state, integrated toolbar, labels, live region, and script load.
- `frontend/css/modals.css` — replace the lightbox section with viewport-safe, responsive, focus-visible, and reduced-motion styles.
- `frontend/js/app.js` — construct the singleton and route listeners/shortcuts through semantic commands.
- `frontend/js/ui.js` — maintain the rendered clip lookup and open from the visible image snapshot.
- `frontend/js/wails-api.js` — clear/rebuild the rendered clip lookup and atomically replace the controller collection after gallery loads.
- `frontend/js/editor.js` — use `openSingle` for Markdown-linked images.
- `frontend/js/modals.js` — remove legacy lightbox lifecycle, transform, gesture, and menu ownership; retain comparison behavior.
- `frontend/js/context-menu.js` — accept an optional portal root so lightbox menus remain inside its focus model.
- `e2e/helpers/selectors.ts` — centralize every lightbox selector used by tests.
- `e2e/fixtures/test-fixtures.ts` — make helpers wait for active and settled states and use real locator actions.
- `e2e/tests/images/lightbox.spec.ts` — replace weak navigation checks and add lifecycle, zoom, pan, focus, responsive, and accessibility coverage.
- `e2e/tests/shortcuts/shortcuts.spec.ts` — cover Fit, 1:1, keyboard pan, and navigation precedence.
- `e2e/tests/plugins/ui-actions.spec.ts` — cover plugin-menu focus and current-clip targeting after navigation.

---

### Task 1: Make the existing controls testable and visible

**Files:**
- Modify: `e2e/helpers/selectors.ts:139-153`
- Modify: `e2e/fixtures/test-fixtures.ts:971-1008`
- Modify: `e2e/tests/images/lightbox.spec.ts:9-60`
- Modify: `frontend/css/modals.css:357-430`

**Interfaces:**
- Consumes: Existing `AppHelper.openLightbox`, `closeLightbox`, `lightboxNext`, and `lightboxPrev`.
- Produces: Actionability-preserving helpers and selectors used by every later task.

- [ ] **Step 1: Run the required baseline suite**

Run:

```bash
cd e2e && npm test 2>&1 | tail -50
```

Expected: both the main and share suites exit successfully. If a baseline failure appears, diagnose and fix it before continuing.

- [ ] **Step 2: Add selectors for the complete lightbox surface**

Replace the lightbox selector object with:

```typescript
lightbox: {
  overlay: '#lightbox',
  viewport: '#lightbox-viewport',
  panLayer: '#lightbox-pan-layer',
  image: '#lightbox-img',
  caption: '#lightbox-caption',
  status: '#lightbox-status',
  loading: '#lightbox-loading',
  error: '#lightbox-error',
  retryButton: '#lightbox-retry',
  prevButton: '#lightbox-prev',
  nextButton: '#lightbox-next',
  closeButton: '#lightbox-close',
  bar: '.lightbox-bar',
  imageInfo: '#lightbox-image-info',
  fileTrigger: '#lightbox-file-menu-trigger',
  zoomOut: '#lightbox-zoom-out',
  zoomSlider: '#lightbox-zoom-slider',
  zoomIn: '#lightbox-zoom-in',
  zoomFit: '#lightbox-zoom-fit',
  zoomActual: '#lightbox-zoom-actual',
  zoomInfo: '#lightbox-zoom-info',
  pluginActions: '#lightbox-plugin-actions',
  pluginTrigger: '#lightbox-plugin-menu-trigger',
  pluginMenu: '#lightbox-plugin-menu',
  pluginMenuItem: '.lightbox-plugin-menu-item',
},
```

The new selectors may be absent until later tasks; this step only centralizes names.

- [ ] **Step 3: Write a failing desktop actionability test**

Add under `Open and Close`:

```typescript
test('keeps desktop close and navigation controls inside the viewport', async ({ app }) => {
  const files = await Promise.all([
    createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
    createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png'),
  ]);
  await app.uploadFiles(files);
  await app.openLightbox(path.basename(files[1]));

  await expect(app.page.locator(selectors.lightbox.closeButton)).toBeInViewport();
  await expect(app.page.locator(selectors.lightbox.nextButton)).toBeInViewport();
  await app.page.locator(selectors.lightbox.nextButton).click();
  await expect(app.page.locator(selectors.lightbox.caption)).toContainText(path.basename(files[0]));
  await app.page.locator(selectors.lightbox.closeButton).click();
  await expect(app.page.locator(selectors.lightbox.overlay)).not.toHaveClass(/active/);
});
```

- [ ] **Step 4: Run the test and verify the expected failure**

Run:

```bash
cd e2e && npx playwright test tests/images/lightbox.spec.ts --grep "keeps desktop close"
```

Expected: FAIL because the desktop controls are outside the viewport or not actionable.

- [ ] **Step 5: Put desktop controls inside the viewport**

Replace the negative positioning rules with:

```css
.lightbox-close {
    position: absolute;
    top: 1rem;
    right: 1rem;
    z-index: 102;
    color: #a8a29e;
    background: rgba(28, 25, 23, 0.72);
    border: 1px solid rgba(120, 113, 108, 0.35);
    padding: 0.5rem;
    border-radius: 0.375rem;
    cursor: pointer;
    transition: color 0.15s, background 0.15s, border-color 0.15s;
}

.lightbox-nav-prev { left: 1rem; }
.lightbox-nav-next { right: 1rem; }
```

Keep the existing hover colors. Remove the mobile rules that merely undo the negative offsets; retain their darker background treatment.

- [ ] **Step 6: Replace synthetic helper clicks with locator actions and settled waits**

Use:

```typescript
async openLightbox(filename: string): Promise<void> {
  await this.viewClip(filename);
  await this.page.locator(`${selectors.lightbox.overlay}.active`).waitFor({ state: 'visible' });
  await expect(this.page.locator(selectors.lightbox.caption)).toContainText(filename);
  await expect.poll(async () => {
    const image = this.page.locator(selectors.lightbox.image);
    return image.evaluate((element: HTMLImageElement) => element.complete && element.naturalWidth > 0);
  }).toBe(true);
}

async closeLightbox(): Promise<void> {
  await this.page.locator(selectors.lightbox.closeButton).click();
  await expect(this.page.locator(selectors.lightbox.overlay)).not.toHaveClass(/active/);
}

async lightboxNext(): Promise<void> {
  await this.page.locator(selectors.lightbox.nextButton).click();
}

async lightboxPrev(): Promise<void> {
  await this.page.locator(selectors.lightbox.prevButton).click();
}
```

- [ ] **Step 7: Run the focused lightbox suite**

Run:

```bash
cd e2e && npx playwright test tests/images/lightbox.spec.ts
```

Expected: PASS, including the new real-click test.

- [ ] **Step 8: Commit**

```bash
git restore frontend/wailsjs/go/models.ts 2>/dev/null || true
git add frontend/css/modals.css e2e/helpers/selectors.ts e2e/fixtures/test-fixtures.ts e2e/tests/images/lightbox.spec.ts
git commit -m "fix(lightbox): keep viewer controls actionable"
```

---

### Task 2: Introduce the controller and migrate open, close, and navigation

**Files:**
- Create: `frontend/js/lightbox.js`
- Modify: `frontend/index.html:2110-2145`
- Modify: `frontend/js/app.js:121-126, 171-173, 547-556, 1460-1535`
- Modify: `frontend/js/ui.js:1008-1031`
- Modify: `frontend/js/editor.js:202-232`
- Modify: `frontend/js/modals.js:415-522`
- Test: `e2e/tests/images/lightbox.spec.ts`

**Interfaces:**
- Consumes: `getImageDataUrl(clip.id)`, `trapFocus(root)`, existing plugin/file action renderers, `openEditor`, and `copyClipContents`.
- Produces: `window.createLightboxController(dependencies)` and singleton `window.LightboxController` with `open`, `openSingle`, `setClips`, `command`, and `close`.

- [ ] **Step 1: Write a failing exact-focus test**

Add under `Focus Management`:

```typescript
test('restores the exact opener after keyboard navigation', async ({ app }) => {
  const files = await Promise.all([
    createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
    createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png'),
  ]);
  await app.uploadFiles(files);

  const opener = await app.getClipByFilename(path.basename(files[1]));
  await opener.focus();
  await opener.press('Enter');
  await expect(app.page.locator(selectors.lightbox.overlay)).toHaveClass(/active/);
  await app.page.keyboard.press('ArrowRight');
  await app.page.keyboard.press('Escape');

  await expect(opener).toBeFocused();
});
```

- [ ] **Step 2: Run the test and verify the expected failure**

Run:

```bash
cd e2e && npx playwright test tests/images/lightbox.spec.ts --grep "restores the exact opener"
```

Expected: FAIL because navigation overwrites `lastFocusedElementBeforeLightbox`.

- [ ] **Step 3: Add the controller factory**

Create `frontend/js/lightbox.js` with this initial controller shape:

```javascript
(() => {
    function createLightboxController(deps) {
        const state = {
            phase: 'closed',
            clips: [],
            currentId: null,
            opener: null,
            focusTrapCleanup: null,
        };

        function currentClip() {
            return state.clips.find(clip => clip.id === state.currentId) || null;
        }

        function currentIndex() {
            return state.clips.findIndex(clip => clip.id === state.currentId);
        }

        function renderBoundaryState() {
            const index = currentIndex();
            const hasPrevious = index > 0;
            const hasNext = index >= 0 && index < state.clips.length - 1;
            deps.elements.previous.hidden = !hasPrevious;
            deps.elements.previous.disabled = !hasPrevious;
            deps.elements.next.hidden = !hasNext;
            deps.elements.next.disabled = !hasNext;
        }

        async function renderCurrent() {
            const clip = currentClip();
            if (!clip) return;
            deps.resetViewport();
            const dataURL = await deps.loadImage(clip);
            deps.elements.image.src = dataURL;
            deps.elements.image.alt = clip.filename || 'Image preview';
            deps.elements.caption.textContent = clip.filename || 'Pasted Image';
            renderBoundaryState();
            deps.renderPluginActions(clip);
            deps.renderFileActions(clip);
        }

        async function open({ clips, currentId, opener }) {
            const wasClosed = state.phase === 'closed';
            state.clips = [...clips];
            state.currentId = currentId;
            if (wasClosed) state.opener = opener || document.activeElement;
            state.phase = 'loading';
            deps.elements.root.removeAttribute('inert');
            deps.elements.root.classList.add('active');
            if (state.focusTrapCleanup) state.focusTrapCleanup();
            state.focusTrapCleanup = deps.trapFocus(deps.elements.root);
            if (wasClosed) deps.elements.close.focus();
            await renderCurrent();
            if (state.phase !== 'closed') state.phase = 'ready';
        }

        function openSingle({ clip, opener }) {
            return open({ clips: [clip], currentId: clip.id, opener });
        }

        function setClips(clips) {
            state.clips = [...clips];
            if (state.phase === 'closed') return;
            if (!currentClip()) {
                if (state.clips.length === 0) {
                    close();
                    return;
                }
                state.currentId = state.clips[0].id;
                void renderCurrent();
            } else {
                renderBoundaryState();
            }
        }

        function navigate(delta) {
            const index = currentIndex();
            const target = state.clips[index + delta];
            if (!target) return;
            state.currentId = target.id;
            void renderCurrent();
        }

        function command(name, detail = {}) {
            if (name === 'next') navigate(1);
            else if (name === 'previous') navigate(-1);
            else if (name === 'edit') {
                const clip = currentClip();
                if (clip) deps.editClip(clip);
            } else if (name === 'copy') {
                const clip = currentClip();
                if (clip) deps.copyClip(clip);
            } else if (name === 'close') close();
            else deps.viewportCommand(name, detail);
        }

        function close() {
            if (state.phase === 'closed') return;
            state.phase = 'closed';
            if (state.focusTrapCleanup) state.focusTrapCleanup();
            state.focusTrapCleanup = null;
            deps.closeMenus();
            deps.elements.root.classList.remove('active');
            deps.elements.root.setAttribute('inert', '');
            deps.resetViewport();
            const opener = state.opener;
            state.opener = null;
            deps.restoreFocus(opener);
        }

        return Object.freeze({ open, openSingle, setClips, command, close });
    }

    window.createLightboxController = createLightboxController;
})();
```

This task intentionally delegates the old transform implementation through `resetViewport` and `viewportCommand`; Task 5 removes that temporary internal seam.

- [ ] **Step 4: Load the factory before callers and construct the singleton**

Add before `ui.js`:

```html
<script src="js/lightbox.js"></script>
```

In `app.js`, construct the singleton immediately after the lightbox DOM references. Retain `imageClips` and `currentLightboxIndex` only for the opening adapter in this task; Task 4 removes both globals:

```javascript
window.LightboxController = window.createLightboxController({
    elements: {
        root: lightbox,
        image: document.getElementById('lightbox-img'),
        caption: lightboxCaption,
        close: lightboxClose,
        previous: lightboxPrev,
        next: lightboxNext,
    },
    loadImage: clip => getImageDataUrl(clip.id),
    trapFocus,
    resetViewport: resetLightboxZoom,
    viewportCommand: (name) => {
        if (name === 'zoom-in' || name === 'zoom-out') {
            const slider = document.getElementById('lightbox-zoom-slider');
            if (!slider) return;
            const delta = name === 'zoom-in' ? 25 : -25;
            slider.value = Math.max(100, Math.min(400, Number(slider.value) + delta));
            slider.dispatchEvent(new Event('input'));
        }
    },
    renderPluginActions: clip => renderLightboxPluginButtons(clip),
    renderFileActions: clip => renderLightboxFileActions(clip),
    editClip: clip => {
        window.LightboxController.close();
        setTimeout(() => openEditor(clip.id), LIGHTBOX_CLOSE_DELAY);
    },
    copyClip: clip => copyClipContents(clip.id),
    closeMenus: () => {
        closeLightboxPluginMenu(true);
        closeLightboxFileMenu(true);
    },
    restoreFocus: opener => {
        if (opener?.isConnected && opener.getClientRects().length > 0) opener.focus();
        else gallery.focus();
    },
});
```

Update action rendering in `modals.js` so every callback closes over the clip supplied by the controller. Make these exact signature replacements:

```javascript
async function renderLightboxPluginButtons(clip)
function openLightboxPluginMenu(trigger, actions, clip)
async function handleLightboxPluginAction(action, clip)
function renderLightboxFileActions(clip)
function openLightboxFileMenu(trigger, clip)
async function handleLightboxFileAction(action, clip)
```

Delete each `const clip = imageClips[currentLightboxIndex]` statement from those functions. Replace plugin trigger/item calls with:

```javascript
openLightboxPluginMenu(btn, actions, clip);
handleLightboxPluginAction(action, clip);
```

Use this complete plugin action body:

```javascript
async function handleLightboxPluginAction(action, clip) {
    if (!clip) return;
    if (action.options?.length > 0) {
        openPluginOptionsDialog(action, [clip.id]);
    } else {
        await executePluginAction(action.plugin_id, action.id, [clip.id], {}, action.async);
    }
}
```

Replace file trigger/action calls with:

```javascript
openLightboxFileMenu(btn, clip);
handleLightboxFileAction(action, clip);
```

At the start of `openLightboxFileMenu`, use:

```javascript
if (!clip) return;
const items = buildMenuItemList(clip);
```

At the start of `handleLightboxFileAction`, use `if (!clip) return;` and leave its existing action switch below that guard.

- [ ] **Step 5: Route open/close/navigation through the controller**

Use these event handlers in `app.js`:

```javascript
lightboxClose.addEventListener('click', () => window.LightboxController.close());
lightboxPrev.addEventListener('click', (event) => {
    event.stopPropagation();
    window.LightboxController.command('previous');
});
lightboxNext.addEventListener('click', (event) => {
    event.stopPropagation();
    window.LightboxController.command('next');
});
lightbox.addEventListener('click', (event) => {
    if (event.target === lightbox) window.LightboxController.close();
});
```

In `createClipCard`, replace index-based opening with:

```javascript
const lightboxTrigger = card.querySelector('[data-action="open-lightbox"]');
lightboxTrigger.addEventListener('click', (event) => {
    window.LightboxController.open({
        clips: imageClips,
        currentId: clip.id,
        opener: card,
    });
});
```

Replace shortcut callbacks with:

```javascript
ShortcutManager.register({
    id: 'lightbox-close', label: 'Close Lightbox', category: 'lightbox',
    defaultKey: 'Escape', context: 'lightbox',
    callback: () => {
        if (ContextMenu.isOpen()) {
            ContextMenu.close();
            document.getElementById('lightbox-file-menu-trigger')?.focus();
            return;
        }
        if (document.getElementById('lightbox-plugin-menu')) {
            closeLightboxPluginMenu();
            document.getElementById('lightbox-plugin-menu-trigger')?.focus();
            return;
        }
        window.LightboxController.close();
    },
});
ShortcutManager.register({ id: 'lightbox-next', label: 'Next Image', category: 'lightbox', defaultKey: 'ArrowRight', context: 'lightbox', callback: () => window.LightboxController.command('next') });
ShortcutManager.register({ id: 'lightbox-prev', label: 'Previous Image', category: 'lightbox', defaultKey: 'ArrowLeft', context: 'lightbox', callback: () => window.LightboxController.command('previous') });
ShortcutManager.register({ id: 'lightbox-zoom-in', label: 'Zoom In', category: 'lightbox', defaultKey: '+', context: 'lightbox', callback: () => window.LightboxController.command('zoom-in') });
ShortcutManager.register({ id: 'lightbox-zoom-out', label: 'Zoom Out', category: 'lightbox', defaultKey: '-', context: 'lightbox', callback: () => window.LightboxController.command('zoom-out') });
ShortcutManager.register({ id: 'lightbox-edit', label: 'Open Editor', category: 'lightbox', defaultKey: 'e', context: 'lightbox', callback: () => window.LightboxController.command('edit') });
if (window.mahpastesMode !== 'server') {
    ShortcutManager.register({ id: 'lightbox-copy', label: 'Copy Image', category: 'lightbox', defaultKey: 'mod+c', context: 'lightbox', callback: () => window.LightboxController.command('copy') });
}
```

Keep `imageClips` only until Task 4 removes collection ownership from callers.

- [ ] **Step 6: Remove the legacy lifecycle functions**

Delete these definitions from `modals.js` after all call sites are routed:

```text
openLinkedImageLightbox
openLightbox
closeLightbox
showNextImage
showPrevImage
updateLightboxNav
```

During this migration task, replace the Markdown reference call with:

```javascript
await window.LightboxController.openSingle({ clip: candidateClip, opener: document.activeElement });
```

- [ ] **Step 7: Run focused lifecycle and shortcut tests**

Run:

```bash
cd e2e
npx playwright test tests/images/lightbox.spec.ts
npx playwright test tests/shortcuts/shortcuts.spec.ts --grep "Lightbox"
```

Expected: PASS, including exact opener restoration.

- [ ] **Step 8: Commit**

```bash
git restore frontend/wailsjs/go/models.ts 2>/dev/null || true
git add frontend/js/lightbox.js frontend/index.html frontend/js/app.js frontend/js/ui.js frontend/js/editor.js frontend/js/modals.js e2e/tests/images/lightbox.spec.ts
git commit -m "refactor(lightbox): centralize viewer lifecycle"
```

---

### Task 3: Guard asynchronous loads and add loading/error states

**Files:**
- Create: `e2e/tests/images/lightbox-controller.spec.ts`
- Modify: `frontend/js/lightbox.js`
- Modify: `frontend/js/app.js`
- Modify: `frontend/index.html:711-752`
- Modify: `frontend/css/modals.css:20-52`
- Modify: `e2e/tests/images/lightbox.spec.ts`

**Interfaces:**
- Consumes: `createLightboxController(dependencies)` from Task 2.
- Produces: generation-guarded phases `closed`, `loading`, `ready`, and `error`; `command('retry')`.

- [ ] **Step 1: Add a deterministic controller fixture**

Create `lightbox-controller.spec.ts` with a browser helper that mounts detached elements and injects a deferred loader:

```typescript
import { test, expect } from '../../fixtures/test-fixtures.js';
import type { Page } from '@playwright/test';

async function mountController(page: Page) {
  return page.evaluate(() => {
    const host = document.createElement('div');
    host.innerHTML = `
      <div data-root inert>
        <button data-close>Close</button>
        <button data-prev>Previous</button>
        <button data-next>Next</button>
        <div data-viewport aria-busy="false">
          <img data-image>
          <div data-loading hidden>Loading</div>
          <div data-error hidden><button data-retry>Retry</button></div>
        </div>
        <p data-caption></p>
        <div data-status></div>
      </div>`;
    document.body.appendChild(host);

    const pending = new Map<number, { resolve: (value: string) => void; reject: (error: Error) => void }>();
    const loadImage = (clip: { id: number }) => new Promise<string>((resolve, reject) => {
      pending.set(clip.id, { resolve, reject });
    });
    const query = (selector: string) => host.querySelector(selector);
    // @ts-ignore classic-script factory
    const controller = window.createLightboxController({
      elements: {
        root: query('[data-root]'),
        viewport: query('[data-viewport]'),
        image: query('[data-image]'),
        caption: query('[data-caption]'),
        status: query('[data-status]'),
        loading: query('[data-loading]'),
        error: query('[data-error]'),
        retry: query('[data-retry]'),
        close: query('[data-close]'),
        previous: query('[data-prev]'),
        next: query('[data-next]'),
      },
      loadImage,
      trapFocus: () => () => {},
      resetViewport: () => {},
      viewportCommand: () => {},
      renderPluginActions: () => {},
      renderFileActions: () => {},
      editClip: () => {},
      copyClip: () => {},
      closeMenus: () => {},
      restoreFocus: () => {},
      onImageReady: () => {},
      reportError: () => {},
    });
    // @ts-ignore browser-only controller harness
    window.__lightboxHarness = { host, pending, controller };
    return true;
  });
}

test.afterEach(async ({ page }) => {
  await page.evaluate(() => {
    // @ts-ignore browser-only controller harness
    window.__lightboxHarness?.host.remove();
    // @ts-ignore browser-only controller harness
    delete window.__lightboxHarness;
  });
});
```
- [ ] **Step 2: Write failing stale-load and close-during-load tests**

Add:

```typescript
test('ignores an older image response that resolves last', async ({ page }) => {
  await mountController(page);
  await page.evaluate(() => {
    // @ts-ignore browser-only controller harness
    const h = window.__lightboxHarness;
    void h.controller.open({ clips: [{ id: 1, filename: 'one.png' }, { id: 2, filename: 'two.png' }], currentId: 1, opener: null });
    h.controller.command('next');
    h.pending.get(2).resolve('data:image/png;base64,dHdv');
  });
  await expect(page.locator('[data-caption]')).toHaveText('two.png');
  await page.evaluate(() => {
    // @ts-ignore browser-only controller harness
    window.__lightboxHarness.pending.get(1).resolve('data:image/png;base64,b25l');
  });
  await expect(page.locator('[data-caption]')).toHaveText('two.png');
  await expect(page.locator('[data-image]')).toHaveAttribute('src', 'data:image/png;base64,dHdv');
});

test('does not reopen after closing during a pending load', async ({ page }) => {
  await mountController(page);
  await page.evaluate(() => {
    // @ts-ignore browser-only controller harness
    const h = window.__lightboxHarness;
    void h.controller.open({ clips: [{ id: 1, filename: 'one.png' }], currentId: 1, opener: null });
    h.controller.close();
    h.pending.get(1).resolve('data:image/png;base64,b25l');
  });
  await expect(page.locator('[data-root]')).not.toHaveClass(/active/);
  await expect(page.locator('[data-root]')).toHaveAttribute('inert', '');
});

test('keeps a reopened image when the previous request resolves', async ({ page }) => {
  await mountController(page);
  await page.evaluate(() => {
    // @ts-ignore browser-only controller harness
    const h = window.__lightboxHarness;
    void h.controller.open({ clips: [{ id: 1, filename: 'one.png' }], currentId: 1, opener: null });
    h.controller.close();
    void h.controller.open({ clips: [{ id: 2, filename: 'two.png' }], currentId: 2, opener: null });
    h.pending.get(2).resolve('data:image/png;base64,dHdv');
    h.pending.get(1).resolve('data:image/png;base64,b25l');
  });
  await expect(page.locator('[data-caption]')).toHaveText('two.png');
  await expect(page.locator('[data-image]')).toHaveAttribute('src', 'data:image/png;base64,dHdv');
});

test('shows an error and retries the selected clip', async ({ page }) => {
  await mountController(page);
  await page.evaluate(() => {
    // @ts-ignore browser-only controller harness
    const h = window.__lightboxHarness;
    void h.controller.open({ clips: [{ id: 1, filename: 'one.png' }], currentId: 1, opener: null });
    h.pending.get(1).reject(new Error('load failed'));
  });
  await expect(page.locator('[data-error]')).toBeVisible();
  await page.evaluate(() => {
    // @ts-ignore browser-only controller harness
    const h = window.__lightboxHarness;
    h.controller.command('retry');
    h.pending.get(1).resolve('data:image/png;base64,b25l');
  });
  await expect(page.locator('[data-image]')).toHaveAttribute('src', 'data:image/png;base64,b25l');
});
```
- [ ] **Step 3: Run the tests and verify they fail**

Run:

```bash
cd e2e && npx playwright test tests/images/lightbox-controller.spec.ts
```

Expected: FAIL because Task 2 has no request generation guard.

- [ ] **Step 4: Add generation-guarded loading**

Extend state with:

```javascript
requestGeneration: 0,
```

Replace `renderCurrent` with:

```javascript
async function renderCurrent() {
    const clip = currentClip();
    if (!clip || state.phase === 'closed') return;
    const generation = ++state.requestGeneration;
    state.phase = 'loading';
    deps.elements.viewport.setAttribute('aria-busy', 'true');
    deps.elements.loading.hidden = false;
    deps.elements.error.hidden = true;
    deps.elements.image.hidden = true;
    deps.elements.caption.textContent = clip.filename || 'Pasted Image';
    renderBoundaryState();

    try {
        const dataURL = await deps.loadImage(clip);
        if (generation !== state.requestGeneration || state.phase === 'closed' || state.currentId !== clip.id) return;
        let committed = false;
        const commitReady = () => {
            if (committed || generation !== state.requestGeneration || state.currentId !== clip.id) return;
            committed = true;
            state.phase = 'ready';
            deps.elements.viewport.setAttribute('aria-busy', 'false');
            deps.elements.loading.hidden = true;
            deps.elements.image.hidden = false;
            deps.elements.status.textContent = `${clip.filename || 'Image'} loaded, ${currentIndex() + 1} of ${state.clips.length}`;
            deps.onImageReady(clip, deps.elements.image);
        };
        deps.elements.image.addEventListener('load', commitReady, { once: true });
        deps.elements.image.alt = clip.filename || 'Image preview';
        deps.elements.image.src = dataURL;
        if (deps.elements.image.complete && deps.elements.image.naturalWidth > 0) {
            queueMicrotask(commitReady);
        }
        deps.renderPluginActions(clip);
        deps.renderFileActions(clip);
    } catch (error) {
        if (generation !== state.requestGeneration || state.phase === 'closed' || state.currentId !== clip.id) return;
        state.phase = 'error';
        deps.elements.viewport.setAttribute('aria-busy', 'false');
        deps.elements.loading.hidden = true;
        deps.elements.error.hidden = false;
        deps.elements.status.textContent = `Failed to load ${clip.filename || 'image'}`;
        deps.reportError(error, clip);
    }
}
```

In `close`, increment `state.requestGeneration`. In `command`, route `retry` to `renderCurrent()` only when `state.phase === 'error'`.

- [ ] **Step 5: Add persistent loading/error markup**

Inside the lightbox viewport add:

```html
<div id="lightbox-loading" class="lightbox-state" role="status" hidden>
    <svg class="lightbox-spinner" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9"></circle></svg>
    <span>Loading image…</span>
</div>
<div id="lightbox-error" class="lightbox-state" hidden>
    <p>Could not load this image.</p>
    <button id="lightbox-retry" class="lightbox-state-button">Retry</button>
</div>
<div id="lightbox-status" class="sr-only" aria-live="polite" aria-atomic="true"></div>
```

Wire Retry to `LightboxController.command('retry')`. Add these elements and callbacks to the singleton dependencies in `app.js`:

```javascript
viewport: document.getElementById('lightbox-viewport'),
status: document.getElementById('lightbox-status'),
loading: document.getElementById('lightbox-loading'),
error: document.getElementById('lightbox-error'),
retry: document.getElementById('lightbox-retry'),
```

```javascript
onImageReady: () => updateLightboxImageInfo(),
reportError: (error, clip) => console.error(`Failed to load ${clip.filename || 'image'}:`, error),
```

Register:

```javascript
document.getElementById('lightbox-retry').addEventListener('click', () => {
    window.LightboxController.command('retry');
});
```

Add state styling:

```css
.lightbox-state {
    position: absolute;
    inset: 0;
    z-index: 5;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.75rem;
    color: #d6d3d1;
    background: rgba(28, 25, 23, 0.72);
    font-size: 0.75rem;
}

.lightbox-state[hidden] { display: none; }
.lightbox-spinner { width: 1.5rem; height: 1.5rem; animation: spin 0.8s linear infinite; }
.lightbox-spinner circle { fill: none; stroke: currentColor; stroke-width: 2; stroke-dasharray: 42 16; }
.lightbox-state-button {
    min-height: 2rem;
    padding: 0.375rem 0.75rem;
    border: 1px solid #78716c;
    border-radius: 0.375rem;
    color: #fafaf9;
    background: #44403c;
}
```

Do not remove the image element on close.

- [ ] **Step 6: Run controller and integrated lightbox tests**

Run:

```bash
cd e2e
npx playwright test tests/images/lightbox-controller.spec.ts
npx playwright test tests/images/lightbox.spec.ts
```

Expected: PASS. Confirm failed loads show Retry, Previous/Next remain usable, and stale completions do not change the requested clip.

- [ ] **Step 7: Commit**

```bash
git restore frontend/wailsjs/go/models.ts 2>/dev/null || true
git add frontend/js/lightbox.js frontend/js/app.js frontend/index.html frontend/css/modals.css e2e/tests/images/lightbox-controller.spec.ts e2e/tests/images/lightbox.spec.ts
git commit -m "fix(lightbox): guard asynchronous image loading"
```

---

### Task 4: Replace global arrays with visible stable-ID collections

**Files:**
- Modify: `frontend/js/ui.js:900-1040, 1215-1233`
- Modify: `frontend/js/wails-api.js:6-105`
- Modify: `frontend/js/editor.js:202-232`
- Modify: `frontend/js/app.js:171-173, 1516-1535`
- Modify: `frontend/js/lightbox.js`
- Modify: `frontend/js/modals.js`
- Modify: `e2e/tests/images/lightbox.spec.ts`
- Modify: `e2e/tests/search/filtering.spec.ts`

**Interfaces:**
- Consumes: `LightboxController.open`, `openSingle`, and `setClips`.
- Produces: `getVisibleImageClips()` in `ui.js`; no `imageClips`, `currentLightboxIndex`, or linked-array restoration globals.

- [ ] **Step 1: Write failing visible-search navigation coverage**

Add to `search/filtering.spec.ts`:

```typescript
test('lightbox navigation includes only search-visible images', async ({ app }) => {
  const matchingSource = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
  const hiddenSource = await createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png');
  const matching = path.join(path.dirname(matchingSource), `match-image-${uniqueId()}.png`);
  const hidden = path.join(path.dirname(hiddenSource), `other-image-${uniqueId()}.png`);
  await fs.rename(matchingSource, matching);
  await fs.rename(hiddenSource, hidden);
  await app.uploadFiles([matching, hidden]);
  await app.page.locator(selectors.searchInput).fill('match-image');
  await app.openLightbox(path.basename(matching));

  await expect(app.page.locator(selectors.lightbox.prevButton)).toBeHidden();
  await expect(app.page.locator(selectors.lightbox.nextButton)).toBeHidden();
  await app.page.keyboard.press('ArrowRight');
  await expect(app.page.locator(selectors.lightbox.caption)).toContainText(path.basename(matching));
});
```

Add `import * as fs from 'fs/promises';` to the test file; `path` and `uniqueId` are already imported.

- [ ] **Step 2: Run and verify the failure**

Run:

```bash
cd e2e && npx playwright test tests/search/filtering.spec.ts --grep "search-visible images"
```

Expected: FAIL because `imageClips` still contains hidden cards.

- [ ] **Step 3: Add a rendered clip lookup and visible snapshot function**

In `ui.js` add:

```javascript
const renderedClipsById = new Map();

function rememberRenderedClip(clip) {
    renderedClipsById.set(Number(clip.id), clip);
}

function clearRenderedClips() {
    renderedClipsById.clear();
}

function getVisibleImageClips() {
    return Array.from(gallery.querySelectorAll(':scope > li[data-id]'))
        .filter(card => card.style.display !== 'none' && card.getClientRects().length > 0)
        .map(card => renderedClipsById.get(Number(card.dataset.id)))
        .filter(clip => clip && clip.content_type.startsWith('image/'));
}
```

Call `rememberRenderedClip(clip)` from `createClipCard`. Remove every mutation of `imageClips` and `currentLightboxIndex`.

- [ ] **Step 4: Open from the visible snapshot and update it after search**

Use:

```javascript
lightboxTrigger.addEventListener('click', event => {
    window.LightboxController.open({
        clips: getVisibleImageClips(),
        currentId: clip.id,
        opener: card,
    });
});
```

At the end of `applySearchFilter`, add:

```javascript
window.LightboxController?.setClips(getVisibleImageClips());
```

- [ ] **Step 5: Replace the collection atomically after gallery loads**

In `loadClips`, call `clearRenderedClips()` with `gallery.innerHTML = ''`. After all cards and the search filter are rendered, call:

```javascript
window.LightboxController?.setClips(getVisibleImageClips());
```

Do not call `setClips` during each card append.

- [ ] **Step 6: Replace linked-image array swapping**

In `editor.js`, use:

```javascript
await window.LightboxController.openSingle({
    clip: {
        id: candidate.clip_id,
        filename,
        content_type: contentType,
        is_archived: candidate.is_archived || false,
    },
    opener: document.activeElement,
});
```

Delete `linkedLightboxPreviousClips` and all restoration code from `modals.js`.

- [ ] **Step 7: Preserve the former index during `setClips`**

Replace the missing-current branch with:

```javascript
const previousIndex = currentIndex();
state.clips = [...clips];
if (state.phase === 'closed') return;
if (state.clips.length === 0) {
    close();
    return;
}
if (!currentClip()) {
    const nextIndex = Math.max(0, Math.min(previousIndex, state.clips.length - 1));
    state.currentId = state.clips[nextIndex].id;
    void renderCurrent();
} else {
    renderBoundaryState();
}
```

Capture `previousIndex` before replacing `state.clips`.

- [ ] **Step 8: Run search, lightbox, and Markdown reference tests**

Run:

```bash
cd e2e
npx playwright test tests/search/filtering.spec.ts --grep "lightbox|search-visible"
npx playwright test tests/images/lightbox.spec.ts
npx playwright test tests/maintenance/markdown-image-cache.spec.ts
```

Expected: PASS with no global image collection references remaining.

- [ ] **Step 9: Commit**

```bash
git restore frontend/wailsjs/go/models.ts 2>/dev/null || true
git add frontend/js/ui.js frontend/js/wails-api.js frontend/js/editor.js frontend/js/app.js frontend/js/lightbox.js frontend/js/modals.js e2e/tests/images/lightbox.spec.ts e2e/tests/search/filtering.spec.ts
git commit -m "refactor(lightbox): navigate visible clips by stable id"
```

---

### Task 5: Implement actual-scale zoom and the integrated toolbar

**Files:**
- Modify: `frontend/index.html:711-752`
- Modify: `frontend/js/lightbox.js`
- Modify: `frontend/js/app.js:547-556`
- Modify: `frontend/js/modals.js:1-175, 884-922`
- Modify: `frontend/css/modals.css:20-65, 290-357`
- Modify: `e2e/tests/images/lightbox-controller.spec.ts`
- Modify: `e2e/tests/images/lightbox.spec.ts:241-273`

**Interfaces:**
- Consumes: `onImageReady(clip, image)` lifecycle hook from Task 3.
- Produces: controller-owned `fitScale`, `scale`, `panX`, `panY`, logarithmic slider mapping, and commands `zoom-in`, `zoom-out`, `fit`, and `actual-size`.

- [ ] **Step 1: Write failing Fit, 1:1, and 800% tests**

Add integrated tests:

```typescript
test('uses actual image scale for Fit and 1:1', async ({ app }) => {
  const imagePath = await createTempFile(generateTestImage(2000, 1000), 'png');
  await app.uploadFile(imagePath);
  await app.openLightbox(path.basename(imagePath));

  const fitText = await app.page.locator(selectors.lightbox.zoomInfo).textContent();
  expect(Number.parseInt(fitText || '100', 10)).toBeLessThan(100);
  await app.page.locator(selectors.lightbox.zoomActual).click();
  await expect(app.page.locator(selectors.lightbox.zoomInfo)).toHaveText('100%');
  await app.page.locator(selectors.lightbox.zoomFit).click();
  await expect(app.page.locator(selectors.lightbox.zoomInfo)).toHaveText(fitText || '');
});

test('clamps actual scale at 800 percent', async ({ app }) => {
  const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
  await app.uploadFile(imagePath);
  await app.openLightbox(path.basename(imagePath));
  await app.page.locator(selectors.lightbox.zoomSlider).press('End');
  await expect(app.page.locator(selectors.lightbox.zoomInfo)).toHaveText('800%');
});

test('recomputes Fit when the viewport changes', async ({ app }) => {
  const imagePath = await createTempFile(generateTestImage(2000, 1000), 'png');
  await app.uploadFile(imagePath);
  await app.openLightbox(path.basename(imagePath));
  const before = Number.parseInt(await app.page.locator(selectors.lightbox.zoomInfo).textContent() || '100', 10);
  await app.page.setViewportSize({ width: 640, height: 600 });
  await expect.poll(async () => Number.parseInt(
    await app.page.locator(selectors.lightbox.zoomInfo).textContent() || '100',
    10,
  )).toBeLessThan(before);
  await expect(app.page.locator(selectors.lightbox.zoomFit)).toHaveAttribute('aria-pressed', 'true');
});
```

- [ ] **Step 2: Run and verify the tests fail**

Run:

```bash
cd e2e && npx playwright test tests/images/lightbox.spec.ts --grep "actual image scale|800 percent"
```

Expected: FAIL because the current slider is a 100–400% Fit multiplier and there are no Fit/1:1 buttons.

- [ ] **Step 3: Add nested pan and scale layers**

Move close/navigation controls into the viewport and use this structure above the bottom bar:

```html
<div id="lightbox-viewport" class="lightbox-viewport" aria-busy="false">
    <button id="lightbox-close" class="lightbox-close" aria-label="Close lightbox" data-tooltip="Close viewer (Esc)">
        <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M6 18L18 6M6 6l12 12"></path>
        </svg>
    </button>
    <button id="lightbox-prev" class="lightbox-nav lightbox-nav-prev" aria-label="Previous image" data-tooltip="Previous clip (Left)">
        <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M15 19l-7-7 7-7"></path>
        </svg>
    </button>
    <div id="lightbox-pan-layer" class="lightbox-pan-layer">
        <img id="lightbox-img" src="" alt="" class="lightbox-image" draggable="false">
    </div>
    <button id="lightbox-next" class="lightbox-nav lightbox-nav-next" aria-label="Next image" data-tooltip="Next clip (Right)">
        <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M9 5l7 7-7 7"></path>
        </svg>
    </button>
    <div id="lightbox-loading" class="lightbox-state" role="status" hidden>
        <svg class="lightbox-spinner" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9"></circle></svg>
        <span>Loading image…</span>
    </div>
    <div id="lightbox-error" class="lightbox-state" hidden>
        <p>Could not load this image.</p>
        <button id="lightbox-retry" class="lightbox-state-button">Retry</button>
    </div>
    <div id="lightbox-status" class="sr-only" aria-live="polite" aria-atomic="true"></div>
</div>
```

The pan layer handles viewport-pixel translation. The image handles actual scale, preventing pan translation from being multiplied by zoom.

Replace the base layout and image constraints with:

```css
.lightbox-backdrop {
    display: grid;
    grid-template-rows: minmax(0, 1fr) auto;
}

.lightbox-viewport {
    position: relative;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
    touch-action: none;
}

.lightbox-pan-layer {
    position: absolute;
    left: 50%;
    top: 50%;
    transform: translate(-50%, -50%);
    will-change: transform;
}

.lightbox-image {
    display: block;
    width: auto;
    height: auto;
    max-width: none;
    max-height: none;
    transform-origin: center;
    will-change: transform;
}
```

- [ ] **Step 4: Add zoom state and pure mappings**

Inside the controller add:

```javascript
const MAX_SCALE = 8;
const ZOOM_FACTOR = 1.2;
const SLIDER_STEPS = 100;

Object.assign(state, {
    naturalWidth: 0,
    naturalHeight: 0,
    viewportWidth: 0,
    viewportHeight: 0,
    fitScale: 1,
    scale: 1,
    panX: 0,
    panY: 0,
});

function computeFitScale() {
    const widthScale = state.viewportWidth / state.naturalWidth;
    const heightScale = state.viewportHeight / state.naturalHeight;
    return Math.min(1, widthScale, heightScale);
}

function sliderPositionToScale(position) {
    if (state.fitScale >= MAX_SCALE) return MAX_SCALE;
    return state.fitScale * Math.pow(MAX_SCALE / state.fitScale, Number(position) / SLIDER_STEPS);
}

function scaleToSliderPosition(scale) {
    if (state.fitScale >= MAX_SCALE) return SLIDER_STEPS;
    return SLIDER_STEPS * Math.log(scale / state.fitScale) / Math.log(MAX_SCALE / state.fitScale);
}
```

- [ ] **Step 5: Add focal zoom, constraints, and rendering**

Use:

```javascript
function constrainPan() {
    const maxX = Math.max(0, (state.naturalWidth * state.scale - state.viewportWidth) / 2);
    const maxY = Math.max(0, (state.naturalHeight * state.scale - state.viewportHeight) / 2);
    state.panX = Math.max(-maxX, Math.min(maxX, state.panX));
    state.panY = Math.max(-maxY, Math.min(maxY, state.panY));
}

function applyViewport() {
    deps.elements.panLayer.style.transform = `translate(-50%, -50%) translate(${state.panX}px, ${state.panY}px)`;
    deps.elements.image.style.transform = `scale(${state.scale})`;
    const actualPercent = Math.round(state.scale * 100);
    deps.elements.zoomInfo.textContent = `${actualPercent}%`;
    deps.elements.slider.value = String(Math.round(scaleToSliderPosition(state.scale)));
    deps.elements.slider.setAttribute('aria-valuetext', `${actualPercent}% actual; Fit is ${Math.round(state.fitScale * 100)}%`);
    deps.elements.fit.setAttribute('aria-pressed', String(Math.abs(state.scale - state.fitScale) < 0.0001));
    deps.elements.actual.setAttribute('aria-pressed', String(Math.abs(state.scale - 1) < 0.0001));
}

function zoomAt(nextScale, focalX, focalY) {
    const clamped = Math.max(state.fitScale, Math.min(MAX_SCALE, nextScale));
    const centerX = state.viewportWidth / 2;
    const centerY = state.viewportHeight / 2;
    const imageX = (focalX - centerX - state.panX) / state.scale;
    const imageY = (focalY - centerY - state.panY) / state.scale;
    state.panX = focalX - centerX - imageX * clamped;
    state.panY = focalY - centerY - imageY * clamped;
    state.scale = clamped;
    constrainPan();
    applyViewport();
}
```

For centered commands, call `zoomAt(scale, viewportWidth / 2, viewportHeight / 2)`.

- [ ] **Step 6: Initialize Fit after image load and resize**

Add and call this internal initializer from the image `load` handler before the external image-info callback:

```javascript
function initializeViewport(image) {
    state.naturalWidth = image.naturalWidth;
    state.naturalHeight = image.naturalHeight;
    const rect = deps.elements.viewport.getBoundingClientRect();
    state.viewportWidth = rect.width;
    state.viewportHeight = rect.height;
    state.fitScale = computeFitScale();
    state.scale = state.fitScale;
    state.panX = 0;
    state.panY = 0;
    applyViewport();
}
```

The load handler order is:

```javascript
initializeViewport(deps.elements.image);
deps.onImageReady(clip, deps.elements.image);
```

Create one `ResizeObserver` during controller construction:

```javascript
const resizeObserver = new ResizeObserver(() => {
    if (!state.naturalWidth || !state.naturalHeight) return;
    const wasFit = Math.abs(state.scale - state.fitScale) < 0.0001;
    const rect = deps.elements.viewport.getBoundingClientRect();
    state.viewportWidth = rect.width;
    state.viewportHeight = rect.height;
    state.fitScale = computeFitScale();
    if (wasFit) {
        state.scale = state.fitScale;
        state.panX = 0;
        state.panY = 0;
    } else {
        state.scale = Math.max(state.fitScale, state.scale);
        constrainPan();
    }
    applyViewport();
});
resizeObserver.observe(deps.elements.viewport);
```

The controller creates this observer once; repeated open/close cycles reuse it.

- [ ] **Step 7: Add the integrated toolbar**

Replace the old range-only markup with:

```html
<div class="lightbox-zoom-control" role="group" aria-label="Image zoom controls">
    <button id="lightbox-zoom-out" class="lightbox-zoom-button" aria-label="Zoom out" data-tooltip="Zoom out (-)">−</button>
    <label class="sr-only" for="lightbox-zoom-slider">Image zoom</label>
    <input type="range" id="lightbox-zoom-slider" class="lightbox-range" min="0" max="100" step="1" value="0" aria-describedby="lightbox-zoom-info">
    <button id="lightbox-zoom-in" class="lightbox-zoom-button" aria-label="Zoom in" data-tooltip="Zoom in (+)">+</button>
    <button id="lightbox-zoom-fit" class="lightbox-zoom-mode" aria-label="Fit image to window" aria-pressed="true" data-tooltip="Fit image (0)">Fit</button>
    <button id="lightbox-zoom-actual" class="lightbox-zoom-mode" aria-label="Show image at actual size" aria-pressed="false" data-tooltip="Actual size (1)">1:1</button>
    <span id="lightbox-zoom-info" class="lightbox-zoom-info">100%</span>
</div>
```

Add these entries to the singleton element map:

```javascript
panLayer: document.getElementById('lightbox-pan-layer'),
slider: document.getElementById('lightbox-zoom-slider'),
zoomOut: document.getElementById('lightbox-zoom-out'),
zoomIn: document.getElementById('lightbox-zoom-in'),
fit: document.getElementById('lightbox-zoom-fit'),
actual: document.getElementById('lightbox-zoom-actual'),
zoomInfo: document.getElementById('lightbox-zoom-info'),
```

Extend `command` with:

```javascript
else if (name === 'zoom-in') zoomAt(state.scale * ZOOM_FACTOR, state.viewportWidth / 2, state.viewportHeight / 2);
else if (name === 'zoom-out') zoomAt(state.scale / ZOOM_FACTOR, state.viewportWidth / 2, state.viewportHeight / 2);
else if (name === 'fit') zoomAt(state.fitScale, state.viewportWidth / 2, state.viewportHeight / 2);
else if (name === 'actual-size') zoomAt(1, state.viewportWidth / 2, state.viewportHeight / 2);
else if (name === 'slider') {
    const nextScale = sliderPositionToScale(Number(detail.position));
    zoomAt(nextScale, state.viewportWidth / 2, state.viewportHeight / 2);
}
```

Register controls once during controller creation:

```javascript
deps.elements.zoomOut.addEventListener('click', () => command('zoom-out'));
deps.elements.zoomIn.addEventListener('click', () => command('zoom-in'));
deps.elements.fit.addEventListener('click', () => command('fit'));
deps.elements.actual.addEventListener('click', () => command('actual-size'));
deps.elements.slider.addEventListener('input', event => command('slider', { position: event.target.value }));
```

`+` and `-` multiply/divide by 1.2; Fit uses `fitScale`; 1:1 uses scale `1`, clamped to Fit when a pathological viewport makes Fit greater than 1.

- [ ] **Step 8: Remove legacy zoom state and handlers from `modals.js`**

Delete the old `lightboxZoom`, pan globals, `updateLightboxTransform`, `updateZoomDisplay`, slider handler, reset, constraints, and legacy gesture initialization. Remove the temporary `resetViewport` and `viewportCommand` dependencies added in Task 2, and delete the `initLightboxGestures()` call from `app.js`.

- [ ] **Step 9: Run zoom tests**

Run:

```bash
cd e2e
npx playwright test tests/images/lightbox.spec.ts --grep "Zoom|actual image scale|800 percent"
npx playwright test tests/shortcuts/shortcuts.spec.ts --grep "zoom"
```

Expected: PASS with slider and visible percentage agreeing on actual scale.

- [ ] **Step 10: Commit**

```bash
git restore frontend/wailsjs/go/models.ts 2>/dev/null || true
git add frontend/index.html frontend/js/lightbox.js frontend/js/app.js frontend/js/modals.js frontend/css/modals.css e2e/tests/images/lightbox-controller.spec.ts e2e/tests/images/lightbox.spec.ts
git commit -m "feat(lightbox): add actual-scale zoom controls"
```

---

### Task 6: Add focal pointer, touchpad, and touchscreen interaction

**Files:**
- Modify: `frontend/js/lightbox.js`
- Modify: `frontend/css/modals.css`
- Modify: `e2e/tests/images/lightbox-controller.spec.ts`
- Modify: `e2e/tests/images/lightbox.spec.ts`

**Interfaces:**
- Consumes: `zoomAt`, `constrainPan`, and `applyViewport` from Task 5.
- Produces: cursor-centered modified-wheel zoom, pointer-captured drag pan, double-click Fit/1:1, pinch zoom, and touchscreen pan/swipe.

- [ ] **Step 1: Write a failing cursor-invariance test**

Add:

```typescript
test('keeps the image coordinate beneath the modified-wheel cursor stationary', async ({ app }) => {
  const imagePath = await createTempFile(generateTestImage(1200, 800), 'png');
  await app.uploadFile(imagePath);
  await app.openLightbox(path.basename(imagePath));

  const result = await app.page.locator(selectors.lightbox.viewport).evaluate((viewport) => {
    const rect = viewport.getBoundingClientRect();
    const clientX = rect.left + rect.width * 0.55;
    const clientY = rect.top + rect.height * 0.45;
    const image = document.getElementById('lightbox-img');
    const before = image.getBoundingClientRect();
    const beforePoint = {
      x: (clientX - before.left) / before.width,
      y: (clientY - before.top) / before.height,
    };
    viewport.dispatchEvent(new WheelEvent('wheel', {
      deltaY: -120,
      ctrlKey: true,
      clientX,
      clientY,
      bubbles: true,
      cancelable: true,
    }));
    const after = image.getBoundingClientRect();
    return {
      beforePoint,
      afterPoint: {
        x: (clientX - after.left) / after.width,
        y: (clientY - after.top) / after.height,
      },
    };
  });
  expect(Math.abs(result.afterPoint.x - result.beforePoint.x)).toBeLessThan(0.001);
  expect(Math.abs(result.afterPoint.y - result.beforePoint.y)).toBeLessThan(0.001);
});

test('drags a zoomed image and double-click toggles Fit and 1:1', async ({ app }) => {
  const imagePath = await createTempFile(generateTestImage(1600, 1200), 'png');
  await app.uploadFile(imagePath);
  await app.openLightbox(path.basename(imagePath));
  const viewport = app.page.locator(selectors.lightbox.viewport);
  await viewport.dblclick({ position: { x: 320, y: 220 } });
  await expect(app.page.locator(selectors.lightbox.zoomInfo)).toHaveText('100%');
  const before = await app.page.locator(selectors.lightbox.panLayer).evaluate(element => getComputedStyle(element).transform);
  const box = await viewport.boundingBox();
  if (!box) throw new Error('Lightbox viewport has no bounding box');
  await app.page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await app.page.mouse.down();
  await app.page.mouse.move(box.x + box.width / 2 + 80, box.y + box.height / 2 + 40);
  await app.page.mouse.up();
  const after = await app.page.locator(selectors.lightbox.panLayer).evaluate(element => getComputedStyle(element).transform);
  expect(after).not.toBe(before);
  await viewport.dblclick({ position: { x: 320, y: 220 } });
  await expect(app.page.locator(selectors.lightbox.zoomFit)).toHaveAttribute('aria-pressed', 'true');
});

test('pinch zooms around the touch centroid', async ({ app }) => {
  const imagePath = await createTempFile(generateTestImage(1200, 800), 'png');
  await app.uploadFile(imagePath);
  await app.openLightbox(path.basename(imagePath));
  const fit = Number.parseInt(await app.page.locator(selectors.lightbox.zoomInfo).textContent() || '100', 10);
  await app.page.locator(selectors.lightbox.viewport).evaluate(viewport => {
    type TestTouch = { clientX: number; clientY: number };
    const fire = (type: string, touches: TestTouch[]) => {
      const event = new Event(type, { bubbles: true, cancelable: true });
      Object.defineProperty(event, 'touches', { value: touches });
      viewport.dispatchEvent(event);
    };
    fire('touchstart', [{ clientX: 180, clientY: 200 }, { clientX: 280, clientY: 200 }]);
    fire('touchmove', [{ clientX: 140, clientY: 200 }, { clientX: 320, clientY: 200 }]);
    fire('touchend', []);
  });
  const zoomed = Number.parseInt(await app.page.locator(selectors.lightbox.zoomInfo).textContent() || '0', 10);
  expect(zoomed).toBeGreaterThan(fit);
});
```

- [ ] **Step 2: Run and verify the expected failure**

Run:

```bash
cd e2e && npx playwright test tests/images/lightbox.spec.ts --grep "modified-wheel cursor"
```

Expected: FAIL because pointer-centered wheel handling is not registered.

- [ ] **Step 3: Add pointer drag and double-click**

Add controller-owned pointer state:

```javascript
Object.assign(state, {
    pointerId: null,
    dragStartX: 0,
    dragStartY: 0,
    dragStartPanX: 0,
    dragStartPanY: 0,
});
```

Register:

```javascript
function onPointerDown(event) {
    if (event.pointerType === 'touch' || state.scale <= state.fitScale || event.button !== 0) return;
    state.pointerId = event.pointerId;
    state.dragStartX = event.clientX;
    state.dragStartY = event.clientY;
    state.dragStartPanX = state.panX;
    state.dragStartPanY = state.panY;
    deps.elements.viewport.setPointerCapture(event.pointerId);
    deps.elements.viewport.classList.add('is-panning');
    event.preventDefault();
}

function onPointerMove(event) {
    if (event.pointerId !== state.pointerId) return;
    state.panX = state.dragStartPanX + event.clientX - state.dragStartX;
    state.panY = state.dragStartPanY + event.clientY - state.dragStartY;
    constrainPan();
    applyViewport();
}

function endPointerPan(event) {
    if (event.pointerId !== state.pointerId) return;
    state.pointerId = null;
    deps.elements.viewport.classList.remove('is-panning');
}

function onDoubleClick(event) {
    const rect = deps.elements.viewport.getBoundingClientRect();
    const focalX = event.clientX - rect.left;
    const focalY = event.clientY - rect.top;
    const target = Math.abs(state.scale - state.fitScale) < 0.0001 ? 1 : state.fitScale;
    zoomAt(target, focalX, focalY);
}

deps.elements.viewport.addEventListener('pointerdown', onPointerDown);
deps.elements.viewport.addEventListener('pointermove', onPointerMove);
deps.elements.viewport.addEventListener('pointerup', endPointerPan);
deps.elements.viewport.addEventListener('pointercancel', endPointerPan);
deps.elements.viewport.addEventListener('dblclick', onDoubleClick);
```

- [ ] **Step 4: Add modified-wheel zoom and two-finger pan**

Use:

```javascript
function onWheel(event) {
    if (state.phase !== 'ready') return;
    const rect = deps.elements.viewport.getBoundingClientRect();
    if (event.ctrlKey || event.metaKey) {
        event.preventDefault();
        const factor = Math.exp(-event.deltaY * 0.002);
        zoomAt(state.scale * factor, event.clientX - rect.left, event.clientY - rect.top);
        return;
    }
    if (state.scale > state.fitScale + 0.0001) {
        event.preventDefault();
        state.panX -= event.deltaX;
        state.panY -= event.deltaY;
        constrainPan();
        applyViewport();
    }
}
```

Register the listener as `{ passive: false }`. Task 7 adds the Fit-state navigation branch after this zoomed-pan branch.

- [ ] **Step 5: Add touchscreen pinch, pan, and Fit swipe**

Add touch state:

```javascript
Object.assign(state, {
    touchMode: null,
    pinchStartDistance: 0,
    pinchStartScale: 1,
    lastTouchX: 0,
    lastTouchY: 0,
    swipeStartX: 0,
    swipeStartY: 0,
    swipeStartedAt: 0,
});

function touchGeometry(touches) {
    const first = touches[0];
    const second = touches[1];
    const dx = second.clientX - first.clientX;
    const dy = second.clientY - first.clientY;
    return {
        distance: Math.hypot(dx, dy),
        clientX: (first.clientX + second.clientX) / 2,
        clientY: (first.clientY + second.clientY) / 2,
    };
}

function onTouchStart(event) {
    if (state.phase !== 'ready') return;
    if (event.touches.length === 2) {
        const geometry = touchGeometry(event.touches);
        state.touchMode = 'pinch';
        state.pinchStartDistance = geometry.distance;
        state.pinchStartScale = state.scale;
        event.preventDefault();
        return;
    }
    if (event.touches.length === 1) {
        const touch = event.touches[0];
        state.touchMode = state.scale > state.fitScale + 0.0001 ? 'pan' : 'swipe';
        state.lastTouchX = touch.clientX;
        state.lastTouchY = touch.clientY;
        state.swipeStartX = touch.clientX;
        state.swipeStartY = touch.clientY;
        state.swipeStartedAt = performance.now();
    }
}

function onTouchMove(event) {
    if (event.touches.length === 2 && state.touchMode === 'pinch') {
        const geometry = touchGeometry(event.touches);
        const rect = deps.elements.viewport.getBoundingClientRect();
        const nextScale = state.pinchStartScale * geometry.distance / state.pinchStartDistance;
        zoomAt(nextScale, geometry.clientX - rect.left, geometry.clientY - rect.top);
        event.preventDefault();
        return;
    }
    if (event.touches.length !== 1) return;
    const touch = event.touches[0];
    if (state.touchMode === 'pan') {
        state.panX += touch.clientX - state.lastTouchX;
        state.panY += touch.clientY - state.lastTouchY;
        constrainPan();
        applyViewport();
        event.preventDefault();
    }
    state.lastTouchX = touch.clientX;
    state.lastTouchY = touch.clientY;
}

function onTouchEnd(event) {
    if (event.touches.length > 0) return;
    if (state.touchMode === 'swipe') {
        const deltaX = state.lastTouchX - state.swipeStartX;
        const deltaY = state.lastTouchY - state.swipeStartY;
        const elapsed = performance.now() - state.swipeStartedAt;
        if (elapsed < 350 && Math.abs(deltaX) > 50 && Math.abs(deltaX) > Math.abs(deltaY) * 2) {
            command(deltaX < 0 ? 'next' : 'previous');
        }
    }
    state.touchMode = null;
}

deps.elements.viewport.addEventListener('touchstart', onTouchStart, { passive: false });
deps.elements.viewport.addEventListener('touchmove', onTouchMove, { passive: false });
deps.elements.viewport.addEventListener('touchend', onTouchEnd);
deps.elements.viewport.addEventListener('touchcancel', onTouchEnd);
```

Keep `touch-action: none` on the viewport.

- [ ] **Step 6: Add panning cursor styles**

```css
.lightbox-viewport[data-pannable="true"] { cursor: grab; }
.lightbox-viewport.is-panning { cursor: grabbing; }
.lightbox-image { user-select: none; -webkit-user-drag: none; transform-origin: center; }
```

Set `data-pannable` from `applyViewport` according to `scale > fitScale` and at least one scaled dimension exceeding the viewport.

- [ ] **Step 7: Run interaction tests**

Run:

```bash
cd e2e
npx playwright test tests/images/lightbox.spec.ts --grep "cursor|drag|double-click|pinch"
npx playwright test tests/images/lightbox-controller.spec.ts
```

Expected: PASS with focal-point drift below the stated tolerance.

- [ ] **Step 8: Commit**

```bash
git restore frontend/wailsjs/go/models.ts 2>/dev/null || true
git add frontend/js/lightbox.js frontend/css/modals.css e2e/tests/images/lightbox-controller.spec.ts e2e/tests/images/lightbox.spec.ts
git commit -m "feat(lightbox): add focal pointer and touch zoom"
```

---

### Task 7: Complete keyboard pan and wheel navigation at Fit

**Files:**
- Modify: `frontend/js/lightbox.js`
- Modify: `frontend/js/app.js:1483-1515`
- Modify: `e2e/tests/images/lightbox-controller.spec.ts`
- Modify: `e2e/tests/images/lightbox.spec.ts`
- Modify: `e2e/tests/shortcuts/shortcuts.spec.ts:549-655`

**Interfaces:**
- Consumes: controller commands and viewport state from Tasks 5–6.
- Produces: `command('pan', { dx, dy })`, `0`, `1`, Shift+Arrow bindings, and one-image-per-wheel-gesture navigation.

- [ ] **Step 1: Write failing keyboard and wheel tests**

Add:

```typescript
test('pans with Shift+Arrow without navigating', async ({ app }) => {
  const files = await Promise.all([
    createTempFile(generateTestImage(1600, 1200, [255, 0, 0]), 'png'),
    createTempFile(generateTestImage(1600, 1200, [0, 0, 255]), 'png'),
  ]);
  await app.uploadFiles(files);
  await app.openLightbox(path.basename(files[1]));
  await app.page.keyboard.press('1');
  const caption = await app.page.locator(selectors.lightbox.caption).textContent();
  const before = await app.page.locator(selectors.lightbox.panLayer).evaluate(el => getComputedStyle(el).transform);
  await app.page.keyboard.press('Shift+ArrowRight');
  const after = await app.page.locator(selectors.lightbox.panLayer).evaluate(el => getComputedStyle(el).transform);
  expect(after).not.toBe(before);
  await expect(app.page.locator(selectors.lightbox.caption)).toHaveText(caption || '');
});

for (const axis of ['vertical', 'horizontal'] as const) {
  test(`advances once for a ${axis} wheel gesture at Fit`, async ({ app }) => {
    const files = await Promise.all([
      createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png'),
      createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png'),
    ]);
    await app.uploadFiles(files);
    await app.openLightbox(path.basename(files[2]));
    await app.page.locator(selectors.lightbox.viewport).evaluate((viewport, wheelAxis) => {
      for (const delta of [8, 18, 30, 22, 10, 4]) {
        const init = wheelAxis === 'horizontal' ? { deltaX: delta } : { deltaY: delta };
        viewport.dispatchEvent(new WheelEvent('wheel', { ...init, bubbles: true, cancelable: true }));
      }
    }, axis);
    await expect(app.page.locator(selectors.lightbox.caption)).toContainText(path.basename(files[1]));
  });
}

test('pans instead of navigating when wheel-scrolling above Fit', async ({ app }) => {
  const files = await Promise.all([
    createTempFile(generateTestImage(1600, 1200, [255, 0, 0]), 'png'),
    createTempFile(generateTestImage(1600, 1200, [0, 0, 255]), 'png'),
  ]);
  await app.uploadFiles(files);
  await app.openLightbox(path.basename(files[1]));
  await app.page.keyboard.press('1');
  const caption = await app.page.locator(selectors.lightbox.caption).textContent();
  const before = await app.page.locator(selectors.lightbox.panLayer).evaluate(element => getComputedStyle(element).transform);
  await app.page.locator(selectors.lightbox.viewport).evaluate(viewport => {
    viewport.dispatchEvent(new WheelEvent('wheel', { deltaY: 80, bubbles: true, cancelable: true }));
  });
  const after = await app.page.locator(selectors.lightbox.panLayer).evaluate(element => getComputedStyle(element).transform);
  expect(after).not.toBe(before);
  await expect(app.page.locator(selectors.lightbox.caption)).toHaveText(caption || '');
});
```

- [ ] **Step 2: Run and verify the failures**

Run:

```bash
cd e2e && npx playwright test tests/images/lightbox.spec.ts --grep "Shift.Arrow|wheel gesture"
```

Expected: FAIL because pan commands and guarded wheel navigation are absent.

- [ ] **Step 3: Add semantic pan and zoom-mode commands**

Retain the zoom handlers from Task 5 and add semantic pan handling:

```javascript
else if (name === 'pan') {
    state.panX += Number(detail.dx) || 0;
    state.panY += Number(detail.dy) || 0;
    constrainPan();
    applyViewport();
}
```

- [ ] **Step 4: Register keyboard shortcuts**

Use ShortcutManager registrations:

```javascript
{ id: 'lightbox-fit', label: 'Fit Image', category: 'lightbox', defaultKey: '0', context: 'lightbox', callback: () => window.LightboxController.command('fit') }
{ id: 'lightbox-actual', label: 'Actual Size', category: 'lightbox', defaultKey: '1', context: 'lightbox', callback: () => window.LightboxController.command('actual-size') }
{ id: 'lightbox-pan-left', label: 'Pan Left', category: 'lightbox', defaultKey: 'shift+ArrowLeft', context: 'lightbox', callback: () => window.LightboxController.command('pan', { dx: 48, dy: 0 }) }
{ id: 'lightbox-pan-right', label: 'Pan Right', category: 'lightbox', defaultKey: 'shift+ArrowRight', context: 'lightbox', callback: () => window.LightboxController.command('pan', { dx: -48, dy: 0 }) }
{ id: 'lightbox-pan-up', label: 'Pan Up', category: 'lightbox', defaultKey: 'shift+ArrowUp', context: 'lightbox', callback: () => window.LightboxController.command('pan', { dx: 0, dy: 48 }) }
{ id: 'lightbox-pan-down', label: 'Pan Down', category: 'lightbox', defaultKey: 'shift+ArrowDown', context: 'lightbox', callback: () => window.LightboxController.command('pan', { dx: 0, dy: -48 }) }
```

Keep plain Left/Right bound to previous/next.

- [ ] **Step 5: Implement accumulated wheel navigation**

Add:

```javascript
const WHEEL_THRESHOLD = 56;
const WHEEL_QUIET_MS = 180;
Object.assign(state, {
    wheelX: 0,
    wheelY: 0,
    wheelDirection: 0,
    wheelLocked: false,
    wheelQuietTimer: null,
});

function resetWheelNavigation() {
    state.wheelX = 0;
    state.wheelY = 0;
    state.wheelDirection = 0;
    state.wheelLocked = false;
    clearTimeout(state.wheelQuietTimer);
    state.wheelQuietTimer = null;
}

function handleWheelNavigation(event) {
    if (Math.abs(state.scale - state.fitScale) > 0.0001) return;
    event.preventDefault();
    clearTimeout(state.wheelQuietTimer);
    state.wheelQuietTimer = setTimeout(resetWheelNavigation, WHEEL_QUIET_MS);
    if (state.wheelLocked) return;

    const eventDominant = Math.abs(event.deltaX) >= Math.abs(event.deltaY) ? event.deltaX : event.deltaY;
    const direction = Math.sign(eventDominant);
    if (state.wheelDirection && direction && direction !== state.wheelDirection) {
        state.wheelX = 0;
        state.wheelY = 0;
    }
    if (direction) state.wheelDirection = direction;
    state.wheelX += event.deltaX;
    state.wheelY += event.deltaY;
    const dominant = Math.abs(state.wheelX) >= Math.abs(state.wheelY) ? state.wheelX : state.wheelY;
    if (Math.abs(dominant) < WHEEL_THRESHOLD) return;
    command(dominant > 0 ? 'next' : 'previous');
    state.wheelLocked = true;
}
```

Call `resetWheelNavigation()` on open, close, and zoom above Fit. Do not reset it from `navigate`; a wheel-triggered navigation must remain locked through its momentum tail. Add this final branch to `onWheel` after the zoomed-pan branch from Task 6:

```javascript
if (Math.abs(state.scale - state.fitScale) <= 0.0001) {
    handleWheelNavigation(event);
}
```

Continuous momentum events keep resetting the quiet timer, so one physical gesture cannot unlock and advance again until input has stopped for 180 ms.

- [ ] **Step 6: Add slider Home/End and larger-step behavior**

On slider `keydown`, prevent default for Home, End, PageUp, and PageDown, set positions `0`, `100`, `+10`, or `-10`, then dispatch `input`. Leave Arrow keys to the native range input so they move one position.

- [ ] **Step 7: Run focused input suites**

Run:

```bash
cd e2e
npx playwright test tests/images/lightbox.spec.ts --grep "Shift.Arrow|wheel|Fit|actual"
npx playwright test tests/shortcuts/shortcuts.spec.ts --grep "Lightbox"
```

Expected: PASS. Verify one vertical and one horizontal wheel gesture each advance exactly one image at Fit.

- [ ] **Step 8: Commit**

```bash
git restore frontend/wailsjs/go/models.ts 2>/dev/null || true
git add frontend/js/lightbox.js frontend/js/app.js e2e/tests/images/lightbox-controller.spec.ts e2e/tests/images/lightbox.spec.ts e2e/tests/shortcuts/shortcuts.spec.ts
git commit -m "feat(lightbox): complete keyboard and wheel navigation"
```

---

### Task 8: Complete focus containment and menu integration

**Files:**
- Modify: `frontend/js/lightbox.js`
- Modify: `frontend/js/context-menu.js:1-547`
- Modify: `frontend/js/modals.js:528-879`
- Modify: `frontend/js/app.js:1460-1482, 1769-1780`
- Modify: `frontend/index.html`
- Modify: `e2e/tests/images/lightbox.spec.ts`
- Modify: `e2e/tests/plugins/ui-actions.spec.ts:129-162, 360-425`

**Interfaces:**
- Consumes: controller focus ownership and existing `ContextMenu.open`.
- Produces: `ContextMenu.open(items, clipId, anchor, onAction, { portalRoot })`; exact opener fallback; background inert management; menus inside the lightbox focus model.

- [ ] **Step 1: Write failing menu and opener-fallback tests**

Add plugin coverage:

```typescript
test('keeps Tab focus inside the lightbox after closing the plugin menu', async ({ app }) => {
  const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
  expect(plugin).not.toBeNull();
  await app.enablePlugin(plugin!.id);
  await app.page.reload();
  await app.waitForReady();

  const imagePath = await createTempFile(generateTestImage(), 'png');
  await app.uploadFile(imagePath);
  await app.openLightbox(path.basename(imagePath));
  const trigger = app.page.locator(selectors.lightbox.pluginTrigger);
  await trigger.click();
  await expect(app.page.locator(selectors.lightbox.pluginMenu)).toBeVisible();
  await app.page.keyboard.press('Tab');

  await expect(app.page.locator(selectors.lightbox.pluginMenu)).toHaveCount(0);
  await expect(trigger).toBeFocused();
  const focusInside = await app.page.evaluate(() => {
    const lightbox = document.getElementById('lightbox');
    return lightbox?.contains(document.activeElement) ?? false;
  });
  expect(focusInside).toBe(true);
});

test('targets plugin actions at the clip selected after navigation', async ({ app }) => {
  const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
  expect(plugin).not.toBeNull();
  await app.enablePlugin(plugin!.id);
  await app.page.reload();
  await app.waitForReady();

  const files = await Promise.all([
    createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
    createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png'),
  ]);
  await app.uploadFiles(files);
  const expectedId = await app.page.evaluate(async (filename) => {
    // @ts-ignore Wails binding
    const clips = await window.go.main.App.GetClips(false, [], [], '', '');
    return clips.find((clip: { filename: string; id: number }) => clip.filename === filename)?.id;
  }, path.basename(files[0]));

  await app.openLightbox(path.basename(files[1]));
  await app.page.keyboard.press('ArrowRight');
  await app.page.evaluate(() => {
    // @ts-ignore Wails binding and test capture
    const service = window.go.main.PluginService;
    const original = service.ExecutePluginAction;
    window.__restoreLightboxExecute = () => { service.ExecutePluginAction = original; };
    service.ExecutePluginAction = async (pluginId: number, actionId: string, clipIds: number[]) => {
      window.__lightboxActionClipIds = clipIds;
      return { success: true };
    };
  });
  await app.clickLightboxPluginAction(plugin!.id, 'test_simple');
  await expect.poll(() => app.page.evaluate(() => {
    // @ts-ignore test capture
    return window.__lightboxActionClipIds?.[0];
  })).toBe(expectedId);
  await app.page.evaluate(() => {
    // @ts-ignore test capture
    window.__restoreLightboxExecute?.();
    // @ts-ignore test capture
    delete window.__restoreLightboxExecute;
    // @ts-ignore test capture
    delete window.__lightboxActionClipIds;
  });
});
```

Add this fallback test to `lightbox.spec.ts`:

```typescript
test('focuses the clip at the former opener index when the opener is removed', async ({ app }) => {
  const files = await Promise.all([
    createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
    createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png'),
  ]);
  await app.uploadFiles(files);
  const openerCard = await app.getClipByFilename(path.basename(files[1]));
  await openerCard.locator('[data-action="open-lightbox"]').click();
  await expect(app.page.locator(selectors.lightbox.overlay)).toHaveClass(/active/);
  await openerCard.evaluate(card => card.remove());
  await app.page.keyboard.press('Escape');

  const remainingCard = await app.getClipByFilename(path.basename(files[0]));
  await expect(remainingCard).toBeFocused();
});
```

- [ ] **Step 2: Run and verify the failures**

Run:

```bash
cd e2e
npx playwright test tests/plugins/ui-actions.spec.ts --grep "Tab focus inside"
npx playwright test tests/images/lightbox.spec.ts --grep "opener card"
```

Expected: FAIL because body-level menus leave the trap and fallback focus is not index-aware.

- [ ] **Step 3: Extend ContextMenu with an optional portal root**

Add module state:

```javascript
let activePortalRoot = document.body;
```

Change the signature and initialize the portal immediately after closing any prior menu:

```javascript
function open(items, clipId, anchor, onAction, options = {}) {
    close();
    activePortalRoot = options.portalRoot || document.body;

    if (typeof closeTagPopover === 'function') closeTagPopover();
    currentClipId = clipId;
    onActionCallback = onAction;
    lastFocusedBeforeOpen = document.activeElement;
```

Replace the main-menu append line with:

```javascript
activePortalRoot.appendChild(menu);
positionMainMenu(menu, anchor);
```

In `positionSubmenu`, replace `document.body.appendChild(panel)` with `activePortalRoot.appendChild(panel)`. In `close`, set `activePortalRoot = document.body` after removing menus. Existing card callers omit the fifth argument and retain the body portal.

- [ ] **Step 4: Keep lightbox menus under the dialog**

Open file actions with:

```javascript
ContextMenu.open(items, clip.id, trigger, onAction, {
    portalRoot: deps.elements.root,
});
```

Append the plugin menu to `deps.elements.root` rather than `document.body`. Set `aria-controls` on triggers and an accessible label on each menu. On Tab from a plugin item, close the menu and focus the trigger before allowing the next Tab event; on Escape, close and focus the trigger.

- [ ] **Step 5: Make the background inert and preserve previous values**

Pass explicit background roots from `app.js`:

```javascript
backgroundRoots: [
    document.querySelector('header'),
    document.querySelector('main'),
    document.querySelector('footer'),
    document.getElementById('nav-drawer'),
].filter(Boolean),
```

In the controller:

```javascript
const backgroundInert = new Map();

function setBackgroundInert(enabled) {
    for (const element of deps.backgroundRoots) {
        if (enabled) {
            backgroundInert.set(element, element.hasAttribute('inert'));
            element.setAttribute('inert', '');
        } else if (!backgroundInert.get(element)) {
            element.removeAttribute('inert');
        }
    }
    if (!enabled) backgroundInert.clear();
}
```

Call with `true` on closed→open and `false` on close. Overlay dialogs opened from lightbox remain outside these background roots and can become active.

- [ ] **Step 6: Implement exact fallback focus order**

When opening from the closed phase, store:

```javascript
state.opener = opener || document.activeElement;
state.openerIndex = Array.from(gallery.querySelectorAll(':scope > li')).indexOf(state.opener?.closest('li'));
```

In `close`, capture both values before clearing state and call `deps.restoreFocus(opener, openerIndex)`. Restore with:

```javascript
function restoreFocus(opener, formerIndex) {
    if (opener?.isConnected && opener.getClientRects().length > 0) {
        opener.focus();
        return;
    }
    const visible = Array.from(gallery.querySelectorAll(':scope > li'))
        .filter(card => card.getClientRects().length > 0);
    const fallbackIndex = Math.max(0, Math.min(formerIndex, visible.length - 1));
    const fallback = visible[fallbackIndex];
    if (fallback) fallback.focus();
    else gallery.focus();
}
```

Add `tabindex="-1"` to `#gallery` in `frontend/index.html` so the final fallback is always programmatically focusable.

- [ ] **Step 7: Run focus, plugin, and context-menu suites**

Run:

```bash
cd e2e
npx playwright test tests/images/lightbox.spec.ts --grep "Focus|opener"
npx playwright test tests/plugins/ui-actions.spec.ts --grep "Lightbox Plugin|Tab focus"
npx playwright test tests/clips/context-menu.spec.ts
npx playwright test tests/clips/html-integrity.spec.ts --grep "Lightbox"
```

Expected: PASS. Existing card context menus must retain their body portal and focus behavior.

- [ ] **Step 8: Commit**

```bash
git restore frontend/wailsjs/go/models.ts 2>/dev/null || true
git add frontend/js/lightbox.js frontend/js/context-menu.js frontend/js/modals.js frontend/js/app.js frontend/index.html e2e/tests/images/lightbox.spec.ts e2e/tests/plugins/ui-actions.spec.ts
git commit -m "fix(lightbox): contain focus across viewer menus"
```

---

### Task 9: Finish responsive styling, announcements, and reduced motion

**Files:**
- Modify: `frontend/index.html:711-770`
- Modify: `frontend/css/modals.css:1-430, 850-885`
- Modify: `frontend/js/lightbox.js`
- Modify: `e2e/tests/images/lightbox.spec.ts`
- Modify: `e2e/tests/clips/html-integrity.spec.ts:267-331`

**Interfaces:**
- Consumes: final controller state and toolbar DOM.
- Produces: responsive two-row bar, contrast-compliant controls, visible focus rings, reduced-motion behavior, dialog labeling, and live announcements.

- [ ] **Step 1: Write failing accessibility and narrow-layout tests**

Add:

```typescript
test('names every zoom control and exposes actual slider value text', async ({ app }) => {
  const imagePath = await createTempFile(generateTestImage(1200, 800), 'png');
  await app.uploadFile(imagePath);
  await app.openLightbox(path.basename(imagePath));

  await expect(app.page.getByRole('button', { name: 'Zoom out' })).toBeVisible();
  await expect(app.page.getByRole('slider', { name: 'Image zoom' })).toHaveAttribute('aria-valuetext', /% actual; Fit is \d+%/);
  await expect(app.page.getByRole('button', { name: 'Zoom in' })).toBeVisible();
  await expect(app.page.getByRole('button', { name: 'Fit image to window' })).toBeVisible();
  await expect(app.page.getByRole('button', { name: 'Show image at actual size' })).toBeVisible();
});

test('keeps the integrated toolbar usable at a narrow viewport', async ({ app }) => {
  const imagePath = await createTempFile(generateTestImage(800, 600), 'png');
  await app.uploadFile(imagePath);
  await app.page.setViewportSize({ width: 480, height: 700 });
  await app.openLightbox(path.basename(imagePath));

  const controls = [selectors.lightbox.closeButton, selectors.lightbox.zoomOut, selectors.lightbox.zoomSlider, selectors.lightbox.zoomIn, selectors.lightbox.zoomFit, selectors.lightbox.zoomActual];
  for (const selector of controls) await expect(app.page.locator(selector)).toBeInViewport();
  const overflow = await app.page.locator(selectors.lightbox.bar).evaluate(element => element.scrollWidth > element.clientWidth);
  expect(overflow).toBe(false);
});
```

- [ ] **Step 2: Run and verify the failures**

Run:

```bash
cd e2e && npx playwright test tests/images/lightbox.spec.ts --grep "names every zoom|narrow viewport"
```

Expected: FAIL until final labels and responsive rules are applied.

- [ ] **Step 3: Finalize dialog semantics**

Use:

```html
<div id="lightbox" class="lightbox-backdrop" role="dialog" aria-modal="true"
     aria-labelledby="lightbox-caption" tabindex="-1" inert>
```

Keep the caption non-empty while loading. Set raw filename text and raw `alt` properties; never call `escapeHTML` when assigning DOM properties or `textContent`.

Update the lightbox structure expectation in `html-integrity.spec.ts`:

```typescript
requiredChildren: [
  '#lightbox-viewport',
  '#lightbox-img',
  '#lightbox-close',
  '#lightbox-prev',
  '#lightbox-next',
  '#lightbox-file-menu-trigger',
  '#lightbox-zoom-out',
  '#lightbox-zoom-slider',
  '#lightbox-zoom-in',
  '#lightbox-zoom-fit',
  '#lightbox-zoom-actual',
  '#lightbox-zoom-info',
  '#lightbox-status',
],
requiredAttributes: ['role', 'aria-modal', 'aria-labelledby'],
childAttributes: [
  { selector: '#lightbox-close', attrs: ['aria-label'] },
  { selector: '#lightbox-prev', attrs: ['aria-label'] },
  { selector: '#lightbox-next', attrs: ['aria-label'] },
  { selector: '#lightbox-zoom-slider', attrs: ['aria-describedby', 'aria-valuetext'] },
],
```

- [ ] **Step 4: Apply the responsive two-row toolbar**

Wrap plugin and file actions together before the image-info element:

```html
<div class="lightbox-action-groups">
    <div id="lightbox-plugin-actions" class="lightbox-plugin-actions hidden"></div>
    <div id="lightbox-file-actions" class="lightbox-file-actions"></div>
</div>
<div id="lightbox-image-info" class="lightbox-image-info"></div>
<!-- lightbox-zoom-control from Task 5 -->
```

Use:

```css
.lightbox-bar {
    position: relative;
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    grid-template-areas: "actions info zoom";
    align-items: center;
    gap: 0.75rem;
    padding: 0.625rem max(1rem, env(safe-area-inset-right)) max(0.625rem, env(safe-area-inset-bottom)) max(1rem, env(safe-area-inset-left));
    background: rgba(28, 25, 23, 0.9);
    border-top: 1px solid rgba(120, 113, 108, 0.45);
}

.lightbox-action-groups {
    grid-area: actions;
    display: flex;
    align-items: center;
    gap: 0.5rem;
}

.lightbox-image-info { grid-area: info; }

.lightbox-zoom-control {
    grid-area: zoom;
    display: flex;
    align-items: center;
    gap: 0.375rem;
    min-width: 0;
}

.lightbox-zoom-button,
.lightbox-zoom-mode {
    min-width: 2rem;
    min-height: 2rem;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: 1px solid rgba(120, 113, 108, 0.55);
    border-radius: 0.375rem;
    color: #d6d3d1;
    background: rgba(68, 64, 60, 0.55);
}

.lightbox-zoom-button:focus-visible,
.lightbox-zoom-mode:focus-visible,
.lightbox-range:focus-visible {
    outline: 2px solid #d6d3d1;
    outline-offset: 2px;
}

.lightbox-range { min-height: 2rem; width: 7rem; }
.lightbox-zoom-info { color: #a8a29e; }

@media (max-width: 720px) {
    .lightbox-bar {
        grid-template-columns: minmax(0, 1fr) auto;
        grid-template-areas:
            "info actions"
            "zoom zoom";
        row-gap: 0.5rem;
    }
    .lightbox-image-info { grid-area: info; }
    .lightbox-action-groups { grid-area: actions; justify-content: flex-end; }
    .lightbox-zoom-control { grid-area: zoom; justify-content: center; }
    .lightbox-range { flex: 1; width: auto; max-width: 10rem; }
}
```

- [ ] **Step 5: Add reduced-motion rules**

```css
@media (prefers-reduced-motion: reduce) {
    .lightbox-backdrop,
    .lightbox-content,
    .lightbox-image,
    .lightbox-pan-layer,
    .lightbox-plugin-menu,
    .card-menu-dropdown,
    .card-menu-submenu {
        transition: none !important;
        animation: none !important;
    }
}
```

The controller must not wait for animation timers before restoring focus or changing state.

- [ ] **Step 6: Finalize announcements**

Announce only transitions that matter:

```javascript
deps.elements.status.textContent = state.phase === 'ready'
    ? `${clip.filename || 'Image'}, ${currentIndex() + 1} of ${state.clips.length}, ${Math.round(state.scale * 100)}%`
    : `Failed to load ${clip.filename || 'image'}`;
```

Do not announce every pan or continuous slider input. Update slider `aria-valuetext` without writing to the live region.

- [ ] **Step 7: Run accessibility and structure suites**

Run:

```bash
cd e2e
npx playwright test tests/images/lightbox.spec.ts --grep "names every zoom|narrow viewport|Focus"
npx playwright test tests/clips/html-integrity.spec.ts --grep "Lightbox"
```

Expected: PASS at desktop and 480×700 viewport sizes.

- [ ] **Step 8: Commit**

```bash
git restore frontend/wailsjs/go/models.ts 2>/dev/null || true
git add frontend/index.html frontend/css/modals.css frontend/js/lightbox.js e2e/tests/images/lightbox.spec.ts e2e/tests/clips/html-integrity.spec.ts
git commit -m "feat(lightbox): finish accessible responsive viewer"
```

---

### Task 10: Remove legacy code and complete verification

**Files:**
- Modify: `frontend/js/modals.js`
- Modify: `frontend/js/app.js`
- Modify: `frontend/js/ui.js`
- Modify: `frontend/js/wails-api.js`
- Modify: `frontend/js/editor.js`
- Modify: `frontend/css/modals.css`
- Modify: `docs/docs/developers/frontend.md:158-175`
- Test: all focused and complete suites

**Interfaces:**
- Consumes: completed `window.LightboxController`.
- Produces: no legacy global lightbox state, no dead lightbox menu CSS, updated developer documentation, and a clean verified worktree.

- [ ] **Step 1: Prove legacy symbols are gone**

Run:

```bash
rg -n "imageClips|currentLightboxIndex|linkedLightboxPreviousClips|lightboxZoom|lightboxPanX|lightboxPanY|openLightbox\(|closeLightbox\(|showNextImage|showPrevImage|lightbox-file-menu" frontend/js frontend/css
```

Expected before cleanup: remaining matches identify only legacy definitions or callers that must be removed. Expected after cleanup: no matches except documentation or compatibility names explicitly routed to `LightboxController`.

- [ ] **Step 2: Remove dead implementation and styles**

Delete legacy lightbox lifecycle, gesture, transform, and custom file-menu functions from `modals.js`. Keep comparison functions and shared constants still used by the editor/comparison paths. Delete `.lightbox-file-menu*` CSS because file actions use `ContextMenu`. Replace direct DOM or state mutation in callers with controller commands.

Remove `LIGHTBOX_CLOSE_DELAY` and replace delayed editor/delete transitions with direct lifecycle ordering:

```javascript
editClip: clip => {
    window.LightboxController.close();
    openEditor(clip.id);
},
```

```javascript
window.LightboxController.close();
deleteClip(clip.id);
```

The final `modals.js` must begin with comparison/plugin-modal behavior rather than retaining dead lightbox state.

- [ ] **Step 3: Update developer documentation**

Replace the lightbox section with:

````markdown
### lightbox.js — Image Viewer

`LightboxController` owns the complete image-viewer lifecycle and exposes:

```javascript
LightboxController.open({ clips, currentId, opener })
LightboxController.openSingle({ clip, opener })
LightboxController.setClips(clips)
LightboxController.command(name, detail)
LightboxController.close()
```

The controller uses stable clip IDs, generation-guarded image loading, actual-scale Fit/1:1 zoom, focal-point transforms, focus restoration, and keyboard/mouse/touchpad/touch adapters. Callers must not mutate lightbox DOM or state directly.

`modals.js` retains image comparison and other modal behavior.
````

- [ ] **Step 4: Run static checks**

Run:

```bash
git diff --check
rg -n "imageClips|currentLightboxIndex|linkedLightboxPreviousClips" frontend/js
rg -n "lightbox-file-menu" frontend/css/modals.css
```

Expected: `git diff --check` exits 0; both `rg` commands return no matches.

- [ ] **Step 5: Run focused suites**

Run:

```bash
cd e2e
npx playwright test tests/images/lightbox-controller.spec.ts
npx playwright test tests/images/lightbox.spec.ts
npx playwright test tests/shortcuts/shortcuts.spec.ts --grep "Lightbox"
npx playwright test tests/plugins/ui-actions.spec.ts --grep "Lightbox"
npx playwright test tests/search/filtering.spec.ts --grep "lightbox"
npx playwright test tests/clips/context-menu.spec.ts
npx playwright test tests/clips/html-integrity.spec.ts --grep "Lightbox"
```

Expected: every command exits 0 with no retries required.

- [ ] **Step 6: Run the complete required E2E suite**

Run:

```bash
cd e2e && npm test 2>&1 | tail -50
```

Expected: the main Chromium suite and single-worker share suite both pass. If Wails regenerates `frontend/wailsjs/go/models.ts`, restore it before committing.

- [ ] **Step 7: Manually inspect the interaction matrix**

Run:

```bash
make dev
```

Verify this exact matrix in the running desktop app:

```text
Open: Fit, close button focused, controls visible
Keyboard: +, -, 0, 1, Shift+Arrows, Left/Right, Escape
Mouse: Ctrl/Cmd+wheel focal zoom, drag pan, double-click Fit/1:1
Touchpad: pinch focal zoom, two-finger pan above Fit, both-axis navigation at Fit
Toolbar: buttons, logarithmic slider, Home/End/PageUp/PageDown
Focus: exact opener restoration after navigation; plugin/file menu containment
Responsive: 480px-wide two-row toolbar without overflow
Error: Retry/Previous/Next/Close remain available
```

Stop the dev process after inspection.

- [ ] **Step 8: Commit cleanup and documentation**

```bash
git restore frontend/wailsjs/go/models.ts 2>/dev/null || true
git add frontend/js/modals.js frontend/js/app.js frontend/js/ui.js frontend/js/wails-api.js frontend/js/editor.js frontend/css/modals.css docs/docs/developers/frontend.md
git commit -m "refactor(lightbox): remove legacy viewer globals"
```

- [ ] **Step 9: Confirm the final repository state**

Run:

```bash
git status --short
git log --oneline -10
```

Expected: clean status and one focused commit per task.
