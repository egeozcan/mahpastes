// Pasted-path disambiguation.
//
// Copying a file in Finder/Explorer as a pathname, or dragging one into a
// terminal, puts plain text on the clipboard. Pasting that into mahpastes is
// ambiguous: the user may want the path itself as a text clip, or the file it
// points at. When the pasted text is path-shaped AND resolves to real files on
// this machine, ask instead of guessing. Everything else pastes as text as
// before — no prompt, no extra round trip.

// What to do when a paste turns out to name real files. Settings-controlled;
// cached here so a paste never waits on a settings round trip.
//   ask  — prompt (default)
//   text — always keep the path as a text clip, never touch the disk
//   file — always add the file the path points to
const PASTE_PATH_BEHAVIORS = ['ask', 'text', 'file'];
let pastePathBehavior = 'ask';

// The paste listener is live from page load, but the stored preference takes a
// round trip to arrive. A paste during startup must wait for it rather than
// silently fall back to `ask` — under `text` that would mean probing the
// filesystem the user asked us to leave alone. Callers await this.
let markPastePathBehaviorReady;
const pastePathBehaviorReady = new Promise(resolve => { markPastePathBehaviorReady = resolve; });

window.getPastePathBehavior = () => pastePathBehavior;

// Resolves to the *current* preference, not the one captured at load — the user
// may have changed it in Settings since.
window.whenPastePathBehaviorReady = async () => {
    await pastePathBehaviorReady;
    return pastePathBehavior;
};

window.loadPastePathBehavior = async () => {
    try {
        // Server mode names files on the viewer's machine, which the server
        // cannot see, so the preference is desktop-only — and the setting key is
        // not on the viewer-readable whitelist, so asking would only 403.
        if (window.mahpastesMode !== 'server') {
            const stored = await window.go.main.App.GetSetting('paste_path_behavior');
            if (PASTE_PATH_BEHAVIORS.includes(stored)) pastePathBehavior = stored;
        }
    } catch (error) {
        console.error('Failed to load pasted-path setting:', error);
    } finally {
        // Always settle: a paste blocked on this must never hang, even if the
        // lookup failed and we are staying on the default.
        markPastePathBehaviorReady();
    }
    return pastePathBehavior;
};

// Self-initializing so the preference does not depend on where it sits in
// app.js's startup sequence. This file is included before app.js, so this
// listener runs first — and window.go is available to both by then.
window.addEventListener('load', () => { window.loadPastePathBehavior(); });

window.setPastePathBehavior = async (behavior) => {
    if (!PASTE_PATH_BEHAVIORS.includes(behavior)) return;
    pastePathBehavior = behavior;
    try {
        await window.go.main.App.SetSetting('paste_path_behavior', behavior);
    } catch (error) {
        console.error('Failed to save pasted-path setting:', error);
    }
};

// Bounds on what is even worth probing: a paste larger than this, or with more
// lines than this, is prose, not a file reference.
const MAX_PATH_PASTE_CHARS = 4096;
const MAX_PATH_PASTE_LINES = 16;
const MAX_PATH_PASTE_LINE_CHARS = 1024;

