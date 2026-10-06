// Full-size images for the lightbox and comparison view, as data URLs
// (desktop) or same-origin URLs (server mode). Cards never use it: they load
// URL-served thumbnails the browser caches itself. Bounded by bytes and evicted
// least recently used first, so viewing a large library in the lightbox cannot
// keep every image resident for the rest of the session. Videos must never
// enter this cache: they use range-capable HTTP URLs so the browser can fetch
// only the bytes needed for a thumbnail or playback.
const IMAGE_CACHE_MAX_BYTES = 150 * 1024 * 1024;
const imageCache = new Map(); // id -> { url, bytes }, oldest use first
let imageCacheBytes = 0;

function imageCacheGet(id) {
    const entry = imageCache.get(id);
    if (!entry) return undefined;
    imageCache.delete(id);
    imageCache.set(id, entry);
    return entry.url;
}

function imageCacheDelete(id) {
    const entry = imageCache.get(id);
    if (!entry) return;
    imageCache.delete(id);
    imageCacheBytes -= entry.bytes;
}

function imageCacheSet(id, url) {
    imageCacheDelete(id);
    // A JS string of a data URL costs about one byte per character.
    const bytes = url.length;
    if (bytes > IMAGE_CACHE_MAX_BYTES) return;
    imageCache.set(id, { url, bytes });
    imageCacheBytes += bytes;
    for (const [oldId, entry] of imageCache) {
        if (imageCacheBytes <= IMAGE_CACHE_MAX_BYTES) break;
        imageCache.delete(oldId);
        imageCacheBytes -= entry.bytes;
    }
}
window.__imageCacheStats = () => ({ entries: imageCache.size, bytes: imageCacheBytes, maxBytes: IMAGE_CACHE_MAX_BYTES });
// Longest edge of a captured video-card thumbnail, in CSS pixels.
const THUMBNAIL_MAX_EDGE = 640;
// Furthest into a clip the thumbnail sampler will seek.
const THUMBNAIL_SEEK_MAX_SECONDS = 2;
const videoMediaURLCache = new Map();
const pendingVideoMediaURLs = new Map();
const renderedClipsById = new Map();

// Bumped per clip whenever its bytes change, so a fetch that started before
// the change cannot repopulate a cache entry with the previous revision.
const mediaRevisions = new Map();

// Captured video-card frames, so a card rebuilt for a clip whose bytes have
// not changed (another view, a folder visited again) shows the frame at once
// instead of leasing a temp file, range-fetching, seeking and re-encoding.
// Keyed by clip id; each entry remembers the revision it was captured from and
// is used only while that revision is current. The frame is an object URL
// (canvas.toBlob), revoked when the entry is evicted or invalidated. It is
// only ever shown through img.video-thumb, never video.poster (WebKit paints a
// source-less video black and ignores its poster).
const VIDEO_FRAME_CACHE_MAX = 300;
const videoFrameCache = new Map(); // id -> { revision, url }, oldest use first

function videoFrameRevision(clip) {
    const id = Number(clip.id);
    return `${clip.content_hash || ''}:${mediaRevisions.get(id) || 0}`;
}

function videoFrameCacheGet(clip) {
    const id = Number(clip.id);
    const entry = videoFrameCache.get(id);
    if (!entry) return null;
    if (entry.revision !== videoFrameRevision(clip)) {
        videoFrameCacheDelete(id);
        return null;
    }
    videoFrameCache.delete(id);
    videoFrameCache.set(id, entry);
    return entry.url;
}

function videoFrameCacheDelete(id) {
    const entry = videoFrameCache.get(id);
    if (!entry) return;
    videoFrameCache.delete(id);
    if (entry.url.startsWith('blob:')) URL.revokeObjectURL(entry.url);
}

function videoFrameCacheSet(id, revision, url) {
    videoFrameCacheDelete(id);
    videoFrameCache.set(id, { revision, url });
    while (videoFrameCache.size > VIDEO_FRAME_CACHE_MAX) {
        videoFrameCacheDelete(videoFrameCache.keys().next().value);
    }
}
window.__videoFrameCacheStats = () => ({ entries: videoFrameCache.size, max: VIDEO_FRAME_CACHE_MAX });

function clearMediaCaches() {
    imageCache.clear();
    imageCacheBytes = 0;
    videoMediaURLCache.clear();
    pendingVideoMediaURLs.clear();
    // Captured frames stay: each is checked against its clip's revision.
}

// Forget everything cached for one clip after its bytes were replaced in place
// (editor Save, upload-conflict Overwrite). The caches are keyed by clip id, so
// without this the card, lightbox and comparison keep showing the old image,
// and an overwritten video keeps pointing at a leased file the backend dropped.
function invalidateClipMedia(clipId) {
    const id = Number(clipId);
    imageCacheDelete(id);
    videoMediaURLCache.delete(id);
    pendingVideoMediaURLs.delete(id);
    videoFrameCacheDelete(id);
    mediaRevisions.set(id, (mediaRevisions.get(id) || 0) + 1);
}
window.invalidateClipMedia = invalidateClipMedia;

function rememberRenderedClip(clip) {
    renderedClipsById.set(Number(clip.id), clip);
}

function clearRenderedClips() {
    renderedClipsById.clear();
    resetCardMediaScheduler();
}

// Card media work that costs a bridge call or a decoder — video frame
// capture, and the full-image fallback when a thumbnail fails — starts only
// when the card comes within about a screen of the viewport, and at most
// CARD_MEDIA_CONCURRENCY at a time, so the first visible row is not queued
// behind fifty offscreen cards. Image thumbnails need none of this: they are
// plain URLs on <img loading="lazy">, which the browser already schedules.
const CARD_MEDIA_CONCURRENCY = 4;
const cardMediaQueue = [];
const cardMediaTasks = new WeakMap();
let cardMediaActive = 0;
let cardMediaObserver = null;

function scheduleCardMedia(card, task) {
    if (typeof IntersectionObserver !== 'function') {
        enqueueCardMedia(card, task);
        return;
    }
    if (!cardMediaObserver) {
        cardMediaObserver = new IntersectionObserver((entries) => {
            for (const entry of entries) {
                if (!entry.isIntersecting) continue;
                cardMediaObserver.unobserve(entry.target);
                const pending = cardMediaTasks.get(entry.target);
                cardMediaTasks.delete(entry.target);
                if (pending) enqueueCardMedia(entry.target, pending);
            }
        }, { rootMargin: '100% 0px' });
    }
    cardMediaTasks.set(card, task);
    cardMediaObserver.observe(card);
}

function enqueueCardMedia(card, task) {
    cardMediaQueue.push({ card, task });
    pumpCardMedia();
}

function pumpCardMedia() {
    while (cardMediaActive < CARD_MEDIA_CONCURRENCY && cardMediaQueue.length > 0) {
        const { card, task } = cardMediaQueue.shift();
        // The gallery may have been rebuilt since this was queued; loading a
        // detached card opens a fetch and a decoder nothing will release.
        if (!card.isConnected) continue;
        cardMediaActive++;
        Promise.resolve()
            .then(task)
            .catch(error => console.error('Card media load failed:', error))
            .finally(() => {
                cardMediaActive--;
                pumpCardMedia();
            });
    }
}

// A gallery rebuild drops the previous render's cards: stop watching them
// and forget their queued work (tasks already running finish on their own).
function resetCardMediaScheduler() {
    cardMediaQueue.length = 0;
    if (cardMediaObserver) {
        cardMediaObserver.disconnect();
        cardMediaObserver = null;
    }
}
window.__cardMediaStats = () => ({ active: cardMediaActive, queued: cardMediaQueue.length });

// Desktop thumbnails are served by the app's asset handler under a per-run key
// (App.ThumbnailURLBase); server mode has an authenticated REST route. Either
// way the URL names the content hash, so an edited clip gets a new URL.
let thumbnailBasePromise = null;

function getThumbnailBase() {
    if (!thumbnailBasePromise) {
        const fn = window.go?.main?.App?.ThumbnailURLBase;
        thumbnailBasePromise = typeof fn === 'function'
            ? Promise.resolve().then(() => fn()).catch(() => null)
            : Promise.resolve(null);
        // A failure is not remembered: the next render may try again.
        thumbnailBasePromise.then(base => { if (!base) thumbnailBasePromise = null; });
    }
    return thumbnailBasePromise;
}

async function getThumbnailUrl(clip) {
    const id = Number(clip.id);
    const hash = typeof clip.content_hash === 'string' && /^[0-9a-f]{64}$/.test(clip.content_hash)
        ? clip.content_hash : '';
    const revision = mediaRevisions.get(id) || 0;
    if (window.mahpastesMode === 'server') {
        const params = new URLSearchParams();
        if (hash) params.set('h', hash);
        if (revision) params.set('rev', String(revision));
        const query = params.toString();
        return `/api/v1/clips/${id}/thumb${query ? `?${query}` : ''}`;
    }
    const base = await getThumbnailBase();
    if (!base) return null;
    // The revision keeps a card rendered from a listing older than an edit
    // from reusing the cached response for its stale hash.
    return `${base}${id}${hash ? `/${hash}` : ''}${revision ? `?rev=${revision}` : ''}`;
}

function getVisibleMediaClips() {
    return Array.from(gallery.querySelectorAll(':scope > li[data-id]'))
        .filter(card => card.style.display !== 'none' && card.getClientRects().length > 0)
        .map(card => renderedClipsById.get(Number(card.dataset.id)))
        .filter(clip => clip && (
            clip.content_type.startsWith('image/') || clip.content_type.startsWith('video/')
        ));
}

// Track last checked checkbox for shift-click range selection
let lastCheckedCheckbox = null;

// Plugin UI actions cache
let pluginUIActions = null;
let pluginUIActionsLoadGeneration = 0;
let dragPrepBusyCount = 0;

// Roving tabindex for bulk toolbar
let bulkToolbarRover = null;

// A cold startup may finish loading the frontend before all plugins have been
// initialized. Refresh from the definitive backend readiness signal instead
// of relying only on a fixed retry window. If readiness arrives before the DOM
// is complete, the normal window-load fetch will pick up the full action set.
if (window.runtime && window.runtime.EventsOn) {
    window.runtime.EventsOn('plugin:ready', () => {
        if (document.readyState === 'complete') {
            loadPluginUIActions();
        }
    });
}

function beginGlobalDragPrepareCursor() {
    dragPrepBusyCount += 1;
    document.body.classList.add('drag-preparing');
}

function endGlobalDragPrepareCursor() {
    dragPrepBusyCount = Math.max(0, dragPrepBusyCount - 1);
    if (dragPrepBusyCount === 0) {
        document.body.classList.remove('drag-preparing');
    }
}

// Load plugin UI actions from backend
async function loadPluginUIActions({ retry = true } = {}) {
    const generation = ++pluginUIActionsLoadGeneration;
    // The backoff covers a cold start where the frontend fetches before the
    // plugin manager has registered its actions. Callers that know an empty
    // action set is the real answer (e.g. just after removing every plugin)
    // pass retry:false, since retrying can only re-confirm the empty result.
    const retryDelays = retry ? [0, 100, 250, 500, 1000] : [0];

    for (let attempt = 0; attempt < retryDelays.length; attempt++) {
        if (retryDelays[attempt] > 0) {
            await new Promise(resolve => setTimeout(resolve, retryDelays[attempt]));
        }
        // A plugin install/enable may trigger a newer load while this retry
        // loop is waiting. Never let the older response overwrite it.
        if (generation !== pluginUIActionsLoadGeneration) return pluginUIActions;

        try {
            const actions = await window.go.main.PluginService.GetPluginUIActions();
            pluginUIActions = actions || {};
            // The backend reports whether plugin loading has finished, so an
            // empty action set is no longer ambiguous. Previously this waited
            // for a non-empty result, which meant a user with no plugins
            // installed paid the whole backoff on every load.
            if (pluginUIActions.ready || attempt === retryDelays.length - 1) break;
        } catch (error) {
            if (attempt === retryDelays.length - 1) {
                console.error('Failed to load plugin UI actions:', error);
                pluginUIActions = {};
            }
        }
    }

    pluginUIActions.card_actions ||= [];
    pluginUIActions.lightbox_buttons ||= [];
    pluginUIActions.bulk_actions ||= [];
    pluginUIActions.global_actions ||= [];
    renderDrawerPluginActions();
    if (typeof updateBulkToolbar === 'function') updateBulkToolbar();
    return pluginUIActions;
}

function getApplicableBulkPluginActions() {
    const selectedClips = Array.from(selectedIds).map(id => {
        const card = gallery.querySelector(`li[data-id="${id}"]`);
        return card ? {
            content_type: card.dataset.type || '',
            size: Number(card.dataset.size) || 0,
        } : null;
    }).filter(Boolean);

    return (pluginUIActions?.bulk_actions || []).filter(action =>
        selectedClips.length > 0
        && selectedClips.length === selectedIds.size
        && selectedClips.every(clip => shouldShowPluginAction(action, clip))
    );
}

