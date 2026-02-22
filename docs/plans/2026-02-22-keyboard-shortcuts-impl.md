# Keyboard Shortcuts Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a centralized keyboard shortcuts system with rebindable keys, arrow-key grid navigation, settings UI, and a cheat sheet overlay.

**Architecture:** A single `shortcuts.js` module owns all keyboard handling via one `document.addEventListener('keydown', ...)`. It maintains a registry of actions, resolves the current context (gallery/lightbox/bulk/clip/watch/global), and dispatches to callbacks. Custom bindings persist as a JSON blob in the SQLite `settings` table. Existing keyboard handlers in app.js and modals.js are migrated into this system.

**Tech Stack:** Vanilla JavaScript (no build step), Tailwind CSS, Playwright e2e tests, Go backend (existing `GetSetting`/`SetSetting` API).

---

### Task 1: Create shortcuts.js — Core Engine

**Files:**
- Create: `frontend/js/shortcuts.js`

**Step 1: Write the shortcut manager module**

This is the core engine. It must load before app.js and modals.js but after wails-api.js (which provides the Go API wrappers). Create `frontend/js/shortcuts.js` with:

```javascript
// --- Shortcut Manager ---

const ShortcutManager = (() => {
    // Registry: Map<actionId, ActionDef>
    const actions = new Map();

    // Current bindings: Map<comboString, actionId> per context
    // Built from defaults + user overrides
    let bindingsByContext = new Map(); // Map<context, Map<combo, actionId>>

    // User overrides loaded from backend (only stores differences from defaults)
    let userOverrides = {}; // { actionId: comboString | null }

    // Grid focus state
    let focusedClipIndex = -1;

    // Whether the manager is initialized
    let initialized = false;

    // --- Context Detection ---

    function getActiveContexts() {
        const contexts = ['global'];
        const active = document.activeElement;
        const lightbox = document.getElementById('lightbox');
        const editorModal = document.getElementById('editor-modal');
        const comparisonModal = document.getElementById('comparison-modal');
        const confirmDialog = document.getElementById('confirm-dialog');
        const watchView = document.getElementById('watch-view');
        const bulkToolbar = document.getElementById('bulk-toolbar');
        const settingsModal = document.getElementById('settings-modal');
        const pluginsModal = document.querySelector('[data-testid="plugins-modal"]');

        // Don't fire shortcuts when a modal dialog that has its own key handling is open
        if (confirmDialog && confirmDialog.classList.contains('opacity-100')) return [];
        if (editorModal && editorModal.classList.contains('active')) return [];
        if (comparisonModal && comparisonModal.classList.contains('active')) return [];
        if (settingsModal && !settingsModal.classList.contains('opacity-0')) return [];
        if (pluginsModal && !pluginsModal.classList.contains('opacity-0')) return [];

        // Check for any other open modal (plugin options, plugin review, folder modal, etc.)
        const openModals = ['plugin-options-modal', 'plugin-result-modal', 'plugin-review-modal',
                            'folder-modal', 'restore-confirm-dialog', 'text-editor-modal'];
        for (const id of openModals) {
            const modal = document.getElementById(id);
            if (modal && (modal.classList.contains('opacity-100') || modal.classList.contains('active') ||
                (modal.style.display !== 'none' && modal.offsetParent !== null && !modal.classList.contains('opacity-0') && !modal.classList.contains('hidden')))) {
                return [];
            }
        }

        if (lightbox && lightbox.classList.contains('active')) {
            contexts.push('lightbox');
            return contexts;
        }

        if (watchView && !watchView.classList.contains('hidden')) {
            contexts.push('watch');
            return contexts;
        }

        // Gallery-level contexts
        contexts.push('gallery');

        if (bulkToolbar && bulkToolbar.classList.contains('pointer-events-auto')) {
            contexts.push('bulk');
        }

        if (focusedClipIndex >= 0) {
            contexts.push('clip');
        }

        return contexts;
    }

    // --- Key Combo Parsing ---

    function eventToCombo(e) {
        const parts = [];
        if (e.metaKey || e.ctrlKey) parts.push('mod');
        if (e.shiftKey) parts.push('shift');
        if (e.altKey) parts.push('alt');

        let key = e.key;
        // Normalize key names
        if (key === ' ') key = 'Space';
        if (key.length === 1) key = key.toLowerCase();

        // Don't add modifier keys themselves as the key part
        if (['Control', 'Meta', 'Shift', 'Alt'].includes(key)) return null;

        parts.push(key);
        return parts.join('+');
    }

    function comboToDisplay(combo) {
        if (!combo) return '—';
        const isMac = navigator.platform.toUpperCase().indexOf('MAC') >= 0;
        return combo
            .split('+')
            .map(part => {
                switch (part) {
                    case 'mod': return isMac ? '⌘' : 'Ctrl';
                    case 'shift': return isMac ? '⇧' : 'Shift';
                    case 'alt': return isMac ? '⌥' : 'Alt';
                    case 'ArrowUp': return '↑';
                    case 'ArrowDown': return '↓';
                    case 'ArrowLeft': return '←';
                    case 'ArrowRight': return '→';
                    case 'Escape': return 'Esc';
                    case 'Space': return '␣';
                    case 'Enter': return '↵';
                    case 'Backspace': return '⌫';
                    case 'Delete': return isMac ? '⌦' : 'Del';
                    default: return part.length === 1 ? part.toUpperCase() : part;
                }
            })
            .join(isMac ? '' : '+');
    }

    // --- Registration ---

    function register(def) {
        // def: { id, label, category, defaultKey, context, callback }
        actions.set(def.id, { ...def });
    }

    // --- Binding Resolution ---

    function rebuildBindings() {
        bindingsByContext = new Map();

        for (const [id, action] of actions) {
            // Determine effective key: user override or default
            let combo;
            if (id in userOverrides) {
                combo = userOverrides[id]; // null means unbound
            } else {
                combo = action.defaultKey;
            }
            if (!combo) continue;

            const ctx = action.context;
            if (!bindingsByContext.has(ctx)) {
                bindingsByContext.set(ctx, new Map());
            }
            bindingsByContext.get(ctx).set(combo, id);
        }
    }

    // --- Dispatch ---

    function handleKeydown(e) {
        if (!initialized) return;

        // Input guard: suppress shortcuts when typing in form fields (except Escape)
        const tag = e.target.tagName;
        const isEditable = tag === 'INPUT' || tag === 'TEXTAREA' || e.target.isContentEditable;
        if (isEditable && e.key !== 'Escape') return;

        // Don't interfere with selected text + browser copy
        if ((e.metaKey || e.ctrlKey) && e.key === 'c') {
            const selection = window.getSelection();
            if (selection && selection.toString().length > 0) return;
            // Don't interfere with text inputs
            const active = document.activeElement;
            if (active && (active.tagName === 'TEXTAREA' || active.isContentEditable ||
                (active.tagName === 'INPUT' && active.type !== 'checkbox' && active.type !== 'radio'))) {
                return;
            }
        }

        const combo = eventToCombo(e);
        if (!combo) return;

        const activeContexts = getActiveContexts();
        if (activeContexts.length === 0) return;

        // Check contexts in priority order (most specific first)
        // clip > bulk > lightbox > watch > gallery > global
        const priority = ['clip', 'bulk', 'lightbox', 'watch', 'gallery', 'global'];

        for (const ctx of priority) {
            if (!activeContexts.includes(ctx)) continue;
            const ctxBindings = bindingsByContext.get(ctx);
            if (!ctxBindings) continue;

            const actionId = ctxBindings.get(combo);
            if (!actionId) continue;

            const action = actions.get(actionId);
            if (!action || !action.callback) continue;

            e.preventDefault();
            e.stopPropagation();
            action.callback(e);
            return;
        }
    }

    // --- Grid Navigation ---

    function getGridColumnCount() {
        const gallery = document.getElementById('gallery');
        if (!gallery) return 1;
        const style = getComputedStyle(gallery);
        const columns = style.getPropertyValue('grid-template-columns');
        if (!columns || columns === 'none') return 1;
        return columns.split(' ').filter(c => c.trim()).length;
    }

    function getVisibleClipCount() {
        const gallery = document.getElementById('gallery');
        if (!gallery) return 0;
        return gallery.querySelectorAll(':scope > li').length;
    }

    function setFocusedClipIndex(index) {
        const gallery = document.getElementById('gallery');
        if (!gallery) return;

        const clips = gallery.querySelectorAll(':scope > li');

        // Remove focus from previous
        if (focusedClipIndex >= 0 && focusedClipIndex < clips.length) {
            clips[focusedClipIndex].classList.remove('clip-focused');
        }

        focusedClipIndex = index;

        // Add focus to new
        if (focusedClipIndex >= 0 && focusedClipIndex < clips.length) {
            clips[focusedClipIndex].classList.add('clip-focused');
            clips[focusedClipIndex].scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        }
    }

    function clearFocus() {
        setFocusedClipIndex(-1);
    }

    function navigateGrid(direction) {
        const total = getVisibleClipCount();
        if (total === 0) return;

        const cols = getGridColumnCount();

        if (focusedClipIndex < 0) {
            // No focus yet — focus first clip
            setFocusedClipIndex(0);
            return;
        }

        let next = focusedClipIndex;
        switch (direction) {
            case 'ArrowRight':
                next = focusedClipIndex + 1;
                if (next >= total) next = total - 1; // Don't wrap past end
                break;
            case 'ArrowLeft':
                next = focusedClipIndex - 1;
                if (next < 0) next = 0;
                break;
            case 'ArrowDown':
                next = focusedClipIndex + cols;
                if (next >= total) next = total - 1; // Clamp to last
                break;
            case 'ArrowUp':
                next = focusedClipIndex - cols;
                if (next < 0) next = 0;
                break;
        }

        if (next !== focusedClipIndex) {
            setFocusedClipIndex(next);
        }
    }

    function getFocusedClip() {
        if (focusedClipIndex < 0) return null;
        const gallery = document.getElementById('gallery');
        if (!gallery) return null;
        const clips = gallery.querySelectorAll(':scope > li');
        if (focusedClipIndex >= clips.length) return null;
        return clips[focusedClipIndex];
    }

    function getFocusedClipId() {
        const clip = getFocusedClip();
        if (!clip) return null;
        return parseInt(clip.dataset.id, 10);
    }

    // --- Persistence ---

    async function loadUserOverrides() {
        try {
            const json = await window.go.main.App.GetSetting('keyboard_shortcuts');
            if (json) {
                userOverrides = JSON.parse(json);
            }
        } catch (err) {
            console.error('Failed to load keyboard shortcut overrides:', err);
            userOverrides = {};
        }
        rebuildBindings();
    }

    async function saveUserOverrides() {
        try {
            await window.go.main.App.SetSetting('keyboard_shortcuts', JSON.stringify(userOverrides));
        } catch (err) {
            console.error('Failed to save keyboard shortcut overrides:', err);
        }
    }

    function setOverride(actionId, combo) {
        if (combo === actions.get(actionId)?.defaultKey) {
            // Same as default — remove override
            delete userOverrides[actionId];
        } else {
            userOverrides[actionId] = combo;
        }
        rebuildBindings();
        saveUserOverrides();
    }

    function removeBinding(actionId) {
        userOverrides[actionId] = null;
        rebuildBindings();
        saveUserOverrides();
    }

    function resetAllToDefaults() {
        userOverrides = {};
        rebuildBindings();
        saveUserOverrides();
    }

    function getEffectiveCombo(actionId) {
        if (actionId in userOverrides) {
            return userOverrides[actionId];
        }
        return actions.get(actionId)?.defaultKey || null;
    }

    // --- Conflict Detection ---

    // Context overlap: two contexts overlap if one is a parent of the other
    // or they are the same. The hierarchy is:
    // global > gallery > clip, global > gallery > bulk, global > lightbox, global > watch
    function contextsOverlap(ctx1, ctx2) {
        if (ctx1 === ctx2) return true;
        const hierarchy = {
            global: ['gallery', 'lightbox', 'watch', 'bulk', 'clip'],
            gallery: ['clip', 'bulk'],
        };
        return (hierarchy[ctx1]?.includes(ctx2)) || (hierarchy[ctx2]?.includes(ctx1));
    }

    function findConflict(actionId, newCombo) {
        const action = actions.get(actionId);
        if (!action) return null;

        for (const [id, other] of actions) {
            if (id === actionId) continue;
            const otherCombo = getEffectiveCombo(id);
            if (otherCombo !== newCombo) continue;
            if (contextsOverlap(action.context, other.context)) {
                return other;
            }
        }
        return null;
    }

    // --- Cheat Sheet ---

    function openCheatSheet() {
        const overlay = document.getElementById('shortcuts-cheatsheet');
        if (!overlay) return;
        overlay.classList.remove('opacity-0', 'pointer-events-none');
        overlay.classList.add('opacity-100');
        renderCheatSheet();
    }

    function closeCheatSheet() {
        const overlay = document.getElementById('shortcuts-cheatsheet');
        if (!overlay) return;
        overlay.classList.add('opacity-0', 'pointer-events-none');
        overlay.classList.remove('opacity-100');
    }

    function isCheatSheetOpen() {
        const overlay = document.getElementById('shortcuts-cheatsheet');
        return overlay && overlay.classList.contains('opacity-100');
    }

    function renderCheatSheet() {
        const container = document.getElementById('shortcuts-cheatsheet-content');
        if (!container) return;

        // Group actions by category
        const groups = new Map();
        for (const [id, action] of actions) {
            const cat = action.category || 'other';
            if (!groups.has(cat)) groups.set(cat, []);
            groups.get(cat).push({ id, ...action });
        }

        const categoryOrder = ['navigation', 'gallery', 'clip', 'lightbox', 'bulk', 'system'];
        const categoryLabels = {
            navigation: 'Navigation',
            gallery: 'Gallery',
            clip: 'Clip Actions',
            lightbox: 'Lightbox',
            bulk: 'Bulk Actions',
            system: 'System',
        };

        let html = '<div class="grid grid-cols-1 sm:grid-cols-2 gap-6">';

        for (const cat of categoryOrder) {
            const items = groups.get(cat);
            if (!items || items.length === 0) continue;

            html += `<div>`;
            html += `<h3 class="text-[10px] font-semibold text-stone-400 uppercase tracking-wider mb-2">${categoryLabels[cat] || cat}</h3>`;
            html += `<div class="space-y-1.5">`;

            for (const item of items) {
                const combo = getEffectiveCombo(item.id);
                const display = comboToDisplay(combo);
                html += `<div class="flex items-center justify-between">`;
                html += `<span class="text-xs text-stone-300">${escapeHTML(item.label)}</span>`;
                html += `<kbd class="bg-stone-700 border border-stone-600 text-stone-300 rounded px-2 py-0.5 text-[10px] font-mono min-w-[24px] text-center">${escapeHTML(display)}</kbd>`;
                html += `</div>`;
            }

            html += `</div></div>`;
        }

        html += '</div>';
        html += `<p class="text-[10px] text-stone-500 mt-6 text-center">Edit shortcuts in <button id="cheatsheet-open-settings" class="underline hover:text-stone-300 transition-colors">Settings</button></p>`;

        container.innerHTML = html;

        // Wire up "open settings" link
        const openSettingsLink = document.getElementById('cheatsheet-open-settings');
        if (openSettingsLink) {
            openSettingsLink.addEventListener('click', () => {
                closeCheatSheet();
                if (typeof openSettings === 'function') openSettings();
            });
        }
    }

    // --- Init ---

    async function init() {
        document.addEventListener('keydown', handleKeydown, true);
        await loadUserOverrides();
        initialized = true;
    }

    // --- Public API ---

    return {
        register,
        init,
        rebuildBindings,
        getActiveContexts,
        eventToCombo,
        comboToDisplay,
        getEffectiveCombo,
        findConflict,
        setOverride,
        removeBinding,
        resetAllToDefaults,
        loadUserOverrides,
        saveUserOverrides,
        navigateGrid,
        clearFocus,
        getFocusedClip,
        getFocusedClipId,
        setFocusedClipIndex,
        openCheatSheet,
        closeCheatSheet,
        isCheatSheetOpen,
        renderCheatSheet,
        get actions() { return actions; },
        get focusedClipIndex() { return focusedClipIndex; },
        get userOverrides() { return userOverrides; },
    };
})();
```

