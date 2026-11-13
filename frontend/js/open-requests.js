// Open requests: files and mahpastes:// links the operating system asked this
// window to open.
//
// They come from the command line of this launch, from a second launch that
// handed its arguments to the running window instead of starting another one
// (Send To, a drop onto the exe or launcher icon, a file manager's Open With,
// a mahpastes:// link, the "Save clipboard as clip" desktop action), and on
// macOS from Finder "Open With", a drop on the Dock icon or a link. The backend
// queues them (open_request.go) and emits `app:open-request` when there is
// something to take; this file takes the queue and handles it. Pulling rather
// than receiving a payload means a request that arrived while the page was
// still loading is picked up by the drain at startup, and that only one page
// imports each file when more than one is attached.

// Each batch is read into memory in full before it is uploaded, so a large
// Send To selection is imported a few files at a time (the same bound as a
// multi-path paste) rather than all at once.
const OPEN_REQUEST_IMPORT_BATCH = 32;

// A batch is also bounded by its bytes: every file in it is held as base64 on
// both sides of the bridge at once, several times its size. A file over this
// on its own is imported alone.
const OPEN_REQUEST_IMPORT_BATCH_BYTES = 256 * 1024 * 1024;

// Splits opened files into import batches of at most OPEN_REQUEST_IMPORT_BATCH
// files and OPEN_REQUEST_IMPORT_BATCH_BYTES bytes, keeping their order. sizes
// matches files index for index; a missing size counts as 0.
function openRequestBatches(files, sizes) {
    const batches = [];
    let batch = [];
    let bytes = 0;
    files.forEach((file, i) => {
        const size = Number(sizes && sizes[i]) || 0;
        if (batch.length > 0 && (batch.length >= OPEN_REQUEST_IMPORT_BATCH || bytes + size > OPEN_REQUEST_IMPORT_BATCH_BYTES)) {
            batches.push(batch);
            batch = [];
            bytes = 0;
        }
        batch.push(file);
        bytes += size;
    });
    if (batch.length > 0) batches.push(batch);
    return batches;
}

// Names the first few paths of a list for a toast.
function openRequestNames(paths) {
    const names = paths.map(p => p.split(/[/\\]/).pop() || p);
    return names.slice(0, 3).join(', ') + (names.length > 3 ? `, +${names.length - 3} more` : '');
}

// How much of an unsupported link the error toast shows.
const OPEN_REQUEST_LINK_SHOWN = 80;

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

// Handles one batch: { files, file_sizes, skipped, too_large, links,
// unsupported_links, paste_clipboard }. Files were already checked by the
// backend (existing, readable regular files outside the app's temp dir, small
// enough to store); they import through the same path as a pasted file path,
// so duplicate detection, the upload expiry and folder auto-tagging all apply.
async function handleOpenRequests(batch) {
    if (!batch) return;
    const files = batch.files || [];
    const skipped = batch.skipped || [];
    const tooLarge = batch.too_large || [];
    const links = batch.links || [];
    const unsupported = batch.unsupported_links || [];

    if (files.length > 0 || batch.paste_clipboard) {
        // New clips land in the active list; showing the archive would hide them.
        // Only from the gallery: with the lightbox or an editor open over the
        // archive, the upload's toast says where the clip went instead.
        if (typeof leaveArchiveForUpload === 'function') leaveArchiveForUpload();
    }

    for (const paths of openRequestBatches(files, batch.file_sizes)) {
        await uploadFilesFromPaths(paths);
    }

    if (batch.paste_clipboard) {
        await pasteSystemClipboard();
    }

    // One toast for both kinds: a second toast would replace the first.
    const notImported = [];
    if (skipped.length > 0) {
        const noun = skipped.length === 1 ? 'item' : 'items';
        notImported.push(`Skipped ${skipped.length} ${noun} that can't be imported: ${openRequestNames(skipped)}`);
    }
    if (tooLarge.length > 0) {
        // The limit is maxOpenedFileBytes in open_request.go: the most a clip can hold.
        const noun = tooLarge.length === 1 ? 'file' : 'files';
        notImported.push(`Skipped ${tooLarge.length} ${noun} too large to import (over 1 GB): ${openRequestNames(tooLarge)}`);
    }
    if (notImported.length > 0) {
        showToast(notImported.join('. '), 'error');
    }

    // Any web page can send a link, so a long one is shortened rather than
    // filling the toast (showToast sets text, never markup).
    for (const url of unsupported) {
        const shown = url.length > OPEN_REQUEST_LINK_SHOWN ? `${url.slice(0, OPEN_REQUEST_LINK_SHOWN)}…` : url;
        showToast(`Unsupported mahpastes link: ${shown}`, 'error');
    }

    // Only the last clip link is shown: each one would replace the previous
    // viewer anyway. "show" links need nothing here; the backend has already
    // brought the window forward.
    const clipLinks = links.filter(link => link && link.kind === 'clip' && link.clip_id > 0);
    if (clipLinks.length > 0) {
        await openClipFromLink(clipLinks[clipLinks.length - 1].clip_id);
    }
}

