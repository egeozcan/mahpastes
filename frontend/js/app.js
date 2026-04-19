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

/**
 * Normalize activeTagFilters after any tag change. Pass the set of currently
 * valid tag IDs and an optional Map of substitutions (source_id -> dest_id
 * for merges). Mutates the array in place so existing references stay live.
 * Exposed on window because the tag event handler lives in tags.js.
 */
function normalizeActiveTagFilters(validIDs, substitutions) {
    const subs = substitutions || new Map();
    const replaced = activeTagFilters.map(id => subs.has(id) ? subs.get(id) : id);
    const kept = replaced.filter(id => validIDs.has(id));
    activeTagFilters.length = 0;
    activeTagFilters.push(...kept);
    // Re-render the filter pills so the UI reflects the normalized list.
    if (typeof renderTagFilterDropdown === 'function') {
        renderTagFilterDropdown();
    }
}
window.normalizeActiveTagFilters = normalizeActiveTagFilters;

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
    // Exiting folder mode — clear the remembered folder.
    if (!folderMode && typeof window.rememberCurrentFolder === 'function') {
        window.rememberCurrentFolder(null);
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
  handleFolderDrop: (folderFiles, looseFiles, dirPaths) => handleFolderDrop(folderFiles, looseFiles, dirPaths),
  confirmFolderDrop: (fileCount, maxDepth) => confirmFolderDrop(fileCount, maxDepth),
  parseGitignore: (content) => parseGitignore(content),
  buildIgnoreFn: (content) => buildIgnoreFn(content),
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

document.addEventListener('drop', async e => {
    e.preventDefault();
    if (window.__internalDragActive) return;
    dragCounter = 0;
    dropOverlay.classList.add('opacity-0');
    dropOverlay.classList.remove('opacity-100');

    const items = e.dataTransfer.items;
    if (!items || items.length === 0) return;

    // Check if any item is a directory
    if (hasDirectoryEntries(items)) {
        const { folderFiles, looseFiles, dirPaths, maxDepth } = await traverseDropItems(items);
        const totalFiles = folderFiles.length + looseFiles.length;
        if (totalFiles === 0 && dirPaths.size === 0) return;

        const proceed = await confirmFolderDrop(totalFiles, maxDepth);
        if (!proceed) return;

        if (isFolderMode()) {
            await handleFolderDrop(folderFiles, looseFiles, dirPaths);
        } else {
            // Non-folder mode: flatten everything
            const allFiles = [...looseFiles, ...folderFiles.map(f => f.file)];
            handleFiles(allFiles);
        }
    } else if (e.dataTransfer.files.length > 0) {
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
    confirmCancelCallback = null; // Prevent cancel callback when confirmed
    closeConfirmDialog();
});

document.getElementById('confirm-no-btn').addEventListener('click', closeConfirmDialog);

document.getElementById('confirm-dialog').addEventListener('click', (e) => {
    if (e.target.id === 'confirm-dialog') closeConfirmDialog();
});

// Prompt Dialog Listeners
document.getElementById('prompt-save-btn').addEventListener('click', async () => {
    const value = document.getElementById('prompt-input').value;
    if (promptCallback) await promptCallback(value);
    closePromptDialog();
});

document.getElementById('prompt-cancel-btn').addEventListener('click', closePromptDialog);

document.getElementById('prompt-dialog').addEventListener('click', (e) => {
    if (e.target.id === 'prompt-dialog') closePromptDialog();
});

document.getElementById('prompt-input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
        e.preventDefault();
        document.getElementById('prompt-save-btn').click();
    }
});

// Conflict Dialog Listeners
document.getElementById('conflict-overwrite-btn').addEventListener('click', () => closeConflictDialog('overwrite'));
document.getElementById('conflict-keep-btn').addEventListener('click', () => closeConflictDialog('keep'));
document.getElementById('conflict-skip-btn').addEventListener('click', () => closeConflictDialog('skip'));

document.getElementById('conflict-dialog').addEventListener('click', (e) => {
    if (e.target.id === 'conflict-dialog') closeConflictDialog('skip');
});

// Delete All Temp Files
deleteAllTempBtn.addEventListener('click', deleteAllTempFiles);

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

// --- Directory Traversal Helpers ---

/**
 * Parse .gitignore content into a filter function.
 * Supports exact names, simple globs (*.ext), directory patterns (name/).
 * Comments (#) and blank lines are skipped. No negation or nested support.
 * @param {string} content - Raw .gitignore file content
 * @returns {(name: string, isDirectory: boolean) => boolean} - Returns true if the name should be ignored
 */