function canCompareBulkSelection() {
    if (selectedIds.size !== 2) return false;
    return Array.from(selectedIds).every(id => {
        const card = gallery.querySelector(`li[data-id="${id}"]`);
        return card && card.dataset.type.startsWith('image/');
    });
}

function updateBulkMoreButton() {
    const button = document.getElementById('bulk-more-btn');
    if (!button) return;
    if (!button.dataset.menuInitialized) {
        button.addEventListener('click', () => openBulkMoreMenu(button));
        button.dataset.menuInitialized = 'true';
    }
    const hasActions = canCompareBulkSelection() || getApplicableBulkPluginActions().length > 0;
    button.classList.toggle('hidden', !hasActions);
}

// Build menu entries for the applicable plugin bulk actions
function buildBulkPluginItems(pluginActions) {
    return pluginActions.map(action => ({
        id: 'plugin',
        label: escapeHTML(action.label),
        iconHtml: typeof getPluginIcon === 'function'
            ? (getPluginIcon(action.icon) || getPluginIcon('bolt') || '')
            : '',
        pluginId: action.plugin_id,
        actionId: action.id,
        hasOptions: action.options && action.options.length > 0,
    }));
}

// Dispatch a plugin bulk action clicked in a menu built by buildBulkPluginItems
function runBulkPluginAction(pluginActions, item) {
    const pluginId = Number(item.dataset.pluginId);
    const actionId = item.dataset.actionId;
    const action = pluginActions.find(candidate =>
        candidate.plugin_id === pluginId && candidate.id === actionId
    );
    if (!action) return;

    const clipIds = Array.from(selectedIds);
    if (action.options && action.options.length > 0) {
        openPluginOptionsDialog(action, clipIds);
    } else {
        executePluginAction(action.plugin_id, action.id, clipIds, {}, action.async);
    }
}

function openBulkMoreMenu(anchor) {
    const items = [];
    const canCompare = canCompareBulkSelection();
    const pluginActions = getApplicableBulkPluginActions();

    if (canCompare) {
        items.push({
            id: 'compare',
            label: 'Compare',
            iconHtml: getMenuIcon('compare'),
        });
    }
    if (canCompare && pluginActions.length > 0) items.push({ type: 'divider' });

    items.push(...buildBulkPluginItems(pluginActions));

    if (items.length === 0) return;
    ContextMenu.open(items, null, anchor, (menuAction, _id, item) => {
        if (menuAction === 'compare') {
            openComparisonModal();
            return;
        }
        if (menuAction !== 'plugin') return;
        runBulkPluginAction(pluginActions, item);
    });
}

// --- Multi-select context menu ---
// Right-clicking a selected card while several clips are selected opens the
// bulk actions as a context menu instead of the single-clip one.

function anySelectedClipExpiring() {
    return Array.from(selectedIds).some(id => {
        const card = gallery.querySelector(`li[data-id="${id}"]`);
        return !!(card && card.dataset.expiresAt);
    });
}

function buildBulkMenuItemList(pluginActions) {
    const isServerMode = window.mahpastesMode === 'server';
    const count = selectedIds.size;
    const items = [];

    items.push({ id: 'bulk-tag', label: `Tag ${count} clips`, iconHtml: getMenuIcon('tags'), tooltip: 'Add a tag to selected clips' });
    items.push({ id: 'bulk-expiry', label: 'Set Expiration', iconHtml: getMenuIcon('set-expiration'), tooltip: 'Set expiration on selected clips' });
    if (anySelectedClipExpiring()) {
        items.push({ id: 'bulk-cancel-expiry', label: 'Clear Expiration', iconHtml: getMenuIcon('cancel-expiration'), tooltip: 'Remove expiration from selected clips' });
    }

    items.push({ type: 'divider' });

    // Copying files onto the host clipboard is desktop-only, same as the
    // toolbar's .desktop-only Copy button.
    if (!isServerMode) {
        items.push({ id: 'bulk-copy', label: `Copy ${count} files`, iconHtml: getMenuIcon('copy-file'), tooltip: 'Copy selected files to clipboard for pasting' });
    }
    items.push({ id: 'bulk-download', label: `Download ${count} clips`, iconHtml: getMenuIcon('save'), tooltip: 'Save the selected clips as a ZIP file' });
    if (canCompareBulkSelection()) {
        items.push({ id: 'bulk-compare', label: 'Compare', iconHtml: getMenuIcon('compare'), tooltip: 'Compare the two selected images' });
    }
    items.push({
        id: 'bulk-archive',
        label: isViewingArchive ? `Restore ${count} clips` : `Archive ${count} clips`,
        iconHtml: getMenuIcon(isViewingArchive ? 'restore' : 'archive'),
        tooltip: isViewingArchive ? 'Move selected clips back from archive' : 'Archive selected clips',
    });
    items.push({ id: 'bulk-delete', label: `Delete ${count} clips`, iconHtml: getMenuIcon('delete'), danger: true, tooltip: 'Permanently delete selected -- cannot be undone' });

    if (pluginActions.length > 0) {
        items.push({ type: 'divider' });
        items.push({
            type: 'submenu',
            label: 'Plugins',
            iconHtml: getMenuIcon('plugins'),
            submenuId: 'bulk-plugins',
            children: buildBulkPluginItems(pluginActions),
        });
    }

    items.push({ type: 'divider' });
    items.push({ id: 'bulk-deselect', label: 'Deselect All', iconHtml: getMenuIcon('deselect'), tooltip: 'Clear the selection' });

    return items;
}

// `anchor` positions the menu (pointer rect or element); `popoverAnchor` must be
// a real element because the tag/expiration popovers measure it.
function openBulkContextMenu(anchor, popoverAnchor) {
    const pluginActions = getApplicableBulkPluginActions();
    const items = buildBulkMenuItemList(pluginActions);

    const menu = ContextMenu.open(items, null, anchor, (action, _id, item) => {
        if (action === 'plugin') {
            runBulkPluginAction(pluginActions, item);
            return;
        }
        handleBulkMenuAction(action, popoverAnchor);
    }, { ariaLabel: 'Bulk actions for selected clips' });

    // Tag the menu so bulk-specific selectors can distinguish it from the
    // single-clip and folder menus.
    if (menu) menu.setAttribute('data-source', 'bulk');
    return menu;
}

function handleBulkMenuAction(action, popoverAnchor) {
    switch (action) {
        case 'bulk-tag':
            openBulkTagPopover(popoverAnchor);
            break;
        case 'bulk-expiry':
            openExpirationPopover(null, popoverAnchor, true);
            break;
        case 'bulk-cancel-expiry':
            bulkCancelExpiry();
            break;
        case 'bulk-copy':
            bulkCopyFiles();
            break;
        case 'bulk-download':
            bulkDownload();
            break;
        case 'bulk-compare':
            openComparisonModal();
            break;
        case 'bulk-archive':
            bulkArchive();
            break;
        case 'bulk-delete':
            bulkDelete();
            break;
        case 'bulk-deselect':
            cancelSelection();
            break;
    }
}

// Render global plugin actions in the hamburger menu drawer
function renderDrawerPluginActions() {
    const container = document.getElementById('drawer-plugin-actions');
    if (!container) return;

    // Remove old action buttons (keep the divider which is the first child)
    while (container.children.length > 1) {
        container.removeChild(container.lastChild);
    }

    const actions = pluginUIActions?.global_actions || [];
    if (actions.length === 0) {
        container.classList.add('hidden');
        return;
    }

    container.classList.remove('hidden');

    actions.forEach(action => {
        const btn = document.createElement('button');
        btn.className = 'border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-500 text-xs font-medium py-2.5 px-3 rounded-md transition-colors flex items-center w-full';
        btn.dataset.globalAction = 'true';
        btn.dataset.pluginId = action.plugin_id;
        btn.dataset.actionId = action.id;
        btn.dataset.hasOptions = action.options && action.options.length > 0 ? 'true' : 'false';
        btn.dataset.isAsync = action.async ? 'true' : 'false';

        let iconHtml = '';
        if (typeof getPluginIcon === 'function') {
            iconHtml = getPluginIcon(action.icon) || getPluginIcon('bolt') || '';
        }
        // Wrap icon in a span with same styling as drawer menu icons
        if (iconHtml) {
            iconHtml = iconHtml.replace('class="w-4 h-4"', 'class="w-4 h-4 mr-2 opacity-60"');
        }

        btn.setAttribute('aria-label', action.label);
        btn.innerHTML = `${iconHtml}<span>${escapeHTML(action.label)}</span>`;
        container.appendChild(btn);
    });
}

// Get icon SVG for built-in menu actions
function getMenuIcon(name) {
    const icons = {
        'copy-path': '<path stroke-linecap="round" stroke-linejoin="round" d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2m0 0h2a2 2 0 012 2v3m2 4H10m0 0l3-3m-3 3l3 3"/>',
        'copy-public-link': '<path stroke-linecap="round" stroke-linejoin="round" d="M13.19 8.688a4.5 4.5 0 011.242 7.244l-4.5 4.5a4.5 4.5 0 01-6.364-6.364l1.757-1.757m13.35-.622l1.757-1.757a4.5 4.5 0 00-6.364-6.364l-4.5 4.5a4.5 4.5 0 001.242 7.244"/>',
        'save': '<path stroke-linecap="round" stroke-linejoin="round" d="M4 16v1a2 2 0 002 2h12a2 2 0 002-2v-1m-4-4l-4 4m0 0l-4-4m4 4V4"/>',
        'edit': '<path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/>',
        'tags': '<path stroke-linecap="round" stroke-linejoin="round" d="M7 7h.01M7 3h5c.512 0 1.024.195 1.414.586l7 7a2 2 0 010 2.828l-7 7a2 2 0 01-2.828 0l-7-7A1.994 1.994 0 013 12V7a4 4 0 014-4z"/>',
        'metadata': '<path stroke-linecap="round" stroke-linejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"/>',
        'archive': '<path stroke-linecap="round" stroke-linejoin="round" d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4"/>',
        'restore': '<path stroke-linecap="round" stroke-linejoin="round" d="M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6"/>',
        'copy-file': '<path stroke-linecap="round" stroke-linejoin="round" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"/>',
        'copy-contents': '<path stroke-linecap="round" stroke-linejoin="round" d="M8 7v8a2 2 0 002 2h6M8 7V5a2 2 0 012-2h4.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V15a2 2 0 01-2 2h-2M8 7H6a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2v-2"/>',
        'delete': '<path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/>',
        'set-expiration': '<path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"/>',
        'cancel-expiration': '<path stroke-linecap="round" stroke-linejoin="round" d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z"/>',
        'merge': '<path stroke-linecap="round" stroke-linejoin="round" d="M7 16V4m0 0L3 8m4-4l4 4m6 0v12m0 0l4-4m-4 4l-4-4"/>',
        'deselect': '<path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12"/>',
        'compare': '<path stroke-linecap="round" stroke-linejoin="round" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2"/>',
        'open': '<path stroke-linecap="round" stroke-linejoin="round" d="M13.5 6H5.25A2.25 2.25 0 003 8.25v10.5A2.25 2.25 0 005.25 21h10.5A2.25 2.25 0 0018 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25"/>',
        'open-with': '<path stroke-linecap="round" stroke-linejoin="round" d="M3.75 9.776c.112-.017.227-.026.344-.026h15.812c.117 0 .232.009.344.026m-16.5 0a2.25 2.25 0 00-1.883 2.542l.857 6a2.25 2.25 0 002.227 1.932H19.05a2.25 2.25 0 002.227-1.932l.857-6a2.25 2.25 0 00-1.883-2.542m-16.5 0V6A2.25 2.25 0 016 3.75h3.879a1.5 1.5 0 011.06.44l2.122 2.12a1.5 1.5 0 001.06.44H18A2.25 2.25 0 0120.25 9v.776"/>',
        'copy': '<path stroke-linecap="round" stroke-linejoin="round" d="M15.75 17.25v3.375c0 .621-.504 1.125-1.125 1.125h-9.75a1.125 1.125 0 01-1.125-1.125V7.875c0-.621.504-1.125 1.125-1.125H6.75a9.06 9.06 0 011.5.124m7.5 10.376h3.375c.621 0 1.125-.504 1.125-1.125V11.25c0-4.46-3.243-8.161-7.5-8.876a9.06 9.06 0 00-1.5-.124H9.375c-.621 0-1.125.504-1.125 1.125v3.5m7.5 10.375H9.375a1.125 1.125 0 01-1.125-1.125v-9.25m12 6.625v-1.875a3.375 3.375 0 00-3.375-3.375h-1.5a1.125 1.125 0 01-1.125-1.125v-1.5a3.375 3.375 0 00-3.375-3.375H9.75"/>',
        'rename': '<path stroke-linecap="round" stroke-linejoin="round" d="M16.862 4.487l1.687-1.688a1.875 1.875 0 112.652 2.652L10.582 16.07a4.5 4.5 0 01-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 011.13-1.897l8.932-8.931z"/>',
        'plugins': '<path stroke-linecap="round" stroke-linejoin="round" d="M14.25 6.087c0-.355.186-.676.401-.959.221-.29.349-.634.349-1.003 0-1.036-1.007-1.875-2.25-1.875s-2.25.84-2.25 1.875c0 .369.128.713.349 1.003.215.283.401.604.401.959v0a.64.64 0 01-.657.643 48.39 48.39 0 01-4.163-.3c.186 1.613.293 3.25.315 4.907a.656.656 0 01-.658.663v0c-.355 0-.676-.186-.959-.401a1.647 1.647 0 00-1.003-.349c-1.036 0-1.875 1.007-1.875 2.25s.84 2.25 1.875 2.25c.369 0 .713-.128 1.003-.349.283-.215.604-.401.959-.401v0c.31 0 .555.26.532.57a48.039 48.039 0 01-.642 5.056c1.518.19 3.058.309 4.616.354a.64.64 0 00.657-.643v0c0-.355-.186-.676-.401-.959a1.647 1.647 0 01-.349-1.003c0-1.035 1.008-1.875 2.25-1.875 1.243 0 2.25.84 2.25 1.875 0 .369-.128.713-.349 1.003-.215.283-.401.604-.401.959v0c0 .333.277.599.61.58a48.1 48.1 0 005.427-.63 48.05 48.05 0 00.582-4.717.532.532 0 00-.533-.57v0c-.355 0-.676.186-.959.401-.29.221-.634.349-1.003.349-1.035 0-1.875-1.007-1.875-2.25s.84-2.25 1.875-2.25c.37 0 .713.128 1.003.349.283.215.604.401.959.401v0a.656.656 0 00.658-.663 48.422 48.422 0 00-.37-5.36c-1.886.342-3.81.574-5.766.689a.578.578 0 01-.61-.58v0z"/>',
    };
    const path = icons[name];
    if (!path) return '';
    return `<svg class="card-menu-icon" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24">${path}</svg>`;
}

