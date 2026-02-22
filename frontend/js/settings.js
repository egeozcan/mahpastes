// --- Settings Module ---

// Elements
const settingsModal = document.getElementById('settings-modal');
const openSettingsBtn = document.getElementById('open-settings-btn');
const settingsCloseBtn = document.getElementById('settings-close');
const settingsSaveBtn = document.getElementById('settings-save');

function openSettings() {
    renderHiddenTagsSettings();
    loadUpdateInterval();
    renderShortcutsSettings();
    settingsModal.classList.remove('opacity-0', 'pointer-events-none');
    settingsModal.classList.add('opacity-100');
    settingsModal.querySelector(':scope > div').classList.remove('scale-95');
    settingsModal.querySelector(':scope > div').classList.add('scale-100');
}

function closeSettings() {
    stopRecording();
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
        const isHidden = getHiddenTags().includes(tag.id);
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
    const current = getHiddenTags();
    if (hidden) {
        if (!current.includes(tagId)) {
            setHiddenTagsState([...current, tagId]);
        }
    } else {
        setHiddenTagsState(current.filter(id => id !== tagId));
    }

    try {
        await window.go.main.App.SetHiddenTags(getHiddenTags());
    } catch (error) {
        console.error('Error saving hidden tags:', error);
        showToast('Failed to save hidden tags setting.');
    }
}

// --- Plugin Update Interval ---
const updateIntervalSelect = document.getElementById('update-interval-select');

async function loadUpdateInterval() {
    try {
        const interval = await window.go.main.PluginService.GetUpdateCheckInterval();
        if (updateIntervalSelect && interval) {
            updateIntervalSelect.value = interval;
        }
    } catch (error) {
        console.error('Failed to load update interval:', error);
    }
}

if (updateIntervalSelect) {
    updateIntervalSelect.addEventListener('change', async () => {
        try {
            await window.go.main.PluginService.SetUpdateCheckInterval(updateIntervalSelect.value);
            showToast('Update check interval saved');
        } catch (error) {
            console.error('Failed to save update interval:', error);
            showToast('Failed to save setting');
        }
    });
}

// --- Keyboard Shortcuts Settings ---

let recordingActionId = null;
let recordingKeydownHandler = null;

function renderShortcutsSettings() {
    const container = document.getElementById('shortcuts-settings-list');
    if (!container || typeof ShortcutManager === 'undefined') return;

    const groups = new Map();
    for (const [id, action] of ShortcutManager.actions) {
        const cat = action.category || 'other';
        if (!groups.has(cat)) groups.set(cat, []);
        groups.get(cat).push({ id, ...action });
    }

    const categoryOrder = ['navigation', 'gallery', 'clip', 'lightbox', 'bulk', 'system'];
    const categoryLabels = {
        navigation: 'Navigation',
        gallery: 'Gallery',
        clip: 'Clip Actions',
        lightbox: 'Lightbox',
        bulk: 'Bulk Actions',
        system: 'System',
    };

    let html = '';

    for (const cat of categoryOrder) {
        const items = groups.get(cat);
        if (!items || items.length === 0) continue;

        html += `<div class="mb-4">`;
        html += `<h4 class="text-[10px] font-semibold text-stone-400 uppercase tracking-wider mb-1.5">${escapeHtml(categoryLabels[cat] || cat)}</h4>`;

        for (const item of items) {
            const combo = ShortcutManager.getEffectiveCombo(item.id);
            const display = ShortcutManager.comboToDisplay(combo);
            html += `<div class="flex items-center justify-between py-1.5 px-1 hover:bg-stone-50 rounded transition-colors" data-testid="shortcut-row-${item.id}">`;
            html += `<span class="text-xs text-stone-700">${escapeHtml(item.label)}</span>`;
            html += `<button class="shortcut-key-badge bg-stone-100 border border-stone-200 rounded px-2 py-0.5 text-[11px] font-mono text-stone-600 min-w-[28px] text-center hover:border-stone-400 hover:bg-stone-50 transition-colors cursor-pointer" data-action-id="${item.id}" data-testid="shortcut-badge-${item.id}">${escapeHtml(display)}</button>`;
            html += `</div>`;
        }

        html += `</div>`;
    }

    container.innerHTML = html;

    // Add click handlers to badges
    container.querySelectorAll('.shortcut-key-badge').forEach(badge => {
        badge.addEventListener('click', () => startRecording(badge.dataset.actionId, badge));
    });
}

function startRecording(actionId, badgeEl) {
    // Cancel any existing recording
    stopRecording();

    recordingActionId = actionId;
    badgeEl.textContent = '...';
    badgeEl.classList.add('ring-2', 'ring-stone-400', 'animate-pulse');

    recordingKeydownHandler = (e) => {
        e.preventDefault();
        e.stopPropagation();

        // Escape cancels recording
        if (e.key === 'Escape') {
            stopRecording();
            renderShortcutsSettings();
            return;
        }

        // Ignore bare modifier presses
        if (['Control', 'Meta', 'Shift', 'Alt'].includes(e.key)) return;

        const combo = ShortcutManager.eventToCombo(e);
        if (!combo) return;

        // Check for conflict
        const conflict = ShortcutManager.findConflict(recordingActionId, combo);
        if (conflict) {
            const proceed = confirm(`"${ShortcutManager.comboToDisplay(combo)}" is already used by "${conflict.label}". Override?`);
            if (!proceed) {
                stopRecording();
                renderShortcutsSettings();
                return;
            }
            // Remove the conflicting binding
            ShortcutManager.setOverride(conflict.id, null);
        }

        ShortcutManager.setOverride(recordingActionId, combo);
        stopRecording();
        renderShortcutsSettings();
    };

    document.addEventListener('keydown', recordingKeydownHandler, true);
}

function stopRecording() {
    if (recordingKeydownHandler) {
        document.removeEventListener('keydown', recordingKeydownHandler, true);
        recordingKeydownHandler = null;
    }
    recordingActionId = null;
}

// Wire up reset button
document.getElementById('shortcuts-reset-btn')?.addEventListener('click', () => {
    if (typeof ShortcutManager !== 'undefined') {
        ShortcutManager.resetAllToDefaults();
        renderShortcutsSettings();
        if (typeof showToast === 'function') showToast('Shortcuts reset to defaults');
    }
});
