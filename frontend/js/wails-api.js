// Wails API - replaces fetch-based api.js
// All methods call Go bindings via window.go.main.App.*

let _pendingFocusAfterLoad = false;

async function loadClips({ focusFirst = false } = {}) {
    _pendingFocusAfterLoad = focusFirst;
    try {
        // Clear gallery focus, but not if user is interacting with the tag filter dropdown
        const tagDropdown = document.getElementById('tag-filter-dropdown');
        if (typeof ShortcutManager !== 'undefined' &&
            !(tagDropdown && !tagDropdown.classList.contains('hidden') && tagDropdown.contains(document.activeElement))) {
            ShortcutManager.clearFocus();
        }
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
            if (typeof initFolderDrag === 'function') initFolderDrag();
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

        // Re-index roving tabindex after gallery re-render
        if (window.__galleryRover) window.__galleryRover.update();

        // Focus first gallery item after folder navigation or search
        if (_pendingFocusAfterLoad) {
            _pendingFocusAfterLoad = false;
            const firstItem = gallery.querySelector(':scope > li');
            if (firstItem) {
                if (window.__galleryRover) window.__galleryRover.setActiveIndex(0);
                firstItem.focus();
            } else {
                // No items — focus the last breadcrumb remove button
                const pills = document.querySelectorAll('#active-tags-container button[aria-label^="Remove"]');
                if (pills.length > 0) {
                    pills[pills.length - 1].focus();
                }
            }
        }
    } catch (error) {
        console.error('Error loading clips:', error);
        gallery.innerHTML = '<p class="text-red-500 col-span-full text-center">Error loading clips.</p>';
    }
}

