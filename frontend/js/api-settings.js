// --- API Settings Module ---

const apiModal = document.getElementById('api-modal');
const openApiBtn = document.getElementById('open-api-btn');
const apiModalClose = document.getElementById('api-modal-close');

const apiPortInput = document.getElementById('api-port-input');
const apiBindToggle = document.getElementById('api-bind-toggle');
const apiToggleBtn = document.getElementById('api-toggle-btn');
const apiAutostartToggle = document.getElementById('api-autostart-toggle');
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
const apiModalFocus = createModalFocus(apiModal);

function openApiModal() {
    loadAPIStatus();
    loadAPIKeys();
    populateTagScopeOptions();
    apiModal.removeAttribute('inert');
    apiModal.classList.remove('opacity-0', 'pointer-events-none');
    apiModal.classList.add('opacity-100');
    apiModal.querySelector(':scope > div').classList.remove('scale-95');
    apiModal.querySelector(':scope > div').classList.add('scale-100');
    apiModalFocus.open(apiModalClose);
}

function closeApiModal() {
    apiModal.classList.add('opacity-0', 'pointer-events-none');
    apiModal.classList.remove('opacity-100');
    apiModal.querySelector(':scope > div').classList.add('scale-95');
    apiModal.querySelector(':scope > div').classList.remove('scale-100');
    apiModal.setAttribute('inert', '');
    hideCreateKeyForm();
    hideKeyReveal();
    apiModalFocus.close();
}

// Event listeners
openApiBtn.addEventListener('click', openApiModal);
apiModalClose.addEventListener('click', closeApiModal);
apiModal.addEventListener('click', (e) => {
    if (e.target === apiModal) closeApiModal();
});

apiToggleBtn.addEventListener('click', toggleAPIServer);
apiAutostartToggle.addEventListener('change', () => saveAPILaunchSettings(true));
// "Start on launch" uses the port shown above, so a port edited while it is on
// is saved too (the server itself only picks it up on the next Start).
apiPortInput.addEventListener('change', () => {
    if (apiAutostartToggle.checked) saveAPILaunchSettings(false);
});
apiShowCreateBtn.addEventListener('click', showCreateKeyForm);
apiCancelCreateBtn.addEventListener('click', hideCreateKeyForm);
apiCreateKeyBtn.addEventListener('click', createAPIKey);
// Enter in the name field creates the key (createAPIKey guards doubles).
apiKeyNameInput?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
        e.preventDefault();
        createAPIKey();
    }
});

// The commands that use the key just created: the mp CLI reads both from the
// environment. The address is this page's own in the web UI; on the desktop
// it is the API server's (running, or the port it is set to start on).
let apiServerURL = '';
function renderAPIKeyUsage() {
    const snippet = document.getElementById('api-key-usage-snippet');
    const stopped = document.getElementById('api-key-usage-stopped');
    if (!snippet || apiKeyReveal.classList.contains('hidden')) return;
    const server = window.mahpastesMode === 'server';
    const port = parseInt(apiPortInput.value, 10) || 44557;
    const url = server ? window.location.origin : (apiServerURL || `http://localhost:${port}`);
    snippet.textContent = `export MP_API_KEY=${apiKeyRevealValue.textContent}\nexport MP_API_URL=${url}\nmp clip list`;
    if (stopped) stopped.hidden = server || apiServerRunning;
}
document.getElementById('api-key-usage-copy')?.addEventListener('click', async () => {
    const text = document.getElementById('api-key-usage-snippet')?.textContent || '';
    if (text && await copyTextQuietly(text)) showToast('Commands copied');
    else showToast('Could not copy the commands.', 'error');
});

// A backend timestamp as a Date: "YYYY-MM-DD HH:MM:SS" (UTC, no zone) or
// RFC 3339. Null when it is neither.
function parseAPITimestamp(value) {
    if (!value) return null;
    let text = String(value).trim();
    if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}/.test(text)) {
        text = text.replace(' ', 'T');
        if (!/(Z|[+-]\d{2}:?\d{2})$/.test(text)) text += 'Z';
    }
    const date = new Date(text);
    return Number.isFinite(date.getTime()) ? date : null;
}