**Step 2: Add the script tag to index.html**

In `frontend/index.html`, add the script tag after `wails-api.js` and before `ui.js` in the script loading section (around line 1075):

```html
<script src="js/shortcuts.js"></script>
```

The order should be: `wails-api.js` → `shortcuts.js` → `ui.js` → ... → `app.js`

**Step 3: Add the CSS class for clip focus ring**

In `frontend/css/main.css`, add the focus ring style:

```css
/* Keyboard navigation focus indicator */
.clip-focused {
    outline: 2px solid #a8a29e; /* stone-400 */
    outline-offset: 2px;
    transform: scale(1.02);
    z-index: 1;
}
```

**Step 4: Run tests to verify nothing is broken**

Run: `cd e2e && npm test`
Expected: All existing tests pass (no functionality changed yet)

**Step 5: Commit**

```bash
git add frontend/js/shortcuts.js frontend/css/main.css frontend/index.html
git commit -m "feat: add keyboard shortcut manager core engine"
```

---

### Task 2: Add Cheat Sheet Overlay HTML

**Files:**
- Modify: `frontend/index.html` (add overlay markup before closing `</body>`)

**Step 1: Add the cheat sheet overlay markup**

Add this before the closing `</body>` tag in `frontend/index.html`, after all the other modal divs but before the script tags:

