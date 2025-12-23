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

function showToast(message) {
    const toast = document.getElementById('toast');
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