const cardMenuTooltips = {
    'open': 'Open with your default application',
    'open-with': 'Choose an application to open this clip',
    'copy-path': 'Create a temp file and copy its path to clipboard',
    'copy-public-link': 'Create a revocable public link and copy it to clipboard',
    'copy-file': 'Place file on clipboard for pasting into other apps',
    'copy-contents': 'Copy the raw text or data to clipboard',
    'save-file': 'Save a copy to a file',
    'edit': 'Open in the built-in image editor for annotation',
    'tags': 'Add or remove tags',
    'metadata': 'View and edit file metadata',
    'rename': 'Change the filename of this clip',
    'set-expiration': 'Schedule auto-deletion after a time period',
    'cancel-expiration': 'Cancel the scheduled auto-deletion',
    'archive': 'Move to archive without deleting',
    'merge-duplicates': 'Find clips with identical content and merge them',
    'delete': 'Permanently delete -- this cannot be undone',
};

// Build the menu item list for ContextMenu.open()
function buildMenuItemList(clip) {
    const ct = clip.content_type || '';
    const items = [];
    const isServerMode = window.mahpastesMode === 'server';

    // Open actions (top of menu)
    items.push({ id: 'open', label: 'Open', iconHtml: getMenuIcon('open'), tooltip: cardMenuTooltips['open'] });
    if (!isServerMode) {
        items.push({ id: 'open-with', label: 'Open With\u2026', iconHtml: getMenuIcon('open-with'), tooltip: cardMenuTooltips['open-with'] });
    }

    items.push({ type: 'divider' });

    // Copy submenu
    const copyChildren = [
        { id: 'copy-path', label: isServerMode ? 'URL' : 'Path', iconHtml: getMenuIcon('copy-path'), tooltip: cardMenuTooltips['copy-path'] },
    ];
    // Public share links only make sense for the network-reachable headless
    // server; the desktop API binds to localhost. Offer them in server mode.
    if (isServerMode) {
        copyChildren.push({ id: 'copy-public-link', label: 'Public Link', iconHtml: getMenuIcon('copy-public-link'), tooltip: cardMenuTooltips['copy-public-link'] });
    }
    if (!isServerMode) {
        copyChildren.push({ id: 'copy-file', label: 'File', iconHtml: getMenuIcon('copy-file'), tooltip: cardMenuTooltips['copy-file'] });
    }
    // "Copy Contents" targets the host clipboard via ClipboardService, which is
    // desktop-only; in server mode it would just throw a "Failed to copy" toast.
    if (!isServerMode && (ct.startsWith('text/') || ct === 'application/json' || ct.startsWith('image/'))) {
        copyChildren.push({ id: 'copy-contents', label: 'Contents', iconHtml: getMenuIcon('copy-contents'), tooltip: cardMenuTooltips['copy-contents'] });
    }
    items.push({ type: 'submenu', label: 'Copy', iconHtml: getMenuIcon('copy'), submenuId: 'copy', children: copyChildren });

    items.push({ id: 'save-file', label: 'Save', iconHtml: getMenuIcon('save'), tooltip: cardMenuTooltips['save-file'] });

    // The combined check, not the text-only one: this item gates the editor as a
    // whole, and narrowing it would silently remove image editing from the card
    // menu.
    if (isEditableClip(clip.filename || '', ct)) {
        items.push({ id: 'edit', label: 'Edit', iconHtml: getMenuIcon('edit'), tooltip: cardMenuTooltips['edit'] });
    }

    items.push({ id: 'tags', label: 'Tags', iconHtml: getMenuIcon('tags'), tooltip: cardMenuTooltips['tags'] });
    items.push({ id: 'metadata', label: 'Metadata', iconHtml: getMenuIcon('metadata'), tooltip: cardMenuTooltips['metadata'] });
    items.push({ id: 'rename', label: 'Rename', iconHtml: getMenuIcon('rename'), tooltip: cardMenuTooltips['rename'] });

    if (clip.expires_at) {
        items.push({ id: 'cancel-expiration', label: 'Cancel Expiration', iconHtml: getMenuIcon('cancel-expiration'), tooltip: cardMenuTooltips['cancel-expiration'] });
    } else {
        items.push({ id: 'set-expiration', label: 'Set Expiration', iconHtml: getMenuIcon('set-expiration'), tooltip: cardMenuTooltips['set-expiration'] });
    }

    items.push({
        id: 'archive',
        label: isViewingArchive ? 'Restore' : 'Archive',
        iconHtml: getMenuIcon(isViewingArchive ? 'restore' : 'archive'),
        tooltip: isViewingArchive ? 'Move back from archive' : cardMenuTooltips['archive'],
    });

    if (clip.duplicate_count > 0) {
        items.push({ id: 'merge-duplicates', label: 'Merge Duplicates', iconHtml: getMenuIcon('merge'), tooltip: cardMenuTooltips['merge-duplicates'] });
    }

    items.push({ id: 'delete', label: 'Delete', iconHtml: getMenuIcon('delete'), danger: true, tooltip: cardMenuTooltips['delete'] });

    // Plugin actions submenu
    if (pluginUIActions && pluginUIActions.card_actions && pluginUIActions.card_actions.length > 0) {
        const applicableActions = pluginUIActions.card_actions.filter(action =>
            shouldShowPluginAction(action, clip)
        );
        if (applicableActions.length > 0) {
            items.push({ type: 'divider' });
            const pluginChildren = applicableActions.map(action => ({
                id: 'plugin',
                label: escapeHTML(action.label),
                pluginId: action.plugin_id,
                actionId: action.id,
                hasOptions: action.options && action.options.length > 0,
                iconHtml: typeof getPluginIcon === 'function'
                    ? (getPluginIcon(action.icon) || getPluginIcon('bolt') || '')
                    : '',
            }));
            items.push({ type: 'submenu', label: 'Plugins', iconHtml: getMenuIcon('plugins'), submenuId: 'plugins', children: pluginChildren });
        }
    }

    return items;
}

// Handle plugin card action dispatch (extracted from app.js event delegation)
function handlePluginCardAction(dataset) {
    const pluginId = Number(dataset.pluginId);
    const actionId = dataset.actionId;
    const clipId = dataset.clipId;
    const hasOptions = dataset.hasOptions === 'true';

    // Verify plugin UI actions are loaded
    if (!pluginUIActions || !pluginUIActions.card_actions) {
        console.error('Plugin UI actions not loaded');
        if (typeof showToast === 'function') {
            showToast('Plugin actions not available. Try refreshing the page.', 'error');
        }
        return;
    }

    if (hasOptions && typeof openPluginOptionsDialog === 'function') {
        const pluginAction = pluginUIActions.card_actions.find(
            a => a.plugin_id === pluginId && a.id === actionId
        );
        if (pluginAction) {
            openPluginOptionsDialog(pluginAction, [Number(clipId)]);
        } else {
            console.error('Could not find plugin action:', pluginId, actionId);
            if (typeof showToast === 'function') {
                showToast('Plugin action not found', 'error');
            }
        }
    } else if (typeof executePluginAction === 'function') {
        const pluginAction = pluginUIActions.card_actions.find(
            a => a.plugin_id === pluginId && a.id === actionId
        );
        executePluginAction(pluginId, actionId, [Number(clipId)], {}, pluginAction && pluginAction.async);
    } else {
        console.error('Plugin action handler not available');
        if (typeof showToast === 'function') {
            showToast('Plugin system not initialized', 'error');
        }
    }
}

// Render card menu dropdown via ContextMenu
function renderCardMenu(clipId, button, clip, anchor = button) {
    const items = buildMenuItemList(clip);
    return ContextMenu.open(items, clipId, anchor, (action, id, item) => {
        if (action === 'plugin') {
            handlePluginCardAction({
                pluginId: item.dataset.pluginId,
                actionId: item.dataset.actionId,
                clipId: id,
                hasOptions: item.dataset.hasOptions,
            });
        } else {
            handleCardAction(action, id, button);
        }
    });
}

// Close any open card menu
function closeCardMenu() {
    ContextMenu.close();
}

// Handle built-in card actions
async function handleCardAction(action, clipId, triggerButton) {
    closeCardMenu();
    const id = Number(clipId);

    switch (action) {
        case 'open':
            try {
                await window.go.main.App.OpenClipWithDefaultApp(id);
            } catch (err) {
                showToast('Failed to open clip.', 'error');
            }
            break;
        case 'open-with':
            try {
                const appPath = await window.go.main.App.ChooseApplication();
                if (appPath) {
                    await window.go.main.App.OpenClipWithApp(id, appPath);
                }
            } catch (err) {
                showToast('Failed to open clip.', 'error');
            }
            break;
        case 'copy-path':
            saveTempFile(id);
            break;
        case 'copy-public-link':
            createAndCopyPublicLink(id);
            break;
        case 'copy-file':
            copyFileToClipboard(id);
            break;
        case 'copy-contents':
            copyClipContents(id);
            break;
        case 'save-file':
            saveClipToFile(id);
            break;
        case 'edit':
            // An explicit Edit action always starts in Edit, including for
            // Markdown and CSV/TSV.
            openEditor(id, { initialMode: 'edit' });
            break;
        case 'tags':
            // Get the card to find a reference element for the popover
            const card = gallery.querySelector(`li[data-id="${clipId}"]`);
            if (card) {
                const tagBtn = card.querySelector('[data-action="menu"]');
                openTagPopover(id, tagBtn || triggerButton);
            }
            break;
        case 'metadata': {
            const card = gallery.querySelector(`li[data-id="${id}"]`);
            const clipData = card ? {
                filename: card.querySelector('.p-2\\.5 p')?.getAttribute('title') || '',
                content_type: card.dataset.type || '',
                size: Number(card.dataset.size) || 0,
                created_at: card.dataset.createdAt || '',
            } : null;
            openMetadataModal(id, clipData);
            break;
        }
        case 'set-expiration': {
            const card = gallery.querySelector(`li[data-id="${clipId}"]`);
            if (card) {
                const menuBtn = card.querySelector('[data-action="menu"]');
                openExpirationPopover(clipId, menuBtn || triggerButton);
            }
            break;
        }
        case 'cancel-expiration':
            cancelExpiration(id);
            break;
        case 'archive':
            toggleArchiveClip(id);
            break;
        case 'merge-duplicates':
            try {
                await window.go.main.App.MergeDuplicates(id);
                showToast('Merged duplicates', 'success');
                loadClips();
            } catch (err) {
                showToast('Failed to merge duplicates', 'error');
            }
            break;
        case 'rename':
            renameClip(id);
            break;
        case 'delete':
            deleteClip(id);
            break;
    }
}

