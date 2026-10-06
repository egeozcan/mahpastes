// Wails API - replaces fetch-based api.js
// All methods call Go bindings via window.go.main.App.*

let _pendingFocusAfterLoad = false;

// Generation counter, same idiom as renderFolderCards. Deep search made
// loadClips keystroke-driven, so two runs can now be in flight at once: a slow
// content scan started first can return after a later, narrower one and repaint
// the gallery with stale results. Each run captures its generation and bails as
// soon as a newer one has started.
let _clipLoadGen = 0;

// Gallery listings are paged: the first CLIP_PAGE_SIZE clips render, and
// "Load more" under the gallery appends the next page.
const CLIP_PAGE_SIZE = 50;
// What is on screen: the request it came from (minus paging), how many clips
// are loaded, and how many the listing holds in all.
let _galleryView = { key: null, request: null, loaded: 0, total: 0, hasMore: false };

// Fetch clips from the start of a listing until `want` are loaded or it runs out.
// More than 200 takes several requests; rows that shift between them (an
// upload, a delete) would otherwise render one clip twice, so each id is kept
// once. The offset advances by rows fetched, not rows kept.
async function fetchClipPages(request, want) {
    const clips = [];
    const seen = new Set();
    let fetched = 0;
    let total = 0;
    let hasMore = false;
    do {
        const page = await window.go.main.App.ListClipsPage({
            ...request,
            offset: fetched,
            limit: Math.min(200, want - fetched),
        });
        const got = (page && page.clips) || [];
        fetched += got.length;
        for (const clip of got) {
            const id = Number(clip.id);
            if (seen.has(id)) continue;
            seen.add(id);
            clips.push(clip);
        }
        total = page ? page.total : clips.length;
        hasMore = !!(page && page.has_more);
        if (got.length === 0) break;
    } while (hasMore && fetched < want);
    return { clips, total, hasMore };
}

// The gallery's footer count: clip cards only (folder cards are not clips),
// "50 of 120 clips" while more pages exist, and the visible subset while the
// plain search filter is hiding cards.
function renderGalleryCount() {
    const cards = Array.from(gallery.querySelectorAll(':scope > li[data-id]'));
    const loaded = cards.length;
    const visible = cards.filter(c => c.style.display !== 'none').length;
    const filtering = visible !== loaded;
    if (filtering) {
        updateClipCount(visible, _galleryView.hasMore ? null : loaded, { filtering: true, loaded });
    } else if (_galleryView.hasMore) {
        updateClipCount(loaded, _galleryView.total);
    } else {
        updateClipCount(loaded);
    }
}

// Empty and error states live beside the gallery, never inside it: the
// gallery's children are cards, and everything that walks it assumes so.
function showGalleryStatus(message, kind = 'empty') {
    const el = document.getElementById('gallery-status');
    if (!el) return;
    el.textContent = message;
    el.classList.toggle('text-stone-400', kind !== 'error');
    el.classList.toggle('text-red-500', kind === 'error');
    el.classList.remove('hidden');
}

function hideGalleryStatus() {
    const el = document.getElementById('gallery-status');
    if (!el) return;
    el.classList.add('hidden');
    el.textContent = '';
}

function renderLoadMore() {
    const wrap = document.getElementById('gallery-load-more');
    if (!wrap) return;
    const { hasMore, loaded, total } = _galleryView;
    wrap.classList.toggle('hidden', !hasMore);
    const btn = document.getElementById('gallery-load-more-btn');
    if (btn) {
        btn.disabled = false;
        btn.textContent = 'Load more';
        btn.setAttribute('aria-label', `Load more clips (${loaded} of ${total} shown)`);
    }
}

// Where focus was in the gallery before a reload, so a reload of the same
// view (delete, archive, a background refresh) can put it back instead of
// dropping it to <body>, where the arrow keys stop working.
function captureGalleryFocus() {
    const active = document.activeElement;
    const inGallery = active && active !== gallery && gallery.contains(active);
    if (!inGallery) return null;
    const card = active.closest('#gallery > li');
    if (!card) return null;
    const items = Array.from(gallery.querySelectorAll(':scope > li'));
    return { id: card.dataset.id || null, folder: card.dataset.folder || null, index: items.indexOf(card) };
}

