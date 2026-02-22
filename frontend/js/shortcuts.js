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

    // Map unshifted keys to shifted equivalents (US layout).
    // Some browsers (headless Chromium) report the raw key on Shift combos.
    const SHIFT_KEY_MAP = {
        '/': '?', '1': '!', '2': '@', '3': '#', '4': '$', '5': '%',
        '6': '^', '7': '&', '8': '*', '9': '(', '0': ')', '-': '_',
        '=': '+', '[': '{', ']': '}', '\\': '|', ';': ':', "'": '"',
        ',': '<', '.': '>', '`': '~'
    };

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
        const gallery = getGallery();
        if (!gallery) return 1;
        const style = getComputedStyle(gallery);
        const columns = style.getPropertyValue('grid-template-columns');
        if (!columns || columns === 'none') return 1;
        return columns.split(' ').filter(c => c.trim()).length;
    }

    function getVisibleClipCount() {
        const gallery = getGallery();
        if (!gallery) return 0;
        return gallery.querySelectorAll(':scope > li').length;
    }

    function setFocusedClipIndex(index) {
        const gallery = getGallery();
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
        const gallery = getGallery();
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
            html += `<h3 class="text-[10px] font-semibold text-stone-400 uppercase tracking-wider mb-2">${escapeHTML(categoryLabels[cat] || cat)}</h3>`;
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