// Expiration preset popover
const EXPIRATION_PRESETS = [
    { label: '15m', minutes: 15 },
    { label: '1h', minutes: 60 },
    { label: '6h', minutes: 360 },
    { label: '24h', minutes: 1440 },
    { label: '7d', minutes: 10080 },
];

// What had focus when the expiration popover opened; Escape returns there.
let expirationPopoverOpener = null;

function openExpirationPopover(clipId, anchorElement, isBulk = false) {
    closeExpirationPopover();
    expirationPopoverOpener = document.activeElement;

    // z-[115]: it also opens from the lightbox's Actions menu (lightbox is 100).
    const popover = document.createElement('div');
    popover.className = 'expiration-popover fixed bg-white rounded-lg shadow-xl border border-stone-200 p-2 z-[115]';
    popover.setAttribute('role', 'menu');
    popover.setAttribute('aria-label', 'Set expiration');
    // Its arrow keys are its own, not the gallery's or the lightbox's.
    popover.dataset.ownsKeys = '';

    const row = document.createElement('div');
    row.className = 'flex items-center gap-1.5';

    EXPIRATION_PRESETS.forEach(preset => {
        const btn = document.createElement('button');
        btn.className = 'px-2.5 py-1.5 text-[11px] font-medium text-stone-600 bg-stone-100 hover:bg-stone-200 rounded-md transition-colors';
        btn.textContent = preset.label;
        btn.setAttribute('role', 'menuitem');
        btn.addEventListener('click', () => {
            closeExpirationPopover();
            if (isBulk) {
                // The helper clears the selection only once the change succeeded.
                bulkSetExpiration(Array.from(selectedIds), preset.minutes);
            } else {
                setExpiration(Number(clipId), preset.minutes);
            }
        });
        row.appendChild(btn);
    });

    popover.appendChild(row);
    // Left/Right move between presets (a one-row menu).
    row.addEventListener('keydown', (e) => {
        if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
        const items = Array.from(row.querySelectorAll('[role="menuitem"]'));
        const idx = items.indexOf(document.activeElement);
        if (idx === -1) return;
        e.preventDefault();
        const next = (idx + (e.key === 'ArrowRight' ? 1 : -1) + items.length) % items.length;
        items[next].focus();
    });
    document.body.appendChild(popover);
    positionExpirationPopover(popover, anchorElement);
    row.querySelector('[role="menuitem"]')?.focus();
}

function positionExpirationPopover(popover, anchor) {
    const rect = anchor.getBoundingClientRect();
    const popoverRect = popover.getBoundingClientRect();
    const pad = 8;

    const spaceBelow = window.innerHeight - rect.bottom - pad;
    let top;
    if (spaceBelow >= popoverRect.height) {
        top = rect.bottom + pad;
    } else {
        top = rect.top - popoverRect.height - pad;
    }

    let left = rect.left + (rect.width / 2) - (popoverRect.width / 2);
    if (left < pad) left = pad;
    if (left + popoverRect.width > window.innerWidth - pad) {
        left = window.innerWidth - popoverRect.width - pad;
    }

    popover.style.left = `${left}px`;
    popover.style.top = `${top}px`;
}

// restoreFocus: return focus to the opener (Escape). Also done whenever focus
// is inside the popover being removed, so it never drops to body.
function closeExpirationPopover({ restoreFocus: restore = false } = {}) {
    const existing = document.querySelector('.expiration-popover');
    if (!existing) return;
    const focusWasInside = existing.contains(document.activeElement);
    existing.remove();
    const opener = expirationPopoverOpener;
    expirationPopoverOpener = null;
    if (restore || focusWasInside) restoreFocus(opener);
}

function renderDragHandle(clipId) {
    if (typeof canDragOut !== 'function' || !canDragOut()) {
        return '';
    }
    return `
        <span class="clip-drag-handle p-1 text-stone-400 hover:text-stone-600 hover:bg-stone-100 rounded transition-colors"
                data-action="drag-out"
                data-id="${clipId}"
                draggable="true"
                role="button"
                tabindex="0"
                aria-label="Drag clip to another app"
                title="Creates a temp file -- drag into another app to export">
            <svg class="clip-drag-icon-grip w-3 h-3" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24" aria-hidden="true">
                <path stroke-linecap="round" stroke-linejoin="round" d="M8 5h.01M8 12h.01M8 19h.01M16 5h.01M16 12h.01M16 19h.01" />
            </svg>
            <svg class="clip-drag-icon-progress w-3 h-3 hidden" viewBox="0 0 24 24" aria-hidden="true">
                <circle class="clip-drag-progress-track" cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="2"></circle>
                <circle class="clip-drag-progress-fill" cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="2"></circle>
            </svg>
            <svg class="clip-drag-icon-spinner w-3 h-3 hidden animate-spin" viewBox="0 0 24 24" aria-hidden="true">
                <circle class="opacity-25" cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="2"></circle>
                <path class="opacity-75" fill="currentColor" d="M12 3a9 9 0 0 1 9 9h-2a7 7 0 0 0-7-7z"></path>
            </svg>
        </span>
    `;
}

function setDragHandleMode(handle, mode) {
    const isArming = mode === 'arming';
    const isPreparing = mode === 'preparing';
    const isReady = mode === 'ready';

    handle.classList.toggle('is-hover-arming', isArming);
    handle.classList.toggle('is-preparing', isPreparing);
    handle.classList.toggle('is-ready', isReady);
    handle.setAttribute('aria-busy', isPreparing ? 'true' : 'false');
    handle.setAttribute('draggable', isReady ? 'true' : 'false');

    const gripIcon = handle.querySelector('.clip-drag-icon-grip');
    const progressIcon = handle.querySelector('.clip-drag-icon-progress');
    const spinnerIcon = handle.querySelector('.clip-drag-icon-spinner');
    if (gripIcon) {
        gripIcon.classList.toggle('hidden', isArming || isPreparing);
    }
    if (progressIcon) {
        progressIcon.classList.toggle('hidden', !isArming);
    }
    if (spinnerIcon) {
        spinnerIcon.classList.toggle('hidden', !isPreparing);
    }
}

async function ensureDragPrepared(handle, clipId) {
    if (typeof getPreparedDragItem === 'function') {
        const cached = getPreparedDragItem(clipId);
        if (cached) {
            setDragHandleMode(handle, 'ready');
            return cached;
        }
    }
    if (typeof prepareDrag !== 'function') {
        setDragHandleMode(handle, 'idle');
        return null;
    }

    setDragHandleMode(handle, 'preparing');
    beginGlobalDragPrepareCursor();
    try {
        const prepared = await prepareDrag(clipId);
        setDragHandleMode(handle, 'ready');
        return prepared;
    } catch (error) {
        console.error(`Failed to prepare clip ${clipId} for drag:`, error);
        setDragHandleMode(handle, 'idle');
        return null;
    } finally {
        endGlobalDragPrepareCursor();
    }
}

function setupDragHandle(handle, clipId) {
    const hoverPrepDelayMs = 1000;
    const id = Number(clipId);
    let armTimer = null;
    let prepPromise = null;
    let hoverLookupSeq = 0;
    let isHovering = false;

    const getPrepared = () => {
        if (typeof getPreparedDragItem !== 'function') {
            return null;
        }
        return getPreparedDragItem(id);
    };

    const clearArmTimer = () => {
        if (armTimer) {
            clearTimeout(armTimer);
            armTimer = null;
        }
    };

    const refreshMode = () => {
        if (prepPromise) {
            setDragHandleMode(handle, 'preparing');
            return;
        }
        const prepared = getPrepared();
        if (prepared) {
            setDragHandleMode(handle, 'ready');
            return;
        }
        if (isHovering && armTimer) {
            setDragHandleMode(handle, 'arming');
            return;
        }
        setDragHandleMode(handle, 'idle');
    };

    const startPrepare = () => {
        if (prepPromise) {
            return prepPromise;
        }
        prepPromise = ensureDragPrepared(handle, id)
            .catch((error) => {
                console.error(`Failed preparing drag for clip ${id}:`, error);
                return null;
            })
            .finally(() => {
                prepPromise = null;
                refreshMode();
            });
        return prepPromise;
    };

    const scheduleHoverPrep = () => {
        const seq = ++hoverLookupSeq;
        const beginArming = () => {
            if (!isHovering || seq !== hoverLookupSeq) {
                return;
            }
            clearArmTimer();
            setDragHandleMode(handle, 'arming');
            armTimer = setTimeout(() => {
                armTimer = null;
                startPrepare();
            }, hoverPrepDelayMs);
        };

        const prepared = getPrepared();
        if (prepared) {
            setDragHandleMode(handle, 'ready');
            return;
        }
        if (prepPromise) {
            setDragHandleMode(handle, 'preparing');
            return;
        }
        if (typeof lookupPreparedDrag !== 'function') {
            beginArming();
            return;
        }

        lookupPreparedDrag(id).then((existing) => {
            if (!isHovering || seq !== hoverLookupSeq) {
                return;
            }
            if (existing) {
                setDragHandleMode(handle, 'ready');
                return;
            }
            beginArming();
        }).catch((error) => {
            console.error(`Failed lookup for prepared drag clip ${id}:`, error);
            beginArming();
        });
    };

    handle.addEventListener('pointerenter', () => {
        isHovering = true;
        scheduleHoverPrep();
    });

    handle.addEventListener('pointerleave', () => {
        isHovering = false;
        hoverLookupSeq += 1;
        clearArmTimer();
        refreshMode();
    });

    handle.addEventListener('pointerdown', (e) => {
        e.stopPropagation();

        const prepared = getPrepared();
        if (!prepared) {
            e.preventDefault();
            clearArmTimer();
            startPrepare();
        }
    });

    // Native drag must be triggered directly from a mouse event for reliable macOS behavior.
    handle.addEventListener('mousedown', (e) => {
        if (e.button !== 0) {
            return;
        }
        const prepared = getPrepared();
        if (!prepared) {
            return;
        }
        if (typeof canUseNativeDragOut === 'function' && canUseNativeDragOut() && typeof startNativeDrag === 'function') {
            e.preventDefault();
            e.stopPropagation();
            clearArmTimer();
            startNativeDrag(id, prepared).catch((error) => {
                console.error(`Failed native drag for clip ${id}:`, error);
            }).finally(() => {
                refreshMode();
            });
        }
    });

    handle.addEventListener('click', (e) => {
        e.preventDefault();
        e.stopPropagation();
    });

    handle.addEventListener('dragstart', (e) => {
        e.stopPropagation();

        if (handle.classList.contains('is-hover-arming') || handle.classList.contains('is-preparing')) {
            e.preventDefault();
            clearArmTimer();
            startPrepare();
            return;
        }

        if (!e.dataTransfer || typeof setDragData !== 'function') {
            e.preventDefault();
            return;
        }

        const prepared = getPrepared();
        const strategy = typeof getDragStrategy === 'function' ? getDragStrategy() : '';
        if (!prepared || !setDragData(e.dataTransfer, prepared, strategy)) {
            e.preventDefault();
            clearArmTimer();
            startPrepare();
            return;
        }

        if (e.dataTransfer) {
            e.dataTransfer.dropEffect = 'copy';
        }
        e.dataTransfer.effectAllowed = 'copy';
        window.__internalDragActive = true;
    });

    handle.addEventListener('dragend', () => {
        window.__internalDragActive = false;
        clearArmTimer();
        refreshMode();
    });

    refreshMode();
}

function resetDragHandleStates() {
    if (!gallery) {
        return;
    }
    const handles = gallery.querySelectorAll('[data-action="drag-out"]');
    handles.forEach((handle) => {
        const id = Number(handle.dataset.id);
        const prepared = typeof getPreparedDragItem === 'function' ? getPreparedDragItem(id) : null;
        setDragHandleMode(handle, prepared ? 'ready' : 'idle');
    });
}

