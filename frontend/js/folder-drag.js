// --- Folder Drag-and-Drop Module ---
// Handles drag initiation, drop target resolution, validation, and execution
// for moving clips between folders and reparenting folders in folder mode.

// Highlight class sets for drop targets
const _DROP_HIGHLIGHT_CLASSES = ['border-stone-800', 'bg-stone-50', 'scale-[1.03]'];
const _BREADCRUMB_HIGHLIGHT_CLASSES = ['bg-stone-100', 'ring-1', 'ring-stone-800', 'rounded'];

// Module-level drag state
let _currentHighlightedTarget = null;
let _currentDragImage = null;
let _currentPayload = null;
let _draggedCards = [];

// --- Live region for screen reader announcements ---

function _ensureLiveRegion() {
    let region = document.getElementById('folder-drag-live-region');
    if (!region) {
        region = document.createElement('div');
        region.id = 'folder-drag-live-region';
        region.setAttribute('role', 'status');
        region.setAttribute('aria-live', 'polite');
        region.className = 'sr-only';
        document.body.appendChild(region);
    }
    return region;
}

function _announceDragResult(message) {
    const region = _ensureLiveRegion();
    // Clear then set via requestAnimationFrame so screen readers re-announce
    region.textContent = '';
    requestAnimationFrame(() => {
        region.textContent = message;
    });
}

// --- Custom drag image ---

function _buildDragImage(type, count, label) {
    const container = document.createElement('div');
    container.style.cssText = 'position: fixed; top: -1000px; left: -1000px; z-index: -1; pointer-events: none;';

    if (type === 'clips' && count > 1) {
        // Multi-clip: stacked cards with count badge
        container.innerHTML = `
            <div style="position: relative; padding: 8px 8px 0 0;">
                <div style="position: absolute; top: 6px; left: 6px; width: 120px; height: 40px;"
                     class="bg-stone-100 rounded-md border border-stone-200"></div>
                <div style="position: absolute; top: 3px; left: 3px; width: 120px; height: 40px;"
                     class="bg-stone-100 rounded-md border border-stone-200"></div>
                <div style="position: relative; width: 120px;"
                     class="bg-white rounded-md border border-stone-800 px-3 py-2">
                    <span class="text-xs font-medium text-stone-700 block truncate" style="max-width: 96px;">
                        ${escapeHTML(label)}
                    </span>
                </div>
                <div style="position: absolute; top: -4px; right: -4px;"
                     class="bg-stone-800 text-white text-[10px] font-medium rounded-full w-5 h-5 flex items-center justify-center">
                    ${count}
                </div>
            </div>
        `;
    } else if (type === 'folder') {
        // Folder: white card with folder icon and name
        container.innerHTML = `
            <div class="bg-white rounded-md border border-stone-800 px-3 py-2 flex items-center gap-2">
                <svg class="w-4 h-4 text-stone-600" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M2.25 12.75V12A2.25 2.25 0 0 1 4.5 9.75h15A2.25 2.25 0 0 1 21.75 12v.75m-8.69-6.44-2.12-2.12a1.5 1.5 0 0 0-1.06-.44H4.5A2.25 2.25 0 0 0 2.25 6v12a2.25 2.25 0 0 0 2.25 2.25h15A2.25 2.25 0 0 0 21.75 18V9a2.25 2.25 0 0 0-2.25-2.25h-5.379a1.5 1.5 0 0 1-1.06-.44Z" />
                </svg>
                <span class="text-xs font-medium text-stone-700 truncate" style="max-width: 100px;">
                    ${escapeHTML(label)}
                </span>
            </div>
        `;
    } else {
        // Single clip: simple white card with filename
        container.innerHTML = `
            <div class="bg-white rounded-md border border-stone-800 px-3 py-2">
                <span class="text-xs font-medium text-stone-700 block truncate" style="max-width: 120px;">
                    ${escapeHTML(label)}
                </span>
            </div>
        `;
    }

    document.body.appendChild(container);
    return container;
}

// --- Payload resolution ---

