// Initialize test helpers early so all scripts can register their helpers
window.__testHelpers = {};

// Keep the document stationary while a modal is open without removing its
// scrollbar. CSS overflow locking replaces the real, custom-styled scrollbar
// with a system-painted gutter in WebKit, which changes both its width and its
// appearance. This interaction lock leaves the scrollbar mounted, allows
// scrolling inside modal content, and restores attempted document movement.
(() => {
    const openModalSelector = '[aria-modal="true"]:not([inert])';
    const scrollableOverflow = new Set(['auto', 'scroll', 'overlay']);
    let documentScrollLocked = false;
    let lockedScrollX = 0;
    let lockedScrollY = 0;
    let lastTouchX = null;
    let lastTouchY = null;

    function syncDocumentScrollLock() {
        const shouldLock = document.querySelector(openModalSelector) !== null;
        if (shouldLock === documentScrollLocked) return;

        documentScrollLocked = shouldLock;
        if (shouldLock) {
            lockedScrollX = window.scrollX;
            lockedScrollY = window.scrollY;
        }
    }

    function canConsumeScroll(element, deltaX, deltaY) {
        if (!(element instanceof HTMLElement)) return false;

        const { overflowX, overflowY } = getComputedStyle(element);
        if (deltaY && scrollableOverflow.has(overflowY) && element.scrollHeight > element.clientHeight) {
            if (deltaY < 0 && element.scrollTop > 0) return true;
            if (deltaY > 0 && element.scrollTop + element.clientHeight < element.scrollHeight - 1) {
                return true;
            }
        }

        if (deltaX && scrollableOverflow.has(overflowX) && element.scrollWidth > element.clientWidth) {
            if (deltaX < 0 && element.scrollLeft > 0) return true;
            if (deltaX > 0 && element.scrollLeft + element.clientWidth < element.scrollWidth - 1) {
                return true;
            }
        }
        return false;
    }

    function modalCanConsumeScroll(target, deltaX, deltaY) {
        if (!(target instanceof Element)) return false;
        const modal = target.closest(openModalSelector);
        if (!modal) return false;

        for (let element = target; element; element = element.parentElement) {
            if (canConsumeScroll(element, deltaX, deltaY)) return true;
            if (element === modal) break;
        }
        return false;
    }

    window.addEventListener('scroll', () => {
        if (!documentScrollLocked) return;
        if (window.scrollX === lockedScrollX && window.scrollY === lockedScrollY) return;
        window.scrollTo(lockedScrollX, lockedScrollY);
    }, { passive: true });

    document.addEventListener('wheel', (event) => {
        if (!documentScrollLocked || modalCanConsumeScroll(event.target, event.deltaX, event.deltaY)) return;
        event.preventDefault();
    }, { capture: true, passive: false });

    document.addEventListener('touchstart', (event) => {
        const touch = documentScrollLocked && event.touches.length === 1 ? event.touches[0] : null;
        lastTouchX = touch?.clientX ?? null;
        lastTouchY = touch?.clientY ?? null;
    }, { capture: true, passive: true });

    document.addEventListener('touchmove', (event) => {
        if (!documentScrollLocked || lastTouchX === null || lastTouchY === null || event.touches.length !== 1) {
            return;
        }

        const currentTouchX = event.touches[0].clientX;
        const currentTouchY = event.touches[0].clientY;
        const deltaX = lastTouchX - currentTouchX;
        const deltaY = lastTouchY - currentTouchY;
        lastTouchX = currentTouchX;
        lastTouchY = currentTouchY;
        if (!modalCanConsumeScroll(event.target, deltaX, deltaY)) event.preventDefault();
    }, { capture: true, passive: false });

    const clearTouch = () => {
        lastTouchX = null;
        lastTouchY = null;
    };
    document.addEventListener('touchend', clearTouch, { capture: true, passive: true });
    document.addEventListener('touchcancel', clearTouch, { capture: true, passive: true });

    new MutationObserver(syncDocumentScrollLock).observe(document.body, {
        subtree: true,
        childList: true,
        attributes: true,
        attributeFilter: ['inert'],
    });
    syncDocumentScrollLock();
})();

let confirmCallback = null;
let confirmCancelCallback = null;
let confirmFocusTrapCleanup = null;
let promptCallback = null;
let promptFocusTrapCleanup = null;
// Each dialog remembers its own opener: they stack (a confirm over a prompt),
// and a shared variable would send focus back to the wrong layer.
let confirmOpener = null;
let promptOpener = null;

