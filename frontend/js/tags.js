// --- Tag UI Management ---

// Roving tabindex for tag filter dropdown
let tagFilterRover = null;

// Tracks the tag ID of the current folder-view, so we can re-resolve the
// path after any tag change (rename / delete / merge).
let currentFolderTagID = null;

/** Set by navigateToFolder — called whenever the user enters a folder. */
function rememberCurrentFolder(tagID) {
    currentFolderTagID = tagID;
}
window.rememberCurrentFolder = rememberCurrentFolder;

// Constants
const MAX_TAG_NAME_LENGTH = 50;

// Tag filter dropdown element references
const tagFilterBtn = document.getElementById('tag-filter-btn');
const tagFilterDropdown = document.getElementById('tag-filter-dropdown');
const tagFilterList = document.getElementById('tag-filter-list');
const tagFilterBadge = document.getElementById('tag-filter-badge');
const activeTagsContainer = document.getElementById('active-tags-container');
const clearTagFiltersBtn = document.getElementById('clear-tag-filters');

// Bulk tag button
const bulkTagBtn = document.getElementById('bulk-tag-btn');

// Tag popover elements
const tagPopover = document.getElementById('tag-popover');
const tagPopoverList = document.getElementById('tag-popover-list');
const createTagInput = document.getElementById('create-tag-input');
const createTagBtn = document.getElementById('create-tag-btn');

// Tag state (scoped to this module)
const tagState = {
    currentTaggingClipId: null,
    tagPopoverMode: 'single' // 'single' or 'bulk'
};

// Legacy accessors for compatibility
let currentTaggingClipId = null;
let tagPopoverMode = 'single';

// --- Tag Filter Dropdown ---

function renderTagFilterDropdown() {
    if (!tagFilterList) return;
    tagFilterList.innerHTML = '';

    if (allTags.length === 0) {
        tagFilterList.innerHTML = '<p class="text-stone-400 text-xs px-3 py-2">No tags yet</p>';
        return;
    }

    const tree = buildTagTree(allTags);
    const hiddenTagIds = getHiddenTags();

    function renderNode(node, depth, parentHidden) {
        const { tag, children } = node;
        const isActive = activeTagFilters.includes(tag.id);
        const isHidden = hiddenTagIds.includes(tag.id) || parentHidden;
        const displayName = depth === 0 ? tag.name : getShortTagName(tag.name);

        const item = document.createElement('label');
        item.className = `flex items-center gap-2 py-1.5 hover:bg-stone-100 cursor-pointer transition-colors${isHidden ? ' opacity-50' : ''}`;
        item.style.paddingLeft = `${12 + depth * 16}px`;
        item.style.paddingRight = '12px';
        item.innerHTML = `
            <input type="checkbox"
                   data-testid="tag-checkbox-${tag.name}"
                   class="rounded border-stone-300 text-stone-600 focus:ring-stone-500"
                   ${isActive ? 'checked' : ''}>
            <span class="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-medium text-white"
                  style="background-color: ${tag.color}">
                ${escapeHTML(displayName)}
            </span>
            ${isHidden ? '<svg class="w-3 h-3 text-stone-400 ml-auto flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.878 9.878L3 3m6.878 6.878L21 21"/></svg>' : `<span class="text-stone-400 text-[10px] ml-auto">${tag.count}</span>`}
        `;

        item.querySelector('input').addEventListener('change', () => toggleTagFilter(tag.id));

        // Right-click → context menu with merge action.
        item.addEventListener('contextmenu', (e) => {
            e.preventDefault();
            e.stopPropagation();
            if (typeof ContextMenu === 'undefined' || !ContextMenu.open) return;
            const menuItems = [
                {
                    id: 'merge-into',
                    label: 'Merge into\u2026',
                    iconHtml: '<svg class="w-3 h-3 opacity-60" stroke="currentColor" stroke-width="1.5" fill="none" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M7 7h10v10M7 7l10 10"/></svg>',
                },
            ];
            const capturedTag = tag;
            ContextMenu.open(menuItems, null, item, (actionId) => {
                if (actionId === 'merge-into' && typeof window.openMergeTagModal === 'function') {
                    window.openMergeTagModal(capturedTag.id, capturedTag.name);
                }
            });
        });

        tagFilterList.appendChild(item);

        // Always render children — even for hidden parents, subtags should remain
        // accessible in the dropdown (they inherit the dimmed style).
        for (const child of children) {
            renderNode(child, depth + 1, isHidden);
        }
    }

    for (const root of tree) {
        renderNode(root, 0, false);
    }

    // Initialize roving tabindex for tag filter labels
    if (tagFilterRover) tagFilterRover.destroy();
    if (tagFilterList && tagFilterList.children.length > 0 && tagFilterList.querySelector('label')) {
        tagFilterRover = RovingTabindex.create({
            container: tagFilterList,
            itemSelector: 'label',
            orientation: 'vertical',
            wrap: false,
            onActivate: (label) => {
                const checkbox = label.querySelector('input[type="checkbox"]');
                if (checkbox) {
                    checkbox.checked = !checkbox.checked;
                    checkbox.dispatchEvent(new Event('change', { bubbles: true }));
                }
            },
        });
    }
}

