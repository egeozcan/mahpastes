// --- Text Preview ---
// Dispatches Preview rendering by descriptor: the existing Markdown pipeline, the
// safe source renderer, or the bounded CSV/TSV table.
//
// This lives in the classic-script layer rather than the bundle because Markdown
// preview is a classic script — MarkdownRenderer and MarkdownPreview are globals
// with their own sanitization, reference resolution, and image policy. The bundle
// contributes only SourcePreviewRenderer, which touches no app globals.
//
// Every renderer returns app-owned DOM nodes plus renderer diagnostics. A renderer
// failure never replaces Preview with an error screen: the source stays visible
// and the failure becomes a diagnostic.

const TextPreview = (() => {
    let sourceRenderer = null;
    let tableRenderer = null;
    let lastKey = null;
    let lastDiagnostics = [];
    // The last table render's effective interpretation, so the control bar can
    // label what was detected without reparsing. Null for every non-table render.
    let lastInterpretation = null;

    function panel() {
        return document.getElementById('editor-preview-panel');
    }

    function markdownHost() {
        return document.getElementById('markdown-preview-content');
    }

    function sourceHost() {
        return document.getElementById('source-preview-content');
    }

    function tableHost() {
        return document.getElementById('table-preview-content');
    }

    function renderer() {
        if (!sourceRenderer) {
            // The highlighter is the same Lezer-tree-driven one the editor's own
            // highlighting comes from, so Preview and Edit paint the same tokens the
            // same way. It is injected rather than reached for by the renderer: the
            // renderer's job is to validate every token type against its own
            // allowlist, and that has to hold for any highlighter.
            sourceRenderer = window.MahpastesTextEditor.SourcePreviewRenderer.create({
                highlight: window.MahpastesTextEditor.LanguageModes.highlight,
            });
        }
        return sourceRenderer;
    }

    function table() {
        if (!tableRenderer) {
            tableRenderer = window.MahpastesTextEditor.TablePreviewRenderer.create();
        }
        return tableRenderer;
    }

    function show(kind) {
        panel().dataset.preview = kind;
        markdownHost().classList.toggle('hidden', kind !== 'markdown');
        sourceHost().classList.toggle('hidden', kind !== 'source');
        tableHost().classList.toggle('hidden', kind !== 'table');
    }

    function renderSource(options) {
        const result = renderer().render({
            source: options.source,
            language: options.descriptor ? options.descriptor.language : null,
            wrap: options.wrap,
            generation: options.generation,
            plain: options.plain,
        });
        sourceHost().replaceChildren(result.node);
        markdownHost().replaceChildren();
        tableHost().replaceChildren();
        lastInterpretation = null;
        show('source');
        return result.diagnostics;
    }

    function renderTable(options) {
        const descriptor = options.descriptor;
        const result = table().render({
            source: options.source,
            // Registry-driven: TSV carries its own tab bias, so nothing here
            // switches on a format id.
            preferredID: descriptor ? descriptor.tableDelimiter : null,
            delimiterID: options.tableOptions.delimiterID,
            headerMode: options.tableOptions.headerMode,
            wrap: options.wrap,
            generation: options.generation,
        });
        tableHost().replaceChildren(result.node);
        markdownHost().replaceChildren();
        sourceHost().replaceChildren();
        lastInterpretation = result.interpretation;
        show('table');
        return result.diagnostics;
    }

    function renderMarkdown(options) {
        MarkdownPreview.beginRender();
        try {
            const rendered = MarkdownRenderer.render(options.source);
            markdownHost().replaceChildren(rendered);
            sourceHost().replaceChildren();
            tableHost().replaceChildren();
            lastInterpretation = null;
            show('markdown');
            MarkdownPreview.enhance(rendered);
            return [];
        } catch (error) {
            console.error('Markdown preview failed:', error);
            // A Markdown renderer exception falls back to inert source plus a
            // diagnostic — deliberately not a format-specific unavailable screen,
            // which would hide content the user can still read and copy.
            const diagnostics = renderSource(options);
            return diagnostics.concat([{
                severity: 'possible-issue',
                source: 'markdown-renderer',
                // Fixed text, not the exception's message: a renderer that throws with
                // source in the message would put document content into the drawer and
                // into its accessible name. The detail goes to the console instead.
                message: 'Markdown preview is unavailable for this document. Showing plain source.',
                line: 1,
                column: 1,
            }]);
        }
    }

    /**
     * Renders the current unsaved source into the preview panel.
     *
     * Returns the renderer diagnostics. Identical input is not re-rendered — the
     * Markdown pipeline in particular re-resolves references and re-applies the
     * image policy on every render.
     */
    function render(options) {
        const descriptor = options.descriptor || null;
        const source = options.source || '';
        const wrap = options.wrap !== false;
        const generation = options.generation || 0;
        // Above the enhanced-assistance threshold every format previews as plain
        // inert source: no Markdown rendering, no highlighting, no table parsing.
        const plain = options.degraded === true;
        const kind = plain ? 'source' : (descriptor && descriptor.preview) || 'source';
        const tableOptions = options.tableOptions || { delimiterID: null, headerMode: 'auto' };

        // The interpretation controls belong in the key: changing the delimiter
        // changes the table without changing a character of source, and a key that
        // ignored them would silently keep showing the previous interpretation.
        const key = `${kind}|${wrap ? 1 : 0}|${plain ? 1 : 0}|${tableOptions.delimiterID || ''}|${tableOptions.headerMode}|${source}`;
        if (!options.force && key === lastKey) return lastDiagnostics;
        lastKey = key;

        const request = { descriptor, source, wrap, generation, plain, tableOptions };
        if (kind === 'markdown') {
            lastDiagnostics = renderMarkdown(request);
        } else if (kind === 'table') {
            lastDiagnostics = renderTable(request);
        } else {
            lastDiagnostics = renderSource(request);
        }
        return lastDiagnostics;
    }

    function invalidate() {
        lastKey = null;
        lastDiagnostics = [];
    }

    function clear() {
        invalidate();
        lastInterpretation = null;
        markdownHost().replaceChildren();
        sourceHost().replaceChildren();
        tableHost().replaceChildren();
    }

    return { render, invalidate, clear, getInterpretation: () => lastInterpretation };
})();