function parseGitignore(content) {
    const rules = [];
    const lines = content.split('\n');

    for (const raw of lines) {
        const line = raw.trim();
        if (!line || line.startsWith('#')) continue;

        // Directory-only pattern (trailing slash)
        if (line.endsWith('/')) {
            const name = line.slice(0, -1);
            rules.push({ name, dirOnly: true, regex: null });
        } else if (line.includes('*') || line.includes('?')) {
            // Glob pattern — convert to regex
            const escaped = line
                .replace(/[.+^${}()|[\]\\]/g, '\\$&')
                .replace(/\*/g, '.*')
                .replace(/\?/g, '.');
            const regex = new RegExp('^' + escaped + '$');
            rules.push({ name: null, dirOnly: false, regex });
        } else {
            // Exact name match (files and dirs)
            rules.push({ name: line, dirOnly: false, regex: null });
        }
    }

    return (name, isDirectory) => {
        for (const rule of rules) {
            if (rule.regex) {
                if (rule.regex.test(name)) return true;
            } else if (rule.dirOnly) {
                if (isDirectory && name === rule.name) return true;
            } else {
                if (name === rule.name) return true;
            }
        }
        return false;
    };
}

/**
 * Combine dotfile filtering with optional gitignore rules.
 * Always skips names starting with '.' (dotfiles/dotdirs).
 * @param {string|null} gitignoreContent - Raw .gitignore content, or null
 * @returns {(name: string, isDirectory: boolean) => boolean} - Returns true if the name should be ignored
 */
function buildIgnoreFn(gitignoreContent) {
    const gitignoreFn = gitignoreContent ? parseGitignore(gitignoreContent) : null;

    return (name, isDirectory) => {
        // Always skip dotfiles/dotdirs
        if (name.startsWith('.')) return true;
        if (gitignoreFn && gitignoreFn(name, isDirectory)) return true;
        return false;
    };
}

/**
 * Read all entries from a DirectoryReader, handling batched reads.
 * The readEntries API may return partial results; must call repeatedly until empty.
 * @param {FileSystemDirectoryReader} reader
 * @returns {Promise<FileSystemEntry[]>}
 */
async function readAllEntries(reader) {
    const entries = [];
    let batch;
    do {
        batch = await new Promise((resolve, reject) => {
            reader.readEntries(resolve, reject);
        });
        entries.push(...batch);
    } while (batch.length > 0);
    return entries;
}

/**
 * Promisified wrapper for FileSystemFileEntry.file().
 * @param {FileSystemFileEntry} fileEntry
 * @returns {Promise<File>}
 */
function entryToFile(fileEntry) {
    return new Promise((resolve, reject) => {
        fileEntry.file(resolve, reject);
    });
}

/**
 * Try to read .gitignore from a directory entry.
 * @param {FileSystemDirectoryEntry} dirEntry
 * @returns {Promise<string|null>} - .gitignore content or null if not found
 */
async function readGitignoreFromDir(dirEntry) {
    try {
        const gitignoreEntry = await new Promise((resolve, reject) => {
            dirEntry.getFile('.gitignore', {}, resolve, reject);
        });
        const file = await entryToFile(gitignoreEntry);
        return await file.text();
    } catch {
        return null;
    }
}

/**
 * Recursively traverse a directory entry, collecting {relativePath, file} objects.
 * @param {FileSystemEntry} entry - File or directory entry
 * @param {string} basePath - Current path prefix (e.g., "mydir/sub")
 * @param {(name: string, isDirectory: boolean) => boolean} ignoreFn - Returns true to skip
 * @param {Set<string>} dirPaths - Accumulates directory relative paths for empty folder tracking
 * @returns {Promise<Array<{relativePath: string, file: File}>>}
 */
async function traverseEntry(entry, basePath, ignoreFn, dirPaths) {
    if (entry.isFile) {
        if (ignoreFn(entry.name, false)) return [];
        const file = await entryToFile(entry);
        const relativePath = basePath ? basePath + '/' + entry.name : entry.name;
        return [{ relativePath, file }];
    }

    if (entry.isDirectory) {
        if (ignoreFn(entry.name, true)) return [];
        const dirPath = basePath ? basePath + '/' + entry.name : entry.name;
        dirPaths.add(dirPath);

        const reader = entry.createReader();
        const entries = await readAllEntries(reader);
        const results = [];

        for (const child of entries) {
            const childResults = await traverseEntry(child, dirPath, ignoreFn, dirPaths);
            results.push(...childResults);
        }

        return results;
    }

    return [];
}

