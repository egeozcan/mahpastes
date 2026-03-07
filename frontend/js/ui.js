// Image cache for base64 data
const imageCache = new Map();

// Track last checked checkbox for shift-click range selection
let lastCheckedCheckbox = null;

// Plugin UI actions cache
let pluginUIActions = null;
let dragPrepBusyCount = 0;

function beginGlobalDragPrepareCursor() {
    dragPrepBusyCount += 1;
    document.body.classList.add('drag-preparing');
}

function endGlobalDragPrepareCursor() {
    dragPrepBusyCount = Math.max(0, dragPrepBusyCount - 1);
    if (dragPrepBusyCount === 0) {
        document.body.classList.remove('drag-preparing');
    }
}

// Load plugin UI actions from backend
async function loadPluginUIActions() {
    try {
        pluginUIActions = await window.go.main.PluginService.GetPluginUIActions();
    } catch (error) {
        console.error('Failed to load plugin UI actions:', error);
        pluginUIActions = { card_actions: [], lightbox_buttons: [], global_actions: [] };
    }
    renderDrawerPluginActions();
    return pluginUIActions;
}

// Render global plugin actions in the hamburger menu drawer
function renderDrawerPluginActions() {
    const container = document.getElementById('drawer-plugin-actions');
    if (!container) return;

    // Remove old action buttons (keep the divider which is the first child)
    while (container.children.length > 1) {
        container.removeChild(container.lastChild);
    }

    const actions = pluginUIActions?.global_actions || [];
    if (actions.length === 0) {
        container.classList.add('hidden');
        return;
    }

    container.classList.remove('hidden');

    actions.forEach(action => {
        const btn = document.createElement('button');
        btn.className = 'border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-500 text-xs font-medium py-2.5 px-3 rounded-md transition-colors flex items-center w-full';
        btn.dataset.globalAction = 'true';
        btn.dataset.pluginId = action.plugin_id;
        btn.dataset.actionId = action.id;
        btn.dataset.hasOptions = action.options && action.options.length > 0 ? 'true' : 'false';
        btn.dataset.isAsync = action.async ? 'true' : 'false';

        let iconHtml = '';
        if (typeof getPluginIcon === 'function') {
            iconHtml = getPluginIcon(action.icon) || getPluginIcon('bolt') || '';
        }
        // Wrap icon in a span with same styling as drawer menu icons
        if (iconHtml) {
            iconHtml = iconHtml.replace('class="w-4 h-4"', 'class="w-4 h-4 mr-2 opacity-60"');
        }

        btn.setAttribute('aria-label', action.label);
        btn.innerHTML = `${iconHtml}<span>${escapeHTML(action.label)}</span>`;
        container.appendChild(btn);
    });
}

