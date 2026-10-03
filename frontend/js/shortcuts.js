// --- Shortcut Manager ---

const ShortcutManager = (() => {
    // Registry: Map<actionId, ActionDef>
    const actions = new Map();

    // Current bindings: Map<comboString, actionId> per context
    // Built from defaults + user overrides
    let bindingsByContext = new Map(); // Map<context, Map<combo, actionId>>

    // User overrides loaded from backend (only stores differences from defaults)
    let userOverrides = {}; // { actionId: comboString | null }

    // Whether the manager is initialized
    let initialized = false;

    // Shared category ordering and labels (used by cheat sheet and settings)
    const CATEGORY_ORDER = ['navigation', 'gallery', 'clip', 'lightbox', 'editor', 'comparison', 'bulk', 'import', 'system'];
    const CATEGORY_LABELS = {
        navigation: 'Navigation',
        gallery: 'Gallery',
        clip: 'Clip Actions',
        lightbox: 'Lightbox',
        editor: 'Image Editor',
        comparison: 'Comparison',
        bulk: 'Bulk Actions',
        import: 'Import Folder',
        system: 'System',
    };

    // Platform detection (cached — never changes during session)
    const isMac = navigator.userAgentData
        ? navigator.userAgentData.platform === 'macOS'
        : /Mac|iPhone|iPad/.test(navigator.userAgent);

    // Cached DOM references (gallery is the main hot-path element)
    function getGallery() {
        return document.getElementById('gallery');
    }

    // --- Context Detection ---

    // Full-screen viewers own shortcut contexts of their own, so the generic
    // "a dialog is open" check must not count them as blocking.
    const VIEWER_IDS = new Set(['lightbox', 'editor-modal', 'comparison-modal', 'import-wizard-modal']);

    // Every modal in the app is marked inert while closed; open ones drop it
    // and show themselves (no opacity-0 / hidden).
    function isModalShown(el) {
        return !!el && !el.hasAttribute('inert') && !el.classList.contains('hidden')
            && !el.classList.contains('opacity-0');
    }

    // App dialogs are top-level layers. User content — a plugin's markdown
    // result, a previewed clip — always renders inside one of them, so an
    // aria-modal nested in another aria-modal element is content that happens
    // to carry the attribute, never a dialog. Counting it would leave every
    // shortcut dead for as long as it sits in the DOM.
    function isAppDialog(el) {
        return !el.parentElement?.closest('[aria-modal="true"]');
    }

    function isViewerShown(el) {
        if (!el) return false;
        if (el.id === 'import-wizard-modal') return isModalShown(el);
        return el.classList.contains('active');
    }

    // Painted above `other`: higher z-index, or later in the DOM on a tie.
    function isAbove(el, other) {
        const dz = zIndexOf(el) - zIndexOf(other);
        if (dz !== 0) return dz > 0;
        return !!(other.compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING);
    }

    // The first open dialog that is not a full-screen viewer, if any. Generic
    // on purpose: a hand-kept list of modal ids drifts every time a modal is
    // added, and each one it misses lets gallery keys fire behind it. A dialog
    // painted beneath an open viewer is not the one the user is looking at;
    // it must not take the viewer's keys (nor leave Escape with nothing to do).
    function getOpenDialog() {
        let topViewer = null;
        for (const id of VIEWER_IDS) {
            const viewer = document.getElementById(id);
            if (isViewerShown(viewer) && (!topViewer || isAbove(viewer, topViewer))) topViewer = viewer;
        }
        for (const el of document.querySelectorAll('[aria-modal="true"]')) {
            if (VIEWER_IDS.has(el.id) || !isAppDialog(el) || !isModalShown(el)) continue;
            if (topViewer && !isAbove(el, topViewer)) continue;
            return el;
        }
        return null;
    }

    function getActiveContexts() {
        // An open dialog (confirm, prompt, settings, share modals, the cheat
        // sheet, ...) owns the keyboard. Escape still reaches it through
        // closeTopModalOverlay, which runs before context dispatch.
        if (getOpenDialog()) return [];

        const contexts = ['global'];
        const lightbox = document.getElementById('lightbox');
        const editorModal = document.getElementById('editor-modal');
        const comparisonModal = document.getElementById('comparison-modal');
        const watchView = document.getElementById('watch-view');
        const bulkToolbar = document.getElementById('bulk-toolbar');

        if (editorModal && editorModal.classList.contains('active')) {
            contexts.push('editor');
            const imageEditorView = document.getElementById('image-editor-view');
            if (imageEditorView && !imageEditorView.classList.contains('hidden')) {
                contexts.push('image-editor');
            } else {
                contexts.push('text-editor');
            }
            return contexts;
        }
        if (comparisonModal && comparisonModal.classList.contains('active')) {
            contexts.push('comparison');
            return contexts;
        }
        // Import wizard: a full-screen modal with its own single-letter keys.
        const importWizardModal = document.getElementById('import-wizard-modal');
        if (importWizardModal && !importWizardModal.classList.contains('opacity-0')) {
            contexts.push('import-wizard');
            return contexts;
        }

        // Nav drawer open — suppress all shortcuts (drawer has its own key handling)
        const navDrawer = document.getElementById('nav-drawer');
        if (navDrawer && !navDrawer.classList.contains('translate-x-full')) return [];

        if (lightbox && lightbox.classList.contains('active')) {
            contexts.push('lightbox');
            return contexts;
        }

        // From here on the main window is showing: app-level keys (menu,
        // settings, cheat sheet) are live. They are deliberately absent above,
        // where they would open their overlay behind a full-screen viewer.
        contexts.push('app');

        if (watchView && !watchView.classList.contains('hidden')) {
            contexts.push('watch');
            return contexts;
        }

        // Serve and share views hide the gallery: no gallery/bulk/clip keys.
        for (const id of ['serve-view', 'share-view']) {
            const view = document.getElementById(id);
            if (view && !view.classList.contains('hidden')) return contexts;
        }

        // Gallery-level contexts
        contexts.push('gallery');

        if (bulkToolbar && bulkToolbar.classList.contains('pointer-events-auto')) {
            contexts.push('bulk');
        }

        if (document.activeElement && document.activeElement.matches('#gallery > li')) {
            contexts.push('clip');
        }

        return contexts;
    }

    // --- Key Combo Parsing ---

    // Map unshifted keys to shifted equivalents (US layout).
    // Some browsers (headless Chromium) report the raw key on Shift combos.
    const SHIFT_KEY_MAP = {
        '/': '?', '1': '!', '2': '@', '3': '#', '4': '$', '5': '%',
        '6': '^', '7': '&', '8': '*', '9': '(', '0': ')', '-': '_',
        '=': '+', '[': '{', ']': '}', '\\': '|', ';': ':', "'": '"',
        ',': '<', '.': '>', '`': '~'
    };

    // Set of shifted punctuation chars — shift is implicit in these characters
    const SHIFTED_CHARS = new Set(Object.values(SHIFT_KEY_MAP));

    function eventToCombo(e) {
        const parts = [];
        if (e.metaKey || e.ctrlKey) parts.push('mod');
        if (e.shiftKey) parts.push('shift');
        if (e.altKey) parts.push('alt');

        let key = e.key;
        // Normalize key names
        if (key === ' ') key = 'Space';
        // Normalize shifted punctuation for headless browsers
        if (e.shiftKey && key.length === 1 && SHIFT_KEY_MAP[key]) {
            key = SHIFT_KEY_MAP[key];
        }
        // Strip 'shift' for shifted punctuation chars (shift is implicit in the char).
        // e.g. Shift+= produces '+', Shift+/ produces '?' — no need for explicit shift.
        if (e.shiftKey && key.length === 1 && SHIFTED_CHARS.has(key)) {
            const idx = parts.indexOf('shift');
            if (idx !== -1) parts.splice(idx, 1);
        }
        if (key.length === 1) key = key.toLowerCase();

        // Don't add modifier keys themselves as the key part
        if (['Control', 'Meta', 'Shift', 'Alt'].includes(key)) return null;

        parts.push(key);
        return parts.join('+');
    }

    function comboToDisplay(combo) {
        if (!combo) return '—';
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

        // Don't intercept keys when a context menu is open — it has its own keyboard handler
        if (typeof ContextMenu !== 'undefined' && ContextMenu.isOpen()) return;

        // Input guard: suppress shortcuts when interacting with form fields. Escape
        // normally closes the active modal, except for editor controls that use it
        // locally to dismiss an inline mode without closing the whole editor.
        const tag = e.target.tagName;
        const isEditable = tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || e.target.isContentEditable;
        const handlesEscapeLocally = e.target.matches?.('#editor-filename, #text-editor-find, #text-editor-replace, #canvas-text-input');
        if (isEditable && (e.key !== 'Escape' || handlesEscapeLocally)) return;

        // Escape inside something that dismisses itself — an autocomplete with
        // its suggestions showing, or a picker marked data-owns-escape — is
        // that component's to handle. Closing the dialog around it instead
        // would throw away what the user was typing.
        if (e.key === 'Escape' && e.target.closest?.('[aria-expanded="true"][aria-autocomplete], [data-owns-escape]')) return;

        // A popover marked data-owns-keys runs its own keyboard (arrow keys
        // between menu items) while focus is inside it. Without this the
        // capture-phase dispatch below hands ArrowLeft/Right to whatever is
        // underneath — the lightbox pages to the next clip. Escape still
        // reaches the layer handling below, which closes the popover.
        if (e.key !== 'Escape' && e.target.closest?.('[data-owns-keys]')) return;

        // Escape closes the topmost layer. Runs before context dispatch:
        // dialogs make getActiveContexts() return [], so they can't be
        // handled through it.
        if (e.key === 'Escape') {
            if (closeTopModalOverlay()) {
                e.preventDefault();
                e.stopImmediatePropagation();
                return;
            }
        }

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
        if (activeContexts.length === 0) {
            // The cheat sheet is a dialog, so it blocks every context — but
            // its own key toggles it closed again.
            if (isCheatSheetOpen() && combo === getEffectiveCombo('show-cheatsheet')) {
                e.preventDefault();
                e.stopPropagation();
                closeCheatSheet();
            }
            return;
        }

        // Check contexts in priority order (most specific first)
        // import-wizard > clip > bulk > lightbox > watch > gallery > app > global
        const priority = ['import-wizard', 'clip', 'bulk', 'lightbox', 'image-editor', 'text-editor', 'editor', 'comparison', 'watch', 'gallery', 'app', 'global'];

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
        const gallery = getGallery();
        if (!gallery) return 1;
        const style = getComputedStyle(gallery);
        const columns = style.getPropertyValue('grid-template-columns');
        if (!columns || columns === 'none') return 1;
        return columns.split(' ').filter(c => c.trim()).length;
    }

    function clearFocus() {
        const rover = window.__galleryRover;
        if (rover) rover.reset();
        const clip = getFocusedClip();
        if (clip) clip.blur();
    }

    function getFocusedClip() {
        const active = document.activeElement;
        if (active && active.matches('#gallery > li')) return active;
        return null;
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
            if (typeof showToast === 'function') showToast('Failed to save shortcut', 'error');
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
    // or they are the same. Image-only editor actions live below the shared
    // editor context used by both image and text editors.
    function contextsOverlap(ctx1, ctx2) {
        if (ctx1 === ctx2) return true;
        const hierarchy = {
            global: ['app', 'gallery', 'lightbox', 'editor', 'image-editor', 'text-editor', 'comparison', 'import-wizard', 'watch', 'bulk', 'clip'],
            app: ['gallery', 'watch', 'bulk', 'clip'],
            gallery: ['clip', 'bulk'],
            editor: ['image-editor', 'text-editor'],
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

    let cheatSheetFocusTrapCleanup = null;
    let lastFocusedBeforeCheatSheet = null;

    function openCheatSheet() {
        const overlay = document.getElementById('shortcuts-cheatsheet');
        if (!overlay) return;
        lastFocusedBeforeCheatSheet = document.activeElement;
        overlay.removeAttribute('inert');
        overlay.classList.remove('opacity-0', 'pointer-events-none');
        overlay.classList.add('opacity-100');
        renderCheatSheet();
        if (cheatSheetFocusTrapCleanup) cheatSheetFocusTrapCleanup();
        cheatSheetFocusTrapCleanup = trapFocus(overlay);
        const closeBtn = document.getElementById('shortcuts-cheatsheet-close');
        if (closeBtn) closeBtn.focus();
    }

    function closeCheatSheet() {
        const overlay = document.getElementById('shortcuts-cheatsheet');
        if (!overlay) return;
        if (cheatSheetFocusTrapCleanup) {
            cheatSheetFocusTrapCleanup();
            cheatSheetFocusTrapCleanup = null;
        }
        overlay.classList.add('opacity-0', 'pointer-events-none');
        overlay.classList.remove('opacity-100');
        overlay.setAttribute('inert', '');
        if (lastFocusedBeforeCheatSheet) {
            lastFocusedBeforeCheatSheet.focus();
            lastFocusedBeforeCheatSheet = null;
        }
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

        let html = '<div class="grid grid-cols-1 sm:grid-cols-2 gap-6">';

        for (const cat of CATEGORY_ORDER) {
            const items = groups.get(cat);
            if (!items || items.length === 0) continue;

            html += `<div>`;
            html += `<h3 class="text-[10px] font-semibold text-stone-400 uppercase tracking-wider mb-2">${escapeHTML(CATEGORY_LABELS[cat] || cat)}</h3>`;
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

    // --- Modal Overlay Escape ---

    // Every layer Escape can dismiss, and how. Escape closes only the topmost
    // open one (highest z-index, later in the DOM on a tie), so a confirm
    // stacked on Settings closes the confirm, not Settings beneath it.
    // `close` may return false to decline (Settings while recording a key).
    // Layers without `close` still count: when a full-screen viewer is on top,
    // Escape falls through to its own shortcut context.
    const call = (name, ...args) => () => {
        if (typeof window[name] === 'function') return window[name](...args);
    };
    const ESCAPE_LAYERS = [
        { selector: '#confirm-dialog', close: call('closeConfirmDialog') },
        { selector: '#prompt-dialog', close: call('closePromptDialog') },
        { selector: '#conflict-dialog', close: call('closeConflictDialog', 'skip') },
        { selector: '#path-paste-dialog', close: call('closePathPasteDialog', null) },
        { selector: '#restore-confirm-dialog', close: call('hideRestoreConfirmDialog') },
        { selector: '#plugin-review-modal', close: call('closePluginReview', false) },
        { selector: '#plugin-result-modal', close: call('closePluginResultModal') },
        { selector: '#plugin-options-modal', close: call('closePluginOptionsDialog') },
        { selector: '#metadata-modal', close: call('closeMetadataModal') },
        {
            selector: '#settings-modal',
            close: () => {
                // Let the shortcut recorder consume Escape.
                if (typeof recordingActionId !== 'undefined' && recordingActionId !== null) return false;
                if (typeof closeSettings === 'function') closeSettings();
            },
        },
        { selector: '#plugins-modal', close: call('closePlugins') },
        { selector: '#maintenance-modal', close: call('closeMaintenance') },
        { selector: '#api-modal', close: call('closeApiModal') },
        { selector: '#queue-modal', close: call('closeQueueModal') },
        { selector: '#folder-modal', close: call('closeFolderModal') },
        { selector: '#merge-tag-modal', close: call('closeMergeTagModal') },
        { selector: '[data-testid="folder-move-modal"]', close: () => window.FolderMoveModal?.close() },
        { selector: '#create-share-modal', close: () => window.ShareView?.closeCreate() },
        { selector: '#follow-share-modal', close: () => window.ShareView?.closeFollow() },
        { selector: '#edit-follow-modal', close: () => window.ShareView?.closeEditFollow() },
        { selector: '#share-logs-modal', close: () => window.ShareView?.closeLogs() },
        { selector: '#shortcuts-cheatsheet', close: () => closeCheatSheet() },
        { selector: '#import-wizard-modal', close: () => window.ImportWizard?.requestClose() },
        { selector: '#tag-popover', isOpen: el => !el.classList.contains('hidden'), close: call('closeTagPopover', { restoreFocus: true }) },
        { selector: '.expiration-popover', isOpen: () => true, close: call('closeExpirationPopover', { restoreFocus: true }) },
        { selector: '.search-options-popover', isOpen: () => true, close: () => {
            if (typeof closeSearchOptionsPopover === 'function') closeSearchOptionsPopover();
            document.getElementById('search-options-btn')?.focus();
        } },
        { selector: '.sort-popover', isOpen: () => true, close: call('closeSortPopover') },
        { selector: '#serve-tag-picker', isOpen: () => true, close: call('closeServeTagPicker') },
        { selector: '#tag-filter-dropdown', isOpen: el => !el.classList.contains('hidden'), close: call('closeTagFilterDropdown') },
        { selector: '#nav-drawer', isOpen: el => !el.classList.contains('translate-x-full'), close: call('closeDrawer') },
        { selector: '#lightbox', isOpen: el => el.classList.contains('active') },
        { selector: '#editor-modal', isOpen: el => el.classList.contains('active') },
        { selector: '#comparison-modal', isOpen: el => el.classList.contains('active') },
    ];

    function zIndexOf(el) {
        const z = parseInt(getComputedStyle(el).zIndex, 10);
        return Number.isNaN(z) ? 0 : z;
    }

    function getTopEscapeLayer() {
        const open = [];
        const known = new Set();
        for (const layer of ESCAPE_LAYERS) {
            const el = document.querySelector(layer.selector);
            if (!el) continue;
            known.add(el);
            const shown = layer.isOpen ? layer.isOpen(el) : isModalShown(el);
            if (shown) open.push({ el, close: layer.close });
        }
        // A dialog nobody registered still sits on top of what is beneath it;
        // with no closer, its own Escape handler gets the key.
        for (const el of document.querySelectorAll('[aria-modal="true"]')) {
            if (!known.has(el) && isAppDialog(el) && isModalShown(el)) open.push({ el, close: null });
        }
        if (open.length === 0) return null;
        open.sort((a, b) => {
            const dz = zIndexOf(b.el) - zIndexOf(a.el);
            if (dz !== 0) return dz;
            return a.el.compareDocumentPosition(b.el) & Node.DOCUMENT_POSITION_FOLLOWING ? 1 : -1;
        });
        return open[0];
    }

    function closeTopModalOverlay() {
        const top = getTopEscapeLayer();
        if (!top || !top.close) return false;
        return top.close() !== false;
    }

    // --- Override migration ---

    // Renamed actions must not silently lose a user's custom binding. Copies the
    // stored override from a retired action ID to its replacement (only when the
    // replacement has none of its own, so a deliberate rebinding always wins),
    // then drops the retired entry so the list does not accumulate dead IDs.
    // Returns true when anything changed.
    const RENAMED_ACTIONS = [
        // Markdown-only preview toggle generalized to every previewable text clip.
        { from: 'editor.markdown_preview', to: 'editor.preview_toggle' },
    ];

    function migrateRenamedOverrides() {
        let changed = false;
        for (const { from, to } of RENAMED_ACTIONS) {
            if (!(from in userOverrides)) continue;
            if (!(to in userOverrides)) {
                userOverrides[to] = userOverrides[from];
            }
            delete userOverrides[from];
            changed = true;
        }
        return changed;
    }

    // --- Init ---

    async function init() {
        document.addEventListener('keydown', handleKeydown, true);
        await loadUserOverrides();
        if (migrateRenamedOverrides()) {
            rebuildBindings();
            await saveUserOverrides();
        }
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
        migrateRenamedOverrides,
        clearFocus,
        getFocusedClip,
        getFocusedClipId,
        getGridColumnCount,
        openCheatSheet,
        closeCheatSheet,
        isCheatSheetOpen,
        renderCheatSheet,
        get actions() { return actions; },
        get userOverrides() { return userOverrides; },
        CATEGORY_ORDER,
        CATEGORY_LABELS,
    };
})();
