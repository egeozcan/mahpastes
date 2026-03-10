// --- Drawer ---
const drawerToggleBtn = document.getElementById('drawer-toggle-btn');
const drawerCloseBtn = document.getElementById('drawer-close-btn');
const drawerOverlay = document.getElementById('drawer-overlay');
const navDrawer = document.getElementById('nav-drawer');

let drawerFocusTrapCleanup = null;
let lastFocusedBeforeDrawer = null;

// Roving tabindex for drawer nav buttons
let drawerNavRover = null;

function openDrawer() {
    lastFocusedBeforeDrawer = document.activeElement;
    navDrawer.removeAttribute('inert');
    navDrawer.classList.remove('translate-x-full');
    drawerOverlay.classList.remove('opacity-0', 'pointer-events-none');
    drawerOverlay.classList.add('opacity-100');
    drawerToggleBtn.setAttribute('aria-expanded', 'true');

    // Focus trap and focus the active view tab
    if (drawerFocusTrapCleanup) drawerFocusTrapCleanup();
    drawerFocusTrapCleanup = trapFocus(navDrawer);
    const activeTab = navDrawer.querySelector('[role="tab"][aria-selected="true"]');
    if (activeTab) activeTab.focus();

    // Initialize nav buttons rover if not yet created
    initDrawerNavRover();
}

function closeDrawer(restoreFocus = true) {
    navDrawer.setAttribute('inert', '');
    navDrawer.classList.add('translate-x-full');
    drawerOverlay.classList.add('opacity-0', 'pointer-events-none');
    drawerOverlay.classList.remove('opacity-100');
    drawerToggleBtn.setAttribute('aria-expanded', 'false');

    if (drawerFocusTrapCleanup) {
        drawerFocusTrapCleanup();
        drawerFocusTrapCleanup = null;
    }
    if (restoreFocus && lastFocusedBeforeDrawer) {
        lastFocusedBeforeDrawer.focus();
    }
    lastFocusedBeforeDrawer = null;
}

function initDrawerNavRover() {
    if (drawerNavRover) drawerNavRover.destroy();
    const navContainer = navDrawer.querySelector('nav');
    if (navContainer) {
        drawerNavRover = RovingTabindex.create({
            container: navContainer,
            itemSelector: 'button:not([style*="display: none"])',
            orientation: 'vertical',
            wrap: false,
            onActivate: (btn) => btn.click(),
        });
    }
}

drawerToggleBtn.addEventListener('click', openDrawer);
drawerCloseBtn.addEventListener('click', closeDrawer);
drawerOverlay.addEventListener('click', closeDrawer);

// Close drawer when any nav button inside it is clicked (skip focus restore — the action handles focus)
navDrawer.addEventListener('click', (e) => {
    const globalActionBtn = e.target.closest('[data-global-action]');
    if (globalActionBtn) {
        closeDrawer(false);
        handleGlobalAction(globalActionBtn);
        return;
    }
    if (e.target.closest('button[id]') && e.target.closest('button[id]') !== drawerCloseBtn) {
        closeDrawer(false);
    }
});

// Handle a global plugin action click from the drawer
function handleGlobalAction(btn) {
    const pluginId = parseInt(btn.dataset.pluginId, 10);
    const actionId = btn.dataset.actionId;
    const hasOptions = btn.dataset.hasOptions === 'true';
    const isAsync = btn.dataset.isAsync === 'true';

    if (hasOptions) {
        // Find the full action object from cache
        const action = pluginUIActions?.global_actions?.find(
            a => a.plugin_id === pluginId && a.id === actionId
        );
        if (action) {
            openPluginOptionsDialog(action, []);
        }
    } else {
        executePluginAction(pluginId, actionId, [], {}, isAsync);
    }
}

