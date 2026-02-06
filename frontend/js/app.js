// --- Elements ---
const dropZone = document.getElementById('drop-zone');
const fileInput = document.getElementById('file-input');
const fileSelectBtn = document.getElementById('file-select-btn');
const gallery = document.getElementById('gallery');
const deleteAllTempBtn = document.getElementById('delete-all-temp-btn');
const toggleArchiveViewBtn = document.getElementById('toggle-archive-view-btn');
const archiveBtnText = document.getElementById('archive-btn-text');
const uploadSection = document.getElementById('upload-section');
const expirationSelect = document.getElementById('expiration-select');
const bulkToolbar = document.getElementById('bulk-toolbar');
const selectAllCheckbox = document.getElementById('select-all-checkbox');
const selectedCountEl = document.getElementById('selected-count');
const bulkDownloadBtn = document.getElementById('bulk-download-btn');
const bulkArchiveBtn = document.getElementById('bulk-archive-btn');
const bulkArchiveText = document.getElementById('bulk-archive-text');
const bulkDeleteBtn = document.getElementById('bulk-delete-btn');
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
let lastFocusedElement = null; // For confirm dialog

// Tag state
let allTags = [];
let activeTagFilters = [];

// App ready flag for testing
window.__appReady = false;

// Expose state and functions for testing
window.__testHelpers = {
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
  // Expose loadClips function (defined in wails-api.js, but called here)
  loadClips: () => {
    if (typeof loadClips === 'function') {
      loadClips();
    }
  },
};

// --- Event Listeners ---

// Drag and Drop
['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
    dropZone.addEventListener(eventName, e => {
        e.preventDefault();
        e.stopPropagation();
    }, false);
});

['dragenter', 'dragover'].forEach(eventName => {
    dropZone.addEventListener(eventName, () => {
        dropZone.classList.add('border-blue-500', 'bg-blue-50');
    }, false);
});

['dragleave', 'drop'].forEach(eventName => {
    dropZone.addEventListener(eventName, () => {
        dropZone.classList.remove('border-blue-500', 'bg-blue-50');
    }, false);
});

dropZone.addEventListener('drop', e => {
    const files = e.dataTransfer.files;
    handleFiles(files);
});

// File Select Button
fileSelectBtn.addEventListener('click', () => fileInput.click());
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

// Confirm Dialog Listeners
document.getElementById('confirm-yes-btn').addEventListener('click', () => {
    if (confirmCallback) confirmCallback();
    closeConfirmDialog();
});

document.getElementById('confirm-no-btn').addEventListener('click', closeConfirmDialog);

document.getElementById('confirm-dialog').addEventListener('click', (e) => {
    if (e.target.id === 'confirm-dialog') closeConfirmDialog();
});

// Delete All Temp Files
deleteAllTempBtn.addEventListener('click', deleteAllTempFiles);

// Bulk Action Listeners
selectAllCheckbox.addEventListener('change', toggleSelectAll);
cancelSelectionBtn.addEventListener('click', cancelSelection);
bulkDeleteBtn.addEventListener('click', bulkDelete);
bulkArchiveBtn.addEventListener('click', bulkArchive);
bulkDownloadBtn.addEventListener('click', bulkDownload);
bulkCompareBtn.addEventListener('click', openComparisonModal);

// Comparison Listeners
comparisonClose.addEventListener('click', closeComparisonModal);
comparisonRange.addEventListener('input', updateComparisonView);

modeFadeBtn.addEventListener('click', () => { comparisonMode = 'fade'; updateComparisonView(); });
modeSliderBtn.addEventListener('click', () => { comparisonMode = 'slider'; updateComparisonView(); });

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
document.addEventListener('keydown', handleLightboxKeydown);

// Initialize lightbox gestures (touch, wheel, drag, zoom slider)
// All gesture listeners are centralized in modals.js for better cohesion
initLightboxGestures();

// Keyboard Handlers for Drop Zone
dropZone.addEventListener('keydown', e => {
    if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        fileInput.click();
    }
});

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

    const expiration = parseInt(expirationSelect.value) || 0;
    upload(fileDataArray, expiration);
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

    const expiration = parseInt(expirationSelect.value) || 0;
    upload([fileData], expiration);
}

// --- Initial Load ---
window.addEventListener('load', async () => {
    window.__appReady = false;
    try {
        await loadPluginUIActions();
        await loadTags();
        await loadClips();
        setupEditorListeners();
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
    }
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