async function createClipCard(clip, options = {}) {
    const card = document.createElement('li');
    card.className = 'bg-white rounded-md border border-stone-200 overflow-hidden flex flex-col transition-all duration-150 hover:border-stone-300 relative group [&.has-checked]:ring-2 [&.has-checked]:ring-stone-800';
    card.dataset.id = clip.id;
    card.dataset.filename = (clip.filename || '').toLowerCase();
    card.dataset.type = (clip.content_type || '').toLowerCase();
    card.dataset.size = clip.size || 0;
    card.dataset.createdAt = clip.created_at || '';
    // The clip object the card's listeners close over; in-place patches and
    // card reuse (reuseClipCard) update this object rather than replacing it.
    card._clip = clip;
    card._renderSig = clipCardSignature(clip);
    rememberRenderedClip(clip);
    if (clip.expires_at) {
        card.dataset.expiresAt = clip.expires_at;
    }
    card.setAttribute('aria-label', `Clip: ${clip.filename || 'Pasted Content'}`);
    if (typeof isFolderMode === 'function' && isFolderMode()) {
        card.setAttribute('draggable', 'true');
        card.setAttribute('aria-grabbed', 'false');
    }

    const checkboxHTML = `
        <div class="absolute top-2 right-2 z-30 opacity-0 group-hover:opacity-100 focus-within:opacity-100 group-[.has-checked]:opacity-100 transition-opacity duration-150">
            <div class="relative">
                <input type="checkbox" data-id="${clip.id}"
                    aria-label="Select clip ${escapeHTML(clip.filename) || 'Pasted Content'}"
                    class="clip-checkbox appearance-none w-5 h-5 rounded border border-white/60 bg-black/20 backdrop-blur-sm checked:bg-stone-700 checked:border-stone-700 transition-all cursor-pointer peer" ${selectedIds.has(clip.id) ? 'checked' : ''}>
                <svg class="absolute inset-0 w-5 h-5 text-white pointer-events-none opacity-0 peer-checked:opacity-100 transition-opacity p-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M5 13l4 4L19 7" />
                </svg>
            </div>
        </div>
    `;

    let previewHTML;

    if (clip.content_type.startsWith('image/')) {
        // For images, show loading placeholder initially
        // The <img> stays in layout (transparent until loaded) rather than
        // display:none, which would stop loading="lazy" from ever fetching it.
        previewHTML = `<div class="preview-container overflow-hidden aspect-square w-full bg-stone-100 flex items-center justify-center relative">
            <img data-clip-id="${clip.id}" alt="${escapeHTML(clip.filename) || 'Uploaded image'}" loading="lazy" decoding="async" class="h-full w-full object-cover transition-transform duration-300 group-hover:scale-[1.02] opacity-0">
            <div class="loading-spinner absolute inset-0 flex items-center justify-center text-stone-400">
                <svg class="animate-spin h-5 w-5" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                    <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3"></circle>
                    <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                </svg>
            </div>
        </div>`;
    } else if (clip.content_type.startsWith('video/')) {
        previewHTML = `<div class="preview-container overflow-hidden aspect-square w-full bg-stone-900 flex items-center justify-center relative">
            <img data-clip-id="${clip.id}" alt="${escapeHTML(clip.filename) || 'Uploaded video'}" draggable="false" class="video-thumb h-full w-full object-cover transition-transform duration-300 group-hover:scale-[1.02] hidden">
            <video data-clip-id="${clip.id}" aria-label="${escapeHTML(clip.filename) || 'Uploaded video'}" muted playsinline preload="metadata" draggable="false" class="h-full w-full object-cover transition-transform duration-300 group-hover:scale-[1.02] hidden"></video>
            <div class="video-play-badge absolute inset-0 hidden items-center justify-center pointer-events-none" aria-hidden="true">
                <span class="w-10 h-10 rounded-full bg-stone-900/70 border border-white/40 text-white flex items-center justify-center backdrop-blur-sm">
                    <svg class="w-4 h-4 ml-0.5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"></path></svg>
                </span>
            </div>
            <div class="loading-spinner absolute inset-0 flex items-center justify-center text-stone-400">
                <svg class="animate-spin h-5 w-5" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                    <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3"></circle>
                    <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                </svg>
            </div>
        </div>`;
    } else if (clip.content_type === 'text/html') {
        // For HTML, show text preview (no iframe in Wails)
        const htmlPreview = escapeHTML(clip.preview || '').substring(0, 200);
        previewHTML = `<div class="preview-container aspect-square w-full relative bg-stone-50">
            <div class="p-3 text-[10px] text-stone-500 font-mono overflow-hidden h-full leading-relaxed">${htmlPreview}...</div>
            <div class="absolute inset-0 bg-transparent" title="HTML Preview"></div>
        </div>`;
    } else if (clip.content_type.startsWith('text/') || clip.content_type === 'application/json') {
        previewHTML = `<div class="preview-container aspect-square w-full overflow-hidden bg-stone-900"><pre class="p-3 text-[9px] leading-relaxed overflow-auto h-full text-stone-400"><code>${escapeHTML(clip.preview)}</code></pre></div>`;
    } else {
        previewHTML = `
        <div class="preview-container aspect-square w-full flex flex-col items-center justify-center bg-stone-50 text-stone-400">
            <svg class="w-10 h-10" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"></path></svg>
            <span class="mt-2 text-[9px] font-medium uppercase tracking-wider">${escapeHTML(getFriendlyFileType(clip.content_type, clip.filename))}</span>
        </div>`;
    }

    const expirationBadge = expirationBadgeHTML(clip);
    const dragHandleHTML = renderDragHandle(clip.id);

    card.innerHTML = `
        ${checkboxHTML}
        <div class="relative cursor-pointer" data-action="open-lightbox">
            ${expirationBadge}
            ${previewHTML}
        </div>

        <!-- Minimal footer -->
        <div class="p-2.5 flex flex-col gap-1.5 border-t border-stone-100">
            <p class="clip-filename text-[11px] font-medium text-stone-700 truncate" title="${escapeHTML(clip.filename) || 'Pasted Content'}">
                ${escapeHTML(clip.filename) || '<span class="text-stone-400 font-normal">Pasted</span>'}
            </p>
            <div class="flex justify-between items-center">
                <div class="flex items-center gap-1.5"><span class="clip-type-label text-[9px] font-medium text-stone-400 uppercase tracking-wide">${escapeHTML(getFriendlyFileType(clip.content_type, clip.filename))}</span>${clip.duplicate_count > 0 ? `<span class="dedup-badge text-[9px] font-medium text-stone-400 bg-stone-100 border border-stone-200 rounded px-1">${clip.duplicate_count + 1} copies</span>` : ''}</div>
                <div class="flex items-center gap-1">
                    ${dragHandleHTML}
                    <button class="card-menu-trigger p-1 text-stone-400 hover:text-stone-600 hover:bg-stone-100 rounded transition-colors"
                            data-action="menu"
                            data-id="${clip.id}"
                            aria-label="Actions"
                            aria-haspopup="true"
                            aria-expanded="false"
                            title="More actions for this clip">
                        <svg class="w-3 h-3" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" d="M12 6.75a.75.75 0 110-1.5.75.75 0 010 1.5zM12 12.75a.75.75 0 110-1.5.75.75 0 010 1.5zM12 18.75a.75.75 0 110-1.5.75.75 0 010 1.5z" />
                        </svg>
                    </button>
                </div>
            </div>
        </div>
    `;

    // Menu trigger listener
    const menuTrigger = card.querySelector('[data-action="menu"]');
    menuTrigger.addEventListener('click', (e) => {
        e.stopPropagation();
        renderCardMenu(clip.id, e.currentTarget, clip);
    });

    // Right-click anywhere on the card opens the same actions menu as the three-dot trigger.
    // With several clips selected, right-clicking one of them opens the bulk
    // actions instead, so the toolbar's operations are reachable in place.
    card.addEventListener('contextmenu', (e) => {
        e.preventDefault();
        e.stopPropagation();
        const anchor = { top: e.clientY, left: e.clientX, right: e.clientX, bottom: e.clientY, width: 0, height: 0 };
        if (selectedIds.size > 1 && selectedIds.has(Number(clip.id))) {
            openBulkContextMenu(anchor, card);
            return;
        }
        renderCardMenu(clip.id, menuTrigger, clip, anchor);
    });

    const dragHandle = card.querySelector('[data-action="drag-out"]');
    if (dragHandle) {
        setupDragHandle(dragHandle, clip.id);
    }

    // Render tags on the card
    if (typeof renderCardTags === 'function') {
        renderCardTags(card, clip.tags);
    }

    // A clip only reaches the gallery with a hidden tag when a search was asked
    // to include it. Dim it the same way a hidden folder card is dimmed, so the
    // result never looks like an ordinary listing that leaked something. Only
    // then: inside a hidden tag's folder, or filtering by it, the clip is an
    // ordinary result and must not look withheld.
    if (clipShownDespiteHiddenTag(clip)) {
        card.dataset.hidden = 'true';
    }
    if (selectedIds.has(Number(clip.id))) card.classList.add('has-checked');

    // Checkbox logic
    const checkbox = card.querySelector('.clip-checkbox');
    checkbox.addEventListener('change', (e) => {
        const id = Number(clip.id);
        if (e.target.checked) {
            selectedIds.add(id);
            card.classList.add('has-checked');
        } else {
            selectedIds.delete(id);
            card.classList.remove('has-checked');
        }

        // Shift-click range selection
        if (e.target.checked && checkbox._shiftEvent && lastCheckedCheckbox && lastCheckedCheckbox !== checkbox) {
            const allCards = Array.from(gallery.querySelectorAll(':scope > li'));
            const currentIndex = allCards.indexOf(card);
            const lastIndex = allCards.indexOf(lastCheckedCheckbox.closest('li'));
            if (currentIndex !== -1 && lastIndex !== -1) {
                const start = Math.min(currentIndex, lastIndex);
                const end = Math.max(currentIndex, lastIndex);
                for (let i = start; i <= end; i++) {
                    const cb = allCards[i].querySelector('.clip-checkbox');
                    if (cb && !cb.checked) {
                        cb.checked = true;
                        const cbId = Number(cb.dataset.id);
                        selectedIds.add(cbId);
                        allCards[i].classList.add('has-checked');
                    }
                }
            }
        }
        lastCheckedCheckbox = checkbox;

        // Sync Select All checkbox
        const allCheckboxes = Array.from(gallery.querySelectorAll('.clip-checkbox'));
        selectAllCheckbox.checked = allCheckboxes.length > 0 && allCheckboxes.every(cb => cb.checked);

        updateBulkToolbar();
    });

    // Prevent lightbox trigger if clicking checkbox; track shift state for range selection
    checkbox.addEventListener('click', (e) => {
        e.stopPropagation();
        checkbox._shiftEvent = e.shiftKey;
    });

    // Lightbox trigger logic
    if (clip.content_type.startsWith('image/') || clip.content_type.startsWith('video/')) {
        const lightboxTrigger = card.querySelector('[data-action="open-lightbox"]');
        lightboxTrigger.addEventListener('click', () => {
            window.LightboxController.open({
                clips: getVisibleMediaClips(),
                currentId: clip.id,
                opener: card,
            });
        });

        // Load the image or a representative video frame asynchronously.
        loadMediaForCard(clip, card);
    } else {
        // For non-media clips, clicking opens the editor or shows content. This
        // sits inside the non-media branch, so the text-only check is right —
        // and this is a generic open, so the descriptor picks the mode.
        card.querySelector('[data-action="open-lightbox"]').addEventListener('click', () => {
            if (isTextCandidate(clip.filename || '', clip.content_type || '')) {
                openEditor(clip.id);
            }
        });
    }

    if (options.prepend) {
        gallery.prepend(card);
        window.LightboxController?.setClips(getVisibleMediaClips());
        if (window.__galleryRover) window.__galleryRover.update();
    } else {
        // Batch renders (loadClips, loadMoreClips) update the rover once when
        // done: update() lays out the whole grid and walks every card, so
        // calling it per append made a render quadratic.
        gallery.appendChild(card);
    }

    return card;
}

function clipShownDespiteHiddenTag(clip) {
    return galleryWaivedHiddenTags.length > 0 && Array.isArray(clip.tags)
        && clip.tags.some(tag => galleryWaivedHiddenTags.some(h => tag.name === h || tag.name.startsWith(h + '/')));
}

// Everything a card's markup is built from. A full reload keeps a card whose
// signature is unchanged (reuseClipCard) instead of rebuilding it, which would
// refetch and re-decode its thumbnail or recapture its video frame. Besides the
// clip's own fields it covers the render context a card bakes in: folder mode
// (draggable), the hidden-tag dimming of a search that included hidden clips,
// and whether drag-out is available (the drag handle).
function clipCardSignature(clip) {
    const id = Number(clip.id);
    const tags = Array.isArray(clip.tags) ? clip.tags.map(t => [t.id, t.name, t.color]) : [];
    return JSON.stringify([
        clip.filename || '', Number(clip.size) || 0, clip.content_type || '', clip.created_at || '',
        clip.expires_at || '', !!clip.is_archived, Number(clip.duplicate_count) || 0,
        clip.content_hash || '', clip.preview || '', tags, mediaRevisions.get(id) || 0,
        typeof isFolderMode === 'function' && isFolderMode(), clipShownDespiteHiddenTag(clip),
        typeof canDragOut === 'function' && canDragOut(),
    ]);
}