// Get icon SVG for built-in menu actions
function getMenuIcon(name) {
    const icons = {
        'copy-path': '<path stroke-linecap="round" stroke-linejoin="round" d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2m0 0h2a2 2 0 012 2v3m2 4H10m0 0l3-3m-3 3l3 3"/>',
        'save': '<path stroke-linecap="round" stroke-linejoin="round" d="M4 16v1a2 2 0 002 2h12a2 2 0 002-2v-1m-4-4l-4 4m0 0l-4-4m4 4V4"/>',
        'edit': '<path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/>',
        'tags': '<path stroke-linecap="round" stroke-linejoin="round" d="M7 7h.01M7 3h5c.512 0 1.024.195 1.414.586l7 7a2 2 0 010 2.828l-7 7a2 2 0 01-2.828 0l-7-7A1.994 1.994 0 013 12V7a4 4 0 014-4z"/>',
        'metadata': '<path stroke-linecap="round" stroke-linejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"/>',
        'archive': '<path stroke-linecap="round" stroke-linejoin="round" d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4"/>',
        'restore': '<path stroke-linecap="round" stroke-linejoin="round" d="M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6"/>',
        'copy-file': '<path stroke-linecap="round" stroke-linejoin="round" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"/>',
        'copy-contents': '<path stroke-linecap="round" stroke-linejoin="round" d="M8 7v8a2 2 0 002 2h6M8 7V5a2 2 0 012-2h4.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V15a2 2 0 01-2 2h-2M8 7H6a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2v-2"/>',
        'delete': '<path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/>',
        'set-expiration': '<path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"/>',
        'cancel-expiration': '<path stroke-linecap="round" stroke-linejoin="round" d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z"/>',
        'merge': '<path stroke-linecap="round" stroke-linejoin="round" d="M7 16V4m0 0L3 8m4-4l4 4m6 0v12m0 0l4-4m-4 4l-4-4"/>',
        'open': '<path stroke-linecap="round" stroke-linejoin="round" d="M13.5 6H5.25A2.25 2.25 0 003 8.25v10.5A2.25 2.25 0 005.25 21h10.5A2.25 2.25 0 0018 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25"/>',
        'open-with': '<path stroke-linecap="round" stroke-linejoin="round" d="M3.75 9.776c.112-.017.227-.026.344-.026h15.812c.117 0 .232.009.344.026m-16.5 0a2.25 2.25 0 00-1.883 2.542l.857 6a2.25 2.25 0 002.227 1.932H19.05a2.25 2.25 0 002.227-1.932l.857-6a2.25 2.25 0 00-1.883-2.542m-16.5 0V6A2.25 2.25 0 016 3.75h3.879a1.5 1.5 0 011.06.44l2.122 2.12a1.5 1.5 0 001.06.44H18A2.25 2.25 0 0120.25 9v.776"/>',
        'copy': '<path stroke-linecap="round" stroke-linejoin="round" d="M15.75 17.25v3.375c0 .621-.504 1.125-1.125 1.125h-9.75a1.125 1.125 0 01-1.125-1.125V7.875c0-.621.504-1.125 1.125-1.125H6.75a9.06 9.06 0 011.5.124m7.5 10.376h3.375c.621 0 1.125-.504 1.125-1.125V11.25c0-4.46-3.243-8.161-7.5-8.876a9.06 9.06 0 00-1.5-.124H9.375c-.621 0-1.125.504-1.125 1.125v3.5m7.5 10.375H9.375a1.125 1.125 0 01-1.125-1.125v-9.25m12 6.625v-1.875a3.375 3.375 0 00-3.375-3.375h-1.5a1.125 1.125 0 01-1.125-1.125v-1.5a3.375 3.375 0 00-3.375-3.375H9.75"/>',
        'plugins': '<path stroke-linecap="round" stroke-linejoin="round" d="M14.25 6.087c0-.355.186-.676.401-.959.221-.29.349-.634.349-1.003 0-1.036-1.007-1.875-2.25-1.875s-2.25.84-2.25 1.875c0 .369.128.713.349 1.003.215.283.401.604.401.959v0a.64.64 0 01-.657.643 48.39 48.39 0 01-4.163-.3c.186 1.613.293 3.25.315 4.907a.656.656 0 01-.658.663v0c-.355 0-.676-.186-.959-.401a1.647 1.647 0 00-1.003-.349c-1.036 0-1.875 1.007-1.875 2.25s.84 2.25 1.875 2.25c.369 0 .713-.128 1.003-.349.283-.215.604-.401.959-.401v0c.31 0 .555.26.532.57a48.039 48.039 0 01-.642 5.056c1.518.19 3.058.309 4.616.354a.64.64 0 00.657-.643v0c0-.355-.186-.676-.401-.959a1.647 1.647 0 01-.349-1.003c0-1.035 1.008-1.875 2.25-1.875 1.243 0 2.25.84 2.25 1.875 0 .369-.128.713-.349 1.003-.215.283-.401.604-.401.959v0c0 .333.277.599.61.58a48.1 48.1 0 005.427-.63 48.05 48.05 0 00.582-4.717.532.532 0 00-.533-.57v0c-.355 0-.676.186-.959.401-.29.221-.634.349-1.003.349-1.035 0-1.875-1.007-1.875-2.25s.84-2.25 1.875-2.25c.37 0 .713.128 1.003.349.283.215.604.401.959.401v0a.656.656 0 00.658-.663 48.422 48.422 0 00-.37-5.36c-1.886.342-3.81.574-5.766.689a.578.578 0 01-.61-.58v0z"/>',
    };
    const path = icons[name];
    if (!path) return '';
    return `<svg class="card-menu-icon" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24">${path}</svg>`;
}

const cardMenuTooltips = {
    'open': 'Open with your default application',
    'open-with': 'Choose an application to open this clip',
    'copy-path': 'Create a temp file and copy its path to clipboard',
    'copy-file': 'Place file on clipboard for pasting into other apps',
    'copy-contents': 'Copy the raw text or data to clipboard',
    'save-file': 'Save a copy to your Downloads folder',
    'edit': 'Open in the built-in image editor for annotation',
    'tags': 'Add or remove tags',
    'metadata': 'View and edit file metadata',
    'set-expiration': 'Schedule auto-deletion after a time period',
    'cancel-expiration': 'Cancel the scheduled auto-deletion',
    'archive': 'Move to archive without deleting',
    'merge-duplicates': 'Find clips with identical content and merge them',
    'delete': 'Permanently delete -- this cannot be undone',
};

