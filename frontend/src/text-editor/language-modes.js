// Language modes for Edit and Source Preview.
//
// One place decides what a registry language id means, and it decides it for both
// panels at once: the Compartment extension CodeMirror reconfigures in Edit, and
// the token stream the read-only Source Preview renderer paints. Editor and
// Preview therefore cannot drift into looking like two different systems, because
// there is exactly one tag→token-class table and both read it.
//
// This module and `code-editor-adapter.js` are the only two files in the app that
// import CodeMirror or Lezer packages. The adapter owns the CodeMirror *core*
// boundary — state, view, commands; this file owns the *language package*
// boundary. Nothing under `frontend/js/` imports either.
//
// Two properties are load-bearing rather than stylistic:
//
//   * The language is always the trusted registry `language` field. There is no
//     automatic language detection anywhere in this file, and no code path that
//     derives a mode from document content.
//   * `highlight()` emits only the token type names in the renderer's fixed
//     allowlist. A Lezer node whose tags map to nothing contributes no token, so
//     no part of a document can influence a class attribute.

import { HighlightStyle, StreamLanguage, syntaxHighlighting } from '@codemirror/language';
import { EditorState } from '@codemirror/state';
import { highlightTree, tagHighlighter, tags } from '@lezer/highlight';
// The bare Language objects, deliberately not the `css()` / `html()` /
// `javascript()` LanguageSupport bundles those packages also export. A
// LanguageSupport carries the package's opinion of a full editing experience:
// completion sources in language data, and `autoCloseTags` for XML, HTML and JSX.
// Both are out of scope by design — the spec registers no completion popup,
// suggestion source or hover documentation — and `autoCloseTags` is worse than
// unused, because it rewrites what the user typed. A Language carries the parser,
// its highlight tags and its indentation, which is exactly the agreed feature set.
import { cssLanguage } from '@codemirror/lang-css';
import { htmlLanguage } from '@codemirror/lang-html';
import { javascriptLanguage, typescriptLanguage } from '@codemirror/lang-javascript';
import { jsonLanguage } from '@codemirror/lang-json';
import { markdownLanguage } from '@codemirror/lang-markdown';
import { xmlLanguage } from '@codemirror/lang-xml';
import { yamlLanguage } from '@codemirror/lang-yaml';
// Legacy stream modes where a full Lezer package would be disproportionate: each
// of these is a few hundred lines of tokenizer against tens of kilobytes of
// generated parser tables, and none of the three has a validator that would need
// a real syntax tree.
import { properties as propertiesMode } from '@codemirror/legacy-modes/mode/properties';
import { shell as shellMode } from '@codemirror/legacy-modes/mode/shell';
import { toml as tomlMode } from '@codemirror/legacy-modes/mode/toml';
import { createPositionIndex, makeDiagnostic, DIAGNOSTIC_LIMITS, SEVERITY } from './diagnostics.js';
import { MAX_TOKEN_SPANS } from './source-preview.js';

// --- the one tag → token-class table -----------------------------------------
//
// The right-hand names are the renderer's app-owned allowlist (TOKEN_CLASSES in
// source-preview.js) and the CSS classes in modals.css. Widening the left side is
// ordinary work; widening the right side is a security change.
//
// Only base tags are listed where Lezer derives the specific ones from them:
// `comment` covers lineComment/blockComment/docComment, `bracket` covers
// paren/brace/squareBracket/angleBracket, `operator` covers every arithmetic,
// logic, compare and update variant, and a modifier such as
// `definition(variableName)` resolves through its base.
const TOKEN_TAGS = [
    ['comment', [tags.comment, tags.quote, tags.strikethrough]],
    ['string', [tags.string, tags.attributeValue, tags.regexp, tags.escape, tags.url, tags.link, tags.monospace, tags.color]],
    ['number', [tags.number, tags.unit]],
    // Markdown/YAML/TOML booleans and legacy-mode `atom` tokens land here; `null`
    // keeps its own class because JSON and YAML both distinguish it.
    ['boolean', [tags.bool, tags.atom]],
    ['null', [tags.null]],
    ['keyword', [tags.keyword, tags.self, tags.modifier, tags.heading, tags.strong]],
    ['type', [tags.typeName, tags.className, tags.namespace, tags.emphasis]],
    ['tag', [tags.tagName]],
    ['property', [tags.propertyName, tags.labelName]],
    ['attribute', [tags.attributeName]],
    ['variable', [tags.name, tags.variableName, tags.macroName]],
    ['operator', [tags.operator]],
    ['punctuation', [tags.punctuation, tags.separator, tags.bracket, tags.contentSeparator, tags.list]],
    ['meta', [tags.meta, tags.processingInstruction, tags.annotation, tags.documentMeta]],
];

