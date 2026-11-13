// --- View Switching ---
// Shared view state: 'clips', 'watch', 'serve'
let currentView = 'clips';

// --- Serve View State ---
let isViewingServe = false;
let servePollingInterval = null;
const bindPreferences = new Map(); // tagID -> bindAll preference
const apiAccessPreferences = new Map(); // tagID -> "none" | "read" | "readwrite"
// Configured entries: tags the user has added to the serve list (persists when stopped)
// Each entry: { tag_id, tag_name, bind_all }
const configuredEntries = new Map();

// --- Remembered serve list ---
//
// The list, each entry's Local/Network and API choice, and the port it last
// ran on are kept in one setting, so they survive a restart and a tag's URL
// stays the same from one start to the next. Nothing is started from it: a
// server only ever runs because the user pressed Start. The setting is
// machine-local (the backend keeps it out of backups and refuses it over
// REST), so in server mode the save simply fails and the list lasts for the
// session, as before.
const SERVE_ENTRIES_SETTING = 'serve_entries';
let serveEntriesRestored = false;
let lastSavedServeEntries = null;

function serveEntriesSnapshot() {
    return JSON.stringify(Array.from(configuredEntries.values()).map(entry => ({
        tag_id: entry.tag_id,
        // The name the choices were made for: an id can come to mean another
        // tag (a backup restored over the library), and "Network, read/write"
        // must not follow the id to a tag nobody configured.
        tag_name: entry.tag_name,
        bind_all: bindPreferences.has(entry.tag_id) ? !!bindPreferences.get(entry.tag_id) : !!entry.bind_all,
        api_access: apiAccessPreferences.get(entry.tag_id) || 'none',
        last_port: Number(entry.last_port) || 0,
    })));
}

function persistServeEntries() {
    if (!serveEntriesRestored) return; // nothing to save over what is stored yet
    const snapshot = serveEntriesSnapshot();
    if (snapshot === lastSavedServeEntries) return;
    lastSavedServeEntries = snapshot;
    window.go.main.App.SetSetting(SERVE_ENTRIES_SETTING, snapshot).catch(() => { /* session-only */ });
}

// Once, before the first render: bring back the stored entries whose tag
// still exists under the same name. An entry already present (a server
// running right now) keeps its live state and only takes the remembered port.
// Single-flight: every caller waits for the same restore.
let serveEntriesRestoring = null;
function restoreServeEntries() {
    if (serveEntriesRestored) return Promise.resolve();
    if (!serveEntriesRestoring) {
        serveEntriesRestoring = doRestoreServeEntries().finally(() => { serveEntriesRestoring = null; });
    }
    return serveEntriesRestoring;
}

async function doRestoreServeEntries() {
    try {
        const raw = await window.go.main.App.GetSetting(SERVE_ENTRIES_SETTING);
        const stored = raw ? JSON.parse(raw) : [];
        if (Array.isArray(stored) && stored.length > 0) {
            const tags = await window.go.main.App.GetTags();
            const names = new Map((tags || []).map(t => [Number(t.id), t.name]));
            for (const item of stored) {
                const tagID = Number(item && item.tag_id);
                if (!names.has(tagID) || names.get(tagID) !== item.tag_name) continue;
                const port = Number(item.last_port);
                const lastPort = Number.isInteger(port) && port > 0 && port < 65536 ? port : 0;
                const present = configuredEntries.get(tagID);
                if (present) {
                    if (!present.last_port) present.last_port = lastPort;
                    continue;
                }
                configuredEntries.set(tagID, {
                    tag_id: tagID,
                    tag_name: names.get(tagID),
                    bind_all: item.bind_all === true,
                    last_port: lastPort,
                });
                if (['read', 'readwrite'].includes(item.api_access)) apiAccessPreferences.set(tagID, item.api_access);
            }
        }
    } catch (error) {
        console.error('Failed to restore the serve list:', error);
    }
    serveEntriesRestored = true;
    lastSavedServeEntries = serveEntriesSnapshot();
}

// --- Serve Elements ---
const serveView = document.getElementById('serve-view');
const serveList = document.getElementById('serve-list');
const serveBackBtn = document.getElementById('serve-back-btn');
const addServeBtn = document.getElementById('add-serve-btn');
const addServeZone = document.getElementById('add-serve-zone');

