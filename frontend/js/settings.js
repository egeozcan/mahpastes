// --- Settings Module ---

// Elements
const settingsModal = document.getElementById('settings-modal');
const openSettingsBtn = document.getElementById('open-settings-btn');
const settingsCloseBtn = document.getElementById('settings-close');
const settingsSaveBtn = document.getElementById('settings-save');

function openSettings() {
    renderHiddenTagsSettings();
    settingsModal.classList.remove('opacity-0', 'pointer-events-none');
    settingsModal.classList.add('opacity-100');
    settingsModal.querySelector(':scope > div').classList.remove('scale-95');
    settingsModal.querySelector(':scope > div').classList.add('scale-100');
}

function closeSettings() {
    settingsModal.classList.add('opacity-0', 'pointer-events-none');
    settingsModal.classList.remove('opacity-100');
    settingsModal.querySelector(':scope > div').classList.add('scale-95');
    settingsModal.querySelector(':scope > div').classList.remove('scale-100');
    renderTagFilterDropdown();
    loadClips();
}

async function saveSettings() {
    try {
        showToast('Settings saved');
        closeSettings();
    } catch (error) {
        console.error('Failed to save settings:', error);
        showToast('Failed to save settings');
    }
}

// Event listeners
openSettingsBtn.addEventListener('click', openSettings);
settingsCloseBtn.addEventListener('click', closeSettings);
settingsSaveBtn.addEventListener('click', saveSettings);
settingsModal.addEventListener('click', (e) => {
    if (e.target === settingsModal) closeSettings();
});

// --- Backup & Restore ---

const createBackupBtn = document.getElementById('create-backup-btn');
const restoreBackupBtn = document.getElementById('restore-backup-btn');
const restoreConfirmDialog = document.getElementById('restore-confirm-dialog');
const restoreConfirmCancel = document.getElementById('restore-confirm-cancel');
const restoreConfirmYes = document.getElementById('restore-confirm-yes');
const restoreBackupInfo = document.getElementById('restore-backup-info');

let pendingRestorePath = null;

async function createBackup() {
    try {
        createBackupBtn.disabled = true;
        createBackupBtn.textContent = 'Creating...';

        const savedPath = await window.go.main.App.ShowCreateBackupDialog();

        if (savedPath) {
            showToast('Backup created successfully');
        }
    } catch (error) {
        console.error('Failed to create backup:', error);
        showToast('Failed to create backup: ' + error.message);
    } finally {
        createBackupBtn.disabled = false;
        createBackupBtn.textContent = 'Create Backup';
    }
}

async function selectRestoreBackup() {
    try {
        const result = await window.go.main.App.ShowRestoreBackupDialog();

        if (!result || !result[0]) {
            return; // User cancelled or no manifest
        }

        const manifest = result[0];
        const backupPath = result[1];

        // Store path for confirmation
        pendingRestorePath = backupPath;

        // Format backup info
        const createdDate = new Date(manifest.created_at).toLocaleString();
        restoreBackupInfo.innerHTML = `
            <div class="flex justify-between py-1 border-b border-stone-100">
                <span class="text-stone-500">Backup created:</span>
                <span class="font-medium">${createdDate}</span>
            </div>
            <div class="flex justify-between py-1 border-b border-stone-100">
                <span class="text-stone-500">App version:</span>
                <span class="font-medium">${manifest.app_version}</span>
            </div>
            <div class="pt-2">
                <span class="text-stone-500">This backup contains:</span>
                <ul class="mt-1 space-y-1 pl-4">
                    <li class="flex items-center gap-1">
                        <span class="w-1.5 h-1.5 rounded-full bg-stone-400"></span>
                        ${manifest.summary.clips} clips
                    </li>
                    <li class="flex items-center gap-1">
                        <span class="w-1.5 h-1.5 rounded-full bg-stone-400"></span>
                        ${manifest.summary.tags} tags
                    </li>
                    <li class="flex items-center gap-1">
                        <span class="w-1.5 h-1.5 rounded-full bg-stone-400"></span>
                        ${manifest.summary.plugins} plugins
                    </li>
                    <li class="flex items-center gap-1">
                        <span class="w-1.5 h-1.5 rounded-full bg-stone-400"></span>
                        ${manifest.summary.watch_folders} watch folders <span class="text-stone-400">(will be paused)</span>
                    </li>
                </ul>
            </div>
        `;

        // Show confirmation dialog
        showRestoreConfirmDialog();

    } catch (error) {
        console.error('Failed to select backup:', error);
        showToast('Failed to read backup: ' + error.message);
    }
}