function toggleTagFilter(tagId) {
    const idx = activeTagFilters.indexOf(tagId);
    if (idx === -1) {
        // Adding a tag filter
        if (typeof isFolderMode === 'function' && isFolderMode() && typeof navigateToFolder === 'function') {
            // In folder mode, navigate to this tag's folder instead of appending
            navigateToFolder(tagId);
            return;
        }
        activeTagFilters.push(tagId);
    } else {
        if (typeof isFolderMode === 'function' && isFolderMode()) {
            // In folder mode, unchecking a tag navigates up to its parent
            const tag = allTags.find(t => t.id === tagId);
            if (tag) {
                const parentName = getParentTagName(tag.name);
                activeTagFilters.length = 0;
                if (parentName) {
                    let p = '';
                    for (const s of parentName.split('/')) {
                        p = p ? p + '/' + s : s;
                        const pt = allTags.find(t => t.name === p);
                        if (pt) activeTagFilters.push(pt.id);
                    }
                }
                updateActiveTagsDisplay();
                renderTagFilterDropdown();
                restoreTagFilterFocus(tagId);
                loadClips();
                return;
            }
        }
        activeTagFilters.splice(idx, 1);
    }
    updateActiveTagsDisplay();
    renderTagFilterDropdown();
    restoreTagFilterFocus(tagId);
    loadClips();
}

function restoreTagFilterFocus(tagId) {
    if (!tagFilterRover) return;
    // Find the tag's name to locate its checkbox in the re-rendered list
    const tag = allTags.find(t => t.id === tagId);
    if (!tag) return;
    const checkbox = tagFilterList.querySelector(`[data-testid="tag-checkbox-${tag.name}"]`);
    if (!checkbox) return;
    const label = checkbox.closest('label');
    if (!label) return;
    const items = tagFilterRover.getItems();
    const idx = items.indexOf(label);
    if (idx >= 0) {
        tagFilterRover.setActiveIndex(idx);
        label.focus();
    }
}