// Take a card from the previous render into this one. The listeners close over
// card._clip, so the fresh listing row is copied into that object rather than
// swapped for it. Selection is re-read (a bulk action clears it before its
// reload) and the expiry countdown is re-rendered, as a rebuild would have.
function reuseClipCard(card, clip) {
    const kept = card._clip;
    Object.assign(kept, clip);
    rememberRenderedClip(kept);
    renderCardExpiry(card, kept);
    const selected = selectedIds.has(Number(kept.id));
    const checkbox = card.querySelector('.clip-checkbox');
    if (checkbox) checkbox.checked = selected;
    card.classList.toggle('has-checked', selected);
    card._renderSig = clipCardSignature(kept);
    return card;
}

function expirationBadgeHTML(clip) {
    if (!clip.expires_at) return '';
    const remaining = formatTimeRemaining(clip.expires_at);
    return `<div class="clip-expiration-badge absolute top-2 left-2 bg-stone-700 text-white text-[8px] font-semibold px-1.5 py-0.5 rounded z-20 uppercase tracking-wide">
            Temp · ${escapeHTML(remaining)}
        </div>`;
}

// Re-render a card's expiry badge from clip.expires_at.
function renderCardExpiry(card, clip) {
    card.querySelector('.clip-expiration-badge')?.remove();
    if (clip.expires_at) {
        card.dataset.expiresAt = clip.expires_at;
        card.querySelector('[data-action="open-lightbox"]')?.insertAdjacentHTML('afterbegin', expirationBadgeHTML(clip));
    } else {
        delete card.dataset.expiresAt;
    }
}

// Re-render everything on a card that shows the clip's filename.
function renderCardFilename(card, clip) {
    const name = clip.filename || '';
    card.dataset.filename = name.toLowerCase();
    card.setAttribute('aria-label', `Clip: ${name || 'Pasted Content'}`);
    const label = card.querySelector('.clip-filename');
    if (label) {
        label.setAttribute('title', name || 'Pasted Content');
        label.innerHTML = escapeHTML(name) || '<span class="text-stone-400 font-normal">Pasted</span>';
    }
    card.querySelector('.clip-checkbox')?.setAttribute('aria-label', `Select clip ${name || 'Pasted Content'}`);
    const type = card.querySelector('.clip-type-label');
    if (type) type.textContent = getFriendlyFileType(clip.content_type, clip.filename);
    const isVideo = (clip.content_type || '').startsWith('video/');
    card.querySelectorAll('img[data-clip-id]').forEach(img => {
        img.setAttribute('alt', name || (isVideo ? 'Uploaded video' : 'Uploaded image'));
    });
    card.querySelector('video[data-clip-id]')?.setAttribute('aria-label', name || 'Uploaded video');
}

// A card leaving the gallery: stop watching it for media work.
function forgetCardMedia(card) {
    cardMediaTasks.delete(card);
    if (cardMediaObserver) cardMediaObserver.unobserve(card);
}

// Load a card's media. Images get a URL-served thumbnail straight away (the
// browser lazy-loads it); videos capture a representative frame, through the
// card media scheduler.
function loadMediaForCard(clip, card) {
    const clipId = clip.id;
    if (card.querySelector(`img[data-clip-id="${clipId}"]:not(.video-thumb)`)) {
        loadImageCard(clip, card).catch(error => showCardMediaError(clipId, card, error));
    } else if (card.querySelector(`video[data-clip-id="${clipId}"]`)) {
        // A frame already captured for this revision needs no decoder at all.
        if (showCachedVideoFrame(clip, card)) return;
        scheduleCardMedia(card, () => loadVideoCard(clip, card).catch(error => showCardMediaError(clipId, card, error)));
    }
}

// A card whose media failed (or fell back to a live video) is rebuilt by the
// next reload rather than reused, so a transient failure is retried.
function markCardMediaFailed(card) {
    card._mediaFailed = true;
}

function showCardMediaError(clipId, card, error) {
    console.error(`Failed to load media for clip ${clipId}:`, error);
    markCardMediaFailed(card);
    const spinner = card.querySelector('.loading-spinner');
    if (spinner) {
        spinner.innerHTML = '<span class="text-red-400 text-xs">Failed to load</span>';
    }
}

// Image cards show the backend thumbnail (thumbnail.go) — a bounded copy
// served over HTTP — never the full original through GetClipData. If the
// thumbnail fails, the card falls back to the full image, once.
async function loadImageCard(clip, card) {
    const clipId = clip.id;
    const img = card.querySelector(`img[data-clip-id="${clipId}"]:not(.video-thumb)`);
    const spinner = card.querySelector('.loading-spinner');
    let fellBack = false;

    const fallback = () => {
        if (fellBack) {
            showCardMediaError(clipId, card, new Error('image failed to load'));
            return;
        }
        fellBack = true;
        scheduleCardMedia(card, async () => {
            const url = await getImageDataUrl(clipId);
            if (card.isConnected) img.src = url;
        });
    };
    img.addEventListener('load', () => {
        img.classList.remove('opacity-0');
        spinner?.remove();
    }, { once: true });
    img.addEventListener('error', fallback);

    const url = await getThumbnailUrl(clip);
    if (!card.isConnected) return;
    if (url) {
        img.dataset.thumbnail = 'true';
        img.src = url;
    } else {
        fallback();
    }
}

const VIDEO_CARD_SLOT_TIMEOUT_MS = 15000;

// Capture a video card's frame. Video elements seek just past the start so
// clips whose first frame is empty still get a useful thumbnail. Resolves when
// the card has settled on a frame, the live element or an error.
// Show a cached frame (videoFrameCache) on a video card: the same end state
// freezeToImage reaches, without loading the video. False when nothing is cached.
function showCachedVideoFrame(clip, card) {
    const clipId = clip.id;
    const url = videoFrameCacheGet(clip);
    const thumb = card.querySelector(`img.video-thumb[data-clip-id="${clipId}"]`);
    if (!url || !thumb) return false;
    thumb.src = url;
    thumb.dataset.frameCached = 'true';
    thumb.classList.remove('hidden');
    card.querySelector(`video[data-clip-id="${clipId}"]`)?.remove();
    const badge = card.querySelector('.video-play-badge');
    badge?.classList.remove('hidden');
    badge?.classList.add('flex');
    card.querySelector('.loading-spinner')?.remove();
    return true;
}

async function loadVideoCard(clip, card) {
    const clipId = clip.id;
    // Captured now, so a frame from bytes replaced mid-capture is not cached
    // under the new revision.
    const frameRevision = videoFrameRevision(clip);
    if (showCachedVideoFrame(clip, card)) return;
    const video = card.querySelector(`video[data-clip-id="${clipId}"]`);
    const thumb = card.querySelector(`img.video-thumb[data-clip-id="${clipId}"]`);
    const spinner = card.querySelector('.loading-spinner');
    const mediaURL = await getVideoMediaUrl(clipId);
    // Preparing the URL is a round-trip, and the gallery may have been
    // rebuilt underneath us in the meantime (a search keystroke is
    // enough). Loading a detached element would open a range fetch and
    // a decoder that nothing will ever release.
    if (!card.isConnected) return;
    let thumbnailSeekPending = false;
    let settled = false;
    // The scheduler slot is held until the frame is captured (or the
    // load gives up), so only a few decoders run at once. A stalled load
    // releases its slot after VIDEO_CARD_SLOT_TIMEOUT_MS without being
    // cancelled.
    let finish;
    const done = new Promise(resolve => { finish = resolve; });
    setTimeout(() => finish(), VIDEO_CARD_SLOT_TIMEOUT_MS);

    const showBadge = () => {
        const badge = card.querySelector('.video-play-badge');
        badge?.classList.remove('hidden');
        badge?.classList.add('flex');
        spinner?.remove();
    };

    // Fall back to the live element. It renders correctly everywhere;
    // it just costs a decoder and keeps a range fetch open.
    const keepLiveVideo = () => {
        if (settled) return;
        settled = true;
        markCardMediaFailed(card);
        video.classList.remove('hidden');
        showBadge();
        finish();
    };

    // Hand the frame to an <img> and drop the video element entirely.
    //
    // The frame cannot be carried by the video's own `poster`: WebKit —
    // the WebView this app ships in — paints a video whose `src` has
    // been removed as an empty black box and ignores the poster, while
    // Chromium honours it, so the e2e suite cannot see the difference.
    // An <img> renders the same frame correctly in both. Verified by
    // snapshotting a real WKWebView; see docs in CLAUDE.md.
    const freezeToImage = () => {
        if (settled || !thumb) return keepLiveVideo();
        let canvas;
        try {
            const width = video.videoWidth;
            const height = video.videoHeight;
            if (!width || !height) return keepLiveVideo();
            const scale = Math.min(1, THUMBNAIL_MAX_EDGE / Math.max(width, height));
            canvas = document.createElement('canvas');
            canvas.width = Math.max(1, Math.round(width * scale));
            canvas.height = Math.max(1, Math.round(height * scale));
            canvas.getContext('2d').drawImage(video, 0, 0, canvas.width, canvas.height);
            // A tainted canvas throws here, synchronously, so the live-video
            // fallback still applies. The JPEG becomes an object URL kept in
            // videoFrameCache, so a rebuilt card never captures it again.
            canvas.toBlob((blob) => {
                let url = null;
                if (blob && videoFrameRevision(clip) === frameRevision) {
                    url = URL.createObjectURL(blob);
                    videoFrameCacheSet(Number(clipId), frameRevision, url);
                } else {
                    // No blob, or the clip's bytes changed while encoding:
                    // show this frame without caching it under the new revision.
                    try { url = canvas.toDataURL('image/jpeg', 0.85); } catch { url = null; }
                }
                if (url) {
                    thumb.src = url;
                    thumb.classList.remove('hidden');
                    showBadge();
                } else {
                    markCardMediaFailed(card);
                    if (spinner) spinner.innerHTML = '<span class="text-red-400 text-xs">Failed to load</span>';
                }
                finish();
            }, 'image/jpeg', 0.85);
        } catch (error) {
            console.warn(`Could not capture thumbnail for clip ${clipId}:`, error);
            return keepLiveVideo();
        }
        settled = true;
        // Releasing the source frees the decoder and ends the range
        // fetch. A gallery page holds up to defaultClipLimit cards, and
        // a still frame has no business keeping a video pipeline alive.
        // The canvas already holds the frame.
        video.removeAttribute('src');
        video.load();
        video.remove();
    };

    // `seeked` says the seek finished, not that a frame was presented.
    const freezeWhenPainted = () => {
        if (settled) return;
        if (typeof video.requestVideoFrameCallback === 'function') {
            let fired = false;
            video.requestVideoFrameCallback(() => { fired = true; freezeToImage(); });
            setTimeout(() => { if (!fired) freezeToImage(); }, 1000);
            return;
        }
        requestAnimationFrame(() => requestAnimationFrame(freezeToImage));
    };

    video.addEventListener('loadedmetadata', () => {
        if (Number.isFinite(video.duration) && video.duration > 0.1) {
            thumbnailSeekPending = true;
            video.currentTime = thumbnailSeekTime(video.duration);
        }
    }, { once: true });
    video.addEventListener('loadeddata', () => {
        if (!thumbnailSeekPending) freezeWhenPainted();
    }, { once: true });
    video.addEventListener('seeked', () => {
        thumbnailSeekPending = false;
        freezeWhenPainted();
    }, { once: true });
    video.addEventListener('error', () => {
        if (settled) return;
        settled = true;
        // Release the source on failure too, or the element sits in the
        // gallery holding a dead fetch for the life of the render.
        video.removeAttribute('src');
        video.load();
        video.remove();
        markCardMediaFailed(card);
        if (spinner) spinner.innerHTML = '<span class="text-red-400 text-xs">Failed to load</span>';
        finish();
    });
    video.src = mediaURL;
    video.load();
    return done;
}

// Where to sample a video for its card thumbnail. Not the opening frame: real
// footage very often fades in from black, so a frame from the first moments is
// a black square that tells the user nothing. A short way in is past the fade
// on typical clips while staying cheap to reach over a range request.
function thumbnailSeekTime(duration) {
    return Math.min(THUMBNAIL_SEEK_MAX_SECONDS, Math.max(0.1, duration * 0.1));
}

