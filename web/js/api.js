async function loadClips() {
    try {
        const url = isViewingArchive ? '/clips?archived=true' : '/clips';
        const response = await fetch(url);
        if (!response.ok) throw new Error('Failed to load clips');
        const clips = await response.json();

        gallery.innerHTML = ''; // Clear gallery
        selectedIds.clear();
        imageClips = []; // Clear image clips
        updateBulkToolbar();
        if (clips && clips.length > 0) {
            clips.forEach(createClipCard);
        } else {
            const emptyMsg = isViewingArchive
                ? 'No archived clips.'
                : 'No active clips. Paste or drop something!';
            gallery.innerHTML = `<p class="text-gray-500 col-span-full text-center">${emptyMsg}</p>`;
        }
    } catch (error) {
        console.error('Error loading clips:', error);
        gallery.innerHTML = '<p class="text-red-500 col-span-full text-center">Error loading clips. Is the server running?</p>';
    }
}

async function upload(formData) {
    try {
        const response = await fetch('/upload', {
            method: 'POST',
            body: formData
        });
        if (!response.ok) throw new Error('Upload failed');
        showToast('Upload successful!');
        if (!isViewingArchive) {
            loadClips(); // Refresh gallery only if looking at active
        }
    } catch (error) {
        console.error('Error uploading:', error);
        showToast('Upload failed.');
    }
}

async function deleteClip(id) {
    showConfirmDialog('Delete Clip', 'Are you sure you want to delete this clip permanently?', async () => {
        try {
            const response = await fetch('/clip/' + id, { method: 'DELETE' });
            if (!response.ok) throw new Error('Delete failed');
            showToast('Clip deleted.');
            loadClips(); // Refresh gallery
        } catch (error) {
            console.error('Error deleting clip:', error);
            showToast('Failed to delete clip.');
        }
    });
}

async function toggleArchiveClip(id) {
    try {
        const response = await fetch('/archive/' + id, { method: 'POST' });
        if (!response.ok) throw new Error('Failed to toggle archive status');
        showToast(isViewingArchive ? 'Clip restored.' : 'Clip archived.');
        loadClips(); // Refresh gallery to remove/add the item
    } catch (error) {
        console.error('Error toggling archive:', error);
        showToast('Failed to change archive status.');
    }
}

async function cancelExpiration(id) {
    try {
        const response = await fetch('/cancel-expiration/' + id, { method: 'POST' });
        if (!response.ok) throw new Error('Failed to cancel expiration');
        showToast('Clip is now permanent.');
        loadClips(); // Refresh gallery
    } catch (error) {
        console.error('Error cancelling expiration:', error);
        showToast('Failed to cancel expiration.');
    }
}

async function saveTempFile(id) {
    try {
        const response = await fetch('/tempfile/' + id, { method: 'POST' });
        if (!response.ok) throw new Error('Failed to create temp file');
        const result = await response.json();
        if (result.path) {
            copyToClipboard(result.path);
        } else {
            throw new Error('Invalid response from server');
        }
    } catch (error) {
        console.error('Error saving temp file:', error);
        showToast('Failed to save temp file.');
    }
}

function copyUrl(id) {
    const url = window.location.origin + '/clip/' + id;
    copyToClipboard(url);
}

async function deleteAllTempFiles() {
    showConfirmDialog('Delete All Temp Files', 'Are you sure you want to delete ALL temporary files?', async () => {
        try {
            const response = await fetch('/tempfiles', { method: 'DELETE' });
            if (!response.ok) throw new Error('Failed to delete temp files');
            showToast('All temp files deleted.');
        } catch (error) {
            console.error('Error deleting temp files:', error);
            showToast('Failed to delete temp files.');
        }
    });
}

async function bulkDelete() {
    if (selectedIds.size === 0) return;
    showConfirmDialog('Bulk Delete', `Are you sure you want to delete ${selectedIds.size} clips permanently ? `, async () => {
        try {
            const response = await fetch('/bulk-delete', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ids: Array.from(selectedIds) })
            });
            if (!response.ok) throw new Error('Bulk delete failed');
            showToast(`Deleted ${selectedIds.size} clips.`);
            selectedIds.clear();
            loadClips();
        } catch (error) {
            console.error('Error in bulk delete:', error);
            showToast('Bulk delete failed.');
        }
    });
}

async function bulkArchive() {
    if (selectedIds.size === 0) return;
    try {
        const response = await fetch('/bulk-archive', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ids: Array.from(selectedIds) })
        });
        if (!response.ok) throw new Error('Bulk archive failed');
        showToast(isViewingArchive ? `Restored ${selectedIds.size} clips.` : `Archived ${selectedIds.size} clips.`);
        selectedIds.clear();
        loadClips();
    } catch (error) {
        console.error('Error in bulk archive:', error);
        showToast('Bulk archive failed.');
    }
}

function bulkDownload() {
    if (selectedIds.size === 0) return;
    const ids = Array.from(selectedIds).join(',');
    window.location.href = `/bulk-download?ids=${ids}`;
}
