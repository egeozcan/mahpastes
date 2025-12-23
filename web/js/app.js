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
const lightboxImg = document.getElementById('lightbox-img');
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

function handleFiles(files) {
    if (isViewingArchive) {
        showToast('Switch to Active view to upload.');
        return;
    }
    if (files.length === 0) return;

    const formData = new FormData();
    for (let i = 0; i < files.length; i++) {
        formData.append('data', files[i], files[i].name);
    }
    formData.append('expiration', expirationSelect.value);
    upload(formData);
}

function handleText(text) {
    if (isViewingArchive) {
        showToast('Switch to Active view to upload.');
        return;
    }
    const formData = new FormData();
    formData.append('data', new Blob([text], { type: 'text/plain' }), 'pasted_text.txt');
    formData.append('expiration', expirationSelect.value);
    upload(formData);
}

// --- Initial Load ---
window.addEventListener('load', () => {
    loadClips();
    setupEditorListeners();
});