// A renamed or removed tag in a future @lezer/highlight would otherwise silently
// stop highlighting one kind of token. Fail at load instead.
for (const [type, list] of TOKEN_TAGS) {
    list.forEach((tag, index) => {
        if (!tag) throw new Error(`language-modes: unknown highlight tag for ${type}[${index}]`);
    });
}

export const TOKEN_TYPES = TOKEN_TAGS.map(([type]) => type);

// Editor highlighting reuses the Source Preview classes verbatim rather than
// generating its own, which is what makes the two panels the same system: the
// neutral stone token colors in modals.css are defined once and applied in both.
const EDITOR_HIGHLIGHT_STYLE = HighlightStyle.define(
    TOKEN_TAGS.flatMap(([type, list]) => list.map((tag) => ({ tag, class: `source-token source-token-${type}` }))),
);

// The Preview side wants the bare token type, which the renderer then validates
// against its own allowlist before it becomes a class.
const PREVIEW_HIGHLIGHTER = tagHighlighter(
    TOKEN_TAGS.flatMap(([type, list]) => list.map((tag) => ({ tag, class: type }))),
);

// --- the registry's language ids ----------------------------------------------

const FACTORIES = {
    // The GFM variant, matching what Markdown Preview renders.
    markdown: () => markdownLanguage,
    json: () => jsonLanguage,
    yaml: () => yamlLanguage,
    toml: () => StreamLanguage.define(withoutIDEMetadata(tomlMode)),
    xml: () => xmlLanguage,
    // Already mixed: htmlLanguage wraps the HTML parser with CSS and JavaScript
    // nesting, so a <style> or <script> body is highlighted as what it is.
    html: () => htmlLanguage,
    css: () => cssLanguage,
    javascript: () => javascriptLanguage,
    // The same @lezer/javascript parser in its TypeScript dialect. No separate
    // package, and emphatically not the TypeScript compiler.
    typescript: () => typescriptLanguage,
    shell: () => StreamLanguage.define(withoutIDEMetadata(shellMode)),
    properties: () => StreamLanguage.define(withoutIDEMetadata(propertiesMode)),
};

export const LANGUAGE_IDS = Object.keys(FACTORIES);

/**
 * Strips a legacy stream mode's IDE metadata before it becomes a Language.
 *
 * The bare `Language` objects were chosen over the `LanguageSupport` bundles
 * precisely to keep completion sources and auto-close out of language data. Legacy
 * modes smuggle the same things back in a different way: the shell mode ships an
 * 88-word `autocomplete` list and a `closeBrackets` config, which
 * `state.languageDataAt()` then exposes. No popup appears today because no
 * completion extension is mounted, but "no suggestion source is registered" is the
 * requirement, and a future extension would find one waiting.
 */
function withoutIDEMetadata(mode) {
    const stripped = { ...mode };
    delete stripped.autocomplete;
    delete stripped.closeBrackets;
    delete stripped.electricInput;
    delete stripped.electricChars;
    if (stripped.languageData) {
        const data = { ...stripped.languageData };
        delete data.autocomplete;
        delete data.closeBrackets;
        stripped.languageData = data;
    }
    return stripped;
}

function isSupported(id) {
    return typeof id === 'string' && Object.prototype.hasOwnProperty.call(FACTORIES, id);
}

// Resolved on first use and kept. The lang-* Language objects are module
// singletons already; the three StreamLanguage.define calls are not, and handing
// the Compartment a different Language instance per open would reconfigure the
// parser for no reason.
const built = new Map();

function entryFor(id) {
    if (!isSupported(id)) return null;
    let language = built.get(id);
    if (!language) {
        language = FACTORIES[id]();
        built.set(id, language);
    }
    // A Language is usable directly as an extension and exposes the parser the
    // Preview highlighter and the recovery pass both read.
    return { extension: language, language };
}

