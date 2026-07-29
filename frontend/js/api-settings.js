// --- API Settings Module ---

const apiModal = document.getElementById('api-modal');
const openApiBtn = document.getElementById('open-api-btn');
const apiModalClose = document.getElementById('api-modal-close');

const apiPortInput = document.getElementById('api-port-input');
const apiBindToggle = document.getElementById('api-bind-toggle');
const apiToggleBtn = document.getElementById('api-toggle-btn');
const apiStatusText = document.getElementById('api-status-text');

const apiShowCreateBtn = document.getElementById('api-show-create-btn');
const apiCreateForm = document.getElementById('api-create-form');
const apiKeyNameInput = document.getElementById('api-key-name');
const apiKeyRoleSelect = document.getElementById('api-key-role');
const apiKeyScopeSelect = document.getElementById('api-key-scope');
const apiCreateKeyBtn = document.getElementById('api-create-key-btn');
const apiCancelCreateBtn = document.getElementById('api-cancel-create-btn');

const apiKeyReveal = document.getElementById('api-key-reveal');
const apiKeyRevealValue = document.getElementById('api-key-reveal-value');
const apiKeyCopyBtn = document.getElementById('api-key-copy-btn');
const apiKeyRevealClose = document.getElementById('api-key-reveal-close');

const apiKeysList = document.getElementById('api-keys-list');

let apiServerRunning = false;

function openApiModal() {
    loadAPIStatus();
    loadAPIKeys();
    populateTagScopeOptions();
    apiModal.removeAttribute('inert');
    apiModal.classList.remove('opacity-0', 'pointer-events-none');
    apiModal.classList.add('opacity-100');
    apiModal.querySelector(':scope > div').classList.remove('scale-95');
    apiModal.querySelector(':scope > div').classList.add('scale-100');
}

function closeApiModal() {
    apiModal.classList.add('opacity-0', 'pointer-events-none');
    apiModal.classList.remove('opacity-100');
    apiModal.querySelector(':scope > div').classList.add('scale-95');
    apiModal.querySelector(':scope > div').classList.remove('scale-100');
    apiModal.setAttribute('inert', '');
    hideCreateKeyForm();
    hideKeyReveal();
}

// Event listeners
openApiBtn.addEventListener('click', openApiModal);
apiModalClose.addEventListener('click', closeApiModal);
apiModal.addEventListener('click', (e) => {
    if (e.target === apiModal) closeApiModal();
});

apiToggleBtn.addEventListener('click', toggleAPIServer);
apiShowCreateBtn.addEventListener('click', showCreateKeyForm);
apiCancelCreateBtn.addEventListener('click', hideCreateKeyForm);
apiCreateKeyBtn.addEventListener('click', createAPIKey);
apiKeyCopyBtn.addEventListener('click', () => {
    navigator.clipboard.writeText(apiKeyRevealValue.textContent);
    showToast('Key copied to clipboard');
});
apiKeyRevealClose.addEventListener('click', hideKeyReveal);

async function loadAPIStatus() {
    try {
        const status = await window.go.main.APIService.GetAPIStatus();
        apiServerRunning = status.running;
        if (status.running) {
            apiToggleBtn.textContent = 'Stop Server';
            apiToggleBtn.classList.remove('bg-stone-800', 'hover:bg-stone-700');
            apiToggleBtn.classList.add('bg-red-600', 'hover:bg-red-700');
            apiStatusText.innerHTML = `<span class="inline-block w-2 h-2 bg-emerald-500 rounded-full mr-1"></span>${status.url} &middot; ${status.request_count} reqs`;
            apiPortInput.disabled = true;
            apiBindToggle.disabled = true;
        } else {
            apiToggleBtn.textContent = 'Start Server';
            apiToggleBtn.classList.remove('bg-red-600', 'hover:bg-red-700');
            apiToggleBtn.classList.add('bg-stone-800', 'hover:bg-stone-700');
            apiStatusText.textContent = 'Stopped';
            apiPortInput.disabled = false;
            apiBindToggle.disabled = false;
        }
    } catch (err) {
        console.error('Failed to load API status:', err);
    }
}

async function toggleAPIServer() {
    try {
        if (apiServerRunning) {
            await window.go.main.APIService.StopAPI();
            showToast('API server stopped');
        } else {
            const port = parseInt(apiPortInput.value, 10) || 8484;
            const bindAll = apiBindToggle.checked;
            await window.go.main.APIService.StartAPI(port, bindAll);
            showToast('API server started');
        }
        loadAPIStatus();
    } catch (err) {
        showToast('Error: ' + err);
    }
}