function _resolveDragPayload(card) {
    // Folder card
    if (card.hasAttribute('data-folder')) {
        const folderId = parseInt(card.getAttribute('data-folder'), 10);
        const tag = allTags.find(t => t.id === folderId);
        const shortName = tag ? getShortTagName(tag.name) : 'Folder';
        return { type: 'folder', ids: [folderId], folderId: null, label: shortName };
    }

    // Clip card
    if (card.dataset.id) {
        const clipId = parseInt(card.dataset.id, 10);
        const currentFolderId = activeTagFilters.length > 0
            ? activeTagFilters[activeTagFilters.length - 1]
            : null;

        // Get filename from the card's footer paragraph
        const filenameEl = card.querySelector('.p-2\\.5 p');
        const label = (filenameEl && (filenameEl.getAttribute('title') || filenameEl.textContent)) || 'Clip';

        // Smart selection: if this clip is in the multi-selection, include all selected
        let ids;
        if (selectedIds.has(clipId) && selectedIds.size > 1) {
            ids = Array.from(selectedIds);
        } else {
            ids = [clipId];
        }

        return { type: 'clips', ids, folderId: currentFolderId, label };
    }

    return null;
}

// --- Drop target resolution ---

function _resolveDropTarget(e) {
    let el = e.target;
    while (el && el !== document.body) {
        // Folder card target
        if (el.hasAttribute('data-folder')) {
            return {
                tagId: parseInt(el.getAttribute('data-folder'), 10),
                type: 'folder',
                element: el
            };
        }
        // Breadcrumb / root target
        if (el.hasAttribute('data-drop-target')) {
            const val = el.getAttribute('data-drop-target');
            if (val === 'root') {
                return { tagId: null, type: 'root', element: el };
            }
            return {
                tagId: parseInt(val, 10),
                type: 'breadcrumb',
                element: el
            };
        }
        el = el.parentElement;
    }
    return null;
}

// --- Validation ---

function _isValidDrop(payload, target) {
    if (!payload || !target) return false;

    if (payload.type === 'clips') {
        // Can't drop on current folder
        if (target.tagId === payload.folderId) return false;
        // Can't drop on root if already at root
        if (target.type === 'root' && payload.folderId === null) return false;
        return true;
    }

    if (payload.type === 'folder') {
        const draggedFolderId = payload.ids[0];
        // Can't drop folder on itself
        if (target.tagId === draggedFolderId) return false;
        // Can't drop on root if already a top-level tag
        if (target.type === 'root') {
            const draggedTag = allTags.find(t => t.id === draggedFolderId);
            if (draggedTag && !draggedTag.name.includes('/')) return false;
        }
        // Can't drop on own descendant
        if (target.tagId != null) {
            const draggedTag = allTags.find(t => t.id === draggedFolderId);
            const targetTag = allTags.find(t => t.id === target.tagId);
            if (draggedTag && targetTag && isDescendantOf(targetTag.name, draggedTag.name)) {
                return false;
            }
        }
        return true;
    }

    return false;
}

// --- Drop execution ---

async function _executeDrop(payload, target) {
    try {
        if (payload.type === 'clips') {
            if (target.type === 'root' || target.tagId === null) {
                // Move clips to root: remove current folder tag
                await window.go.main.App.BulkRemoveTag(payload.ids, payload.folderId);
                _announceDragResult(`Moved ${payload.ids.length} clip${payload.ids.length !== 1 ? 's' : ''} to root.`);
            } else {
                // Move clips to a folder: tree exclusivity handles removing old tag
                await window.go.main.App.BulkAddTag(payload.ids, target.tagId);
                const targetTag = allTags.find(t => t.id === target.tagId);
                const targetName = targetTag ? getShortTagName(targetTag.name) : 'folder';
                _announceDragResult(`Moved ${payload.ids.length} clip${payload.ids.length !== 1 ? 's' : ''} to ${targetName}.`);
            }
            selectedIds.clear();
            updateBulkToolbar();
            await loadClips();
        } else if (payload.type === 'folder') {
            const draggedFolderId = payload.ids[0];
            const draggedTag = allTags.find(t => t.id === draggedFolderId);
            if (!draggedTag) throw new Error('Tag not found.');

            const shortName = getShortTagName(draggedTag.name);
            let newName;

            if (target.type === 'root' || target.tagId === null) {
                // Move to root: just the short name
                newName = shortName;
            } else {
                // Move under target folder
                const targetTag = allTags.find(t => t.id === target.tagId);
                if (!targetTag) throw new Error('Target tag not found.');
                newName = targetTag.name + '/' + shortName;
            }

            await window.go.main.App.UpdateTag(draggedTag.id, newName, draggedTag.color);
            _announceDragResult(`Moved folder "${shortName}" to ${target.tagId === null ? 'root' : getShortTagName(allTags.find(t => t.id === target.tagId)?.name || 'folder')}.`);
            await loadTags();
            await loadClips();
        }
    } catch (err) {
        const msg = (err && (err.message || String(err))) || 'Failed to move item.';
        showToast(msg, 'error');
        _announceDragResult('Move failed.');
    }
}