// --- Elements ---
const fileInput = document.getElementById('file-input');
const dropOverlay = document.getElementById('drop-overlay');
const gallery = document.getElementById('gallery');
const deleteAllTempBtn = document.getElementById('delete-all-temp-btn');
const deduplicateBtn = document.getElementById('deduplicate-btn');
const toggleArchiveViewBtn = document.getElementById('toggle-archive-view-btn');
const archiveBtnText = document.getElementById('archive-btn-text');
const headerArchiveBtn = document.getElementById('header-archive-btn');
const headerAddBtn = document.getElementById('add-btn');
const bulkToolbar = document.getElementById('bulk-toolbar');
const selectAllCheckbox = document.getElementById('select-all-checkbox');
const selectedCountEl = document.getElementById('selected-count');
const bulkCopyBtn = document.getElementById('bulk-copy-btn');
const bulkDownloadBtn = document.getElementById('bulk-download-btn');
const bulkArchiveBtn = document.getElementById('bulk-archive-btn');
const bulkArchiveText = document.getElementById('bulk-archive-text');
const bulkDeleteBtn = document.getElementById('bulk-delete-btn');
const bulkExpiryBtn = document.getElementById('bulk-expiry-btn');
const bulkCancelExpiryBtn = document.getElementById('bulk-cancel-expiry-btn');
const cancelSelectionBtn = document.getElementById('cancel-selection-btn');

// Lightbox Elements
const lightbox = document.getElementById('lightbox');
// Note: lightboxImg is managed by modals.js via getLightboxImg()
const lightboxCaption = document.getElementById('lightbox-caption');
const lightboxClose = document.getElementById('lightbox-close');
const lightboxPrev = document.getElementById('lightbox-prev');
const lightboxNext = document.getElementById('lightbox-next');

// Comparison Modal Elements
const bulkCompareBtn = document.getElementById('bulk-compare-btn');
const comparisonModal = document.getElementById('comparison-modal');
const comparisonClose = document.getElementById('comparison-close');
const comparisonViewport = document.getElementById('comparison-viewport');
const comparisonContainer = document.getElementById('comparison-container');
const comparisonImgBottom = document.getElementById('comparison-img-bottom');
const comparisonImgTop = document.getElementById('comparison-img-top');
const comparisonImgTopWrapper = document.getElementById('comparison-img-top-wrapper');
const comparisonSliderLine = document.getElementById('comparison-slider-line');
const comparisonRange = document.getElementById('comparison-range');
const comparisonRangeLabel = document.getElementById('comparison-range-label');
const modeFadeBtn = document.getElementById('mode-fade');
const modeSliderBtn = document.getElementById('mode-slider');
const alignHSelect = document.getElementById('align-h');
const alignVSelect = document.getElementById('align-v');
const toggleStretchBtn = document.getElementById('toggle-stretch');
const zoomInBtn = document.getElementById('zoom-in');
const zoomOutBtn = document.getElementById('zoom-out');
const zoomLevelEl = document.getElementById('zoom-level');
const zoomFitBtn = document.getElementById('zoom-fit');
const modeDiffBtn = document.getElementById('mode-diff');
const comparisonImgDiff = document.getElementById('comparison-img-diff');
const comparisonSwapBtn = document.getElementById('comparison-swap');
const comparisonSimilarity = document.getElementById('comparison-similarity');
const comparisonImageInfo = document.getElementById('comparison-image-info');
const comparisonLabelA = document.getElementById('comparison-label-a');
const comparisonLabelB = document.getElementById('comparison-label-b');

const uploadExpirySelect = document.getElementById('expiry-select');
const clipCountEl = document.getElementById('clip-count');

function getUploadExpirationMinutes() {
    return parseInt(uploadExpirySelect.value, 10) || 0;
}

function updateClipCount(count) {
    if (clipCountEl) {
        clipCountEl.textContent = count === 1 ? '1 clip' : `${count} clips`;
    }
}

// --- State ---
let isViewingArchive = false;
let selectedIds = new Set();
let imageClips = []; // Store image clips for lightbox navigation
let currentLightboxIndex = -1;
let lastFocusedElementBeforeLightbox = null;

let comparisonMode = 'fade';
let zoomLevel = 1;
let isStretched = false;
let lastFocusedElementBeforeComparison = null;
let comparisonClipIds = []; // [idA, idB] - track which clips are being compared
let diffCache = new Map(); // Map<threshold, {dataUrl, similarity}> - cache diff results
let lastFocusedElement = null; // For confirm dialog

// Tag state
let allTags = [];
let activeTagFilters = [];
let hiddenTags = [];

// Sort state
let currentSortField = 'date';
let currentSortDir = 'desc';