// --- Share Elements ---
const shareView = document.getElementById('share-view');

// --- View Switching ---
function switchView(view) {
    const prev = currentView;
    currentView = view;

    // Update tab button styles
    const tabs = document.querySelectorAll('[data-view]');
    tabs.forEach(tab => {
        const tabView = tab.dataset.view;
        if (tabView === view) {
            tab.classList.add('bg-stone-800', 'text-white');
            tab.classList.remove('text-stone-400', 'hover:bg-stone-100');
            tab.setAttribute('aria-selected', 'true');
        } else {
            tab.classList.remove('bg-stone-800', 'text-white');
            tab.classList.add('text-stone-400', 'hover:bg-stone-100');
            tab.setAttribute('aria-selected', 'false');
        }
    });

    // Hide all view sections
    gallery.parentElement.classList.add('hidden');
    watchView.classList.add('hidden');
    serveView.classList.add('hidden');
    if (shareView) shareView.classList.add('hidden');

    // Stop serve polling when leaving serve view
    if (prev === 'serve' && view !== 'serve') {
        stopServePolling();
    }

    // Update legacy flags for backward compatibility
    isViewingWatch = (view === 'watch');
    isViewingServe = (view === 'serve');

    // Toggle clip-related controls visibility
    const isClipView = (view === 'clips');

    // Close open popovers before hiding (keyboard shortcuts bypass click-outside handlers)
    if (!isClipView) {
        if (typeof closeSortPopover === 'function') closeSortPopover();
        if (typeof closeTagFilterDropdown === 'function') closeTagFilterDropdown(false);
    }

    const ccClipControls = document.getElementById('clip-controls');
    const ccAddBtn = document.getElementById('add-btn');
    const ccExpirySelect = document.getElementById('expiry-select');
    const ccClipCount = document.getElementById('clip-count');
    const ccActiveTags = document.getElementById('active-tags-container');

    if (ccClipControls) ccClipControls.classList.toggle('hidden', !isClipView);
    if (ccAddBtn) ccAddBtn.classList.toggle('hidden', !isClipView);
    if (ccExpirySelect) ccExpirySelect.classList.toggle('hidden', !isClipView);
    if (ccClipCount) ccClipCount.classList.toggle('hidden', !isClipView);

    // active-tags-container: hide on departure; restore on return by calling
    // updateActiveTagsDisplay() which correctly shows/hides based on active filters.
    if (!isClipView && ccActiveTags) {
        ccActiveTags.classList.add('hidden');
    }

    // Show the selected view and trigger its load
    switch (view) {
        case 'clips':
            gallery.parentElement.classList.remove('hidden');
            clearMediaCaches();
            // Restore active tags bar if filters are still active
            if (typeof updateActiveTagsDisplay === 'function') updateActiveTagsDisplay();
            loadClips();
            break;
        case 'watch':
            watchView.classList.remove('hidden');
            loadWatchFolders();
            break;
        case 'serve':
            serveView.classList.remove('hidden');
            loadServeStatus({ force: true });
            startServePolling();
            break;
        case 'share':
            if (shareView) shareView.classList.remove('hidden');
            if (window.ShareView && typeof window.ShareView.refresh === 'function') {
                window.ShareView.refresh();
            }
            break;
    }
    if (window.folderStatusPoller) window.folderStatusPoller.evaluate();
}

// Tab click handlers
document.querySelectorAll('[data-view]').forEach(tab => {
    tab.addEventListener('click', () => {
        switchView(tab.dataset.view);
        if (typeof closeDrawer === 'function') closeDrawer();
    });
});