```html
<!-- Shortcuts Cheat Sheet Overlay -->
<div id="shortcuts-cheatsheet"
    class="fixed inset-0 z-[60] flex items-center justify-center p-8 bg-stone-900/80 backdrop-blur-sm transition-opacity duration-200 opacity-0 pointer-events-none"
    data-testid="shortcuts-cheatsheet">
    <div class="bg-stone-800 rounded-lg shadow-2xl max-w-lg w-full max-h-[80vh] overflow-y-auto border border-stone-700">
        <div class="p-5 border-b border-stone-700 flex justify-between items-center">
            <h2 class="text-sm font-semibold text-stone-200">Keyboard Shortcuts</h2>
            <button id="shortcuts-cheatsheet-close"
                class="p-1 hover:bg-stone-700 rounded-md transition-colors text-stone-400 hover:text-stone-200">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
                </svg>
            </button>
        </div>
        <div id="shortcuts-cheatsheet-content" class="p-5">
            <!-- Rendered by ShortcutManager.renderCheatSheet() -->
        </div>
    </div>
</div>
```

**Step 2: Commit**

```bash
git add frontend/index.html
git commit -m "feat: add cheat sheet overlay markup"
```

---

### Task 3: Register All Shortcuts & Migrate Existing Handlers

**Files:**
- Modify: `frontend/js/app.js` (remove old keydown handlers, register shortcuts, init manager)
- Modify: `frontend/js/modals.js` (remove old lightbox keydown handler, keep function references)

**Step 1: Modify app.js — Remove the Escape-to-close-drawer keydown listener**

Remove lines 25-30 in `app.js`:

```javascript
// REMOVE THIS BLOCK:
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !navDrawer.classList.contains('translate-x-full')) {
        e.stopImmediatePropagation();
        closeDrawer();
    }
});
```