// Folder mode state
let folderMode = false;
function isFolderMode() { return folderMode; }
function toggleFolderMode() {
    folderMode = !folderMode;
    const btn = document.getElementById('folder-mode-btn');
    if (btn) {
        btn.setAttribute('aria-pressed', folderMode);
        if (folderMode) {
            btn.classList.add('bg-stone-800', 'text-white', 'border-stone-800');
            btn.classList.remove('border-stone-200', 'text-stone-500', 'hover:border-stone-300', 'hover:bg-stone-100');
        } else {
            btn.classList.remove('bg-stone-800', 'text-white', 'border-stone-800');
            btn.classList.add('border-stone-200', 'text-stone-500', 'hover:border-stone-300', 'hover:bg-stone-100');
        }
    }
    // When entering folder mode with filters active, normalize to a single
    // folder path by navigating to the last selected tag.  This prevents
    // broken breadcrumbs when multiple unrelated trees are checked.
    if (folderMode && activeTagFilters.length > 0 && typeof navigateToFolder === 'function') {
        const lastTagId = activeTagFilters[activeTagFilters.length - 1];
        navigateToFolder(lastTagId);
        return;
    }
    if (typeof updateActiveTagsDisplay === 'function') {
        updateActiveTagsDisplay();
    }
    if (typeof renderTagFilterDropdown === 'function') {
        renderTagFilterDropdown();
    }
    loadClips();
}

// Accessors for hiddenTags — other files should use these instead of the variable directly
function getHiddenTags() { return hiddenTags; }
function setHiddenTagsState(tags) {
    hiddenTags.length = 0;
    hiddenTags.push(...tags);
}

// Sort setter — called from sort.js popover
async function setSort(field, dir) {
    currentSortField = field;
    currentSortDir = dir;
    await window.go.main.App.SetSetting('sort_field', field);
    await window.go.main.App.SetSetting('sort_dir', dir);
    await loadClips();
}

// App ready flag for testing
window.__appReady = false;

// Expose state and functions for testing (extend, don't overwrite - other scripts add helpers too)
Object.assign(window.__testHelpers, {
  setAllTags: (tags) => {
    // Modify in place to preserve references
    allTags.length = 0;
    allTags.push(...tags);
  },
  getAllTags: () => allTags,
  setActiveTagFilters: (filters) => {
    activeTagFilters.length = 0;
    activeTagFilters.push(...filters);
  },
  getActiveTagFilters: () => activeTagFilters,
  setHiddenTags: (tags) => setHiddenTagsState(tags),
  getHiddenTags: () => getHiddenTags(),
  setViewingArchive: (val) => { isViewingArchive = val; },
  setSort: (field, dir) => { currentSortField = field; currentSortDir = dir; },
  isFolderMode: () => isFolderMode(),
  setFolderMode: (val) => { folderMode = val; },
  getShortcutManager: () => typeof ShortcutManager !== 'undefined' ? ShortcutManager : null,
  // Expose loadClips function (defined in wails-api.js, but called here)
  loadClips: () => {
    if (typeof loadClips === 'function') {
      loadClips();
    }
  },
});

// --- Event Listeners ---

// Whole-app drag and drop
let dragCounter = 0;

document.addEventListener('dragenter', e => {
    e.preventDefault();
    if (window.__internalDragActive) return;
    dragCounter++;
    if (dragCounter === 1) {
        dropOverlay.classList.remove('opacity-0');
        dropOverlay.classList.add('opacity-100');
    }
}, false);

document.addEventListener('dragover', e => {
    e.preventDefault();
}, false);

document.addEventListener('dragleave', e => {
    e.preventDefault();
    if (window.__internalDragActive) return;
    dragCounter--;
    if (dragCounter <= 0) {
        dragCounter = 0;
        dropOverlay.classList.add('opacity-0');
        dropOverlay.classList.remove('opacity-100');
    }
}, false);

document.addEventListener('drop', e => {
    e.preventDefault();
    if (window.__internalDragActive) return;
    dragCounter = 0;
    dropOverlay.classList.add('opacity-0');
    dropOverlay.classList.remove('opacity-100');
    if (e.dataTransfer.files.length > 0) {
        handleFiles(e.dataTransfer.files);
    }
}, false);

// File input change
fileInput.addEventListener('change', e => handleFiles(e.target.files));