function showRestoreConfirmDialog() {
    restoreConfirmDialog.classList.remove('opacity-0', 'pointer-events-none');
    restoreConfirmDialog.classList.add('opacity-100');
    restoreConfirmDialog.querySelector(':scope > div').classList.remove('scale-95');
    restoreConfirmDialog.querySelector(':scope > div').classList.add('scale-100');
}

function hideRestoreConfirmDialog() {
    restoreConfirmDialog.classList.add('opacity-0', 'pointer-events-none');
    restoreConfirmDialog.classList.remove('opacity-100');
    restoreConfirmDialog.querySelector(':scope > div').classList.add('scale-95');
    restoreConfirmDialog.querySelector(':scope > div').classList.remove('scale-100');
    pendingRestorePath = null;
}

async function confirmRestore() {
    if (!pendingRestorePath) {
        hideRestoreConfirmDialog();
        return;
    }

    try {
        restoreConfirmYes.disabled = true;
        restoreConfirmYes.textContent = 'Restoring...';

        await window.go.main.App.ConfirmRestoreBackup(pendingRestorePath);

        hideRestoreConfirmDialog();
        closeSettings();
        showToast('Backup restored successfully');

        // Reload the page to refresh all data
        setTimeout(() => {
            window.location.reload();
        }, 500);

    } catch (error) {
        console.error('Failed to restore backup:', error);
        showToast('Failed to restore: ' + error.message);
    } finally {
        restoreConfirmYes.disabled = false;
        restoreConfirmYes.textContent = 'Delete & Restore';
    }
}

// Event listeners for backup
createBackupBtn.addEventListener('click', createBackup);
restoreBackupBtn.addEventListener('click', selectRestoreBackup);
restoreConfirmCancel.addEventListener('click', hideRestoreConfirmDialog);
restoreConfirmYes.addEventListener('click', confirmRestore);
restoreConfirmDialog.addEventListener('click', (e) => {
    if (e.target === restoreConfirmDialog) hideRestoreConfirmDialog();
});

// --- Hidden Tags ---

const hiddenTagsList = document.getElementById('hidden-tags-list');

function renderHiddenTagsSettings() {
    if (!hiddenTagsList) return;

    hiddenTagsList.innerHTML = '';

    if (allTags.length === 0) {
        hiddenTagsList.innerHTML = '<p class="text-stone-400 text-xs">No tags yet</p>';
        return;
    }

    allTags.forEach(tag => {
        const isHidden = hiddenTags.includes(tag.id);
        const row = document.createElement('label');
        row.className = 'flex items-center justify-between py-2 px-1 cursor-pointer hover:bg-stone-50 rounded transition-colors';
        row.dataset.testid = `hidden-tag-row-${tag.name}`;
        row.innerHTML = `
            <div class="flex items-center gap-2">
                <span class="w-2.5 h-2.5 rounded-full flex-shrink-0" style="background-color: ${tag.color}"></span>
                <span class="text-xs font-medium text-stone-700">${escapeHtml(tag.name)}</span>
                <span class="text-[10px] text-stone-400">${tag.count}</span>
            </div>
            <div class="relative inline-flex items-center cursor-pointer">
                <input type="checkbox" class="sr-only peer" data-testid="hidden-tag-toggle-${tag.name}" ${isHidden ? 'checked' : ''}>
                <div class="w-8 h-4 bg-stone-300 rounded-full peer peer-checked:bg-stone-800 after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:rounded-full after:h-3 after:w-3 after:transition-all peer-checked:after:translate-x-4"></div>
            </div>
        `;

        const checkbox = row.querySelector('input');
        checkbox.addEventListener('change', () => toggleHiddenTag(tag.id, checkbox.checked));

        hiddenTagsList.appendChild(row);
    });
}

async function toggleHiddenTag(tagId, hidden) {
    if (hidden) {
        if (!hiddenTags.includes(tagId)) {
            hiddenTags.push(tagId);
        }
    } else {
        const idx = hiddenTags.indexOf(tagId);
        if (idx !== -1) {
            hiddenTags.splice(idx, 1);
        }
    }

    try {
        await window.go.main.App.SetHiddenTags(hiddenTags);
    } catch (error) {
        console.error('Error saving hidden tags:', error);
        showToast('Failed to save hidden tags setting.');
    }
}