**Step 2: Modify app.js — Remove the Cmd+C/Ctrl+C keydown listener**

Remove lines 269-306 in `app.js` (the `document.addEventListener('keydown', (e) => { if (!(e.key === 'c' ...` block).

**Step 3: Modify app.js — Remove the lightbox keydown listener registration**

Remove line 267 in `app.js`:

```javascript
// REMOVE THIS LINE:
document.addEventListener('keydown', handleLightboxKeydown);
```

**Step 4: Modify app.js — Add shortcut registrations and init in the window load handler**

In the `window.addEventListener('load', async () => { ... })` block (around line 386), add shortcut registration and init call AFTER all existing initialization but BEFORE setting `window.__appReady = true`:

```javascript
    // --- Register Keyboard Shortcuts ---

    // System
    ShortcutManager.register({
        id: 'show-cheatsheet', label: 'Show Shortcuts', category: 'system',
        defaultKey: 'shift+?', context: 'global',
        callback: () => {
            if (ShortcutManager.isCheatSheetOpen()) {
                ShortcutManager.closeCheatSheet();
            } else {
                ShortcutManager.openCheatSheet();
            }
        }
    });
    ShortcutManager.register({
        id: 'close-modal', label: 'Close / Dismiss', category: 'system',
        defaultKey: 'Escape', context: 'global',
        callback: () => {
            if (ShortcutManager.isCheatSheetOpen()) {
                ShortcutManager.closeCheatSheet();
                return;
            }
            if (!navDrawer.classList.contains('translate-x-full')) {
                closeDrawer();
                return;
            }
        }
    });

    // Navigation
    ShortcutManager.register({
        id: 'focus-search', label: 'Focus Search', category: 'navigation',
        defaultKey: '/', context: 'gallery',
        callback: () => {
            document.getElementById('search-input')?.focus();
            ShortcutManager.clearFocus();
        }
    });
    ShortcutManager.register({
        id: 'toggle-archive', label: 'Toggle Archive View', category: 'navigation',
        defaultKey: 'a', context: 'gallery',
        callback: () => toggleViewMode()
    });
    ShortcutManager.register({
        id: 'open-watch', label: 'Open Watch View', category: 'navigation',
        defaultKey: 'w', context: 'gallery',
        callback: () => { if (typeof toggleWatchView === 'function') toggleWatchView(); }
    });
    ShortcutManager.register({
        id: 'open-settings', label: 'Open Settings', category: 'navigation',
        defaultKey: ',', context: 'global',
        callback: () => { if (typeof openSettings === 'function') openSettings(); }
    });
    ShortcutManager.register({
        id: 'open-plugins', label: 'Open Plugins', category: 'navigation',
        defaultKey: 'p', context: 'gallery',
        callback: () => { if (typeof openPluginsModal === 'function') openPluginsModal(); }
    });
    ShortcutManager.register({
        id: 'open-drawer', label: 'Open Menu', category: 'navigation',
        defaultKey: 'm', context: 'global',
        callback: () => openDrawer()
    });

    // Gallery
    ShortcutManager.register({
        id: 'upload-clip', label: 'Upload / Add Clip', category: 'gallery',
        defaultKey: 'n', context: 'gallery',
        callback: () => fileInput.click()
    });
    ShortcutManager.register({
        id: 'select-all', label: 'Select All', category: 'gallery',
        defaultKey: 'mod+a', context: 'gallery',
        callback: () => toggleSelectAll()
    });
    ShortcutManager.register({
        id: 'clear-temp', label: 'Clear Temp Files', category: 'gallery',
        defaultKey: 'mod+shift+Delete', context: 'gallery',
        callback: () => deleteAllTempFiles()
    });

    // Grid navigation
    ShortcutManager.register({
        id: 'grid-up', label: 'Navigate Up', category: 'gallery',
        defaultKey: 'ArrowUp', context: 'gallery',
        callback: () => ShortcutManager.navigateGrid('ArrowUp')
    });
    ShortcutManager.register({
        id: 'grid-down', label: 'Navigate Down', category: 'gallery',
        defaultKey: 'ArrowDown', context: 'gallery',
        callback: () => ShortcutManager.navigateGrid('ArrowDown')
    });
    ShortcutManager.register({
        id: 'grid-left', label: 'Navigate Left', category: 'gallery',
        defaultKey: 'ArrowLeft', context: 'gallery',
        callback: () => ShortcutManager.navigateGrid('ArrowLeft')
    });
    ShortcutManager.register({
        id: 'grid-right', label: 'Navigate Right', category: 'gallery',
        defaultKey: 'ArrowRight', context: 'gallery',
        callback: () => ShortcutManager.navigateGrid('ArrowRight')
    });

    // Clip actions (when a clip has keyboard focus)
    ShortcutManager.register({
        id: 'clip-open', label: 'Open in Lightbox', category: 'clip',
        defaultKey: 'Enter', context: 'clip',
        callback: () => {
            const clip = ShortcutManager.getFocusedClip();
            if (!clip) return;
            const viewBtn = clip.querySelector('[data-action="open-lightbox"]');
            if (viewBtn) viewBtn.click();
        }
    });
    ShortcutManager.register({
        id: 'clip-copy', label: 'Copy Clip', category: 'clip',
        defaultKey: 'c', context: 'clip',
        callback: () => {
            const id = ShortcutManager.getFocusedClipId();
            if (id) copyFileToClipboard(id);
        }
    });
    ShortcutManager.register({
        id: 'clip-delete', label: 'Delete Clip', category: 'clip',
        defaultKey: 'd', context: 'clip',
        callback: () => {
            const id = ShortcutManager.getFocusedClipId();
            if (id) deleteClip(id);
        }
    });
    ShortcutManager.register({
        id: 'clip-archive', label: 'Archive / Unarchive', category: 'clip',
        defaultKey: 'e', context: 'clip',
        callback: () => {
            const id = ShortcutManager.getFocusedClipId();
            if (id) toggleArchiveClip(id);
        }
    });
    ShortcutManager.register({
        id: 'clip-download', label: 'Download Clip', category: 'clip',
        defaultKey: 'mod+d', context: 'clip',
        callback: () => {
            const id = ShortcutManager.getFocusedClipId();
            if (id) saveClipToFile(id);
        }
    });
    ShortcutManager.register({
        id: 'clip-tag', label: 'Tag Clip', category: 'clip',
        defaultKey: 't', context: 'clip',
        callback: () => {
            const clip = ShortcutManager.getFocusedClip();
            if (!clip) return;
            const id = parseInt(clip.dataset.id, 10);
            const tagBtn = clip.querySelector('[data-action="tags"]') || clip.querySelector('[data-action="menu"]');
            if (tagBtn) openTagPopover(id, tagBtn);
        }
    });
    ShortcutManager.register({
        id: 'clip-select', label: 'Select / Deselect', category: 'clip',
        defaultKey: 'Space', context: 'clip',
        callback: () => {
            const clip = ShortcutManager.getFocusedClip();
            if (!clip) return;
            const checkbox = clip.querySelector('.clip-checkbox');
            if (checkbox) {
                checkbox.checked = !checkbox.checked;
                checkbox.dispatchEvent(new Event('change', { bubbles: true }));
            }
        }
    });

    // Bulk actions
    ShortcutManager.register({
        id: 'bulk-clear', label: 'Clear Selection', category: 'bulk',
        defaultKey: 'Escape', context: 'bulk',
        callback: () => cancelSelection()
    });
    ShortcutManager.register({
        id: 'bulk-copy', label: 'Copy Selected', category: 'bulk',
        defaultKey: 'c', context: 'bulk',
        callback: () => bulkCopyFiles()
    });
    ShortcutManager.register({
        id: 'bulk-delete', label: 'Delete Selected', category: 'bulk',
        defaultKey: 'd', context: 'bulk',
        callback: () => bulkDelete()
    });
    ShortcutManager.register({
        id: 'bulk-archive', label: 'Archive Selected', category: 'bulk',
        defaultKey: 'e', context: 'bulk',
        callback: () => bulkArchive()
    });
    ShortcutManager.register({
        id: 'bulk-download', label: 'Download Selected', category: 'bulk',
        defaultKey: 'mod+d', context: 'bulk',
        callback: () => bulkDownload()
    });
    ShortcutManager.register({
        id: 'bulk-tag', label: 'Tag Selected', category: 'bulk',
        defaultKey: 't', context: 'bulk',
        callback: () => {
            const btn = document.getElementById('bulk-tag-btn');
            if (btn) btn.click();
        }
    });

    // Lightbox
    ShortcutManager.register({
        id: 'lightbox-close', label: 'Close Lightbox', category: 'lightbox',
        defaultKey: 'Escape', context: 'lightbox',
        callback: () => {
            // Close any open menu first
            const fileMenu = document.getElementById('lightbox-file-menu');
            if (fileMenu) {
                closeLightboxFileMenu();
                const trigger = document.getElementById('lightbox-file-menu-trigger');
                if (trigger) trigger.focus();
                return;
            }
            const pluginMenu = document.getElementById('lightbox-plugin-menu');
            if (pluginMenu) {
                closeLightboxPluginMenu();
                const trigger = document.getElementById('lightbox-plugin-menu-trigger');
                if (trigger) trigger.focus();
                return;
            }
            closeLightbox();
        }
    });
    ShortcutManager.register({
        id: 'lightbox-next', label: 'Next Image', category: 'lightbox',
        defaultKey: 'ArrowRight', context: 'lightbox',
        callback: () => showNextImage()
    });
    ShortcutManager.register({
        id: 'lightbox-prev', label: 'Previous Image', category: 'lightbox',
        defaultKey: 'ArrowLeft', context: 'lightbox',
        callback: () => showPrevImage()
    });
    ShortcutManager.register({
        id: 'lightbox-zoom-in', label: 'Zoom In', category: 'lightbox',
        defaultKey: '+', context: 'lightbox',
        callback: () => {
            // Reuse the zoom slider logic
            const slider = document.getElementById('lightbox-zoom-slider');
            if (slider) {
                slider.value = Math.min(400, parseInt(slider.value) + 25);
                slider.dispatchEvent(new Event('input'));
            }
        }
    });
    ShortcutManager.register({
        id: 'lightbox-zoom-out', label: 'Zoom Out', category: 'lightbox',
        defaultKey: '-', context: 'lightbox',
        callback: () => {
            const slider = document.getElementById('lightbox-zoom-slider');
            if (slider) {
                slider.value = Math.max(100, parseInt(slider.value) - 25);
                slider.dispatchEvent(new Event('input'));
            }
        }
    });
    ShortcutManager.register({
        id: 'lightbox-edit', label: 'Open Editor', category: 'lightbox',
        defaultKey: 'e', context: 'lightbox',
        callback: () => {
            const clip = imageClips[currentLightboxIndex];
            if (clip && isEditableType(clip.content_type)) {
                closeLightbox();
                setTimeout(() => openEditor(clip.id), 350);
            }
        }
    });
    ShortcutManager.register({
        id: 'lightbox-copy', label: 'Copy Image', category: 'lightbox',
        defaultKey: 'mod+c', context: 'lightbox',
        callback: () => {
            const clip = imageClips[currentLightboxIndex];
            if (clip) copyClipContents(clip.id);
        }
    });

    // Initialize the shortcut manager (loads user overrides and starts listening)
    await ShortcutManager.init();
```