async function upload(files) {
    try {
        const minutes = typeof getUploadExpirationMinutes === 'function' ? getUploadExpirationMinutes() : 0;
        let autoTagID = 0;
        if (isFolderMode() && activeTagFilters.length > 0) {
            autoTagID = activeTagFilters[activeTagFilters.length - 1];
        }

        const result = await checkAndResolveConflicts(files, autoTagID);
        if (!result) return; // user cancelled

        // Overwrite existing clips
        for (const { clipID, fileData } of result.toOverwrite) {
            await window.go.main.App.UpdateClipData(clipID, fileData.content_type, fileData.data, fileData.name);
        }

        // Upload new files
        if (result.toUpload.length > 0) {
            await window.go.main.App.UploadFiles(result.toUpload, minutes, autoTagID);
        }

        const totalProcessed = result.toUpload.length + result.toOverwrite.length;
        if (totalProcessed > 0 || result.skippedCount > 0) {
            const parts = [];
            if (result.toUpload.length > 0) parts.push(`${result.toUpload.length} uploaded`);
            if (result.toOverwrite.length > 0) parts.push(`${result.toOverwrite.length} overwritten`);
            if (result.skippedCount > 0) parts.push(`${result.skippedCount} skipped`);
            showToast(parts.join(', ') + '.');
        }

        if (!isViewingArchive) {
            loadClips();
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

function renameClip(id) {
    const card = gallery.querySelector(`li[data-id="${id}"]`);
    const currentName = card?.querySelector('.p-2\\.5 p')?.getAttribute('title') || '';
    showPromptDialog('Rename Clip', currentName, async (newName) => {
        if (!newName || !newName.trim()) return;
        try {
            await window.go.main.App.RenameClip(id, newName.trim());
            showToast('Clip renamed.');
            loadClips();
        } catch (error) {
            console.error('Error renaming clip:', error);
            showToast('Failed to rename clip.', 'error');
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
        const ids = Array.from(selectedIds);
        if (isViewingArchive) {
            await window.go.main.App.BulkUnarchive(ids);
            showToast(`Restored ${selectedIds.size} clips.`);
        } else {
            await window.go.main.App.BulkArchive(ids);
            showToast(`Archived ${selectedIds.size} clips.`);
        }
        selectedIds.clear();
        loadClips();
    } catch (error) {
        console.error('Error in bulk archive:', error);
        showToast(isViewingArchive ? 'Bulk restore failed.' : 'Bulk archive failed.');
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

/**
 * Compute SHA-256 hex hash of base64-encoded data.
 * Uses Web Crypto API to match the Go backend's computeContentHash.
 */
async function computeFileHash(base64Data) {
    const binary = atob(base64Data);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
    const hashBuffer = await crypto.subtle.digest('SHA-256', bytes);
    const hashArray = Array.from(new Uint8Array(hashBuffer));
    return hashArray.map(b => b.toString(16).padStart(2, '0')).join('');
}

/**
 * Check incoming files against existing clips in the target tag.
 * Silently skips identical-content files; prompts user for different-content conflicts.
 *
 * @param {Array<{name: string, content_type: string, data: string}>} fileDataArray
 * @param {number} tagID - Target tag ID (0 for untagged)
 * @returns {Promise<{toUpload: Array, toOverwrite: Array<{clipID: number, fileData: object}>, skippedCount: number} | null>}
 *          Returns null if the user cancelled / closed the dialog.
 */
async function checkAndResolveConflicts(fileDataArray, tagID) {
    const filenames = fileDataArray.map(f => f.name);
    let matches;
    try {
        matches = await window.go.main.App.FindClipsByFilenameAndTag(filenames, tagID);
    } catch (err) {
        console.error('Error checking for duplicates:', err);
        return { toUpload: fileDataArray, toOverwrite: [], skippedCount: 0 };
    }

    if (!matches || matches.length === 0) {
        return { toUpload: fileDataArray, toOverwrite: [], skippedCount: 0 };
    }

    // Build a map: filename → { clipID, existingHash }
    // Results are ordered by id DESC, so first match per filename is the most recent.
    const matchMap = {};
    for (const m of matches) {
        if (!matchMap[m.filename]) {
            matchMap[m.filename] = { clipID: m.id, existingHash: m.content_hash };
        }
    }

    // Separate files into: no conflict, identical (skip), different content (conflict)
    const noConflict = [];
    const identical = [];
    const conflicts = []; // { fileData, clipID }

    for (const fd of fileDataArray) {
        const match = matchMap[fd.name];
        if (!match) {
            noConflict.push(fd);
            continue;
        }
        const incomingHash = await computeFileHash(fd.data);
        if (incomingHash === match.existingHash) {
            identical.push(fd);
        } else {
            conflicts.push({ fileData: fd, clipID: match.clipID });
        }
    }

    // If all matches were identical content, skip silently
    if (conflicts.length === 0) {
        if (identical.length > 0) {
            showToast(`${identical.length} identical file${identical.length === 1 ? '' : 's'} skipped.`);
        }
        return { toUpload: noConflict, toOverwrite: [], skippedCount: identical.length };
    }

    // Show conflict dialog and wait for user choice
    const resolution = await new Promise(resolve => {
        showConflictDialog(conflicts.map(c => c.fileData.name), resolve);
    });

    if (resolution === 'overwrite') {
        return {
            toUpload: noConflict,
            toOverwrite: conflicts.map(c => ({ clipID: c.clipID, fileData: c.fileData })),
            skippedCount: identical.length,
        };
    } else if (resolution === 'keep') {
        return {
            toUpload: [...noConflict, ...conflicts.map(c => c.fileData)],
            toOverwrite: [],
            skippedCount: identical.length,
        };
    } else {
        // 'skip'
        return {
            toUpload: noConflict,
            toOverwrite: [],
            skippedCount: identical.length + conflicts.length,
        };
    }
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

// Create tag without showing toast (for bulk operations like folder drops).
// Returns {tag, error} — tag is the created/existing tag, error is a string if creation failed.
async function createTagSilent(name) {
    try {
        const tag = await window.go.main.App.CreateTag(name);
        return { tag, error: null };
    } catch (error) {
        const msg = error.message || String(error);
        // "already exists" is not a real failure — the tag is usable
        if (msg.includes('already exists')) {
            return { tag: null, error: null };
        }
        console.error('Error creating tag:', error);
        return { tag: null, error: msg };
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
    } catch (error) {
        console.error('Error in bulk add tag:', error);
        showToast('Failed to add tag to clips.');
    }
}

async function bulkRemoveTag(clipIds, tagId) {
    try {
        await window.go.main.App.BulkRemoveTag(clipIds, tagId);
        showToast(`Tag removed from ${clipIds.length} clips.`);
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
