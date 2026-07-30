// The single source of truth for "what kind of text is this clip?".
//
// Callers ask for a descriptor; they never repeat an extension or MIME check.
// Classification decides presentation and editor eligibility — it never rewrites
// a clip's stored content type.

// Preview kind also determines the default open mode. The three modes whose
// Preview is a materially different artifact (rendered GFM, a parsed table) open
// in Preview; every read-only-highlighted-source mode opens in Edit, because
// landing on a panel that looks like the editor but refuses typing costs a click
// and invites "why can't I type here".
const PREVIEW_SOURCE = 'source';
const PREVIEW_MARKDOWN = 'markdown';
const PREVIEW_TABLE = 'table';

// A filename match does not confer text candidacy when the content type is a
// known media family. Extensions collide across domains — `.ts` is both
// TypeScript and MPEG transport stream, and several platforms sniff it as
// video/mp2t — and without this guard opening a transport-stream video would
// enter the text editor and dead-end on the byte-safety screen.
const MEDIA_MIME_PREFIXES = ['image/', 'audio/', 'video/'];

// Deliberately NOT a media family. It is the generic "I don't know" type that
// uploads and watch folders routinely produce, so blocking it would make a
// perfectly good config.yaml or notes.md uneditable — a regression, not a guard.
// A recognized text filename under it is a candidate *conditionally*: the
// retrieval path's fatal UTF-8 decode decides. This costs nothing extra because
// valid_utf8 is already computed for every clip.
const OCTET_STREAM = 'application/octet-stream';

// Generic plain-text extensions. Held apart from every descriptor's `extensions`
// because they are matched at step 3, *after* specific MIME: a pasted
// `pasted_text_<timestamp>.txt` clip that the backend identified as
// application/json must keep JSON behavior.
const GENERIC_TEXT_EXTENSIONS = ['.txt', '.text', '.log'];

function descriptor(spec) {
    const preview = spec.preview || PREVIEW_SOURCE;
    return {
        id: spec.id,
        label: spec.label,
        // CodeMirror language mode key, chosen from this trusted result — never
        // from automatic language detection.
        language: spec.language || null,
        preview,
        defaultMode: preview === PREVIEW_SOURCE ? 'edit' : 'preview',
        // For table previews only: the delimiter this format is biased toward
        // before detection runs. Registry-driven so nothing downstream has to
        // switch on a format id.
        tableDelimiter: spec.tableDelimiter || null,
        validator: spec.validator || null,
        // Whether this validator's findings are authoritative errors (able to
        // trigger Save Anyway) or non-blocking possible issues.
        authoritative: !!spec.authoritative,
        formatter: spec.formatter || null,
        extensions: spec.extensions || [],
        basenames: spec.basenames || [],
        // Whole-basename predicates for families a literal list cannot express.
        basenameMatch: spec.basenameMatch || null,
        mimes: spec.mimes || [],
        // Structured-suffix matches such as application/*+json. A suffix match,
        // not a general glob implementation.
        mimeSuffixes: spec.mimeSuffixes || [],
        generic: !!spec.generic,
    };
}

export const PLAIN_TEXT_ID = 'plaintext';