// Build the menu item list for ContextMenu.open()
function buildMenuItemList(clip) {
    const ct = clip.content_type || '';
    const items = [];

    // Open actions (top of menu)
    items.push({ id: 'open', label: 'Open', iconHtml: getMenuIcon('open'), tooltip: cardMenuTooltips['open'] });
    items.push({ id: 'open-with', label: 'Open With\u2026', iconHtml: getMenuIcon('open-with'), tooltip: cardMenuTooltips['open-with'] });

    items.push({ type: 'divider' });

    // Copy submenu
    const copyChildren = [
        { id: 'copy-path', label: 'Path', iconHtml: getMenuIcon('copy-path'), tooltip: cardMenuTooltips['copy-path'] },
        { id: 'copy-file', label: 'File', iconHtml: getMenuIcon('copy-file'), tooltip: cardMenuTooltips['copy-file'] },
    ];
    if (ct.startsWith('text/') || ct === 'application/json' || ct.startsWith('image/')) {
        copyChildren.push({ id: 'copy-contents', label: 'Contents', iconHtml: getMenuIcon('copy-contents'), tooltip: cardMenuTooltips['copy-contents'] });
    }
    items.push({ type: 'submenu', label: 'Copy', iconHtml: getMenuIcon('copy'), submenuId: 'copy', children: copyChildren });

    items.push({ id: 'save-file', label: 'Save', iconHtml: getMenuIcon('save'), tooltip: cardMenuTooltips['save-file'] });

    if (isEditableType(ct)) {
        items.push({ id: 'edit', label: 'Edit', iconHtml: getMenuIcon('edit'), tooltip: cardMenuTooltips['edit'] });
    }

    items.push({ id: 'tags', label: 'Tags', iconHtml: getMenuIcon('tags'), tooltip: cardMenuTooltips['tags'] });
    items.push({ id: 'metadata', label: 'Metadata', iconHtml: getMenuIcon('metadata'), tooltip: cardMenuTooltips['metadata'] });

    if (clip.expires_at) {
        items.push({ id: 'cancel-expiration', label: 'Cancel Expiration', iconHtml: getMenuIcon('cancel-expiration'), tooltip: cardMenuTooltips['cancel-expiration'] });
    } else {
        items.push({ id: 'set-expiration', label: 'Set Expiration', iconHtml: getMenuIcon('set-expiration'), tooltip: cardMenuTooltips['set-expiration'] });
    }

    items.push({
        id: 'archive',
        label: isViewingArchive ? 'Restore' : 'Archive',
        iconHtml: getMenuIcon(isViewingArchive ? 'restore' : 'archive'),
        tooltip: isViewingArchive ? 'Move back from archive' : cardMenuTooltips['archive'],
    });

    if (clip.duplicate_count > 0) {
        items.push({ id: 'merge-duplicates', label: 'Merge Duplicates', iconHtml: getMenuIcon('merge'), tooltip: cardMenuTooltips['merge-duplicates'] });
    }

    items.push({ id: 'delete', label: 'Delete', iconHtml: getMenuIcon('delete'), danger: true, tooltip: cardMenuTooltips['delete'] });

    // Plugin actions submenu
    if (pluginUIActions && pluginUIActions.card_actions && pluginUIActions.card_actions.length > 0) {
        const applicableActions = pluginUIActions.card_actions.filter(action =>
            shouldShowPluginAction(action, clip)
        );
        if (applicableActions.length > 0) {
            items.push({ type: 'divider' });
            const pluginChildren = applicableActions.map(action => ({
                id: 'plugin',
                label: escapeHTML(action.label),
                pluginId: action.plugin_id,
                actionId: action.id,
                hasOptions: action.options && action.options.length > 0,
                iconHtml: typeof getPluginIcon === 'function'
                    ? (getPluginIcon(action.icon) || getPluginIcon('bolt') || '')
                    : '',
            }));
            items.push({ type: 'submenu', label: 'Plugins', iconHtml: getMenuIcon('plugins'), submenuId: 'plugins', children: pluginChildren });
        }
    }

    return items;
}