**Step 5: Modify modals.js — Remove the standalone handleLightboxKeydown listener usage**

The `handleLightboxKeydown` function in modals.js (lines 499-540) should be kept as a utility but is no longer registered as a global keydown listener. It's now called through the ShortcutManager registrations in app.js. The `document.addEventListener('keydown', handleLightboxKeydown)` was in app.js (removed in Step 3). The `handleLightboxKeydown` function itself stays in modals.js but is now unused — the Tab focus trap logic needs to stay. Add this separate Tab-only handler at the end of `initLightboxGestures()`:

In modals.js `initLightboxGestures()` function, add a Tab focus trap listener:

```javascript
    // Tab focus trap for lightbox (not routed through ShortcutManager since Tab shouldn't be rebindable)
    lightbox.addEventListener('keydown', (e) => {
        if (e.key !== 'Tab') return;
        if (!lightbox.classList.contains('active')) return;

        const focusableElements = lightbox.querySelectorAll('button');
        const first = focusableElements[0];
        const last = focusableElements[focusableElements.length - 1];

        if (e.shiftKey && document.activeElement === first) {
            last.focus();
            e.preventDefault();
        } else if (!e.shiftKey && document.activeElement === last) {
            first.focus();
            e.preventDefault();
        }
    });
```

**Step 6: Wire up cheat sheet close button and backdrop click**

In `app.js` window load handler (after shortcut registrations), add:

```javascript
    // Cheat sheet close handlers
    const cheatsheetClose = document.getElementById('shortcuts-cheatsheet-close');
    const cheatsheetOverlay = document.getElementById('shortcuts-cheatsheet');
    if (cheatsheetClose) {
        cheatsheetClose.addEventListener('click', () => ShortcutManager.closeCheatSheet());
    }
    if (cheatsheetOverlay) {
        cheatsheetOverlay.addEventListener('click', (e) => {
            if (e.target === cheatsheetOverlay) ShortcutManager.closeCheatSheet();
        });
    }
```