function restoreGalleryFocus(snapshot) {
    if (!snapshot) return;
    // Only reclaim focus nobody else has taken in the meantime.
    const active = document.activeElement;
    if (active && active !== document.body && active !== gallery && !gallery.contains(active)) return;
    const items = Array.from(gallery.querySelectorAll(':scope > li'))
        .filter(el => el.style.display !== 'none');
    if (items.length === 0) return;
    let target = null;
    if (snapshot.id) target = items.find(el => el.dataset.id === snapshot.id);
    if (!target && snapshot.folder) target = items.find(el => el.dataset.folder === snapshot.folder);
    if (!target) target = items[Math.min(Math.max(snapshot.index, 0), items.length - 1)];
    const rover = window.__galleryRover;
    if (rover) {
        const idx = rover.getItems().indexOf(target);
        if (idx >= 0) rover.setActiveIndex(idx);
    }
    target.focus({ preventScroll: true });
}

async function loadClips({ focusFirst = false } = {}) {
    _pendingFocusAfterLoad = focusFirst;
    const myGen = ++_clipLoadGen;
    // Any in-flight standalone folder-card render is superseded by this one.
    if (typeof _folderRenderGen !== 'undefined') _folderRenderGen++;
    // Focus is read twice: here, and again just before the gallery is cleared
    // (see below). The later reading wins when it finds a card.
    const entryFocusSnapshot = captureGalleryFocus();
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

        // A deep search (contents and/or hidden clips) is answered by the
        // database: neither option can be resolved from the cards on screen.
        const deepSearch = typeof isDeepSearchActive === 'function' && isDeepSearchActive();
        const searchOptions = typeof getSearchOptions === 'function' ? getSearchOptions() : { inContent: false, includeHidden: false };

        const request = {
            mode: 'all',
            archived: !!isViewingArchive,
            tag_ids: [...activeTagFilters],
            hidden_tag_ids: effectiveHidden,
            folder_tag_id: 0,
            query: '',
            search_content: false,
            sort_field: currentSortField,
            sort_dir: currentSortDir,
        };
        if (deepSearch) {
            // "Show hidden clips" is expressed by asking for no hidden tags at all.
            request.mode = 'search';
            request.hidden_tag_ids = searchOptions.includeHidden ? [] : effectiveHidden;
            request.query = getSearchQuery();
            request.search_content = !!searchOptions.inContent;
        } else if (isFolderMode() && activeTagFilters.length > 0) {
            // Clips tagged directly with this folder's tag; those with a
            // descendant tag belong in subfolders. Hidden tags are not applied:
            // inside a folder, hiding only dims folder cards.
            request.mode = 'folder';
            request.folder_tag_id = activeTagFilters[activeTagFilters.length - 1];
            request.tag_ids = [];
            request.hidden_tag_ids = [];
        } else if (isFolderMode()) {
            // Root level folder mode: only untagged clips alongside folder cards
            request.mode = 'untagged';
            request.tag_ids = [];
        }

        // A reload of the view already on screen keeps as many clips as were
        // loaded (and the focused card); any other view starts at one page.
        const viewKey = JSON.stringify(request);
        const sameView = viewKey === _galleryView.key;
        const want = sameView ? Math.max(CLIP_PAGE_SIZE, _galleryView.loaded) : CLIP_PAGE_SIZE;
        const isStale = () => myGen !== _clipLoadGen;

        // Fetch the clips and the folder cards together, before touching the
        // gallery, so a reload never shows an empty frame.
        const [page, folderCards] = await Promise.all([
            fetchClipPages(request, want),
            isFolderMode() ? buildFolderCards(isStale) : Promise.resolve([]),
        ]);
        if (isStale() || folderCards === null) return;

        // Where focus is now, just before the gallery is cleared: a delete
        // confirmed in a dialog starts the reload while focus is still on the
        // dialog's button, and only returns it to the card as the dialog
        // closes. When focus has left the gallery since (a card removed ahead
        // of the reload), the reading taken at the start still applies.
        const focusSnapshot = captureGalleryFocus() || entryFocusSnapshot;

        if (typeof clearPreparedDragState === 'function') {
            clearPreparedDragState();
        }
        if (typeof setGalleryShowsSearchResults === 'function') setGalleryShowsSearchResults(deepSearch);
        if (typeof setGalleryWaivedHiddenTags === 'function') {
            setGalleryWaivedHiddenTags(deepSearch && searchOptions.includeHidden ? effectiveHidden : []);
        }

        gallery.innerHTML = '';
        clearRenderedClips();
        hideGalleryStatus();
        _galleryView = { key: viewKey, request, loaded: 0, total: page.total, hasMore: page.hasMore };

        for (const card of folderCards) gallery.appendChild(card);
        if (isFolderMode() && typeof initFolderDrag === 'function') initFolderDrag();

        for (const clip of page.clips) {
            if (isStale()) return;
            const card = await createClipCard(clip);
            if (isStale()) {
                card?.remove();
                return;
            }
        }
        _galleryView.loaded = page.clips.length;

        // The selection survives a reload; only clips no longer listed drop out.
        const rendered = new Set(page.clips.map(c => Number(c.id)));
        for (const id of Array.from(selectedIds)) {
            if (!rendered.has(id)) selectedIds.delete(id);
        }
        const checkboxes = Array.from(gallery.querySelectorAll('.clip-checkbox'));
        selectAllCheckbox.checked = selectedIds.size > 0 && checkboxes.length > 0 && checkboxes.every(cb => cb.checked);
        updateBulkToolbar();

        if (page.clips.length === 0 && folderCards.length === 0) {
            let emptyMsg;
            if (deepSearch) {
                emptyMsg = 'No clips match your search.';
            } else if (activeTagFilters.length > 0) {
                emptyMsg = 'No clips match the selected tags.';
            } else if (isViewingArchive) {
                emptyMsg = 'No archived clips.';
            } else {
                emptyMsg = 'No active clips. Paste or drop something!';
            }
            showGalleryStatus(emptyMsg);
        }
        if (typeof applySearchFilter === 'function') applySearchFilter();
        renderGalleryCount();
        renderLoadMore();
        // Nothing is being withheld when the search was told to include hidden clips.
        updateHiddenClipsNote(deepSearch && searchOptions.includeHidden ? [] : effectiveHidden);
        window.LightboxController?.setClips(getVisibleMediaClips());

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
        } else if (sameView) {
            restoreGalleryFocus(focusSnapshot);
        }
    } catch (error) {
        console.error('Error loading clips:', error);
        // Same rule as the success path: a run that has been superseded must not
        // touch the gallery. Without this a slow search that fails late replaces
        // the results of the newer search that already rendered.
        if (myGen === _clipLoadGen) {
            gallery.innerHTML = '';
            clearRenderedClips();
            _galleryView = { key: null, request: null, loaded: 0, total: 0, hasMore: false };
            // Nothing is on screen, so nothing may stay selected: bulk actions
            // would otherwise act on clips the user can no longer see.
            selectedIds.clear();
            selectAllCheckbox.checked = false;
            updateBulkToolbar();
            renderLoadMore();
            updateClipCount(0);
            showGalleryStatus('Error loading clips.', 'error');
            clearHiddenClipsNote();
        }
    } finally {
        // Render-completion signal. Several callers (folder-mode toggle, folder
        // navigation) fire loadClips() without awaiting it from a sync click
        // handler, so there is otherwise no way to tell that the gallery has
        // finished re-rendering. Tests wait for this to advance. A superseded
        // run never touched the gallery, so it must not claim a render.
        if (myGen === _clipLoadGen) {
            window.__galleryRenderSeq = (window.__galleryRenderSeq || 0) + 1;
        }
    }
}

