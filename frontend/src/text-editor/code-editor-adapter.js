// The only module in the app that touches CodeMirror's core APIs.
//
// Everything above this file — the editor shell, the find panel, drafts, save —
// speaks in semantic operations: getValue, selectRange, findNext. That boundary
// is the point: it keeps CodeMirror out of the surrounding editor and leaves one
// place to change when a diagnostic source or a CodeMirror major version arrives.
//
// Its sibling `language-modes.js` owns the language-package boundary: this file
// knows that a language is an opaque extension it reconfigures a Compartment to,
// and nothing more. Nothing under `frontend/js/` imports either.

import { Compartment, EditorSelection, EditorState, StateEffect, StateField, Transaction } from '@codemirror/state';
import { Decoration, EditorView, drawSelection, keymap, lineNumbers } from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, redo, undo } from '@codemirror/commands';
import { bracketMatching, indentOnInput, indentUnit } from '@codemirror/language';
import {
    SearchQuery,
    findNext as cmFindNext,
    findPrevious as cmFindPrevious,
    getSearchQuery,
    highlightSelectionMatches,
    replaceAll as cmReplaceAll,
    replaceNext as cmReplaceNext,
    search,
    setSearchQuery,
} from '@codemirror/search';
import { resolveLanguage as defaultResolveLanguage } from './language-modes.js';

// A one-character query on a large document can match a great many times.
// Collecting every hit would make both the count and the decoration set
// unbounded, so the list is capped and the cap is reported rather than hidden.
export const MAX_SEARCH_MATCHES = 5000;

// --- app-owned search highlighting -------------------------------------------
//
// @codemirror/search ships its own .cm-searchMatch decorations, but they are
// gated on the stock search panel being open (searchHighlighter returns
// Decoration.none when `panel` is null) and are limited to the current viewport.
// This design mounts the app's own find panel and never the stock one, so those
// decorations would never appear — and a viewport-limited set would also make an
// "N of M" count disagree with itself as the user scrolls.
//
// This is not the mirrored highlight layer that was retired. That layer was a
// second copy of the document's text in a parallel DOM node, kept aligned by
// hand through a ResizeObserver and scroll syncing. These are ordinary
// CodeMirror mark decorations: no duplicated text, no alignment, no observer.
const matchMark = Decoration.mark({ attributes: { 'data-search-match': '', 'data-search-active': 'false' } });
const activeMatchMark = Decoration.mark({ attributes: { 'data-search-match': '', 'data-search-active': 'true' } });

const setMatchHighlights = StateEffect.define();

// --- inline diagnostic markers ------------------------------------------------
//
// App-owned decorations rather than @codemirror/lint: lint brings a panel, a
// tooltip layer, its own gutter and its own keymap, and the approved UI is a
// collapsible drawer this file knows nothing about. Severity and index travel as
// data attributes so the shell can route a click back to the drawer row without
// the shell ever touching a CodeMirror API.
const diagnosticMarks = {
    error: Decoration.mark({ attributes: { 'data-diagnostic-severity': 'error' } }),
    'possible-issue': Decoration.mark({ attributes: { 'data-diagnostic-severity': 'possible-issue' } }),
};
const diagnosticLines = {
    error: Decoration.line({ attributes: { 'data-diagnostic-line': 'error' } }),
    'possible-issue': Decoration.line({ attributes: { 'data-diagnostic-line': 'possible-issue' } }),
};

const setDiagnosticMarkers = StateEffect.define();

const diagnosticField = StateField.define({
    create() {
        return Decoration.none;
    },
    update(value, tr) {
        for (const effect of tr.effects) {
            if (effect.is(setDiagnosticMarkers)) return effect.value;
        }
        // Diagnostics are recomputed on a debounce, so between an edit and the
        // next result the existing markers are mapped rather than left at stale
        // offsets.
        return tr.docChanged ? value.map(tr.changes) : value;
    },
    provide: (field) => EditorView.decorations.from(field),
});

const matchHighlightField = StateField.define({
    create() {
        return Decoration.none;
    },
    update(value, tr) {
        for (const effect of tr.effects) {
            if (effect.is(setMatchHighlights)) {
                const { matches, activeIndex } = effect.value;
                if (!matches.length) return Decoration.none;
                return Decoration.set(
                    matches.map((match, index) =>
                        (index === activeIndex ? activeMatchMark : matchMark).range(match.from, match.to)),
                    true,
                );
            }
        }
        // Map through document changes so a replace does not leave stale marks
        // sitting at the wrong offsets until the next refresh.
        return tr.docChanged ? value.map(tr.changes) : value;
    },
    provide: (field) => EditorView.decorations.from(field),
});

/**
 * Creates the adapter. Nothing is mounted until mount() is called.
 */