// Paste
document.addEventListener('paste', e => {
    // Don't capture paste events when user is typing in an input field
    const target = e.target;
    if (target.tagName === 'TEXTAREA' || target.tagName === 'INPUT' || target.isContentEditable) {
        return; // Let native paste work in form fields
    }

    if (e.clipboardData.files.length > 0) {
        handleFiles(e.clipboardData.files);
    } else {
        const text = e.clipboardData.getData('text/plain');
        if (text) {
            handleText(text);
        }
    }
});

// Toggle Archive View
toggleArchiveViewBtn.addEventListener('click', toggleViewMode);
headerArchiveBtn.addEventListener('click', toggleViewMode);
headerAddBtn.addEventListener('click', () => fileInput.click());

// Confirm Dialog Listeners
document.getElementById('confirm-yes-btn').addEventListener('click', async () => {
    if (confirmCallback) await confirmCallback();
    closeConfirmDialog();
});

document.getElementById('confirm-no-btn').addEventListener('click', closeConfirmDialog);

document.getElementById('confirm-dialog').addEventListener('click', (e) => {
    if (e.target.id === 'confirm-dialog') closeConfirmDialog();
});

// Delete All Temp Files
deleteAllTempBtn.addEventListener('click', deleteAllTempFiles);

// Deduplicate
deduplicateBtn.addEventListener('click', async () => {
    try {
        const groups = await window.go.main.App.GetDuplicateGroups();
        if (!groups || groups.length === 0) {
            showToast('No duplicates found');
            return;
        }

        const totalRemoved = groups.reduce((sum, g) => sum + (g.count - 1), 0);
        const listHTML = groups.map(g =>
            `<span class="block text-left">&middot; ${escapeHTML(g.filename || 'Untitled')} — ${g.count} copies (oldest kept, ${g.count - 1} removed)</span>`
        ).join('');

        const message = `<span class="block mb-2">${groups.length} duplicate group${groups.length > 1 ? 's' : ''} found:</span>` +
            `<span class="block text-[10px] text-stone-400 mb-2 max-h-40 overflow-y-auto">${listHTML}</span>` +
            `<span class="block">Tags will be merged. ${totalRemoved} clip${totalRemoved > 1 ? 's' : ''} will be removed.</span>`;

        showConfirmDialog('Deduplicate All', message, async () => {
            try {
                const removed = await window.go.main.App.DeduplicateAll();
                showToast(`Deduplicated: removed ${removed} clip${removed !== 1 ? 's' : ''}`, 'success');
                loadClips();
                checkDuplicatesExist();
            } catch (err) {
                showToast('Failed to deduplicate', 'error');
            }
        });
    } catch (err) {
        showToast('Failed to check duplicates', 'error');
    }
});

async function checkDuplicatesExist() {
    try {
        const groups = await window.go.main.App.GetDuplicateGroups();
        deduplicateBtn.style.display = (groups && groups.length > 0) ? '' : 'none';
    } catch (e) {
        deduplicateBtn.style.display = 'none';
    }
}

// Bulk Action Listeners
selectAllCheckbox.addEventListener('change', toggleSelectAll);
cancelSelectionBtn.addEventListener('click', cancelSelection);
bulkDeleteBtn.addEventListener('click', bulkDelete);
bulkArchiveBtn.addEventListener('click', bulkArchive);
bulkCopyBtn.addEventListener('click', bulkCopyFiles);
bulkDownloadBtn.addEventListener('click', bulkDownload);
bulkCompareBtn.addEventListener('click', openComparisonModal);
bulkExpiryBtn.addEventListener('click', () => {
    openExpirationPopover(null, bulkExpiryBtn, true);
});
bulkCancelExpiryBtn.addEventListener('click', bulkCancelExpiry);

async function bulkCancelExpiry() {
    if (selectedIds.size === 0) return;
    await bulkCancelExpiration(Array.from(selectedIds));
    selectedIds.clear();
}

// Comparison Listeners
comparisonClose.addEventListener('click', closeComparisonModal);
comparisonRange.addEventListener('input', updateComparisonView);

modeFadeBtn.addEventListener('click', () => { comparisonMode = 'fade'; updateComparisonView(); });
modeSliderBtn.addEventListener('click', () => { comparisonMode = 'slider'; updateComparisonView(); });
modeDiffBtn.addEventListener('click', () => { comparisonMode = 'diff'; updateComparisonView(); });
comparisonSwapBtn.addEventListener('click', swapComparisonImages);

