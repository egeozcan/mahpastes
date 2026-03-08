// Wails API - replaces fetch-based api.js
// All methods call Go bindings via window.go.main.App.*

async function loadClips() {
    try {
        if (typeof ShortcutManager !== 'undefined') ShortcutManager.clearFocus();
        // Build set of filter IDs + their ancestors so hidden parent tags
        // are revealed when filtering by a subtag.
        const revealedIds = new Set(activeTagFilters);
        for (const filterId of activeTagFilters) {
            const tag = allTags.find(t => t.id === filterId);
            if (tag) {
                let parentName = getParentTagName(tag.name);
                while (parentName) {
                    const parentTag = allTags.find(t => t.name === parentName);
                    if (parentTag) revealedIds.add(parentTag.id);
                    parentName = getParentTagName(parentName);
                }
            }
        }
        const effectiveHidden = getHiddenTags().filter(id => !revealedIds.has(id));

        let clips;
        if (isFolderMode() && activeTagFilters.length > 0) {
            // Show clips tagged directly with this folder's tag, excluding clips
            // that also have a descendant tag (those belong in subfolders).
            const currentFolderTagId = activeTagFilters[activeTagFilters.length - 1];
            clips = await window.go.main.App.GetFolderClips(isViewingArchive, currentFolderTagId, effectiveHidden, currentSortField, currentSortDir);
        } else if (isFolderMode()) {
            // Root level folder mode: only show untagged clips alongside folder cards
            clips = await window.go.main.App.GetUntaggedClips(isViewingArchive, effectiveHidden, currentSortField, currentSortDir);
        } else {
            clips = await window.go.main.App.GetClips(isViewingArchive, activeTagFilters, effectiveHidden, currentSortField, currentSortDir);
        }

        if (typeof clearPreparedDragState === 'function') {
            clearPreparedDragState();
        }

        gallery.innerHTML = '';
        selectedIds.clear();
        imageClips = [];
        updateBulkToolbar();

        // Render folder cards in folder mode
        if (isFolderMode()) {
            await renderFolderCards();
        }

        if (clips && clips.length > 0) {
            for (const clip of clips) {
                await createClipCard(clip);
            }
            // Update count to include folder cards
            const folderCount = gallery.querySelectorAll('[data-folder]').length;
            updateClipCount(clips.length + folderCount);
        } else if (!isFolderMode() || gallery.children.length === 0) {
            let emptyMsg;
            if (activeTagFilters.length > 0) {
                emptyMsg = 'No clips match the selected tags.';
            } else if (isViewingArchive) {
                emptyMsg = 'No archived clips.';
            } else {
                emptyMsg = 'No active clips. Paste or drop something!';
            }
            gallery.innerHTML = `<p class="text-gray-500 col-span-full text-center">${emptyMsg}</p>`;
            updateClipCount(0);
        } else {
            updateClipCount(gallery.querySelectorAll('[data-folder]').length);
        }
        if (typeof checkDuplicatesExist === 'function') checkDuplicatesExist();
    } catch (error) {
        console.error('Error loading clips:', error);
        gallery.innerHTML = '<p class="text-red-500 col-span-full text-center">Error loading clips.</p>';
    }
}

