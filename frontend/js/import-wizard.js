// --- Import Folder Wizard ---
//
// Walks a folder one file at a time, records a decision per file, and executes
// the whole reviewed plan in a single backend call.
//
// The central rule: NOTHING happens on disk or in the library while the user is
// walking. Every button in the review pane only writes to `state.decisions`.
// The backend enforces the same thing structurally — there is no bound method
// that imports or deletes one file, so a half-executed plan is not
// representable. Keep it that way.

const ImportWizard = (() => {
    const modal = document.getElementById('import-wizard-modal');
    const stepLabel = document.getElementById('import-step-label');
    const closeBtn = document.getElementById('import-close');
    const footer = document.getElementById('import-footer');

    const setupPane = document.getElementById('import-setup-pane');
    const rootPathEl = document.getElementById('import-root-path');
    const recursiveToggle = document.getElementById('import-recursive');
    const scanSummaryEl = document.getElementById('import-scan-summary');
    const truncatedNote = document.getElementById('import-truncated-note');
    const trashWarning = document.getElementById('import-trash-warning');

    const reviewPane = document.getElementById('import-review-pane');
    const positionEl = document.getElementById('import-position');
    const decidedCountEl = document.getElementById('import-decided-count');
    const progressBar = document.getElementById('import-progress-bar');
    const previewEl = document.getElementById('import-preview');
    const detailsEl = document.getElementById('import-file-details');
    const exifEl = document.getElementById('import-exif');
    const duplicatesEl = document.getElementById('import-duplicates');
    const fileErrorEl = document.getElementById('import-file-error');
    const tagInput = document.getElementById('import-tag-input');

    const summaryPane = document.getElementById('import-summary-pane');
    const summaryCountsEl = document.getElementById('import-summary-counts');
    const summaryListEl = document.getElementById('import-summary-list');

    const ACTIONS = {
        skip: { label: 'Skip', badge: 'bg-stone-100 text-stone-500' },
        import: { label: 'Import', badge: 'bg-stone-800 text-white' },
        delete: { label: 'Delete', badge: 'bg-red-100 text-red-700' },
        import_delete: { label: 'Import + Delete', badge: 'bg-amber-100 text-amber-700' },
    };

    // Each cached inspection can carry megabytes of base64 preview, so the
    // cache is bounded. A 3000-photo folder would otherwise be an OOM.
    const INSPECT_CACHE_LIMIT = 15;
    const SUMMARY_CHUNK = 200;

    let lastFocusedElementBeforeImport = null;
    // True once the user has typed into the tag field for the CURRENT file.
    // Reset on every repaint. This is what separates "the user chose this tag"
    // from "this tag is the subfolder pre-fill", which is the distinction
    // repeatLast() depends on.
    let tagDirty = false;
    // Both counters are module-level and never reset. They used to live in
    // `state`, which reset() replaces wholesale — so after closing and
    // reopening, an in-flight inspection from the old folder could carry the
    // same token as the new folder's first render and paint over it.
    // Monotonic here, so a stale result can never collide with a live one.
    let inspectToken = 0;
    let sessionEpoch = 0;
    let importFocusTrapCleanup = null;
    let tagAutocomplete = null;

    const state = freshState();

    function freshState() {
        return {
            root: '',
            recursive: false,
            trashRecoverable: true,
            truncated: false,
            skipped: null,
            entries: [],
            decisions: new Map(),
            index: 0,
            last: null,
            inspectCache: new Map(),
            pane: 'setup',
            applying: false,
            applied: false,
            results: null,
            resultsByPath: null,
        };
    }

    function reset() {
        Object.assign(state, freshState());
        inFlight.clear();
        // Drop queued work for the session being torn down, settling each one
        // so nothing is left awaiting it. Anything already running finishes and
        // is discarded by the epoch check in inspectUncached.
        const abandoned = inspectQueue.splice(0, inspectQueue.length);
        for (const job of abandoned) job.cancel();
        // Start the next session on a clean chain. Any decision still in flight
        // is already neutered by its epoch check, so nothing is lost — but an
        // inherited pending chain would stall every new decision behind it.
        decisionChain = Promise.resolve();
    }

    // --- open / close -----------------------------------------------------
    // Adapted from maintenance.js, the one modal in this app that does focus
    // management correctly. watch.js's version has none; do not copy that one.

    function isOpen() {
        return modal && !modal.classList.contains('opacity-0');
    }

    function show() {
        lastFocusedElementBeforeImport = document.activeElement;
        modal.removeAttribute('inert');
        modal.classList.remove('opacity-0', 'pointer-events-none');
        modal.classList.add('opacity-100');
        modal.querySelector(':scope > div').classList.remove('scale-95');
        modal.querySelector(':scope > div').classList.add('scale-100');
        if (importFocusTrapCleanup) importFocusTrapCleanup();
        importFocusTrapCleanup = trapFocus(modal);
        modal.focus();
    }

    function close() {
        if (importFocusTrapCleanup) {
            importFocusTrapCleanup();
            importFocusTrapCleanup = null;
        }
        if (tagAutocomplete) {
            tagAutocomplete.destroy();
            tagAutocomplete = null;
        }
        modal.classList.add('opacity-0', 'pointer-events-none');
        modal.classList.remove('opacity-100');
        modal.querySelector(':scope > div').classList.add('scale-95');
        modal.querySelector(':scope > div').classList.remove('scale-100');
        modal.setAttribute('inert', '');

        // Drop the backend session so its path-taking methods fail closed again.
        window.go?.main?.App?.EndImportSession?.().catch(() => {});
        sessionEpoch++;
        reset();

        if (lastFocusedElementBeforeImport && lastFocusedElementBeforeImport !== document.body
            && !lastFocusedElementBeforeImport.closest('[inert]')) {
            lastFocusedElementBeforeImport.focus();
        } else {
            document.getElementById('drawer-toggle-btn')?.focus();
        }
        lastFocusedElementBeforeImport = null;
    }

    // requestClose is the Escape / X path. A wizard holds unapplied decisions,
    // so discarding has to be deliberate — but only when there is something to
    // discard.
    function requestClose() {
        if (state.applying) {
            showToast('Import in progress.');
            return;
        }
        if (state.applied || !hasPendingWork()) {
            close();
            return;
        }
        const n = pendingCount();
        showConfirmDialog(
            'Discard import plan?',
            `${n} file${n === 1 ? '' : 's'} ${n === 1 ? 'has' : 'have'} an action assigned. Nothing has been imported or deleted yet.`,
            () => close(),
            null,
            { variant: 'primary', confirmLabel: 'Discard' }
        );
    }

    function pendingCount() {
        let n = 0;
        for (const d of state.decisions.values()) {
            if (d.action !== 'skip') n++;
        }
        return n;
    }

    function hasPendingWork() {
        return pendingCount() > 0;
    }

    // --- entry points -----------------------------------------------------

    // Opens the native picker, then the wizard. Split from openWithScan on
    // purpose: Playwright cannot dismiss a native folder dialog, so the e2e
    // suite drives openImportWizard() below instead.
    async function openFromPicker() {
        try {
            const scan = await window.go.main.App.BeginImportSession(false);
            if (!scan) return; // user cancelled
            openWithScan(scan);
        } catch (err) {
            console.error('Failed to start folder import:', err);
            showToast('Could not open that folder.', 'error');
        }
    }

    // Scans an explicit path. This is the seam the tests use.
    async function openPath(path, opts = {}) {
        const scan = await window.go.main.App.StartImportSession(path, !!opts.recursive);
        openWithScan(scan);
    }

    function openWithScan(scan) {
        reset();
        applyScan(scan);
        state.pane = 'setup';
        show();
        attachAutocomplete();
        render();
    }

    function applyScan(scan) {
        // A rescan (the "include subfolders" toggle) replaces the backend
        // session, so anything still in flight belongs to a scan that no longer
        // exists. Bumping the epoch here rather than only in openWithScan keeps
        // a stale inspection from landing in the fresh cache — where it would
        // carry no review mark, and so silently opt its file out of the
        // changed-since-review check.
        sessionEpoch++;
        state.root = scan.root || '';
        state.recursive = !!scan.recursive;
        state.truncated = !!scan.truncated;
        state.skipped = scan.skipped || null;
        state.trashRecoverable = scan.trash_recoverable !== false;
        state.entries = Array.isArray(scan.entries) ? scan.entries : [];
        state.index = 0;
        state.inspectCache.clear();

        // Seed every entry now, not lazily. Two things fall out of that: the
        // safe action is the default, and the summary is complete and honest
        // even if the user jumps straight to it without walking a single file.
        state.decisions = new Map();
        for (const e of state.entries) {
            state.decisions.set(e.rel_path, { action: 'skip', tagName: e.dir || '', tagEdited: false });
        }
    }

    async function rescan() {
        try {
            const scan = await window.go.main.App.StartImportSession(state.root, !!recursiveToggle.checked);
            applyScan(scan);
            render();
        } catch (err) {
            console.error('Failed to rescan folder:', err);
            showToast('Could not rescan that folder.', 'error');
        }
    }

    // --- decisions --------------------------------------------------------

    function currentEntry() {
        return state.entries[state.index] || null;
    }

    function decisionFor(relPath) {
        return state.decisions.get(relPath) || { action: 'skip', tagName: '', tagEdited: false };
    }

    function setDecision(relPath, next) {
        state.decisions.set(relPath, next);
        state.last = { ...next };
    }

    // fileIsBlocked reports that the app could not read this file. Offering to
    // delete it would be hostile, so only Skip is allowed — and this has to be
    // checked here, not just on the disabled buttons, because the i/d/b/r
    // shortcuts call chooseAction and repeatLast directly.
    function fileIsBlocked(entry) {
        const insp = state.inspectCache.get(entry.rel_path);
        return !!(insp && insp.error);
    }

    // An action may only be committed once the file has actually been read.
    // Before that the app does not yet know whether it is readable, so the
    // unreadable-file guard has nothing to go on — and pressing `d` in that
    // window would queue a permanent delete for a file nobody has seen.
    // Inspections are local and fast, and the next file is prefetched, so this
    // is normally already true by the time the pane paints.
    function inspectionReady(entry) {
        return state.inspectCache.has(entry.rel_path);
    }

    // Wait for the file to have been read, so the unreadable-file guard has
    // something to go on. The controls deliberately stay enabled meanwhile:
    // rejecting input until the read lands would silently swallow keystrokes
    // from anyone triaging quickly, which is a worse failure than the one this
    // guard exists to prevent. Inspections are local and the next file is
    // prefetched, so this almost always resolves immediately.
    async function ensureInspected(entry) {
        if (inspectionReady(entry)) return;
        try {
            await inspect(entry.rel_path);
        } catch (err) {
            console.error('Inspect failed:', err);
        }
        if (!inspectionReady(entry)) {
            state.inspectCache.set(entry.rel_path, {
                rel_path: entry.rel_path,
                name: entry.name,
                size: entry.size,
                content_type: entry.content_type,
                duplicates: [],
                error: 'This file could not be read.',
            });
        }
    }

    // Decisions are serialized. They became async when they started waiting for
    // the file to be read, and two keystrokes arriving inside that window would
    // both resolve currentEntry() to the same file: the second silently
    // overwrote the first, and every later key landed one file off. Anyone
    // triaging at speed would hit this constantly. Queueing means each
    // keystroke sees the index the previous one left behind.
    let decisionChain = Promise.resolve();
    function queueDecision(fn) {
        // Bind the queued work to the session it was requested in. A decision
        // that is still waiting on a file read when the user closes the wizard
        // must not land in the next one: press Delete on a slow file, close
        // (nothing is committed yet, so no discard prompt appears), then open a
        // folder that happens to contain the same relative path, and the stale
        // promise would commit a delete the user never made there.
        const epoch = sessionEpoch;
        decisionChain = decisionChain
            .then(() => {
                if (epoch !== sessionEpoch) return;
                return fn();
            })
            .catch(err => console.error('Import decision failed:', err));
        return decisionChain;
    }

    function chooseAction(action) {
        return queueDecision(() => applyChoice(action));
    }

    function repeatLast() {
        return queueDecision(applyRepeat);
    }

    async function applyChoice(action) {
        const entry = currentEntry();
        if (!entry) return;
        const epoch = sessionEpoch;
        const startIndex = state.index;
        await ensureInspected(entry);
        if (epoch !== sessionEpoch) return;
        if (fileIsBlocked(entry) && action !== 'skip') {
            showToast('This file could not be read — it can only be skipped.');
            render();
            return;
        }
        const current = decisionFor(entry.rel_path);
        // Read the tag straight off the input: the action buttons advance, so
        // this is the user's last chance to have set one for this file. The
        // field is pre-filled with the subfolder name and editable from the
        // moment the file is shown, which is why it is read rather than the
        // stored decision.
        const typed = tagInput && !tagInput.disabled ? tagInput.value.trim() : current.tagName;
        setDecision(entry.rel_path, {
            action,
            tagName: typed,
            tagEdited: current.tagEdited || tagDirty,
        });
        // An await means the user may have navigated on; only advance if we are
        // still on the file this decision was made for.
        if (state.index === startIndex) goNext(); else render();
    }

    // Copy the action always; copy the tag only when the user actually typed
    // one. A literal copy of the whole decision silently breaks recursive mode:
    // tag 2024/rome.jpg as "2024", hit Repeat five times, walk into 2025/, and
    // every file lands in "2024".
    async function applyRepeat() {
        const entry = currentEntry();
        if (!entry || !state.last) return;
        const epoch = sessionEpoch;
        const startIndex = state.index;
        await ensureInspected(entry);
        if (epoch !== sessionEpoch) return;
        if (fileIsBlocked(entry) && state.last.action !== 'skip') {
            showToast('This file could not be read — it can only be skipped.');
            render();
            return;
        }
        setDecision(entry.rel_path, {
            action: state.last.action,
            tagName: state.last.tagEdited ? state.last.tagName : (entry.dir || ''),
            tagEdited: state.last.tagEdited,
        });
        if (state.index === startIndex) goNext(); else render();
    }

    function repeatLabel() {
        if (!state.last) return 'Repeat last';
        const entry = currentEntry();
        const label = ACTIONS[state.last.action]?.label || state.last.action;
        if (state.last.action === 'skip' || state.last.action === 'delete') {
            return `Repeat: ${label}`;
        }
        const tag = state.last.tagEdited ? state.last.tagName : (entry?.dir || '');
        return tag ? `Repeat: ${label} → ${tag}` : `Repeat: ${label}`;
    }

    function goNext() {
        if (state.index < state.entries.length - 1) {
            state.index++;
            render();
        } else {
            showSummary();
        }
    }

    function goPrev() {
        if (state.index > 0) {
            state.index--;
            render();
        }
    }

    function showSummary() {
        state.pane = 'summary';
        render();
    }

    function jumpTo(relPath) {
        const i = state.entries.findIndex(e => e.rel_path === relPath);
        if (i < 0) return;
        state.index = i;
        state.pane = 'review';
        render();
    }

    // --- rendering --------------------------------------------------------

    function render() {
        setupPane.classList.toggle('hidden', state.pane !== 'setup');
        reviewPane.classList.toggle('hidden', state.pane !== 'review');
        summaryPane.classList.toggle('hidden', state.pane !== 'summary');

        if (state.pane === 'setup') renderSetup();
        else if (state.pane === 'review') renderReview();
        else renderSummary();

        renderFooter();
    }

    function renderSetup() {
        stepLabel.textContent = 'Step 1 of 3 · Choose';
        rootPathEl.textContent = state.root || 'No folder selected';
        recursiveToggle.checked = state.recursive;

        const n = state.entries.length;
        const parts = [`${n} file${n === 1 ? '' : 's'}`];
        const s = state.skipped || {};
        const reasons = [];
        if (s.dotted) reasons.push(`${s.dotted} hidden`);
        if (s.symlinks) reasons.push(`${s.symlinks} symlink${s.symlinks === 1 ? '' : 's'}`);
        if (s.non_regular) reasons.push(`${s.non_regular} not a regular file`);
        if (s.app_temp) reasons.push(`${s.app_temp} app temp`);
        if (s.unreadable) reasons.push(`${s.unreadable} unreadable`);
        const skippedTotal = reasons.length
            ? (s.dotted || 0) + (s.symlinks || 0) + (s.non_regular || 0) + (s.app_temp || 0) + (s.unreadable || 0)
            : 0;
        if (skippedTotal) parts.push(`${skippedTotal} skipped (${reasons.join(', ')})`);
        scanSummaryEl.textContent = n === 0
            ? 'No importable files found in this folder.'
            : parts.join(' · ');

        truncatedNote.classList.toggle('hidden', !state.truncated);
        if (state.truncated) {
            truncatedNote.textContent =
                `This folder holds more files than the wizard walks at once — showing the first ${state.entries.length}.`;
        }
        trashWarning.classList.toggle('hidden', state.trashRecoverable);
    }

    function renderReview() {
        const entry = currentEntry();
        if (!entry) { showSummary(); return; }

        stepLabel.textContent = 'Step 2 of 3 · Review';
        positionEl.textContent = `${state.index + 1} / ${state.entries.length}`;
        const decided = pendingCount();
        decidedCountEl.textContent = decided ? `${decided} with actions` : 'no actions yet';
        progressBar.style.width = `${((state.index + 1) / state.entries.length) * 100}%`;

        tagDirty = false;
        renderDetails(entry, null);
        renderActionButtons(entry, null);

        // Token guard: fast Next-clicking must never paint one file's EXIF
        // against another file's name.
        const token = ++inspectToken;
        const cached = state.inspectCache.get(entry.rel_path);
        if (cached) {
            paintInspection(entry, cached);
        } else {
            previewEl.innerHTML = '<span class="text-xs text-stone-400">Loading…</span>';
            inspect(entry.rel_path).then(insp => {
                if (token !== inspectToken) return;
                paintInspection(entry, insp);
            }).catch(err => {
                if (token !== inspectToken) return;
                console.error('Inspect failed:', err);
                // Cache a synthetic failure so the pane is not stuck with every
                // control disabled: the user must still be able to skip past a
                // file the backend refused to describe.
                const failed = {
                    rel_path: entry.rel_path,
                    name: entry.name,
                    size: entry.size,
                    content_type: entry.content_type,
                    duplicates: [],
                    error: String(err && err.message ? err.message : err),
                };
                state.inspectCache.set(entry.rel_path, failed);
                paintInspection(entry, failed);
            });
        }

        prefetchNext();
    }

    // In-flight inspections, keyed by path. ensureInspected, prefetchNext and
    // renderReview can all ask for the same file at once, and each request is a
    // full read and hash on the Go side — without this, holding down the arrow
    // key over a folder of large files fans out into duplicate concurrent
    // reads of the same bytes.
    const inFlight = new Map();

    // Each inspection is a full read and hash on the Go side. Holding down the
    // arrow key over a folder of large files would otherwise launch one per
    // file with nothing holding them back — dedup only helps when the paths
    // repeat. A small cap keeps the useful case (the current file and its
    // prefetch) instant while refusing to turn key-repeat into a disk storm.
    const MAX_CONCURRENT_INSPECTS = 4;
    let activeInspects = 0;
    const inspectQueue = [];

    function pumpInspectQueue() {
        while (activeInspects < MAX_CONCURRENT_INSPECTS && inspectQueue.length) {
            const job = inspectQueue.shift();
            activeInspects++;
            job.run().finally(() => {
                activeInspects--;
                pumpInspectQueue();
            });
        }
    }

    function scheduleInspect(relPath) {
        return new Promise((resolve, reject) => {
            inspectQueue.push({
                run: () => inspectUncached(relPath).then(resolve, reject),
                // Dropping a queued job on reset must still settle its promise.
                // A decision awaiting one would otherwise never resume, leaving
                // decisionChain pending forever — and since every later action
                // queues behind that chain, the wizard would silently stop
                // responding to input for the rest of the session.
                cancel: () => reject(new Error('import session closed')),
            });
            pumpInspectQueue();
        });
    }

    function inspect(relPath) {
        const existing = inFlight.get(relPath);
        if (existing) return existing;
        const p = scheduleInspect(relPath).finally(() => {
            if (inFlight.get(relPath) === p) inFlight.delete(relPath);
        });
        inFlight.set(relPath, p);
        return p;
    }

    async function inspectUncached(relPath) {
        const epoch = sessionEpoch;
        const insp = await window.go.main.App.ImportInspect(relPath);
        // reset() installs a fresh cache; without this an inspection started
        // for folder A could land in folder B's cache under the same relPath,
        // showing A's preview and duplicates for B's file.
        if (epoch !== sessionEpoch) return insp;
        if (state.inspectCache.size >= INSPECT_CACHE_LIMIT) {
            // Evict the whole oldest entry, not just its preview, so a revisit
            // also re-reads duplicate state.
            state.inspectCache.delete(state.inspectCache.keys().next().value);
        }
        state.inspectCache.set(relPath, insp);
        return insp;
    }

    function prefetchNext() {
        const next = state.entries[state.index + 1];
        if (!next || state.inspectCache.has(next.rel_path)) return;
        inspect(next.rel_path).catch(() => {});
    }

    function paintInspection(entry, insp) {
        renderPreview(insp);
        renderDetails(entry, insp);
        renderEXIF(insp);
        renderDuplicates(insp);
        renderActionButtons(entry, insp);
        // The footer gates Repeat on the same "has this been read yet" rule, so
        // it has to be refreshed here too — otherwise the button stays disabled
        // until some unrelated render happens to run.
        renderFooter();
    }

    function renderPreview(insp) {
        if (insp.error) {
            previewEl.innerHTML = '<span class="text-xs text-stone-400">No preview</span>';
            return;
        }
        const ct = insp.content_type || '';
        if (insp.preview_data && ct.startsWith('image/')) {
            const img = document.createElement('img');
            img.src = `data:${ct};base64,${insp.preview_data}`;
            img.alt = insp.name;
            img.className = 'max-h-56 max-w-full object-contain';
            previewEl.replaceChildren(img);
            return;
        }
        if (insp.preview_data) {
            const pre = document.createElement('pre');
            pre.className = 'text-[11px] text-stone-600 whitespace-pre-wrap break-all max-h-56 overflow-y-auto w-full';
            try {
                pre.textContent = decodeBase64Text(insp.preview_data);
            } catch {
                pre.textContent = '';
            }
            previewEl.replaceChildren(pre);
            return;
        }
        // Say why rather than showing a broken image.
        const why = insp.preview_omitted === 'too_large'
            ? `${formatFileSize(insp.size)} — preview skipped`
            : 'No preview for this file type';
        previewEl.innerHTML = `<div class="text-center px-3">
            <p class="text-xs font-medium text-stone-500">${escapeHTML(getFriendlyFileType(ct, insp.name))}</p>
            <p class="text-[11px] text-stone-400 mt-1">${escapeHTML(why)}</p>
        </div>`;
    }

    function decodeBase64Text(b64) {
        const binary = atob(b64);
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
        return new TextDecoder().decode(bytes);
    }

    function renderDetails(entry, insp) {
        const size = insp ? insp.size : entry.size;
        const ct = (insp && insp.content_type) || entry.content_type || '';
        const rows = [];
        if (entry.dir) {
            rows.push(['Folder', entry.dir]);
        }
        rows.push(['Name', entry.name]);
        rows.push(['Type', getFriendlyFileType(ct, entry.name)]);
        rows.push(['Size', formatFileSize(size)]);
        const modTime = (insp && insp.mod_time) || entry.mod_time;
        if (modTime) rows.push(['Modified', new Date(modTime).toLocaleString()]);

        detailsEl.innerHTML = rows.map(([k, v]) => `
            <div class="flex justify-between gap-3 text-xs">
                <span class="text-stone-400 shrink-0">${escapeHTML(k)}</span>
                <span class="font-medium text-stone-700 text-right break-all">${escapeHTML(String(v))}</span>
            </div>`).join('');

        const hasError = !!(insp && insp.error);
        fileErrorEl.classList.toggle('hidden', !hasError);
        if (hasError) {
            fileErrorEl.innerHTML = `<div class="text-[11px] text-red-700 bg-red-50 border border-red-200 rounded-md p-2">
                ${escapeHTML(insp.error)}
            </div>`;
        }
    }

    function renderEXIF(insp) {
        const exif = insp.exif;
        if (!exif) { exifEl.classList.add('hidden'); return; }
        const rows = [
            ['Camera', [exif.camera_make, exif.camera_model].filter(Boolean).join(' ')],
            ['Lens', exif.lens],
            ['ISO', exif.iso],
            ['Aperture', exif.aperture ? `f/${exif.aperture}` : ''],
            ['Shutter', exif.shutter_speed],
            ['Focal length', exif.focal_length ? `${exif.focal_length}mm` : ''],
            ['Taken', exif.date],
            ['GPS', exif.gps ? `${exif.gps.latitude.toFixed(5)}, ${exif.gps.longitude.toFixed(5)}` : ''],
        ].filter(([, v]) => v !== '' && v !== null && v !== undefined);

        if (!rows.length) { exifEl.classList.add('hidden'); return; }
        exifEl.classList.remove('hidden');
        exifEl.innerHTML = `
            <span class="block text-[10px] font-medium text-stone-400 uppercase tracking-wide mb-1">Metadata</span>
            ${rows.map(([k, v]) => `
                <div class="flex justify-between gap-3 text-xs">
                    <span class="text-stone-400 shrink-0">${escapeHTML(k)}</span>
                    <span class="font-medium text-stone-700 text-right break-all">${escapeHTML(String(v))}</span>
                </div>`).join('')}`;
    }

    function renderDuplicates(insp) {
        const dups = insp.duplicates || [];
        if (!dups.length) { duplicatesEl.classList.add('hidden'); return; }
        duplicatesEl.classList.remove('hidden');
        duplicatesEl.innerHTML = `
            <div class="text-[11px] text-amber-700 bg-amber-50 border border-amber-200 rounded-md p-2 space-y-1">
                <p class="font-medium">Already in mahpastes (${dups.length})</p>
                ${dups.map(d => `
                    <div data-testid="import-duplicate-row" class="flex justify-between gap-2">
                        <span class="break-all">${escapeHTML(d.filename || `clip ${d.clip_id}`)}${d.is_archived ? ' (archived)' : ''}</span>
                        <span class="shrink-0 opacity-70">${escapeHTML((d.tags || []).map(t => t.name).join(', '))}</span>
                    </div>`).join('')}
            </div>`;
    }

    // commitTagValue is the single place tagEdited is set to true.
    function commitTagValue(entry, value) {
        const current = decisionFor(entry.rel_path);
        state.decisions.set(entry.rel_path, { ...current, tagName: value, tagEdited: true });
        if (state.last) state.last = { ...state.last, tagName: value, tagEdited: true };
    }

    function renderActionButtons(entry, insp) {
        const decision = decisionFor(entry.rel_path);
        // Offering to delete a file the app could not even read is hostile, so
        // an unreadable file can only be skipped.
        const blocked = !!(insp && insp.error);

        reviewPane.querySelectorAll('.import-action-btn').forEach(btn => {
            const action = btn.dataset.importAction;
            const active = decision.action === action;
            btn.setAttribute('aria-pressed', active ? 'true' : 'false');
            btn.classList.toggle('bg-stone-800', active);
            btn.classList.toggle('text-white', active);
            btn.classList.toggle('border-stone-800', active);
            btn.disabled = blocked && action !== 'skip';
            btn.classList.toggle('opacity-40', btn.disabled);
            btn.classList.toggle('cursor-not-allowed', btn.disabled);
        });

        // This runs twice per file — once immediately, once when the inspection
        // resolves — so it must not clobber a tag the user typed in between.
        // tagDirty is reset by renderReview for each new file, so it can only
        // be true for the file currently on screen.
        if (!tagDirty) {
            tagInput.value = decision.tagName || '';
        }
        tagInput.disabled = blocked;
        tagInput.classList.toggle('opacity-50', tagInput.disabled);
    }

    function renderSummary() {
        stepLabel.textContent = 'Step 3 of 3 · Summary';

        let imports = 0, deletes = 0, skips = 0;
        for (const d of state.decisions.values()) {
            if (d.action === 'skip') skips++;
            else {
                if (d.action.includes('import')) imports++;
                if (d.action.includes('delete')) deletes++;
            }
        }
        summaryCountsEl.textContent =
            `${imports} to import · ${deletes} to delete · ${skips} skipped`;

        // Index the results once: at the 10k entry cap, a linear find per row
        // inside the rAF chunks would be quadratic.
        state.resultsByPath = state.results
            ? new Map(state.results.results.map(r => [r.rel_path, r]))
            : null;

        summaryListEl.replaceChildren();
        renderSummaryChunk(0);
    }

    // Chunked so a 10k-row summary does not blow the frame budget. Full
    // virtualization would be over-engineering at that cap.
    function renderSummaryChunk(start) {
        const frag = document.createDocumentFragment();
        const end = Math.min(start + SUMMARY_CHUNK, state.entries.length);
        for (let i = start; i < end; i++) {
            frag.appendChild(summaryRow(state.entries[i]));
        }
        summaryListEl.appendChild(frag);
        if (end < state.entries.length) {
            requestAnimationFrame(() => renderSummaryChunk(end));
        }
    }

    function summaryRow(entry) {
        const d = decisionFor(entry.rel_path);
        const meta = ACTIONS[d.action] || ACTIONS.skip;
        const li = document.createElement('li');

        const result = state.resultsByPath ? state.resultsByPath.get(entry.rel_path) : null;

        const btn = document.createElement('button');
        btn.type = 'button';
        btn.dataset.testid = 'import-summary-row';
        btn.dataset.relPath = entry.rel_path;
        btn.className = 'w-full text-left px-5 py-2.5 hover:bg-stone-50 transition-colors flex items-center gap-3';
        btn.innerHTML = `
            <span class="flex-1 min-w-0">
                <span class="block text-xs font-medium text-stone-700 break-all">${escapeHTML(entry.rel_path)}</span>
                <span class="block text-[10px] text-stone-400">${escapeHTML(formatFileSize(entry.size))}</span>
            </span>
            ${d.tagName && d.action.includes('import')
                ? `<span class="shrink-0 text-[10px] font-medium px-1.5 py-0.5 rounded bg-stone-100 text-stone-500">${escapeHTML(d.tagName)}</span>`
                : ''}
            <span data-testid="import-row-action"
                class="shrink-0 text-[10px] font-medium px-2 py-0.5 rounded ${meta.badge}">${escapeHTML(meta.label)}</span>
            ${result ? summaryStatusBadge(result) : ''}`;
        btn.addEventListener('click', () => {
            if (state.applying) return;
            jumpTo(entry.rel_path);
        });
        li.appendChild(btn);
        return li;
    }

    function summaryStatusBadge(result) {
        const ok = result.status === 'ok';
        const skipped = result.status === 'skipped';
        if (skipped) return '';
        const cls = ok ? 'bg-emerald-100 text-emerald-700' : 'bg-red-100 text-red-700';
        const text = ok ? 'done' : result.status.replace(/_/g, ' ');
        return `<span data-testid="import-row-status" title="${escapeHTML(result.error || '')}"
            class="shrink-0 text-[10px] font-medium px-2 py-0.5 rounded ${cls}">${escapeHTML(text)}</span>`;
    }

    // --- footer -----------------------------------------------------------

    function renderFooter() {
        footer.replaceChildren();
        if (state.pane === 'setup') {
            footer.appendChild(spacer());
            footer.appendChild(primaryBtn('Start review', () => {
                state.pane = 'review';
                state.index = 0;
                render();
            }, state.entries.length === 0, 'import-start'));
            return;
        }

        if (state.pane === 'review') {
            const left = document.createElement('div');
            left.className = 'flex gap-2';
            left.appendChild(secondaryBtn('← Prev', goPrev, state.index === 0, 'import-prev'));
            left.appendChild(secondaryBtn('Next →', goNext, false, 'import-next'));
            footer.appendChild(left);

            const right = document.createElement('div');
            right.className = 'flex gap-2';
            right.appendChild(secondaryBtn(repeatLabel(), repeatLast, !state.last, 'import-repeat'));
            right.appendChild(primaryBtn('Review all →', showSummary, false, 'import-goto-summary'));
            footer.appendChild(right);
            return;
        }

        // summary
        if (state.applied) {
            footer.appendChild(spacer());
            footer.appendChild(primaryBtn('Done', close, false, 'import-done'));
            return;
        }
        footer.appendChild(secondaryBtn('← Back', () => { state.pane = 'review'; render(); }, state.applying, 'import-back'));

        const right = document.createElement('div');
        right.className = 'flex gap-2 items-center';
        const counts = planCounts();
        const label = counts.imports || counts.deletes
            ? `Apply — import ${counts.imports}, delete ${counts.deletes}`
            : 'Apply';
        right.appendChild(primaryBtn(label, apply, state.applying || (!counts.imports && !counts.deletes), 'import-apply'));
        footer.appendChild(right);
    }

    function planCounts() {
        let imports = 0, deletes = 0;
        for (const d of state.decisions.values()) {
            if (d.action.includes('import')) imports++;
            if (d.action.includes('delete')) deletes++;
        }
        return { imports, deletes };
    }

    function spacer() {
        const el = document.createElement('span');
        return el;
    }

    function primaryBtn(label, onClick, disabled, testid) {
        const b = document.createElement('button');
        b.type = 'button';
        b.textContent = label;
        b.dataset.testid = testid;
        b.disabled = !!disabled;
        b.className = 'bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2 px-4 rounded-md transition-colors'
            + (disabled ? ' opacity-40 cursor-not-allowed' : '');
        b.addEventListener('click', onClick);
        return b;
    }

    function secondaryBtn(label, onClick, disabled, testid) {
        const b = document.createElement('button');
        b.type = 'button';
        b.textContent = label;
        b.dataset.testid = testid;
        b.disabled = !!disabled;
        b.className = 'border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md transition-colors'
            + (disabled ? ' opacity-40 cursor-not-allowed' : '');
        b.addEventListener('click', onClick);
        return b;
    }

    // --- apply ------------------------------------------------------------

    function apply() {
        if (state.applying || state.applied) return;
        const counts = planCounts();
        const message = counts.deletes
            ? `${counts.imports} file${counts.imports === 1 ? '' : 's'} will be imported and ${counts.deletes} `
              + `${state.trashRecoverable ? 'moved to the Trash' : 'permanently deleted'}.`
            : `${counts.imports} file${counts.imports === 1 ? '' : 's'} will be imported.`;

        showConfirmDialog('Apply import plan?', message, runApply, null, {
            variant: counts.deletes ? 'danger' : 'primary',
            confirmLabel: 'Apply',
        });
    }

    async function runApply() {
        // The import-before-delete ordering is NOT implemented here — it lives
        // in ImportApply on the Go side, where a JS exception or a closed modal
        // cannot break it. Do not refactor this into a per-file loop.
        const plan = state.entries.map(e => {
            const d = decisionFor(e.rel_path);
            return {
                rel_path: e.rel_path,
                action: d.action,
                tag_name: d.action.includes('import') ? (d.tagName || '') : '',
            };
        });

        state.applying = true;
        closeBtn.disabled = true;
        renderFooter();

        try {
            state.results = await window.go.main.App.ImportApply(plan);
            state.applied = true;
            state.pane = 'summary';
            render();

            const r = state.results;
            const parts = [];
            if (r.imported) parts.push(`${r.imported} imported`);
            if (r.trashed) parts.push(`${r.trashed} deleted`);
            if (r.failed) parts.push(`${r.failed} failed`);
            // Exactly one toast: #toast is a singleton with a shared timeout,
            // so one per file would just be a flicker.
            showToast(parts.length ? parts.join(', ') + '.' : 'Nothing to do.', r.failed ? 'error' : 'success');

            if (typeof loadClips === 'function') await loadClips();
            if (typeof loadTags === 'function') await loadTags();
        } catch (err) {
            console.error('Import apply failed:', err);
            showToast('Import failed.', 'error');
        } finally {
            state.applying = false;
            closeBtn.disabled = false;
            renderFooter();
        }
    }

    // --- wiring -----------------------------------------------------------

    if (modal) {
        // The drawer auto-closes on any id'd button click (app.js), so this
        // only has to open the wizard.
        document.getElementById('open-import-btn')?.addEventListener('click', openFromPicker);

        closeBtn?.addEventListener('click', requestClose);

        reviewPane?.addEventListener('click', (e) => {
            const btn = e.target.closest('.import-action-btn');
            if (!btn || btn.disabled) return;
            chooseAction(btn.dataset.importAction);
        });

        recursiveToggle?.addEventListener('change', () => {
            if (!state.root) return;
            if (hasPendingWork()) {
                showConfirmDialog(
                    'Rescan folder?',
                    'Changing this rescans the folder and discards the actions you have assigned.',
                    rescan,
                    () => { recursiveToggle.checked = state.recursive; },
                    { variant: 'primary', confirmLabel: 'Rescan' }
                );
                return;
            }
            rescan();
        });

        // tagEdited must mean "the user typed a tag", never "the field was
        // focused". Committing on blur alone would set it when someone clicks
        // into the pre-filled input and straight back out — and then Repeat
        // would carry that stale tag across subfolders, which is precisely the
        // failure this flag exists to prevent. So: only a real keystroke (the
        // `input` event) arms it, and blur/change only commit what was typed.
        tagInput?.addEventListener('input', () => { tagDirty = true; });

        const commitTag = () => {
            if (!tagDirty) return;
            const entry = currentEntry();
            if (!entry) return;
            commitTagValue(entry, tagInput.value.trim());
        };
        tagInput?.addEventListener('change', commitTag);
        tagInput?.addEventListener('blur', commitTag);
    }

    function attachAutocomplete() {
        if (tagAutocomplete || !tagInput || typeof TagAutocomplete === 'undefined') return;
        tagAutocomplete = TagAutocomplete.attach(tagInput, {
            onSelect: (value) => {
                tagInput.value = value;
                const entry = currentEntry();
                if (entry) commitTagValue(entry, value);
            },
        });
    }

    const api = {
        openFromPicker,
        openPath,
        openWithScan,
        requestClose,
        close,
        isOpen,
        // Keyboard entry points, driven from the shortcut registrations.
        chooseAction,
        repeatLast,
        goNext,
        goPrev,
        showSummary,
        // Decisions are queued and each may wait on a file read, so a caller
        // that acts immediately after a keystroke can observe the queue
        // mid-drain. Awaiting this settles it. Used by the e2e helpers, which
        // otherwise read the seeded defaults and see phantom "lost" keystrokes.
        settled: () => decisionChain,
        _state: state,
    };
    return api;
})();

window.ImportWizard = ImportWizard;

// Test seam: Playwright cannot dismiss a native folder dialog, so tests scan an
// explicit temp directory instead of clicking the drawer button.
window.__testHelpers = window.__testHelpers || {};
window.__testHelpers.openImportWizard = (path, opts = {}) => ImportWizard.openPath(path, opts);
window.__testHelpers.getImportDecisions = () => {
    const out = {};
    for (const [rel, d] of ImportWizard._state.decisions) out[rel] = d.action;
    return out;
};
window.__testHelpers.getImportTags = () => {
    const out = {};
    for (const [rel, d] of ImportWizard._state.decisions) out[rel] = d.tagName || '';
    return out;
};