alignHSelect.addEventListener('change', updateComparisonView);
alignVSelect.addEventListener('change', updateComparisonView);
toggleStretchBtn.addEventListener('click', () => { isStretched = !isStretched; updateComparisonView(); });

zoomInBtn.addEventListener('click', () => { zoomLevel = Math.min(zoomLevel * 1.2, 5); updateComparisonView(); });
zoomOutBtn.addEventListener('click', () => { zoomLevel = Math.max(zoomLevel / 1.2, 0.1); updateComparisonView(); });
zoomFitBtn.addEventListener('click', zoomFit);

comparisonModal.addEventListener('click', (e) => {
    if (e.target === comparisonModal || e.target.classList.contains('comparison-viewport')) {
        closeComparisonModal();
    }
});

comparisonSliderLine.addEventListener('mousedown', startDragging);
comparisonSliderLine.addEventListener('touchstart', startDragging, { passive: false });

comparisonContainer.addEventListener('mousedown', (e) => {
    if (comparisonMode === 'slider' && e.target !== comparisonSliderLine) {
        startDragging(e);
    }
});

// Lightbox Listeners
lightboxClose.addEventListener('click', closeLightbox);
lightboxPrev.addEventListener('click', (e) => { e.stopPropagation(); showPrevImage(); });
lightboxNext.addEventListener('click', (e) => { e.stopPropagation(); showNextImage(); });
lightbox.addEventListener('click', (e) => {
    if (e.target === lightbox) closeLightbox();
});
// Initialize lightbox gestures (touch, wheel, drag, zoom slider)
// All gesture listeners are centralized in modals.js for better cohesion
initLightboxGestures();

// Focus Trap for Confirm Dialog
function setupConfirmDialogFocusTrap() {
    const dialog = document.getElementById('confirm-dialog');
    const focusableElements = dialog.querySelectorAll('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])');
    const firstFocusable = focusableElements[0];
    const lastFocusable = focusableElements[focusableElements.length - 1];

    dialog.addEventListener('keydown', e => {
        if (e.key === 'Tab') {
            if (e.shiftKey) { // Shift + Tab
                if (document.activeElement === firstFocusable) {
                    lastFocusable.focus();
                    e.preventDefault();
                }
            } else { // Tab
                if (document.activeElement === lastFocusable) {
                    firstFocusable.focus();
                    e.preventDefault();
                }
            }
        } else if (e.key === 'Escape') {
            e.preventDefault();
            closeConfirmDialog();
        }
    });
}
setupConfirmDialogFocusTrap();

// --- Upload Handlers ---

async function handleFiles(files) {
    if (isViewingArchive) {
        showToast('Switch to Active view to upload.');
        return;
    }
    if (files.length === 0) return;

    const fileDataArray = [];
    for (let i = 0; i < files.length; i++) {
        const fileData = await fileToFileData(files[i]);
        fileDataArray.push(fileData);
    }

    upload(fileDataArray);
}

async function handleText(text) {
    if (isViewingArchive) {
        showToast('Switch to Active view to upload.');
        return;
    }

    // Convert text to base64
    const base64 = btoa(unescape(encodeURIComponent(text)));
    const fileData = {
        name: 'pasted_text.txt',
        content_type: 'text/plain',
        data: base64
    };

    upload([fileData]);
}

async function loadHiddenTags() {
    try {
        const tags = await window.go.main.App.GetHiddenTags();
        setHiddenTagsState(tags);
    } catch (error) {
        console.error('Error loading hidden tags:', error);
        setHiddenTagsState([]);
    }
}