**Step 7: Clear grid focus when gallery re-renders**

In `frontend/js/wails-api.js` or wherever `loadClips` / `renderGallery` lives, add a call to `ShortcutManager.clearFocus()` at the start of gallery rendering. Find the function that re-renders the gallery and add:

```javascript
if (typeof ShortcutManager !== 'undefined') ShortcutManager.clearFocus();
```

**Step 8: Add test helpers for shortcuts**

In app.js, extend `window.__testHelpers` to expose shortcut state:

```javascript
Object.assign(window.__testHelpers, {
    getShortcutManager: () => typeof ShortcutManager !== 'undefined' ? ShortcutManager : null,
});
```

**Step 9: Run tests**

Run: `cd e2e && npm test`
Expected: All existing tests pass. The migrated keyboard handlers should work identically.

**Step 10: Commit**

```bash
git add frontend/js/app.js frontend/js/modals.js frontend/js/shortcuts.js frontend/index.html
git commit -m "feat: register all shortcuts and migrate existing keyboard handlers"
```

---

### Task 4: Add Settings UI for Shortcut Rebinding

**Files:**
- Modify: `frontend/index.html` (add shortcuts section in settings modal)
- Modify: `frontend/js/settings.js` (add rendering and rebinding logic)

**Step 1: Add shortcuts section HTML in settings modal**

In `frontend/index.html`, inside the settings modal `<div class="p-5 space-y-6">` container, add a new section AFTER the "Plugin Updates" section (before the closing `</div>` of the content area):

```html
            <!-- Keyboard Shortcuts Section -->
            <div class="pt-4 border-t border-stone-100" id="settings-shortcuts-section" data-testid="settings-shortcuts-section">
                <h3 class="text-xs font-semibold text-stone-600 uppercase tracking-wider mb-3 flex items-center gap-2">
                    <svg class="w-3.5 h-3.5 opacity-60" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M6.75 7.5l3 2.25-3 2.25m4.5 0h3m-9 8.25h13.5A2.25 2.25 0 0021 18V6a2.25 2.25 0 00-2.25-2.25H5.25A2.25 2.25 0 003 6v12a2.25 2.25 0 002.25 2.25z" />
                    </svg>
                    Keyboard Shortcuts
                </h3>
                <p class="text-[11px] text-stone-500 mb-3">
                    Click a key badge to rebind. Press the new key combination to assign it.
                </p>
                <div id="shortcuts-settings-list" data-testid="shortcuts-settings-list">
                    <!-- Rendered by renderShortcutsSettings() -->
                </div>
                <button id="shortcuts-reset-btn" data-testid="shortcuts-reset-btn"
                    class="mt-3 border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-[11px] font-medium py-1.5 px-3 rounded-md transition-colors">
                    Reset All to Defaults
                </button>
            </div>
```

**Step 2: Add rendering logic to settings.js**

Add these functions to `frontend/js/settings.js`:

```javascript
// --- Keyboard Shortcuts Settings ---

let recordingActionId = null;
let recordingKeydownHandler = null;

function renderShortcutsSettings() {
    const container = document.getElementById('shortcuts-settings-list');
    if (!container || typeof ShortcutManager === 'undefined') return;

    const groups = new Map();
    for (const [id, action] of ShortcutManager.actions) {
        const cat = action.category || 'other';
        if (!groups.has(cat)) groups.set(cat, []);
        groups.get(cat).push({ id, ...action });
    }

    const categoryOrder = ['navigation', 'gallery', 'clip', 'lightbox', 'bulk', 'system'];
    const categoryLabels = {
        navigation: 'Navigation',
        gallery: 'Gallery',
        clip: 'Clip Actions',
        lightbox: 'Lightbox',
        bulk: 'Bulk Actions',
        system: 'System',
    };

    let html = '';

    for (const cat of categoryOrder) {
        const items = groups.get(cat);
        if (!items || items.length === 0) continue;

        html += `<div class="mb-4">`;
        html += `<h4 class="text-[10px] font-semibold text-stone-400 uppercase tracking-wider mb-1.5">${categoryLabels[cat] || cat}</h4>`;

        for (const item of items) {
            const combo = ShortcutManager.getEffectiveCombo(item.id);
            const display = ShortcutManager.comboToDisplay(combo);
            html += `<div class="flex items-center justify-between py-1.5 px-1 hover:bg-stone-50 rounded transition-colors" data-testid="shortcut-row-${item.id}">`;
            html += `<span class="text-xs text-stone-700">${escapeHtml(item.label)}</span>`;
            html += `<button class="shortcut-key-badge bg-stone-100 border border-stone-200 rounded px-2 py-0.5 text-[11px] font-mono text-stone-600 min-w-[28px] text-center hover:border-stone-400 hover:bg-stone-50 transition-colors cursor-pointer" data-action-id="${item.id}" data-testid="shortcut-badge-${item.id}">${escapeHtml(display)}</button>`;
            html += `</div>`;
        }

        html += `</div>`;
    }

    container.innerHTML = html;

    // Add click handlers to badges
    container.querySelectorAll('.shortcut-key-badge').forEach(badge => {
        badge.addEventListener('click', () => startRecording(badge.dataset.actionId, badge));
    });
}

function startRecording(actionId, badgeEl) {
    // Cancel any existing recording
    stopRecording();

    recordingActionId = actionId;
    badgeEl.textContent = '...';
    badgeEl.classList.add('ring-2', 'ring-stone-400', 'animate-pulse');

    recordingKeydownHandler = (e) => {
        e.preventDefault();
        e.stopPropagation();

        // Escape cancels recording
        if (e.key === 'Escape') {
            stopRecording();
            renderShortcutsSettings();
            return;
        }

        // Ignore bare modifier presses
        if (['Control', 'Meta', 'Shift', 'Alt'].includes(e.key)) return;

        const combo = ShortcutManager.eventToCombo(e);
        if (!combo) return;

        // Check for conflict
        const conflict = ShortcutManager.findConflict(recordingActionId, combo);
        if (conflict) {
            const proceed = confirm(`"${combo}" is already used by "${conflict.label}". Override?`);
            if (!proceed) {
                stopRecording();
                renderShortcutsSettings();
                return;
            }
            // Remove the conflicting binding
            ShortcutManager.setOverride(conflict.id, null);
        }

        ShortcutManager.setOverride(recordingActionId, combo);
        stopRecording();
        renderShortcutsSettings();
    };

    document.addEventListener('keydown', recordingKeydownHandler, true);
}

function stopRecording() {
    if (recordingKeydownHandler) {
        document.removeEventListener('keydown', recordingKeydownHandler, true);
        recordingKeydownHandler = null;
    }
    recordingActionId = null;
}

// Wire up reset button
document.getElementById('shortcuts-reset-btn')?.addEventListener('click', () => {
    if (typeof ShortcutManager !== 'undefined') {
        ShortcutManager.resetAllToDefaults();
        renderShortcutsSettings();
        if (typeof showToast === 'function') showToast('Shortcuts reset to defaults');
    }
});
```

