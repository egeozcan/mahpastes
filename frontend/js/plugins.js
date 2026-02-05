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

                <!-- Settings Section (loaded dynamically) -->
                <div data-settings-placeholder data-plugin-id="${plugin.id}"></div>

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

    // Load permissions and settings if expanded
    if (isExpanded) {
        loadPluginPermissions(plugin.id, li);
        loadPluginSettings(plugin.id, li);
    }

    return li;
}

// --- Render Settings Section ---
function renderSettingsSection(settings, pluginId, storageValues) {
    if (!settings || settings.length === 0) {
        return '';
    }

    const fields = settings.map(field => {
        const currentValue = storageValues[field.key];
        const displayValue = currentValue !== undefined ? currentValue : (field.default || '');

        return renderSettingField(field, displayValue, pluginId);
    }).join('');

    return `
        <div data-settings-section data-plugin-id="${pluginId}">
            <h4 class="text-[10px] font-semibold text-stone-500 uppercase tracking-wider mb-2">Settings</h4>
            <div class="space-y-3">
                ${fields}
            </div>
        </div>
    `;
}

function renderSettingField(field, currentValue, pluginId) {
    const description = field.description
        ? `<p class="text-[10px] text-stone-400 mt-1">${escapeHTML(field.description)}</p>`
        : '';

    switch (field.type) {
        case 'text':
            return `
                <div class="setting-field" data-key="${escapeHTML(field.key)}">
                    <label class="block text-[11px] font-medium text-stone-600 mb-1">${escapeHTML(field.label)}</label>
                    <input type="text"
                           class="block w-full border border-stone-200 rounded-md text-xs bg-white px-2 py-1.5 placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors"
                           value="${escapeHTML(currentValue || '')}"
                           placeholder="${escapeHTML(field.default || '')}"
                           data-plugin-id="${pluginId}"
                           data-setting-key="${escapeHTML(field.key)}"
                           data-setting-type="text">
                    ${description}
                </div>
            `;

        case 'password':
            return `
                <div class="setting-field" data-key="${escapeHTML(field.key)}">
                    <label class="block text-[11px] font-medium text-stone-600 mb-1">${escapeHTML(field.label)}</label>
                    <div class="relative">
                        <input type="password"
                               class="block w-full border border-stone-200 rounded-md text-xs bg-white px-2 py-1.5 pr-8 placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors"
                               value="${escapeHTML(currentValue || '')}"
                               data-plugin-id="${pluginId}"
                               data-setting-key="${escapeHTML(field.key)}"
                               data-setting-type="password">
                        <button type="button"
                                class="absolute right-2 top-1/2 -translate-y-1/2 text-stone-400 hover:text-stone-600"
                                data-action="toggle-password"
                            aria-label="Toggle password visibility">
                            <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
                            </svg>
                        </button>
                    </div>
                    ${description}
                </div>
            `;

        case 'checkbox':
            const isChecked = currentValue === 'true' || currentValue === true || (currentValue === '' && field.default === true);
            return `
                <div class="setting-field" data-key="${escapeHTML(field.key)}">
                    <label class="flex items-center gap-2 cursor-pointer">
                        <input type="checkbox"
                               class="w-4 h-4 rounded border-stone-300 text-stone-800 focus:ring-stone-400/20"
                               ${isChecked ? 'checked' : ''}
                               data-plugin-id="${pluginId}"
                               data-setting-key="${escapeHTML(field.key)}"
                               data-setting-type="checkbox">
                        <span class="text-[11px] font-medium text-stone-600">${escapeHTML(field.label)}</span>
                    </label>
                    ${description}
                </div>
            `;

        case 'select':
            const options = (field.options || []).map(opt => {
                const selected = currentValue === opt || (currentValue === '' && field.default === opt);
                return `<option value="${escapeHTML(opt)}" ${selected ? 'selected' : ''}>${escapeHTML(opt)}</option>`;
            }).join('');
            return `
                <div class="setting-field" data-key="${escapeHTML(field.key)}">
                    <label class="block text-[11px] font-medium text-stone-600 mb-1">${escapeHTML(field.label)}</label>
                    <select class="block w-full border border-stone-200 rounded-md text-xs bg-white px-2 py-1.5 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors"
                            data-plugin-id="${pluginId}"
                            data-setting-key="${escapeHTML(field.key)}"
                            data-setting-type="select">
                        ${options}
                    </select>
                    ${description}
                </div>
            `;

        default:
            return '';
    }
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

    // Load permissions and settings for newly expanded plugin
    if (expandedPluginId) {
        const card = pluginsList.querySelector(`li[data-id="${expandedPluginId}"]`);
        if (card) {
            await loadPluginPermissions(expandedPluginId, card);
            await loadPluginSettings(expandedPluginId, card);
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

// --- Load Plugin Settings ---
async function loadPluginSettings(pluginId, cardElement) {
    const placeholder = cardElement.querySelector('[data-settings-placeholder]');
    if (!placeholder) return;

    // Find the plugin in cache
    const plugin = pluginsCache.find(p => p.id === pluginId);
    if (!plugin || !plugin.settings || plugin.settings.length === 0) {
        placeholder.innerHTML = '';
        return;
    }

    try {
        // Load current storage values
        const storageValues = await window.go.main.PluginService.GetAllPluginStorage(pluginId);

        // Render settings section
        placeholder.innerHTML = renderSettingsSection(plugin.settings, pluginId, storageValues || {});

        // Add event listeners for setting changes
        setupSettingListeners(cardElement, pluginId);
    } catch (error) {
        console.error('Failed to load plugin settings:', error);
        placeholder.innerHTML = '<span class="text-red-500 text-[11px]">Failed to load settings</span>';
    }
}

// --- Setup Setting Event Listeners ---
let settingDebounceTimers = {};

function setupSettingListeners(cardElement, pluginId) {
    // Text and password inputs
    cardElement.querySelectorAll('input[data-setting-type="text"], input[data-setting-type="password"]').forEach(input => {
        input.addEventListener('input', (e) => {
            const key = e.target.dataset.settingKey;
            const value = e.target.value;
            debounceSaveSetting(pluginId, key, value);
        });
    });

    // Checkboxes
    cardElement.querySelectorAll('input[data-setting-type="checkbox"]').forEach(input => {
        input.addEventListener('change', (e) => {
            const key = e.target.dataset.settingKey;
            const value = e.target.checked ? 'true' : 'false';
            saveSetting(pluginId, key, value);
        });
    });

    // Selects
    cardElement.querySelectorAll('select[data-setting-type="select"]').forEach(select => {
        select.addEventListener('change', (e) => {
            const key = e.target.dataset.settingKey;
            const value = e.target.value;
            saveSetting(pluginId, key, value);
        });
    });

    // Password toggle buttons
    cardElement.querySelectorAll('[data-action="toggle-password"]').forEach(btn => {
        btn.addEventListener('click', (e) => {
            const button = e.target.closest('[data-action="toggle-password"]');
            const input = button.closest('.relative').querySelector('input');
            if (input) {
                input.type = input.type === 'password' ? 'text' : 'password';
            }
        });
    });
}

function debounceSaveSetting(pluginId, key, value) {
    const timerId = `${pluginId}-${key}`;
    if (settingDebounceTimers[timerId]) {
        clearTimeout(settingDebounceTimers[timerId]);
    }
    settingDebounceTimers[timerId] = setTimeout(() => {
        saveSetting(pluginId, key, value);
        delete settingDebounceTimers[timerId];
    }, 300);
}

async function saveSetting(pluginId, key, value) {
    try {
        await window.go.main.PluginService.SetPluginStorage(pluginId, key, value);
    } catch (error) {
        console.error('Failed to save setting:', error);
        showToast('Failed to save setting', 'error');
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

// --- Execute Plugin Action ---
// This function is called when a plugin action is triggered from card menu or lightbox
async function executePluginAction(pluginId, actionId, clipIds, options) {
    try {
        const result = await window.go.main.PluginService.ExecutePluginAction(pluginId, actionId, clipIds, options || {});
        if (result && result.success) {
            showToast('Action completed');
            // Reload clips to show any new clips created
            if (typeof loadClips === 'function') {
                loadClips();
            }
            // If there's a result clip, we could show a "View" link in the toast
            // For now, just refresh
        } else if (result && result.error) {
            showToast(result.error, 'error');
        }
        return result;
    } catch (error) {
        console.error('Failed to execute plugin action:', error);
        showToast('Action failed: ' + (error.message || 'Unknown error'), 'error');
        return { success: false, error: error.message };
    }
}

// --- Open Plugin Options Dialog ---
// This function is called when a plugin action has options that need user input
// action: the full action object with plugin_id, id, label, options, etc.
// clipIds: array of clip IDs to apply the action to
function openPluginOptionsDialog(action, clipIds) {
    // TODO: Implement options dialog in Task 15
    // For now, execute directly without options
    console.warn('Options dialog not yet implemented, executing without options');
    executePluginAction(action.plugin_id, action.id, clipIds, {});
}