// Saves what is on the system clipboard as a clip, as if it had been pasted
// into the window: the "Save clipboard as clip" desktop action
// (--paste-clipboard). An image wins over text, matching a paste; text goes
// through handleText, so a copied file path still offers to import the file.
async function pasteSystemClipboard() {
    const app = window.go.main.App;
    try {
        const image = await app.GetClipboardImage();
        if (image && image.data) {
            const binary = atob(image.data);
            const bytes = new Uint8Array(binary.length);
            for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
            const type = image.content_type || 'image/png';
            await handlePastedFiles([new File([bytes], 'image.png', { type })]);
            return;
        }
    } catch (_) {
        // No image on the clipboard; try text.
    }
    let text = '';
    try {
        text = await app.GetClipboardText();
    } catch (error) {
        console.error('Failed to read the clipboard:', error);
    }
    if (text) {
        await handleText(text);
        return;
    }
    showToast('The clipboard is empty or holds nothing mahpastes can save.', 'error');
}

// Shows the clip a mahpastes://clip/<id> link names. Links only navigate:
// images and videos open in the lightbox, text in the editor, anything else is
// scrolled to and focused in the gallery. Nothing is ever opened in another
// application from a link, because any web page can fire one.
async function openClipFromLink(id) {
    let clip;
    try {
        clip = await window.go.main.App.GetClipPreview(id);
    } catch (error) {
        clip = null;
    }
    if (!clip) {
        showToast(`Clip ${id} was not found.`, 'error');
        return;
    }
    const contentType = clip.content_type || '';
    const filename = clip.filename || '';

    const show = async () => {
        if (editorClipId) closeEditor({ force: true, discardDraft: true });
        if (contentType.startsWith('image/') || contentType.startsWith('video/')) {
            // From the gallery when the clip is on screen, so the arrow keys
            // walk its neighbours as after a click.
            const visible = getVisibleMediaClips();
            if (visible.some(c => c.id === clip.id)) {
                await window.LightboxController.open({ clips: visible, currentId: clip.id, opener: document.activeElement });
            } else {
                await window.LightboxController.openSingle({ clip, opener: document.activeElement });
            }
            return;
        }
        window.LightboxController.close();
        if (isTextCandidate(filename, contentType)) {
            await openEditor(clip.id);
            return;
        }
        revealClipCard(clip);
    };

    if (hasUnsavedEditorChanges()) {
        showConfirmDialog(
            'Discard unsaved changes?',
            'Opening the linked clip will discard your unsaved changes.',
            show,
            null,
            { variant: 'danger', confirmLabel: 'Open Clip' }
        );
        return;
    }
    await show();
}

// Scrolls a clip's card into view and focuses it, for clips with no in-app
// viewer.
function revealClipCard(clip) {
    const card = gallery.querySelector(`li[data-id="${Number(clip.id)}"]`);
    if (!card || card.style.display === 'none' || card.getClientRects().length === 0) {
        const label = clip.filename ? `"${clip.filename}"` : `${clip.id}`;
        const where = clip.is_archived ? 'It is in the archive.' : 'It is not in the current view.';
        showToast(`Clip ${label} has no viewer here. ${where}`);
        return;
    }
    const rover = window.__galleryRover;
    if (rover) {
        const index = rover.getItems().indexOf(card);
        if (index >= 0) rover.setActiveIndex(index);
    }
    if (!card.hasAttribute('tabindex')) card.setAttribute('tabindex', '-1');
    card.scrollIntoView({ block: 'center' });
    card.focus({ preventScroll: true });
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
    openRequestBatches: (files, sizes) => openRequestBatches(files, sizes),
    openClipFromLink: (id) => openClipFromLink(id),
    drainOpenRequests: () => drainOpenRequests(),
});