// Append the next page of the listing on screen.
async function loadMoreClips() {
    const view = _galleryView;
    if (!view.hasMore || !view.request) return;
    const myGen = _clipLoadGen;
    const btn = document.getElementById('gallery-load-more-btn');
    const buttonHadFocus = !!btn && document.activeElement === btn;
    if (btn) {
        btn.disabled = true;
        btn.textContent = 'Loading…';
    }
    const firstNewIndex = gallery.querySelectorAll(':scope > li').length;
    let reloaded = false;
    try {
        const page = await window.go.main.App.ListClipsPage({
            ...view.request,
            offset: view.loaded,
            limit: CLIP_PAGE_SIZE,
        });
        // A reload or navigation since the click owns the gallery now.
        if (myGen !== _clipLoadGen || view !== _galleryView) return;

        // The listing changed size behind the gallery (the expiry reaper, a
        // REST client, a plugin): offsets no longer line up with what is on
        // screen, so appending this page positionally would skip clips or
        // keep deleted ones. Reload the same view one page further instead.
        if (page && page.total !== view.total) {
            reloaded = true;
            view.loaded += CLIP_PAGE_SIZE;
            await loadClips();
        } else {
            for (const clip of (page && page.clips) || []) {
                // Clips added since the first page shift offsets; never show one twice.
                if (gallery.querySelector(`:scope > li[data-id="${Number(clip.id)}"]`)) continue;
                await createClipCard(clip);
                if (myGen !== _clipLoadGen) return;
            }
            view.loaded += ((page && page.clips) || []).length;
            view.total = page ? page.total : view.total;
            view.hasMore = !!(page && page.has_more);
            if (typeof applySearchFilter === 'function') applySearchFilter();
            renderGalleryCount();
            window.LightboxController?.setClips(getVisibleMediaClips());
            if (window.__galleryRover) window.__galleryRover.update();
            // New cards arrive unselected, so "select all" no longer holds.
            const checkboxes = Array.from(gallery.querySelectorAll('.clip-checkbox'));
            selectAllCheckbox.checked = selectedIds.size > 0 && checkboxes.length > 0 && checkboxes.every(cb => cb.checked);
            updateBulkToolbar();
        }
    } catch (error) {
        console.error('Error loading more clips:', error);
        showToast('Failed to load more clips: ' + errText(error), 'error');
    } finally {
        const current = _galleryView;
        if (view === current || reloaded) {
            renderLoadMore();
            // Disabling the button during the fetch dropped its focus to <body>.
            // With more pages left, give it back so Enter keeps loading; the last
            // page hides the button, so hand focus to the first card it brought in.
            const active = document.activeElement;
            if (buttonHadFocus && (active === btn || active === document.body || !active)) {
                const nextBtn = document.getElementById('gallery-load-more-btn');
                if (current.hasMore && nextBtn) nextBtn.focus();
                else focusGalleryItem(firstNewIndex);
            }
        }
    }
}