async function loadAPIKeys() {
    try {
        const keys = await window.go.main.APIService.ListAPIKeys();
        if (!keys || keys.length === 0) {
            apiKeysList.innerHTML = '<p class="text-xs text-stone-400">No API keys yet.</p>';
            return;
        }
        apiKeysList.innerHTML = keys.map(renderAPIKeyCard).join('');
    } catch (err) {
        console.error('Failed to load API keys:', err);
    }
}

function renderAPIKeyCard(key) {
    const roleBadgeColors = {
        admin: 'bg-stone-800 text-white',
        editor: 'bg-stone-200 text-stone-700',
        viewer: 'bg-stone-100 text-stone-500'
    };
    const badgeClass = roleBadgeColors[key.role] || roleBadgeColors.viewer;
    const scopeLabel = key.scoped_tag_name ? `<span class="text-[10px] text-stone-400">scope: ${escapeHtml(key.scoped_tag_name)}</span>` : '';
    const lastUsed = key.last_used_at ? `Last used: ${key.last_used_at}` : 'Never used';
    const revokedClass = key.is_revoked ? 'opacity-50' : '';
    const revokeBtn = key.is_revoked
        ? '<span class="text-[10px] text-red-400 font-medium">Revoked</span>'
        : `<button data-revoke-key="${key.id}" data-revoke-name="${escapeHtml(key.name)}" class="text-[10px] text-stone-400 hover:text-red-500 transition-colors" data-testid="revoke-key-${key.id}">Revoke</button>`;

    return `
        <div class="p-2.5 border border-stone-200 rounded-md ${revokedClass}" data-testid="api-key-card-${key.id}">
            <div class="flex items-center justify-between mb-1">
                <div class="flex items-center gap-2">
                    <span class="text-xs font-medium text-stone-700">${escapeHtml(key.name)}</span>
                    <span class="text-[9px] font-medium px-1.5 py-0.5 rounded ${badgeClass}">${key.role}</span>
                    ${scopeLabel}
                </div>
                ${revokeBtn}
            </div>
            <div class="flex items-center justify-between text-[10px] text-stone-400">
                <code class="font-mono">${escapeHtml(key.key_prefix)}</code>
                <span>${lastUsed}</span>
            </div>
        </div>
    `;
}

// Escapes quotes too, so the result is safe inside a double-quoted attribute
// value — innerHTML serialisation alone leaves `"` intact.
function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML.replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function showCreateKeyForm() {
    apiCreateForm.classList.remove('hidden');
    apiKeyNameInput.value = '';
    apiKeyRoleSelect.value = 'editor';
    apiKeyScopeSelect.value = '0';
    apiKeyNameInput.focus();
}

function hideCreateKeyForm() {
    apiCreateForm.classList.add('hidden');
}

function hideKeyReveal() {
    apiKeyReveal.classList.add('hidden');
}

async function createAPIKey() {
    const name = apiKeyNameInput.value.trim();
    if (!name) {
        showToast('Key name is required');
        return;
    }
    const role = apiKeyRoleSelect.value;
    const scopedTagID = parseInt(apiKeyScopeSelect.value, 10) || 0;

    try {
        const result = await window.go.main.APIService.CreateAPIKey(name, role, scopedTagID);
        hideCreateKeyForm();

        // Show key reveal
        apiKeyRevealValue.textContent = result.key;
        apiKeyReveal.classList.remove('hidden');

        loadAPIKeys();
    } catch (err) {
        showToast('Failed to create key: ' + err);
    }
}

// Native confirm() is a silent no-op inside the Wails webview on macOS, so this
// goes through the in-app confirm dialog like every other destructive action.
apiKeysList.addEventListener('click', (e) => {
    const btn = e.target.closest('[data-revoke-key]');
    if (!btn) return;
    const id = parseInt(btn.dataset.revokeKey, 10);
    const name = btn.dataset.revokeName || 'this key';
    showConfirmDialog(
        'Revoke API Key',
        `Revoke <strong>${escapeHtml(name)}</strong>? It will immediately stop working, and this cannot be undone.`,
        () => revokeAPIKey(id),
        null,
        { confirmLabel: 'Revoke' }
    );
});

async function revokeAPIKey(id) {
    try {
        await window.go.main.APIService.RevokeAPIKey(id);
        showToast('Key revoked');
        loadAPIKeys();
    } catch (err) {
        showToast('Failed to revoke key: ' + err);
    }
}

async function populateTagScopeOptions() {
    try {
        const tags = await window.go.main.App.GetTags();
        apiKeyScopeSelect.innerHTML = '<option value="0">All tags</option>';
        if (tags) {
            tags.forEach(tag => {
                const opt = document.createElement('option');
                opt.value = tag.id;
                opt.textContent = tag.name;
                apiKeyScopeSelect.appendChild(opt);
            });
        }
    } catch (err) {
        console.error('Failed to load tags for scope:', err);
    }
}