// --- Highlight management ---

function _highlightTarget(target) {
    if (!target || !target.element) return;

    if (target.type === 'folder') {
        target.element.classList.remove('border-stone-200');
        _DROP_HIGHLIGHT_CLASSES.forEach(cls => target.element.classList.add(cls));
    } else {
        // Breadcrumb or root
        _BREADCRUMB_HIGHLIGHT_CLASSES.forEach(cls => target.element.classList.add(cls));
    }
    target.element.setAttribute('aria-dropeffect', 'move');
}

function _unhighlightTarget(target) {
    if (!target || !target.element) return;

    if (target.type === 'folder') {
        _DROP_HIGHLIGHT_CLASSES.forEach(cls => target.element.classList.remove(cls));
        target.element.classList.add('border-stone-200');
    } else {
        _BREADCRUMB_HIGHLIGHT_CLASSES.forEach(cls => target.element.classList.remove(cls));
    }
    target.element.removeAttribute('aria-dropeffect');
}

// --- Cleanup helper ---

function _cleanupDrag() {
    // Remove drag image from DOM
    if (_currentDragImage && _currentDragImage.parentNode) {
        _currentDragImage.parentNode.removeChild(_currentDragImage);
    }
    _currentDragImage = null;

    // Restore opacity on dragged cards
    for (const card of _draggedCards) {
        card.classList.remove('opacity-40');
        card.setAttribute('aria-grabbed', 'false');
    }
    _draggedCards = [];

    // Unhighlight any active target
    if (_currentHighlightedTarget) {
        _unhighlightTarget(_currentHighlightedTarget);
        _currentHighlightedTarget = null;
    }

    _currentPayload = null;
}

// --- Main init function ---