const DESCRIPTORS = [
    descriptor({
        id: 'markdown',
        label: 'Markdown',
        language: 'markdown',
        preview: PREVIEW_MARKDOWN,
        // The renderer can fail; nothing else about Markdown is validated.
        validator: 'markdown-renderer',
        extensions: ['.md', '.markdown'],
        mimes: ['text/markdown'],
    }),
    descriptor({
        id: 'json',
        label: 'JSON',
        language: 'json',
        validator: 'json',
        authoritative: true,
        formatter: 'json',
        extensions: ['.json'],
        mimes: ['application/json'],
        mimeSuffixes: ['application/+json'],
    }),
    descriptor({
        id: 'jsonl',
        label: 'JSON Lines',
        language: 'json',
        validator: 'jsonl',
        authoritative: true,
        formatter: 'jsonl',
        extensions: ['.jsonl', '.ndjson'],
        mimes: ['application/x-ndjson', 'application/jsonl'],
    }),
    descriptor({
        id: 'yaml',
        label: 'YAML',
        language: 'yaml',
        validator: 'yaml',
        authoritative: true,
        extensions: ['.yaml', '.yml'],
        mimes: ['application/yaml', 'application/x-yaml', 'text/yaml', 'text/x-yaml'],
    }),
    descriptor({
        id: 'toml',
        label: 'TOML',
        language: 'toml',
        validator: 'toml',
        authoritative: true,
        extensions: ['.toml'],
        mimes: ['application/toml', 'text/toml'],
    }),
    descriptor({
        id: 'xml',
        label: 'XML',
        language: 'xml',
        validator: 'xml',
        authoritative: true,
        extensions: ['.xml'],
        mimes: ['application/xml', 'text/xml'],
        mimeSuffixes: ['application/+xml'],
    }),
    descriptor({
        id: 'csv',
        label: 'CSV',
        language: null,
        preview: PREVIEW_TABLE,
        // Not authoritative in any state: CSV has no single authoritative
        // grammar, so every finding is a possible issue and none can block a save.
        validator: 'csv',
        extensions: ['.csv'],
        mimes: ['text/csv'],
    }),
    descriptor({
        id: 'tsv',
        label: 'TSV',
        language: null,
        preview: PREVIEW_TABLE,
        // TSV initially prefers tab. Detection still runs and still rescues a
        // `.tsv` file that is really comma-separated.
        tableDelimiter: 'tab',
        validator: 'tsv',
        extensions: ['.tsv'],
        mimes: ['text/tab-separated-values'],
    }),
    descriptor({
        id: 'html',
        label: 'HTML',
        language: 'html',
        // Lezer error recovery only — never authoritative, never rendered.
        validator: 'lezer',
        extensions: ['.html', '.htm'],
        mimes: ['text/html'],
    }),
    descriptor({
        id: 'css',
        label: 'CSS',
        language: 'css',
        validator: 'lezer',
        extensions: ['.css'],
        mimes: ['text/css'],
    }),
    descriptor({
        id: 'javascript',
        label: 'JavaScript',
        language: 'javascript',
        validator: 'lezer',
        extensions: ['.js', '.mjs', '.cjs'],
        mimes: ['text/javascript', 'application/javascript'],
    }),
    descriptor({
        id: 'typescript',
        label: 'TypeScript',
        language: 'typescript',
        validator: 'lezer',
        extensions: ['.ts', '.mts', '.cts'],
        mimes: ['text/typescript', 'application/typescript'],
    }),
    descriptor({
        id: 'shell',
        label: 'Shell',
        language: 'shell',
        // Highlighting only. No shell parser is bundled: every finding one could
        // produce is advisory by design, and it would have to assume Bash syntax
        // even for .zsh.
        extensions: ['.sh', '.bash', '.zsh'],
        mimes: ['text/x-shellscript', 'application/x-sh'],
    }),
    descriptor({
        id: 'env',
        label: 'Environment',
        language: 'properties',
        // Exact `.env` plus `.env.*` variants (.env.local, .env.example). A
        // filename merely *containing* ".env" does not match.
        basenames: ['.env'],
        basenameMatch: (basename) => basename.startsWith('.env.'),
    }),
    descriptor({
        id: 'properties',
        label: 'Properties',
        language: 'properties',
        extensions: ['.ini', '.cfg', '.conf'],
        mimes: ['text/x-ini', 'text/x-properties'],
    }),
    descriptor({
        id: PLAIN_TEXT_ID,
        label: 'Plain text',
        language: null,
        generic: true,
        // Its extensions live in GENERIC_TEXT_EXTENSIONS, not here, so they
        // cannot win step 1.
    }),
];

const BY_ID = new Map(DESCRIPTORS.map((d) => [d.id, d]));
const PLAIN_TEXT = BY_ID.get(PLAIN_TEXT_ID);

// --- normalization ------------------------------------------------------------

// Treat both separators regardless of host platform: a clip filename is data,
// and a Windows path can arrive on a Unix host and vice versa.
export function basenameOf(filename) {
    const raw = typeof filename === 'string' ? filename : '';
    let cut = -1;
    for (let i = raw.length - 1; i >= 0; i--) {
        const ch = raw[i];
        if (ch === '/' || ch === '\\') { cut = i; break; }
    }
    return cut === -1 ? raw : raw.slice(cut + 1);
}

// Filename whitespace is preserved (it is part of the name); only the comparison
// form is folded. toLowerCase is locale-independent by specification, unlike
// toLocaleLowerCase, which would fold Turkish dotted I differently per host.
export function normalizeFilename(filename) {
    const base = basenameOf(filename);
    const normalized = typeof base.normalize === 'function' ? base.normalize('NFC') : base;
    return normalized.toLowerCase();
}

// Only the final extension counts: `archive.json.gz` is not JSON.
export function finalExtension(normalizedBasename) {
    const dot = normalizedBasename.lastIndexOf('.');
    // A leading dot is a dotfile, not an extension (`.env` has no extension).
    if (dot <= 0) return '';
    return normalizedBasename.slice(dot);
}