// --- Server Card Rendering ---
function renderServeCard(info) {
    const li = document.createElement('li');
    li.className = 'bg-white rounded-md border border-stone-200 p-4 flex items-center justify-between gap-4 transition-all duration-150 hover:border-stone-300';
    li.dataset.tagId = info.tag_id;

    const statusDot = info.running
        ? '<span class="w-2.5 h-2.5 rounded-full bg-emerald-500 flex-shrink-0"></span>'
        : '<span class="w-2.5 h-2.5 rounded-full bg-stone-300 flex-shrink-0"></span>';

    const urlDisplay = info.running && info.url
        ? `<button class="serve-url-copy text-[11px] text-stone-500 hover:text-stone-700 underline underline-offset-2 decoration-stone-300 truncate max-w-[200px] transition-colors" data-url="${escapeHTML(info.url)}" title="Click to copy URL">${escapeHTML(info.url)}</button>
           <button type="button" class="serve-url-open desktop-only text-[11px] text-stone-500 hover:text-stone-700 underline underline-offset-2 decoration-stone-300 transition-colors" data-url="${escapeHTML(info.url)}" aria-label="Open ${escapeHTML(info.url)} in the browser">Open</button>`
        : '<span class="text-[11px] text-stone-400">Not running</span>';

    const requestBadge = info.running
        ? `<span class="text-[10px] text-stone-400 tabular-nums">${info.request_count} req${info.request_count !== 1 ? 's' : ''}</span>`
        : '';

    const toggleLabel = info.running ? 'Stop' : 'Start';
    const toggleClass = info.running
        ? 'border border-stone-200 hover:border-red-300 hover:bg-red-50 text-stone-500 hover:text-red-600'
        : 'bg-stone-800 hover:bg-stone-700 text-white';

    const bindLabel = info.bind_all ? 'Network' : 'Local';
    const bindDisabled = info.running ? 'pointer-events-none opacity-50' : 'cursor-pointer';

    const currentApiAccess = apiAccessPreferences.get(info.tag_id) || 'none';
    const apiDisabled = info.running ? 'pointer-events-none opacity-50' : '';

    li.innerHTML = `
        <div class="flex items-center gap-3 min-w-0 flex-1">
            ${statusDot}
            <div class="min-w-0">
                <p class="text-xs font-medium text-stone-700 truncate">${escapeHTML(info.tag_name)}</p>
                <div class="flex items-center gap-2 mt-0.5">
                    ${urlDisplay}
                    ${requestBadge}
                </div>
            </div>
        </div>
        <div class="flex items-center gap-2 shrink-0">
            <button class="serve-bind-toggle text-[10px] font-medium py-1 px-2 rounded border transition-colors ${info.bind_all ? 'bg-stone-100 border-stone-300 text-stone-600' : 'border-stone-200 text-stone-400 hover:border-stone-300 hover:text-stone-600'} ${bindDisabled}"
                    data-tag-id="${info.tag_id}" data-bind-all="${info.bind_all}" ${info.running ? 'disabled' : ''} title="${info.bind_all ? 'Accessible from network (0.0.0.0)' : 'Local only (127.0.0.1)'}">
                ${bindLabel}
            </button>
            <select class="serve-api-access text-[10px] font-medium py-1 px-1.5 rounded border border-stone-200 text-stone-500 bg-white transition-colors ${apiDisabled}"
                    data-tag-id="${info.tag_id}" ${info.running ? 'disabled' : ''}>
                <option value="none"${currentApiAccess === 'none' ? ' selected' : ''}>No API</option>
                <option value="read"${currentApiAccess === 'read' ? ' selected' : ''}>API Read</option>
                <option value="readwrite"${currentApiAccess === 'readwrite' ? ' selected' : ''}>API R/W</option>
            </select>
            <button class="serve-toggle-btn text-[11px] font-medium py-1.5 px-3 rounded-md transition-colors ${toggleClass}"
                    data-tag-id="${info.tag_id}" data-running="${info.running}" data-bind-all="${info.bind_all}">
                ${toggleLabel}
            </button>
            ${!info.running ? `<button class="serve-remove-btn p-1 text-stone-400 hover:text-red-500 transition-colors rounded hover:bg-red-50" data-tag-id="${info.tag_id}" title="Remove from list" aria-label="Remove from list">
                <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M6 18L18 6M6 6l12 12"/></svg>
            </button>` : ''}
        </div>
    `;

    return li;
}

// What the list was last rendered from. The 2s poll re-renders only when this
// changes, so it does not rebuild the list (and drop focus or a click in
// progress) every tick.
let lastServeRenderKey = null;
// Tags whose Start/Stop is in flight; their buttons ignore further clicks.
const serveToggleInFlight = new Set();

