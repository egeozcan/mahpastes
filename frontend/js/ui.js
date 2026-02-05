// Image cache for base64 data
const imageCache = new Map();

// Plugin UI actions cache
let pluginUIActions = null;

// Load plugin UI actions from backend
async function loadPluginUIActions() {
    try {
        pluginUIActions = await window.go.main.PluginService.GetPluginUIActions();
    } catch (error) {
        console.error('Failed to load plugin UI actions:', error);
        pluginUIActions = { card_actions: [], lightbox_buttons: [] };
    }
    return pluginUIActions;
}

// Get icon SVG for built-in menu actions
function getMenuIcon(name) {
    const icons = {
        'copy-path': '<path stroke-linecap="round" stroke-linejoin="round" d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2m0 0h2a2 2 0 012 2v3m2 4H10m0 0l3-3m-3 3l3 3"/>',
        'save': '<path stroke-linecap="round" stroke-linejoin="round" d="M4 16v1a2 2 0 002 2h12a2 2 0 002-2v-1m-4-4l-4 4m0 0l-4-4m4 4V4"/>',
        'edit': '<path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/>',
        'tags': '<path stroke-linecap="round" stroke-linejoin="round" d="M7 7h.01M7 3h5c.512 0 1.024.195 1.414.586l7 7a2 2 0 010 2.828l-7 7a2 2 0 01-2.828 0l-7-7A1.994 1.994 0 013 12V7a4 4 0 014-4z"/>',
        'archive': '<path stroke-linecap="round" stroke-linejoin="round" d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4"/>',
        'restore': '<path stroke-linecap="round" stroke-linejoin="round" d="M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6"/>',
        'delete': '<path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/>',
    };
    const path = icons[name];
    if (!path) return '';
    return `<svg class="card-menu-icon" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24">${path}</svg>`;
}

// Render card menu dropdown
function renderCardMenu(clipId, button, clip) {
    // Close any existing menu
    closeCardMenu();

    const menu = document.createElement('div');
    menu.className = 'card-menu-dropdown fixed';
    menu.setAttribute('role', 'menu');
    menu.setAttribute('aria-label', 'Clip actions');
    menu.dataset.clipId = clipId;

    // Built-in actions
    const builtInActions = [
        { id: 'copy-path', label: 'Copy Path', icon: 'copy-path' },
        { id: 'save-file', label: 'Save', icon: 'save' },
    ];

    // Add edit option for editable types
    if (isEditableType(clip.content_type)) {
        builtInActions.push({ id: 'edit', label: 'Edit', icon: 'edit' });
    }

    builtInActions.push({ id: 'tags', label: 'Tags', icon: 'tags' });
    builtInActions.push({ id: 'archive', label: isViewingArchive ? 'Restore' : 'Archive', icon: isViewingArchive ? 'restore' : 'archive' });
    builtInActions.push({ id: 'delete', label: 'Delete', icon: 'delete', danger: true });

    // Render built-in actions
    builtInActions.forEach(action => {
        const item = document.createElement('button');
        item.className = `card-menu-item${action.danger ? ' card-menu-item-danger' : ''}`;
        item.setAttribute('role', 'menuitem');
        item.dataset.action = action.id;
        item.dataset.clipId = clipId;
        item.innerHTML = `${getMenuIcon(action.icon)}<span>${action.label}</span>`;
        menu.appendChild(item);
    });

    // Add plugin actions if any
    if (pluginUIActions && pluginUIActions.card_actions && pluginUIActions.card_actions.length > 0) {
        // Add divider
        const divider = document.createElement('hr');
        divider.className = 'card-menu-divider';
        menu.appendChild(divider);

        // Render plugin actions
        pluginUIActions.card_actions.forEach(action => {
            const item = document.createElement('button');
            item.className = 'card-menu-item';
            item.setAttribute('role', 'menuitem');
            item.dataset.action = 'plugin';
            item.dataset.pluginId = action.plugin_id;
            item.dataset.actionId = action.id;
            item.dataset.clipId = clipId;
            item.dataset.hasOptions = action.options && action.options.length > 0 ? 'true' : 'false';

            const iconHtml = action.icon && typeof getPluginIcon === 'function'
                ? getPluginIcon(action.icon) || ''
                : '';
            const iconClass = iconHtml ? '' : 'card-menu-icon'; // Fallback empty space

            item.innerHTML = `${iconHtml || `<span class="${iconClass}"></span>`}<span>${escapeHTML(action.label)}</span>`;
            menu.appendChild(item);
        });
    }

    // Position the menu
    document.body.appendChild(menu);
    positionCardMenu(menu, button);

    // Setup keyboard navigation
    setupMenuKeyboard(menu);

    // Update button state
    button.setAttribute('aria-expanded', 'true');

    // Focus first item
    const firstItem = menu.querySelector('[role="menuitem"]');
    if (firstItem) firstItem.focus();

    return menu;
}

