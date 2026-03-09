// --- View Switching ---
// Shared view state: 'clips', 'watch', 'serve'
let currentView = 'clips';

// --- Serve View State ---
let isViewingServe = false;
let servePollingInterval = null;
const bindPreferences = new Map(); // tagID -> bindAll preference
// Configured entries: tags the user has added to the serve list (persists when stopped)
// Each entry: { tag_id, tag_name, bind_all }
const configuredEntries = new Map();

// --- Serve Elements ---
const serveView = document.getElementById('serve-view');
const serveList = document.getElementById('serve-list');
const serveBackBtn = document.getElementById('serve-back-btn');
const addServeBtn = document.getElementById('add-serve-btn');
const addServeZone = document.getElementById('add-serve-zone');

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

    // Stop serve polling when leaving serve view
    if (prev === 'serve' && view !== 'serve') {
        stopServePolling();
    }

    // Update legacy flags for backward compatibility
    isViewingWatch = (view === 'watch');
    isViewingServe = (view === 'serve');

    // Show the selected view and trigger its load
    switch (view) {
        case 'clips':
            gallery.parentElement.classList.remove('hidden');
            imageCache.clear();
            loadClips();
            break;
        case 'watch':
            watchView.classList.remove('hidden');
            loadWatchFolders();
            break;
        case 'serve':
            serveView.classList.remove('hidden');
            loadServeStatus();
            startServePolling();
            break;
    }
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
        ? `<button class="serve-url-copy text-[11px] text-stone-500 hover:text-stone-700 underline underline-offset-2 decoration-stone-300 truncate max-w-[200px] transition-colors" data-url="${escapeHTML(info.url)}" title="Click to copy URL">${escapeHTML(info.url)}</button>`
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
            <button class="serve-toggle-btn text-[11px] font-medium py-1.5 px-3 rounded-md transition-colors ${toggleClass}"
                    data-tag-id="${info.tag_id}" data-running="${info.running}" data-bind-all="${info.bind_all}">
                ${toggleLabel}
            </button>
            ${!info.running ? `<button class="serve-remove-btn p-1 text-stone-400 hover:text-red-500 transition-colors rounded hover:bg-red-50" data-tag-id="${info.tag_id}" title="Remove from list">
                <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M6 18L18 6M6 6l12 12"/></svg>
            </button>` : ''}
        </div>
    `;

    return li;
}

// --- Load Serve Status ---
async function loadServeStatus() {
    try {
        const statuses = await window.go.main.ServeService.GetServeStatus();
        const runningMap = new Map();
        for (const s of (statuses || [])) {
            runningMap.set(s.tag_id, s);
            // Ensure running servers are in configured entries
            if (!configuredEntries.has(s.tag_id)) {
                configuredEntries.set(s.tag_id, { tag_id: s.tag_id, tag_name: s.tag_name, bind_all: s.bind_all });
            }
        }

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

// --- Tag Picker ---
async function showServeTagPicker() {
    // Remove existing picker if open
    const existing = document.getElementById('serve-tag-picker');
    if (existing) {
        existing.remove();
        return;
    }

    try {
        const tags = await window.go.main.App.GetTags();
        const availableTags = tags.filter(t => !configuredEntries.has(t.id));

        if (availableTags.length === 0) {
            showToast('No available tags to serve');
            return;
        }

        // Create dropdown
        const picker = document.createElement('div');
        picker.id = 'serve-tag-picker';
        picker.className = 'absolute left-1/2 -translate-x-1/2 mt-2 w-56 bg-white rounded-lg shadow-xl border border-stone-200 z-50';
        picker.innerHTML = `
            <div class="p-2 border-b border-stone-100">
                <span class="text-[10px] font-semibold text-stone-400 uppercase tracking-wide">Pick a tag</span>
            </div>
            <div class="max-h-48 overflow-y-auto py-1">
                ${availableTags.map(tag => `
                    <button class="serve-tag-option w-full text-left px-3 py-2 text-xs text-stone-600 hover:bg-stone-100 transition-colors flex items-center gap-2"
                            data-tag-id="${tag.id}">
                        <span class="w-2.5 h-2.5 rounded-full flex-shrink-0" style="background-color: ${tag.color}"></span>
                        ${escapeHTML(tag.name)}
                    </button>
                `).join('')}
            </div>
        `;

        // Position under add-serve-zone
        addServeZone.style.position = 'relative';
        addServeZone.appendChild(picker);

        // Tag selection handler — add to list (stopped), user clicks Start
        picker.addEventListener('click', async (e) => {
            const option = e.target.closest('.serve-tag-option');
            if (option) {
                const tagID = parseInt(option.dataset.tagId, 10);
                const tagName = option.textContent.trim();
                picker.remove();
                configuredEntries.set(tagID, { tag_id: tagID, tag_name: tagName, bind_all: false });
                await loadServeStatus();
            }
        });

        // Close on outside click
        const closeOnOutside = (e) => {
            if (!picker.contains(e.target) && e.target !== addServeBtn) {
                picker.remove();
                document.removeEventListener('click', closeOnOutside);
            }
        };
        // Delay to avoid immediate close from the current click
        setTimeout(() => document.addEventListener('click', closeOnOutside), 0);

    } catch (error) {
        console.error('Failed to load tags for serve picker:', error);
        showToast('Failed to load tags');
    }
}

// --- Start Serving a Tag ---
async function startServingTag(tagID, bindAll = false) {
    try {
        const port = await window.go.main.ServeService.GetRandomPort();
        await window.go.main.ServeService.StartServing(tagID, port, bindAll, 'none');
        showToast('Tag server started');
        await loadServeStatus();
        updateServeIndicator();
    } catch (error) {
        console.error('Failed to start serving:', error);
        showToast('Failed to start server: ' + error.message);
    }
}

// --- Stop Serving a Tag ---
async function stopServingTag(tagID) {
    try {
        await window.go.main.ServeService.StopServing(tagID);
        showToast('Tag server stopped');
        await loadServeStatus();
        updateServeIndicator();
    } catch (error) {
        console.error('Failed to stop serving:', error);
        showToast('Failed to stop server: ' + error.message);
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
    if (drawerActivityIndicator) {
        drawerActivityIndicator.classList.toggle('hidden', !watchActive && !serveActive);
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
    // Copy URL to clipboard
    const copyBtn = e.target.closest('.serve-url-copy');
    if (copyBtn) {
        const url = copyBtn.dataset.url;
        try {
            await navigator.clipboard.writeText(url);
            showToast('URL copied');
        } catch (err) {
            console.error('Failed to copy URL:', err);
        }
        return;
    }

    // Toggle bind mode (local/network) — only when stopped
    const bindBtn = e.target.closest('.serve-bind-toggle');
    if (bindBtn && !bindBtn.disabled) {
        const tagID = parseInt(bindBtn.dataset.tagId, 10);
        const currentBind = bindBtn.dataset.bindAll === 'true';
        // Store preference per tag for next start
        bindPreferences.set(tagID, !currentBind);
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
        configuredEntries.delete(tagID);
        bindPreferences.delete(tagID);
        await loadServeStatus();
        return;
    }

    // Toggle start/stop
    const toggleBtn = e.target.closest('.serve-toggle-btn');
    if (toggleBtn) {
        const tagID = parseInt(toggleBtn.dataset.tagId, 10);
        const running = toggleBtn.dataset.running === 'true';
        const bindAll = toggleBtn.dataset.bindAll === 'true';
        if (running) {
            await stopServingTag(tagID);
        } else {
            await startServingTag(tagID, bindAll);
        }
    }
});

// Expose for testing
if (window.__testHelpers) {
    window.__testHelpers.switchView = (view) => switchView(view);
    window.__testHelpers.getCurrentView = () => currentView;
}