function updateActiveTagsDisplay() {
    // Update badge
    if (tagFilterBadge) {
        if (activeTagFilters.length > 0) {
            tagFilterBadge.textContent = activeTagFilters.length;
            tagFilterBadge.classList.remove('hidden');
        } else {
            tagFilterBadge.classList.add('hidden');
        }
    }

    // Update active tags pills
    if (activeTagsContainer) {
        activeTagsContainer.innerHTML = '';

        if (activeTagFilters.length === 0) {
            activeTagsContainer.classList.add('hidden');
        } else {
            activeTagsContainer.classList.remove('hidden');

            // Add label
            const label = document.createElement('span');
            label.className = 'text-[10px] font-medium text-stone-400 uppercase tracking-wide';
            label.textContent = 'Filtering:';
            activeTagsContainer.appendChild(label);

            if (typeof isFolderMode === 'function' && isFolderMode()) {
                // Folder mode: breadcrumb-style navigation
                const deepestTag = allTags.find(t => t.id === activeTagFilters[activeTagFilters.length - 1]);
                if (deepestTag) {
                    // Home icon for root navigation
                    const homeBtn = document.createElement('button');
                    homeBtn.className = 'p-1 rounded transition-colors text-stone-400 hover:text-stone-600 hover:bg-stone-100';
                    homeBtn.setAttribute('data-testid', 'folder-home-icon');
                    homeBtn.setAttribute('data-drop-target', 'root');
                    homeBtn.setAttribute('aria-label', 'Root folder');
                    homeBtn.innerHTML = `
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" d="m2.25 12 8.954-8.955c.44-.439 1.152-.439 1.591 0L21.75 12M4.5 9.75v10.125c0 .621.504 1.125 1.125 1.125H9.75v-4.875c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125V21h4.125c.621 0 1.125-.504 1.125-1.125V9.75M8.25 21h8.25" />
                        </svg>
                    `;
                    homeBtn.addEventListener('click', () => {
                        activeTagFilters.length = 0;
                        updateActiveTagsDisplay();
                        renderTagFilterDropdown();
                        loadClips();
                    });
                    activeTagsContainer.appendChild(homeBtn);

                    // Separator after home
                    const homeSep = document.createElement('span');
                    homeSep.className = 'text-stone-300 text-xs';
                    homeSep.textContent = '/';
                    activeTagsContainer.appendChild(homeSep);

                    const segments = deepestTag.name.split('/');
                    let path = '';
                    for (let i = 0; i < segments.length; i++) {
                        path = i === 0 ? segments[i] : path + '/' + segments[i];
                        const segTag = allTags.find(t => t.name === path);
                        if (!segTag) continue;

                        // Add separator
                        if (i > 0) {
                            const sep = document.createElement('span');
                            sep.className = 'text-stone-300 text-xs';
                            sep.textContent = '/';
                            activeTagsContainer.appendChild(sep);
                        }

                        const pill = document.createElement('span');
                        pill.className = 'inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-medium text-white';
                        pill.style.backgroundColor = segTag.color;
                        pill.dataset.testid = `tag-pill-${segTag.name}`;
                        pill.setAttribute('data-drop-target', String(segTag.id));
                        pill.innerHTML = `
                            ${escapeHTML(segments[i])}
                            <button class="hover:opacity-75" aria-label="Remove ${segTag.name} filter">
                                <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
                                </svg>
                            </button>
                        `;

                        const capturedSegTag = segTag;
                        pill.querySelector('button').addEventListener('click', (e) => {
                            e.stopPropagation();
                            // Navigate up: keep ancestors of this tag, remove this and deeper
                            const parentName = getParentTagName(capturedSegTag.name);
                            activeTagFilters.length = 0;
                            if (parentName) {
                                // Rebuild filter chain up to parent
                                let p = '';
                                for (const s of parentName.split('/')) {
                                    p = p ? p + '/' + s : s;
                                    const pt = allTags.find(t => t.name === p);
                                    if (pt) activeTagFilters.push(pt.id);
                                }
                            }
                            updateActiveTagsDisplay();
                            renderTagFilterDropdown();
                            loadClips();
                        });

                        activeTagsContainer.appendChild(pill);
                    }
                }
            } else {
                // Normal mode: existing pill rendering
                activeTagFilters.forEach(tagId => {
                    const tag = allTags.find(t => t.id === tagId);
                    if (!tag) return;

                    const pill = document.createElement('span');
                    pill.className = 'inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-medium text-white';
                    pill.style.backgroundColor = tag.color;
                    pill.innerHTML = `
                        ${escapeHTML(tag.name)}
                        <button class="hover:opacity-75" aria-label="Remove ${tag.name} filter">
                            <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
                            </svg>
                        </button>
                    `;

                    pill.querySelector('button').addEventListener('click', (e) => {
                        e.stopPropagation();
                        toggleTagFilter(tagId);
                    });

                    activeTagsContainer.appendChild(pill);
                });
            }

            // Add clear all button
            const clearBtn = document.createElement('button');
            clearBtn.className = 'text-[10px] text-stone-400 hover:text-stone-600 underline ml-1 transition-colors';
            clearBtn.textContent = 'Clear all';
            clearBtn.addEventListener('click', clearAllTagFilters);
            activeTagsContainer.appendChild(clearBtn);
        }

        // Show/hide clear button in dropdown
        if (clearTagFiltersBtn) {
            clearTagFiltersBtn.classList.toggle('hidden', activeTagFilters.length === 0);
        }
    }
}

function clearAllTagFilters() {
    activeTagFilters = [];
    updateActiveTagsDisplay();
    renderTagFilterDropdown();
    loadClips();
}

// --- Tag Filter Dropdown Toggle ---

let tagFilterFocusTrapCleanup = null;