// Return a URL the browser can request with byte ranges. Desktop mode leases a
// temp file behind the transfer handler, so seeking is a file seek; server mode
// streams the clip from the authenticated same-origin REST endpoint. The URL is
// cached for the lease window so a clip is materialized at most once.
async function getVideoMediaUrl(clipId) {
    const id = Number(clipId);
    if (!Number.isFinite(id) || id <= 0) throw new Error(`Invalid clip ID: ${clipId}`);

    const cached = videoMediaURLCache.get(id);
    if (cached && cached.expiresAt > Date.now() + 5000) return cached.url;
    if (pendingVideoMediaURLs.has(id)) return pendingVideoMediaURLs.get(id);

    const revision = mediaRevisions.get(id) || 0;
    const pending = (async () => {
        if (window.mahpastesMode === 'server') {
            // The revision keeps the browser from reusing a response for bytes
            // that have since been overwritten.
            const url = revision ? `/api/v1/clips/${id}/data?rev=${revision}` : `/api/v1/clips/${id}/data`;
            videoMediaURLCache.set(id, { url, expiresAt: Number.POSITIVE_INFINITY });
            return url;
        }

        const service = window.go?.main?.TransferService;
        if (!service || typeof service.PrepareClipForTransfer !== 'function') {
            throw new Error('Transfer service binding is unavailable');
        }
        const prepared = await service.PrepareClipForTransfer({
            clip_id: id,
            channel: 'media_preview',
        });
        if (!prepared?.transfer_url) throw new Error('Video media URL is unavailable');

        const parsedExpiry = Date.parse(prepared.lease_expires_at);
        const expiresAt = Number.isFinite(parsedExpiry) ? parsedExpiry : Date.now() + 55 * 60 * 1000;
        if ((mediaRevisions.get(id) || 0) === revision) {
            videoMediaURLCache.set(id, { url: prepared.transfer_url, expiresAt });
        }
        return prepared.transfer_url;
    })();
    pendingVideoMediaURLs.set(id, pending);
    try {
        return await pending;
    } finally {
        if (pendingVideoMediaURLs.get(id) === pending) pendingVideoMediaURLs.delete(id);
    }
}

// The full-size image for the lightbox and comparison view (and a card whose
// thumbnail failed). Desktop: a data URL over the bridge, kept in the bounded
// imageCache. Server mode: the same-origin data URL itself — the browser
// fetches and caches it, with no Blob -> FileReader -> base64 round trip.
const SERVER_IMAGE_MAX_INLINE = 64 * 1024 * 1024; // matches rest-glue GetClipData

async function getImageDataUrl(clipId) {
    const id = Number(clipId);
    const revision = mediaRevisions.get(id) || 0;
    if (window.mahpastesMode === 'server') {
        // Same ceiling rest-glue's GetClipData applies: past it the browser is
        // asked to decode an arbitrarily large image, so offer a download.
        const card = gallery?.querySelector(`li[data-id="${id}"]`);
        const size = Number(card?.dataset.size) || 0;
        if (size > SERVER_IMAGE_MAX_INLINE) {
            throw new Error(`clip is too large to preview in the browser (${Math.round(size / 1048576)} MB) — download it instead`);
        }
        // The revision keeps the browser from reusing a response for bytes
        // that have since been overwritten.
        return revision ? `/api/v1/clips/${id}/data?rev=${revision}` : `/api/v1/clips/${id}/data`;
    }

    const cached = imageCacheGet(id);
    if (cached !== undefined) return cached;

    const clipData = await getClipData(id);
    const dataUrl = `data:${clipData.content_type};base64,${clipData.data}`;
    if ((mediaRevisions.get(id) || 0) === revision) imageCacheSet(id, dataUrl);
    return dataUrl;
}

function updateBulkToolbar() {
    const count = selectedIds.size;
    if (count > 0) {
        bulkToolbar.removeAttribute('inert');
        bulkToolbar.classList.remove('hidden', 'translate-y-4', 'opacity-0', 'pointer-events-none');
        bulkToolbar.classList.add('translate-y-0', 'opacity-100', 'pointer-events-auto');
        selectedCountEl.textContent = `${count} selected`;
        bulkArchiveText.textContent = isViewingArchive ? 'Restore' : 'Archive';

        updateBulkMoreButton();

        // Show "Clear Expiry" if any selected clip has expiration
        const bulkCancelExpiryBtn = document.getElementById('bulk-cancel-expiry-btn');
        if (bulkCancelExpiryBtn) {
            const anyExpiring = Array.from(selectedIds).some(id => {
                const card = gallery.querySelector(`li[data-id="${id}"]`);
                return card && card.dataset.expiresAt;
            });
            if (anyExpiring) {
                bulkCancelExpiryBtn.classList.remove('hidden');
            } else {
                bulkCancelExpiryBtn.classList.add('hidden');
            }
        }

        // Initialize or update roving tabindex for bulk toolbar buttons
        if (!bulkToolbarRover) {
            bulkToolbarRover = RovingTabindex.create({
                container: bulkToolbar,
                itemSelector: 'button:not(.hidden):not([style*="display: none"])',
                orientation: 'horizontal',
                wrap: false,
            });
        } else {
            bulkToolbarRover.update();
        }
    } else {
        const moreButton = document.getElementById('bulk-more-btn');
        if (moreButton) moreButton.classList.add('hidden');
        bulkToolbar.setAttribute('inert', '');
        bulkToolbar.classList.remove('translate-y-0', 'opacity-100', 'pointer-events-auto');
        bulkToolbar.classList.add('translate-y-4', 'opacity-0', 'pointer-events-none');
        selectAllCheckbox.checked = false;
        if (typeof ContextMenu !== 'undefined') ContextMenu.close();

        // Destroy roving tabindex when toolbar is hidden
        if (bulkToolbarRover) {
            bulkToolbarRover.destroy();
            bulkToolbarRover = null;
        }
    }

}

function toggleSelectAll() {
    const checkboxes = gallery.querySelectorAll('.clip-checkbox');
    const shouldSelectAll = selectAllCheckbox.checked;

    checkboxes.forEach(cb => {
        const id = Number(cb.dataset.id);
        const card = cb.closest('li');
        if (shouldSelectAll) {
            if (card && card.style.display === 'none') return;
            cb.checked = true;
            selectedIds.add(id);
            if (card) card.classList.add('has-checked');
        } else {
            cb.checked = false;
            selectedIds.delete(id);
            if (card) card.classList.remove('has-checked');
        }
    });
    updateBulkToolbar();
}

function cancelSelection() {
    selectedIds.clear();
    lastCheckedCheckbox = null;
    const checkboxes = gallery.querySelectorAll('.clip-checkbox');
    checkboxes.forEach(cb => {
        cb.checked = false;
        const card = cb.closest('li');
        if (card) card.classList.remove('has-checked');
    });
    selectAllCheckbox.checked = false;
    updateBulkToolbar();
}

function toggleViewMode() {
    isViewingArchive = !isViewingArchive;

    // Return to clips view if not already there
    if (currentView !== 'clips') {
        switchView('clips');
    }

    toggleArchiveViewBtn.setAttribute('aria-pressed', isViewingArchive);
    if (isViewingArchive) {
        archiveBtnText.textContent = "Active";
        toggleArchiveViewBtn.classList.add('bg-stone-800', 'text-white', 'border-stone-800');
        toggleArchiveViewBtn.classList.remove('border-stone-200', 'text-stone-600', 'hover:border-stone-300', 'hover:bg-stone-100');
        headerArchiveBtn.setAttribute('aria-pressed', 'true');
        headerArchiveBtn.classList.add('bg-stone-800', 'text-white', 'border-stone-800');
        headerArchiveBtn.classList.remove('border-stone-200', 'text-stone-500', 'hover:border-stone-300', 'hover:bg-stone-100');
    } else {
        archiveBtnText.textContent = "Archive";
        toggleArchiveViewBtn.classList.remove('bg-stone-800', 'text-white', 'border-stone-800');
        toggleArchiveViewBtn.classList.add('border-stone-200', 'text-stone-600', 'hover:border-stone-300', 'hover:bg-stone-100');
        headerArchiveBtn.setAttribute('aria-pressed', 'false');
        headerArchiveBtn.classList.remove('bg-stone-800', 'text-white', 'border-stone-800');
        headerArchiveBtn.classList.add('border-stone-200', 'text-stone-500', 'hover:border-stone-300', 'hover:bg-stone-100');
    }

    // Ensure main view is visible
    gallery.parentElement.classList.remove('hidden');

    // Release references to media cached for the previous view.
    clearMediaCaches();
    loadClips();
}

// Search Logic
const searchInput = document.getElementById('search-input');

// True when the cards on screen came from App.SearchClips rather than the plain
// listing. The database already applied the query — including matches inside
// file contents, which no card attribute can express — so the local filter must
// not second-guess what it returned.
let galleryShowsSearchResults = false;
// Names of the hidden tags a deep search with "Show hidden clips" waived for
// the cards on screen; clips carrying one (or a descendant) are dimmed.
let galleryWaivedHiddenTags = [];
function setGalleryWaivedHiddenTags(ids) {
    const wanted = new Set(ids || []);
    galleryWaivedHiddenTags = allTags.filter(t => wanted.has(t.id)).map(t => t.name);
}
function setGalleryShowsSearchResults(value) { galleryShowsSearchResults = value; }
function galleryIsSearchResults() { return galleryShowsSearchResults; }

function applySearchFilter() {
    const query = searchInput.value.trim().toLowerCase();
    const cards = gallery.querySelectorAll(':scope > li');
    let deselected = false;
    cards.forEach(card => {
        if (!query || galleryShowsSearchResults) {
            card.style.display = '';
            return;
        }
        // Folder cards match on their name; clip cards on filename and type.
        const name = card.dataset.filename ?? card.dataset.folderName ?? '';
        const type = card.dataset.type || '';
        const visible = name.includes(query) || type.includes(query);
        card.style.display = visible ? '' : 'none';
        // A clip the filter hides cannot stay selected: bulk actions would act
        // on cards the user can no longer see.
        if (!visible && card.dataset.id && selectedIds.has(Number(card.dataset.id))) {
            selectedIds.delete(Number(card.dataset.id));
            const cb = card.querySelector('.clip-checkbox');
            if (cb) cb.checked = false;
            card.classList.remove('has-checked');
            deselected = true;
        }
    });
    if (deselected) updateBulkToolbar();
    if (typeof renderGalleryCount === 'function') renderGalleryCount();
    window.LightboxController?.setClips(getVisibleMediaClips());
}

// A deep search is a database round-trip, so it cannot ride every keystroke the
// way the local filter does. Debounce it, and fire it on the way out too — when
// the query is cleared or the options are turned off, the gallery has to come
// back from search results to the plain listing.
let searchReloadTimer = null;
const SEARCH_RELOAD_DELAY_MS = 250;

function scheduleSearchReload() {
    const wanted = typeof isDeepSearchActive === 'function' && isDeepSearchActive();
    if (!wanted && !galleryShowsSearchResults) return;
    clearTimeout(searchReloadTimer);
    searchReloadTimer = setTimeout(() => { loadClips(); }, SEARCH_RELOAD_DELAY_MS);
}

searchInput.addEventListener('input', () => {
    applySearchFilter();
    scheduleSearchReload();
});

searchInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
        e.preventDefault();
        // Focus the first visible gallery item
        const firstVisible = gallery.querySelector(':scope > li:not([style*="display: none"])');
        if (firstVisible && window.__galleryRover) {
            const items = window.__galleryRover.getItems();
            const idx = items.indexOf(firstVisible);
            if (idx >= 0) window.__galleryRover.setActiveIndex(idx);
            firstVisible.focus();
        }
    }
});

// --- Folder Mode Rendering ---

// Generation counter: incremented by loadClips before each render.
// renderFolderCards captures its value at start and aborts if superseded.
let _folderRenderGen = 0;

// Status-badge state for folder cards (populated by folderStatusPoller in app.js).
// Keyed by tagID; values are { served, shared, servePaused, sharePaused, serveURL, serveRequests, shareFollowers }.
const folderStatusMap = new Map();

// In-place badge update. Only mutates cards that already carry a .folder-status-badges
// container, so it's a harmless no-op during Phase 2 (before renderFolderCards is updated
// to emit the container in Phase 3). No focus/hover state disturbed.
function updateFolderBadgesInPlace() {
    const containers = gallery.querySelectorAll('[data-folder] .folder-status-badges');
    containers.forEach(container => {
        const card = container.closest('[data-folder]');
        if (!card) return;
        const tagID = parseInt(card.getAttribute('data-folder'), 10);
        const state = folderStatusMap.get(tagID) || {};
        const badges = [];
        if (state.served) badges.push(renderBadge('served', state));
        if (state.shared) badges.push(renderBadge('shared', state));
        container.innerHTML = badges.join('');
        const path = card.getAttribute('data-folder-path') || card.querySelector('.text-xs')?.textContent || '';
        const countText = card.querySelector('.text-\\[10px\\]')?.textContent || '';
        card.setAttribute('aria-label', buildFolderAriaLabel(path, countText, state, card.getAttribute('data-hidden') === 'true'));
    });
}