// --- Load Serve Status ---
// `force` re-renders even if nothing changed or focus is inside the list (after
// the user's own action, which must show its result).
async function loadServeStatus({ force = false } = {}) {
    // Skip re-render if user is interacting with a dropdown to avoid
    // nuking the DOM mid-interaction (e.g. API access select being open).
    if (serveList.querySelector('select:focus')) return;
    if (!force && serveList.contains(document.activeElement)) return;

    try {
        await restoreServeEntries();
        const statuses = await window.go.main.ServeService.GetServeStatus();
        const runningMap = new Map();
        for (const s of (statuses || [])) {
            runningMap.set(s.tag_id, s);
            // Ensure running servers are in configured entries
            if (!configuredEntries.has(s.tag_id)) {
                configuredEntries.set(s.tag_id, { tag_id: s.tag_id, tag_name: s.tag_name, bind_all: s.bind_all });
            }
            // The port it runs on now is the one to ask for next time.
            if (Number(s.port) > 0) configuredEntries.get(s.tag_id).last_port = Number(s.port);
            if (s.api_access && s.api_access !== 'none') {
                apiAccessPreferences.set(s.tag_id, s.api_access);
            }
        }

        persistServeEntries();

        const renderKey = JSON.stringify({
            entries: [...configuredEntries.keys()],
            running: (statuses || []).map(st => [st.tag_id, st.url, st.request_count, st.bind_all, st.api_access]),
            bind: [...bindPreferences],
            api: [...apiAccessPreferences],
            busy: [...serveToggleInFlight],
        });
        if (!force && renderKey === lastServeRenderKey) return;
        if (!force && serveList.contains(document.activeElement)) return;
        lastServeRenderKey = renderKey;

        serveList.innerHTML = '';

        if (configuredEntries.size === 0) {
            serveList.innerHTML = '<li class="text-center text-sm text-stone-400 py-8">No tags being served</li>';
            return;
        }

        for (const [tagId, entry] of configuredEntries) {
            const running = runningMap.get(tagId);
            if (running) {
                serveList.appendChild(renderServeCard(running));
            } else {
                // Show stopped card
                const bindAll = bindPreferences.has(tagId) ? bindPreferences.get(tagId) : entry.bind_all;
                serveList.appendChild(renderServeCard({
                    tag_id: tagId,
                    tag_name: entry.tag_name,
                    port: 0,
                    bind_all: bindAll,
                    url: '',
                    running: false,
                    request_count: 0,
                }));
            }
        }
    } catch (error) {
        console.error('Failed to load serve status:', error);
        lastServeRenderKey = null;
        serveList.innerHTML = '<li class="text-center text-sm text-stone-400 py-8">Failed to load serve status</li>';
    }
}

// --- Polling ---
function startServePolling() {
    stopServePolling();
    servePollingInterval = setInterval(() => {
        if (currentView === 'serve') {
            loadServeStatus();
        }
    }, 2000);
}

function stopServePolling() {
    if (servePollingInterval) {
        clearInterval(servePollingInterval);
        servePollingInterval = null;
    }
}

// Shared helper: add a tag to configuredEntries (stopped state) and refresh the view.
// Used by the tag-picker selectOption and by openServeViewForTag (folder-context-menu entry point).
async function addConfiguredServeEntry(tagID, tagName) {
    // First, so a remembered entry for this tag is not shadowed by a default one.
    await restoreServeEntries();
    if (!configuredEntries.has(tagID)) {
        configuredEntries.set(tagID, { tag_id: tagID, tag_name: tagName, bind_all: false });
    }
    await loadServeStatus({ force: true });
}

