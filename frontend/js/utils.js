// Initialize test helpers early so all scripts can register their helpers
window.__testHelpers = {};

let confirmCallback = null;
let confirmCancelCallback = null;
let confirmFocusTrapCleanup = null;
let promptCallback = null;
let promptFocusTrapCleanup = null;

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

    lastFocusedElement = document.activeElement;
    if (confirmFocusTrapCleanup) confirmFocusTrapCleanup();
    confirmFocusTrapCleanup = trapFocus(dialog);
    setTimeout(() => {
        document.getElementById('confirm-no-btn').focus();
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

    if (lastFocusedElement) {
        lastFocusedElement.focus();
    }

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

    lastFocusedElement = document.activeElement;
    if (promptFocusTrapCleanup) promptFocusTrapCleanup();
    promptFocusTrapCleanup = trapFocus(dialog);
    setTimeout(() => {
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

    if (lastFocusedElement) {
        lastFocusedElement.focus();
    }
}

let conflictResolveCallback = null;
let conflictFocusTrapCleanup = null;

function showConflictDialog(filenames, onResolve) {
    const dialog = document.getElementById('conflict-dialog');
    const dialogContent = dialog.querySelector('div');
    const messageEl = document.getElementById('conflict-message');
    const fileList = document.getElementById('conflict-file-list');

    messageEl.textContent = `${filenames.length} file${filenames.length === 1 ? '' : 's'} already exist${filenames.length === 1 ? 's' : ''} with different content:`;
    fileList.innerHTML = filenames.map(f => `<li class="truncate">${escapeHTML(f)}</li>`).join('');
    conflictResolveCallback = onResolve;

    dialog.removeAttribute('inert');
    dialog.classList.remove('opacity-0', 'pointer-events-none');
    dialog.classList.add('opacity-100');
    dialogContent.classList.remove('scale-95');
    dialogContent.classList.add('scale-100');

    lastFocusedElement = document.activeElement;
    if (conflictFocusTrapCleanup) conflictFocusTrapCleanup();
    conflictFocusTrapCleanup = trapFocus(dialog);
    setTimeout(() => document.getElementById('conflict-skip-btn').focus(), 100);
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

    if (lastFocusedElement) lastFocusedElement.focus();

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

        const focusable = Array.from(container.querySelectorAll(
            'button:not([disabled]):not([tabindex="-1"]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
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
        showToast('Failed to copy.');
    }
    document.body.removeChild(textArea);
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

function buildTagTree(tags) {
    const byName = {};
    for (const tag of tags) {
        byName[tag.name] = { tag, children: [] };
    }
    const roots = [];
    for (const tag of tags) {
        const parentName = getParentTagName(tag.name);
        if (parentName && byName[parentName]) {
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
            const tag = (await window.go.main.App.GetTags()).find(t => t.id === tagID);
            await window.go.main.App.UpdateTag(tagID, newPath, tag?.color || '#000000');
            showToast(`Renamed to ${newShortName}`, 'success');
            if (typeof window.renderFolderCards === 'function') await window.renderFolderCards();
            if (typeof loadClips === 'function') await loadClips();
        } catch (e) {
            showToast('Rename failed: ' + (e?.message || e), 'error');
        }
    });
}
window.openFolderRenameDialog = openFolderRenameDialog;