/**
 * Check if any DataTransferItem in the list is a directory.
 * @param {DataTransferItemList} items
 * @returns {boolean}
 */
function hasDirectoryEntries(items) {
    for (let i = 0; i < items.length; i++) {
        const entry = items[i].webkitGetAsEntry?.();
        if (entry && entry.isDirectory) return true;
    }
    return false;
}

/**
 * Traverse all dropped items, separating folder contents from loose files.
 * For each top-level directory, reads .gitignore if present and builds an ignore function.
 * Loose files use dotfile-only filtering.
 * @param {DataTransferItemList} items
 * @returns {Promise<{folderFiles: Array<{relativePath: string, file: File}>, looseFiles: File[], dirPaths: Set<string>, maxDepth: number}>}
 */
async function traverseDropItems(items) {
    const folderFiles = [];
    const looseFiles = [];
    const dirPaths = new Set();

    for (let i = 0; i < items.length; i++) {
        const entry = items[i].webkitGetAsEntry?.();
        if (!entry) continue;

        if (entry.isDirectory) {
            // Skip dotdirs at the top level (e.g. .git)
            if (entry.name.startsWith('.')) continue;

            // Read .gitignore from the top-level dropped directory
            const gitignoreContent = await readGitignoreFromDir(entry);
            const ignoreFn = buildIgnoreFn(gitignoreContent);

            dirPaths.add(entry.name);
            const reader = entry.createReader();
            const children = await readAllEntries(reader);

            for (const child of children) {
                const results = await traverseEntry(child, entry.name, ignoreFn, dirPaths);
                folderFiles.push(...results);
            }
        } else if (entry.isFile) {
            // Loose file — skip dotfiles only
            if (!entry.name.startsWith('.')) {
                const file = await entryToFile(entry);
                looseFiles.push(file);
            }
        }
    }

    // Calculate max depth from directory paths
    let maxDepth = 0;
    for (const p of dirPaths) {
        const depth = p.split('/').length;
        if (depth > maxDepth) maxDepth = depth;
    }

    return { folderFiles, looseFiles, dirPaths, maxDepth };
}

// --- Folder Drop Helpers ---

/**
 * Show a confirmation dialog for folder drops. Returns a promise that resolves
 * to true (proceed) or false (cancel).
 * @param {number} fileCount - Total number of files to upload
 * @param {number} maxDepth - Maximum nesting depth of directories
 * @returns {Promise<boolean>}
 */
function confirmFolderDrop(fileCount, maxDepth) {
    const warnings = [];
    if (fileCount > 50) {
        warnings.push(`This folder contains ${fileCount} files.`);
    }
    if (isFolderMode() && maxDepth > 5) {
        warnings.push(`Folder nesting is ${maxDepth} levels deep.`);
    }
    if (warnings.length === 0) return Promise.resolve(true);

    const message = warnings.join(' ') + ' Continue uploading?';

    return new Promise(resolve => {
        showConfirmDialog(
            'Large Folder Drop',
            message,
            () => resolve(true),
            () => resolve(false),
            { confirmLabel: 'Continue', variant: 'primary' }
        );
    });
}

/**
 * Handle folder drop in folder mode — creates nested tags and uploads files
 * into the appropriate tag folders.
 * @param {Array<{relativePath: string, file: File}>} folderFiles - Files from directories
 * @param {File[]} looseFiles - Top-level loose files
 * @param {Set<string>} dirPaths - All directory relative paths (including empty dirs)
 */