/**
 * The extension the adapter's language Compartment reconfigures to.
 *
 * An unknown or absent id resolves to no extension, which is also the >2 MiB
 * degraded state: plain editing, no parsing.
 */
export function resolveLanguage(id) {
    const entry = entryFor(id);
    if (!entry) return [];
    return [entry.extension, syntaxHighlighting(EDITOR_HIGHLIGHT_STYLE)];
}

// --- parsing ------------------------------------------------------------------
//
// One slot, keyed by content. Preview highlighting and Lezer possible issues are
// two readings of the same tree, and they run within one debounce of each other;
// without this they would parse the same document twice. Keyed by exact source, so
// a stale tree can never be returned for edited text.
// The ceiling on OUR OWN full-document parse.
//
// CodeMirror's editor highlighting is incremental and viewport-driven, so it is
// inherently bounded and stays as it is. This module's `parser.parse(source)` is a
// different thing: one synchronous pass over the whole document, used by Preview
// highlighting and the Lezer recovery pass. At 2 MiB — inside the assistance
// threshold — that measured ~358 ms of blocked main thread, which is exactly the
// unbounded synchronous UI-thread parsing the design forbids. The token-span cap
// does not help: it applies to the tokens the parse produces, after the parse.
//
// Above this, Preview is plain inert source and there are no Lezer possible issues.
// Editing and CodeMirror's own highlighting are unaffected.
// 131,072 UTF-16 code units — named `UNITS`, not `BYTES`, because that is what the
// check measures and what the parser walks. Calling it bytes would overstate the
// bound for any non-ASCII document.
//
// 128 KiB rather than 256: a token-dense 256 KiB document measured 60-100 ms and a
// CSS probe reached ~190 ms. Bounded, but a visible hitch on a panel switch. Halving
// it halves the worst case, at the cost of dropping *preview* highlighting between
// 128 KiB and 2 MiB — those documents still edit with CodeMirror's own incremental
// highlighting, and preview as plain inert source.
export const MAX_PARSE_UNITS = 128 * 1024;

/**
 * Reports what IDE metadata a mode actually exposes through language data.
 *
 * Exists so "no completion popup, suggestion source or hover documentation is
 * registered" can be *tested* rather than asserted in a comment. Legacy stream modes
 * in particular carry word lists and auto-close configuration that survive into
 * language data unless stripped, and nothing else in the app would notice.
 */
export function probeLanguageData(id, doc = '') {
    const extensions = resolveLanguage(id);
    if (!extensions.length) return { autocomplete: 0, closeBrackets: 0 };
    const state = EditorState.create({ doc, extensions });
    const at = Math.min(1, state.doc.length);
    return {
        autocomplete: state.languageDataAt('autocomplete', at).length,
        closeBrackets: state.languageDataAt('closeBrackets', at).length,
    };
}

function withinParseBudget(text) {
    // UTF-16 code units, which is what the parser walks. A cheap length check on
    // purpose: measuring UTF-8 bytes here would itself scan the document.
    return text.length <= MAX_PARSE_UNITS;
}

let cachedParse = null;

export function clearCache() {
    cachedParse = null;
}

function treeFor(source, id) {
    const entry = entryFor(id);
    if (!entry) return null;
    // Refused rather than attempted. Returning null is the same answer as "no
    // language", which both callers already handle by degrading to plain output.
    if (!withinParseBudget(source)) return null;
    if (cachedParse && cachedParse.id === id && cachedParse.source === source) return cachedParse.tree;
    const tree = entry.language.parser.parse(source);
    cachedParse = { id, source, tree };
    return tree;
}

// Line starts as UTF-16 offsets. Matches how the renderer splits the document, so
// a token's line number and its within-line offsets refer to the same string the
// renderer is about to put in a text node.
function lineStartsOf(text) {
    const starts = [0];
    for (let i = 0; i < text.length; i++) {
        if (text[i] === '\n') starts.push(i + 1);
    }
    return starts;
}

function lineIndexAt(starts, offset) {
    let lo = 0;
    let hi = starts.length - 1;
    while (lo < hi) {
        const mid = (lo + hi + 1) >> 1;
        if (starts[mid] <= offset) lo = mid;
        else hi = mid - 1;
    }
    return lo;
}

