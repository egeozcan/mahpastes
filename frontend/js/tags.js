// --- Tag UI Management ---

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

// Current clip being tagged (for single-clip popover)
let currentTaggingClipId = null;
let tagPopoverMode = 'single'; // 'single' or 'bulk'

// --- Tag Filter Dropdown ---

function renderTagFilterDropdown() {
    if (!tagFilterList) return;

    tagFilterList.innerHTML = '';

    if (allTags.length === 0) {
        tagFilterList.innerHTML = '<p class="text-stone-400 text-xs px-3 py-2">No tags yet</p>';
        return;
    }

    allTags.forEach(tag => {
        const isActive = activeTagFilters.includes(tag.id);
        const item = document.createElement('label');
        item.className = 'flex items-center gap-2 px-3 py-1.5 hover:bg-stone-100 cursor-pointer transition-colors';
        item.innerHTML = `
            <input type="checkbox"
                   data-testid="tag-checkbox-${tag.name}"
                   class="rounded border-stone-300 text-stone-600 focus:ring-stone-500"
                   ${isActive ? 'checked' : ''}>
            <span class="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-medium text-white"
                  style="background-color: ${tag.color}">
                ${escapeHtml(tag.name)}
            </span>
            <span class="text-stone-400 text-[10px] ml-auto">${tag.count}</span>
        `;

        const checkbox = item.querySelector('input');
        checkbox.addEventListener('change', () => toggleTagFilter(tag.id));

        tagFilterList.appendChild(item);
    });
}

function toggleTagFilter(tagId) {
    const idx = activeTagFilters.indexOf(tagId);
    if (idx === -1) {
        activeTagFilters.push(tagId);
    } else {
        activeTagFilters.splice(idx, 1);
    }
    updateActiveTagsDisplay();
    renderTagFilterDropdown();
    loadClips();
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

            activeTagFilters.forEach(tagId => {
                const tag = allTags.find(t => t.id === tagId);
                if (!tag) return;

                const pill = document.createElement('span');
                pill.className = 'inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-medium text-white';
                pill.style.backgroundColor = tag.color;
                pill.innerHTML = `
                    ${escapeHtml(tag.name)}
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

if (tagFilterBtn) {
    tagFilterBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        tagFilterDropdown.classList.toggle('hidden');
    });
}

if (clearTagFiltersBtn) {
    clearTagFiltersBtn.addEventListener('click', clearAllTagFilters);
}

// Close dropdown when clicking outside
document.addEventListener('click', (e) => {
    if (tagFilterDropdown && !tagFilterDropdown.classList.contains('hidden')) {
        if (!tagFilterDropdown.contains(e.target) && e.target !== tagFilterBtn) {
            tagFilterDropdown.classList.add('hidden');
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

    // Get clip's current tags if single mode
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

    allTags.forEach(tag => {
        const hasTag = clipTagIds.includes(tag.id);
        const item = document.createElement('label');
        item.className = 'flex items-center gap-2 px-3 py-1.5 hover:bg-stone-100 cursor-pointer transition-colors';
        item.innerHTML = `
            <input type="checkbox"
                   data-testid="tag-checkbox-${tag.name}"
                   class="rounded border-stone-300 text-stone-600 focus:ring-stone-500"
                   ${hasTag ? 'checked' : ''}>
            <span class="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-medium text-white"
                  style="background-color: ${tag.color}">
                ${escapeHtml(tag.name)}
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
    });
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

// Create new tag from popover
if (createTagBtn) {
    createTagBtn.addEventListener('click', async () => {
        const name = createTagInput.value.trim();
        if (!name) return;

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
        pill.className = 'inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-medium text-white hover:opacity-80 transition-opacity';
        pill.style.backgroundColor = tag.color;
        pill.textContent = tag.name;
        pill.dataset.testid = `tag-pill-${tag.name}`;
        pill.title = `Filter by "${tag.name}"`;

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