// Handle plugin card action dispatch (extracted from app.js event delegation)
function handlePluginCardAction(dataset) {
    const pluginId = Number(dataset.pluginId);
    const actionId = dataset.actionId;
    const clipId = dataset.clipId;
    const hasOptions = dataset.hasOptions === 'true';

    // Verify plugin UI actions are loaded
    if (!pluginUIActions || !pluginUIActions.card_actions) {
        console.error('Plugin UI actions not loaded');
        if (typeof showToast === 'function') {
            showToast('Plugin actions not available. Try refreshing the page.', 'error');
        }
        return;
    }

    if (hasOptions && typeof openPluginOptionsDialog === 'function') {
        const pluginAction = pluginUIActions.card_actions.find(
            a => a.plugin_id === pluginId && a.id === actionId
        );
        if (pluginAction) {
            openPluginOptionsDialog(pluginAction, [Number(clipId)]);
        } else {
            console.error('Could not find plugin action:', pluginId, actionId);
            if (typeof showToast === 'function') {
                showToast('Plugin action not found', 'error');
            }
        }
    } else if (typeof executePluginAction === 'function') {
        const pluginAction = pluginUIActions.card_actions.find(
            a => a.plugin_id === pluginId && a.id === actionId
        );
        executePluginAction(pluginId, actionId, [Number(clipId)], {}, pluginAction && pluginAction.async);
    } else {
        console.error('Plugin action handler not available');
        if (typeof showToast === 'function') {
            showToast('Plugin system not initialized', 'error');
        }
    }
}

// Render card menu dropdown via ContextMenu
function renderCardMenu(clipId, button, clip) {
    const items = buildMenuItemList(clip);
    return ContextMenu.open(items, clipId, button, (action, id, item) => {
        if (action === 'plugin') {
            handlePluginCardAction({
                pluginId: item.dataset.pluginId,
                actionId: item.dataset.actionId,
                clipId: id,
                hasOptions: item.dataset.hasOptions,
            });
        } else {
            handleCardAction(action, id, button);
        }
    });
}

// Close any open card menu
function closeCardMenu() {
    ContextMenu.close();
}

// Handle built-in card actions
async function handleCardAction(action, clipId, triggerButton) {
    closeCardMenu();
    const id = Number(clipId);

    switch (action) {
        case 'open':
            try {
                await window.go.main.App.OpenClipWithDefaultApp(id);
            } catch (err) {
                showToast('Failed to open clip.');
            }
            break;
        case 'open-with':
            try {
                const appPath = await window.go.main.App.ChooseApplication();
                if (appPath) {
                    await window.go.main.App.OpenClipWithApp(id, appPath);
                }
            } catch (err) {
                showToast('Failed to open clip.');
            }
            break;
        case 'copy-path':
            saveTempFile(id);
            break;
        case 'copy-file':
            copyFileToClipboard(id);
            break;
        case 'copy-contents':
            copyClipContents(id);
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
        case 'metadata': {
            const card = gallery.querySelector(`li[data-id="${id}"]`);
            const clipData = card ? {
                filename: card.querySelector('.p-2\\.5 p')?.getAttribute('title') || '',
                content_type: card.dataset.type || '',
                size: Number(card.dataset.size) || 0,
                created_at: card.dataset.createdAt || '',
            } : null;
            openMetadataModal(id, clipData);
            break;
        }
        case 'set-expiration': {
            const card = gallery.querySelector(`li[data-id="${clipId}"]`);
            if (card) {
                const menuBtn = card.querySelector('[data-action="menu"]');
                openExpirationPopover(clipId, menuBtn || triggerButton);
            }
            break;
        }
        case 'cancel-expiration':
            cancelExpiration(id);
            break;
        case 'archive':
            toggleArchiveClip(id);
            break;
        case 'merge-duplicates':
            try {
                await window.go.main.App.MergeDuplicates(id);
                showToast('Merged duplicates', 'success');
                loadClips();
            } catch (err) {
                showToast('Failed to merge duplicates', 'error');
            }
            break;
        case 'delete':
            deleteClip(id);
            break;
    }
}

// Expiration preset popover
const EXPIRATION_PRESETS = [
    { label: '15m', minutes: 15 },
    { label: '1h', minutes: 60 },
    { label: '6h', minutes: 360 },
    { label: '24h', minutes: 1440 },
    { label: '7d', minutes: 10080 },
];

