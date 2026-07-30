// Read-only source Preview.
//
// The security property this module exists to hold: source text becomes text
// nodes and app-owned <span> elements, and nothing else. It never assigns source
// to innerHTML, never turns a URL into a link, and never gives a token a class
// name derived from the document. HTML source is highlighted text, not markup.
//
// Highlighting is injected rather than imported, and stays that way now that
// language modes exist: the allowlist below has to hold for *any* highlighter,
// including a hostile one and none at all. A renderer that reached for its own
// highlighter would make that guarantee depend on which one it picked.
//
// With no highlighter, Preview is line-numbered inert source — the same shape the
// >2 MiB degraded state and the token-cap fallback produce.

// Exceeding this falls back to plain inert source with an explanation. A
// pathological document can otherwise produce a span per character.
export const MAX_TOKEN_SPANS = 100000;

// The token cap bounds *spans*, not lines. A document of nothing but newlines has
// zero tokens and would still ask for one <li> per line: 2 MiB of "\n" is 2,097,153
// elements, inside the assistance threshold and enough to freeze or OOM the panel.
// Past this, Preview is the same plain inert <pre> the degraded state uses.
export const MAX_PREVIEW_LINES = 50000;

// Token class names are app-owned and drawn from this fixed set. A highlighter
// that reports anything else contributes plain text, so no document content can
// reach a class attribute.
const TOKEN_CLASSES = new Set([
    'attribute',
    'boolean',
    'comment',
    'keyword',
    'meta',
    'null',
    'number',
    'operator',
    'property',
    'punctuation',
    'string',
    'tag',
    'type',
    'variable',
]);

// Counts lines without materializing them, stopping as soon as the answer can only
// be "more than `stopAfter`". The whole point of the cap is not to allocate an array
// per line for a pathological document, so the check itself must not do it either.
function countLines(text, stopAfter) {
    let count = 1;
    for (let i = 0; i < text.length; i++) {
        if (text[i] === '\n') {
            count += 1;
            if (count > stopAfter) return count;
        }
    }
    return count;
}

function diagnostic(message) {
    return {
        severity: 'possible-issue',
        source: 'source-preview',
        message,
        line: 1,
        column: 1,
    };
}

// The unhighlighted form: one <pre> holding the whole document as a single text
// node. Used above the enhanced-assistance threshold, after a highlighter
// failure, and when the token cap is exceeded.
function plainSource(source, wrap) {
    const pre = document.createElement('pre');
    pre.className = 'source-preview-plain';
    pre.dataset.wrap = wrap ? 'on' : 'off';
    pre.appendChild(document.createTextNode(source));
    return pre;
}

function explanation(message) {
    const note = document.createElement('p');
    note.className = 'source-preview-note';
    note.textContent = message;
    return note;
}

// Line numbers are CSS counters, not text nodes: a selection that crosses lines
// must copy the source, not the gutter.
function lineElement(text, tokens) {
    const line = document.createElement('li');
    line.className = 'source-preview-line';
    if (!tokens || tokens.length === 0) {
        line.appendChild(document.createTextNode(text));
        return line;
    }
    let cursor = 0;
    for (const token of tokens) {
        const from = Math.max(0, Math.min(token.from, text.length));
        const to = Math.max(from, Math.min(token.to, text.length));
        if (from > cursor) line.appendChild(document.createTextNode(text.slice(cursor, from)));
        const slice = text.slice(from, to);
        if (slice) {
            if (TOKEN_CLASSES.has(token.type)) {
                const span = document.createElement('span');
                span.className = `source-token source-token-${token.type}`;
                span.appendChild(document.createTextNode(slice));
                line.appendChild(span);
            } else {
                // An unrecognized token kind contributes plain text rather than an
                // attacker-influenced class name.
                line.appendChild(document.createTextNode(slice));
            }
        }
        cursor = to;
    }
    if (cursor < text.length) line.appendChild(document.createTextNode(text.slice(cursor)));
    return line;
}

/**
 * Creates the renderer.
 *
 * `highlight(source, language)` returns an array of `{ line, from, to, type }`
 * tokens, where `line` is 1-based and `from`/`to` are UTF-16 offsets within that
 * line. Throwing is an accepted outcome: the caller gets plain source and a
 * diagnostic rather than a broken Preview.
 */