**Step 3: Call renderShortcutsSettings() when opening settings**

In `settings.js`, modify the `openSettings()` function to also render the shortcuts section:

```javascript
function openSettings() {
    renderHiddenTagsSettings();
    loadUpdateInterval();
    renderShortcutsSettings();  // ADD THIS LINE
    settingsModal.classList.remove('opacity-0', 'pointer-events-none');
    // ... rest unchanged
}
```

**Step 4: Stop recording when settings closes**

In `settings.js`, modify the `closeSettings()` function:

```javascript
function closeSettings() {
    stopRecording();  // ADD THIS LINE
    settingsModal.classList.add('opacity-0', 'pointer-events-none');
    // ... rest unchanged
}
```

**Step 5: Run tests**

Run: `cd e2e && npm test`
Expected: All existing tests pass.

**Step 6: Commit**

```bash
git add frontend/index.html frontend/js/settings.js
git commit -m "feat: add keyboard shortcuts rebinding UI in settings"
```

---

### Task 5: Add Selectors for Test Infrastructure

**Files:**
- Modify: `e2e/helpers/selectors.ts`

**Step 1: Add shortcut-related selectors**

Add a new section to the selectors object in `e2e/helpers/selectors.ts`:

```typescript
  // Keyboard Shortcuts
  shortcuts: {
    cheatsheet: '[data-testid="shortcuts-cheatsheet"]',
    cheatsheetClose: '#shortcuts-cheatsheet-close',
    cheatsheetContent: '#shortcuts-cheatsheet-content',
    settingsSection: '[data-testid="settings-shortcuts-section"]',
    settingsList: '[data-testid="shortcuts-settings-list"]',
    resetButton: '[data-testid="shortcuts-reset-btn"]',
    shortcutRow: (id: string) => `[data-testid="shortcut-row-${id}"]`,
    shortcutBadge: (id: string) => `[data-testid="shortcut-badge-${id}"]`,
    focusedClip: '.clip-focused',
  },
```

**Step 2: Update fastReset in test-fixtures.ts**

In `e2e/fixtures/test-fixtures.ts`, add cheat sheet cleanup to the `fastReset()` method's modal cleanup section:

```javascript
      // Shortcuts cheatsheet
      const cheatsheet = document.getElementById('shortcuts-cheatsheet');
      if (cheatsheet) {
        cheatsheet.classList.remove('opacity-100');
        cheatsheet.classList.add('opacity-0', 'pointer-events-none');
      }

      // Clear grid focus
      document.querySelector('.clip-focused')?.classList.remove('clip-focused');
```

And in the `closeAllModalsSafe()` method, add:

```javascript
    // Cheat sheet
    try {
      const cheatsheetOpen = await this.page.evaluate(() => {
        const el = document.getElementById('shortcuts-cheatsheet');
        return el ? el.classList.contains('opacity-100') : false;
      });
      if (cheatsheetOpen) {
        await this.page.evaluate(() => {
          const el = document.getElementById('shortcuts-cheatsheet');
          if (el) {
            el.classList.remove('opacity-100');
            el.classList.add('opacity-0', 'pointer-events-none');
          }
        });
      }
    } catch {
      // Ignore
    }
```

**Step 3: Add AppHelper methods for shortcuts**

Add these methods to `AppHelper` in `e2e/fixtures/test-fixtures.ts`:

```typescript
  // ==================== Keyboard Shortcuts ====================

  async pressKey(key: string, modifiers?: { meta?: boolean; ctrl?: boolean; shift?: boolean; alt?: boolean }): Promise<void> {
    const mods: string[] = [];
    if (modifiers?.meta) mods.push('Meta');
    if (modifiers?.ctrl) mods.push('Control');
    if (modifiers?.shift) mods.push('Shift');
    if (modifiers?.alt) mods.push('Alt');

    const combo = [...mods, key].join('+');
    await this.page.keyboard.press(combo);
  }

  async isCheatSheetOpen(): Promise<boolean> {
    return this.page.evaluate(() => {
      const el = document.getElementById('shortcuts-cheatsheet');
      return el ? el.classList.contains('opacity-100') : false;
    });
  }

  async openCheatSheet(): Promise<void> {
    await this.page.keyboard.press('Shift+?');
    await this.page.waitForSelector('[data-testid="shortcuts-cheatsheet"].opacity-100', { timeout: 5000 });
  }

  async closeCheatSheet(): Promise<void> {
    await this.page.keyboard.press('Escape');
    await this.page.waitForSelector('[data-testid="shortcuts-cheatsheet"].opacity-0', { timeout: 5000 });
  }

  async getFocusedClipIndex(): Promise<number> {
    return this.page.evaluate(() => {
      // @ts-ignore
      return typeof ShortcutManager !== 'undefined' ? ShortcutManager.focusedClipIndex : -1;
    });
  }

  async isFocusedClipVisible(): Promise<boolean> {
    return this.page.evaluate(() => {
      return !!document.querySelector('.clip-focused');
    });
  }
```

**Step 4: Commit**

```bash
git add e2e/helpers/selectors.ts e2e/fixtures/test-fixtures.ts
git commit -m "feat: add e2e selectors and helpers for keyboard shortcuts"
```

---

### Task 6: Write E2E Tests

**Files:**
- Create: `e2e/tests/shortcuts/shortcuts.spec.ts`

**Step 1: Write the test file**