function focusGalleryItem(index) {
    const items = Array.from(gallery.querySelectorAll(':scope > li')).filter(el => el.style.display !== 'none');
    if (items.length === 0) return;
    const target = items[Math.min(Math.max(index, 0), items.length - 1)];
    const rover = window.__galleryRover;
    if (rover) {
        const idx = rover.getItems().indexOf(target);
        if (idx >= 0) rover.setActiveIndex(idx);
    }
    target.focus();
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
            invalidateClipMedia(clipID);
        }

        // Upload new files
        const duplicatesBefore = window.__duplicateUploadEvents || 0;
        if (result.toUpload.length > 0) {
            await window.go.main.App.UploadFiles(result.toUpload, minutes, autoTagID);
        }
        // Content that already exists elsewhere in the library raises a
        // clip:duplicate toast; this summary would immediately replace it.
        const duplicates = (window.__duplicateUploadEvents || 0) - duplicatesBefore;

        const totalProcessed = result.toUpload.length + result.toOverwrite.length;
        if (totalProcessed > 0 || result.skippedCount > 0) {
            const parts = [];
            if (result.toUpload.length > 0) {
                parts.push(`${result.toUpload.length} uploaded` + (duplicates > 0
                    ? ` (${duplicates} duplicate${duplicates === 1 ? '' : 's'} of existing clips)`
                    : ''));
            }
            if (result.toOverwrite.length > 0) parts.push(`${result.toOverwrite.length} overwritten`);
            if (result.skippedCount > 0) parts.push(`${result.skippedCount} skipped`);
            showToast(parts.join(', ') + '.');
        }

        if (!isViewingArchive) {
            loadClips();
        }
    } catch (error) {
        console.error('Error uploading:', error);
        showToast('Upload failed: ' + errText(error), 'error');
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
            showToast('Failed to delete clip.', 'error');
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
        showToast('Failed to change archive status.', 'error');
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
        showToast('Failed to save temp file.', 'error');
    }
}

async function copyFileToClipboard(id) {
    try {
        await window.go.main.ClipboardService.CopyFileToClipboard(id);
        showToast('File copied to clipboard!');
    } catch (error) {
        console.error('Error copying file to clipboard:', error);
        showToast('Failed to copy file.', 'error');
    }
}

async function copyClipContents(id) {
    try {
        await window.go.main.ClipboardService.CopyClipContents(id);
        showToast('Contents copied to clipboard!');
    } catch (error) {
        console.error('Error copying contents:', error);
        showToast('Failed to copy contents.', 'error');
    }
}