export function createSourcePreviewRenderer(options = {}) {
    const highlight = typeof options.highlight === 'function' ? options.highlight : null;
    const maxTokenSpans = options.maxTokenSpans || MAX_TOKEN_SPANS;

    function render({ source = '', language = null, wrap = true, generation = 0, plain = false } = {}) {
        const root = document.createElement('div');
        root.className = 'source-preview';
        root.dataset.wrap = wrap ? 'on' : 'off';
        if (language) root.dataset.language = language;
        root.dataset.generation = String(generation);

        const diagnostics = [];

        // Both bail-outs come BEFORE the split. `source.split('\n')` on a 16 MiB
        // newline-only clip allocates 16,777,217 strings — measured at ~144 MiB and
        // ~215 ms — so a cap checked after it is a cap that has already paid the cost
        // it exists to avoid.
        if (plain) {
            // The degraded path: above the enhanced-assistance threshold there is no
            // highlighting, no per-line element, and no parsing.
            root.dataset.mode = 'plain';
            root.appendChild(plainSource(source, wrap));
            return { node: root, diagnostics, generation, mode: 'plain', tokenSpans: 0 };
        }
        if (countLines(source, MAX_PREVIEW_LINES) > MAX_PREVIEW_LINES) {
            root.dataset.mode = 'line-capped';
            root.appendChild(explanation(
                `Highlighting is unavailable for this document: it has more than ${MAX_PREVIEW_LINES.toLocaleString()} lines. Showing plain source.`,
            ));
            root.appendChild(plainSource(source, wrap));
            return { node: root, diagnostics, generation, mode: 'line-capped', tokenSpans: 0 };
        }

        const lines = source.split('\n');

        // Line numbering is independent of highlighting. A missing highlighter — the
        // state until language modes land — is not a failure: it means no token
        // spans, not a Preview without a gutter.
        let byLine = new Map();
        try {
            const tokens = language && highlight ? (highlight(source, language) || []) : [];
            if (tokens.length > maxTokenSpans) {
                root.dataset.mode = 'capped';
                root.appendChild(explanation(
                    `Highlighting is unavailable for this document: it produces more than ${maxTokenSpans.toLocaleString()} highlighted regions. Showing plain source.`,
                ));
                root.appendChild(plainSource(source, wrap));
                return { node: root, diagnostics, generation, mode: 'capped', tokenSpans: tokens.length };
            }
            for (const token of tokens) {
                const list = byLine.get(token.line);
                if (list) list.push(token);
                else byLine.set(token.line, [token]);
            }
        } catch (err) {
            // Parser or highlighter failure falls back to a plain <pre> plus a
            // diagnostic. Invalid syntax does not replace Preview with an error
            // screen; the source stays visible.
            root.dataset.mode = 'failed';
            // Deliberately NOT the exception's message. A highlighter that throws
            // with source in the message — `throw new Error(source)` — would put a
            // `.env` value straight into the drawer and into its accessible name.
            // The real detail goes to the console, where it is a developer's problem
            // rather than a rendered string.
            console.error('Source preview highlighting failed:', err);
            diagnostics.push(diagnostic('Highlighting is unavailable for this document. Showing plain source.'));
            root.appendChild(plainSource(source, wrap));
            return { node: root, diagnostics, generation, mode: 'failed', tokenSpans: 0 };
        }

        // 'lines' rather than 'highlighted': the structure is the same whether or
        // not any tokens were contributed.
        root.dataset.mode = 'lines';
        const list = document.createElement('ol');
        list.className = 'source-preview-lines';
        let tokenSpans = 0;
        for (let i = 0; i < lines.length; i++) {
            const tokens = byLine.get(i + 1);
            if (tokens) tokenSpans += tokens.length;
            list.appendChild(lineElement(lines[i], tokens));
        }
        root.appendChild(list);
        return { node: root, diagnostics, generation, mode: 'lines', tokenSpans };
    }

    return { render, MAX_TOKEN_SPANS: maxTokenSpans };
}

export const SourcePreviewRenderer = {
    create: createSourcePreviewRenderer,
    MAX_TOKEN_SPANS,
    MAX_PREVIEW_LINES,
    TOKEN_CLASSES: Array.from(TOKEN_CLASSES),
};