function renderBadge(kind, state) {
    if (kind === 'served') {
        const paused = !!state.servePaused;
        const tooltip = buildServeTooltip(state);
        const aria = paused ? 'Serving paused' : (state.serveURL ? `Served on ${state.serveURL}` : 'Serving this folder');
        const klass = paused ? 'folder-badge folder-badge-paused' : 'folder-badge folder-badge-serve';
        return `<span role="img" data-kind="serve" aria-label="${escapeHTML(aria)}" data-tooltip="${escapeHTML(tooltip)}" class="${klass}">
            <svg aria-hidden="true" focusable="false" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a15 15 0 0 1 0 18M12 3a15 15 0 0 0 0 18"/></svg>
        </span>`;
    }
    if (kind === 'shared') {
        const paused = !!state.sharePaused;
        const tooltip = buildShareTooltip(state);
        const aria = paused ? 'Sharing paused' : (typeof state.shareFollowers === 'number' ? `Sharing, ${state.shareFollowers} followers` : 'Sharing this folder');
        const klass = paused ? 'folder-badge folder-badge-paused' : 'folder-badge folder-badge-share';
        return `<span role="img" data-kind="share" aria-label="${escapeHTML(aria)}" data-tooltip="${escapeHTML(tooltip)}" class="${klass}">
            <svg aria-hidden="true" focusable="false" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10 13a5 5 0 0 0 7.07 0l3-3a5 5 0 0 0-7.07-7.07L12 4.5M14 11a5 5 0 0 0-7.07 0l-3 3a5 5 0 0 0 7.07 7.07L12 19.5"/></svg>
        </span>`;
    }
    return '';
}

function buildServeTooltip(state) {
    if (state.servePaused) return 'Serving paused — click to resume in Serve view';
    if (state.serveURL) {
        const count = typeof state.serveRequests === 'number' ? ` · ${state.serveRequests} request${state.serveRequests === 1 ? '' : 's'}` : '';
        return `Serving on ${state.serveURL}${count}`;
    }
    return 'Serving this folder';
}

function buildShareTooltip(state) {
    if (state.sharePaused) return 'Sharing paused';
    if (typeof state.shareFollowers === 'number') {
        return `Sharing this folder — ${state.shareFollowers} follower${state.shareFollowers === 1 ? '' : 's'}`;
    }
    return 'Sharing this folder';
}

function buildFolderAriaLabel(path, countText, state, hidden) {
    const parts = [`Folder: ${path}`, countText];
    if (state.served) parts.push(state.servePaused ? 'serving paused' : 'served');
    if (state.shared) parts.push(state.sharePaused ? 'sharing paused' : 'shared');
    if (hidden) parts.push('hidden');
    return parts.filter(Boolean).join(', ');
}

function applyFolderStatusUpdate(serveStatuses, shareStatus) {
    folderStatusMap.clear();
    (serveStatuses || []).forEach(s => {
        const id = s.tag_id;
        const entry = folderStatusMap.get(id) || {};
        // Serve entries in the returned list imply "running" unless the field says otherwise.
        entry.served = (s.running === undefined) ? true : !!s.running;
        entry.servePaused = !!s.paused;
        entry.serveURL = s.url || null;
        entry.serveRequests = typeof s.request_count === 'number' ? s.request_count : undefined;
        folderStatusMap.set(id, entry);
    });
    const shares = (shareStatus && shareStatus.shares) || [];
    shares.forEach(s => {
        const id = s.tag_id;
        const entry = folderStatusMap.get(id) || {};
        entry.shared = true;
        // ShareInfo DTO (share_types.go): Status is "active" | "paused" | "invalid"; Followers is int.
        entry.sharePaused = s.status === 'paused';
        entry.shareFollowers = typeof s.followers === 'number' ? s.followers : undefined;
        folderStatusMap.set(id, entry);
    });
    updateFolderBadgesInPlace();
}

// Expose to other modules via window — simpler than ES imports for this codebase.
window.folderStatusMap = folderStatusMap;
window.updateFolderBadgesInPlace = updateFolderBadgesInPlace;
window.applyFolderStatusUpdate = applyFolderStatusUpdate;

// Re-render the folder cards in place (rename, status changes). loadClips does
// not use this: it builds the cards before clearing the gallery, so a reload
// never flashes an empty frame.
async function renderFolderCards() {
    const myGen = ++_folderRenderGen;
    const cards = await buildFolderCards(() => myGen !== _folderRenderGen);
    if (!cards) return;

    gallery.querySelectorAll(':scope > [data-folder]').forEach(card => card.remove());
    // Folders lead the gallery, ahead of the clip cards.
    const firstClip = gallery.querySelector(':scope > li[data-id]');
    for (const card of cards) gallery.insertBefore(card, firstClip);
    if (window.__galleryRover) window.__galleryRover.update();
}

// Build (but do not insert) the folder cards for the folder being viewed.
// Resolves to null when isStale() reports the render was superseded.
async function buildFolderCards(isStale = () => false) {
    let folderTags;
    if (activeTagFilters.length > 0) {
        const currentTagId = activeTagFilters[activeTagFilters.length - 1];
        folderTags = await getChildTags(currentTagId);
    } else {
        folderTags = await getTopLevelTags();
    }

    if (isStale()) return null;
    if (!folderTags || folderTags.length === 0) return [];

    const hidden = (typeof getHiddenTags === 'function') ? (getHiddenTags() || []) : [];
    // One call for every card's count, not one bridge round trip per card.
    const counts = await getDescendantClipCounts(folderTags.map(tag => tag.id), isViewingArchive);
    if (isStale()) return null;

    // Path relative to the folder being viewed: normally the leaf name, but a
    // tag whose intermediate parent was deleted (a/b/c listed under a, or p/q
    // at the root once p is gone) shows the rest of its path so it is not
    // mistaken for a/c or q.
    const viewedTag = activeTagFilters.length > 0
        ? allTags.find(t => t.id === activeTagFilters[activeTagFilters.length - 1])
        : null;

    const cards = [];
    for (const tag of folderTags) {
        const count = Number(counts[tag.id]) || 0;

        let shortName = tag.name;
        if (viewedTag) {
            shortName = tag.name.startsWith(viewedTag.name + '/')
                ? tag.name.substring(viewedTag.name.length + 1)
                : getShortTagName(tag.name);
        }
        const isHidden = hidden.includes(tag.id);
        const state = folderStatusMap.get(tag.id) || {};
        const countText = `${count} clip${count !== 1 ? 's' : ''}`;

        const card = document.createElement('li');
        card.className = 'bg-white rounded-md border border-stone-200 overflow-visible flex flex-col items-center justify-center p-6 cursor-pointer transition-all duration-150 hover:border-stone-300 hover:scale-[1.02] relative';
        card.setAttribute('data-testid', `folder-card-${shortName}`);
        card.setAttribute('data-folder', tag.id);
        card.setAttribute('data-folder-path', tag.name);
        // What the plain search filter matches a folder card against.
        card.dataset.folderName = shortName.toLowerCase();
        card.setAttribute('draggable', 'true');
        card.setAttribute('aria-grabbed', 'false');
        card.setAttribute('tabindex', '0');
        if (isHidden) card.setAttribute('data-hidden', 'true');
        card.setAttribute('aria-label', buildFolderAriaLabel(tag.name, countText, state, isHidden));

        const badgeBadges = [];
        if (state.served) badgeBadges.push(renderBadge('served', state));
        if (state.shared) badgeBadges.push(renderBadge('shared', state));
        const badgesHTML = `<div class="folder-status-badges absolute top-2 right-2 flex gap-1">${badgeBadges.join('')}</div>`;

        card.innerHTML = `
            ${badgesHTML}
            <svg class="w-10 h-10 mb-2" fill="none" viewBox="0 0 24 24" stroke-width="1" stroke="${safeTagColor(tag.color)}">
                <path stroke-linecap="round" stroke-linejoin="round" d="M2.25 12.75V12A2.25 2.25 0 0 1 4.5 9.75h15A2.25 2.25 0 0 1 21.75 12v.75m-8.69-6.44-2.12-2.12a1.5 1.5 0 0 0-1.06-.44H4.5A2.25 2.25 0 0 0 2.25 6v12a2.25 2.25 0 0 0 2.25 2.25h15A2.25 2.25 0 0 0 21.75 18V9a2.25 2.25 0 0 0-2.25-2.25h-5.379a1.5 1.5 0 0 1-1.06-.44Z" />
            </svg>
            <span class="text-xs font-medium text-stone-700">${escapeHTML(shortName)}</span>
            <span class="text-[10px] text-stone-400 mt-0.5">${countText}</span>
        `;

        card.addEventListener('click', () => {
            navigateToFolder(tag.id);
        });
        card.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                navigateToFolder(tag.id);
            }
        });

        if (typeof FolderContextMenu !== 'undefined') {
            FolderContextMenu.attach(card, tag);
        }

        cards.push(card);
    }
    return cards;
}

// Expose to other modules and for test hooks.
window.renderFolderCards = renderFolderCards;

// Bumped by every folder navigation. A navigation that has to wait for the
// tag list checks it afterwards: one that started meanwhile has won.
let _folderNavGen = 0;

async function navigateToFolder(tagId, { focusFirst = false, isHistoryNav = false } = {}) {
    const myNav = ++_folderNavGen;
    // Replace active filters with this tag's ancestors + this tag
    let tag = allTags.find(t => t.id === tagId);
    if (!tag && typeof loadTags === 'function') {
        // The tag may be newer than the cached list (created by a plugin, the
        // REST API or a just-finished upload): refresh once before giving up.
        // The filters are compared too, since not every way of moving
        // (a filter checkbox, leaving folder mode) comes through here.
        const filtersBefore = activeTagFilters.join(',');
        const folderModeBefore = typeof isFolderMode === 'function' ? isFolderMode() : null;
        await loadTags();
        if (myNav !== _folderNavGen || activeTagFilters.join(',') !== filtersBefore
            || (typeof isFolderMode === 'function' ? isFolderMode() : null) !== folderModeBefore) return;
        tag = allTags.find(t => t.id === tagId);
    }
    if (!tag) return;

    // A fresh navigation (folder click, filter change) invalidates the
    // forward history — just like a browser. Back/forward navigations
    // preserve it so the user can retrace their steps.
    if (!isHistoryNav) folderForwardStack.length = 0;

    // Build filter chain: all ancestors + this tag
    const ancestors = [];
    let current = tag.name;
    while (current) {
        const parentName = getParentTagName(current);
        if (parentName) {
            const parentTag = allTags.find(t => t.name === parentName);
            if (parentTag) ancestors.unshift(parentTag.id);
        }
        current = parentName;
    }
    ancestors.push(tagId);

    // Replace active filters
    activeTagFilters.length = 0;
    activeTagFilters.push(...ancestors);

    updateActiveTagsDisplay();
    renderTagFilterDropdown();
    loadClips({ focusFirst });
}

// --- Folder back/forward history (mouse navigation buttons) ---
//
// Browsing folders is the app's primary "navigation" axis, so the mouse
// back/forward buttons walk up and down the folder tree. `back` is always
// derivable from the current position (go to the parent), while `forward`
// needs a stack to remember which child the user backed out of.
let folderForwardStack = [];

// The deepest active folder filter is the folder currently being viewed,
// or null when at the folder-mode root (showing top-level folder cards).
function currentFolderTagId() {
    return activeTagFilters.length ? activeTagFilters[activeTagFilters.length - 1] : null;
}

function navigateToFolderRoot() {
    _folderNavGen++;
    activeTagFilters.length = 0;
    updateActiveTagsDisplay();
    renderTagFilterDropdown();
    loadClips();
}

// Go up one folder level. Returns true if it navigated.
function navigateFolderBack() {
    if (!(typeof isFolderMode === 'function' && isFolderMode())) return false;
    const currentId = currentFolderTagId();
    if (currentId == null) return false; // already at the root — nowhere to go up

    folderForwardStack.push(currentId);

    const tag = allTags.find(t => t.id === currentId);
    // Nearest surviving ancestor: a/b/c whose a/b was deleted goes up to a.
    const byName = Object.fromEntries(allTags.map(t => [t.name, t]));
    const parentName = tag ? nearestExistingAncestorName(tag.name, byName) : '';
    if (parentName) {
        const parentTag = allTags.find(t => t.name === parentName);
        if (parentTag) {
            navigateToFolder(parentTag.id, { isHistoryNav: true });
            return true;
        }
    }
    // Root-level folder: step out to the top-level folder view.
    navigateToFolderRoot();
    return true;
}

// Re-enter the folder most recently backed out of. Returns true if it navigated.
function navigateFolderForward() {
    if (!(typeof isFolderMode === 'function' && isFolderMode())) return false;
    while (folderForwardStack.length) {
        const targetId = folderForwardStack.pop();
        if (allTags.some(t => t.id === targetId)) {
            navigateToFolder(targetId, { isHistoryNav: true });
            return true;
        }
        // Tag was deleted while in the forward stack — skip it.
    }
    return false;
}

window.navigateFolderBack = navigateFolderBack;
window.navigateFolderForward = navigateFolderForward;
