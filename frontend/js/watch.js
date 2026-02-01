// --- Watch View State ---
let isViewingWatch = false;
let watchFolders = [];
let editingFolderId = null;

// --- Elements ---
const toggleWatchViewBtn = document.getElementById('toggle-watch-view-btn');
const watchBtnText = document.getElementById('watch-btn-text');
const watchIndicator = document.getElementById('watch-indicator');
const watchView = document.getElementById('watch-view');
const watchFolderList = document.getElementById('watch-folder-list');
const globalWatchToggle = document.getElementById('global-watch-toggle');
const globalWatchLabel = document.getElementById('global-watch-label');
const watchFolderCount = document.getElementById('watch-folder-count');
const addFolderZone = document.getElementById('add-folder-zone');
const addFolderBtn = document.getElementById('add-folder-btn');

// Folder modal elements
const folderModal = document.getElementById('folder-modal');
const folderModalTitle = document.getElementById('folder-modal-title');
const folderModalPath = document.getElementById('folder-modal-path');
const filterAll = document.getElementById('filter-all');
const filterImages = document.getElementById('filter-images');
const filterDocuments = document.getElementById('filter-documents');
const filterVideos = document.getElementById('filter-videos');
const filterRegex = document.getElementById('filter-regex');
const processExisting = document.getElementById('process-existing');
const autoArchive = document.getElementById('auto-archive');
const folderModalCancel = document.getElementById('folder-modal-cancel');
const folderModalSave = document.getElementById('folder-modal-save');

// --- View Toggle ---
function toggleWatchView() {
    isViewingWatch = !isViewingWatch;
    toggleWatchViewBtn.setAttribute('aria-pressed', isViewingWatch);

    if (isViewingWatch) {
        // Switch to watch view
        watchBtnText.textContent = 'Clips';
        toggleWatchViewBtn.classList.add('bg-stone-800', 'text-white', 'border-stone-800');
        toggleWatchViewBtn.classList.remove('border-stone-200', 'text-stone-600');

        uploadSection.classList.add('hidden');
        gallery.parentElement.classList.add('hidden');
        watchView.classList.remove('hidden');

        loadWatchFolders();
    } else {
        // Switch back to clips view
        watchBtnText.textContent = 'Watch';
        toggleWatchViewBtn.classList.remove('bg-stone-800', 'text-white', 'border-stone-800');
        toggleWatchViewBtn.classList.add('border-stone-200', 'text-stone-600');

        uploadSection.classList.remove('hidden');
        gallery.parentElement.classList.remove('hidden');
        watchView.classList.add('hidden');
    }
}

// --- Load Watch Status ---
async function updateWatchIndicator() {
    try {
        const status = await window.go.main.App.GetWatchStatus();
        if (status.is_watching) {
            watchIndicator.classList.remove('hidden');
        } else {
            watchIndicator.classList.add('hidden');
        }
    } catch (error) {
        console.error('Failed to get watch status:', error);
    }
}

// --- Load Folders ---
async function loadWatchFolders() {
    try {
        const globalPaused = await window.go.main.App.GetGlobalWatchPaused();
        watchFolders = await window.go.main.App.GetWatchedFolders();

        // Update global toggle
        globalWatchToggle.checked = !globalPaused;
        globalWatchLabel.textContent = globalPaused ? 'Watching paused' : 'Watching active';

        // Update count
        const activeCount = watchFolders.filter(f => !f.is_paused && f.exists).length;
        watchFolderCount.textContent = `${watchFolders.length} folder${watchFolders.length !== 1 ? 's' : ''}`;

        // Render folder cards
        renderWatchFolderList();

        // Update indicator
        updateWatchIndicator();
    } catch (error) {
        console.error('Failed to load watch folders:', error);
    }
}

// --- Render Folder List ---
function renderWatchFolderList() {
    watchFolderList.innerHTML = '';

    if (watchFolders.length === 0) {
        watchFolderList.innerHTML = '<li class="text-center text-sm text-stone-400 py-8">No watched folders yet</li>';
        return;
    }

    for (const folder of watchFolders) {
        const card = createWatchFolderCard(folder);
        watchFolderList.appendChild(card);
    }
}

