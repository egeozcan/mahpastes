// Open requests: files and mahpastes:// links the operating system asked this
// window to open.
//
// They come from the command line of this launch, from a second launch that
// handed its arguments to the running window instead of starting another one
// (Send To, a drop onto the exe or launcher icon, a file manager's Open With),
// and on macOS from Finder "Open With" or a drop on the Dock icon. The backend
// queues them (open_request.go) and emits `app:open-request` when there is
// something to take; this file takes the queue and handles it. Pulling rather
// than receiving a payload means a request that arrived while the page was
// still loading is picked up by the drain at startup, and that only one page
// imports each file when more than one is attached.

let openRequestDrain = null;
let openRequestDrainAgain = false;

// Takes and handles everything queued. Calls that arrive while a drain is
// running fold into one more pass after it, so a burst never runs two imports
// side by side.
async function drainOpenRequests() {
    if (openRequestDrain) {
        openRequestDrainAgain = true;
        return openRequestDrain;
    }
    openRequestDrain = (async () => {
        try {
            do {
                openRequestDrainAgain = false;
                let batch;
                try {
                    batch = await window.go.main.App.TakeOpenRequests();
                } catch (error) {
                    console.error('Failed to take open requests:', error);
                    return;
                }
                await handleOpenRequests(batch);
            } while (openRequestDrainAgain);
        } finally {
            openRequestDrain = null;
        }
    })();
    return openRequestDrain;
}

// Handles one batch: { files, skipped, urls }. Files were already checked by
// the backend (existing regular files outside the app's temp dir); they import
// through the same path as a pasted file path, so duplicate detection, the
// upload expiry and folder auto-tagging all apply.
async function handleOpenRequests(batch) {
    if (!batch) return;
    const files = batch.files || [];
    const skipped = batch.skipped || [];
    const urls = batch.urls || [];

    if (files.length > 0) {
        // New clips land in the active list; showing the archive would hide them.
        if (isViewingArchive && typeof toggleViewMode === 'function') {
            toggleViewMode();
        }
        await uploadFilesFromPaths(files);
    }

    if (skipped.length > 0) {
        const names = skipped.map(p => p.split(/[/\\]/).pop() || p);
        const shown = names.slice(0, 3).join(', ') + (names.length > 3 ? `, +${names.length - 3} more` : '');
        const noun = skipped.length === 1 ? 'item' : 'items';
        showToast(`Skipped ${skipped.length} ${noun} that ${skipped.length === 1 ? 'is' : 'are'} not a file: ${shown}`, 'error');
    }

    for (const url of urls) {
        handleOpenRequestUrl(url);
    }
}

// Acts on one mahpastes:// link. Links only ever navigate the window; they
// never import or change anything. No link actions are defined yet, so every
// link is reported as unsupported (the window has already been brought
// forward by the backend).
function handleOpenRequestUrl(url) {
    showToast(`Unsupported mahpastes link: ${url}`, 'error');
    return false;
}

// Called once the app has finished its initial load. Subscribes first, then
// drains, so a request queued in between is not missed.
function initOpenRequests() {
    if (window.mahpastesMode === 'server') return;
    if (!window.go || !window.go.main || !window.go.main.App || !window.go.main.App.TakeOpenRequests) return;
    if (window.runtime && window.runtime.EventsOn) {
        window.runtime.EventsOn('app:open-request', () => { drainOpenRequests(); });
    }
    drainOpenRequests();
}

Object.assign(window.__testHelpers || (window.__testHelpers = {}), {
    handleOpenRequests: (batch) => handleOpenRequests(batch),
    drainOpenRequests: () => drainOpenRequests(),
});
