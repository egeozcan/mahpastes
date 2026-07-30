// --- Text Diagnostics ---
// Owns validation scheduling, generations, the merge of authoritative validator
// results with non-authoritative possible issues, the presentation cap, and the
// collapsible bottom drawer.
//
// This lives in the classic-script layer rather than the bundle for the same
// reason TextPreview does: it drives app DOM. The validators it drives touch no
// DOM at all — they run in the static worker, or in the bounded time-sliced
// fallback when a surface cannot construct one, behind the single executor
// interface that makes the choice invisible here.
//
// Two rules this module exists to hold:
//
//   * A timeout, a resource limit, or a parser failure produces the
//     "Validation unavailable within safety limits" notice. It never claims the
//     document is valid and never triggers Save Anyway.
//   * Possible issues are counted, shown and navigable, but never confirm a save.

const TextDiagnostics = (() => {
    const DEBOUNCE_MS = 250;

    // The one validator id that is answered locally instead of in the executor.
    // HTML, CSS, JavaScript and TypeScript have no authoritative validator; what
    // they have is the Lezer error recovery already required for highlighting, and
    // those trees live in the editor bundle, not in the worker.
    const LEZER_VALIDATOR = 'lezer';

    let initialized = false;
    let active = false;
    let descriptor = null;
    // The >2 MiB gate: validation, diagnostics and formatting are all off above it.
    let enabled = false;
    // The shell's two hooks: publish inline markers, and reveal a source range.
    // Keeping them injected is what stops this module from needing CodeMirror.
    let shell = null;
    // Presentation options a validator needs in order to describe what the user is
    // looking at. Only the CSV/TSV delimiter override uses this today: findings
    // computed for a different delimiter than the rendered table would be worse
    // than no findings. Per-open state, so it is reset by open() and close().
    let validatorOptions = null;

    // Monotonic across opens, never reset: the executor rejects a request for a
    // generation it has already superseded, so reusing a number would be refused.
    let generation = 0;

    let validatorDiagnostics = [];
    let rendererDiagnostics = [];
    let truncatedCollection = false;
    let unavailable = null;
    let expanded = false;
    // Consumed by the first applied result. A recovered draft whose source has
    // authoritative errors opens the drawer once; after that, new errors never
    // expand it while the user types.
    let expandOnFirstErrors = false;

    let debounceTimer = null;
    let pendingSource = null;
    // While a baseline or save validation is in flight, background validation is
    // held back. Without this, the "newer generation terminates superseded work"
    // rule in the executor would let a keystroke kill the very validation a save
    // is waiting on.
    let criticalDepth = 0;

    function api() {
        return window.MahpastesTextEditor;
    }

    function element(id) {
        return document.getElementById(id);
    }

    function limits() {
        return api().DIAGNOSTIC_LIMITS;
    }

    function isError(item) {
        return item && item.severity === 'error';
    }

    function byteLengthOf(text) {
        try {
            return new TextEncoder().encode(text).length;
        } catch (_) {
            return text.length;
        }
    }

    // --- validation -----------------------------------------------------------

    /**
     * Runs one validation generation.
     *
     * Never throws: every failure mode is folded into `unavailable`, because the
     * one thing a failed validation must not do is read as "the document is valid".
     */
    async function runValidation(source, targetDescriptor) {
        const format = targetDescriptor && targetDescriptor.validator;
        const shared = api();
        const mine = ++generation;
        // No validator at all is a real answer: shell, .env, INI/CFG/CONF, plain
        // text and logs produce no diagnostics whatsoever.
        if (!format) return { generation: mine, diagnostics: [], truncated: false, unavailable: null };

        // Lezer recovery findings. Answered here rather than shipped to the worker
        // because the worker bundle carries no language packages, and it stays on
        // the ordinary debounced, generation-guarded path so a result can still go
        // stale exactly like a worker result. Every finding is a possible issue by
        // construction, so nothing here can reach the Save Anyway comparison.
        if (format === LEZER_VALIDATOR) {
            let items = [];
            try {
                items = shared.LanguageModes.possibleIssues(source, targetDescriptor.language) || [];
            } catch (err) {
                // A recovery pass that threw establishes nothing — and must not
                // become the safety-limit notice, which is a statement about an
                // *authoritative* validator failing to answer. This one never had an
                // authoritative answer to give.
                items = [];
            }
            return { generation: mine, diagnostics: items, truncated: false, unavailable: null };
        }

        try {
            const executor = await shared.getExecutor();
            const result = await executor.run({
                op: shared.OP_VALIDATE,
                generation: mine,
                payload: { format, source, options: validatorOptions },
                sourceBytes: byteLengthOf(source),
            });
            return {
                generation: mine,
                diagnostics: Array.isArray(result.diagnostics) ? result.diagnostics : [],
                truncated: !!result.truncated,
                unavailable: result.unavailable || null,
            };
        } catch (err) {
            // A superseded request reports `terminated` (worker) or `stale`
            // (fallback). Neither is a safety-limit notice: the answer was simply
            // no longer wanted, and a newer generation is already on its way.
            if (mine < generation) return { generation: mine, stale: true, diagnostics: [], unavailable: null };
            const code = err && err.code;
            return {
                generation: mine,
                diagnostics: [],
                truncated: false,
                unavailable: shared.isUnavailableCode(code) ? code : 'failed',
            };
        }
    }

    /**
     * A validation whose result a caller is waiting on — the open-time baseline
     * and the pre-save check. Background validation is suspended for its duration
     * and resumes afterwards.
     */
    async function validateCritical(source, targetDescriptor, options = {}) {
        criticalDepth++;
        cancelDebounce();
        try {
            const result = await runValidation(source, targetDescriptor);
            // The same guard the debounced path uses. `stale` only covers being
            // superseded by a *newer validation*; a close/open in between bumps the
            // generation without starting one, so the equality check is what stops a
            // previous session's result from painting this one.
            if (options.apply && active && !result.stale && result.generation === generation) {
                applyResult(result);
            }
            return result;
        } finally {
            criticalDepth--;
            flushPending();
        }
    }

    function cancelDebounce() {
        if (debounceTimer) {
            clearTimeout(debounceTimer);
            debounceTimer = null;
        }
    }

    function flushPending() {
        if (pendingSource === null || criticalDepth > 0 || !active) return;
        cancelDebounce();
        debounceTimer = setTimeout(runPending, DEBOUNCE_MS);
    }

    async function runPending() {
        debounceTimer = null;
        const source = pendingSource;
        pendingSource = null;
        if (source === null || !active) return;
        const result = await runValidation(source, descriptor);
        if (!active || result.stale || result.generation !== generation) return;
        applyResult(result);
    }

    function applyResult(result) {
        validatorDiagnostics = result.diagnostics;
        truncatedCollection = result.truncated;
        unavailable = result.unavailable;

        if (expandOnFirstErrors) {
            expandOnFirstErrors = false;
            // The one case where the drawer does not start collapsed: a recovered
            // draft whose source is already broken must not hide that fact.
            if (validatorDiagnostics.some(isError)) expanded = true;
        }
        render();
    }

    /**
     * Debounced revalidation while editing.
     */
    function schedule(source) {
        if (!active) return;
        if (!enabled || !descriptor || !descriptor.validator) {
            validatorDiagnostics = [];
            truncatedCollection = false;
            unavailable = null;
            render();
            return;
        }
        pendingSource = source;
        if (criticalDepth > 0) return;
        cancelDebounce();
        debounceTimer = setTimeout(runPending, DEBOUNCE_MS);
    }

    // Entering Preview reruns validation without waiting out the debounce.
    function refresh(source) {
        if (!active || !enabled || !descriptor || !descriptor.validator) return;
        pendingSource = source;
        if (criticalDepth > 0) return;
        cancelDebounce();
        runPending();
    }

    // --- merge and presentation ----------------------------------------------

    function merged() {
        const all = validatorDiagnostics.concat(rendererDiagnostics);
        // Position order, so activating rows top to bottom walks the document.
        return all.slice().sort((a, b) => (a.line - b.line) || (a.column - b.column));
    }

    function summaryText(items) {
        const errors = items.filter(isError).length;
        const possible = items.length - errors;
        const parts = [];
        if (errors) parts.push(`${errors} ${errors === 1 ? 'error' : 'errors'}`);
        if (possible) parts.push(`${possible} possible ${possible === 1 ? 'issue' : 'issues'}`);
        if (!parts.length) return unavailable ? api().UNAVAILABLE_NOTICE : 'No issues';
        return parts.join(', ');
    }

    function statusText(items) {
        if (!descriptor || !enabled) return { text: '', tone: 'neutral' };
        if (unavailable) return { text: api().UNAVAILABLE_NOTICE, tone: 'neutral' };
        if (!descriptor.validator || !descriptor.authoritative) return { text: '', tone: 'neutral' };

        const errors = items.filter(isError);
        if (!errors.length) return { text: `Valid ${descriptor.label}`, tone: 'valid' };
        if (errors.length === 1) {
            return { text: `Invalid ${descriptor.label} · Ln ${errors[0].line}, Col ${errors[0].column}`, tone: 'error' };
        }
        return { text: `Invalid ${descriptor.label} · ${errors.length} errors`, tone: 'error' };
    }

    function rowLabel(item) {
        const severity = isError(item) ? 'Error' : 'Possible issue';
        return `${severity} at line ${item.line}, column ${item.column}: ${item.message}`;
    }

    function buildRow(item, index) {
        const row = document.createElement('li');
        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'editor-diagnostic-row';
        button.dataset.severity = isError(item) ? 'error' : 'possible-issue';
        button.dataset.diagnosticIndex = String(index);
        // Severity, location and message all live in the accessible name.
        button.setAttribute('aria-label', rowLabel(item));

        const location = document.createElement('span');
        location.className = 'editor-diagnostic-location';
        location.textContent = `Ln ${item.line}, Col ${item.column}`;

        const message = document.createElement('span');
        message.className = 'editor-diagnostic-message';
        message.textContent = item.message;

        button.appendChild(location);
        button.appendChild(message);
        row.appendChild(button);
        return row;
    }

    let presented = [];

    function render() {
        const drawer = element('editor-diagnostics');
        if (!drawer) return;

        const items = merged();
        const cap = limits().presented;
        presented = items.slice(0, cap);

        drawer.classList.toggle('hidden', !active);
        drawer.dataset.expanded = expanded ? 'true' : 'false';
        element('editor-diagnostics-body').classList.toggle('hidden', !expanded);
        const toggle = element('editor-diagnostics-toggle');
        toggle.setAttribute('aria-expanded', expanded ? 'true' : 'false');
        const errorCount = items.filter(isError).length;
        toggle.dataset.severity = errorCount ? 'error' : (items.length ? 'possible-issue' : 'none');

        element('editor-diagnostics-summary').textContent = summaryText(items);

        const list = element('editor-diagnostics-list');
        list.replaceChildren(...presented.map(buildRow));
        element('editor-diagnostics-empty').classList.toggle('hidden', presented.length > 0);

        const notices = [];
        if (unavailable) {
            notices.push(`${api().UNAVAILABLE_NOTICE}. Saving remains available because no syntax error was established.`);
        }
        if (items.length > presented.length) {
            notices.push(`Showing the first ${cap} of ${items.length} diagnostics; ${items.length - presented.length} more are not listed.`);
        }
        if (truncatedCollection) {
            notices.push(`Collection stopped after ${limits().collected} findings.`);
        }
        const notice = element('editor-diagnostics-notice');
        notice.textContent = notices.join(' ');
        notice.classList.toggle('hidden', notices.length === 0);

        const status = element('text-editor-validation-status');
        if (status) {
            const { text, tone } = statusText(items);
            status.textContent = text;
            status.classList.toggle('text-red-400', tone === 'error');
            status.classList.toggle('text-emerald-400', tone === 'valid');
        }

        if (shell) shell.setMarkers(presented);
    }

    // --- drawer ---------------------------------------------------------------

    function setExpanded(next) {
        if (!active) return;
        expanded = !!next;
        render();
    }

    function activateRow(index) {
        const item = presented[index];
        if (!item || !shell) return;
        expanded = true;
        render();
        shell.reveal(item);
    }

    function setup(handlers) {
        shell = handlers;
        if (initialized) return;
        initialized = true;

        element('editor-diagnostics-toggle').addEventListener('click', () => setExpanded(!expanded));
        // Delegated: rows are rebuilt on every result.
        element('editor-diagnostics-list').addEventListener('click', (event) => {
            const button = event.target.closest('[data-diagnostic-index]');
            if (button) activateRow(Number(button.dataset.diagnosticIndex));
        });
    }

    // --- lifecycle ------------------------------------------------------------

    function open(options) {
        // Opening is a new generation, so a validation still in flight from the
        // previous open can never apply here. Without this, closing a JSON clip and
        // opening a .txt one — a path that starts no validation of its own, so
        // nothing supersedes the old request — lets the JSON errors and their inline
        // markers land on the plain-text document.
        generation += 1;
        active = true;
        descriptor = options.descriptor || null;
        enabled = options.enabled !== false;
        // The page is reused across tests and across clip opens, so per-open state
        // has to be re-established here rather than merely initialized once.
        validatorOptions = options.validatorOptions ? { ...options.validatorOptions } : null;
        validatorDiagnostics = [];
        rendererDiagnostics = [];
        truncatedCollection = false;
        unavailable = null;
        // Collapsed for ordinary opens.
        expanded = false;
        expandOnFirstErrors = options.recoveredDraft === true;
        pendingSource = null;
        cancelDebounce();
        render();
    }

    function close() {
        // Closing supersedes in-flight work too, so a result that resolves between
        // close and the next open is discarded rather than queued against it.
        generation += 1;
        active = false;
        descriptor = null;
        enabled = false;
        validatorOptions = null;
        validatorDiagnostics = [];
        rendererDiagnostics = [];
        presented = [];
        truncatedCollection = false;
        unavailable = null;
        expanded = false;
        expandOnFirstErrors = false;
        pendingSource = null;
        cancelDebounce();
        const drawer = element('editor-diagnostics');
        if (drawer) {
            drawer.classList.add('hidden');
            drawer.dataset.expanded = 'false';
            element('editor-diagnostics-body').classList.add('hidden');
            element('editor-diagnostics-toggle').setAttribute('aria-expanded', 'false');
            element('editor-diagnostics-list').replaceChildren();
        }
        const status = element('text-editor-validation-status');
        if (status) {
            status.textContent = '';
            status.classList.remove('text-red-400', 'text-emerald-400');
        }
    }

    /**
     * Arms the one exception to "starts collapsed".
     *
     * Called after draft recovery rather than passed to open(), because whether a
     * draft was recovered is only known once the baseline has already been taken
     * from the persisted source — and that ordering is not negotiable.
     */
    function expectRecoveredDraft() {
        if (active) expandOnFirstErrors = true;
    }

    /**
     * Replaces the presentation options the validator is given.
     *
     * Used by the temporary CSV/TSV delimiter control. Revalidation is the caller's
     * call, not an implicit side effect, because the same control also has to
     * repaint the table and both should happen in one pass.
     */
    function setValidatorOptions(options) {
        validatorOptions = options ? { ...options } : null;
    }

    // Preview and Markdown renderer failures are possible issues. They are merged
    // here rather than shown separately, so one drawer holds everything.
    function setRendererDiagnostics(items) {
        rendererDiagnostics = Array.isArray(items) ? items.slice() : [];
        if (active) render();
    }

    function getAuthoritativeErrors() {
        return validatorDiagnostics.filter(isError);
    }

    // Formatting runs through the same executor and the same deadline as
    // validation, and like a save it suspends background validation for its
    // duration so a keystroke cannot terminate the request it is waiting on.
    async function format(source, targetFormat) {
        const shared = api();
        criticalDepth++;
        cancelDebounce();
        try {
            const executor = await shared.getExecutor();
            return await executor.run({
                op: shared.OP_FORMAT,
                generation: ++generation,
                payload: { format: targetFormat, source },
                sourceBytes: byteLengthOf(source),
            });
        } catch (err) {
            return { ok: false, reason: shared.isUnavailableCode(err && err.code) ? 'unavailable' : 'failed' };
        } finally {
            criticalDepth--;
            flushPending();
        }
    }

    /**
     * Presents a *reclassified target's* findings in the drawer.
     *
     * Used by Save As, where the document is judged by the proposed filename's
     * grammar rather than the open clip's. The findings are shown and the drawer is
     * expanded, but they are not adopted as this session's validator state: the next
     * ordinary validation of the open document must overwrite them, not merge with
     * them.
     */
    /**
     * Re-evaluates the >2 MiB assistance gate mid-session.
     *
     * The gate used to be fixed at open. Pasting past the threshold has to turn
     * validation off and clear whatever findings were already on screen, because they
     * describe a document that is no longer being checked.
     */
    function setEnabled(next) {
        const value = next !== false;
        if (value === enabled) return;
        enabled = value;
        if (!enabled) {
            generation += 1;
            cancelDebounce();
            pendingSource = null;
            validatorDiagnostics = [];
            truncatedCollection = false;
            unavailable = null;
        }
        render();
    }

    function showForeign(items) {
        if (!active) return;
        validatorDiagnostics = Array.isArray(items) ? items.slice() : [];
        truncatedCollection = false;
        unavailable = null;
        expanded = true;
        render();
    }

    return {
        setup,
        open,
        close,
        schedule,
        refresh,
        expectRecoveredDraft,
        setValidatorOptions,
        setRendererDiagnostics,
        getAuthoritativeErrors,
        validateCritical,
        setEnabled,
        showForeign,
        format,
        expand: () => setExpanded(true),
        collapse: () => setExpanded(false),
        isExpanded: () => expanded,
        isUnavailable: () => !!unavailable,
        getPresented: () => presented.slice(),
    };
})();