// --- Create Folder Card ---
function createWatchFolderCard(folder) {
    const li = document.createElement('li');
    li.className = 'bg-white border border-stone-200 rounded-lg p-4 flex items-center justify-between gap-4';
    li.dataset.id = folder.id;

    // Filter description
    let filterDesc = 'All files';
    if (folder.filter_mode === 'presets' && folder.filter_presets?.length > 0) {
        filterDesc = folder.filter_presets.map(p => p.charAt(0).toUpperCase() + p.slice(1)).join(', ');
    } else if (folder.filter_mode === 'custom' && folder.filter_regex) {
        filterDesc = `Regex: ${folder.filter_regex}`;
    }

    const pausedClass = folder.is_paused ? 'opacity-50' : '';
    const notExistsWarning = !folder.exists
        ? '<span class="text-[10px] text-amber-600 bg-amber-50 px-1.5 py-0.5 rounded">Folder not found</span>'
        : '';

    li.innerHTML = `
        <div class="flex-1 min-w-0 ${pausedClass}">
            <div class="flex items-center gap-2 mb-1">
                <p class="text-sm font-medium text-stone-700 truncate">${escapeHTML(folder.path)}</p>
                ${notExistsWarning}
            </div>
            <p class="text-[11px] text-stone-400">
                ${filterDesc}
                ${folder.auto_archive ? ' • Auto-archive' : ''}
                ${folder.is_paused ? ' • <span class="text-amber-500">Paused</span>' : ''}
            </p>
        </div>
        <div class="flex items-center gap-1">
            <button class="p-2 text-stone-400 hover:text-stone-600 hover:bg-stone-100 rounded-md transition-colors"
                    data-action="toggle-pause" title="${folder.is_paused ? 'Resume' : 'Pause'}">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    ${folder.is_paused
                        ? '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z"></path><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path>'
                        : '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z"></path>'
                    }
                </svg>
            </button>
            <button class="p-2 text-stone-400 hover:text-red-500 hover:bg-red-50 rounded-md transition-colors"
                    data-action="remove" title="Remove">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M6 18L18 6M6 6l12 12"></path>
                </svg>
            </button>
        </div>
    `;

    // Event listeners
    li.querySelector('[data-action="toggle-pause"]').addEventListener('click', () => toggleFolderPause(folder.id, !folder.is_paused));
    li.querySelector('[data-action="remove"]').addEventListener('click', () => removeWatchFolder(folder.id));

    return li;
}

// --- Toggle Folder Pause ---
async function toggleFolderPause(id, paused) {
    try {
        await window.go.main.App.SetFolderPaused(id, paused);
        await window.go.main.App.RefreshWatches();
        loadWatchFolders();
    } catch (error) {
        console.error('Failed to toggle folder pause:', error);
        showToast('Failed to update folder');
    }
}

// --- Remove Folder ---
async function removeWatchFolder(id) {
    showConfirmDialog('Remove Folder', 'Stop watching this folder?', async () => {
        try {
            await window.go.main.App.RemoveWatchedFolder(id);
            await window.go.main.App.RefreshWatches();
            loadWatchFolders();
            showToast('Folder removed');
        } catch (error) {
            console.error('Failed to remove folder:', error);
            showToast('Failed to remove folder');
        }
    });
}

// --- Global Pause Toggle ---
async function toggleGlobalPause() {
    const paused = !globalWatchToggle.checked;
    try {
        await window.go.main.App.SetGlobalWatchPaused(paused);
        await window.go.main.App.RefreshWatches();
        globalWatchLabel.textContent = paused ? 'Watching paused' : 'Watching active';
        updateWatchIndicator();
    } catch (error) {
        console.error('Failed to toggle global pause:', error);
        showToast('Failed to update watch status');
        globalWatchToggle.checked = !globalWatchToggle.checked; // Revert
    }
}

// --- Add Folder ---
async function openAddFolderDialog() {
    try {
        const path = await window.go.main.App.SelectFolder();
        if (!path) return; // User cancelled

        openFolderModal(path);
    } catch (error) {
        console.error('Failed to select folder:', error);
    }
}

function openFolderModal(path) {
    editingFolderId = null;
    folderModalTitle.textContent = 'Add Watched Folder';
    folderModalPath.textContent = path;
    folderModalPath.dataset.path = path;
    folderModalSave.textContent = 'Add Folder';

    // Reset form
    filterAll.checked = true;
    filterImages.checked = false;
    filterDocuments.checked = false;
    filterVideos.checked = false;
    filterRegex.value = '';
    processExisting.checked = false;
    autoArchive.checked = false;

    updateFilterState();

    // Show modal
    folderModal.classList.remove('opacity-0', 'pointer-events-none');
    folderModal.classList.add('opacity-100');
    const innerDiv = folderModal.querySelector('div');
    if (innerDiv) {
        innerDiv.classList.remove('scale-95');
        innerDiv.classList.add('scale-100');
    }
}