function openExpirationPopover(clipId, anchorElement, isBulk = false) {
    closeExpirationPopover();

    const popover = document.createElement('div');
    popover.className = 'expiration-popover fixed bg-white rounded-lg shadow-xl border border-stone-200 p-2 z-[60]';
    popover.setAttribute('role', 'menu');
    popover.setAttribute('aria-label', 'Set expiration');

    const row = document.createElement('div');
    row.className = 'flex items-center gap-1.5';

    EXPIRATION_PRESETS.forEach(preset => {
        const btn = document.createElement('button');
        btn.className = 'px-2.5 py-1.5 text-[11px] font-medium text-stone-600 bg-stone-100 hover:bg-stone-200 rounded-md transition-colors';
        btn.textContent = preset.label;
        btn.setAttribute('role', 'menuitem');
        btn.addEventListener('click', () => {
            closeExpirationPopover();
            if (isBulk) {
                bulkSetExpiration(Array.from(selectedIds), preset.minutes);
                selectedIds.clear();
            } else {
                setExpiration(Number(clipId), preset.minutes);
            }
        });
        row.appendChild(btn);
    });

    popover.appendChild(row);
    document.body.appendChild(popover);
    positionExpirationPopover(popover, anchorElement);
}

function positionExpirationPopover(popover, anchor) {
    const rect = anchor.getBoundingClientRect();
    const popoverRect = popover.getBoundingClientRect();
    const pad = 8;

    const spaceBelow = window.innerHeight - rect.bottom - pad;
    let top;
    if (spaceBelow >= popoverRect.height) {
        top = rect.bottom + pad;
    } else {
        top = rect.top - popoverRect.height - pad;
    }

    let left = rect.left + (rect.width / 2) - (popoverRect.width / 2);
    if (left < pad) left = pad;
    if (left + popoverRect.width > window.innerWidth - pad) {
        left = window.innerWidth - popoverRect.width - pad;
    }

    popover.style.left = `${left}px`;
    popover.style.top = `${top}px`;
}

function closeExpirationPopover() {
    const existing = document.querySelector('.expiration-popover');
    if (existing) existing.remove();
}

function renderDragHandle(clipId) {
    if (typeof canDragOut !== 'function' || !canDragOut()) {
        return '';
    }
    return `
        <span class="clip-drag-handle p-1 text-stone-400 hover:text-stone-600 hover:bg-stone-100 rounded transition-colors"
                data-action="drag-out"
                data-id="${clipId}"
                draggable="true"
                role="button"
                tabindex="0"
                aria-label="Drag clip to another app"
                title="Creates a temp file -- drag into another app to export"
            <svg class="clip-drag-icon-grip w-3 h-3" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24" aria-hidden="true">
                <path stroke-linecap="round" stroke-linejoin="round" d="M8 5h.01M8 12h.01M8 19h.01M16 5h.01M16 12h.01M16 19h.01" />
            </svg>
            <svg class="clip-drag-icon-progress w-3 h-3 hidden" viewBox="0 0 24 24" aria-hidden="true">
                <circle class="clip-drag-progress-track" cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="2"></circle>
                <circle class="clip-drag-progress-fill" cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="2"></circle>
            </svg>
            <svg class="clip-drag-icon-spinner w-3 h-3 hidden animate-spin" viewBox="0 0 24 24" aria-hidden="true">
                <circle class="opacity-25" cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="2"></circle>
                <path class="opacity-75" fill="currentColor" d="M12 3a9 9 0 0 1 9 9h-2a7 7 0 0 0-7-7z"></path>
            </svg>
        </span>
    `;
}

function setDragHandleMode(handle, mode) {
    const isArming = mode === 'arming';
    const isPreparing = mode === 'preparing';
    const isReady = mode === 'ready';

    handle.classList.toggle('is-hover-arming', isArming);
    handle.classList.toggle('is-preparing', isPreparing);
    handle.classList.toggle('is-ready', isReady);
    handle.setAttribute('aria-busy', isPreparing ? 'true' : 'false');
    handle.setAttribute('draggable', isReady ? 'true' : 'false');

    const gripIcon = handle.querySelector('.clip-drag-icon-grip');
    const progressIcon = handle.querySelector('.clip-drag-icon-progress');
    const spinnerIcon = handle.querySelector('.clip-drag-icon-spinner');
    if (gripIcon) {
        gripIcon.classList.toggle('hidden', isArming || isPreparing);
    }
    if (progressIcon) {
        progressIcon.classList.toggle('hidden', !isArming);
    }
    if (spinnerIcon) {
        spinnerIcon.classList.toggle('hidden', !isPreparing);
    }
}

