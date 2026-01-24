// Wails API - replaces fetch-based api.js
// All methods call Go bindings via window.go.main.App.*

async function loadClips() {
    try {
        const clips = await window.go.main.App.GetClips(isViewingArchive);

        gallery.innerHTML = ''; // Clear gallery
        selectedIds.clear();
        imageClips = []; // Clear image clips
        updateBulkToolbar();

        if (clips && clips.length > 0) {
            for (const clip of clips) {
                await createClipCard(clip);
            }
        } else {
            const emptyMsg = isViewingArchive
                ? 'No archived clips.'
                : 'No active clips. Paste or drop something!';
            gallery.innerHTML = `<p class="text-gray-500 col-span-full text-center">${emptyMsg}</p>`;
        }
    } catch (error) {
        console.error('Error loading clips:', error);
        gallery.innerHTML = '<p class="text-red-500 col-span-full text-center">Error loading clips.</p>';
    }
}

async function upload(files, expiration) {
    try {
        await window.go.main.App.UploadFiles(files, expiration);
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
            await window.go.main.App.DeleteClip(id);
            showToast('Clip deleted.');
            loadClips();
        } catch (error) {
            console.error('Error deleting clip:', error);
            showToast('Failed to delete clip.');
        }
    });
}

async function toggleArchiveClip(id) {
    try {
        await window.go.main.App.ToggleArchive(id);
        showToast(isViewingArchive ? 'Clip restored.' : 'Clip archived.');
        loadClips();
    } catch (error) {
        console.error('Error toggling archive:', error);
        showToast('Failed to change archive status.');
    }
}

async function cancelExpiration(id) {
    try {
        await window.go.main.App.CancelExpiration(id);
        showToast('Clip is now permanent.');
        loadClips();
    } catch (error) {
        console.error('Error cancelling expiration:', error);
        showToast('Failed to cancel expiration.');
    }
}

async function saveTempFile(id) {
    try {
        const path = await window.go.main.App.CreateTempFile(id);
        if (path) {
            copyToClipboard(path);
        } else {
            throw new Error('Invalid response');
        }
    } catch (error) {
        console.error('Error saving temp file:', error);
        showToast('Failed to save temp file.');
    }
}

async function deleteAllTempFiles() {
    showConfirmDialog('Delete All Temp Files', 'Are you sure you want to delete ALL temporary files?', async () => {
        try {
            await window.go.main.App.DeleteAllTempFiles();
            showToast('All temp files deleted.');
        } catch (error) {
            console.error('Error deleting temp files:', error);
            showToast('Failed to delete temp files.');
        }
    });
}

async function bulkDelete() {
    if (selectedIds.size === 0) return;
    showConfirmDialog('Bulk Delete', `Are you sure you want to delete ${selectedIds.size} clips permanently?`, async () => {
        try {
            await window.go.main.App.BulkDelete(Array.from(selectedIds));
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
        await window.go.main.App.BulkArchive(Array.from(selectedIds));
        showToast(isViewingArchive ? `Restored ${selectedIds.size} clips.` : `Archived ${selectedIds.size} clips.`);
        selectedIds.clear();
        loadClips();
    } catch (error) {
        console.error('Error in bulk archive:', error);
        showToast('Bulk archive failed.');
    }
}

async function bulkDownload() {
    if (selectedIds.size === 0) return;
    try {
        await window.go.main.App.BulkDownloadToFile(Array.from(selectedIds));
        showToast('Download complete.');
    } catch (error) {
        console.error('Error in bulk download:', error);
        // User cancelled is not an error
        if (!error.message.includes('cancelled')) {
            showToast('Bulk download failed.');
        }
    }
}

// Helper function to convert File to FileData format
async function fileToFileData(file) {
    return new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => {
            // Remove the data URL prefix (e.g., "data:image/png;base64,")
            const base64 = reader.result.split(',')[1];
            resolve({
                name: file.name,
                content_type: file.type || 'application/octet-stream',
                data: base64
            });
        };
        reader.onerror = reject;
        reader.readAsDataURL(file);
    });
}

// Get clip data (for images and editor)
async function getClipData(id) {
    try {
        return await window.go.main.App.GetClipData(id);
    } catch (error) {
        console.error('Error getting clip data:', error);
        throw error;
    }
}

// Save clip to file using native dialog
async function saveClipToFile(id) {
    try {
        await window.go.main.App.SaveClipToFile(id);
    } catch (error) {
        console.error('Error saving clip to file:', error);
        if (!error.message.includes('cancelled')) {
            showToast('Failed to save file.');
        }
    }
}
