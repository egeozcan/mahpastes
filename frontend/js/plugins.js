// --- Plugins Module ---

// Elements
const pluginsModal = document.getElementById('plugins-modal');
const openPluginsBtn = document.getElementById('open-plugins-btn');
const pluginsCloseBtn = document.getElementById('plugins-close');
const importPluginBtn = document.getElementById('import-plugin-btn');
const pluginsList = document.getElementById('plugins-list');
const pluginsEmptyState = document.getElementById('plugins-empty-state');

// State
let pluginsCache = [];
let expandedPluginId = null;

// --- Modal Open/Close ---
function openPlugins() {
    pluginsModal.classList.remove('opacity-0', 'pointer-events-none');
    pluginsModal.classList.add('opacity-100');
    pluginsModal.querySelector(':scope > div').classList.remove('scale-95');
    pluginsModal.querySelector(':scope > div').classList.add('scale-100');
    loadPlugins();
}

function closePlugins() {
    pluginsModal.classList.add('opacity-0', 'pointer-events-none');
    pluginsModal.classList.remove('opacity-100');
    pluginsModal.querySelector(':scope > div').classList.add('scale-95');
    pluginsModal.querySelector(':scope > div').classList.remove('scale-100');
    expandedPluginId = null;
}

// --- Load Plugins ---
async function loadPlugins() {
    try {
        pluginsCache = await window.go.main.PluginService.GetPlugins();
        renderPluginsList();
    } catch (error) {
        console.error('Failed to load plugins:', error);
        showToast('Failed to load plugins');
    }
}

// --- Render Plugins List ---
function renderPluginsList() {
    if (pluginsCache.length === 0) {
        pluginsList.innerHTML = '';
        pluginsEmptyState.classList.remove('hidden');
        return;
    }

    pluginsEmptyState.classList.add('hidden');
    pluginsList.innerHTML = '';

    for (const plugin of pluginsCache) {
        const card = createPluginCard(plugin);
        pluginsList.appendChild(card);
    }
}