function initFolderDrag() {
    // Only active in folder mode
    if (typeof isFolderMode !== 'function' || !isFolderMode()) return;

    const galleryEl = document.getElementById('gallery');
    const breadcrumbEl = document.getElementById('active-tags-container');
    if (!galleryEl) return;

    // Remove previous listeners by cloning technique is not ideal here since
    // gallery is re-rendered each time. Instead, use a flag to avoid duplicate binding.
    // Since gallery content is rebuilt on each loadClips, the card elements are new,
    // but we use event delegation on the gallery container itself.
    // Guard against duplicate bindings.
    if (galleryEl._folderDragBound) return;
    galleryEl._folderDragBound = true;

    // --- Gallery event delegation ---

    galleryEl.addEventListener('dragstart', (e) => {
        const card = e.target.closest('[data-folder], [data-id]');
        if (!card) return;

        const payload = _resolveDragPayload(card);
        if (!payload) return;

        _currentPayload = payload;

        // Set transfer data
        e.dataTransfer.setData('application/x-folder-drag', JSON.stringify(payload));
        e.dataTransfer.effectAllowed = 'move';

        // Build and set drag image
        _currentDragImage = _buildDragImage(payload.type, payload.ids.length, payload.label);
        e.dataTransfer.setDragImage(_currentDragImage, 0, 0);

        // Mark dragged cards with reduced opacity
        _draggedCards = [];
        if (payload.type === 'clips') {
            for (const id of payload.ids) {
                const el = galleryEl.querySelector(`[data-id="${id}"]`);
                if (el) _draggedCards.push(el);
            }
        } else {
            _draggedCards.push(card);
        }

        requestAnimationFrame(() => {
            for (const c of _draggedCards) {
                c.classList.add('opacity-40');
                c.setAttribute('aria-grabbed', 'true');
            }
        });
    });

    galleryEl.addEventListener('dragend', () => {
        _cleanupDrag();
    });

    galleryEl.addEventListener('dragover', (e) => {
        if (!_currentPayload) return;
        const target = _resolveDropTarget(e);
        if (target && _isValidDrop(_currentPayload, target)) {
            e.preventDefault();
            e.dataTransfer.dropEffect = 'move';
        }
    });

    galleryEl.addEventListener('dragenter', (e) => {
        if (!_currentPayload) return;
        const target = _resolveDropTarget(e);
        if (target && _isValidDrop(_currentPayload, target)) {
            // Unhighlight previous if different element
            if (_currentHighlightedTarget && _currentHighlightedTarget.element !== target.element) {
                _unhighlightTarget(_currentHighlightedTarget);
            }
            _highlightTarget(target);
            _currentHighlightedTarget = target;
        }
    });

    galleryEl.addEventListener('dragleave', (e) => {
        if (!_currentPayload || !_currentHighlightedTarget) return;
        // Use bounding rect check for gallery to handle child element transitions
        const rect = galleryEl.getBoundingClientRect();
        const x = e.clientX;
        const y = e.clientY;
        if (x <= rect.left || x >= rect.right || y <= rect.top || y >= rect.bottom) {
            _unhighlightTarget(_currentHighlightedTarget);
            _currentHighlightedTarget = null;
        }
    });

    galleryEl.addEventListener('drop', async (e) => {
        if (!_currentPayload) return;
        e.preventDefault();
        const target = _resolveDropTarget(e);
        if (_currentHighlightedTarget) {
            _unhighlightTarget(_currentHighlightedTarget);
            _currentHighlightedTarget = null;
        }
        const payload = _currentPayload;
        _cleanupDrag();
        if (target && _isValidDrop(payload, target)) {
            await _executeDrop(payload, target);
        }
    });

    // --- Breadcrumb event delegation ---

    if (breadcrumbEl && !breadcrumbEl._folderDragBound) {
        breadcrumbEl._folderDragBound = true;

        breadcrumbEl.addEventListener('dragover', (e) => {
            if (!_currentPayload) return;
            const target = _resolveDropTarget(e);
            if (target && _isValidDrop(_currentPayload, target)) {
                e.preventDefault();
                e.dataTransfer.dropEffect = 'move';
            }
        });

        breadcrumbEl.addEventListener('dragenter', (e) => {
            if (!_currentPayload) return;
            const target = _resolveDropTarget(e);
            if (target && _isValidDrop(_currentPayload, target)) {
                if (_currentHighlightedTarget && _currentHighlightedTarget.element !== target.element) {
                    _unhighlightTarget(_currentHighlightedTarget);
                }
                _highlightTarget(target);
                _currentHighlightedTarget = target;
            }
        });

        breadcrumbEl.addEventListener('dragleave', (e) => {
            if (!_currentPayload || !_currentHighlightedTarget) return;
            // For breadcrumb, use contains check
            if (!breadcrumbEl.contains(e.relatedTarget)) {
                _unhighlightTarget(_currentHighlightedTarget);
                _currentHighlightedTarget = null;
            }
        });

        breadcrumbEl.addEventListener('drop', async (e) => {
            if (!_currentPayload) return;
            e.preventDefault();
            const target = _resolveDropTarget(e);
            if (_currentHighlightedTarget) {
                _unhighlightTarget(_currentHighlightedTarget);
                _currentHighlightedTarget = null;
            }
            const payload = _currentPayload;
            _cleanupDrag();
            if (target && _isValidDrop(payload, target)) {
                await _executeDrop(payload, target);
            }
        });

        breadcrumbEl.addEventListener('dragend', () => {
            _cleanupDrag();
        });
    }
}