// Position the menu relative to the button
function positionCardMenu(menu, button) {
    const buttonRect = button.getBoundingClientRect();
    const menuRect = menu.getBoundingClientRect();

    let top = buttonRect.bottom + 4;
    let left = buttonRect.right - menuRect.width;

    // Ensure menu stays within viewport
    if (left < 8) left = 8;
    if (top + menuRect.height > window.innerHeight - 8) {
        top = buttonRect.top - menuRect.height - 4;
    }

    menu.style.top = `${top}px`;
    menu.style.left = `${left}px`;
}

// Setup keyboard navigation for menu
function setupMenuKeyboard(menu) {
    const items = Array.from(menu.querySelectorAll('[role="menuitem"]'));
    let currentIndex = 0;

    menu.addEventListener('keydown', (e) => {
        switch (e.key) {
            case 'ArrowDown':
                e.preventDefault();
                currentIndex = (currentIndex + 1) % items.length;
                items[currentIndex].focus();
                break;
            case 'ArrowUp':
                e.preventDefault();
                currentIndex = (currentIndex - 1 + items.length) % items.length;
                items[currentIndex].focus();
                break;
            case 'Escape':
                e.preventDefault();
                closeCardMenu();
                break;
            case 'Tab':
                e.preventDefault();
                closeCardMenu();
                break;
        }
    });
}

// Close any open card menu
function closeCardMenu() {
    const existingMenu = document.querySelector('.card-menu-dropdown');
    if (existingMenu) {
        // Reset the trigger button state
        const clipId = existingMenu.dataset.clipId;
        const triggerBtn = document.querySelector(`[data-action="menu"][data-id="${clipId}"]`);
        if (triggerBtn) {
            triggerBtn.setAttribute('aria-expanded', 'false');
        }
        existingMenu.remove();
    }
}

// Handle built-in card actions
async function handleCardAction(action, clipId, triggerButton) {
    closeCardMenu();
    const id = Number(clipId);

    switch (action) {
        case 'copy-path':
            saveTempFile(id);
            break;
        case 'save-file':
            saveClipToFile(id);
            break;
        case 'edit':
            openEditor(id);
            break;
        case 'tags':
            // Get the card to find a reference element for the popover
            const card = gallery.querySelector(`li[data-id="${clipId}"]`);
            if (card) {
                const tagBtn = card.querySelector('[data-action="menu"]');
                openTagPopover(id, tagBtn || triggerButton);
            }
            break;
        case 'archive':
            toggleArchiveClip(id);
            break;
        case 'delete':
            deleteClip(id);
            break;
    }
}

