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
let pluginUpdates = {}; // pluginID -> PluginUpdateInfo
let showingURLInput = false;
let pluginListRover = null;

// Focus management
let lastFocusedElementBeforePlugins = null;
let pluginsFocusTrapCleanup = null;

// --- Modal Open/Close ---
function openPlugins() {
    lastFocusedElementBeforePlugins = document.activeElement;
    pluginsModal.removeAttribute('inert');
    pluginsModal.classList.remove('opacity-0', 'pointer-events-none');
    pluginsModal.classList.add('opacity-100');
    pluginsModal.querySelector(':scope > div').classList.remove('scale-95');
    pluginsModal.querySelector(':scope > div').classList.add('scale-100');
    if (pluginsFocusTrapCleanup) pluginsFocusTrapCleanup();
    pluginsFocusTrapCleanup = trapFocus(pluginsModal);
    pluginsModal.focus();
    loadPlugins();
}

function closePlugins() {
    if (pluginsFocusTrapCleanup) {
        pluginsFocusTrapCleanup();
        pluginsFocusTrapCleanup = null;
    }
    pluginsModal.classList.add('opacity-0', 'pointer-events-none');
    pluginsModal.classList.remove('opacity-100');
    pluginsModal.querySelector(':scope > div').classList.add('scale-95');
    pluginsModal.querySelector(':scope > div').classList.remove('scale-100');
    pluginsModal.setAttribute('inert', '');
    expandedPluginId = null;
    if (lastFocusedElementBeforePlugins && lastFocusedElementBeforePlugins !== document.body
        && !lastFocusedElementBeforePlugins.closest('[inert]')) {
        lastFocusedElementBeforePlugins.focus();
    } else {
        document.getElementById('drawer-toggle-btn')?.focus();
    }
    lastFocusedElementBeforePlugins = null;
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
    // Cards (and any search comboboxes inside them) are rebuilt from scratch
    // here; destroy the old comboboxes so their document-level listeners don't
    // leak.
    destroyActiveSearchFields();
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

    // Initialize roving tabindex for plugin list
    if (pluginListRover) pluginListRover.destroy();
    if (pluginsList.children.length > 0) {
        pluginListRover = RovingTabindex.create({
            container: pluginsList,
            itemSelector: ':scope > li',
            orientation: 'vertical',
            wrap: false,
        });
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
                            ${pluginUpdates[plugin.id] ? '<span class="text-[9px] text-amber-600 font-medium ml-1">Update available</span>' : ''}
                        </div>
                        ${plugin.author ? `<p class="text-[11px] text-stone-400 truncate">by ${escapeHTML(plugin.author)}</p>` : ''}
                    </div>
                </div>
                <div class="flex items-center gap-2">
                    ${pluginUpdates[plugin.id] ? `<button data-action="update" data-testid="update-plugin-${plugin.id}"
                        class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-[10px] font-medium py-1 px-2 rounded-md transition-colors">
                        Update
                    </button>` : ''}
                    <label class="relative inline-flex items-center cursor-pointer" data-action="toggle-enable">
                        <input type="checkbox" data-testid="plugin-toggle-${plugin.id}"
                               class="sr-only peer" ${plugin.enabled ? 'checked' : ''}>
                        <div class="w-9 h-5 bg-stone-300 peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-stone-400 rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:bg-emerald-500"></div>
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

    const updateBtn = li.querySelector('[data-action="update"]');
    if (updateBtn) {
        updateBtn.addEventListener('click', async (e) => {
            e.stopPropagation();
            await updatePlugin(plugin.id, plugin.name);
        });
    }

    // Load permissions and settings if expanded
    if (isExpanded) {
        loadPluginPermissions(plugin.id, li);
        loadPluginSettings(plugin.id, li);
    }

    return li;
}

// --- Render Settings Section ---
function renderSettingsSection(settings, pluginId, storageValues, pluginName, settingLabels) {
    if (!settings || settings.length === 0) {
        return '';
    }

    const fields = settings.map(field => {
        const currentValue = storageValues[field.key];
        const hasDefault = field.default !== undefined && field.default !== null;
        const hasCurrentValue = currentValue !== undefined && currentValue !== null;
        const displayValue = hasCurrentValue ? currentValue : (hasDefault ? field.default : '');

        return renderSettingField(field, displayValue, pluginId, pluginName || '', settingLabels || {});
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

function renderSettingField(field, currentValue, pluginId, pluginName, settingLabels) {
    const description = field.description
        ? `<p class="text-[10px] text-stone-400 mt-1">${escapeHTML(field.description)}</p>`
        : '';

    switch (field.type) {
        case 'text':
            const textValue = currentValue !== undefined && currentValue !== null ? String(currentValue) : '';
            const textPlaceholder = field.default !== undefined && field.default !== null ? String(field.default) : '';
            return `
                <div class="setting-field" data-key="${escapeHTML(field.key)}">
                    <label class="block text-[11px] font-medium text-stone-600 mb-1">${escapeHTML(field.label)}</label>
                    <input type="text"
                           class="block w-full border border-stone-200 rounded-md text-xs bg-white px-2 py-1.5 placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors"
                           value="${escapeHTML(textValue)}"
                           placeholder="${escapeHTML(textPlaceholder)}"
                           data-plugin-id="${pluginId}"
                           data-setting-key="${escapeHTML(field.key)}"
                           data-setting-type="text">
                    ${description}
                </div>
            `;

        case 'password':
            const passwordValue = currentValue !== undefined && currentValue !== null ? String(currentValue) : '';
            return `
                <div class="setting-field" data-key="${escapeHTML(field.key)}">
                    <label class="block text-[11px] font-medium text-stone-600 mb-1">${escapeHTML(field.label)}</label>
                    <div class="relative">
                        <input type="password"
                               class="block w-full border border-stone-200 rounded-md text-xs bg-white px-2 py-1.5 pr-8 placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors"
                               value="${escapeHTML(passwordValue)}"
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

        case 'search': {
            // The visible input shows the label; the value lives in a hidden
            // input. Display the remembered label only while its value still
            // equals what plugin storage holds — otherwise fall back to the
            // raw id (or nothing).
            const searchValue = currentValue !== undefined && currentValue !== null ? String(currentValue) : '';
            const remembered = settingLabels[pluginSettingLabelKey(pluginName, field.key)];
            let displayLabel = searchValue;
            if (searchValue && remembered && String(remembered.value) === searchValue) {
                displayLabel = String(remembered.label || searchValue);
            }
            return `
                <div class="setting-field" data-key="${escapeHTML(field.key)}">
                    <label class="block text-[11px] font-medium text-stone-600 mb-1">${escapeHTML(field.label)}</label>
                    <div class="relative">
                        <input type="text"
                               class="block w-full border border-stone-200 rounded-md text-xs bg-white px-2 py-1.5 pr-7 placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors"
                               value="${escapeHTML(displayLabel)}"
                               placeholder="Search…"
                               aria-label="${escapeHTML(field.label)}"
                               data-plugin-id="${pluginId}"
                               data-setting-key="${escapeHTML(field.key)}"
                               data-setting-type="search"
                               data-setting-source="${escapeHTML(field.source || '')}"
                               data-setting-plugin-name="${escapeHTML(pluginName)}">
                        <input type="hidden" data-setting-type="search-value" value="${escapeHTML(searchValue)}">
                        <button type="button"
                                class="absolute right-2 top-1/2 -translate-y-1/2 text-stone-400 hover:text-stone-600 ${searchValue ? '' : 'hidden'}"
                                data-action="clear-search-setting"
                                aria-label="Clear ${escapeHTML(field.label)}">
                            <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M6 18L18 6M6 6l12 12" />
                            </svg>
                        </button>
                    </div>
                    ${description}
                </div>
            `;
        }

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

        // Load remembered labels for search fields (UI-only state)
        const labels = await loadPluginSettingLabels();

        // Render settings section
        placeholder.innerHTML = renderSettingsSection(plugin.settings, pluginId, storageValues || {}, plugin.name || '', labels || {});

        // Add event listeners for setting changes
        setupSettingListeners(cardElement, pluginId);
    } catch (error) {
        console.error('Failed to load plugin settings:', error);
        placeholder.innerHTML = '<span class="text-red-500 text-[11px]">Failed to load settings</span>';
    }
}

// --- Setup Setting Event Listeners ---
let settingDebounceTimers = {};

// The visible picker input shows a human label; the stored value is the id.
// The label is UI-only state and must not go into plugin storage — a <key>__label
// twin would collide with the plugin's own freely-writable keyspace and could
// drift out of sync with the value. It lives in an app setting keyed
// <plugin_name>::<key> instead, and is only displayed while its value still
// equals what plugin storage holds (plugin storage is independently writable
// from Lua, so a remembered label alone could read "Trips" over an id the
// plugin has since rewritten).
const PLUGIN_SETTING_LABELS_KEY = 'plugin_setting_labels';
let pluginSettingLabels = null; // { "<plugin_name>::<key>": {value, label} }

async function loadPluginSettingLabels() {
    if (pluginSettingLabels) return pluginSettingLabels;
    try {
        const raw = await window.go.main.App.GetSetting(PLUGIN_SETTING_LABELS_KEY);
        const parsed = raw ? JSON.parse(raw) : null;
        pluginSettingLabels = (parsed && typeof parsed === 'object') ? parsed : {};
    } catch (error) {
        console.error('Failed to load plugin setting labels:', error);
        pluginSettingLabels = {};
    }
    return pluginSettingLabels;
}

async function persistPluginSettingLabels() {
    try {
        await window.go.main.App.SetSetting(PLUGIN_SETTING_LABELS_KEY, JSON.stringify(pluginSettingLabels));
    } catch (error) {
        console.error('Failed to save plugin setting labels:', error);
    }
}

function pluginSettingLabelKey(pluginName, key) {
    return `${pluginName}::${key}`;
}

// Handles for comboboxes rendered inside the plugins list. The list re-renders
// whole cards, which would orphan the comboboxes' document-level listeners, so
// they are tracked and destroyed on re-render.
let activeSearchFields = [];

function destroyActiveSearchFields() {
    activeSearchFields.forEach(field => field.destroy());
    activeSearchFields = [];
}

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

    // Search fields (async combobox over the plugin's on_search hook)
    cardElement.querySelectorAll('input[data-setting-type="search"]').forEach(input => {
        const fieldDiv = input.closest('.setting-field');
        const hiddenInput = fieldDiv?.querySelector('input[data-setting-type="search-value"]');
        const key = input.dataset.settingKey;
        const pluginName = input.dataset.settingPluginName || '';
        if (!hiddenInput) return;
        activeSearchFields.push(PluginSearchField.attach({
            input,
            hiddenInput,
            pluginId,
            source: input.dataset.settingSource || '',
            onSelect: async (value, label) => {
                await saveSetting(pluginId, key, value);
                pluginSettingLabels = pluginSettingLabels || {};
                pluginSettingLabels[pluginSettingLabelKey(pluginName, key)] = { value, label };
                persistPluginSettingLabels();
                // The clear button renders hidden when the stored value was
                // empty; reveal it once a value exists.
                fieldDiv.querySelector('[data-action="clear-search-setting"]')?.classList.remove('hidden');
            },
        }));
    });

    // Explicit clear for search fields. Clearing must persist: resetting only
    // the hidden input would leave auto-upload still using the stored owner.
    cardElement.querySelectorAll('[data-action="clear-search-setting"]').forEach(btn => {
        btn.addEventListener('click', async () => {
            const fieldDiv = btn.closest('.setting-field');
            const input = fieldDiv?.querySelector('input[data-setting-type="search"]');
            const hiddenInput = fieldDiv?.querySelector('input[data-setting-type="search-value"]');
            if (!input || !hiddenInput) return;
            await saveSetting(pluginId, input.dataset.settingKey, '');
            input.value = '';
            hiddenInput.value = '';
            btn.classList.add('hidden');
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
        await loadPluginUIActions();
        loadClips(); // re-render cards with updated plugin actions
    } catch (error) {
        console.error('Failed to toggle plugin:', error);
        showToast('Failed to update plugin');
        await loadPlugins(); // Refresh to correct UI state
    }
}

// --- Import Plugin ---
async function importPlugin() {
    try {
        const preview = await window.go.main.PluginService.ImportPlugin();
        if (!preview) return; // User cancelled file dialog

        const approved = await showPluginReview(preview, 'install');
        if (!approved) return;

        const result = await window.go.main.PluginService.ConfirmPluginInstall(preview.source);
        if (result) {
            showToast(`Installed: ${result.name}`);
            await loadPlugins();
            await loadPluginUIActions();
            loadClips();
        }
    } catch (error) {
        console.error('Failed to import plugin:', error);
        showToast('Failed to import plugin: ' + (error.message || 'Unknown error'));
    }
}

async function installFromURL() {
    const urlInput = document.getElementById('plugin-url-input');
    const url = urlInput?.value?.trim();
    if (!url) {
        showToast('Please enter a URL');
        return;
    }

    try {
        const installBtn = document.getElementById('plugin-url-install-btn');
        if (installBtn) {
            installBtn.disabled = true;
            installBtn.textContent = 'Fetching...';
        }

        const preview = await window.go.main.PluginService.PreviewPluginFromURL(url);
        if (!preview) {
            showToast('Failed to fetch plugin from URL');
            return;
        }

        const approved = await showPluginReview(preview, 'install');
        if (!approved) return;

        const result = await window.go.main.PluginService.ConfirmPluginInstall(url);
        if (result) {
            showToast(`Installed: ${result.name}`);
            hideURLInput();
            await loadPlugins();
            await loadPluginUIActions();
            loadClips();
        }
    } catch (error) {
        console.error('Failed to install from URL:', error);
        showToast('Failed to install: ' + (error.message || 'Unknown error'));
    } finally {
        const installBtn = document.getElementById('plugin-url-install-btn');
        if (installBtn) {
            installBtn.disabled = false;
            installBtn.textContent = 'Install';
        }
    }
}

function toggleURLInput() {
    showingURLInput = !showingURLInput;
    renderURLInput();
}

function hideURLInput() {
    showingURLInput = false;
    renderURLInput();
}

function renderURLInput() {
    const container = document.getElementById('plugin-url-container');
    if (!container) return;

    if (showingURLInput) {
        container.classList.remove('hidden');
        container.innerHTML = `
            <div class="flex gap-2 mt-2">
                <input id="plugin-url-input" type="url" placeholder="https://example.com/plugin.lua"
                    data-testid="plugin-url-input"
                    class="flex-1 border border-stone-200 rounded-md text-xs bg-white px-2 py-1.5 placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors"
                    autocomplete="off">
                <button id="plugin-url-install-btn" data-testid="plugin-url-install-btn"
                    class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-1.5 px-3 rounded-md transition-colors whitespace-nowrap"
                    onclick="installFromURL()">Install</button>
            </div>
        `;
        container.querySelector('input')?.focus();
        container.querySelector('input')?.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') installFromURL();
        });
    } else {
        container.classList.add('hidden');
        container.innerHTML = '';
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
            await loadPluginUIActions();
            loadClips();
        } catch (error) {
            console.error('Failed to remove plugin:', error);
            showToast('Failed to remove plugin');
        }
    }, null, { confirmLabel: 'Remove' });
}

async function updatePlugin(pluginId, pluginName) {
    try {
        const result = await window.go.main.PluginService.UpdatePlugin(pluginId);

        if (result.needs_review && result.preview) {
            const plugin = pluginsCache.find(p => p.id === pluginId);
            const currentVersion = plugin?.version || '?';
            const approved = await showPluginReview(result.preview, 'update', currentVersion);
            if (!approved) return;

            const updated = await window.go.main.PluginService.ConfirmPluginUpdate(pluginId);
            if (updated) {
                showToast(`Updated ${pluginName} to v${updated.version}`);
                delete pluginUpdates[pluginId];
                await loadPlugins();
                await loadPluginUIActions();
                loadClips();
            }
        } else if (result.success && result.plugin_info) {
            showToast(`Updated ${pluginName} to v${result.plugin_info.version}`);
            delete pluginUpdates[pluginId];
            await loadPlugins();
            await loadPluginUIActions();
            loadClips();
        } else if (result.error) {
            showToast(result.error, 'error');
        }
    } catch (error) {
        console.error('Failed to update plugin:', error);
        showToast('Failed to update plugin: ' + (error.message || 'Unknown error'));
    }
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

// Escape handling for plugin modals is unified in ShortcutManager (closeTopModalOverlay).

// Capture the invocation context for a plugin action. When the user is in
// folder view inside a folder, this carries the active folder's tag so plugins
// (e.g. fal.ai generate) can place their results into the current folder.
// Returns an empty object outside folder view.
function getPluginActionContext() {
    const context = {};
    try {
        if (typeof isFolderMode === 'function' && isFolderMode()
            && Array.isArray(activeTagFilters) && activeTagFilters.length > 0) {
            const tagId = activeTagFilters[activeTagFilters.length - 1];
            context.folder_tag_id = tagId;
            const tag = Array.isArray(allTags) ? allTags.find(t => t.id === tagId) : null;
            context.folder_tag_path = tag ? tag.name : '';
        }
    } catch (e) {
        // Folder context is best-effort; never block the action on it.
    }
    return context;
}

// --- Execute Plugin Action ---
// This function is called when a plugin action is triggered from card menu or lightbox.
// If isAsync is true, the backend runs the action in a background goroutine and returns immediately.
async function executePluginAction(pluginId, actionId, clipIds, options, isAsync) {
    try {
        const context = getPluginActionContext();
        const result = await window.go.main.PluginService.ExecutePluginAction(pluginId, actionId, clipIds, options || {}, context);
        if (result && result.success) {
            if (result.modal) {
                // Acquire modal guard so concurrent modal.show() calls are blocked
                const acquired = await window.go.main.PluginService.TryAcquireModalGuard();
                if (acquired) {
                    showPluginResultModal(result.modal);
                } else {
                    showToast('Cannot show result — another modal is open', 'error');
                }
            } else if (isAsync) {
                showToast('Processing started...');
            } else {
                showToast('Action completed');
                if (typeof loadClips === 'function') {
                    loadClips();
                }
            }
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

// --- Plugin Options Dialog ---
// Dialog state
let currentPluginAction = null;
let currentActionClipIds = [];
let currentDialogSearchFields = []; // PluginSearchField handles to destroy on close

// --- Remembered option values ---
// The options dialog is shared by every plugin action, so the last submitted
// values are stored per plugin + action and restored the next time that dialog
// opens. Free-text and password fields are deliberately not remembered: prompts
// are meant to be rewritten each run and secrets should not be replayed.
const PLUGIN_OPTION_MEMORY_KEY = 'plugin_action_options';
const REMEMBERED_OPTION_TYPES = new Set(['select', 'checkbox', 'range', 'search']);
let pluginOptionMemory = {};

// Keyed by plugin name rather than row id so choices survive a reinstall, and
// so an action offered from several places (fal.ai's "Upscale" sits on both the
// lightbox and the card menu) shares one remembered set.
function pluginOptionMemoryKey(action) {
    return `${action.plugin_name || action.plugin_id}::${action.id}`;
}

async function loadPluginOptionMemory() {
    try {
        const raw = await window.go.main.App.GetSetting(PLUGIN_OPTION_MEMORY_KEY);
        const parsed = raw ? JSON.parse(raw) : null;
        pluginOptionMemory = (parsed && typeof parsed === 'object') ? parsed : {};
    } catch (error) {
        pluginOptionMemory = {};
    }
}

// The value a field should start at: whatever was chosen last time, falling
// back to the manifest default.
function rememberedOptionValue(action, field) {
    if (!REMEMBERED_OPTION_TYPES.has(field.type)) return field.default;
    const saved = pluginOptionMemory[pluginOptionMemoryKey(action)];
    if (!saved || !(field.id in saved)) return field.default;
    const value = saved[field.id];

    // A plugin update can rename or drop what was remembered, so stale values
    // fall back to the default rather than leaving the control unset.
    switch (field.type) {
        case 'select':
            return (field.choices || []).some(c => String(c.value) === String(value))
                ? value : field.default;
        case 'checkbox':
            return value === true;
        case 'search':
            // A search field has no manifest choices to validate against, so
            // the stored {value, label} pair (captured together at selection
            // time) is what stands in for that check.
            return (value && typeof value === 'object' && 'value' in value) ? value : field.default;
        default: // range
            return Number.isFinite(Number(value)) ? Number(value) : field.default;
    }
}

async function savePluginOptionMemory(action, options, searchLabels) {
    const remembered = {};
    (action.options || []).forEach(field => {
        if (!REMEMBERED_OPTION_TYPES.has(field.type) || !(field.id in options)) return;
        if (field.type === 'search') {
            // Persist the {value, label} pair so the dialog can reopen showing
            // the group name, not just the raw id. A cleared/typed-but-not-
            // selected value (empty value) must not reopen looking like a
            // remembered choice, so its label is dropped too.
            const value = options[field.id];
            remembered[field.id] = {
                value,
                label: value === '' || value == null ? '' : ((searchLabels && searchLabels[field.id]) || value),
            };
        } else {
            remembered[field.id] = options[field.id];
        }
    });
    if (Object.keys(remembered).length === 0) return;

    pluginOptionMemory[pluginOptionMemoryKey(action)] = remembered;
    try {
        await window.go.main.App.SetSetting(PLUGIN_OPTION_MEMORY_KEY, JSON.stringify(pluginOptionMemory));
    } catch (error) {
        console.error('Failed to save plugin option values:', error);
    }
}

// This function is called when a plugin action has options that need user input
// action: the full action object with plugin_id, id, label, options, etc.
// clipIds: array of clip IDs to apply the action to
function openPluginOptionsDialog(action, clipIds) {
    currentDialogSearchFields.forEach(field => field.destroy());
    currentDialogSearchFields = [];
    currentPluginAction = action;
    currentActionClipIds = clipIds;

    const modal = document.getElementById('plugin-options-modal');
    const title = document.getElementById('plugin-options-title');
    const form = document.getElementById('plugin-options-form');

    // Set title
    const clipCount = clipIds.length;
    if (clipCount === 0) {
        title.textContent = action.label;
    } else {
        title.textContent = `${action.label} - ${clipCount} ${clipCount === 1 ? 'clip' : 'clips'}`;
    }

    // Render form fields
    form.innerHTML = '';
    action.options.forEach(field => {
        const wrapper = document.createElement('div');
        wrapper.className = 'form-field';

        const label = document.createElement('label');
        label.className = 'form-label';
        label.textContent = field.label;
        if (field.required) {
            label.innerHTML += '<span class="text-red-500 ml-1">*</span>';
        }
        label.setAttribute('for', `plugin-opt-${field.id}`);

        const initial = rememberedOptionValue(action, field);

        let input;
        switch (field.type) {
            case 'checkbox':
                input = document.createElement('input');
                input.type = 'checkbox';
                input.className = 'form-checkbox';
                input.checked = initial === true;
                break;

            case 'select':
                input = document.createElement('select');
                input.className = 'form-select';
                field.choices?.forEach(choice => {
                    const opt = document.createElement('option');
                    opt.value = choice.value;
                    opt.textContent = choice.label;
                    if (String(choice.value) === String(initial)) opt.selected = true;
                    input.appendChild(opt);
                });
                break;

            case 'range':
                input = document.createElement('input');
                input.type = 'range';
                input.className = 'form-range';
                const hasMin = field.min !== undefined && field.min !== null;
                const hasMax = field.max !== undefined && field.max !== null;
                const hasStep = field.step !== undefined && field.step !== null;
                const hasInitial = initial !== undefined && initial !== null;

                input.min = hasMin ? String(field.min) : '0';
                input.max = hasMax ? String(field.max) : '1';
                input.step = hasStep ? String(field.step) : '0.1';
                input.value = hasInitial ? String(initial) : (hasMin ? String(field.min) : '0');

                const valueDisplay = document.createElement('span');
                valueDisplay.className = 'form-range-value';
                valueDisplay.textContent = input.value;
                input.addEventListener('input', () => valueDisplay.textContent = input.value);

                wrapper.appendChild(label);
                const rangeWrapper = document.createElement('div');
                rangeWrapper.className = 'form-range-wrapper';
                rangeWrapper.appendChild(input);
                rangeWrapper.appendChild(valueDisplay);
                wrapper.appendChild(rangeWrapper);
                input.id = `plugin-opt-${field.id}`;
                input.name = field.id;
                form.appendChild(wrapper);
                return;

            case 'search': {
                // Visible input carries the label; a hidden input named for
                // the field carries the value the form actually submits.
                const searchContainer = document.createElement('div');
                searchContainer.className = 'relative';

                input = document.createElement('input');
                input.type = 'text';
                input.className = 'form-input pr-7';
                input.placeholder = 'Search…';
                input.value = initial && typeof initial === 'object' ? String(initial.label || '') : '';

                const hidden = document.createElement('input');
                hidden.type = 'hidden';
                hidden.name = field.id;
                hidden.value = initial && typeof initial === 'object' ? String(initial.value ?? '') : '';

                searchContainer.appendChild(input);
                searchContainer.appendChild(hidden);
                wrapper.appendChild(label);
                wrapper.appendChild(searchContainer);
                input.id = `plugin-opt-${field.id}`;
                const searchField = PluginSearchField.attach({
                    input,
                    hiddenInput: hidden,
                    pluginId: action.plugin_id,
                    source: field.source || '',
                });
                currentDialogSearchFields.push(searchField);
                form.appendChild(wrapper);
                return;
            }

            default: // text, password
                input = document.createElement('input');
                input.type = field.type === 'password' ? 'password' : 'text';
                input.className = 'form-input';
                input.value = field.default !== undefined && field.default !== null ? String(field.default) : '';
                input.placeholder = field.label;
        }

        input.id = `plugin-opt-${field.id}`;
        input.name = field.id;
        if (field.required) input.required = true;

        wrapper.appendChild(label);
        if (field.type === 'checkbox') {
            const checkWrapper = document.createElement('div');
            checkWrapper.className = 'form-checkbox-wrapper';
            checkWrapper.appendChild(input);
            wrapper.appendChild(checkWrapper);
        } else {
            wrapper.appendChild(input);
        }
        form.appendChild(wrapper);
    });

    // Show modal
    modal.removeAttribute('inert');
    modal.classList.remove('opacity-0', 'pointer-events-none');
    modal.classList.add('opacity-100');
}

function closePluginOptionsDialog() {
    // Tear down comboboxes so their document-level listeners don't leak
    // across dialog opens.
    currentDialogSearchFields.forEach(field => field.destroy());
    currentDialogSearchFields = [];
    const modal = document.getElementById('plugin-options-modal');
    modal.classList.remove('opacity-100');
    modal.classList.add('opacity-0', 'pointer-events-none');
    modal.setAttribute('inert', '');
    currentPluginAction = null;
    currentActionClipIds = [];
}

// --- Plugin Options Dialog Event Listeners ---
document.getElementById('plugin-options-close')?.addEventListener('click', closePluginOptionsDialog);
document.getElementById('plugin-options-cancel')?.addEventListener('click', closePluginOptionsDialog);

document.getElementById('plugin-options-form')?.addEventListener('submit', async (e) => {
    e.preventDefault();

    if (!currentPluginAction) return;

    // Required search fields cannot rely on HTML validation — hidden inputs
    // are skipped by constraint validation — so check them here and keep the
    // dialog open when a required pick is missing.
    const missingRequiredSearch = (currentPluginAction.options || []).filter(field =>
        field.type === 'search' && field.required &&
        !(document.querySelector(`#plugin-options-form [name="${field.id}"]`)?.value)
    );
    if (missingRequiredSearch.length > 0) {
        showToast(`Please choose a value for "${missingRequiredSearch[0].label}"`, 'error');
        return;
    }

    // Gather form values
    const formData = new FormData(e.target);
    const options = {};
    const searchLabels = {}; // search fields: visible label, captured before close

    currentPluginAction.options.forEach(field => {
        const value = formData.get(field.id);
        switch (field.type) {
            case 'checkbox':
                options[field.id] = document.getElementById(`plugin-opt-${field.id}`).checked;
                break;
            case 'range':
                options[field.id] = parseFloat(value);
                break;
            case 'search':
                // The hidden input named for the field carries the value; it is
                // cleared on any keystroke, so only an actual selection lands.
                options[field.id] = value || '';
                {
                    const visible = document.getElementById(`plugin-opt-${field.id}`);
                    searchLabels[field.id] = visible ? visible.value : '';
                }
                break;
            default:
                options[field.id] = value;
        }
    });

    const action = currentPluginAction;
    const clipIds = currentActionClipIds;
    closePluginOptionsDialog();
    await savePluginOptionMemory(action, options, searchLabels);
    await executePluginAction(action.plugin_id, action.id, clipIds, options, action.async);
});

// Close on backdrop click
document.getElementById('plugin-options-modal')?.addEventListener('click', (e) => {
    if (e.target.id === 'plugin-options-modal') closePluginOptionsDialog();
});

// Listen for plugin update events
if (window.runtime && window.runtime.EventsOn) {
    window.runtime.EventsOn('plugin:update_available', (info) => {
        if (info && info.plugin_id) {
            pluginUpdates[info.plugin_id] = info;
            // Re-render if plugins modal is open
            if (!pluginsModal.classList.contains('opacity-0')) {
                renderPluginsList();
            }
        }
    });
}
