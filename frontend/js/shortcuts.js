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
    const CATEGORY_ORDER = ['navigation', 'gallery', 'clip', 'lightbox', 'editor', 'comparison', 'bulk', 'system'];
    const CATEGORY_LABELS = {
        navigation: 'Navigation',
        gallery: 'Gallery',
        clip: 'Clip Actions',
        lightbox: 'Lightbox',
        editor: 'Image Editor',
        comparison: 'Comparison',
        bulk: 'Bulk Actions',
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

    function getActiveContexts() {
        const contexts = ['global'];
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
        if (settingsModal && !settingsModal.classList.contains('opacity-0')) return [];
        if (pluginsModal && !pluginsModal.classList.contains('opacity-0')) return [];

        // Nav drawer open — suppress all shortcuts (drawer has its own key handling)
        const navDrawer = document.getElementById('nav-drawer');
        if (navDrawer && !navDrawer.classList.contains('translate-x-full')) return [];

        // Check for any other open modal (plugin options, plugin review, folder modal, etc.)
        const openModals = ['plugin-options-modal', 'plugin-result-modal', 'plugin-review-modal',
                            'folder-modal', 'restore-confirm-dialog', 'text-editor-modal', 'metadata-modal'];
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

        const serveViewEl = document.getElementById('serve-view');
        if (serveViewEl && !serveViewEl.classList.contains('hidden')) {
            return contexts; // global only — no gallery/bulk/clip shortcuts
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

        // Handle Escape for modal overlays that block all shortcut contexts.
        // These modals cause getActiveContexts() to return [], so they can't be
        // handled through the normal context-based dispatch.
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
        if (activeContexts.length === 0) return;

        // Check contexts in priority order (most specific first)
        // clip > bulk > lightbox > watch > gallery > global
        const priority = ['clip', 'bulk', 'lightbox', 'image-editor', 'text-editor', 'editor', 'comparison', 'watch', 'gallery', 'global'];

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
            global: ['gallery', 'lightbox', 'editor', 'image-editor', 'text-editor', 'comparison', 'watch', 'bulk', 'clip'],
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

    function closeTopModalOverlay() {
        // Upload conflict dialog
        const conflictDialog = document.getElementById('conflict-dialog');
        if (conflictDialog && !conflictDialog.classList.contains('opacity-0')) {
            if (typeof closeConflictDialog === 'function') closeConflictDialog('skip');
            return true;
        }

        // Pasted-path dialog (text clip vs. the file the path names)
        const pathPasteDialog = document.getElementById('path-paste-dialog');
        if (pathPasteDialog && !pathPasteDialog.classList.contains('opacity-0')) {
            if (typeof closePathPasteDialog === 'function') closePathPasteDialog(null);
            return true;
        }

        // Plugin result modal takes highest priority
        const resultModal = document.getElementById('plugin-result-modal');
        if (resultModal && !resultModal.classList.contains('opacity-0')) {
            if (typeof closePluginResultModal === 'function') closePluginResultModal();
            return true;
        }

        // Plugin options dialog
        const optionsModal = document.getElementById('plugin-options-modal');
        if (optionsModal && !optionsModal.classList.contains('opacity-0')) {
            if (typeof closePluginOptionsDialog === 'function') closePluginOptionsDialog();
            return true;
        }

        // Metadata modal
        const metadataModal = document.getElementById('metadata-modal');
        if (metadataModal && !metadataModal.classList.contains('opacity-0')) {
            if (typeof closeMetadataModal === 'function') closeMetadataModal();
            return true;
        }

        // Settings modal (skip if recording a shortcut — let the recording handler consume Escape)
        const settingsModal = document.getElementById('settings-modal');
        if (settingsModal && !settingsModal.classList.contains('opacity-0')) {
            if (typeof recordingActionId !== 'undefined' && recordingActionId !== null) return false;
            if (typeof closeSettings === 'function') closeSettings();
            return true;
        }

        // Plugins modal
        const pluginsModal = document.querySelector('[data-testid="plugins-modal"]');
        if (pluginsModal && !pluginsModal.classList.contains('opacity-0')) {
            if (typeof closePlugins === 'function') closePlugins();
            return true;
        }

        // Tag filter dropdown
        const tagDropdown = document.getElementById('tag-filter-dropdown');
        if (tagDropdown && !tagDropdown.classList.contains('hidden')) {
            if (typeof closeTagFilterDropdown === 'function') closeTagFilterDropdown();
            return true;
        }

        // Sort popover
        const sortPopover = document.querySelector('.sort-popover');
        if (sortPopover) {
            if (typeof closeSortPopover === 'function') closeSortPopover();
            return true;
        }

        // Nav drawer
        const navDrawer = document.getElementById('nav-drawer');
        if (navDrawer && !navDrawer.classList.contains('translate-x-full')) {
            if (typeof closeDrawer === 'function') closeDrawer();
            return true;
        }

        return false;
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