async function createClipCard(clip) {
    const card = document.createElement('li');
    card.className = 'bg-white rounded-md border border-stone-200 overflow-hidden flex flex-col transition-all duration-150 hover:border-stone-300 relative group';
    card.dataset.id = clip.id;
    card.dataset.filename = (clip.filename || '').toLowerCase();
    card.dataset.type = (clip.content_type || '').toLowerCase();
    card.setAttribute('aria-label', `Clip: ${clip.filename || 'Pasted Content'}`);

    const checkboxHTML = `
        <div class="absolute top-2 right-2 z-30 opacity-0 group-hover:opacity-100 focus-within:opacity-100 group-[.has-checked]:opacity-100 transition-opacity duration-150">
            <div class="relative">
                <input type="checkbox" data-id="${clip.id}"
                    aria-label="Select clip ${clip.filename || 'Pasted Content'}"
                    class="clip-checkbox appearance-none w-5 h-5 rounded border border-white/60 bg-black/20 backdrop-blur-sm checked:bg-stone-700 checked:border-stone-700 transition-all cursor-pointer peer" ${selectedIds.has(clip.id) ? 'checked' : ''}>
                <svg class="absolute inset-0 w-5 h-5 text-white pointer-events-none opacity-0 peer-checked:opacity-100 transition-opacity p-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M5 13l4 4L19 7" />
                </svg>
            </div>
        </div>
    `;

    let previewHTML = '';

    if (clip.content_type.startsWith('image/')) {
        // For images, show loading placeholder initially
        previewHTML = `<div class="preview-container overflow-hidden aspect-square w-full bg-stone-100 flex items-center justify-center">
            <img data-clip-id="${clip.id}" alt="${escapeHTML(clip.filename) || 'Uploaded image'}" class="h-full w-full object-cover transition-transform duration-300 group-hover:scale-[1.02] hidden">
            <div class="loading-spinner text-stone-400">
                <svg class="animate-spin h-5 w-5" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                    <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3"></circle>
                    <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                </svg>
            </div>
        </div>`;
    } else if (clip.content_type === 'text/html') {
        // For HTML, show text preview (no iframe in Wails)
        const htmlPreview = escapeHTML(clip.preview || '').substring(0, 200);
        previewHTML = `<div class="preview-container aspect-square w-full relative bg-stone-50">
            <div class="p-3 text-[10px] text-stone-500 font-mono overflow-hidden h-full leading-relaxed">${htmlPreview}...</div>
            <div class="absolute inset-0 bg-transparent" title="HTML Preview"></div>
        </div>`;
    } else if (clip.content_type.startsWith('text/') || clip.content_type === 'application/json') {
        previewHTML = `<div class="preview-container aspect-square w-full overflow-hidden bg-stone-900"><pre class="p-3 text-[9px] leading-relaxed overflow-auto h-full text-stone-400"><code>${escapeHTML(clip.preview)}</code></pre></div>`;
    } else {
        previewHTML = `
        <div class="preview-container aspect-square w-full flex flex-col items-center justify-center bg-stone-50 text-stone-400">
            <svg class="w-10 h-10" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"></path></svg>
            <span class="mt-2 text-[9px] font-medium uppercase tracking-wider">${getFriendlyFileType(clip.content_type, clip.filename)}</span>
        </div>`;
    }

    let expirationBadge = '';
    if (clip.expires_at) {
        expirationBadge = `<div class="absolute top-2 left-2 bg-stone-700 text-white text-[8px] font-semibold px-1.5 py-0.5 rounded z-20 uppercase tracking-wide">
            Temp
        </div>`;
    }

    card.innerHTML = `
        ${checkboxHTML}
        <div class="relative cursor-pointer" data-action="open-lightbox">
            ${expirationBadge}
            ${previewHTML}
        </div>

        <!-- Minimal footer -->
        <div class="p-2.5 flex flex-col gap-1.5 border-t border-stone-100">
            <p class="text-[11px] font-medium text-stone-700 truncate" title="${escapeHTML(clip.filename) || 'Pasted Content'}">
                ${escapeHTML(clip.filename) || '<span class="text-stone-400 font-normal">Pasted</span>'}
            </p>
            <div class="flex justify-between items-center">
                <span class="text-[9px] font-medium text-stone-400 uppercase tracking-wide">${getFriendlyFileType(clip.content_type, clip.filename)}</span>
                <button class="card-menu-trigger p-1 text-stone-400 hover:text-stone-600 hover:bg-stone-100 rounded transition-colors"
                        data-action="menu"
                        data-id="${clip.id}"
                        aria-label="Actions"
                        aria-haspopup="true"
                        aria-expanded="false">
                    <svg class="w-3 h-3" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M12 6.75a.75.75 0 110-1.5.75.75 0 010 1.5zM12 12.75a.75.75 0 110-1.5.75.75 0 010 1.5zM12 18.75a.75.75 0 110-1.5.75.75 0 010 1.5z" />
                    </svg>
                </button>
            </div>
        </div>
    `;

    // Menu trigger listener
    const menuTrigger = card.querySelector('[data-action="menu"]');
    menuTrigger.addEventListener('click', (e) => {
        e.stopPropagation();
        renderCardMenu(clip.id, e.currentTarget, clip);
    });

    // Render tags on the card
    if (typeof renderCardTags === 'function') {
        renderCardTags(card, clip.tags);
    }

    // Checkbox logic
    const checkbox = card.querySelector('.clip-checkbox');
    checkbox.addEventListener('change', (e) => {
        const id = Number(clip.id);
        if (e.target.checked) {
            selectedIds.add(id);
            card.classList.add('has-checked');
        } else {
            selectedIds.delete(id);
            card.classList.remove('has-checked');
        }

        // Sync Select All checkbox
        const allCheckboxes = Array.from(gallery.querySelectorAll('.clip-checkbox'));
        selectAllCheckbox.checked = allCheckboxes.length > 0 && allCheckboxes.every(cb => cb.checked);

        updateBulkToolbar();
    });

    // Prevent lightbox trigger if clicking checkbox
    checkbox.addEventListener('click', (e) => e.stopPropagation());

    // Lightbox trigger logic
    if (clip.content_type.startsWith('image/')) {
        const imageIndex = imageClips.length;
        imageClips.push(clip);
        card.querySelector('[data-action="open-lightbox"]').addEventListener('click', () => openLightbox(imageIndex));

        // Load image asynchronously
        loadImageForCard(clip.id, card);
    } else {
        // For non-images, clicking opens the editor or shows content
        card.querySelector('[data-action="open-lightbox"]').addEventListener('click', () => {
            if (isEditableType(clip.content_type)) {
                openEditor(clip.id);
            }
        });
    }

    gallery.appendChild(card);
}

