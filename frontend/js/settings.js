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

// Check on load
window.addEventListener('load', checkFalApiKeyStatus);
