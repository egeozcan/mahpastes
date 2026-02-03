let confirmCallback = null;

function showConfirmDialog(title, message, callback) {
    const dialog = document.getElementById('confirm-dialog');
    const dialogContent = dialog.querySelector('div');
    const titleEl = document.getElementById('confirm-title');
    const messageEl = document.getElementById('confirm-message');

    titleEl.textContent = title;
    messageEl.textContent = message;
    confirmCallback = callback;

    dialog.classList.remove('opacity-0', 'pointer-events-none');
    dialog.classList.add('opacity-100');
    dialogContent.classList.remove('scale-95');
    dialogContent.classList.add('scale-100');

    lastFocusedElement = document.activeElement;
    setTimeout(() => {
        document.getElementById('confirm-no-btn').focus();
    }, 100);
}

function closeConfirmDialog() {
    const dialog = document.getElementById('confirm-dialog');
    const dialogContent = dialog.querySelector('div');

    dialog.classList.remove('opacity-100');
    dialog.classList.add('opacity-0', 'pointer-events-none');
    dialogContent.classList.remove('scale-100');
    dialogContent.classList.add('scale-95');
    confirmCallback = null;

    if (lastFocusedElement) {
        lastFocusedElement.focus();
    }
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

// Alias for compatibility
const escapeHtml = escapeHTML;

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