// Load image data for a card
async function loadImageForCard(clipId, card) {
    try {
        const clipData = await getClipData(clipId);
        const dataUrl = `data:${clipData.content_type};base64,${clipData.data}`;

        // Cache the data URL
        imageCache.set(clipId, dataUrl);

        const img = card.querySelector(`img[data-clip-id="${clipId}"]`);
        const spinner = card.querySelector('.loading-spinner');

        if (img) {
            img.src = dataUrl;
            img.classList.remove('hidden');
        }
        if (spinner) {
            spinner.remove();
        }
    } catch (error) {
        console.error(`Failed to load image for clip ${clipId}:`, error);
        const spinner = card.querySelector('.loading-spinner');
        if (spinner) {
            spinner.innerHTML = '<span class="text-red-400 text-xs">Failed to load</span>';
        }
    }
}

// Get cached or load image data URL
async function getImageDataUrl(clipId) {
    if (imageCache.has(clipId)) {
        return imageCache.get(clipId);
    }

    const clipData = await getClipData(clipId);
    const dataUrl = `data:${clipData.content_type};base64,${clipData.data}`;
    imageCache.set(clipId, dataUrl);
    return dataUrl;
}

function updateBulkToolbar() {
    const count = selectedIds.size;
    if (count > 0) {
        bulkToolbar.classList.remove('hidden', 'translate-y-4', 'opacity-0', 'pointer-events-none');
        bulkToolbar.classList.add('translate-y-0', 'opacity-100', 'pointer-events-auto');
        selectedCountEl.textContent = `${count} selected`;
        bulkArchiveText.textContent = isViewingArchive ? 'Restore' : 'Archive';

        // Comparison Logic: Show compare button if 2 items are selected and BOTH are images
        if (count === 2) {
            const selectedImages = Array.from(selectedIds).filter(id => {
                const card = gallery.querySelector(`li[data-id="${id}"]`);
                return card && card.dataset.type.startsWith('image/');
            });

            if (selectedImages.length === 2) {
                bulkCompareBtn.classList.remove('hidden');
            } else {
                bulkCompareBtn.classList.add('hidden');
            }
        } else {
            bulkCompareBtn.classList.add('hidden');
        }
    } else {
        bulkToolbar.classList.remove('translate-y-0', 'opacity-100', 'pointer-events-auto');
        bulkToolbar.classList.add('translate-y-4', 'opacity-0', 'pointer-events-none');
        selectAllCheckbox.checked = false;
        bulkCompareBtn.classList.add('hidden');
    }

    // Update AI actions visibility
    if (typeof updateAIActionsVisibility === 'function') {
        updateAIActionsVisibility();
    }
}