const CONFIRM_VARIANTS = {
    danger: {
        button: 'bg-red-500 hover:bg-red-600 text-white text-xs font-medium py-2 px-4 rounded-md transition-colors',
        iconCircle: 'flex items-center justify-center w-10 h-10 mx-auto bg-red-50 rounded-full mb-3',
        iconSvg: 'w-5 h-5 text-red-500',
        label: 'Delete',
    },
    primary: {
        button: 'bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2 px-4 rounded-md transition-colors',
        iconCircle: 'flex items-center justify-center w-10 h-10 mx-auto bg-stone-100 rounded-full mb-3',
        iconSvg: 'w-5 h-5 text-stone-600',
        label: 'Confirm',
    },
};

function showConfirmDialog(title, message, callback, cancelCallback, options) {
    const dialog = document.getElementById('confirm-dialog');
    const dialogContent = dialog.querySelector('div');
    const titleEl = document.getElementById('confirm-title');
    const messageEl = document.getElementById('confirm-message');
    const yesBtn = document.getElementById('confirm-yes-btn');
    const iconCircle = document.getElementById('confirm-icon-circle');
    const iconSvg = document.getElementById('confirm-icon-svg');

    const { variant = 'danger', confirmLabel } = options || {};
    const theme = CONFIRM_VARIANTS[variant] || CONFIRM_VARIANTS.danger;
    yesBtn.className = theme.button;
    yesBtn.textContent = confirmLabel || theme.label;
    iconCircle.className = theme.iconCircle;
    // SVG elements have a read-only className (SVGAnimatedString) — must use setAttribute.
    iconSvg.setAttribute('class', theme.iconSvg);

    titleEl.textContent = title;
    messageEl.innerHTML = message;
    confirmCallback = callback;
    confirmCancelCallback = cancelCallback || null;

    dialog.removeAttribute('inert');
    dialog.classList.remove('opacity-0', 'pointer-events-none');
    dialog.classList.add('opacity-100');
    dialogContent.classList.remove('scale-95');
    dialogContent.classList.add('scale-100');

    confirmOpener = document.activeElement;
    if (confirmFocusTrapCleanup) confirmFocusTrapCleanup();
    confirmFocusTrapCleanup = trapFocus(dialog);
    setTimeout(() => {
        if (!isCoveredByHigherLayer(dialog)) document.getElementById('confirm-no-btn').focus();
    }, 100);
}

function closeConfirmDialog() {
    const dialog = document.getElementById('confirm-dialog');
    const dialogContent = dialog.querySelector('div');

    if (confirmFocusTrapCleanup) {
        confirmFocusTrapCleanup();
        confirmFocusTrapCleanup = null;
    }
    dialog.classList.remove('opacity-100');
    dialog.classList.add('opacity-0', 'pointer-events-none');
    dialogContent.classList.remove('scale-100');
    dialogContent.classList.add('scale-95');

    const cancelCb = confirmCancelCallback;
    confirmCallback = null;
    confirmCancelCallback = null;
    dialog.setAttribute('inert', '');

    restoreFocus(confirmOpener);
    confirmOpener = null;

    if (cancelCb) cancelCb();
}

function showPromptDialog(title, defaultValue, callback) {
    const dialog = document.getElementById('prompt-dialog');
    const dialogContent = dialog.querySelector('div');
    const titleEl = document.getElementById('prompt-title');
    const input = document.getElementById('prompt-input');

    titleEl.textContent = title;
    input.value = defaultValue || '';
    promptCallback = callback;

    dialog.removeAttribute('inert');
    dialog.classList.remove('opacity-0', 'pointer-events-none');
    dialog.classList.add('opacity-100');
    dialogContent.classList.remove('scale-95');
    dialogContent.classList.add('scale-100');

    promptOpener = document.activeElement;
    if (promptFocusTrapCleanup) promptFocusTrapCleanup();
    promptFocusTrapCleanup = trapFocus(dialog);
    setTimeout(() => {
        if (isCoveredByHigherLayer(dialog)) return;
        input.focus();
        input.select();
    }, 100);
}