// Mints a revocable public share link for a clip and copies the full URL to the
// clipboard. The link is unauthenticated and downloadable by anyone holding it;
// revoke it from the CLI (`mp link revoke <id>`) or LinkService at any time.
async function createAndCopyPublicLink(id) {
    try {
        const link = await window.go.main.LinkService.CreateShareLink(id, '', 0, 0);
        const path = (link && link.path) || (link && link.token ? '/s/' + link.token : '');
        if (!path) {
            throw new Error('Invalid response');
        }
        copyToClipboard(window.location.origin + path);
    } catch (error) {
        console.error('Error creating public link:', error);
        showToast('Failed to create public link.', 'error');
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
            showToast('Failed to delete temp files.', 'error');
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
            showToast('Bulk delete failed.', 'error');
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
        showToast(isViewingArchive ? 'Bulk restore failed.' : 'Bulk archive failed.', 'error');
    }
}

async function bulkDownload() {
    if (selectedIds.size === 0) return;
    try {
        // Resolves to the path written, or "" when the save dialog was
        // cancelled — only a written file earns the success toast.
        const savedPath = await window.go.main.App.BulkDownloadToFile(Array.from(selectedIds));
        if (savedPath) {
            showToast('Download complete.');
        }
    } catch (error) {
        console.error('Error in bulk download:', error);
        showToast('Bulk download failed: ' + errText(error), 'error');
    }
}