function openTagFilterDropdown() {
    // Don't let the tag filter dropdown coexist with the other header menus.
    if (typeof closeSortPopover === 'function') closeSortPopover();
    if (typeof closeSearchOptionsPopover === 'function') closeSearchOptionsPopover();
    tagFilterDropdown.classList.remove('hidden');
    // Re-index rover now that items are visible, and focus first item
    if (tagFilterRover) {
        tagFilterRover.update();
        const items = tagFilterRover.getItems();
        if (items.length > 0) {
            tagFilterRover.setActiveIndex(0);
            items[0].focus();
        }
    }
    if (tagFilterFocusTrapCleanup) tagFilterFocusTrapCleanup();
    tagFilterFocusTrapCleanup = trapFocus(tagFilterDropdown);
}

function closeTagFilterDropdown(restoreFocus = true) {
    tagFilterDropdown.classList.add('hidden');
    if (tagFilterFocusTrapCleanup) {
        tagFilterFocusTrapCleanup();
        tagFilterFocusTrapCleanup = null;
    }
    if (restoreFocus) tagFilterBtn.focus();
}

if (tagFilterBtn) {
    tagFilterBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        if (tagFilterDropdown.classList.contains('hidden')) {
            openTagFilterDropdown();
        } else {
            closeTagFilterDropdown();
        }
    });
}

if (clearTagFiltersBtn) {
    clearTagFiltersBtn.addEventListener('click', clearAllTagFilters);
}

// Close dropdown on Escape and restore focus to filter button
if (tagFilterDropdown) {
    tagFilterDropdown.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') {
            e.preventDefault();
            e.stopPropagation();
            closeTagFilterDropdown();
        }
    });
}

// Close dropdown when clicking outside
document.addEventListener('click', (e) => {
    if (tagFilterDropdown && !tagFilterDropdown.classList.contains('hidden')) {
        if (!tagFilterDropdown.contains(e.target) && e.target !== tagFilterBtn) {
            closeTagFilterDropdown(false);
        }
    }
});

// --- Tag Popover for Clip Tagging ---

function openTagPopover(clipId, anchorElement) {
    currentTaggingClipId = clipId;
    tagPopoverMode = 'single';
    renderTagPopoverList(clipId);
    positionPopover(anchorElement);
    tagPopover.classList.remove('hidden');
}

function openBulkTagPopover(anchorElement) {
    currentTaggingClipId = null;
    tagPopoverMode = 'bulk';
    renderTagPopoverList(null);
    positionPopover(anchorElement);
    tagPopover.classList.remove('hidden');
}

function closeTagPopover() {
    tagPopover.classList.add('hidden');
    currentTaggingClipId = null;
}

function positionPopover(anchorElement) {
    if (!tagPopover || !anchorElement) return;

    // Temporarily show popover off-screen to measure its actual size
    tagPopover.style.visibility = 'hidden';
    tagPopover.style.left = '-9999px';
    tagPopover.style.top = '-9999px';
    tagPopover.classList.remove('hidden');

    const rect = anchorElement.getBoundingClientRect();
    const popoverRect = tagPopover.getBoundingClientRect();
    const padding = 8;

    // Calculate available space above and below
    const spaceBelow = window.innerHeight - rect.bottom - padding;
    const spaceAbove = rect.top - padding;

    // Decide whether to position above or below
    let top;
    if (spaceBelow >= popoverRect.height) {
        // Enough space below
        top = rect.bottom + padding;
    } else if (spaceAbove >= popoverRect.height) {
        // Enough space above
        top = rect.top - popoverRect.height - padding;
    } else {
        // Not enough space either way - position at top of viewport with some margin
        top = padding;
    }

    // Horizontal positioning - center on anchor
    let left = rect.left + (rect.width / 2) - (popoverRect.width / 2);

    // Keep within viewport horizontally
    if (left < padding) left = padding;
    if (left + popoverRect.width > window.innerWidth - padding) {
        left = window.innerWidth - popoverRect.width - padding;
    }

    // Apply position and make visible
    tagPopover.style.left = `${left}px`;
    tagPopover.style.top = `${top}px`;
    tagPopover.style.visibility = 'visible';
}