async function handleFolderDrop(folderFiles, looseFiles, dirPaths) {
    if (isViewingArchive) {
        showToast('Switch to Active view to upload.');
        return;
    }

    // Determine base tag prefix from current folder context
    let basePrefix = '';
    if (activeTagFilters.length > 0) {
        const currentTagId = activeTagFilters[activeTagFilters.length - 1];
        const currentTag = allTags.find(t => t.id === currentTagId);
        if (currentTag) {
            basePrefix = currentTag.name + '/';
        }
    }

    // Collect ALL unique directory paths including intermediates
    const allDirs = new Set();
    for (const dp of dirPaths) {
        const parts = dp.split('/');
        for (let i = 1; i <= parts.length; i++) {
            allDirs.add(parts.slice(0, i).join('/'));
        }
    }

    // Sort by depth (parent-first) so parent tags are created before children
    const sortedDirs = Array.from(allDirs).sort((a, b) => {
        const depthA = a.split('/').length;
        const depthB = b.split('/').length;
        return depthA - depthB;
    });

    // Create tags for each directory
    const tagNameToId = {};
    const failedTags = new Set(); // tag names that failed to create
    for (const dir of sortedDirs) {
        const tagName = basePrefix + dir;
        const { tag, error } = await createTagSilent(tagName);
        if (tag) {
            tagNameToId[tagName] = tag.id;
        } else if (error) {
            failedTags.add(tagName);
        }
    }

    // Refresh allTags after creation
    const updatedTags = await getAllTags();
    allTags.length = 0;
    allTags.push(...updatedTags);
    renderTagFilterDropdown();

    // Re-map tag names to IDs from the refreshed list (in case createTagSilent
    // returned an existing tag or the ID was already present)
    for (const dir of sortedDirs) {
        const tagName = basePrefix + dir;
        if (!tagNameToId[tagName] && !failedTags.has(tagName)) {
            const existing = allTags.find(t => t.name === tagName);
            if (existing) tagNameToId[tagName] = existing.id;
        }
    }

    const minutes = typeof getUploadExpirationMinutes === 'function' ? getUploadExpirationMinutes() : 0;

    // Group folderFiles by their immediate parent folder tag
    const groups = {};
    for (const { relativePath, file } of folderFiles) {
        const parts = relativePath.split('/');
        const folder = parts.slice(0, -1).join('/');
        const tagName = basePrefix + folder;
        if (!groups[tagName]) groups[tagName] = [];
        groups[tagName].push(file);
    }

    // Upload each group with its tag, checking for conflicts
    let uploadedCount = 0;
    let skippedCount = 0;
    let overwrittenCount = 0;
    let failedCount = 0;
    for (const [tagName, files] of Object.entries(groups)) {
        if (failedTags.has(tagName)) {
            skippedCount += files.length;
            console.warn(`Skipping ${files.length} file(s) for failed tag "${tagName}"`);
            continue;
        }
        const tagId = tagNameToId[tagName] || 0;
        const fileDataArray = [];
        for (const file of files) {
            const fileData = await fileToFileData(file);
            fileDataArray.push(fileData);
        }

        const result = await checkAndResolveConflicts(fileDataArray, tagId);
        if (!result) {
            skippedCount += files.length;
            continue;
        }

        // Overwrite existing clips
        for (const { clipID, fileData } of result.toOverwrite) {
            try {
                await window.go.main.App.UpdateClipData(clipID, fileData.content_type, fileData.data, fileData.name);
                overwrittenCount++;
            } catch (error) {
                console.error('Error overwriting clip:', error);
                failedCount++;
            }
        }

        skippedCount += result.skippedCount;

        // Upload new files
        if (result.toUpload.length > 0) {
            try {
                await window.go.main.App.UploadFiles(result.toUpload, minutes, tagId);
                uploadedCount += result.toUpload.length;
            } catch (error) {
                console.error('Error uploading folder group:', error);
                failedCount += result.toUpload.length;
            }
        }
    }

    // Handle loose files with current folder's autoTagID
    if (looseFiles.length > 0) {
        let autoTagID = 0;
        if (activeTagFilters.length > 0) {
            autoTagID = activeTagFilters[activeTagFilters.length - 1];
        }
        const fileDataArray = [];
        for (const file of looseFiles) {
            const fileData = await fileToFileData(file);
            fileDataArray.push(fileData);
        }

        const result = await checkAndResolveConflicts(fileDataArray, autoTagID);
        if (result) {
            for (const { clipID, fileData } of result.toOverwrite) {
                try {
                    await window.go.main.App.UpdateClipData(clipID, fileData.content_type, fileData.data, fileData.name);
                    overwrittenCount++;
                } catch (error) {
                    console.error('Error overwriting clip:', error);
                    failedCount++;
                }
            }
            skippedCount += result.skippedCount;
            if (result.toUpload.length > 0) {
                try {
                    await window.go.main.App.UploadFiles(result.toUpload, minutes, autoTagID);
                    uploadedCount += result.toUpload.length;
                } catch (error) {
                    console.error('Error uploading loose files:', error);
                    failedCount += result.toUpload.length;
                }
            }
        } else {
            skippedCount += looseFiles.length;
        }
    }

    // Report results accurately
    if (failedCount > 0 || skippedCount > 0 || overwrittenCount > 0) {
        const parts = [];
        if (uploadedCount > 0) parts.push(`${uploadedCount} uploaded`);
        if (overwrittenCount > 0) parts.push(`${overwrittenCount} overwritten`);
        if (skippedCount > 0) parts.push(`${skippedCount} skipped`);
        if (failedCount > 0) parts.push(`${failedCount} failed`);
        showToast(`Folder import: ${parts.join(', ')}.`);
    } else if (uploadedCount > 0) {
        showToast('Folder uploaded!');
    } else {
        showToast('No files to upload.');
    }
    loadClips();
}