/**
 * Tokens for the Source Preview renderer: `{ line, from, to, type }` with a
 * 1-based line and UTF-16 offsets *within that line*.
 *
 * A token that crosses a newline — a block comment, a multi-line string — is split
 * per line, because the renderer builds one element per line and has no way to
 * span them.
 *
 * Collection stops one span past the cap rather than at it: the renderer decides
 * what to do about exceeding it, and it can only see that the cap was exceeded if
 * it receives cap+1. Nothing beyond that is accumulated, so a pathological
 * document costs a bounded array rather than a span per character.
 */
export function highlight(source, language, options = {}) {
    const text = typeof source === 'string' ? source : '';
    const tree = treeFor(text, language);
    if (!tree) return [];

    const limit = Math.max(1, Number(options.limit) || MAX_TOKEN_SPANS);
    const starts = lineStartsOf(text);
    const tokens = [];
    let full = false;

    highlightTree(tree, PREVIEW_HIGHLIGHTER, (from, to, classes) => {
        if (full) return;
        // tagHighlighter emits our own single-word class names, but a tag that
        // matched more than one rule arrives space-separated; the first wins.
        const space = classes.indexOf(' ');
        const type = space === -1 ? classes : classes.slice(0, space);
        let lineIndex = lineIndexAt(starts, from);
        let cursor = from;
        while (cursor < to && lineIndex < starts.length) {
            const lineStart = starts[lineIndex];
            const lineEnd = lineIndex + 1 < starts.length ? starts[lineIndex + 1] - 1 : text.length;
            const sliceEnd = Math.min(to, lineEnd);
            if (sliceEnd > cursor) {
                tokens.push({ line: lineIndex + 1, from: cursor - lineStart, to: sliceEnd - lineStart, type });
                if (tokens.length > limit) { full = true; return; }
            }
            // Past the newline: it belongs to no line's text.
            cursor = sliceEnd + 1;
            lineIndex += 1;
        }
    });

    return tokens;
}

/**
 * Non-authoritative findings from Lezer error recovery.
 *
 * HTML, CSS, JavaScript and TypeScript have no authoritative validator in this
 * release — their grammars are too forgiving and too dialect-ridden to promise
 * reliable errors — so what error recovery leaves in the tree is reported as a
 * **possible issue**: counted, navigable, and unable to trigger Save Anyway.
 *
 * Messages never quote source. An error node's extent is all Lezer knows; naming
 * the offending text would put document content in the drawer and in an
 * accessible name.
 */
export function possibleIssues(source, language, options = {}) {
    const text = typeof source === 'string' ? source : '';
    const tree = treeFor(text, language);
    if (!tree) return [];

    const limit = Math.max(0, Number(options.limit) || DIAGNOSTIC_LIMITS.collected);
    const index = createPositionIndex(text);
    const out = [];
    const cursor = tree.cursor();
    let previousFrom = -1;

    do {
        if (!cursor.type.isError) continue;
        // Recovery frequently leaves a zero-length error node inside the node it
        // already flagged. One finding per position is what a reader can act on.
        if (cursor.from === previousFrom) continue;
        previousFrom = cursor.from;
        out.push(makeDiagnostic({
            severity: SEVERITY.POSSIBLE,
            source: 'lezer',
            message: cursor.to > cursor.from
                ? 'Possible syntax error: unexpected input'
                : 'Possible syntax error: something appears to be missing here',
            start: index.at(cursor.from),
            end: cursor.to > cursor.from ? index.at(cursor.to) : null,
        }));
        if (out.length >= limit) break;
    } while (cursor.next());

    return out;
}

export const LanguageModes = {
    LANGUAGE_IDS: LANGUAGE_IDS.slice(),
    TOKEN_TYPES: TOKEN_TYPES.slice(),
    isSupported,
    resolve: resolveLanguage,
    // Passed by reference into SourcePreviewRenderer.create(), so these must not
    // depend on `this`.
    highlight,
    possibleIssues,
    clearCache,
    MAX_PARSE_UNITS,
    // Test-visible so the "no completion source is registered" requirement is
    // checkable rather than assumed.
    probeLanguageData,
};