async function renderTagPopoverList(clipId) {
    if (!tagPopoverList) return;
    tagPopoverList.innerHTML = '';

    let clipTagIds = [];
    if (clipId) {
        const card = document.querySelector(`[data-id="${clipId}"]`);
        if (card && card.clipTags) {
            clipTagIds = card.clipTags.map(t => t.id);
        }
    }

    if (allTags.length === 0) {
        tagPopoverList.innerHTML = '<p class="text-stone-400 text-xs px-3 py-2">No tags yet. Create one below.</p>';
        return;
    }

    const tree = buildTagTree(allTags);

    function renderNode(node, depth) {
        const { tag, children } = node;
        const hasTag = clipTagIds.includes(tag.id);
        const displayName = depth === 0 ? tag.name : getShortTagName(tag.name);

        const item = document.createElement('label');
        item.className = 'flex items-center gap-2 py-1.5 hover:bg-stone-100 cursor-pointer transition-colors';
        item.style.paddingLeft = `${12 + depth * 16}px`;
        item.style.paddingRight = '12px';
        item.innerHTML = `
            <input type="checkbox"
                   data-testid="tag-checkbox-${tag.name}"
                   class="rounded border-stone-300 text-stone-600 focus:ring-stone-500"
                   ${hasTag ? 'checked' : ''}>
            <span class="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-medium text-white"
                  style="background-color: ${tag.color}">
                ${escapeHTML(displayName)}
            </span>
        `;

        const checkbox = item.querySelector('input');
        checkbox.addEventListener('change', () => {
            if (tagPopoverMode === 'bulk') {
                handleBulkTagToggle(tag.id, checkbox.checked);
            } else {
                handleClipTagToggle(currentTaggingClipId, tag.id, checkbox.checked);
            }
        });

        tagPopoverList.appendChild(item);

        for (const child of children) {
            renderNode(child, depth + 1);
        }
    }

    for (const root of tree) {
        renderNode(root, 0, false);
    }
}

async function handleClipTagToggle(clipId, tagId, add) {
    if (add) {
        await addTagToClip(clipId, tagId);
    } else {
        await removeTagFromClip(clipId, tagId);
        // Reload tags in case the tag was auto-deleted (no more clips)
        await loadTags();
        // Remove from active filters if it no longer exists
        if (!allTags.find(t => t.id === tagId)) {
            const idx = activeTagFilters.indexOf(tagId);
            if (idx !== -1) {
                activeTagFilters.splice(idx, 1);
                updateActiveTagsDisplay();
            }
        }
    }
    // Refresh to show updated tags on card
    loadClips();
    closeTagPopover();
}

async function handleBulkTagToggle(tagId, add) {
    const ids = Array.from(selectedIds);
    if (add) {
        await bulkAddTag(ids, tagId);
    } else {
        await bulkRemoveTag(ids, tagId);
        // Reload tags in case the tag was auto-deleted (no more clips)
        await loadTags();
        // Remove from active filters if it no longer exists
        if (!allTags.find(t => t.id === tagId)) {
            const idx = activeTagFilters.indexOf(tagId);
            if (idx !== -1) {
                activeTagFilters.splice(idx, 1);
                updateActiveTagsDisplay();
            }
        }
    }
    loadClips();
    closeTagPopover();
}

// Validate tag name and show error if invalid
function validateTagName(name) {
    if (!name || name.trim() === '') {
        showToast('Tag name cannot be empty.');
        return false;
    }
    if (name.length > MAX_TAG_NAME_LENGTH) {
        showToast(`Tag name too long (max ${MAX_TAG_NAME_LENGTH} characters).`);
        return false;
    }
    return true;
}

// Create new tag from popover
if (createTagBtn) {
    createTagBtn.addEventListener('click', async () => {
        const name = createTagInput.value.trim();
        if (!validateTagName(name)) return;

        const tag = await createTag(name);
        if (tag) {
            createTagInput.value = '';
            await loadTags();

            // If in single mode, add to the clip
            if (tagPopoverMode === 'single' && currentTaggingClipId) {
                await addTagToClip(currentTaggingClipId, tag.id);
                loadClips();
            } else if (tagPopoverMode === 'bulk') {
                await bulkAddTag(Array.from(selectedIds), tag.id);
                loadClips();
            }

            closeTagPopover();
        }
    });
}

if (createTagInput) {
    createTagInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            createTagBtn.click();
        }
    });
}

// Close popover when clicking outside
document.addEventListener('click', (e) => {
    if (tagPopover && !tagPopover.classList.contains('hidden')) {
        if (!tagPopover.contains(e.target) && !e.target.closest('[data-tag-btn]')) {
            closeTagPopover();
        }
    }
});

// Bulk tag button
if (bulkTagBtn) {
    bulkTagBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        openBulkTagPopover(bulkTagBtn);
    });
}

// --- Tag Pills on Cards ---