async function ensureDragPrepared(handle, clipId) {
    if (typeof getPreparedDragItem === 'function') {
        const cached = getPreparedDragItem(clipId);
        if (cached) {
            setDragHandleMode(handle, 'ready');
            return cached;
        }
    }
    if (typeof prepareDrag !== 'function') {
        setDragHandleMode(handle, 'idle');
        return null;
    }

    setDragHandleMode(handle, 'preparing');
    beginGlobalDragPrepareCursor();
    try {
        const prepared = await prepareDrag(clipId);
        setDragHandleMode(handle, 'ready');
        return prepared;
    } catch (error) {
        console.error(`Failed to prepare clip ${clipId} for drag:`, error);
        setDragHandleMode(handle, 'idle');
        return null;
    } finally {
        endGlobalDragPrepareCursor();
    }
}

function setupDragHandle(handle, clipId) {
    const hoverPrepDelayMs = 1000;
    const id = Number(clipId);
    let armTimer = null;
    let prepPromise = null;
    let hoverLookupSeq = 0;
    let isHovering = false;

    const getPrepared = () => {
        if (typeof getPreparedDragItem !== 'function') {
            return null;
        }
        return getPreparedDragItem(id);
    };

    const clearArmTimer = () => {
        if (armTimer) {
            clearTimeout(armTimer);
            armTimer = null;
        }
    };

    const refreshMode = () => {
        if (prepPromise) {
            setDragHandleMode(handle, 'preparing');
            return;
        }
        const prepared = getPrepared();
        if (prepared) {
            setDragHandleMode(handle, 'ready');
            return;
        }
        if (isHovering && armTimer) {
            setDragHandleMode(handle, 'arming');
            return;
        }
        setDragHandleMode(handle, 'idle');
    };

    const startPrepare = () => {
        if (prepPromise) {
            return prepPromise;
        }
        prepPromise = ensureDragPrepared(handle, id)
            .catch((error) => {
                console.error(`Failed preparing drag for clip ${id}:`, error);
                return null;
            })
            .finally(() => {
                prepPromise = null;
                refreshMode();
            });
        return prepPromise;
    };

    const scheduleHoverPrep = () => {
        const seq = ++hoverLookupSeq;
        const beginArming = () => {
            if (!isHovering || seq !== hoverLookupSeq) {
                return;
            }
            clearArmTimer();
            setDragHandleMode(handle, 'arming');
            armTimer = setTimeout(() => {
                armTimer = null;
                startPrepare();
            }, hoverPrepDelayMs);
        };

        const prepared = getPrepared();
        if (prepared) {
            setDragHandleMode(handle, 'ready');
            return;
        }
        if (prepPromise) {
            setDragHandleMode(handle, 'preparing');
            return;
        }
        if (typeof lookupPreparedDrag !== 'function') {
            beginArming();
            return;
        }

        lookupPreparedDrag(id).then((existing) => {
            if (!isHovering || seq !== hoverLookupSeq) {
                return;
            }
            if (existing) {
                setDragHandleMode(handle, 'ready');
                return;
            }
            beginArming();
        }).catch((error) => {
            console.error(`Failed lookup for prepared drag clip ${id}:`, error);
            beginArming();
        });
    };

    handle.addEventListener('pointerenter', () => {
        isHovering = true;
        scheduleHoverPrep();
    });

    handle.addEventListener('pointerleave', () => {
        isHovering = false;
        hoverLookupSeq += 1;
        clearArmTimer();
        refreshMode();
    });

    handle.addEventListener('pointerdown', (e) => {
        e.stopPropagation();

        const prepared = getPrepared();
        if (!prepared) {
            e.preventDefault();
            clearArmTimer();
            startPrepare();
        }
    });

    // Native drag must be triggered directly from a mouse event for reliable macOS behavior.
    handle.addEventListener('mousedown', (e) => {
        if (e.button !== 0) {
            return;
        }
        const prepared = getPrepared();
        if (!prepared) {
            return;
        }
        if (typeof canUseNativeDragOut === 'function' && canUseNativeDragOut() && typeof startNativeDrag === 'function') {
            e.preventDefault();
            e.stopPropagation();
            clearArmTimer();
            startNativeDrag(id, prepared).catch((error) => {
                console.error(`Failed native drag for clip ${id}:`, error);
            }).finally(() => {
                refreshMode();
            });
        }
    });

    handle.addEventListener('click', (e) => {
        e.preventDefault();
        e.stopPropagation();
    });

    handle.addEventListener('dragstart', (e) => {
        e.stopPropagation();

        if (handle.classList.contains('is-hover-arming') || handle.classList.contains('is-preparing')) {
            e.preventDefault();
            clearArmTimer();
            startPrepare();
            return;
        }

        if (!e.dataTransfer || typeof setDragData !== 'function') {
            e.preventDefault();
            return;
        }

        const prepared = getPrepared();
        const strategy = typeof getDragStrategy === 'function' ? getDragStrategy() : '';
        if (!prepared || !setDragData(e.dataTransfer, prepared, strategy)) {
            e.preventDefault();
            clearArmTimer();
            startPrepare();
            return;
        }

        if (e.dataTransfer) {
            e.dataTransfer.dropEffect = 'copy';
        }
        e.dataTransfer.effectAllowed = 'copy';
        window.__internalDragActive = true;
    });

    handle.addEventListener('dragend', () => {
        window.__internalDragActive = false;
        clearArmTimer();
        refreshMode();
    });

    refreshMode();
}