```typescript
import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
import { generateTestImage, createTempFile } from '../../helpers/test-data.js';
import * as path from 'path';

test.describe('Keyboard Shortcuts', () => {

  test.describe('Cheat Sheet', () => {
    test('should open cheat sheet with ? key and close with Escape', async ({ app }) => {
      // Focus body to ensure shortcuts work
      await app.page.locator('body').click();

      // Open cheat sheet
      await app.page.keyboard.press('Shift+/'); // ? is Shift+/
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-100/);

      // Should show shortcut categories
      const content = app.page.locator(selectors.shortcuts.cheatsheetContent);
      await expect(content).toContainText('Navigation');
      await expect(content).toContainText('Gallery');

      // Close with Escape
      await app.page.keyboard.press('Escape');
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-0/);
    });

    test('should close cheat sheet by clicking backdrop', async ({ app }) => {
      await app.page.locator('body').click();
      await app.page.keyboard.press('Shift+/');
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-100/);

      // Click the backdrop (the overlay itself, not the content panel)
      await app.page.locator(selectors.shortcuts.cheatsheet).click({ position: { x: 10, y: 10 } });
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-0/);
    });
  });

  test.describe('Navigation Shortcuts', () => {
    test('should toggle archive view with "a" key', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await app.page.locator('body').click();
      await app.page.keyboard.press('a');

      // Should be in archive view (empty since clip is not archived)
      await app.expectClipCount(0);

      // Press a again to go back
      await app.page.keyboard.press('a');
      await app.expectClipCount(1);
    });

    test('should focus search with "/" key', async ({ app }) => {
      await app.page.locator('body').click();
      await app.page.keyboard.press('/');

      const searchInput = app.page.locator(selectors.header.searchInput);
      await expect(searchInput).toBeFocused();
    });

    test('should open settings with "," key', async ({ app }) => {
      await app.page.locator('body').click();
      await app.page.keyboard.press(',');

      await expect(app.page.locator(selectors.settings.modal)).toHaveClass(/opacity-100/);
    });
  });

  test.describe('Gallery Shortcuts', () => {
    test('should not fire shortcuts when typing in search input', async ({ app }) => {
      // Focus search
      await app.page.locator(selectors.header.searchInput).click();
      await app.page.keyboard.type('a');

      // Should not toggle archive view - search should have the 'a'
      const searchInput = app.page.locator(selectors.header.searchInput);
      await expect(searchInput).toHaveValue('a');
    });
  });

  test.describe('Grid Navigation', () => {
    test('should focus first clip on arrow key press', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.expectClipCount(2);

      await app.page.locator('body').click();
      await app.page.keyboard.press('ArrowRight');

      // Should have a focused clip
      await expect(app.page.locator(selectors.shortcuts.focusedClip)).toBeVisible();
    });

    test('should navigate between clips with arrow keys', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.expectClipCount(2);

      await app.page.locator('body').click();

      // Focus first clip
      await app.page.keyboard.press('ArrowRight');
      const idx1 = await app.getFocusedClipIndex();
      expect(idx1).toBe(0);

      // Move right
      await app.page.keyboard.press('ArrowRight');
      const idx2 = await app.getFocusedClipIndex();
      expect(idx2).toBe(1);
    });

    test('should open lightbox on Enter when clip is focused', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await app.page.locator('body').click();
      await app.page.keyboard.press('ArrowRight');
      await app.page.keyboard.press('Enter');

      expect(await app.isLightboxOpen()).toBe(true);
    });
  });

  test.describe('Lightbox Shortcuts', () => {
    test('should close lightbox with Escape', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.openLightbox(path.basename(imagePath));

      await app.page.keyboard.press('Escape');
      expect(await app.isLightboxOpen()).toBe(false);
    });

    test('should navigate images with arrow keys in lightbox', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);

      await app.openLightbox(path.basename(imagePath2));

      // Navigate to next image
      await app.page.keyboard.press('ArrowRight');

      // Verify we navigated (caption should change)
      const caption = app.page.locator('#lightbox-caption');
      await expect(caption).toContainText(path.basename(imagePath1));
    });
  });

  test.describe('Settings UI', () => {
    test('should show shortcuts section in settings', async ({ app }) => {
      await app.openSettingsModal();

      const section = app.page.locator(selectors.shortcuts.settingsSection);
      await expect(section).toBeVisible();

      // Should have shortcut rows
      const list = app.page.locator(selectors.shortcuts.settingsList);
      await expect(list).toContainText('Toggle Archive View');
      await expect(list).toContainText('Focus Search');
    });

    test('should rebind a shortcut via click-to-record', async ({ app }) => {
      await app.openSettingsModal();

      // Click the badge for toggle-archive
      const badge = app.page.locator(selectors.shortcuts.shortcutBadge('toggle-archive'));
      await badge.click();

      // Badge should be in recording mode (shows "...")
      await expect(badge).toContainText('...');

      // Press a new key
      await app.page.keyboard.press('x');

      // Badge should now show the new key
      await expect(badge).toContainText('X');

      // Close settings
      await app.closeSettingsModal();

      // Verify the new shortcut works
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await app.page.locator('body').click();
      await app.page.keyboard.press('x');
      await app.expectClipCount(0); // Switched to archive view
    });

    test('should reset shortcuts to defaults', async ({ app }) => {
      await app.openSettingsModal();

      // First rebind something
      const badge = app.page.locator(selectors.shortcuts.shortcutBadge('toggle-archive'));
      await badge.click();
      await app.page.keyboard.press('x');
      await expect(badge).toContainText('X');

      // Click reset
      await app.page.locator(selectors.shortcuts.resetButton).click();

      // Badge should show default again
      await expect(badge).toContainText('A');
    });
  });
});
```

**Step 2: Run the new tests**

Run: `cd e2e && npm test -- --grep "Keyboard Shortcuts"`
Expected: All shortcut tests pass.

**Step 3: Run full test suite**

Run: `cd e2e && npm test`
Expected: All tests pass (both new and existing).

**Step 4: Commit**

```bash
git add e2e/tests/shortcuts/shortcuts.spec.ts
git commit -m "test: add e2e tests for keyboard shortcuts system"
```

---

### Task 7: Final Integration & Polish

**Files:**
- Possibly modify: any files with issues found during testing

**Step 1: Run full test suite and fix any failures**

Run: `cd e2e && npm test`
Expected: All tests pass. If any fail, fix them.

**Step 2: Manual smoke test**

Run: `make dev`

Verify:
- Press `?` (Shift+/) → cheat sheet opens, shows all shortcuts grouped
- Press `Escape` → cheat sheet closes
- Press `a` → toggles archive view
- Press `/` → search input focused
- Press `,` → settings opens
- Arrow keys navigate the clip grid (focus ring visible)
- `Enter` on focused clip opens lightbox
- `c` on focused clip copies it
- In lightbox: arrows navigate, Escape closes
- In settings: shortcuts section visible, click badge to rebind, reset works
- Shortcuts don't fire when typing in search input

**Step 3: Final commit if any polish was needed**

```bash
git add -A
git commit -m "fix: polish keyboard shortcuts integration"
```