function renderCardTags(card, tags) {
    // Store tags on card for later reference
    card.clipTags = tags || [];

    // Find or create tags container
    let tagsContainer = card.querySelector('.clip-tags');
    if (!tagsContainer) {
        tagsContainer = document.createElement('div');
        tagsContainer.className = 'clip-tags flex flex-wrap gap-1 px-2 pb-2';
        // Insert before the action buttons row
        const footer = card.querySelector('.clip-card-footer');
        if (footer) {
            card.insertBefore(tagsContainer, footer);
        } else {
            card.appendChild(tagsContainer);
        }
    }

    tagsContainer.innerHTML = '';

    if (!tags || tags.length === 0) {
        tagsContainer.classList.add('hidden');
        return;
    }

    tagsContainer.classList.remove('hidden');

    // Show max 3 tags, then +N
    const maxVisible = 3;
    const visibleTags = tags.slice(0, maxVisible);
    const overflow = tags.length - maxVisible;

    visibleTags.forEach(tag => {
        const pill = document.createElement('button');
        const showFullPath = tag.name.includes('/');
        pill.className = 'inline-flex max-w-full items-center overflow-hidden text-ellipsis whitespace-nowrap px-1.5 py-0.5 rounded text-[9px] font-medium text-white hover:opacity-80 transition-opacity';
        pill.style.backgroundColor = tag.color;
        pill.textContent = showFullPath ? tag.name : getShortTagName(tag.name);
        pill.dataset.testid = `tag-pill-${tag.name}`;
        pill.title = tag.name;

        // Click to filter by this tag
        pill.addEventListener('click', (e) => {
            e.stopPropagation();
            if (!activeTagFilters.includes(tag.id)) {
                toggleTagFilter(tag.id);
                renderTagFilterDropdown();
            }
        });

        tagsContainer.appendChild(pill);
    });

    if (overflow > 0) {
        const more = document.createElement('span');
        more.className = 'text-[9px] text-stone-400';
        more.textContent = `+${overflow}`;
        tagsContainer.appendChild(more);
    }
}

window.handleTagReferenceEvent = async function(eventName, payload) {
    // Always reload the tag list first so subsequent lookups use fresh data.
    if (typeof loadTags === 'function') {
        await loadTags();
    }
    const validIDs = new Set(allTags.map(t => t.id));

    // Build substitution map for merges: source_id -> dest_id.
    const substitutions = new Map();
    if (eventName === 'tag:merged'
        && payload && typeof payload.source_id === 'number'
        && typeof payload.dest_id === 'number') {
        substitutions.set(payload.source_id, payload.dest_id);
    }

    // (1) Normalize activeTagFilters in BOTH folder-mode and non-folder-mode.
    if (typeof window.normalizeActiveTagFilters === 'function') {
        window.normalizeActiveTagFilters(validIDs, substitutions);
    }

    // (2) Remap currentFolderTagID for merge-source before lookup.
    if (substitutions.has(currentFolderTagID)) {
        currentFolderTagID = substitutions.get(currentFolderTagID);
    }

    if (currentFolderTagID == null) {
        if (typeof loadClips === 'function') loadClips();
        return;
    }

    const current = allTags.find(t => t.id === currentFolderTagID);
    if (current) {
        // Tag still exists (rename, or merge destination after remap) —
        // re-navigate to its current path (name may have changed).
        if (typeof navigateToFolder === 'function') {
            navigateToFolder(current.id);
        }
        return;
    }

    // currentFolderTagID does not resolve to any live tag. Only tag:deleted
    // can genuinely remove an ID — for rename/merge, an unresolvable ID is
    // stale state, not a deletion. Silently clear and reload so a rename
    // doesn't accidentally kick the user out of folder mode.
    if (eventName !== 'tag:deleted') {
        currentFolderTagID = null;
        if (typeof loadClips === 'function') loadClips();
        return;
    }

    // Deletion recovery: try to navigate to parent, else exit folder mode.
    const deletedName = payload && (payload.name || payload.old_name);
    const parentName = deletedName ? getParentTagName(deletedName) : '';
    const parent = parentName ? allTags.find(t => t.name === parentName) : null;
    if (parent && typeof navigateToFolder === 'function') {
        navigateToFolder(parent.id);
    } else if (typeof toggleFolderMode === 'function' && typeof isFolderMode === 'function' && isFolderMode()) {
        // Exit folder mode by toggling it off.
        toggleFolderMode();
    } else {
        currentFolderTagID = null;
        if (typeof loadClips === 'function') loadClips();
    }
};
