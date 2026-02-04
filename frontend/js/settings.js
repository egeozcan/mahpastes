// --- Settings Module ---

let falApiKeyConfigured = false;

// Elements
const settingsModal = document.getElementById('settings-modal');
const openSettingsBtn = document.getElementById('open-settings-btn');
const settingsCloseBtn = document.getElementById('settings-close');
const settingsSaveBtn = document.getElementById('settings-save');
const falApiKeyInput = document.getElementById('fal-api-key');
const toggleApiKeyBtn = document.getElementById('toggle-api-key-visibility');
const falStatusIndicator = document.getElementById('fal-status-indicator');
const falStatusText = document.getElementById('fal-status-text');

function openSettings() {
    settingsModal.classList.remove('opacity-0', 'pointer-events-none');
    settingsModal.classList.add('opacity-100');
    settingsModal.querySelector(':scope > div').classList.remove('scale-95');
    settingsModal.querySelector(':scope > div').classList.add('scale-100');
    loadSettings();
}

function closeSettings() {
    settingsModal.classList.add('opacity-0', 'pointer-events-none');
    settingsModal.classList.remove('opacity-100');
    settingsModal.querySelector(':scope > div').classList.add('scale-95');
    settingsModal.querySelector(':scope > div').classList.remove('scale-100');
}

async function loadSettings() {
    try {
        const apiKey = await window.go.main.App.GetSetting('fal_api_key');
        if (apiKey) {
            falApiKeyInput.value = apiKey;
            updateFalStatus(true);
        } else {
            falApiKeyInput.value = '';
            updateFalStatus(false);
        }
    } catch (error) {
        console.error('Failed to load settings:', error);
    }
}

async function saveSettings() {
    try {
        const apiKey = falApiKeyInput.value.trim();
        await window.go.main.App.SetSetting('fal_api_key', apiKey);

        updateFalStatus(apiKey !== '');
        showToast('Settings saved');
        closeSettings();

        // Update UI based on API key status
        updateAIActionsVisibility();
    } catch (error) {
        console.error('Failed to save settings:', error);
        showToast('Failed to save settings');
    }
}

function updateFalStatus(configured) {
    falApiKeyConfigured = configured;
    if (configured) {
        falStatusIndicator.classList.remove('bg-stone-300');
        falStatusIndicator.classList.add('bg-green-500');
        falStatusText.textContent = 'Connected';
        falStatusText.classList.remove('text-stone-500');
        falStatusText.classList.add('text-green-600');
    } else {
        falStatusIndicator.classList.add('bg-stone-300');
        falStatusIndicator.classList.remove('bg-green-500');
        falStatusText.textContent = 'Not configured';
        falStatusText.classList.add('text-stone-500');
        falStatusText.classList.remove('text-green-600');
    }
}

function toggleApiKeyVisibility() {
    if (falApiKeyInput.type === 'password') {
        falApiKeyInput.type = 'text';
    } else {
        falApiKeyInput.type = 'password';
    }
}

async function checkFalApiKeyStatus() {
    try {
        const hasKey = await window.go.main.App.HasFalApiKey();
        falApiKeyConfigured = hasKey;
        updateAIActionsVisibility();
    } catch (error) {
        console.error('Failed to check API key status:', error);
    }
}

function updateAIActionsVisibility() {
    const bulkAIActions = document.getElementById('bulk-ai-actions');
    const lightboxAIActions = document.getElementById('lightbox-ai-actions');

    if (bulkAIActions) {
        // Show if API key is configured and images are selected
        const hasImages = selectedIds && Array.from(selectedIds).some(id => {
            const card = gallery.querySelector(`li[data-id="${id}"]`);
            return card && card.dataset.type && card.dataset.type.startsWith('image/');
        });

        if (falApiKeyConfigured && hasImages) {
            bulkAIActions.classList.remove('hidden');
        } else {
            bulkAIActions.classList.add('hidden');
        }
    }

    if (lightboxAIActions) {
        if (falApiKeyConfigured) {
            lightboxAIActions.classList.remove('hidden');
        } else {
            lightboxAIActions.classList.add('hidden');
        }
    }
}

// Event listeners
openSettingsBtn.addEventListener('click', openSettings);
settingsCloseBtn.addEventListener('click', closeSettings);
settingsSaveBtn.addEventListener('click', saveSettings);
toggleApiKeyBtn.addEventListener('click', toggleApiKeyVisibility);
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

// Check on load
window.addEventListener('load', checkFalApiKeyStatus);