// The forms a clipboard actually produces for an absolute location: a POSIX
// path, a ~-relative path (either separator, since Windows writes `~\`), a
// file:// URL, a Windows drive path, or a UNC path — each optionally wrapped in
// a quote. Relative paths are excluded on purpose: a desktop app has no working
// directory to resolve them against.
const PATH_LINE_RE = /^["']?(\/|~([\\/]|$)|file:\/\/|[A-Za-z]:[\\/]|\\\\)/;

/**
 * Extract path candidates from pasted text.
 * @param {string} text - the pasted plain text
 * @returns {string[]|null} candidate lines, or null if this is not path-shaped
 */
function pathCandidatesFromText(text) {
    if (typeof text !== 'string' || text.length === 0 || text.length > MAX_PATH_PASTE_CHARS) return null;

    const lines = text.split(/\r?\n/).map(line => line.trim()).filter(line => line !== '');
    if (lines.length === 0 || lines.length > MAX_PATH_PASTE_LINES) return null;

    // Every line must look like a path. A single stray line of prose means the
    // paste is a document that happens to mention a path, not a file reference.
    if (!lines.every(line => line.length <= MAX_PATH_PASTE_LINE_CHARS && PATH_LINE_RE.test(line))) return null;

    return lines;
}

/**
 * Resolve pasted text against the filesystem.
 * @param {string} text - the pasted plain text
 * @returns {Promise<Array<object>|null>} probes for existing files (one per
 *   pasted line, all present and non-directory), or null when the paste is not
 *   a usable file reference.
 */
async function resolvePastedFilePaths(text) {
    const candidates = pathCandidatesFromText(text);
    if (!candidates) return null;

    let probes;
    try {
        probes = await window.go.main.App.ProbeFilePaths(candidates);
    } catch (error) {
        // Probing is best-effort: a failure just means the paste stays text.
        console.error('Error probing pasted paths:', error);
        return null;
    }
    if (!Array.isArray(probes) || probes.length !== candidates.length) return null;

    // All-or-nothing. A mixed paste (some paths resolve, some do not) is not a
    // file reference the user can act on as a unit, so it stays text.
    //   is_regular — reading a FIFO or a character device would never finish.
    //   !is_temp   — this is a path we handed out ourselves via "Copy Path" or
    //                drag-out; importing it would duplicate an existing clip.
    if (!probes.every(p => p && p.exists && p.is_regular && !p.is_temp)) return null;

    return probes;
}

// --- Dialog ---

let pathPasteResolve = null;
let pathPasteFocusTrapCleanup = null;

/**
 * Ask whether a pasted path should become a text clip or the file it names.
 * @param {Array<object>} probes - resolved file probes
 * @returns {Promise<'file'|'text'|null>} null when the user cancels
 */
function showPathPasteDialog(probes) {
    const dialog = document.getElementById('path-paste-dialog');
    const dialogContent = dialog.querySelector('div');
    const messageEl = document.getElementById('path-paste-message');
    const fileList = document.getElementById('path-paste-file-list');
    const fileBtn = document.getElementById('path-paste-file-btn');

    // A second paste while the prompt is open takes over the one dialog there
    // is; the earlier paste is abandoned rather than left hanging forever.
    if (pathPasteResolve) {
        const stale = pathPasteResolve;
        pathPasteResolve = null;
        stale(null);
    }

    const plural = probes.length !== 1;
    messageEl.textContent = plural
        ? `That looks like ${probes.length} paths to files on this computer.`
        : 'That looks like a path to a file on this computer.';
    fileList.innerHTML = probes.map(p =>
        `<li class="flex gap-2 justify-between">
            <span class="truncate" title="${escapeHTML(p.path)}">${escapeHTML(p.name)}</span>
            <span class="text-stone-400 shrink-0">${escapeHTML(formatFileSize(p.size))}</span>
        </li>`
    ).join('');
    fileBtn.textContent = plural ? `Add ${probes.length} Files` : 'Add File';

    dialog.removeAttribute('inert');
    dialog.classList.remove('opacity-0', 'pointer-events-none');
    dialog.classList.add('opacity-100');
    dialogContent.classList.remove('scale-95');
    dialogContent.classList.add('scale-100');

    lastFocusedElement = document.activeElement;
    if (pathPasteFocusTrapCleanup) pathPasteFocusTrapCleanup();
    pathPasteFocusTrapCleanup = trapFocus(dialog);
    setTimeout(() => fileBtn.focus(), 100);

    return new Promise(resolve => { pathPasteResolve = resolve; });
}

function closePathPasteDialog(choice) {
    const dialog = document.getElementById('path-paste-dialog');
    // Called defensively from the global Escape handler and test teardown —
    // closing an already-closed dialog must not steal focus.
    if (!pathPasteResolve && dialog.hasAttribute('inert')) return;
    const dialogContent = dialog.querySelector('div');

    if (pathPasteFocusTrapCleanup) {
        pathPasteFocusTrapCleanup();
        pathPasteFocusTrapCleanup = null;
    }
    dialog.classList.remove('opacity-100');
    dialog.classList.add('opacity-0', 'pointer-events-none');
    dialogContent.classList.remove('scale-100');
    dialogContent.classList.add('scale-95');
    dialog.setAttribute('inert', '');

    if (lastFocusedElement) lastFocusedElement.focus();

    const resolve = pathPasteResolve;
    pathPasteResolve = null;
    if (resolve) resolve(choice);
}

(function initPathPasteDialog() {
    const dialog = document.getElementById('path-paste-dialog');
    if (!dialog) return;

    document.getElementById('path-paste-file-btn').addEventListener('click', () => closePathPasteDialog('file'));
    document.getElementById('path-paste-text-btn').addEventListener('click', () => closePathPasteDialog('text'));
    document.getElementById('path-paste-cancel-btn').addEventListener('click', () => closePathPasteDialog(null));

    dialog.addEventListener('click', e => {
        if (e.target === dialog) closePathPasteDialog(null);
    });
    // Escape is handled centrally by ShortcutManager's closeTopModalOverlay so
    // it works wherever focus happens to be.
})();

Object.assign(window.__testHelpers, {
    pathCandidatesFromText: (text) => pathCandidatesFromText(text),
    getPastePathBehavior: () => window.getPastePathBehavior(),
});
