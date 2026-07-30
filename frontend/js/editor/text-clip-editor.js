// --- Text Clip Editor ---
// Owns text-specific editing behavior: drafts, search/replace, wrapping, mode
// selection, editor status, explicit formatting, and the save snapshot.
// Persistence remains in the shared editor controller.
//
// The editing widget is CodeMirror, reached only through CodeEditorAdapter — this
// file never touches a CodeMirror API. Preview rendering goes through TextPreview
// and validation through TextDiagnostics, both dispatching by descriptor. Nothing
// here is Markdown- or JSON-specific: the mode defaults, the tab labels, the
// byte-safety screen, which validator runs, and whether a formatter exists at all
// are descriptor-driven.
//
// Save is the one flow that deliberately does not read live state. It captures an
// immutable snapshot and every later step — reclassification, validation,
// confirmation, encoding, upload — refers only to that.

const TextClipEditor = (() => {
    const DRAFT_PREFIX_V1 = 'mahpastes:text-editor-draft:v1:';
    const DRAFT_PREFIX_V2 = 'mahpastes:text-editor-draft:v2:';
    const WRAP_PREFERENCE_KEY = 'mahpastes:text-editor-wrap';
    const DRAFT_DELAY_MS = 250;

    // Detection for both, which is the only state a clip can open in. There is no
    // storage key here on purpose: the design says these choices do not persist
    // after close, and the surest way to honor that is to have nowhere to keep them.
    const TABLE_OPTION_DEFAULTS = { delimiterID: null, headerMode: 'auto' };

    // Format-neutral on purpose. The previous message named Markdown, which was
    // both wrong for every other format and misleading about the cause: the bytes,
    // not the renderer, are what make the clip unavailable.
    const UNAVAILABLE_REASONS = {
        'invalid-utf8': 'This clip is not valid UTF-8 text. Preview and editing are unavailable because displaying it would require replacing bytes that cannot be recovered afterwards.',
        'decode-failed': 'This clip’s stored data could not be decoded, so its exact bytes cannot be shown or preserved.',
    };

    let active = false;
    let clipID = null;
    let filename = '';
    let contentType = '';
    // The editor value as opened: BOM excluded, separators normalized to LF.
    let originalValue = '';
    // What is needed to put the bytes back the way they were found.
    let textProfile = null;
    let draftTimer = null;
    let initialized = false;
    let wrapEnabled = true;
    // The classification result. Drives the language mode, the default open mode,
    // the preview renderer, and which assistance is offered.
    let descriptor = null;
    // Non-null means neither Preview nor Edit is available: the byte-safety screen
    // is showing and Save/Save As stay disabled.
    let unavailableReason = null;
    // Above the enhanced-assistance threshold: plain preview and editing only.
    let degraded = false;
    let mode = 'edit';
    let editScrollTop = 0;
    let previewScrollTop = 0;
    let previewGeneration = 0;
    // Both default off, so an untouched panel matches exactly as it did before
    // these controls existed. Not persisted: they reset on each clip open.
    let searchCaseSensitive = false;
    let searchWholeWord = false;
    // Temporary CSV/TSV interpretation. Presentation only: it changes nothing in
    // the source and is deliberately not persisted anywhere, so reopening a clip
    // returns to detection. Reset on every open, which also matters because the
    // page outlives an individual clip.
    let tableOptions = { ...TABLE_OPTION_DEFAULTS };
    let adapter = null;
    // The authoritative errors of the *persisted* source, validated once per open
    // BEFORE draft recovery. See captureBaseline().
    let baselinePromise = null;
    let saveGeneration = 0;
    // Non-zero while a save is between activation and completion. Formatting is
    // locked out for that whole window: the spec says formatting never runs during
    // a save, and letting it run would advance the shared validation generation and
    // hand the save a stale, empty result — silently skipping Save Anyway.
    let savesInFlight = 0;
    // The byte length the clip was opened with. Kept because the assistance gate is
    // re-evaluated against the *current* value: pasting 3 MiB into a small clip has
    // to switch assistance off, and deleting it again has to switch it back on.
    let openByteLength = 0;
    // Bumped on every open. A clip ID alone is not identity: closing clip A and
    // reopening it is a NEW session with freshly loaded bytes, and a save captured
    // before the close must not be allowed to continue into it.
    let sessionToken = 0;

    function element(id) {
        return document.getElementById(id);
    }

    function mountPoint() {
        return element('text-editor-mount');
    }

    function previewPanel() {
        return element('editor-preview-panel');
    }

    function codec() {
        return window.MahpastesTextEditor.TextCodec;
    }

    function fileTypes() {
        return window.MahpastesTextEditor.TextFileTypes;
    }

    function languageModes() {
        return window.MahpastesTextEditor.LanguageModes;
    }

    function ensureAdapter() {
        if (!adapter) adapter = window.MahpastesTextEditor.createCodeEditorAdapter({ ariaLabel: 'Clip content' });
        return adapter;
    }

    function storageGet(key) {
        try {
            return localStorage.getItem(key);
        } catch (_) {
            return null;
        }
    }

    function storageSet(key, value) {
        try {
            localStorage.setItem(key, value);
            return true;
        } catch (_) {
            return false;
        }
    }

    function storageRemove(key) {
        try {
            localStorage.removeItem(key);
        } catch (_) {
            // Draft recovery is best-effort and must never block editing.
        }
    }

    function draftKeyV2() {
        return clipID === null ? null : `${DRAFT_PREFIX_V2}${clipID}`;
    }

    function draftKeyV1() {
        return clipID === null ? null : `${DRAFT_PREFIX_V1}${clipID}`;
    }

    function getValue() {
        return adapter && adapter.isMounted() ? adapter.getValue() : '';
    }

    function isDirty() {
        return active && !unavailableReason && getValue() !== originalValue;
    }

    function clearDraft() {
        // Both keys: a clip may still carry a v1 record that was never opened
        // since the migration landed, and a save must not leave it behind to be
        // "recovered" later.
        const v2 = draftKeyV2();
        const v1 = draftKeyV1();
        if (v2) storageRemove(v2);
        if (v1) storageRemove(v1);
        if (draftTimer) {
            clearTimeout(draftTimer);
            draftTimer = null;
        }
    }

    function persistDraft() {
        if (!active) return;
        const status = element('text-editor-draft-status');
        const key = draftKeyV2();
        if (!key || !isDirty()) {
            clearDraft();
            if (status) status.textContent = '';
            return;
        }

        const saved = storageSet(key, JSON.stringify({
            filename,
            contentType,
            originalText: originalValue,
            text: getValue(),
            // Carried so a recovered draft can be encoded back to bytes with the
            // same BOM and newline style as the clip it came from.
            profile: textProfile,
            updatedAt: Date.now(),
        }));
        if (status) status.textContent = saved ? 'Draft saved' : 'Draft unavailable';
    }

    function scheduleDraft() {
        if (draftTimer) clearTimeout(draftTimer);
        draftTimer = setTimeout(() => {
            draftTimer = null;
            persistDraft();
        }, DRAFT_DELAY_MS);
    }

    /**
     * One-time tolerant migration of a v1 draft record to v2.
     *
     * The trap this exists to avoid: a v1 record stored `originalText` exactly as
     * the backend handed it over, so a BOM'd document's v1 `originalText` begins
     * with U+FEFF and a CRLF document's contains CR LF. The v2 editor value has
     * neither. Recovery matches a draft to its clip by comparing those strings, so
     * without normalizing both sides here, every draft on a BOM'd or CRLF document
     * silently fails to match and is discarded.
     */
    function migrateDraftV1() {
        const v1Key = draftKeyV1();
        const v2Key = draftKeyV2();
        if (!v1Key || !v2Key) return;
        const raw = storageGet(v1Key);
        if (!raw) return;

        try {
            const draft = JSON.parse(raw);
            const { BOM, detectNewline, normalizeNewlines } = codec();
            const rawOriginal = typeof draft.originalText === 'string' ? draft.originalText : '';
            const rawText = typeof draft.text === 'string' ? draft.text : '';

            const bom = rawOriginal.startsWith(BOM);
            const strip = (value) => normalizeNewlines(value.startsWith(BOM) ? value.slice(BOM.length) : value);
            const migratedOriginal = strip(rawOriginal);
            const migratedText = strip(rawText);

            storageSet(v2Key, JSON.stringify({
                filename: draft.filename,
                contentType: draft.contentType,
                originalText: migratedOriginal,
                text: migratedText,
                profile: {
                    bom,
                    // Derived from the stored original, which still has its
                    // separators; the draft text may have been edited.
                    newline: detectNewline(rawOriginal),
                    finalNewline: migratedText.endsWith('\n'),
                },
                updatedAt: typeof draft.updatedAt === 'number' ? draft.updatedAt : Date.now(),
            }));
        } catch (_) {
            // A malformed v1 record is not recoverable; dropping it is correct.
        }
        storageRemove(v1Key);
    }

    function restoreDraft() {
        const key = draftKeyV2();
        if (!key) return false;

        if (!storageGet(key)) migrateDraftV1();

        const raw = storageGet(key);
        if (!raw) return false;

        try {
            const draft = JSON.parse(raw);
            const matchesClip = draft.filename === filename &&
                draft.contentType === contentType &&
                draft.originalText === originalValue;
            if (!matchesClip || typeof draft.text !== 'string' || draft.text === originalValue) {
                storageRemove(key);
                return false;
            }
            // Not undoable: recovering a draft is loading a document, not an edit
            // the user should be able to undo back out of.
            adapter.setValue(draft.text, { undoable: false });
            element('text-editor-draft-status').textContent = 'Recovered draft';
            return true;
        } catch (_) {
            storageRemove(key);
            return false;
        }
    }

    // --- descriptor-driven capabilities ---------------------------------------

    // Descriptor-driven rather than a bare content-type test, so a `.json` clip
    // stored as text/plain gets JSON assistance and a `.txt` clip the backend
    // sniffed as application/json keeps it.
    function formatterID() {
        return descriptor ? descriptor.formatter : null;
    }

    /**
     * Re-evaluates the >2 MiB gate against the current editor value.
     *
     * Fixing this at open time was wrong in both directions: pasting 3 MiB of text
     * into a small clip left highlighting, validation and table rendering enabled on
     * a document far past the threshold, and the reverse never recovered.
     *
     * Returns true when the flag changed, so the caller can re-apply the parts of the
     * UI that depend on it.
     */
    function refreshDegraded() {
        if (!active || unavailableReason) return false;
        const current = Math.max(openByteLength && !isDirty() ? openByteLength : 0, byteLengthOfValue());
        const next = current > codec().ENHANCED_ASSISTANCE_MAX_BYTES;
        if (next === degraded) return false;
        degraded = next;
        return true;
    }

    function byteLengthOfValue() {
        try {
            return new TextEncoder().encode(getValue()).length;
        } catch (_) {
            return getValue().length;
        }
    }

    // Formatting, validation, diagnostics, highlighting, and table rendering are
    // all disabled above the enhanced-assistance threshold.
    function assistanceEnabled() {
        return !unavailableReason && !degraded;
    }

    function defaultModeFor() {
        return descriptor && descriptor.defaultMode === 'preview' ? 'preview' : 'edit';
    }

    /**
     * Resolves the mode a clip opens in.
     *
     * The default is a property of the descriptor, not of the entry point:
     * Markdown, CSV, and TSV land in Preview because their Preview is a materially
     * different artifact; every source-preview format lands in Edit because its
     * Preview looks nearly identical to the editor. Two states override that — an
     * explicit Edit action, and a recovered draft, which is an edit in progress and
     * must never be hidden behind a Preview tab.
     */
    function resolveInitialMode(requested, recoveredDraft) {
        if (recoveredDraft) return 'edit';
        if (requested === 'preview' || requested === 'edit') return requested;
        return defaultModeFor();
    }

    // --- modes ----------------------------------------------------------------

    function updateTab(tab, selected) {
        tab.setAttribute('aria-selected', selected ? 'true' : 'false');
        tab.tabIndex = selected ? 0 : -1;
        tab.classList.toggle('bg-white', selected);
        tab.classList.toggle('text-stone-800', selected);
        tab.classList.toggle('text-stone-300', !selected);
    }

    function renderPreview() {
        previewGeneration += 1;
        // Renderer failures — a Markdown renderer exception, a highlighter that
        // threw, a token cap exceeded — are possible issues, merged into the same
        // drawer as the authoritative validator's findings.
        refreshDegraded();
        const rendererDiagnostics = TextPreview.render({
            descriptor,
            source: getValue(),
            wrap: wrapEnabled,
            degraded,
            generation: previewGeneration,
            tableOptions,
        });
        TextDiagnostics.setRendererDiagnostics(rendererDiagnostics);
        applyTableControls();
    }

    // --- table interpretation controls ----------------------------------------

    function isTablePreview() {
        return !!descriptor && descriptor.preview === 'table';
    }

    /**
     * Shows and labels the temporary CSV/TSV controls.
     *
     * They are Preview controls, so they appear with the Preview panel. The
     * "Detected" options are labelled with what detection actually chose, because a
     * bare "Detected" leaves the user unable to tell whether the table is wrong
     * because of the delimiter or because of the data.
     */
    function applyTableControls() {
        const bar = element('editor-table-controls');
        if (!bar) return;
        const visible = isTablePreview() && mode === 'preview' && assistanceEnabled();
        bar.classList.toggle('hidden', !visible);
        bar.classList.toggle('flex', visible);
        if (!visible) return;

        const interpretation = TextPreview.getInterpretation();
        element('editor-table-delimiter').value = tableOptions.delimiterID || '';
        element('editor-table-header').value = tableOptions.headerMode;

        const detectedDelimiter = element('editor-table-delimiter').querySelector('option[value=""]');
        const detectedHeader = element('editor-table-header').querySelector('option[value="auto"]');
        if (interpretation) {
            // Labelled from what *detection* chose, not from what is in force: an
            // auto option reading back the user's own override would look like
            // agreement, which is the one thing they came here to check.
            detectedDelimiter.textContent = interpretation.detectionInconclusive
                ? 'Detected (inconclusive)'
                : `Detected (${interpretation.detectedDelimiterLabel.toLowerCase()})`;
            detectedHeader.textContent = interpretation.headerDetected
                ? 'Detected (first row)'
                : 'Detected (no header)';
            const columns = interpretation.renderedColumns;
            const rows = interpretation.renderedRows;
            element('editor-table-summary').textContent =
                `${rows.toLocaleString()} ${rows === 1 ? 'row' : 'rows'} × ` +
                `${columns.toLocaleString()} ${columns === 1 ? 'column' : 'columns'}` +
                `${interpretation.header ? ' · header row' : ''}`;
        } else {
            detectedDelimiter.textContent = 'Detected';
            detectedHeader.textContent = 'Detected';
            element('editor-table-summary').textContent = '';
        }
    }

    /**
     * Applies a changed interpretation.
     *
     * Both the table and the drawer are repainted in one pass, because they are two
     * views of the same interpretation: a delimiter change that repainted only the
     * table would leave the drawer reporting ragged rows for a table that no longer
     * has them.
     */
    function setTableOptions(next) {
        tableOptions = { ...tableOptions, ...next };
        TextDiagnostics.setValidatorOptions(tableOptions);
        TextPreview.invalidate();
        if (mode === 'preview') renderPreview();
        else applyTableControls();
        TextDiagnostics.refresh(getValue());
    }

    function setMode(next, options = {}) {
        if (unavailableReason || (next !== 'preview' && next !== 'edit')) return;

        // Each mode keeps its own scroll position while the modal is open.
        if (mode === 'preview') previewScrollTop = previewPanel().scrollTop;
        else if (adapter && adapter.isMounted()) editScrollTop = adapter.getScrollTop();

        mode = next;
        const preview = mode === 'preview';
        updateTab(element('editor-preview-tab'), preview);
        updateTab(element('editor-edit-tab'), !preview);
        mountPoint().classList.toggle('hidden', preview);
        previewPanel().classList.toggle('hidden', !preview);
        element('editor-mode-label').textContent = preview ? 'Preview' : 'Edit';
        setFindPanelOpen(false, { silent: true });
        document.querySelectorAll('[data-editor-edit-only]').forEach((control) => {
            control.classList.toggle('hidden', preview);
        });
        // The blanket toggle above un-hides every edit-only control, so controls
        // with their own visibility rule — the format action, which only exists for
        // formats that have a formatter — have to reassert it afterwards.
        applyToolbarState();

        // Preview always renders the current unsaved editor value.
        if (preview) {
            renderPreview();
            // Validation reruns when entering Preview rather than waiting out the
            // editing debounce.
            TextDiagnostics.refresh(getValue());
        }
        requestAnimationFrame(() => {
            if (preview) {
                previewPanel().scrollTop = previewScrollTop;
                if (options.focus) previewPanel().focus();
            } else {
                // Measurements taken while hidden are for a zero-height box.
                adapter.refreshLayout();
                adapter.setScrollTop(editScrollTop);
                if (options.focus) adapter.focus();
            }
        });
    }

    function togglePreviewMode() {
        if (unavailableReason) return;
        setMode(mode === 'preview' ? 'edit' : 'preview', { focus: true });
    }

    // Called when an asynchronous Preview input resolves — a Markdown image
    // finishing its download, for instance — so the panel picks it up without the
    // user toggling modes.
    function refreshPreview() {
        if (unavailableReason || mode !== 'preview') return;
        TextPreview.invalidate();
        renderPreview();
    }

    // --- diagnostics ----------------------------------------------------------

    /**
     * Publishes the drawer's diagnostics as inline editor markers.
     *
     * Also refreshes the toolbar, because whether formatting is available depends
     * on whether authoritative errors currently exist.
     */
    function publishDiagnostics(items) {
        if (adapter && adapter.isMounted()) adapter.setDiagnostics(items);
        applyToolbarState();
    }

    /**
     * Moves the cursor and focus to a diagnostic's source range.
     *
     * Switches to Edit first when necessary: a diagnostic points at source, and
     * Preview has no cursor to place. The cursor is placed collapsed at the
     * reported position rather than selecting the range — the inline marker already
     * shows the extent, and landing the caret exactly on the reported line and
     * column is what makes a wrong position unit visible instead of plausible.
     */
    function revealDiagnostic(item) {
        if (unavailableReason || !adapter || !adapter.isMounted()) return;
        if (mode !== 'edit') setMode('edit');
        requestAnimationFrame(() => {
            let range;
            try {
                range = adapter.rangeForDiagnostic(item);
            } catch (_) {
                return;
            }
            adapter.selectRange({ from: range.from, to: range.from });
            adapter.revealRange(range);
            adapter.focus();
            updateCursorStatus();
        });
    }

    /**
     * Records the save-confirmation baseline.
     *
     * The baseline is the authoritative error set of the **persisted source** —
     * the bytes as stored — validated once per open **before** draft recovery.
     * Capturing it after recovery would defeat the entire policy: stored `{}` plus
     * a recovered draft of `{"x":}` would classify the user's own error as
     * pre-existing and let it silently overwrite valid stored bytes, which is
     * precisely the case the confirmation exists to catch.
     */
    function captureBaseline() {
        const source = originalValue;
        const openDescriptor = descriptor;
        const id = openDescriptor ? openDescriptor.id : null;

        if (!assistanceEnabled() || !openDescriptor || !openDescriptor.authoritative) {
            baselinePromise = Promise.resolve({ descriptorID: id, errors: [] });
            return;
        }
        baselinePromise = TextDiagnostics.validateCritical(source, openDescriptor, { apply: false })
            .then((result) => ({
                descriptorID: id,
                // An unavailable baseline establishes nothing, so it contributes no
                // pre-existing errors. That errs toward prompting rather than
                // toward silently overwriting, which is the safe direction.
                errors: result.diagnostics.filter((item) => item.severity === 'error'),
                // Recorded so the change comparison knows the baseline is partial.
                truncated: result.truncated === true,
                unavailable: result.unavailable || null,
            }));
    }

    function updateCursorStatus() {
        if (!adapter || !adapter.isMounted()) return;
        const { line, column } = adapter.getCursorPosition();
        element('text-editor-cursor-status').textContent = `Ln ${line}, Col ${column}`;
        const count = Array.from(getValue()).length;
        element('text-editor-character-status').textContent = `${count} ${count === 1 ? 'character' : 'characters'}`;
    }

    function updateSaveState() {
        if (!active) return;
        const saveButton = element('editor-save-in-place');
        if (saveButton) saveButton.disabled = !isDirty();
    }

    // --- search ---------------------------------------------------------------

    function findPanelOpen() {
        return !element('text-editor-find-panel').classList.contains('hidden');
    }

    function searchOptions() {
        return { caseSensitive: searchCaseSensitive, wholeWord: searchWholeWord };
    }

    function setSearchStatus() {
        const status = element('text-editor-search-status');
        if (!status) return;
        if (!element('text-editor-find').value) {
            status.textContent = '';
            return;
        }
        const { matches, activeIndex, truncated } = adapter.getSearchState();
        if (matches.length === 0) {
            status.textContent = 'No matches';
        } else {
            const total = truncated ? `${matches.length}+` : `${matches.length}`;
            status.textContent = `${Math.max(activeIndex, 0) + 1} of ${total}`;
        }
    }

    // Selects a specific match by index. Used where the previous implementation
    // jumped to the first match outright — CodeMirror's findNext searches forward
    // from the current selection, which is not the same thing.
    function selectMatchAt(index) {
        const { matches } = adapter.getSearchState();
        if (!matches.length) return false;
        const target = matches[(index + matches.length) % matches.length];
        adapter.selectRange(target);
        adapter.revealRange(target);
        adapter.refreshSearch();
        updateCursorStatus();
        return true;
    }

    function applyQuery() {
        const query = element('text-editor-find').value;
        if (!query) {
            adapter.clearSearch();
            return { matches: [], activeIndex: -1, truncated: false };
        }
        return adapter.setSearchQuery(query, searchOptions());
    }

    function refreshSearch(selectFirst) {
        if (!adapter || !adapter.isMounted()) return;
        const state = applyQuery();
        if (state.matches.length > 0 && (selectFirst || state.activeIndex === -1)) {
            selectMatchAt(0);
        }
        setSearchStatus();
    }

    function find(direction) {
        if (!element('text-editor-find').value) {
            setSearchStatus();
            return;
        }
        applyQuery();
        if (adapter.getSearchState().matches.length === 0) {
            setSearchStatus();
            return;
        }
        if (direction > 0) adapter.findNext();
        else adapter.findPrevious();
        updateCursorStatus();
        setSearchStatus();
    }

    function replaceCurrent() {
        const query = element('text-editor-find').value;
        if (!query) return;
        applyQuery();
        adapter.replaceCurrent(element('text-editor-replace').value);
        updateCursorStatus();
        setSearchStatus();
    }

    function replaceAll() {
        const query = element('text-editor-find').value;
        if (!query) return;
        applyQuery();
        adapter.replaceAll(element('text-editor-replace').value);
        updateCursorStatus();
        setSearchStatus();
    }

    function updateSearchToggle(id, pressed) {
        const button = element(id);
        if (!button) return;
        button.setAttribute('aria-pressed', pressed ? 'true' : 'false');
        button.classList.toggle('bg-stone-800', pressed);
        button.classList.toggle('text-white', pressed);
        button.classList.toggle('border-stone-800', pressed);
        button.classList.toggle('text-stone-600', !pressed);
    }

    function applySearchToggles() {
        updateSearchToggle('text-editor-match-case', searchCaseSensitive);
        updateSearchToggle('text-editor-whole-word', searchWholeWord);
    }

    function setFindPanelOpen(open, options = {}) {
        const panel = element('text-editor-find-panel');
        const toggle = element('text-editor-find-toggle');
        panel.classList.toggle('hidden', !open);
        panel.classList.toggle('flex', open);
        toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
        if (open) {
            element('text-editor-find').focus();
            element('text-editor-find').select();
            refreshSearch(true);
        } else {
            if (adapter && adapter.isMounted()) adapter.clearSearch();
            setSearchStatus();
            if (active && !unavailableReason && !options.silent) adapter.focus();
        }
    }

    // --- wrapping and formatting ---------------------------------------------

    function applyWrap() {
        const toggle = element('text-editor-wrap-toggle');
        if (adapter && adapter.isMounted()) adapter.setWrap(wrapEnabled);
        // A stable hook for CSS and tests now that there is no textarea `wrap`
        // attribute to read.
        mountPoint().dataset.wrap = wrapEnabled ? 'on' : 'off';
        toggle.setAttribute('aria-pressed', wrapEnabled ? 'true' : 'false');
        toggle.classList.toggle('bg-stone-700', wrapEnabled);
        toggle.classList.toggle('border-stone-500', wrapEnabled);
        toggle.classList.toggle('text-white', wrapEnabled);
    }

    function toggleWrap() {
        wrapEnabled = !wrapEnabled;
        applyWrap();
        storageSet(WRAP_PREFERENCE_KEY, wrapEnabled ? 'true' : 'false');
        // Source Preview honors the same persisted wrap preference as Edit.
        if (mode === 'preview') refreshPreview();
    }

    /**
     * Explicit formatting. Never runs during preview or save.
     *
     * Runs through the same executor and deadline as validation, so a pathological
     * document cannot stall the UI here either. The result is applied as a normal
     * undoable transaction, which is what marks the clip dirty, schedules the
     * draft, and reruns diagnostics through the ordinary change path.
     */
    async function formatDocument() {
        const target = formatterID();
        if (!target || !assistanceEnabled() || !adapter || !adapter.isMounted()) return;
        // A save in flight owns the validation generation until it resolves. Running
        // a format here would terminate the save's in-flight validation and let the
        // save proceed on an empty result, bypassing Save Anyway entirely.
        if (savesInFlight > 0) {
            showToast('Wait for the save to finish before formatting.');
            return;
        }
        if (TextDiagnostics.getAuthoritativeErrors().length) {
            showToast(`Fix the ${descriptor.label} errors before formatting.`);
            return;
        }

        const source = getValue();
        const result = await TextDiagnostics.format(source, target);
        if (!result || !result.ok) {
            // A depth refusal is a specific, actionable answer rather than a generic
            // failure: the document is fine, its indented form would just be enormous.
            const message = String((result && result.message) || '');
            const why = /nesting depth/.test(message) ? ' It is nested too deeply to indent.'
                : /formatted output/.test(message) ? ' Its indented form would be far larger than the file.'
                : '';
            showToast(`Could not format this ${descriptor.label} document.${why}`);
            return;
        }
        // The document may have moved on while the request was in flight; applying
        // a formatting of superseded source would silently discard the newer edit.
        if (!active || getValue() !== source) return;
        adapter.setValue(result.text, { undoable: true });
    }

    // --- lifecycle -----------------------------------------------------------

    /**
     * Applies the descriptor's language extension for the current gate state.
     *
     * The gate is re-evaluated rather than fixed at open, so this has to be able to
     * run mid-session: pasting past the enhanced-assistance threshold must remove
     * the language extension and leave plain editing, and deleting back under it
     * must restore it. Selected from the trusted classification result — there is no
     * automatic language detection anywhere on this path.
     */
    function applyLanguage() {
        if (!adapter || !adapter.isMounted()) return;
        adapter.setLanguage(assistanceEnabled() && descriptor ? descriptor.language : null);
    }

    function handleChange() {
        if (refreshDegraded()) {
            TextDiagnostics.setEnabled(assistanceEnabled());
            applyLanguage();
            applyToolbarState();
        }
        TextDiagnostics.schedule(getValue());
        updateSaveState();
        scheduleDraft();
        if (findPanelOpen()) refreshSearch(false);
        // Preview always renders the current unsaved value, so an edit made while
        // Preview is showing (through find/replace or formatting) must repaint it.
        if (mode === 'preview') refreshPreview();
    }

    function applyToolbarState() {
        const formatButton = element('text-editor-format-json');
        const formatter = formatterID();
        formatButton.classList.toggle('hidden', !formatter);
        if (formatter) formatButton.textContent = `Format ${descriptor.label}`;
        // Formatting never runs above the enhanced-assistance threshold, and is
        // disabled while authoritative syntax errors exist.
        const canFormat = !!formatter && assistanceEnabled() && savesInFlight === 0 &&
            TextDiagnostics.getAuthoritativeErrors().length === 0;
        formatButton.disabled = !canFormat;
        formatButton.classList.toggle('opacity-40', !canFormat);

        const notice = element('text-editor-degraded-notice');
        notice.classList.toggle('hidden', !degraded || !!unavailableReason);

        // Table rendering — and therefore its controls — is off above the
        // enhanced-assistance threshold along with everything else that parses.
        applyTableControls();
    }

    function showUnavailable() {
        const panel = element('editor-unavailable-panel');
        element('editor-unavailable-reason').textContent =
            UNAVAILABLE_REASONS[unavailableReason] || UNAVAILABLE_REASONS['decode-failed'];
        mountPoint().classList.add('hidden');
        previewPanel().classList.add('hidden');
        panel.classList.remove('hidden');

        const tabs = element('editor-mode-tabs');
        tabs.classList.add('hidden');
        tabs.classList.remove('flex');

        element('editor-mode-label').textContent = 'Unavailable';
        element('editor-save-in-place').disabled = true;
        element('editor-save').disabled = true;
        document.querySelectorAll('[data-editor-edit-only]').forEach((control) => {
            control.classList.add('hidden');
        });
        requestAnimationFrame(() => panel.focus());
    }

    function open(options) {
        active = true;
        clipID = options.clipID;
        filename = options.filename;
        contentType = options.contentType;
        textProfile = options.textProfile || codec().defaultProfile();
        sessionToken += 1;
        // A save cannot span a session boundary — snapshotStillTargets rejects one
        // that tries — so any lock still held belongs to a session that is gone.
        // Leaving it set would disable formatting for the rest of the page's life.
        savesInFlight = 0;
        descriptor = fileTypes().classify({ filename, contentType }).descriptor;
        unavailableReason = options.unavailable || null;
        openByteLength = options.byteLength || 0;
        degraded = openByteLength > codec().ENHANCED_ASSISTANCE_MAX_BYTES;
        mode = 'edit';
        editScrollTop = 0;
        previewScrollTop = 0;
        previewGeneration = 0;
        searchCaseSensitive = false;
        searchWholeWord = false;
        // Back to detection on every open, including reopening the same clip.
        tableOptions = { ...TABLE_OPTION_DEFAULTS };
        originalValue = options.text || '';
        TextPreview.clear();
        MarkdownPreview.open(clipID);

        const storedWrap = storageGet(WRAP_PREFERENCE_KEY);
        wrapEnabled = storedWrap === null ? true : storedWrap !== 'false';

        element('text-editor-find').value = '';
        element('text-editor-replace').value = '';
        element('text-editor-search-status').textContent = '';
        element('text-editor-draft-status').textContent = '';
        applySearchToggles();
        setFindPanelOpen(false, { silent: true });
        element('editor-unavailable-panel').classList.add('hidden');

        if (unavailableReason) {
            // Invalid UTF-8 is the only state that prevents both modes. No adapter
            // is mounted and no value is loaded, so there is nothing a save could
            // write back over the original bytes.
            originalValue = '';
            baselinePromise = Promise.resolve({ descriptorID: null, errors: [] });
            TextDiagnostics.close();
            applyWrap();
            applyToolbarState();
            showUnavailable();
            return;
        }

        ensureAdapter().mount({
            container: mountPoint(),
            value: originalValue,
            // Selected from the trusted classification result, never by automatic
            // language detection. Above the enhanced-assistance threshold the
            // language extension is left off entirely.
            language: assistanceEnabled() && descriptor ? descriptor.language : null,
            wrap: wrapEnabled,
            callbacks: {
                onChange: handleChange,
                onSelectionChange: updateCursorStatus,
                onFindRequested: () => setFindPanelOpen(true),
                // Activating an inline marker is one of the three things that
                // expands the drawer.
                onDiagnosticActivated: () => TextDiagnostics.expand(),
            },
        });

        applyWrap();
        TextDiagnostics.open({ descriptor, enabled: assistanceEnabled(), validatorOptions: tableOptions });
        applyToolbarState();

        // Order matters and is the whole point: the baseline is taken from the
        // persisted source before restoreDraft() replaces the editor value. A
        // recovered draft is an edit like any other and is measured against what
        // is on disk, not treated as the starting point.
        captureBaseline();
        const recoveredDraft = restoreDraft();
        if (recoveredDraft) TextDiagnostics.expectRecoveredDraft();
        // Deferred behind the in-flight baseline, then run: the editor's own state
        // is validated whether or not a draft was recovered.
        TextDiagnostics.schedule(getValue());
        updateCursorStatus();
        updateSaveState();

        const tabs = element('editor-mode-tabs');
        tabs.classList.remove('hidden');
        tabs.classList.add('flex');

        // The mode is decided here, once, from the descriptor default plus the
        // caller's request. Callers never simulate a tab click after opening.
        const initialMode = resolveInitialMode(options.initialMode, recoveredDraft);
        // setMode compares against the current mode to save the outgoing scroll
        // position, so the initial call must always run its side effects.
        mode = initialMode === 'preview' ? 'edit' : 'preview';
        setMode(initialMode);
        requestAnimationFrame(() => {
            if (initialMode === 'preview') {
                element('editor-preview-tab').focus();
            } else {
                adapter.refreshLayout();
                adapter.focus();
            }
        });
    }

    function close(options) {
        if (options?.discardDraft) clearDraft();
        if (draftTimer) {
            clearTimeout(draftTimer);
            draftTimer = null;
        }
        active = false;
        clipID = null;
        filename = '';
        contentType = '';
        originalValue = '';
        textProfile = null;
        descriptor = null;
        unavailableReason = null;
        degraded = false;
        mode = 'edit';
        // Both scroll positions reset on close, as does undo history.
        editScrollTop = 0;
        previewScrollTop = 0;
        baselinePromise = null;
        tableOptions = { ...TABLE_OPTION_DEFAULTS };
        element('editor-table-controls').classList.add('hidden');
        element('editor-table-controls').classList.remove('flex');
        element('editor-mode-tabs').classList.add('hidden');
        element('editor-mode-tabs').classList.remove('flex');
        element('editor-unavailable-panel').classList.add('hidden');
        element('text-editor-degraded-notice').classList.add('hidden');
        TextDiagnostics.close();
        TextPreview.clear();
        MarkdownPreview.close();
        // The parse cache holds one syntax tree for the document just closed. It is
        // keyed by exact source so it could never be misapplied, but there is no
        // reason to keep a tree for a clip nobody is looking at.
        languageModes().clearCache();
        setFindPanelOpen(false, { silent: true });
        if (adapter) adapter.destroy();
    }

    function getTextProfile() {
        return textProfile ? { ...textProfile } : null;
    }

    // --- save -----------------------------------------------------------------

    /**
     * Captures the immutable save snapshot.
     *
     * Save operates on this, never on live editor state. Without it the validation
     * window is a race: the user activates Save on `{}`, types `,` while
     * validation is pending, and generation N's approval is applied to generation
     * N+1's bytes.
     *
     * The target filename is reclassified here, so a Save As from `notes.txt` to
     * `notes.json` is validated as strict JSON before upload. Reclassification
     * controls validation and presentation only; the frontend does not rewrite the
     * stored content type.
     */
    function captureSnapshot(targetFilename) {
        const target = targetFilename || filename;
        return Object.freeze({
            // The clip identity is part of the snapshot, not read from live state at
            // write time. Without it a save whose validation is still pending can be
            // applied to whatever clip the editor has since moved to: edit clip A,
            // activate Save, close, open clip B, and A's bytes land on B.
            clipID,
            session: sessionToken,
            source: getValue(),
            filename: target,
            contentType,
            descriptor: fileTypes().classify({ filename: target, contentType }).descriptor,
            profile: Object.freeze(textProfile ? { ...textProfile } : codec().defaultProfile()),
            generation: ++saveGeneration,
        });
    }

    /**
     * Whether this snapshot may still be written.
     *
     * The editor must still be open on the same clip it was captured from. This is
     * the guard that makes the snapshot a save *target* and not just a byte buffer.
     */
    function snapshotStillTargets(snapshot) {
        return !!snapshot && active && snapshot.clipID === clipID && snapshot.session === sessionToken;
    }

    // Compared by normalized message plus start position: moving the same error to
    // a different line is a change, and so is a different message at the same place.
    function errorKey(item) {
        return `${item.message}@${item.line}:${item.column}`;
    }

    /**
     * Decides whether this save needs confirmation.
     *
     * Returns `null` to save without prompting, `'introduced'` when the document
     * was valid at open, or `'changed'` when it was already invalid and the error
     * set moved. A whole class of real files — Helm templates full of `{{ }}`,
     * JSON-with-comments saved as `.json`, XML fragments without a root — is
     * invalid by construction and never becomes valid; prompting on every save of
     * one trains the user to click through the dialog, which destroys its value for
     * the case it exists to catch.
     */
    function classifyErrorChange(before, after, truncated) {
        // A truncated set is not a complete description of the document's errors, so
        // two truncated runs sharing the same first N findings prove nothing about
        // what changed after them. Confirming is the safe direction: the alternative
        // is silently overwriting stored bytes because the difference happened to sit
        // past the cap.
        if (truncated) return after.length ? 'changed' : null;
        if (!after.length) return null;
        if (!before.length) return 'introduced';
        const a = before.map(errorKey).sort();
        const b = after.map(errorKey).sort();
        if (a.length === b.length && a.every((key, i) => key === b[i])) return null;
        return 'changed';
    }

    async function baselineErrorsFor(targetDescriptor) {
        const baseline = await baselinePromise;
        // Reclassification changes the grammar the document is judged by, so a
        // baseline taken under a different descriptor says nothing about it: a Save
        // As from notes.txt to notes.json is *introducing* JSON errors, not
        // preserving pre-existing ones.
        if (!baseline || !targetDescriptor || baseline.descriptorID !== targetDescriptor.id) return [];
        return baseline.errors;
    }

    // True when the open-time baseline itself hit the collection cap, so it is not a
    // complete description of the stored document's errors.
    async function baselineTruncatedFor(targetDescriptor) {
        const baseline = await baselinePromise;
        if (!baseline || !targetDescriptor || baseline.descriptorID !== targetDescriptor.id) return false;
        return baseline.truncated === true;
    }

    function confirmSaveAnyway(targetDescriptor, errors, change) {
        const count = errors.length;
        const noun = count === 1 ? 'error' : 'errors';
        const message = change === 'introduced'
            ? `This edit introduced ${count} ${targetDescriptor.label} ${noun}. Save it anyway?`
            : `${count} ${targetDescriptor.label} ${noun} changed. Save it anyway?`;
        return new Promise((resolve) => {
            showConfirmDialog(
                `Save invalid ${targetDescriptor.label}?`,
                message,
                () => resolve(true),
                () => resolve(false),
                { variant: 'primary', confirmLabel: 'Save Anyway' },
            );
        });
    }

    /**
     * Prepares a save. Resolves to the snapshot to write, or null if cancelled.
     *
     * Possible issues are ignored for confirmation purposes, and a timeout or
     * resource limit establishes no authoritative error — so the save proceeds
     * with the nonblocking notice rather than claiming the document is valid.
     */
    async function prepareSave(options = {}) {
        const snapshot = captureSnapshot(options.targetFilename);
        const target = snapshot.descriptor;
        // Held for the whole prepare window, so formatting cannot steal the
        // validation generation out from under the save.
        const lockSession = sessionToken;
        savesInFlight += 1;
        applyToolbarState();
        try {
            if (!assistanceEnabled() || !target || !target.authoritative) return snapshot;

            const baseline = await baselineErrorsFor(target);
            const applies = target === descriptor;
            const result = await TextDiagnostics.validateCritical(snapshot.source, target, { apply: applies });
            const errors = result.diagnostics.filter((item) => item.severity === 'error');

            // Awaiting validation and confirmation gave the user time to close this
            // clip and open another. Writing now would put these bytes on a clip the
            // user never edited.
            if (!snapshotStillTargets(snapshot)) return null;

            const baselineTruncated = await baselineTruncatedFor(target);
            const change = classifyErrorChange(baseline, errors, baselineTruncated || result.truncated === true);
            if (!change) return snapshot;

            // A save that triggers confirmation expands the drawer; one that proceeds
            // without confirmation leaves it exactly as the user left it. Expanding is
            // skipped when the drawer is showing a different descriptor's findings,
            // because it would open on diagnostics that are not the ones in question.
            // The drawer expands on a save that triggers confirmation — including a
            // reclassified Save As target, whose errors are exactly the ones being
            // asked about. Without showing them the dialog cites a count the user
            // cannot go and look at.
            if (applies) TextDiagnostics.expand();
            else TextDiagnostics.showForeign(result.diagnostics);
            const confirmed = await confirmSaveAnyway(target, errors, change);
            // Foreign findings are presentation only. Whatever the user chose, this
            // session's own diagnostics are re-established rather than left showing
            // another format's markers and status.
            if (!applies && active) TextDiagnostics.schedule(getValue());
            if (!confirmed) return null;
            // Re-checked after the dialog: it is modal, but it is also asynchronous.
            return snapshotStillTargets(snapshot) ? snapshot : null;
        } finally {
            // Only release the lock we actually took. A prepare that outlives its
            // session must not decrement the counter a *later* session's save is
            // relying on, or formatting unlocks mid-save there.
            if (lockSession === sessionToken) {
                savesInFlight = Math.max(0, savesInFlight - 1);
                if (active) applyToolbarState();
            }
        }
    }

    /**
     * Holds/releases the save lock across the *whole* save, persistence included.
     *
     * prepareSave's own lock ends when validation resolves, but the write is still in
     * flight after that — and the spec says formatting never runs during a save, not
     * merely during its validation.
     */
    function beginSave(snapshot) {
        // Scoped to the snapshot's session: a lock taken by a session that has since
        // been replaced must not be releasable against the current one, or a late
        // completion would unlock formatting in the middle of a *different* save.
        if (snapshot && snapshot.session !== sessionToken) return;
        savesInFlight += 1;
        if (active) applyToolbarState();
    }

    function endSave(snapshot) {
        if (snapshot && snapshot.session !== sessionToken) return;
        savesInFlight = Math.max(0, savesInFlight - 1);
        if (active) applyToolbarState();
    }

    function encodeSnapshot(snapshot) {
        return codec().encodeForSave(snapshot.source, snapshot.profile || codec().defaultProfile());
    }

    /**
     * Resets the open-time baseline to what was just written, so the next edit is
     * measured against what is now on disk.
     *
     * Returns true when the editor value moved on during the save — the user typed
     * while it was in flight. Those newer edits were deliberately not part of the
     * snapshot, so the caller must leave the editor open and dirty rather than
     * closing over them.
     */
    function commitSavedBaseline(snapshot) {
        // A snapshot from a previous session must not rebaseline, clear the draft of,
        // or close the clip that happens to be open now.
        if (!snapshotStillTargets(snapshot)) return false;
        originalValue = snapshot.source;
        const id = snapshot.descriptor ? snapshot.descriptor.id : null;
        baselinePromise = TextDiagnostics.validateCritical(snapshot.source, snapshot.descriptor, { apply: false })
            .then((result) => ({
                descriptorID: id,
                errors: result.diagnostics.filter((item) => item.severity === 'error'),
                // A stale or unavailable rebaseline establishes nothing. Recording it
                // as an empty error set would be a claim that the saved bytes are
                // clean, and the next save would then read a restored pre-existing
                // error as newly introduced. Marking it partial makes the comparison
                // confirm instead, which is the safe direction.
                truncated: result.truncated === true || result.stale === true || !!result.unavailable,
                unavailable: result.unavailable || null,
            }));
        const movedOn = getValue() !== snapshot.source;
        updateSaveState();
        return movedOn;
    }

    function setup() {
        // The diagnostics module needs its shell hooks re-supplied even on a repeat
        // call, so this runs before the one-time guard.
        TextDiagnostics.setup({ setMarkers: publishDiagnostics, reveal: revealDiagnostic });
        if (initialized) return;
        initialized = true;

        element('text-editor-find-toggle').addEventListener('click', () => {
            setFindPanelOpen(!findPanelOpen());
        });
        element('text-editor-find-close').addEventListener('click', () => setFindPanelOpen(false));
        element('text-editor-find-next').addEventListener('click', () => find(1));
        element('text-editor-find-previous').addEventListener('click', () => find(-1));
        element('text-editor-replace-current').addEventListener('click', replaceCurrent);
        element('text-editor-replace-all').addEventListener('click', replaceAll);
        element('text-editor-find').addEventListener('input', () => refreshSearch(true));
        element('text-editor-match-case').addEventListener('click', () => {
            searchCaseSensitive = !searchCaseSensitive;
            applySearchToggles();
            refreshSearch(true);
        });
        element('text-editor-whole-word').addEventListener('click', () => {
            searchWholeWord = !searchWholeWord;
            applySearchToggles();
            refreshSearch(true);
        });
        element('text-editor-wrap-toggle').addEventListener('click', toggleWrap);
        element('text-editor-format-json').addEventListener('click', formatDocument);
        element('editor-table-delimiter').addEventListener('change', (event) => {
            // '' is the auto option: back to detection, not a fifth delimiter.
            setTableOptions({ delimiterID: event.target.value || null });
        });
        element('editor-table-header').addEventListener('change', (event) => {
            setTableOptions({ headerMode: event.target.value });
        });
        element('editor-preview-tab').addEventListener('click', () => setMode('preview', { focus: true }));
        element('editor-edit-tab').addEventListener('click', () => setMode('edit', { focus: true }));
        element('editor-mode-tabs').addEventListener('keydown', event => {
            if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
            event.preventDefault();
            setMode(mode === 'preview' ? 'edit' : 'preview');
            element(mode === 'preview' ? 'editor-preview-tab' : 'editor-edit-tab').focus();
        });

        [element('text-editor-find'), element('text-editor-replace')].forEach(searchInput => {
            searchInput.addEventListener('keydown', event => {
                if (event.key === 'Escape') {
                    event.preventDefault();
                    event.stopImmediatePropagation();
                    setFindPanelOpen(false);
                } else if (event.key === 'Enter') {
                    event.preventDefault();
                    find(event.shiftKey ? -1 : 1);
                }
            });
        });

        window.addEventListener('beforeunload', () => {
            if (active && isDirty()) persistDraft();
        });
    }

    return {
        setup,
        open,
        close,
        getValue,
        getTextProfile,
        getMode: () => mode,
        getDescriptorID: () => (descriptor ? descriptor.id : null),
        // Disabling the buttons is not enough on its own: mod+s reaches
        // saveEditorContent directly, so the save paths ask as well.
        isUnavailable: () => !!unavailableReason,
        isDegraded: () => degraded,
        isDirty,
        clearDraft,
        // Save operates on an immutable snapshot, so the outer controller asks for
        // one and then refers only to it.
        prepareSave,
        beginSave,
        endSave,
        encodeSnapshot,
        commitSavedBaseline,
        // The outer controller re-checks this immediately before writing, so a clip
        // that was closed and replaced while the save was pending cannot be written.
        snapshotStillTargets,
        // True when the user typed while the save was in flight. Those edits were
        // deliberately excluded from the snapshot, so the caller keeps the editor
        // open and dirty rather than closing over bytes the user can still see.
        snapshotMovedOn: (snapshot) => snapshotStillTargets(snapshot) && getValue() !== snapshot.source,
        togglePreviewMode,
        refreshPreview,
    };
})();