function resetDragHandleStates() {
    if (!gallery) {
        return;
    }
    const handles = gallery.querySelectorAll('[data-action="drag-out"]');
    handles.forEach((handle) => {
        const id = Number(handle.dataset.id);
        const prepared = typeof getPreparedDragItem === 'function' ? getPreparedDragItem(id) : null;
        setDragHandleMode(handle, prepared ? 'ready' : 'idle');
    });
}

async function createClipCard(clip, options = {}) {
    const card = document.createElement('li');
    card.className = 'bg-white rounded-md border border-stone-200 overflow-hidden flex flex-col transition-all duration-150 hover:border-stone-300 relative group [&.has-checked]:ring-2 [&.has-checked]:ring-stone-800';
    card.dataset.id = clip.id;
    card.dataset.filename = (clip.filename || '').toLowerCase();
    card.dataset.type = (clip.content_type || '').toLowerCase();
    card.dataset.size = clip.size || 0;
    card.dataset.createdAt = clip.created_at || '';
    if (clip.expires_at) {
        card.dataset.expiresAt = clip.expires_at;
    }
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

    let previewHTML;

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
        const remaining = formatTimeRemaining(clip.expires_at);
        expirationBadge = `<div class="absolute top-2 left-2 bg-stone-700 text-white text-[8px] font-semibold px-1.5 py-0.5 rounded z-20 uppercase tracking-wide">
            Temp · ${remaining}
        </div>`;
    }
    const dragHandleHTML = renderDragHandle(clip.id);

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
                <div class="flex items-center gap-1.5"><span class="text-[9px] font-medium text-stone-400 uppercase tracking-wide">${getFriendlyFileType(clip.content_type, clip.filename)}</span>${clip.duplicate_count > 0 ? `<span class="dedup-badge text-[9px] font-medium text-stone-400 bg-stone-100 border border-stone-200 rounded px-1">${clip.duplicate_count + 1} copies</span>` : ''}</div>
                <div class="flex items-center gap-1">
                    ${dragHandleHTML}
                    <button class="card-menu-trigger p-1 text-stone-400 hover:text-stone-600 hover:bg-stone-100 rounded transition-colors"
                            data-action="menu"
                            data-id="${clip.id}"
                            aria-label="Actions"
                            aria-haspopup="true"
                            aria-expanded="false"
                            title="More actions for this clip"
                        <svg class="w-3 h-3" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" d="M12 6.75a.75.75 0 110-1.5.75.75 0 010 1.5zM12 12.75a.75.75 0 110-1.5.75.75 0 010 1.5zM12 18.75a.75.75 0 110-1.5.75.75 0 010 1.5z" />
                        </svg>
                    </button>
                </div>
            </div>
        </div>
    `;

    // Menu trigger listener
    const menuTrigger = card.querySelector('[data-action="menu"]');
    menuTrigger.addEventListener('click', (e) => {
        e.stopPropagation();
        renderCardMenu(clip.id, e.currentTarget, clip);
    });

    // Right-click anywhere on the card opens the same actions menu as the three-dot trigger.
    card.addEventListener('contextmenu', (e) => {
        e.preventDefault();
        e.stopPropagation();
        renderCardMenu(clip.id, menuTrigger, clip);
    });

    const dragHandle = card.querySelector('[data-action="drag-out"]');
    if (dragHandle) {
        setupDragHandle(dragHandle, clip.id);
    }

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

        // Shift-click range selection
        if (e.target.checked && checkbox._shiftEvent && lastCheckedCheckbox && lastCheckedCheckbox !== checkbox) {
            const allCards = Array.from(gallery.querySelectorAll(':scope > li'));
            const currentIndex = allCards.indexOf(card);
            const lastIndex = allCards.indexOf(lastCheckedCheckbox.closest('li'));
            if (currentIndex !== -1 && lastIndex !== -1) {
                const start = Math.min(currentIndex, lastIndex);
                const end = Math.max(currentIndex, lastIndex);
                for (let i = start; i <= end; i++) {
                    const cb = allCards[i].querySelector('.clip-checkbox');
                    if (cb && !cb.checked) {
                        cb.checked = true;
                        const cbId = Number(cb.dataset.id);
                        selectedIds.add(cbId);
                        allCards[i].classList.add('has-checked');
                    }
                }
            }
        }
        lastCheckedCheckbox = checkbox;

        // Sync Select All checkbox
        const allCheckboxes = Array.from(gallery.querySelectorAll('.clip-checkbox'));
        selectAllCheckbox.checked = allCheckboxes.length > 0 && allCheckboxes.every(cb => cb.checked);

        updateBulkToolbar();
    });

    // Prevent lightbox trigger if clicking checkbox; track shift state for range selection
    checkbox.addEventListener('click', (e) => {
        e.stopPropagation();
        checkbox._shiftEvent = e.shiftKey;
    });

    // Lightbox trigger logic
    if (clip.content_type.startsWith('image/')) {
        if (options.prepend) {
            imageClips.unshift(clip);
        } else {
            imageClips.push(clip);
        }
        card.querySelector('[data-action="open-lightbox"]').addEventListener('click', () => {
            const idx = imageClips.findIndex(c => c.id === clip.id);
            if (idx !== -1) openLightbox(idx);
        });

        // Bump lightbox index if prepending while lightbox is open
        if (options.prepend && lightbox.classList.contains('active')) {
            currentLightboxIndex++;
        }

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

    if (options.prepend) {
        gallery.prepend(card);
    } else {
        gallery.appendChild(card);
    }
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

        // Show "Clear Expiry" if any selected clip has expiration
        const bulkCancelExpiryBtn = document.getElementById('bulk-cancel-expiry-btn');
        if (bulkCancelExpiryBtn) {
            const anyExpiring = Array.from(selectedIds).some(id => {
                const card = gallery.querySelector(`li[data-id="${id}"]`);
                return card && card.dataset.expiresAt;
            });
            if (anyExpiring) {
                bulkCancelExpiryBtn.classList.remove('hidden');
            } else {
                bulkCancelExpiryBtn.classList.add('hidden');
            }
        }
    } else {
        bulkToolbar.classList.remove('translate-y-0', 'opacity-100', 'pointer-events-auto');
        bulkToolbar.classList.add('translate-y-4', 'opacity-0', 'pointer-events-none');
        selectAllCheckbox.checked = false;
        bulkCompareBtn.classList.add('hidden');
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
    lastCheckedCheckbox = null;
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
    }

    toggleArchiveViewBtn.setAttribute('aria-pressed', isViewingArchive);
    if (isViewingArchive) {
        archiveBtnText.textContent = "Active";
        toggleArchiveViewBtn.classList.add('bg-stone-800', 'text-white', 'border-stone-800');
        toggleArchiveViewBtn.classList.remove('border-stone-200', 'text-stone-600', 'hover:border-stone-300', 'hover:bg-stone-100');
        headerArchiveBtn.setAttribute('aria-pressed', 'true');
        headerArchiveBtn.classList.add('bg-stone-800', 'text-white', 'border-stone-800');
        headerArchiveBtn.classList.remove('border-stone-200', 'text-stone-500', 'hover:border-stone-300', 'hover:bg-stone-100');
    } else {
        archiveBtnText.textContent = "Archive";
        toggleArchiveViewBtn.classList.remove('bg-stone-800', 'text-white', 'border-stone-800');
        toggleArchiveViewBtn.classList.add('border-stone-200', 'text-stone-600', 'hover:border-stone-300', 'hover:bg-stone-100');
        headerArchiveBtn.setAttribute('aria-pressed', 'false');
        headerArchiveBtn.classList.remove('bg-stone-800', 'text-white', 'border-stone-800');
        headerArchiveBtn.classList.add('border-stone-200', 'text-stone-500', 'hover:border-stone-300', 'hover:bg-stone-100');
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