function closeFolderModal() {
    folderModal.classList.add('opacity-0', 'pointer-events-none');
    folderModal.classList.remove('opacity-100');
    const innerDiv = folderModal.querySelector('div');
    if (innerDiv) {
        innerDiv.classList.add('scale-95');
        innerDiv.classList.remove('scale-100');
    }
}

function updateFilterState() {
    const allChecked = filterAll.checked;
    filterImages.disabled = allChecked;
    filterDocuments.disabled = allChecked;
    filterVideos.disabled = allChecked;

    if (allChecked) {
        filterImages.checked = false;
        filterDocuments.checked = false;
        filterVideos.checked = false;
    }
}

async function saveFolderConfig() {
    const path = folderModalPath.dataset.path;

    let filterMode = 'all';
    let filterPresets = [];

    if (!filterAll.checked) {
        if (filterImages.checked) filterPresets.push('images');
        if (filterDocuments.checked) filterPresets.push('documents');
        if (filterVideos.checked) filterPresets.push('videos');

        if (filterPresets.length > 0) {
            filterMode = 'presets';
        } else if (filterRegex.value.trim()) {
            filterMode = 'custom';
        }
    }

    const config = {
        path: path,
        filter_mode: filterMode,
        filter_presets: filterPresets,
        filter_regex: filterRegex.value.trim(),
        process_existing: processExisting.checked,
        auto_archive: autoArchive.checked
    };

    try {
        const folder = await window.go.main.App.AddWatchedFolder(config);
        await window.go.main.App.RefreshWatches();

        // Process existing if requested
        if (config.process_existing && folder) {
            await window.go.main.App.ProcessExistingFilesInFolder(folder.id);
        }

        closeFolderModal();
        loadWatchFolders();
        showToast('Folder added');
    } catch (error) {
        console.error('Failed to add folder:', error);
        showToast('Failed to add folder: ' + error.message);
    }
}

// --- Event Listeners ---
toggleWatchViewBtn.addEventListener('click', toggleWatchView);
globalWatchToggle.addEventListener('change', toggleGlobalPause);
addFolderBtn.addEventListener('click', openAddFolderDialog);
addFolderZone.addEventListener('click', (e) => {
    if (e.target !== addFolderBtn) openAddFolderDialog();
});

// Filter checkbox logic
filterAll.addEventListener('change', updateFilterState);
filterImages.addEventListener('change', () => { if (filterImages.checked) filterAll.checked = false; });
filterDocuments.addEventListener('change', () => { if (filterDocuments.checked) filterAll.checked = false; });
filterVideos.addEventListener('change', () => { if (filterVideos.checked) filterAll.checked = false; });

// Modal buttons
folderModalCancel.addEventListener('click', closeFolderModal);
folderModalSave.addEventListener('click', saveFolderConfig);
folderModal.addEventListener('click', (e) => {
    if (e.target === folderModal) closeFolderModal();
});

// Drag and drop for folders
addFolderZone.addEventListener('dragover', (e) => {
    e.preventDefault();
    addFolderZone.classList.add('border-stone-400', 'bg-stone-50');
});

addFolderZone.addEventListener('dragleave', () => {
    addFolderZone.classList.remove('border-stone-400', 'bg-stone-50');
});

addFolderZone.addEventListener('drop', async (e) => {
    e.preventDefault();
    addFolderZone.classList.remove('border-stone-400', 'bg-stone-50');

    // Check if it's a folder (Wails should provide path via drop)
    const items = e.dataTransfer.items;
    if (items && items.length > 0) {
        // In Wails, dropped folders come through as file paths
        // We need to handle this via Wails events
        showToast('Use the Add Folder button to select folders');
    }
});

// Wails events for watch notifications
window.runtime.EventsOn('watch:error', (data) => {
    showToast(`Failed to import ${data.file}: ${data.error}`);
});

window.runtime.EventsOn('watch:import', (filename) => {
    // Refresh clips if not viewing watch or archive
    if (!isViewingWatch && !isViewingArchive) {
        loadClips();
    }
    showToast(`Imported: ${filename}`);
});

// Initial status check
updateWatchIndicator();