// "just now", "5 min ago", "3 h ago", "4 days ago", then the local date.
function apiTimeAgo(date) {
    const seconds = Math.max(0, Math.floor((Date.now() - date.getTime()) / 1000));
    if (seconds < 60) return 'just now';
    if (seconds < 3600) return `${Math.floor(seconds / 60)} min ago`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)} h ago`;
    if (seconds < 86400 * 14) return `${Math.floor(seconds / 86400)} days ago`;
    return date.toLocaleDateString();
}
apiKeyCopyBtn.addEventListener('click', async () => {
    try {
        if (!navigator.clipboard) throw new Error('clipboard API unavailable');
        await navigator.clipboard.writeText(apiKeyRevealValue.textContent);
        showToast('Key copied to clipboard');
    } catch (err) {
        console.error('Failed to copy key:', err);
        toastError('Failed to copy key', err);
    }
});
apiKeyRevealClose.addEventListener('click', hideKeyReveal);

async function loadAPIStatus() {
    // The modal is usable while this loads, so note what the launch fields
    // held when it started: an edit made in the meantime is kept.
    const before = { port: apiPortInput.value, autostart: apiAutostartToggle.checked };
    try {
        const status = await window.go.main.APIService.GetAPIStatus();
        apiServerRunning = status.running;
        apiServerURL = status.running && status.url ? status.url : '';
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
        apiStatusText.classList.remove('text-amber-700');
        if (!status.running) await showAPILaunchError();
        await loadAPILaunchSettings(status, before);
    } catch (err) {
        console.error('Failed to load API status:', err);
        apiStatusText.textContent = 'Could not read the server status: ' + errText(err);
    }
    renderAPIKeyUsage();
}

// A failed "Start on launch" (port taken, no permission) leaves the server
// stopped; say why, or it looks the same as a server the user stopped. The
// backend forgets the reason at the next manual start or stop. Server mode's
// APIService has no such method.
async function showAPILaunchError() {
    try {
        const svc = window.go.main.APIService;
        const reason = svc.GetAPILaunchError ? await svc.GetAPILaunchError() : '';
        if (!reason || apiServerRunning) return;
        apiStatusText.textContent = `Stopped \u00b7 start on launch failed: ${reason}`;
        apiStatusText.classList.add('text-amber-700');
    } catch (err) {
        console.error('Failed to load API launch error:', err);
    }
}

// Shows the "Start on launch" choice, and in the port field the port the
// server runs on (or, while stopped, the one it starts on next). A field the
// user changed since `before` was taken keeps their value: overwriting it
// would have the switch save the stored port instead of the one just typed.
async function loadAPILaunchSettings(status, before) {
    try {
        const svc = window.go.main.APIService;
        const autostart = await svc.GetAPIAutostart();
        const port = status.running ? status.port : await svc.GetAPIPort();
        if (apiAutostartToggle.checked === before.autostart) apiAutostartToggle.checked = autostart;
        if (status.running || apiPortInput.value === before.port) apiPortInput.value = port;
    } catch (err) {
        console.error('Failed to load API launch settings:', err);
    }
}

// Saves run one after another, each with the values from when it was made, so
// a port edit followed at once by a click on the switch (the blur fires the
// port save first) cannot land out of order.
let apiLaunchSaveChain = Promise.resolve();

const API_PORT_INVALID = 'invalid port: must be a whole number between 1 and 65535';

// The port in the field, or null when it is not a whole number in 1..65535.
// Never a fallback: `parseInt(v) || 44557` saved 44557 for an empty or 0 field
// and port 1 for "1e3", while the field went on showing something else.
function readAPIPort() {
    const text = apiPortInput.value.trim();
    if (!/^\d{1,5}$/.test(text)) return null;
    const port = Number(text);
    return port >= 1 && port <= 65535 ? port : null;
}

// revertToggle: a failed save from the switch itself flips it back.
function saveAPILaunchSettings(revertToggle) {
    const enabled = apiAutostartToggle.checked;
    const port = readAPIPort();
    apiLaunchSaveChain = apiLaunchSaveChain.then(async () => {
        try {
            // Switching off stores no port, so a bad field does not block it.
            if (enabled && port === null) throw new Error(API_PORT_INVALID);
            await window.go.main.APIService.SetAPIAutostart(enabled, port ?? 0);
        } catch (err) {
            if (revertToggle && apiAutostartToggle.checked === enabled) {
                apiAutostartToggle.checked = !enabled;
            }
            showToast('Error: ' + errText(err), 'error');
        }
    });
    return apiLaunchSaveChain;
}

// Start/Stop and Create Key each ignore clicks while their call is in flight:
// a double click on Create made two keys.
let apiToggleBusy = false;
let apiCreateKeyBusy = false;

async function toggleAPIServer() {
    if (apiToggleBusy) return;
    apiToggleBusy = true;
    apiToggleBtn.disabled = true;
    try {
        if (apiServerRunning) {
            await window.go.main.APIService.StopAPI();
            showToast('API server stopped');
        } else {
            const port = readAPIPort();
            if (port === null) throw new Error(API_PORT_INVALID);
            const bindAll = apiBindToggle.checked;
            await window.go.main.APIService.StartAPI(port, bindAll);
            showToast('API server started');
        }
        await loadAPIStatus();
    } catch (err) {
        showToast('Error: ' + errText(err), 'error');
    } finally {
        apiToggleBusy = false;
        apiToggleBtn.disabled = false;
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
        const note = document.createElement('p');
        note.className = 'text-xs text-red-500';
        note.textContent = 'Failed to load API keys: ' + errText(err);
        apiKeysList.replaceChildren(note);
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
    // The backend's timestamp is UTC and raw ("2026-11-13 09:14:02" or
    // RFC 3339): shown as how long ago, with the local time in the tooltip.
    const lastUsedDate = parseAPITimestamp(key.last_used_at);
    const lastUsed = !key.last_used_at
        ? 'Never used'
        : (lastUsedDate ? `Last used: ${apiTimeAgo(lastUsedDate)}` : `Last used: ${key.last_used_at}`);
    const lastUsedTitle = lastUsedDate ? lastUsedDate.toLocaleString() : '';
    const revokedClass = key.is_revoked ? 'opacity-50' : '';
    // Revoked keys are removed from the DB a week later, so say so rather than
    // letting the card vanish from this list unexplained.
    const revokedTitle = key.revoked_at
        ? ` title="Revoked ${escapeHtml((parseAPITimestamp(key.revoked_at) || { toLocaleString: () => key.revoked_at }).toLocaleString())} — removed from this list 7 days after revoking"`
        : '';
    const revokeBtn = key.is_revoked
        ? `<span class="text-[10px] text-red-400 font-medium"${revokedTitle}>Revoked</span>`
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
                <span data-testid="api-key-last-used-${key.id}"${lastUsedTitle ? ` title="${escapeHtml(lastUsedTitle)}"` : ''}>${escapeHtml(lastUsed)}</span>
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
    if (apiCreateKeyBusy) return;
    apiCreateKeyBusy = true;
    apiCreateKeyBtn.disabled = true;

    try {
        const result = await window.go.main.APIService.CreateAPIKey(name, role, scopedTagID);
        hideCreateKeyForm();

        // Show key reveal
        apiKeyRevealValue.textContent = result.key;
        apiKeyReveal.classList.remove('hidden');
        renderAPIKeyUsage();

        loadAPIKeys();
    } catch (err) {
        showToast('Failed to create key: ' + errText(err), 'error');
    } finally {
        apiCreateKeyBusy = false;
        apiCreateKeyBtn.disabled = false;
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
        showToast('Failed to revoke key: ' + errText(err), 'error');
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