async function handleText(text) {
    if (isViewingArchive) {
        showToast('Switch to Active view to upload.');
        return;
    }

    // Convert text to base64
    const base64 = btoa(unescape(encodeURIComponent(text)));
    const ts = Date.now();
    const fileData = {
        name: `pasted_text_${ts}.txt`,
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

        // Marquee (rubber-band) selection
        initMarqueeSelect({
            gallery: document.getElementById('gallery'),
            selectedIds,
            updateBulkToolbar,
        });

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
            id: 'toggle-folder', label: 'Toggle Folder Mode', category: 'navigation',
            defaultKey: 'f', context: 'gallery',
            callback: () => toggleFolderMode()
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
                if (clip.dataset.folder) {
                    // Folder card — navigate into the folder, focus first item
                    navigateToFolder(parseInt(clip.dataset.folder, 10), { focusFirst: true });
                    return;
                }
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

        // --- Editor shortcuts ---
        ShortcutManager.register({ id: 'editor.brush',     label: 'Brush tool',        category: 'editor', context: 'editor', defaultKey: 'b', callback: () => selectTool('brush') });
        ShortcutManager.register({ id: 'editor.eraser',    label: 'Eraser tool',       category: 'editor', context: 'editor', defaultKey: 'e', callback: () => selectTool('eraser') });
        ShortcutManager.register({ id: 'editor.line',      label: 'Line tool',         category: 'editor', context: 'editor', defaultKey: 'l', callback: () => selectTool('line') });
        ShortcutManager.register({ id: 'editor.rectangle', label: 'Rectangle tool',    category: 'editor', context: 'editor', defaultKey: 'u', callback: () => selectTool('rectangle') });
        ShortcutManager.register({ id: 'editor.circle',    label: 'Circle tool',       category: 'editor', context: 'editor', defaultKey: 'o', callback: () => selectTool('circle') });
        ShortcutManager.register({ id: 'editor.arrow',     label: 'Arrow tool',        category: 'editor', context: 'editor', defaultKey: 'w', callback: () => selectTool('arrow') });
        ShortcutManager.register({ id: 'editor.text',      label: 'Text tool',         category: 'editor', context: 'editor', defaultKey: 't', callback: () => selectTool('text') });
        ShortcutManager.register({ id: 'editor.undo',      label: 'Undo',              category: 'editor', context: 'editor', defaultKey: 'mod+z', callback: () => EditorCore.undo() });
        ShortcutManager.register({ id: 'editor.redo',      label: 'Redo',              category: 'editor', context: 'editor', defaultKey: 'mod+shift+z', callback: () => EditorCore.redo() });
        ShortcutManager.register({ id: 'editor.redo-y',    label: 'Redo (Alt)',        category: 'editor', context: 'editor', defaultKey: 'mod+y', callback: () => EditorCore.redo() });
        ShortcutManager.register({ id: 'editor.eyedropper', label: 'Eyedropper',        category: 'editor', context: 'editor', defaultKey: 'i', callback: () => { selectTool('eyedropper'); updateToolButtons(); } });
        ShortcutManager.register({ id: 'editor.select',     label: 'Select tool',       category: 'editor', context: 'editor', defaultKey: 'v', callback: () => { selectTool('select'); updateToolButtons(); } });
        ShortcutManager.register({ id: 'editor.select_all', label: 'Select all',       category: 'editor', context: 'editor', defaultKey: 'mod+a', callback: () => { if (typeof SelectTool !== 'undefined') SelectTool.selectAll(); } });
        ShortcutManager.register({ id: 'editor.copy',      label: 'Copy selection',    category: 'editor', context: 'editor', defaultKey: 'mod+c', callback: () => { if (typeof SelectTool !== 'undefined') SelectTool.copySelection(); } });
        ShortcutManager.register({ id: 'editor.paste',     label: 'Paste selection',   category: 'editor', context: 'editor', defaultKey: 'mod+v', callback: () => { if (typeof SelectTool !== 'undefined') SelectTool.pasteSelection(); } });
        ShortcutManager.register({ id: 'editor.delete_sel', label: 'Delete selection', category: 'editor', context: 'editor', defaultKey: 'Delete', callback: () => { if (typeof SelectTool !== 'undefined') SelectTool.deleteSelection(); } });
        ShortcutManager.register({ id: 'editor.anonymize',  label: 'Anonymize tool',    category: 'editor', context: 'editor', defaultKey: 'x', callback: () => { selectTool('anonymize'); updateToolButtons(); } });
        ShortcutManager.register({ id: 'editor.crop',       label: 'Crop tool',         category: 'editor', context: 'editor', defaultKey: 'c', callback: () => { selectTool('crop'); updateToolButtons(); } });
        ShortcutManager.register({ id: 'editor.confirm',    label: 'Confirm crop/selection', category: 'editor', context: 'editor', defaultKey: 'Enter', callback: () => {
            if (EditorCore.activeToolName === 'crop' && typeof CropTool !== 'undefined') CropTool.confirm();
            else if (EditorCore.activeToolName === 'select' && typeof SelectTool !== 'undefined') SelectTool.confirmSelection();
        }});
        ShortcutManager.register({ id: 'editor.rotate_cw',  label: 'Rotate 90° CW',   category: 'editor', context: 'editor', defaultKey: 'r', callback: () => TransformTool.rotateCW() });
        ShortcutManager.register({ id: 'editor.rotate_ccw', label: 'Rotate 90° CCW',   category: 'editor', context: 'editor', defaultKey: 'shift+r', callback: () => TransformTool.rotateCCW() });
        ShortcutManager.register({ id: 'editor.save',      label: 'Save as new clip',  category: 'editor', context: 'editor', defaultKey: 'mod+s', callback: () => saveEditorContent() });
        ShortcutManager.register({ id: 'editor.close',     label: 'Close editor',      category: 'editor', context: 'editor', defaultKey: 'Escape', callback: () => {
            // If select tool has an active selection, cancel it instead of closing editor
            if (EditorCore.activeToolName === 'select' && typeof SelectTool !== 'undefined' && SelectTool.hasSelection()) {
                SelectTool.cancelSelectionKey();
                return;
            }
            closeEditor();
        } });
        ShortcutManager.register({ id: 'editor.size_up',    label: 'Increase brush size', category: 'editor', context: 'editor', defaultKey: ']', callback: () => adjustBrushSize(2) });
        ShortcutManager.register({ id: 'editor.size_down',  label: 'Decrease brush size', category: 'editor', context: 'editor', defaultKey: '[', callback: () => adjustBrushSize(-2) });
        ShortcutManager.register({ id: 'editor.opacity_up', label: 'Increase opacity',    category: 'editor', context: 'editor', defaultKey: '}', callback: () => adjustOpacity(0.1) });
        ShortcutManager.register({ id: 'editor.opacity_down', label: 'Decrease opacity',  category: 'editor', context: 'editor', defaultKey: '{', callback: () => adjustOpacity(-0.1) });

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
            itemSelector: ':scope > li',
            orientation: 'grid',
            columns: () => {
                const style = getComputedStyle(gallery);
                const columns = style.getPropertyValue('grid-template-columns');
                if (!columns || columns === 'none') return 1;
                return columns.split(' ').filter(c => c.trim()).length;
            },
            wrap: false,
            onActivate: (item) => {
                if (item.dataset.folder) {
                    // Folder card — navigate into the folder, focus first item
                    navigateToFolder(parseInt(item.dataset.folder, 10), { focusFirst: true });
                } else {
                    // Clip card — open lightbox / editor
                    const openBtn = item.querySelector('[data-action="open-lightbox"]');
                    if (openBtn) openBtn.click();
                }
            },
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

        // Re-resolve folder-view and tag-filter state on any tag change.
        // tag:updated (rename), tag:deleted, tag:merged — all dispatched by Go.
        window.runtime.EventsOn('tag:updated', (payload) => {
            window.handleTagReferenceEvent('tag:updated', payload);
        });
        window.runtime.EventsOn('tag:deleted', (payload) => {
            window.handleTagReferenceEvent('tag:deleted', payload);
        });
        window.runtime.EventsOn('tag:merged', (payload) => {
            window.handleTagReferenceEvent('tag:merged', payload);
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