function closePromptDialog() {
    const dialog = document.getElementById('prompt-dialog');
    const dialogContent = dialog.querySelector('div');

    if (promptFocusTrapCleanup) {
        promptFocusTrapCleanup();
        promptFocusTrapCleanup = null;
    }
    dialog.classList.remove('opacity-100');
    dialog.classList.add('opacity-0', 'pointer-events-none');
    dialogContent.classList.remove('scale-100');
    dialogContent.classList.add('scale-95');
    promptCallback = null;
    dialog.setAttribute('inert', '');

    restoreFocus(promptOpener);
    promptOpener = null;
}

let conflictResolveCallback = null;
let conflictFocusTrapCleanup = null;
let conflictOpener = null;

function showConflictDialog(filenames, onResolve) {
    const dialog = document.getElementById('conflict-dialog');
    const dialogContent = dialog.querySelector('div');
    const messageEl = document.getElementById('conflict-message');
    const fileList = document.getElementById('conflict-file-list');

    messageEl.textContent = `${filenames.length} file${filenames.length === 1 ? '' : 's'} already exist${filenames.length === 1 ? 's' : ''} with different content:`;
    fileList.innerHTML = filenames.map(f => `<li class="truncate">${escapeHTML(f)}</li>`).join('');
    // A second upload while the prompt is open takes over the one dialog there
    // is. Settle the earlier upload as a skip rather than leaving its promise —
    // and every upload queued behind it — pending forever.
    if (conflictResolveCallback) {
        const stale = conflictResolveCallback;
        conflictResolveCallback = null;
        stale('skip');
    }
    conflictResolveCallback = onResolve;

    dialog.removeAttribute('inert');
    dialog.classList.remove('opacity-0', 'pointer-events-none');
    dialog.classList.add('opacity-100');
    dialogContent.classList.remove('scale-95');
    dialogContent.classList.add('scale-100');

    if (!dialog.contains(document.activeElement)) conflictOpener = document.activeElement;
    if (conflictFocusTrapCleanup) conflictFocusTrapCleanup();
    conflictFocusTrapCleanup = trapFocus(dialog);
    setTimeout(() => {
        if (!isCoveredByHigherLayer(dialog)) document.getElementById('conflict-skip-btn').focus();
    }, 100);
}

function closeConflictDialog(resolution) {
    const dialog = document.getElementById('conflict-dialog');
    const dialogContent = dialog.querySelector('div');

    if (conflictFocusTrapCleanup) {
        conflictFocusTrapCleanup();
        conflictFocusTrapCleanup = null;
    }
    dialog.classList.remove('opacity-100');
    dialog.classList.add('opacity-0', 'pointer-events-none');
    dialogContent.classList.remove('scale-100');
    dialogContent.classList.add('scale-95');
    dialog.setAttribute('inert', '');

    restoreFocus(conflictOpener);
    conflictOpener = null;

    const cb = conflictResolveCallback;
    conflictResolveCallback = null;
    if (cb) cb(resolution);
}

/**
 * Trap Tab/Shift+Tab focus within a container.
 * Returns a cleanup function to remove the listener.
 */
function trapFocus(container) {
    function handler(e) {
        if (e.key !== 'Tab') return;

        // [contenteditable="true"] is what puts CodeMirror in the cycle. It is
        // focusable without a tabindex, so the previous selector — which relied on
        // `textarea` matching the old text editor — would have dropped the editor
        // out of the trap entirely.
        const focusable = Array.from(container.querySelectorAll(
            'button:not([disabled]):not([tabindex="-1"]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [contenteditable="true"]:not([tabindex="-1"]), [tabindex]:not([tabindex="-1"])'
        )).filter(el => el.offsetParent !== null || el.offsetWidth > 0);
        if (focusable.length === 0) return;

        const first = focusable[0];
        const last = focusable[focusable.length - 1];

        if (e.shiftKey && document.activeElement === first) {
            last.focus();
            e.preventDefault();
        } else if (!e.shiftKey && document.activeElement === last) {
            first.focus();
            e.preventDefault();
        }
    }

    container.addEventListener('keydown', handler);
    return () => container.removeEventListener('keydown', handler);
}

const FOCUSABLE_SELECTOR = 'input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), button:not([disabled]), [href], [tabindex]:not([tabindex="-1"])';

function firstFocusableIn(container) {
    return Array.from(container.querySelectorAll(FOCUSABLE_SELECTOR))
        .find(el => el.offsetParent !== null || el.getClientRects().length > 0) || null;
}