// --- Tag Picker ---
async function showServeTagPicker() {
    // Remove existing picker if open
    const existing = document.getElementById('serve-tag-picker');
    if (existing) {
        closeServeTagPicker({ restoreFocus: false });
        return;
    }

    try {
        const tags = await window.go.main.App.GetTags();
        const availableTags = tags.filter(t => !configuredEntries.has(t.id));

        if (availableTags.length === 0) {
            // Two different dead ends used to share one message.
            showToast(tags.length === 0
                ? 'No tags yet. Tag a clip from its menu (Tags), or create a folder in folder mode, then serve it here.'
                : 'Every tag is already in this list.');
            return;
        }

        // Create dropdown
        const picker = document.createElement('div');
        picker.id = 'serve-tag-picker';
        // Its own keydown handler runs arrows, Enter and Escape; ShortcutManager
        // leaves them alone while focus is inside, and closes it on Escape from
        // anywhere else (closeServeTagPicker).
        picker.setAttribute('data-owns-escape', '');
        picker.setAttribute('data-owns-keys', '');
        picker.className = 'absolute left-1/2 -translate-x-1/2 mt-2 w-56 bg-white rounded-lg shadow-xl border border-stone-200 z-50';
        picker.innerHTML = `
            <div class="p-2 border-b border-stone-100">
                <span class="text-[10px] font-semibold text-stone-400 uppercase tracking-wide">Pick a tag</span>
            </div>
            <div class="max-h-48 overflow-y-auto py-1">
                ${availableTags.map(tag => `
                    <button class="serve-tag-option w-full text-left px-3 py-2 text-xs text-stone-600 hover:bg-stone-100 transition-colors flex items-center gap-2"
                            data-tag-id="${tag.id}">
                        <span class="w-2.5 h-2.5 rounded-full flex-shrink-0" style="background-color: ${safeTagColor(tag.color)}"></span>
                        ${escapeHTML(tag.name)}
                    </button>
                `).join('')}
            </div>
        `;

        // Position under add-serve-zone
        addServeZone.style.position = 'relative';
        addServeZone.appendChild(picker);

        // Select a tag option: add to list (stopped), user clicks Start
        async function selectOption(option) {
            const tagID = parseInt(option.dataset.tagId, 10);
            const tagName = option.textContent.trim();
            closeServeTagPicker({ restoreFocus: false });
            await addConfiguredServeEntry(tagID, tagName);
        }

        picker.addEventListener('click', async (e) => {
            const option = e.target.closest('.serve-tag-option');
            if (option) await selectOption(option);
        });

        // Keyboard navigation: arrow keys, Enter to select, Escape to close
        picker.addEventListener('keydown', (e) => {
            const options = [...picker.querySelectorAll('.serve-tag-option')];
            const current = document.activeElement;
            const idx = options.indexOf(current);

            if (e.key === 'ArrowDown') {
                e.preventDefault();
                const next = idx < options.length - 1 ? idx + 1 : 0;
                options[next].focus();
            } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                const prev = idx > 0 ? idx - 1 : options.length - 1;
                options[prev].focus();
            } else if (e.key === 'Enter') {
                e.preventDefault();
                if (current && options.includes(current)) selectOption(current);
            } else if (e.key === 'Escape') {
                e.preventDefault();
                closeServeTagPicker();
            }
        });

        // Focus first option for immediate keyboard use
        const firstOption = picker.querySelector('.serve-tag-option');
        if (firstOption) firstOption.focus();

        // Close on outside click
        const closeOnOutside = (e) => {
            if (!picker.contains(e.target) && e.target !== addServeBtn) {
                closeServeTagPicker({ restoreFocus: false });
            }
        };
        servePickerOutsideHandler = closeOnOutside;
        // Delay to avoid immediate close from the current click
        setTimeout(() => document.addEventListener('click', closeOnOutside), 0);

    } catch (error) {
        console.error('Failed to load tags for serve picker:', error);
        showToast('Failed to load tags', 'error');
    }
}

let servePickerOutsideHandler = null;

function closeServeTagPicker({ restoreFocus = true } = {}) {
    const picker = document.getElementById('serve-tag-picker');
    if (servePickerOutsideHandler) {
        document.removeEventListener('click', servePickerOutsideHandler);
        servePickerOutsideHandler = null;
    }
    if (!picker) return;
    picker.remove();
    if (restoreFocus) addServeBtn?.focus();
}

async function openServeViewForTag(tagID) {
    if (typeof switchView === 'function') switchView('serve');
    const tags = await window.go.main.App.GetTags();
    const tag = tags.find(t => t.id === tagID);
    if (!tag) return;
    await addConfiguredServeEntry(tagID, tag.name);

    // Best-effort highlight — if rows expose a per-tag attribute, scroll and flash.
    const row = serveList.querySelector(`:scope > li[data-tag-id="${tagID}"]`);
    if (row) {
        row.scrollIntoView({ behavior: 'smooth', block: 'center' });
        row.classList.add('ring-2', 'ring-stone-500');
        setTimeout(() => row.classList.remove('ring-2', 'ring-stone-500'), 1500);
    }
}
window.openServeViewForTag = openServeViewForTag;