export function createCodeEditorAdapter(options = {}) {
    const wrapCompartment = new Compartment();
    const languageCompartment = new Compartment();
    // The resolver stays injectable so a test can force a failure, but the default
    // is the real registry-driven one. A language is only ever the descriptor's
    // trusted `language` field; the adapter performs no detection of its own.
    const resolveLanguage = options.resolveLanguage || defaultResolveLanguage;

    let view = null;
    let callbacks = {};
    let container = null;
    let lastMatches = [];
    let lastActiveIndex = -1;
    let lastTruncated = false;

    function requireView() {
        if (!view) throw new Error('CodeEditorAdapter is not mounted');
        return view;
    }

    function notifyChange(update) {
        if (update.docChanged && callbacks.onChange) callbacks.onChange();
        if ((update.docChanged || update.selectionSet) && callbacks.onSelectionChange) {
            callbacks.onSelectionChange();
        }
    }

    function baseExtensions() {
        return [
            lineNumbers(),
            history(),
            bracketMatching(),
            indentOnInput(),
            indentUnit.of('    '),
            // Required. A native textarea stops painting its selection when focus
            // moves into the find input; CodeMirror's drawSelection paints from its
            // own layer, so a match stays visible while the query field has focus.
            drawSelection(),
            // Highlights other occurrences of the current selection while editing.
            // Distinct from search highlighting above, which follows the find query.
            highlightSelectionMatches(),
            // Installs the search state the semantic operations below read. It does
            // NOT install searchKeymap and does NOT mount a panel, so the stock
            // search UI never appears.
            search({ literal: true }),
            matchHighlightField,
            diagnosticField,
            // Activating an inline marker is one of the three things that expands
            // the drawer, so the click has to reach the shell. mousedown rather
            // than click, and no preventDefault: CodeMirror still places the
            // cursor where the user pressed.
            EditorView.domEventHandlers({
                mousedown(event) {
                    const target = event.target;
                    if (!target || typeof target.closest !== 'function') return false;
                    const marker = target.closest('[data-diagnostic-severity]');
                    if (!marker || !callbacks.onDiagnosticActivated) return false;
                    callbacks.onDiagnosticActivated();
                    return false;
                },
            }),
            wrapCompartment.of([]),
            languageCompartment.of([]),
            keymap.of([
                // Mod-F belongs to the app's find panel, not to CodeMirror.
                {
                    key: 'Mod-f',
                    preventDefault: true,
                    run: () => {
                        if (callbacks.onFindRequested) callbacks.onFindRequested();
                        return true;
                    },
                },
                // No Mod-S binding on purpose. ShortcutManager suppresses shortcuts
                // while an editable element has focus, so keyboard save behaves
                // inside the editor exactly as it did with the textarea. Adding it
                // here would change save behavior in a milestone that is only
                // supposed to change the editing widget.
            ]),
            // Deliberately after the custom keymap so the bindings above win.
            // defaultKeymap does not bind Tab, and no indentWithTab is added: Tab
            // must keep moving focus out of the editor for keyboard users.
            keymap.of([...defaultKeymap, ...historyKeymap]),
            EditorView.updateListener.of(notifyChange),
            EditorView.contentAttributes.of({
                'aria-label': options.ariaLabel || 'Clip content',
                spellcheck: 'false',
                autocapitalize: 'off',
                autocorrect: 'off',
            }),
        ];
    }

    function mount(config) {
        destroy();
        container = config.container;
        callbacks = config.callbacks || {};
        const extensions = [...baseExtensions()];
        view = new EditorView({
            state: EditorState.create({ doc: config.value || '', extensions }),
            parent: container,
        });
        setWrap(config.wrap !== false);
        setLanguage(config.language || null);
        if (config.diagnostics) setDiagnostics(config.diagnostics);
        return view;
    }

    function destroy() {
        if (view) {
            view.destroy();
            view = null;
        }
        callbacks = {};
        container = null;
        lastMatches = [];
        lastActiveIndex = -1;
        lastTruncated = false;
    }

    function isMounted() {
        return !!view;
    }

    function getValue() {
        return view ? view.state.doc.toString() : '';
    }

    function setValue(value, { undoable = true } = {}) {
        const target = requireView();
        target.dispatch({
            changes: { from: 0, to: target.state.doc.length, insert: value },
            // A non-undoable replacement is for loading a document, not editing
            // it: it must not become a step the user can undo into. Formatting, by
            // contrast, is a normal undoable transaction.
            annotations: undoable ? Transaction.userEvent.of('input.replace') : Transaction.addToHistory.of(false),
            selection: { anchor: Math.min(target.state.selection.main.anchor, value.length) },
        });
    }

    function focus() {
        if (view) view.focus();
    }

    function hasFocus() {
        return !!view && view.hasFocus;
    }

    function getSelection() {
        const target = requireView();
        const { from, to } = target.state.selection.main;
        return { from, to, text: target.state.sliceDoc(from, to) };
    }

    // Positions are 1-based lines and 1-based columns measured in UTF-16 code
    // units, matching CodeMirror's document model and every other position in
    // this feature.
    function getCursorPosition() {
        const target = requireView();
        const head = target.state.selection.main.head;
        const line = target.state.doc.lineAt(head);
        return { line: line.number, column: head - line.from + 1 };
    }

    function selectRange(range) {
        const target = requireView();
        const max = target.state.doc.length;
        const from = Math.max(0, Math.min(range.from, max));
        const to = Math.max(from, Math.min(range.to === undefined ? from : range.to, max));
        target.dispatch({ selection: EditorSelection.single(from, to) });
    }

    function revealRange(range) {
        const target = requireView();
        const max = target.state.doc.length;
        const from = Math.max(0, Math.min(range.from, max));
        const to = Math.max(from, Math.min(range.to === undefined ? from : range.to, max));
        target.dispatch({ effects: EditorView.scrollIntoView(EditorSelection.range(from, to), { y: 'nearest', x: 'nearest' }) });
    }

    function setWrap(enabled) {
        if (!view) return;
        view.dispatch({ effects: wrapCompartment.reconfigure(enabled ? EditorView.lineWrapping : []) });
    }

    // Each mode keeps its own scroll position while the modal is open, so the
    // shell needs to read and restore the editor's scroller directly.
    function getScrollTop() {
        return view ? view.scrollDOM.scrollTop : 0;
    }

    function setScrollTop(value) {
        if (view) view.scrollDOM.scrollTop = value || 0;
    }

    // CodeMirror caches measurements. Hiding the editor (switching to Preview)
    // and showing it again invalidates them, so the shell must ask for a re-measure
    // or the first paint after unhiding can be laid out for a zero-height box.
    function refreshLayout() {
        if (view) view.requestMeasure();
    }

    function setLanguage(language) {
        if (!view) return;
        let extension = [];
        try {
            extension = resolveLanguage(language) || [];
        } catch (err) {
            // An adapter failure removes the language extension and retains plain
            // editing rather than taking the editor down with it.
            console.error('Failed to resolve language extension:', err);
            extension = [];
        }
        view.dispatch({ effects: languageCompartment.reconfigure(extension) });
    }

    // --- search ---------------------------------------------------------------

    function currentQuery() {
        return view ? getSearchQuery(view.state) : null;
    }

    function collectMatches() {
        if (!view) return { matches: [], truncated: false };
        const query = currentQuery();
        if (!query || !query.valid) return { matches: [], truncated: false };
        const matches = [];
        let truncated = false;
        const cursor = query.getCursor(view.state);
        while (!cursor.next().done) {
            if (matches.length >= MAX_SEARCH_MATCHES) { truncated = true; break; }
            matches.push({ from: cursor.value.from, to: cursor.value.to });
        }
        return { matches, truncated };
    }

    function activeIndexIn(matches) {
        if (!view) return -1;
        const { from, to } = view.state.selection.main;
        return matches.findIndex((match) => match.from === from && match.to === to);
    }

    function publishHighlights() {
        if (!view) return { matches: [], activeIndex: -1, truncated: false };
        const { matches, truncated } = collectMatches();
        const activeIndex = activeIndexIn(matches);
        lastMatches = matches;
        lastActiveIndex = activeIndex;
        lastTruncated = truncated;
        view.dispatch({ effects: setMatchHighlights.of({ matches, activeIndex }) });
        return { matches, activeIndex, truncated };
    }

    function setSearchQueryValue(searchText, { caseSensitive = false, wholeWord = false, replace = '' } = {}) {
        const target = requireView();
        target.dispatch({
            effects: setSearchQuery.of(new SearchQuery({
                search: searchText || '',
                caseSensitive,
                wholeWord,
                replace,
                // Literal: the query is what the user typed. Without this,
                // @codemirror/search interprets \n, \r, \t escapes.
                literal: true,
            })),
        });
        return publishHighlights();
    }

    // Every stock search command falls back to openSearchPanel() when the query
    // is absent or invalid. Guarding here is what keeps the stock panel from ever
    // appearing.
    function runSearchCommand(command) {
        const target = requireView();
        const query = currentQuery();
        if (!query || !query.valid) return { matches: [], activeIndex: -1, truncated: false, ran: false };
        command(target);
        return { ...publishHighlights(), ran: true };
    }

    function findNext() {
        return runSearchCommand(cmFindNext);
    }

    function findPrevious() {
        return runSearchCommand(cmFindPrevious);
    }

    function replaceCurrent(replacement) {
        const query = currentQuery();
        if (!query || !query.valid) return { matches: [], activeIndex: -1, truncated: false, ran: false };
        setSearchQueryValue(query.search, {
            caseSensitive: query.caseSensitive,
            wholeWord: query.wholeWord,
            replace: replacement || '',
        });
        return runSearchCommand(cmReplaceNext);
    }

    function replaceAllMatches(replacement) {
        const query = currentQuery();
        if (!query || !query.valid) return { matches: [], activeIndex: -1, truncated: false, ran: false };
        setSearchQueryValue(query.search, {
            caseSensitive: query.caseSensitive,
            wholeWord: query.wholeWord,
            replace: replacement || '',
        });
        return runSearchCommand(cmReplaceAll);
    }

    function clearSearch() {
        if (!view) return;
        // An empty query is invalid, which both disables the stock commands and
        // clears our highlight set.
        view.dispatch({ effects: setSearchQuery.of(new SearchQuery({ search: '', literal: true })) });
        lastMatches = [];
        lastActiveIndex = -1;
        lastTruncated = false;
        view.dispatch({ effects: setMatchHighlights.of({ matches: [], activeIndex: -1 }) });
    }

    function getSearchState() {
        return { matches: lastMatches, activeIndex: lastActiveIndex, truncated: lastTruncated };
    }

    function refreshSearch() {
        return view ? publishHighlights() : getSearchState();
    }

    // --- diagnostics ----------------------------------------------------------

    let diagnostics = [];

    /**
     * Converts a diagnostic's 1-based line and 1-based UTF-16 column into a
     * document range.
     *
     * Every position in this feature arrives in CodeMirror's own unit, so this is
     * a clamp rather than a conversion: each parser adapter already converted at
     * its own boundary, and an unconverted byte or code-point offset would land
     * the cursor in the wrong place instead of failing loudly.
     */
    function rangeForDiagnostic(diagnostic) {
        const target = requireView();
        const doc = target.state.doc;
        const lineNumber = Math.max(1, Math.min(Math.trunc(diagnostic.line) || 1, doc.lines));
        const line = doc.line(lineNumber);
        const from = Math.min(line.from + Math.max(0, (Math.trunc(diagnostic.column) || 1) - 1), line.to);

        let to = from;
        if (diagnostic.endLine && diagnostic.endColumn) {
            const endNumber = Math.max(lineNumber, Math.min(Math.trunc(diagnostic.endLine), doc.lines));
            const endLine = doc.line(endNumber);
            to = Math.min(endLine.from + Math.max(0, Math.trunc(diagnostic.endColumn) - 1), endLine.to);
        }
        if (to <= from) to = Math.min(from + 1, line.to);
        return { from, to: Math.max(from, to) };
    }

    /**
     * Publishes the inline markers.
     *
     * A finding at end of line — a missing closing brace, an unexpected end of
     * input — has no character to underline, so it falls back to a whole-line
     * decoration instead of being dropped.
     */
    function setDiagnostics(items) {
        diagnostics = Array.isArray(items) ? items.slice() : [];
        if (!view) return diagnostics.length;

        const ranges = [];
        for (const item of diagnostics) {
            const severity = item.severity === 'error' ? 'error' : 'possible-issue';
            let range;
            try {
                range = rangeForDiagnostic(item);
            } catch (err) {
                continue;
            }
            if (range.to > range.from) {
                ranges.push(diagnosticMarks[severity].range(range.from, range.to));
            } else {
                const line = view.state.doc.lineAt(range.from);
                ranges.push(diagnosticLines[severity].range(line.from));
            }
        }
        view.dispatch({ effects: setDiagnosticMarkers.of(Decoration.set(ranges, true)) });
        return diagnostics.length;
    }

    function getDiagnostics() {
        return diagnostics.slice();
    }

    return {
        mount,
        destroy,
        isMounted,
        getValue,
        setValue,
        focus,
        hasFocus,
        getSelection,
        getCursorPosition,
        selectRange,
        revealRange,
        setWrap,
        setLanguage,
        getScrollTop,
        setScrollTop,
        refreshLayout,
        setSearchQuery: setSearchQueryValue,
        findNext,
        findPrevious,
        replaceCurrent,
        replaceAll: replaceAllMatches,
        clearSearch,
        getSearchState,
        refreshSearch,
        setDiagnostics,
        getDiagnostics,
        rangeForDiagnostic,
        undo: () => (view ? undo(view) : false),
        redo: () => (view ? redo(view) : false),
        get view() { return view; },
        get container() { return container; },
    };
}