async function upload(files) {
    try {
        const minutes = typeof getUploadExpirationMinutes === 'function' ? getUploadExpirationMinutes() : 0;
        await window.go.main.App.UploadFiles(files, minutes);
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

async function copyFileToClipboard(id) {
    try {
        await window.go.main.ClipboardService.CopyFileToClipboard(id);
        showToast('File copied to clipboard!');
    } catch (error) {
        console.error('Error copying file to clipboard:', error);
        showToast('Failed to copy file.');
    }
}

async function copyClipContents(id) {
    try {
        await window.go.main.ClipboardService.CopyClipContents(id);
        showToast('Contents copied to clipboard!');
    } catch (error) {
        console.error('Error copying contents:', error);
        showToast('Failed to copy contents.');
    }
}

async function deleteAllTempFiles() {
    showConfirmDialog('Delete All Temp Files', 'Are you sure you want to delete ALL temporary files?', async () => {
        try {
            await window.go.main.App.DeleteAllTempFiles();
            if (typeof clearPreparedDragState === 'function') {
                clearPreparedDragState();
            }
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

async function bulkCopyFiles() {
    if (selectedIds.size === 0) return;
    try {
        await window.go.main.ClipboardService.BulkCopyFilesToClipboard(Array.from(selectedIds));
        showToast(`${selectedIds.size} file${selectedIds.size > 1 ? 's' : ''} copied to clipboard!`);
    } catch (error) {
        console.error('Error copying files to clipboard:', error);
        showToast('Failed to copy files.');
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

// --- Tag API functions ---

async function getAllTags() {
    try {
        return await window.go.main.App.GetTags();
    } catch (error) {
        console.error('Error getting tags:', error);
        return [];
    }
}

async function createTag(name) {
    try {
        const tag = await window.go.main.App.CreateTag(name);
        showToast(`Tag "${name}" created.`);
        return tag;
    } catch (error) {
        console.error('Error creating tag:', error);
        showToast(error.message || 'Failed to create tag.');
        return null;
    }
}

async function updateTag(id, name, color) {
    try {
        await window.go.main.App.UpdateTag(id, name, color);
        showToast('Tag updated.');
    } catch (error) {
        console.error('Error updating tag:', error);
        showToast(error.message || 'Failed to update tag.');
    }
}

async function deleteTag(id) {
    try {
        await window.go.main.App.DeleteTag(id);
        showToast('Tag deleted.');
    } catch (error) {
        console.error('Error deleting tag:', error);
        showToast('Failed to delete tag.');
    }
}

async function addTagToClip(clipId, tagId) {
    try {
        await window.go.main.App.AddTagToClip(clipId, tagId);
    } catch (error) {
        console.error('Error adding tag to clip:', error);
        showToast('Failed to add tag.');
    }
}

async function removeTagFromClip(clipId, tagId) {
    try {
        await window.go.main.App.RemoveTagFromClip(clipId, tagId);
    } catch (error) {
        console.error('Error removing tag from clip:', error);
        showToast('Failed to remove tag.');
    }
}

async function bulkAddTag(clipIds, tagId) {
    try {
        await window.go.main.App.BulkAddTag(clipIds, tagId);
        showToast(`Tag added to ${clipIds.length} clips.`);
        loadClips();
    } catch (error) {
        console.error('Error in bulk add tag:', error);
        showToast('Failed to add tag to clips.');
    }
}

async function bulkRemoveTag(clipIds, tagId) {
    try {
        await window.go.main.App.BulkRemoveTag(clipIds, tagId);
        showToast(`Tag removed from ${clipIds.length} clips.`);
        loadClips();
    } catch (error) {
        console.error('Error in bulk remove tag:', error);
        showToast('Failed to remove tag from clips.');
    }
}

// --- Tag Hierarchy API functions ---

async function getChildTags(tagId) {
    try {
        return await window.go.main.App.GetChildTags(tagId);
    } catch (error) {
        console.error('Error getting child tags:', error);
        return [];
    }
}

async function getTopLevelTags() {
    try {
        return await window.go.main.App.GetTopLevelTags();
    } catch (error) {
        console.error('Error getting top level tags:', error);
        return [];
    }
}

async function getDescendantClipCount(tagId) {
    try {
        return await window.go.main.App.GetDescendantClipCount(tagId);
    } catch (error) {
        console.error('Error getting descendant clip count:', error);
        return 0;
    }
}

async function getClipsDirect(archived, tagIds, hiddenTagIds, sortField, sortDir) {
    try {
        return await window.go.main.App.GetClipsDirect(archived, tagIds, hiddenTagIds, sortField, sortDir);
    } catch (error) {
        console.error('Error loading clips direct:', error);
        return [];
    }
}

async function getUntaggedClips(archived, hiddenTagIds, sortField, sortDir) {
    try {
        return await window.go.main.App.GetUntaggedClips(archived, hiddenTagIds, sortField, sortDir);
    } catch (error) {
        console.error('Error loading untagged clips:', error);
        return [];
    }
}

// --- Expiration API functions ---

async function setExpiration(id, minutes) {
    try {
        await window.go.main.App.SetExpiration(id, minutes);
        showToast('Expiration set.');
        loadClips();
    } catch (error) {
        console.error('Error setting expiration:', error);
        showToast('Failed to set expiration.');
    }
}

async function cancelExpiration(id) {
    try {
        await window.go.main.App.CancelExpiration(id);
        showToast('Expiration canceled.');
        loadClips();
    } catch (error) {
        console.error('Error canceling expiration:', error);
        showToast('Failed to cancel expiration.');
    }
}

async function bulkSetExpiration(ids, minutes) {
    try {
        await window.go.main.App.BulkSetExpiration(ids, minutes);
        showToast(`Expiration set on ${ids.length} clips.`);
        loadClips();
    } catch (error) {
        console.error('Error in bulk set expiration:', error);
        showToast('Failed to set expiration.');
    }
}

async function bulkCancelExpiration(ids) {
    try {
        await window.go.main.App.BulkCancelExpiration(ids);
        showToast(`Expiration canceled on ${ids.length} clips.`);
        loadClips();
    } catch (error) {
        console.error('Error in bulk cancel expiration:', error);
        showToast('Failed to cancel expiration.');
    }
}