// --- Create Plugin Card ---
function createPluginCard(plugin) {
    const li = document.createElement('li');
    li.className = 'bg-white border border-stone-200 rounded-lg overflow-hidden transition-all hover:border-stone-300';
    li.dataset.id = plugin.id;
    li.dataset.testid = `plugin-card-${plugin.id}`;

    const isExpanded = expandedPluginId === plugin.id;
    const statusDot = plugin.status === 'error'
        ? 'bg-red-500'
        : (plugin.enabled ? 'bg-emerald-500' : 'bg-stone-300');
    const statusTitle = plugin.enabled ? (plugin.status === 'error' ? 'Error' : 'Enabled') : 'Disabled';

    li.innerHTML = `
        <div class="p-4 cursor-pointer" data-action="toggle-expand">
            <div class="flex items-center justify-between">
                <div class="flex items-center gap-3 min-w-0 flex-1">
                    <span class="w-2 h-2 rounded-full ${statusDot} flex-shrink-0"
                          title="${statusTitle}"></span>
                    <div class="min-w-0 flex-1">
                        <div class="flex items-center gap-2">
                            <h3 class="text-sm font-medium text-stone-700 truncate">${escapeHTML(plugin.name)}</h3>
                            <span class="text-[10px] text-stone-400 font-mono">v${escapeHTML(plugin.version || '0.0.0')}</span>
                        </div>
                        ${plugin.author ? `<p class="text-[11px] text-stone-400 truncate">by ${escapeHTML(plugin.author)}</p>` : ''}
                    </div>
                </div>
                <div class="flex items-center gap-2">
                    <label class="relative inline-flex items-center cursor-pointer" data-action="toggle-enable">
                        <input type="checkbox" data-testid="plugin-toggle-${plugin.id}"
                               class="sr-only peer" ${plugin.enabled ? 'checked' : ''}>
                        <div class="w-9 h-5 bg-stone-300 peer-focus:ring-2 peer-focus:ring-stone-400/30 rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:bg-emerald-500"></div>
                    </label>
                    <svg class="w-4 h-4 text-stone-400 transition-transform ${isExpanded ? 'rotate-180' : ''}"
                         fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M19 9l-7 7-7-7" />
                    </svg>
                </div>
            </div>
        </div>

        <!-- Expanded Details -->
        <div class="border-t border-stone-100 ${isExpanded ? '' : 'hidden'}" data-section="details">
            <div class="p-4 space-y-4 bg-stone-50/50">
                ${plugin.description ? `
                <div>
                    <p class="text-[11px] text-stone-500">${escapeHTML(plugin.description)}</p>
                </div>
                ` : ''}

                ${plugin.status === 'error' ? `
                <div class="flex items-center gap-2 p-2 bg-red-50 rounded text-red-600">
                    <svg class="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                              d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                    </svg>
                    <span class="text-[11px] font-medium">Plugin has errors and was disabled</span>
                </div>
                ` : ''}

                <!-- Events Section -->
                ${plugin.events && plugin.events.length > 0 ? `
                <div>
                    <h4 class="text-[10px] font-semibold text-stone-500 uppercase tracking-wider mb-2">Events</h4>
                    <div class="flex flex-wrap gap-1">
                        ${plugin.events.map(event => `
                            <span class="inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-medium bg-stone-200 text-stone-600">
                                ${escapeHTML(event)}
                            </span>
                        `).join('')}
                    </div>
                </div>
                ` : ''}

                <!-- Permissions Section -->
                <div data-permissions-container data-plugin-id="${plugin.id}">
                    <h4 class="text-[10px] font-semibold text-stone-500 uppercase tracking-wider mb-2">Permissions</h4>
                    <div data-permissions-list class="text-[11px] text-stone-400">Loading...</div>
                </div>

                <!-- Actions -->
                <div class="pt-2 border-t border-stone-200 flex justify-end">
                    <button data-action="remove" data-testid="remove-plugin-${plugin.id}"
                            class="text-xs text-red-500 hover:text-red-600 hover:bg-red-50 px-3 py-1.5 rounded-md transition-colors">
                        Remove Plugin
                    </button>
                </div>
            </div>
        </div>
    `;

    // Event listeners
    const header = li.querySelector('[data-action="toggle-expand"]');
    header.addEventListener('click', (e) => {
        // Don't toggle if clicking the enable toggle
        if (e.target.closest('[data-action="toggle-enable"]')) return;
        togglePluginExpand(plugin.id);
    });

    const enableToggle = li.querySelector('[data-action="toggle-enable"] input');
    enableToggle.addEventListener('change', (e) => {
        e.stopPropagation();
        togglePluginEnabled(plugin.id, e.target.checked);
    });

    const removeBtn = li.querySelector('[data-action="remove"]');
    removeBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        removePlugin(plugin.id, plugin.name);
    });

    // Load permissions if expanded
    if (isExpanded) {
        loadPluginPermissions(plugin.id, li);
    }

    return li;
}

// --- Toggle Plugin Expand ---
async function togglePluginExpand(pluginId) {
    if (expandedPluginId === pluginId) {
        expandedPluginId = null;
    } else {
        expandedPluginId = pluginId;
    }

    // Re-render to update expanded state
    renderPluginsList();

    // Load permissions for newly expanded plugin
    if (expandedPluginId) {
        const card = pluginsList.querySelector(`li[data-id="${expandedPluginId}"]`);
        if (card) {
            await loadPluginPermissions(expandedPluginId, card);
        }
    }
}