/**
 * The topmost open layer (dialog or full-screen viewer), if any: a top-level
 * aria-modal element that is not inert, hidden or faded out. Highest z-index
 * wins, later in the DOM on a tie. Elements nested inside another aria-modal
 * are user content, not layers.
 */
function topmostOpenLayer() {
    let top = null;
    let topZ = 0;
    for (const el of document.querySelectorAll('[aria-modal="true"]')) {
        if (el.hasAttribute('inert') || el.classList.contains('hidden') || el.classList.contains('opacity-0')) continue;
        if (el.parentElement?.closest('[aria-modal="true"]')) continue;
        const z = parseInt(getComputedStyle(el).zIndex, 10) || 0;
        if (!top || z >= topZ) {
            top = el;
            topZ = z;
        }
    }
    return top;
}

/**
 * Return focus to the element that opened a layer. When that element is gone
 * or now sits behind an inert layer, focus goes to the topmost layer still
 * open (a list re-rendered inside Plugins detaches its row's button), and only
 * with nothing open to the drawer toggle — so focus never drops to <body>,
 * where keyboard users lose their place, nor lands behind an open modal.
 */
function restoreFocus(opener) {
    const layer = topmostOpenLayer();
    // An opener on the page itself (header, gallery, bottom bar) sits beneath
    // every layer: when one is still open — a plugin result that arrived while
    // a prompt was up — focus goes into that layer, not behind it.
    const behindLayer = layer && opener && !opener.closest?.('[aria-modal="true"]')
        && !!opener.closest?.('header, main, footer');
    if (opener && opener !== document.body && opener.isConnected && !opener.closest('[inert]') && !behindLayer) {
        opener.focus();
        return;
    }
    // A gallery reload while the layer was open replaced the opener's card:
    // land on the same clip's new card rather than out in the header.
    if (!layer && opener && !opener.isConnected) {
        const id = opener.closest?.('li[data-id]')?.dataset.id;
        const card = id && document.querySelector(`#gallery > li[data-id="${CSS.escape(id)}"]`);
        if (card && !card.closest('[inert]')) {
            card.focus();
            return;
        }
    }
    if (layer) {
        (firstFocusableIn(layer) || layer).focus();
        return;
    }
    document.getElementById('drawer-toggle-btn')?.focus();
}

/**
 * Whether a higher layer covers `layer`. A dialog opened asynchronously (a
 * plugin result, an upload conflict) can arrive beneath a prompt or confirm
 * the user is typing in; it must not pull focus out of that dialog.
 */
function isCoveredByHigherLayer(layer) {
    const top = topmostOpenLayer();
    return !!top && top !== layer && !layer.contains(top);
}

/**
 * Focus bookkeeping for a modal: open() remembers the opener, traps Tab inside
 * the modal and moves focus to `initial` (an element, or the first focusable
 * control); close() releases the trap and restores focus to the opener.
 * Re-opening while already open keeps the original opener.
 */
function createModalFocus(modal) {
    let opener = null;
    let releaseTrap = null;

    function firstFocusable() {
        return firstFocusableIn(modal);
    }

    return {
        open(initial) {
            if (!releaseTrap) opener = document.activeElement;
            else releaseTrap();
            releaseTrap = trapFocus(modal);
            if (isCoveredByHigherLayer(modal)) return;
            const target = initial || firstFocusable() || modal;
            target.focus();
        },
        close() {
            if (!releaseTrap) return;
            releaseTrap();
            releaseTrap = null;
            const previous = opener;
            opener = null;
            restoreFocus(previous);
        },
        get isOpen() { return releaseTrap !== null; },
    };
}

function showToast(message, type = 'info') {
    const toast = document.getElementById('toast');

    // Color mapping
    const colors = {
        info: 'bg-stone-800',
        success: 'bg-emerald-600',
        error: 'bg-red-600'
    };

    // Remove any existing color classes
    toast.classList.remove('bg-stone-800', 'bg-emerald-600', 'bg-red-600');

    // Add the appropriate color class
    const colorClass = colors[type] || colors.info;
    toast.classList.add(colorClass);

    toast.textContent = message;
    toast.classList.remove('translate-x-full', 'opacity-0');
    toast.classList.add('translate-x-0', 'opacity-100');

    if (window.toastTimeout) {
        clearTimeout(window.toastTimeout);
    }

    window.toastTimeout = setTimeout(() => {
        toast.classList.remove('translate-x-0', 'opacity-100');
        toast.classList.add('translate-x-full', 'opacity-0');
    }, 3000);
}