function toggleSelectAll() {
    const checkboxes = gallery.querySelectorAll('.clip-checkbox');
    const shouldSelectAll = selectAllCheckbox.checked;

    checkboxes.forEach(cb => {
        const id = Number(cb.dataset.id);
        cb.checked = shouldSelectAll;
        const card = cb.closest('li');
        if (shouldSelectAll) {
            selectedIds.add(id);
            if (card) card.classList.add('has-checked');
        } else {
            selectedIds.delete(id);
            if (card) card.classList.remove('has-checked');
        }
    });
    updateBulkToolbar();
}

function cancelSelection() {
    selectedIds.clear();
    const checkboxes = gallery.querySelectorAll('.clip-checkbox');
    checkboxes.forEach(cb => {
        cb.checked = false;
        const card = cb.closest('li');
        if (card) card.classList.remove('has-checked');
    });
    selectAllCheckbox.checked = false;
    updateBulkToolbar();
}

function toggleViewMode() {
    isViewingArchive = !isViewingArchive;

    // Hide watch view if open
    if (isViewingWatch) {
        isViewingWatch = false;
        watchBtnText.textContent = 'Watch';
        toggleWatchViewBtn.classList.remove('bg-stone-800', 'text-white', 'border-stone-800', 'hover:bg-stone-700', 'hover:border-stone-700');
        toggleWatchViewBtn.classList.add('border-stone-200', 'text-stone-600', 'hover:bg-stone-100', 'hover:border-stone-300');
        toggleWatchViewBtn.setAttribute('aria-pressed', 'false');
        watchView.classList.add('hidden');
        uploadSection.classList.remove('hidden');
    }

    toggleArchiveViewBtn.setAttribute('aria-pressed', isViewingArchive);
    if (isViewingArchive) {
        archiveBtnText.textContent = "Active";
        toggleArchiveViewBtn.classList.add('bg-stone-800', 'text-white', 'border-stone-800');
        toggleArchiveViewBtn.classList.remove('border-stone-200', 'text-stone-600', 'hover:border-stone-300', 'hover:bg-stone-100');
        uploadSection.classList.add('opacity-50', 'pointer-events-none'); // Disable upload in archive view
        uploadSection.setAttribute('aria-hidden', 'true');
    } else {
        archiveBtnText.textContent = "Archive";
        toggleArchiveViewBtn.classList.remove('bg-stone-800', 'text-white', 'border-stone-800');
        toggleArchiveViewBtn.classList.add('border-stone-200', 'text-stone-600', 'hover:border-stone-300', 'hover:bg-stone-100');
        uploadSection.classList.remove('opacity-50', 'pointer-events-none');
        uploadSection.removeAttribute('aria-hidden');
    }

    // Ensure main view is visible
    gallery.parentElement.classList.remove('hidden');

    // Clear image cache when switching views
    imageCache.clear();
    loadClips();
}

// Search Logic
const searchInput = document.getElementById('search-input');
searchInput.addEventListener('input', (e) => {
    const query = e.target.value.toLowerCase();
    const cards = gallery.querySelectorAll('li');
    cards.forEach(card => {
        const filename = card.dataset.filename || '';
        const type = card.dataset.type || '';
        if (filename.includes(query) || type.includes(query)) {
            card.style.display = '';
        } else {
            card.style.display = 'none';
        }
    });
});