// --- Start Serving a Tag ---
async function startServingTag(tagID, bindAll = false) {
    try {
        const apiAccess = apiAccessPreferences.get(tagID) || 'none';
        // The port it had last time first, so a bookmarked or shared URL keeps
        // working; a random one only if that port is taken now.
        const lastPort = Number(configuredEntries.get(tagID)?.last_port) || 0;
        let moved = false;
        if (lastPort > 0) {
            try {
                await window.go.main.ServeService.StartServing(tagID, lastPort, bindAll, apiAccess);
            } catch (error) {
                if (!/unavailable/i.test(errText(error))) throw error;
                moved = true;
            }
        }
        if (lastPort === 0 || moved) {
            const port = await window.go.main.ServeService.GetRandomPort();
            await window.go.main.ServeService.StartServing(tagID, port, bindAll, apiAccess);
        }
        showToast(moved ? `Tag server started. Port ${lastPort} is in use, so its address changed.` : 'Tag server started');
        await loadServeStatus({ force: true });
        updateServeIndicator();
    } catch (error) {
        console.error('Failed to start serving:', error);
        showToast('Failed to start server: ' + errText(error), 'error');
    }
}

// --- Stop Serving a Tag ---
async function stopServingTag(tagID) {
    try {
        await window.go.main.ServeService.StopServing(tagID);
        showToast('Tag server stopped');
        await loadServeStatus({ force: true });
        updateServeIndicator();
    } catch (error) {
        console.error('Failed to stop serving:', error);
        showToast('Failed to stop server: ' + errText(error), 'error');
    }
}

// --- Logo Click (back to clips) ---
const logoBtn = document.getElementById('logo-btn');
if (logoBtn) {
    logoBtn.addEventListener('click', () => switchView('clips'));
}

// --- Serve Indicator ---
const serveIndicator = document.getElementById('serve-indicator');
const drawerActivityIndicator = document.getElementById('drawer-activity-indicator');

function updateServeIndicator() {
    window.go.main.ServeService.GetServeStatus().then(statuses => {
        const hasRunning = statuses && statuses.length > 0;
        if (serveIndicator) {
            serveIndicator.classList.toggle('hidden', !hasRunning);
        }
        updateDrawerActivityIndicator();
    }).catch(() => {});
}

function updateDrawerActivityIndicator() {
    // Show hamburger dot if watch OR serve is active
    const watchActive = watchIndicator && !watchIndicator.classList.contains('hidden');
    const serveActive = serveIndicator && !serveIndicator.classList.contains('hidden');
    // Amber when a watch folder should be watched but is not, so the problem
    // shows without opening the drawer (the Watch tab dot is inside it).
    const watchUnavailable = watchActive && watchIndicator.dataset.state === 'unavailable';
    if (drawerActivityIndicator) {
        drawerActivityIndicator.classList.toggle('hidden', !watchActive && !serveActive);
        drawerActivityIndicator.classList.toggle('bg-amber-500', watchUnavailable);
        drawerActivityIndicator.classList.toggle('bg-emerald-500', !watchUnavailable);
        drawerActivityIndicator.dataset.state = watchUnavailable ? 'unavailable' : 'active';
    }
}

// --- Event Handlers ---
serveBackBtn.addEventListener('click', () => switchView('clips'));

addServeBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    showServeTagPicker();
});