export function normalizeContentType(contentType) {
    if (typeof contentType !== 'string') return '';
    let value = contentType.trim();
    const semi = value.indexOf(';');
    if (semi >= 0) value = value.slice(0, semi).trim();
    value = value.toLowerCase();
    // An empty or malformed value is unknown, not a match.
    if (!value || !/^[a-z0-9!#$&^_.+-]+\/[a-z0-9!#$&^_.+-]+$/.test(value)) return '';
    return value;
}

function isMediaMIME(mime) {
    return MEDIA_MIME_PREFIXES.some((prefix) => mime.startsWith(prefix));
}

function matchesSuffix(mime, pattern) {
    // 'application/+json' means: type application, subtype ending in '+json'.
    const slash = pattern.indexOf('/');
    const type = pattern.slice(0, slash);
    const suffix = pattern.slice(slash + 1);
    if (!mime.startsWith(`${type}/`)) return false;
    const subtype = mime.slice(type.length + 1);
    return subtype.length > suffix.length && subtype.endsWith(suffix);
}

// --- classification ----------------------------------------------------------

function matchByBasename(basename) {
    for (const d of DESCRIPTORS) {
        if (d.basenames.includes(basename)) return d;
        if (d.basenameMatch && d.basenameMatch(basename)) return d;
    }
    return null;
}

function matchByExtension(extension) {
    if (!extension) return null;
    for (const d of DESCRIPTORS) {
        if (d.extensions.includes(extension)) return d;
    }
    return null;
}

function matchByMIME(mime) {
    if (!mime) return null;
    for (const d of DESCRIPTORS) {
        if (d.mimes.includes(mime)) return d;
    }
    for (const d of DESCRIPTORS) {
        if (d.mimeSuffixes.some((pattern) => matchesSuffix(mime, pattern))) return d;
    }
    return null;
}

/**
 * Classifies a clip.
 *
 * The order is fixed and each step is deliberate:
 *   1. a specific recognized whole basename or final extension;
 *   2. a specific recognized MIME type;
 *   3. a generic plain-text filename (.txt, .text, .log);
 *   4. generic text for any remaining text/* MIME;
 *   5. not a text-editor type.
 *
 * Specific filenames therefore beat a disagreeing MIME, while the generic
 * plain-text family deliberately does not outrank a specific MIME.
 */
export function classify({ filename, contentType } = {}) {
    const basename = normalizeFilename(filename);
    const extension = finalExtension(basename);
    const mime = normalizeContentType(contentType);
    const media = isMediaMIME(mime);
    const conditional = mime === OCTET_STREAM;

    const result = (descriptorMatch, matchedBy) => ({
        descriptor: descriptorMatch,
        matchedBy: descriptorMatch ? matchedBy : null,
        // True when a recognized filename was suppressed by a media content type.
        mediaGuard: !!descriptorMatch && media && (matchedBy === 'basename' || matchedBy === 'extension' || matchedBy === 'generic-filename'),
        // True when candidacy rests on the fatal UTF-8 decode rather than on the
        // content type saying anything useful.
        conditional: !!descriptorMatch && conditional,
        basename,
        extension,
        contentType: mime,
    });

    const byBasename = matchByBasename(basename);
    if (byBasename) return result(byBasename, 'basename');

    const byExtension = matchByExtension(extension);
    if (byExtension) return result(byExtension, 'extension');

    const byMIME = matchByMIME(mime);
    if (byMIME) return result(byMIME, 'mime');

    if (GENERIC_TEXT_EXTENSIONS.includes(extension)) return result(PLAIN_TEXT, 'generic-filename');

    if (mime.startsWith('text/')) return result(PLAIN_TEXT, 'generic-mime');

    return result(null, null);
}

/**
 * Whether the text editor path applies to this clip.
 *
 * Callers that gate the editor *as a whole* rather than the text path
 * specifically must OR this with their own image check — the existing
 * isEditableType also returns true for image/*, and narrowing it to text-only
 * would silently remove image editing.
 */
export function isTextCandidate(input) {
    const { descriptor: match, mediaGuard } = classify(input);
    return !!match && !mediaGuard;
}

export function getDescriptor(id) {
    return BY_ID.get(id) || null;
}

export function allDescriptors() {
    return DESCRIPTORS.slice();
}

export const TextFileTypes = {
    classify,
    isTextCandidate,
    getDescriptor,
    allDescriptors,
    basenameOf,
    normalizeFilename,
    normalizeContentType,
    finalExtension,
    PLAIN_TEXT_ID,
    GENERIC_TEXT_EXTENSIONS: GENERIC_TEXT_EXTENSIONS.slice(),
    MEDIA_MIME_PREFIXES: MEDIA_MIME_PREFIXES.slice(),
    OCTET_STREAM,
};