async function bulkCopyFiles() {
    if (selectedIds.size === 0) return;
    try {
        await window.go.main.ClipboardService.BulkCopyFilesToClipboard(Array.from(selectedIds));
        showToast(`${selectedIds.size} file${selectedIds.size > 1 ? 's' : ''} copied to clipboard!`);
    } catch (error) {
        console.error('Error copying files to clipboard:', error);
        showToast('Failed to copy files.', 'error');
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

// Get a clip for the text editor: filename, content type, bytes, and UTF-8
// validity, all from one read.
//
// Desktop GetClipData already reads all three columns in a single row scan and
// reports valid_utf8/data_encoding, so it satisfies the contract as-is. Server
// mode needs the dedicated /text endpoint: GetClipData there returns raw base64
// with an empty filename, and composing metadata and bytes from two requests
// would let a concurrent update pair one clip's metadata with another revision's
// bytes.
async function getClipText(id) {
    try {
        if (typeof window.go?.main?.App?.GetClipText === 'function') {
            return await window.go.main.App.GetClipText(id);
        }
        return await window.go.main.App.GetClipData(id);
    } catch (error) {
        console.error('Error getting clip text:', error);
        throw error;
    }
}

// Save clip to file using native dialog
async function saveClipToFile(id) {
    try {
        // Resolves to the path written, or "" when the dialog was cancelled.
        await window.go.main.App.SaveClipToFile(id);
    } catch (error) {
        console.error('Error saving clip to file:', error);
        showToast('Failed to save file: ' + errText(error), 'error');
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
        showToast(errText(error) || 'Failed to create tag.', 'error');
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
        const msg = errText(error);
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
        showToast(errText(error) || 'Failed to update tag.', 'error');
    }
}

async function deleteTag(id) {
    try {
        await window.go.main.App.DeleteTag(id);
        showToast('Tag deleted.');
    } catch (error) {
        console.error('Error deleting tag:', error);
        showToast('Failed to delete tag.', 'error');
    }
}

async function addTagToClip(clipId, tagId) {
    try {
        await window.go.main.App.AddTagToClip(clipId, tagId);
    } catch (error) {
        console.error('Error adding tag to clip:', error);
        showToast('Failed to add tag.', 'error');
    }
}

async function removeTagFromClip(clipId, tagId) {
    try {
        await window.go.main.App.RemoveTagFromClip(clipId, tagId);
    } catch (error) {
        console.error('Error removing tag from clip:', error);
        showToast('Failed to remove tag.', 'error');
    }
}

async function bulkAddTag(clipIds, tagId) {
    try {
        await window.go.main.App.BulkAddTag(clipIds, tagId);
        showToast(`Tag added to ${clipIds.length} clips.`);
    } catch (error) {
        console.error('Error in bulk add tag:', error);
        showToast('Failed to add tag to clips.', 'error');
    }
}

async function bulkRemoveTag(clipIds, tagId) {
    try {
        await window.go.main.App.BulkRemoveTag(clipIds, tagId);
        showToast(`Tag removed from ${clipIds.length} clips.`);
    } catch (error) {
        console.error('Error in bulk remove tag:', error);
        showToast('Failed to remove tag from clips.', 'error');
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

async function getDescendantClipCount(tagId, archived) {
    try {
        return await window.go.main.App.GetDescendantClipCount(tagId, !!archived);
    } catch (error) {
        console.error('Error getting descendant clip count:', error);
        return 0;
    }
}

// Clip counts for many folder cards in one call: { [tagId]: count }. A failure
// reads as zero for every card, as getDescendantClipCount's does for one.
async function getDescendantClipCounts(tagIds, archived) {
    try {
        return (await window.go.main.App.GetDescendantClipCounts(tagIds, !!archived)) || {};
    } catch (error) {
        console.error('Error getting descendant clip counts:', error);
        return {};
    }
}

// Note under the gallery for clips the tag filter matched but hidden tags withheld.
// Bumped per render so a slow response from a superseded load cannot overwrite a newer note.
let _hiddenNoteGen = 0;

function clearHiddenClipsNote() {
    _hiddenNoteGen++;
    const note = document.getElementById('hidden-clips-note');
    if (!note) return;
    note.classList.add('hidden');
    note.textContent = '';
    note.removeAttribute('title');
}

async function updateHiddenClipsNote(hiddenTagIds) {
    const note = document.getElementById('hidden-clips-note');
    if (!note) return;

    // Only meaningful while filtering: folder mode ignores hidden tags entirely,
    // and with no filter active there is no "other tag" to explain.
    if (isFolderMode() || activeTagFilters.length === 0) {
        clearHiddenClipsNote();
        return;
    }

    const myGen = ++_hiddenNoteGen;
    let info;
    try {
        info = await window.go.main.App.GetHiddenClipInfo(isViewingArchive, activeTagFilters, hiddenTagIds || []);
    } catch (error) {
        console.error('Error getting hidden clip info:', error);
        return;
    }
    if (myGen !== _hiddenNoteGen) return;

    const count = info && info.count ? info.count : 0;
    if (count === 0) {
        note.classList.add('hidden');
        note.textContent = '';
        note.removeAttribute('title');
        return;
    }

    const tags = (info.tags || []);
    const shown = tags.slice(0, 3).join(', ');
    const more = tags.length > 3 ? `, +${tags.length - 3} more` : '';
    const because = tags.length > 0 ? ` (${shown}${more})` : '';
    note.textContent = `${count} ${count === 1 ? 'clip' : 'clips'} hidden by other tags${because}`;
    if (tags.length > 0) note.title = `Hidden by: ${tags.join(', ')}`;
    note.classList.remove('hidden');
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
        showToast('Failed to set expiration.', 'error');
    }
}

async function cancelExpiration(id) {
    try {
        await window.go.main.App.CancelExpiration(id);
        showToast('Expiration canceled.');
        loadClips();
    } catch (error) {
        console.error('Error canceling expiration:', error);
        showToast('Failed to cancel expiration.', 'error');
    }
}

async function bulkSetExpiration(ids, minutes) {
    try {
        await window.go.main.App.BulkSetExpiration(ids, minutes);
        showToast(`Expiration set on ${ids.length} clips.`);
        selectedIds.clear();
        updateBulkToolbar();
        loadClips();
    } catch (error) {
        // Keep the selection so the user can retry without re-picking clips.
        console.error('Error in bulk set expiration:', error);
        showToast('Failed to set expiration: ' + errText(error), 'error');
    }
}

async function bulkCancelExpiration(ids) {
    try {
        await window.go.main.App.BulkCancelExpiration(ids);
        showToast(`Expiration canceled on ${ids.length} clips.`);
        selectedIds.clear();
        updateBulkToolbar();
        loadClips();
    } catch (error) {
        console.error('Error in bulk cancel expiration:', error);
        showToast('Failed to cancel expiration: ' + errText(error), 'error');
    }
}

async function wailsGetServeStatus() {
    if (!window.go || !window.go.main || !window.go.main.ServeService) return [];
    try {
        return (await window.go.main.ServeService.GetServeStatus()) || [];
    } catch (e) {
        console.error('GetServeStatus failed:', e);
        return [];
    }
}

async function wailsGetShareStatus() {
    if (!window.go || !window.go.main || !window.go.main.ShareService) return { shares: [], follows: [] };
    try {
        return (await window.go.main.ShareService.GetShareStatus()) || { shares: [], follows: [] };
    } catch (e) {
        console.error('GetShareStatus failed:', e);
        return { shares: [], follows: [] };
    }
}