// --- Initial Load ---
window.addEventListener('load', async () => {
    window.__appReady = false;
    try {
        await loadPluginUIActions();
        await loadTags();
        await loadHiddenTags();
        if (typeof initTransferCapabilities === 'function') {
            await initTransferCapabilities();
        }

        // Load sort preferences
        try {
            const savedField = await window.go.main.App.GetSetting('sort_field');
            const savedDir = await window.go.main.App.GetSetting('sort_dir');
            if (['date', 'name', 'size', 'type'].includes(savedField)) currentSortField = savedField;
            if (['asc', 'desc'].includes(savedDir)) currentSortDir = savedDir;
        } catch (e) { /* use defaults */ }

        // Folder mode toggle
        const folderModeBtn = document.getElementById('folder-mode-btn');
        if (folderModeBtn) {
            folderModeBtn.addEventListener('click', toggleFolderMode);
        }

        await loadClips();
        checkDuplicatesExist();
        setupEditorListeners();

        // --- Register Keyboard Shortcuts ---

        // System
        ShortcutManager.register({
            id: 'show-cheatsheet', label: 'Show Shortcuts', category: 'system',
            defaultKey: '?', context: 'global',
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
            callback: () => { if (typeof switchView === 'function') switchView(currentView === 'watch' ? 'clips' : 'watch'); }
        });
        ShortcutManager.register({
            id: 'open-serve', label: 'Open Serve View', category: 'navigation',
            defaultKey: 's', context: 'gallery',
            callback: () => { if (typeof switchView === 'function') switchView(currentView === 'serve' ? 'clips' : 'serve'); }
        });
        ShortcutManager.register({
            id: 'open-settings', label: 'Open Settings', category: 'navigation',
            defaultKey: ',', context: 'global',
            callback: () => { if (typeof openSettings === 'function') openSettings(); }
        });
        ShortcutManager.register({
            id: 'open-plugins', label: 'Open Plugins', category: 'navigation',
            defaultKey: 'p', context: 'gallery',
            callback: () => { if (typeof openPlugins === 'function') openPlugins(); }
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
            callback: () => {
                selectAllCheckbox.checked = !selectAllCheckbox.checked;
                toggleSelectAll();
            }
        });
        ShortcutManager.register({
            id: 'clear-temp', label: 'Clear Temp Files', category: 'gallery',
            defaultKey: 'mod+shift+Delete', context: 'gallery',
            callback: () => deleteAllTempFiles()
        });

        // Grid navigation — registered so actions appear in settings/cheat sheet
        // and honor user key overrides. Uses 'clip' context so arrow keys only fire
        // when a gallery clip is focused, leaving other rovers (view tabs, tag filter,
        // bulk toolbar) free to handle their own arrow keys in bubble phase.
        ShortcutManager.register({
            id: 'grid-up', label: 'Navigate Up', category: 'gallery',
            defaultKey: 'ArrowUp', context: 'clip',
            callback: () => window.__galleryRover?.navigate('up')
        });
        ShortcutManager.register({
            id: 'grid-down', label: 'Navigate Down', category: 'gallery',
            defaultKey: 'ArrowDown', context: 'clip',
            callback: () => window.__galleryRover?.navigate('down')
        });
        ShortcutManager.register({
            id: 'grid-left', label: 'Navigate Left', category: 'gallery',
            defaultKey: 'ArrowLeft', context: 'clip',
            callback: () => window.__galleryRover?.navigate('left')
        });
        ShortcutManager.register({
            id: 'grid-right', label: 'Navigate Right', category: 'gallery',
            defaultKey: 'ArrowRight', context: 'clip',
            callback: () => window.__galleryRover?.navigate('right')
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
        ShortcutManager.register({
            id: 'clip-context-menu', label: 'Open Context Menu', category: 'clip',
            defaultKey: 'mod+Enter', context: 'clip',
            callback: () => {
                const focused = ShortcutManager.getFocusedClip();
                if (!focused) return;
                const clipId = focused.dataset.id;
                const menuBtn = focused.querySelector('[data-action="menu"]');
                if (menuBtn && clipId) {
                    menuBtn.click();
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
            id: 'bulk-copy-mod', label: 'Copy Selected (Mod)', category: 'bulk',
            defaultKey: 'mod+c', context: 'bulk',
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
        ShortcutManager.register({
            id: 'bulk-expire', label: 'Set Expiration', category: 'bulk',
            defaultKey: 'x', context: 'bulk',
            callback: () => {
                const btn = document.getElementById('bulk-expiry-btn');
                if (btn) btn.click();
            }
        });

        // Lightbox
        ShortcutManager.register({
            id: 'lightbox-close', label: 'Close Lightbox', category: 'lightbox',
            defaultKey: 'Escape', context: 'lightbox',
            callback: () => {
                // Close any open menu first
                if (ContextMenu.isOpen()) {
                    ContextMenu.close();
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
                    setTimeout(() => openEditor(clip.id), LIGHTBOX_CLOSE_DELAY);
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

        // Comparison
        ShortcutManager.register({
            id: 'compare-mode-fade', label: 'Fade Mode', category: 'comparison',
            defaultKey: '1', context: 'comparison',
            callback: () => { comparisonMode = 'fade'; comparisonRange.value = 50; updateComparisonView(); }
        });
        ShortcutManager.register({
            id: 'compare-mode-slider', label: 'Slider Mode', category: 'comparison',
            defaultKey: '2', context: 'comparison',
            callback: () => { comparisonMode = 'slider'; comparisonRange.value = 50; updateComparisonView(); }
        });
        ShortcutManager.register({
            id: 'compare-mode-diff', label: 'Diff Mode', category: 'comparison',
            defaultKey: '3', context: 'comparison',
            callback: () => { comparisonMode = 'diff'; comparisonRange.value = 30; updateComparisonView(); }
        });
        ShortcutManager.register({
            id: 'compare-swap', label: 'Swap Images', category: 'comparison',
            defaultKey: 's', context: 'comparison',
            callback: () => swapComparisonImages()
        });
        ShortcutManager.register({
            id: 'compare-zoom-in', label: 'Zoom In', category: 'comparison',
            defaultKey: '+', context: 'comparison',
            callback: () => { zoomLevel = Math.min(zoomLevel * 1.2, 5); updateComparisonView(); }
        });
        ShortcutManager.register({
            id: 'compare-zoom-out', label: 'Zoom Out', category: 'comparison',
            defaultKey: '-', context: 'comparison',
            callback: () => { zoomLevel = Math.max(zoomLevel / 1.2, 0.1); updateComparisonView(); }
        });
        ShortcutManager.register({
            id: 'compare-zoom-fit', label: 'Fit to Viewport', category: 'comparison',
            defaultKey: '0', context: 'comparison',
            callback: () => zoomFit()
        });
        ShortcutManager.register({
            id: 'compare-range-left', label: 'Adjust Range Left', category: 'comparison',
            defaultKey: 'ArrowLeft', context: 'comparison',
            callback: () => {
                comparisonRange.value = Math.max(0, parseInt(comparisonRange.value) - 5);
                updateComparisonView();
            }
        });
        ShortcutManager.register({
            id: 'compare-range-right', label: 'Adjust Range Right', category: 'comparison',
            defaultKey: 'ArrowRight', context: 'comparison',
            callback: () => {
                comparisonRange.value = Math.min(100, parseInt(comparisonRange.value) + 5);
                updateComparisonView();
            }
        });
        ShortcutManager.register({
            id: 'compare-close', label: 'Close Comparison', category: 'comparison',
            defaultKey: 'Escape', context: 'comparison',
            callback: () => closeComparisonModal()
        });

        // Initialize the shortcut manager (loads user overrides and starts listening)
        await ShortcutManager.init();

        // View tabs roving tabindex — arrow keys navigate between Clips/Watch/Serve tabs
        const viewTabsContainer = document.querySelector('[role="tablist"]');
        if (viewTabsContainer) {
            RovingTabindex.create({
                container: viewTabsContainer,
                itemSelector: '[role="tab"]',
                orientation: 'horizontal',
                wrap: false,
                onActivate: (tab) => tab.click(),
            });
        }

        // Gallery roving tabindex — single Tab stop, arrow keys navigate within
        const galleryRover = RovingTabindex.create({
            container: gallery,
            itemSelector: ':scope > li:not([data-folder])',
            orientation: 'grid',
            columns: () => {
                const style = getComputedStyle(gallery);
                const columns = style.getPropertyValue('grid-template-columns');
                if (!columns || columns === 'none') return 1;
                return columns.split(' ').filter(c => c.trim()).length;
            },
            wrap: false,
        });
        window.__galleryRover = galleryRover;
        Object.assign(window.__testHelpers, {
            galleryRover: galleryRover,
        });

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

    } catch (error) {
        console.error('Error during app initialization:', error);
    }
    window.__appReady = true;

    // Listen for plugin toast events
    if (window.runtime && window.runtime.EventsOn) {
        window.runtime.EventsOn("plugin:toast", (data) => {
            if (data && data.message) {
                showToast(data.message, data.type || 'info');
            }
        });

        window.runtime.EventsOn("clip:duplicate", (data) => {
            showToast(`Duplicate clip detected — ${data.count} other ${data.count === 1 ? 'copy' : 'copies'} exist`, 'info');
        });
    }

    // Auto-refresh clips when window regains focus (clears stale expired clips)
    document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible' && window.__appReady) {
            loadClips();
        }
    });
});

// Close card menu when clicking outside
document.addEventListener('click', (e) => {
    const menu = document.querySelector('.card-menu-dropdown');
    if (!menu) return;

    // Check if click is on menu or menu trigger
    const isMenuClick = e.target.closest('.card-menu-dropdown');
    const isTriggerClick = e.target.closest('[data-action="menu"]');

    if (!isMenuClick && !isTriggerClick) {
        closeCardMenu();
    }
});

// Close lightbox plugin menu when clicking outside
document.addEventListener('click', (e) => {
    const menu = document.getElementById('lightbox-plugin-menu');
    if (!menu) return;

    const isMenuClick = e.target.closest('#lightbox-plugin-menu');
    const isTriggerClick = e.target.closest('#lightbox-plugin-menu-trigger');

    if (!isMenuClick && !isTriggerClick) {
        closeLightboxPluginMenu();
    }
});

// Close expiration popover when clicking outside
document.addEventListener('click', (e) => {
    const popover = document.querySelector('.expiration-popover');
    if (!popover) return;
    if (!e.target.closest('.expiration-popover') && !e.target.closest('[data-action="set-expiration"]') && !e.target.closest('#bulk-expiry-btn')) {
        closeExpirationPopover();
    }
});

// Handle menu item clicks via event delegation
document.addEventListener('click', (e) => {
    const menuItem = e.target.closest('.card-menu-item');
    if (!menuItem) return;

    e.stopPropagation();
    const action = menuItem.dataset.action;
    const clipId = menuItem.dataset.clipId;

    if (action === 'plugin') {
        // Handle plugin action
        const pluginId = Number(menuItem.dataset.pluginId);
        const actionId = menuItem.dataset.actionId;
        const hasOptions = menuItem.dataset.hasOptions === 'true';

        closeCardMenu();

        // Verify plugin UI actions are loaded
        if (!pluginUIActions || !pluginUIActions.card_actions) {
            console.error('Plugin UI actions not loaded');
            if (typeof showToast === 'function') {
                showToast('Plugin actions not available. Try refreshing the page.', 'error');
            }
            return;
        }

        if (hasOptions && typeof openPluginOptionsDialog === 'function') {
            // Find the full action object from pluginUIActions
            const pluginAction = pluginUIActions.card_actions.find(
                a => a.plugin_id === pluginId && a.id === actionId
            );
            if (pluginAction) {
                openPluginOptionsDialog(pluginAction, [Number(clipId)]);
            } else {
                console.error('Could not find plugin action:', pluginId, actionId);
                if (typeof showToast === 'function') {
                    showToast('Plugin action not found', 'error');
                }
            }
        } else if (typeof executePluginAction === 'function') {
            // Execute directly - look up action to check async flag
            const pluginAction = pluginUIActions.card_actions.find(
                a => a.plugin_id === pluginId && a.id === actionId
            );
            executePluginAction(pluginId, actionId, [Number(clipId)], {}, pluginAction && pluginAction.async);
        } else {
            console.error('Plugin action handler not available');
            if (typeof showToast === 'function') {
                showToast('Plugin system not initialized', 'error');
            }
        }
    } else {
        // Handle built-in action
        const triggerBtn = document.querySelector(`[data-action="menu"][data-id="${clipId}"]`);
        handleCardAction(action, clipId, triggerBtn);
    }
});

// Also handle DOMContentLoaded for faster initialization
document.addEventListener('DOMContentLoaded', () => {
    // Set initial state so tests know the event listener is attached
    if (window.__appReady === undefined) {
        window.__appReady = false;
    }
});

// Load all tags and update UI
async function loadTags() {
    allTags = await getAllTags();
    renderTagFilterDropdown();
}