// --- Load Plugin Permissions ---
async function loadPluginPermissions(pluginId, cardElement) {
    const container = cardElement.querySelector('[data-permissions-list]');
    if (!container) return;

    try {
        const permissions = await window.go.main.PluginService.GetPluginPermissions(pluginId);

        if (!permissions || permissions.length === 0) {
            container.innerHTML = '<span class="text-stone-400">No filesystem permissions granted</span>';
            return;
        }

        container.innerHTML = `
            <div class="space-y-1.5">
                ${permissions.map(perm => `
                    <div class="flex items-center justify-between gap-2 p-2 bg-white rounded border border-stone-200">
                        <div class="flex items-center gap-2 min-w-0 flex-1">
                            <span class="inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-medium uppercase ${
                                perm.type === 'write' ? 'bg-amber-100 text-amber-700' : 'bg-blue-100 text-blue-700'
                            }">
                                ${perm.type}
                            </span>
                            <span class="truncate text-stone-600 font-mono text-[10px]" title="${escapeHTML(perm.path)}">${escapeHTML(perm.path)}</span>
                        </div>
                        <button class="text-[10px] text-red-500 hover:text-red-600 hover:bg-red-50 px-2 py-1 rounded transition-colors flex-shrink-0"
                                data-action="revoke-permission"
                                data-type="${perm.type}"
                                data-path="${escapeHTML(perm.path)}">
                            Revoke
                        </button>
                    </div>
                `).join('')}
            </div>
        `;

        // Add revoke listeners
        container.querySelectorAll('[data-action="revoke-permission"]').forEach(btn => {
            btn.addEventListener('click', async () => {
                const type = btn.dataset.type;
                const path = btn.dataset.path;
                await revokePermission(pluginId, type, path);
            });
        });
    } catch (error) {
        console.error('Failed to load permissions:', error);
        container.innerHTML = '<span class="text-red-500">Failed to load permissions</span>';
    }
}

// --- Toggle Plugin Enabled ---
async function togglePluginEnabled(pluginId, enabled) {
    try {
        if (enabled) {
            await window.go.main.PluginService.EnablePlugin(pluginId);
            showToast('Plugin enabled');
        } else {
            await window.go.main.PluginService.DisablePlugin(pluginId);
            showToast('Plugin disabled');
        }
        await loadPlugins();
    } catch (error) {
        console.error('Failed to toggle plugin:', error);
        showToast('Failed to update plugin');
        await loadPlugins(); // Refresh to correct UI state
    }
}

// --- Import Plugin ---
async function importPlugin() {
    try {
        const result = await window.go.main.PluginService.ImportPlugin();
        if (result) {
            showToast(`Imported: ${result.name}`);
            await loadPlugins();
        }
        // null means user cancelled, no error
    } catch (error) {
        console.error('Failed to import plugin:', error);
        showToast('Failed to import plugin: ' + (error.message || 'Unknown error'));
    }
}

// --- Remove Plugin ---
function removePlugin(pluginId, pluginName) {
    showConfirmDialog('Remove Plugin', `Remove "${pluginName}"? This cannot be undone.`, async () => {
        try {
            await window.go.main.PluginService.RemovePlugin(pluginId);
            showToast('Plugin removed');
            if (expandedPluginId === pluginId) {
                expandedPluginId = null;
            }
            await loadPlugins();
        } catch (error) {
            console.error('Failed to remove plugin:', error);
            showToast('Failed to remove plugin');
        }
    });
}

// --- Revoke Permission ---
async function revokePermission(pluginId, permType, path) {
    try {
        await window.go.main.PluginService.RevokePluginPermission(pluginId, permType, path);
        showToast('Permission revoked');

        // Reload permissions for this plugin
        const card = pluginsList.querySelector(`li[data-id="${pluginId}"]`);
        if (card) {
            await loadPluginPermissions(pluginId, card);
        }
    } catch (error) {
        console.error('Failed to revoke permission:', error);
        showToast('Failed to revoke permission');
    }
}

// --- Event Listeners ---
openPluginsBtn.addEventListener('click', openPlugins);
pluginsCloseBtn.addEventListener('click', closePlugins);
importPluginBtn.addEventListener('click', importPlugin);
pluginsModal.addEventListener('click', (e) => {
    if (e.target === pluginsModal) closePlugins();
});

// Close on escape
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !pluginsModal.classList.contains('opacity-0')) {
        closePlugins();
    }
});