// Serve list event delegation
serveList.addEventListener('click', async (e) => {
    // Open the served address in the browser. Desktop only: in the web UI
    // the address names the server's own loopback, not the viewer's.
    const openBtn = e.target.closest('.serve-url-open');
    if (openBtn) {
        const url = openBtn.dataset.url;
        if (/^https?:\/\//.test(url) && window.runtime?.BrowserOpenURL) window.runtime.BrowserOpenURL(url);
        return;
    }

    // Copy URL to clipboard
    const copyBtn = e.target.closest('.serve-url-copy');
    if (copyBtn) {
        const url = copyBtn.dataset.url;
        if (await copyTextQuietly(url)) showToast('URL copied');
        else showToast('Could not copy the URL.', 'error');
        return;
    }

    // Toggle bind mode (local/network) — only when stopped
    const bindBtn = e.target.closest('.serve-bind-toggle');
    if (bindBtn && !bindBtn.disabled) {
        const tagID = parseInt(bindBtn.dataset.tagId, 10);
        const currentBind = bindBtn.dataset.bindAll === 'true';
        // Store preference per tag for next start
        bindPreferences.set(tagID, !currentBind);
        persistServeEntries();
        bindBtn.dataset.bindAll = String(!currentBind);
        bindBtn.textContent = !currentBind ? 'Network' : 'Local';
        bindBtn.title = !currentBind ? 'Accessible from network (0.0.0.0)' : 'Local only (127.0.0.1)';
        if (!currentBind) {
            bindBtn.classList.add('bg-stone-100', 'border-stone-300', 'text-stone-600');
            bindBtn.classList.remove('text-stone-400');
        } else {
            bindBtn.classList.remove('bg-stone-100', 'border-stone-300', 'text-stone-600');
            bindBtn.classList.add('text-stone-400');
        }
        // Also update the start button's data
        const startBtn = bindBtn.parentElement.querySelector('.serve-toggle-btn');
        if (startBtn) startBtn.dataset.bindAll = String(!currentBind);
        return;
    }

    // Remove stopped entry
    const removeBtn = e.target.closest('.serve-remove-btn');
    if (removeBtn) {
        const tagID = parseInt(removeBtn.dataset.tagId, 10);
        const hadFocus = document.activeElement === removeBtn;
        configuredEntries.delete(tagID);
        bindPreferences.delete(tagID);
        apiAccessPreferences.delete(tagID);
        await loadServeStatus({ force: true });
        // The row is gone; keep keyboard users in the view.
        if (hadFocus) addServeBtn?.focus();
        return;
    }

    // Toggle start/stop
    const toggleBtn = e.target.closest('.serve-toggle-btn');
    if (toggleBtn) {
        const tagID = parseInt(toggleBtn.dataset.tagId, 10);
        // A double click must not start (or stop) the same server twice.
        if (serveToggleInFlight.has(tagID)) return;
        const running = toggleBtn.dataset.running === 'true';
        const bindAll = toggleBtn.dataset.bindAll === 'true';
        const hadFocus = document.activeElement === toggleBtn;
        serveToggleInFlight.add(tagID);
        toggleBtn.disabled = true;
        toggleBtn.textContent = running ? 'Stopping…' : 'Starting…';
        try {
            if (running) {
                await stopServingTag(tagID);
            } else {
                await startServingTag(tagID, bindAll);
            }
        } finally {
            serveToggleInFlight.delete(tagID);
            await loadServeStatus({ force: true });
            // The forced render replaced the button (and disabling it had
            // already dropped focus to <body>): focus its replacement.
            const active = document.activeElement;
            if (hadFocus && (!active || active === document.body || !active.isConnected)) {
                serveList.querySelector(`.serve-toggle-btn[data-tag-id="${tagID}"]`)?.focus();
            }
        }
    }
});

// API access dropdown change handler
serveList.addEventListener('change', (e) => {
    const select = e.target.closest('.serve-api-access');
    if (select) {
        const tagID = parseInt(select.dataset.tagId, 10);
        apiAccessPreferences.set(tagID, select.value);
        persistServeEntries();
    }
});

// Expose for testing
if (window.__testHelpers) {
    window.__testHelpers.switchView = (view) => switchView(view);
    window.__testHelpers.getCurrentView = () => currentView;
    // Forget the list in memory and read the stored one again on next use.
    window.__testHelpers.resetServeEntries = () => {
        serveEntriesRestoring = null;
        configuredEntries.clear();
        bindPreferences.clear();
        apiAccessPreferences.clear();
        serveEntriesRestored = false;
        lastSavedServeEntries = null;
        lastServeRenderKey = null;
    };
    window.__testHelpers.hasConfiguredServeEntry = (tagName) => {
        for (const entry of configuredEntries.values()) {
            if (entry.tag_name === tagName) return true;
        }
        return false;
    };
}