function copyToClipboard(text) {
    // Using document.execCommand as it works reliably in iFrames/sandboxed envs
    const textArea = document.createElement("textarea");
    textArea.value = text;
    textArea.style.position = "fixed"; //- Remove from old document flow
    textArea.style.left = "-9999px";
    document.body.appendChild(textArea);
    textArea.focus();
    textArea.select();
    try {
        document.execCommand('copy');
        showToast('Copied to clipboard!');
    } catch (err) {
        console.error('Failed to copy: ', err);
        showToast('Failed to copy.', 'error');
    }
    document.body.removeChild(textArea);
}

// Tag colors are free text in the database and reach the DOM from the REST
// API, plugins, followed shares and restored backups. Only a hex color or a
// bare CSS keyword is interpolated into markup; anything else could close the
// attribute or chain extra declarations onto a style, so it falls back to the
// default stone.
function safeTagColor(color) {
    return typeof color === 'string' && /^(#([0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})|[a-zA-Z]{1,30})$/.test(color)
        ? color
        : '#78716C';
}

// Message text of a rejected binding call. Wails v2 rejects with a plain
// string (the Go error text), REST glue and JS code with an Error object.
function errText(e) {
    return (e && e.message) ? e.message : String(e);
}

function escapeHTML(str) {
    if (!str) return '';
    return str.replace(/[&<>"']/g, function (m) {
        return {
            '&': '&amp;',
            '<': '&lt;',
            '>': '&gt;',
            '"': '&quot;',
            "'": '&#039;'
        }[m];
    });
}


function matchesMimePattern(contentType, fileTypes) {
    if (!fileTypes || fileTypes.length === 0) return true;
    if (!contentType) return false;
    const ct = contentType.toLowerCase();
    return fileTypes.some(pattern => {
        const p = pattern.toLowerCase();
        if (p.endsWith('/*')) return ct.startsWith(p.slice(0, -1));
        return ct === p;
    });
}

function shouldShowPluginAction(action, clip) {
    if (!matchesMimePattern(clip.content_type, action.file_types)) return false;
    if (action.max_size && action.max_size > 0 && clip.size > action.max_size) return false;
    return true;
}

// Format remaining time for expiration badge
// Returns compact string like "23m", "2h", "3d"
function formatTimeRemaining(expiresAt) {
    const now = new Date();
    const expires = new Date(expiresAt);
    const diffMs = expires - now;

    if (diffMs <= 0) return '0m';

    const minutes = Math.ceil(diffMs / 60000);
    if (minutes < 60) return `${minutes}m`;

    const hours = Math.round(diffMs / 3600000);
    if (hours < 24) return `${hours}h`;

    const days = Math.round(diffMs / 86400000);
    return `${days}d`;
}

function formatFileSize(bytes) {
    if (!bytes || bytes === 0) return '0 B';
    const neg = bytes < 0;
    const abs = Math.abs(bytes);
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.min(sizes.length - 1, Math.floor(Math.log(abs) / Math.log(k)));
    const value = parseFloat((abs / Math.pow(k, i)).toFixed(i === 0 ? 0 : 1));
    return (neg ? '-' : '') + value + ' ' + sizes[i];
}

// Alias for consistency
const formatBytes = formatFileSize;

function getFriendlyFileType(contentType, filename) {
    // Markdown behavior is filename-authoritative. Preserve a declared
    // text/markdown MIME for transfers without showing an MD badge on a
    // differently named clip.
    if (contentType === 'text/markdown' && !/\.(?:md|markdown)$/i.test(filename || '')) {
        const filenameExt = (filename || '').split('.').pop();
        return filenameExt && filenameExt !== filename && filenameExt.length <= 5
            ? filenameExt.toUpperCase()
            : 'FILE';
    }

    // Map of MIME types to friendly names
    const mimeMap = {
        'application/pdf': 'PDF',
        'application/zip': 'ZIP',
        'application/x-zip-compressed': 'ZIP',
        'application/json': 'JSON',
        'application/javascript': 'JS',
        'application/xml': 'XML',
        'application/vnd.openxmlformats-officedocument.wordprocessingml.document': 'DOCX',
        'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet': 'XLSX',
        'application/vnd.openxmlformats-officedocument.presentationml.presentation': 'PPTX',
        'application/msword': 'DOC',
        'application/vnd.ms-excel': 'XLS',
        'application/vnd.ms-powerpoint': 'PPT',
        'application/rtf': 'RTF',
        'application/x-tar': 'TAR',
        'application/gzip': 'GZ',
        'application/x-rar-compressed': 'RAR',
        'application/x-7z-compressed': '7Z',
        'text/plain': 'TXT',
        'text/html': 'HTML',
        'text/css': 'CSS',
        'text/csv': 'CSV',
        'text/markdown': 'MD',
        'image/jpeg': 'JPG',
        'image/png': 'PNG',
        'image/gif': 'GIF',
        'image/webp': 'WEBP',
        'image/svg+xml': 'SVG',
        'image/bmp': 'BMP',
        'image/tiff': 'TIFF',
        'audio/mpeg': 'MP3',
        'audio/wav': 'WAV',
        'audio/ogg': 'OGG',
        'video/mp4': 'MP4',
        'video/webm': 'WEBM',
        'video/quicktime': 'MOV',
    };

    // Check if we have a direct mapping
    if (mimeMap[contentType]) {
        return mimeMap[contentType];
    }

    // Try to get extension from filename
    if (filename) {
        const ext = filename.split('.').pop();
        if (ext && ext.length <= 5) {
            return ext.toUpperCase();
        }
    }

    // Fallback: use the subtype but truncate if too long
    const subtype = contentType.split('/')[1] || 'FILE';
    if (subtype.length > 8) {
        // For long subtypes, try to extract a meaningful part
        if (subtype.includes('.')) {
            const parts = subtype.split('.');
            return parts[parts.length - 1].toUpperCase().substring(0, 8);
        }
        return subtype.substring(0, 6).toUpperCase() + '…';
    }
    return subtype.toUpperCase();
}

// --- Tag Hierarchy Utilities ---

function getTagDepth(name) {
    return (name.match(/\//g) || []).length;
}

function getParentTagName(name) {
    const i = name.lastIndexOf('/');
    return i < 0 ? '' : name.substring(0, i);
}

function getShortTagName(name) {
    const i = name.lastIndexOf('/');
    return i < 0 ? name : name.substring(i + 1);
}

function isDescendantOf(child, parent) {
    return child.startsWith(parent + '/');
}

function isImmediateChildOf(child, parent) {
    if (parent === '') return !child.includes('/');
    if (!child.startsWith(parent + '/')) return false;
    return !child.substring(parent.length + 1).includes('/');
}

// Nearest ancestor of `name` present in `byName` (a name-keyed lookup), or ''
// when none exists. Deleting a mid-level tag (a/b) leaves its descendants
// (a/b/c) behind; they hang off the nearest surviving ancestor (a), matching
// the backend's GetChildTags/GetTopLevelTags.
function nearestExistingAncestorName(name, byName) {
    for (let p = getParentTagName(name); p; p = getParentTagName(p)) {
        if (byName[p]) return p;
    }
    return '';
}

function buildTagTree(tags) {
    const byName = {};
    for (const tag of tags) {
        byName[tag.name] = { tag, children: [] };
    }
    const roots = [];
    for (const tag of tags) {
        const parentName = nearestExistingAncestorName(tag.name, byName);
        if (parentName) {
            byName[parentName].children.push(byName[tag.name]);
        } else {
            roots.push(byName[tag.name]);
        }
    }
    return roots;
}

async function openFolderRenameDialog(tagID, currentName) {
    const shortName = getShortTagName(currentName);
    const parent = currentName.includes('/') ? currentName.substring(0, currentName.lastIndexOf('/')) : '';
    showPromptDialog('Rename Folder', shortName, async (newShortName) => {
        if (!newShortName || newShortName.trim() === '' || newShortName === shortName) return;
        if (newShortName.includes('/')) {
            showToast('Name cannot contain "/". Use Move to change parent.', 'error');
            return;
        }
        const newPath = parent ? `${parent}/${newShortName.trim()}` : newShortName.trim();
        try {
            // '' keeps the stored color.
            await window.go.main.App.UpdateTag(tagID, newPath, '');
            showToast(`Renamed to ${newShortName}`, 'success');
            if (typeof window.renderFolderCards === 'function') await window.renderFolderCards();
            if (typeof loadClips === 'function') await loadClips();
        } catch (e) {
            showToast('Rename failed: ' + (e?.message || e), 'error');
        }
    });
}
window.openFolderRenameDialog = openFolderRenameDialog;
