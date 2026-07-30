// The shared diagnostic shape, position conversion, and collection limits.
//
// Every validator adapter produces this one structure, and every position in it
// is a **1-based line and a 1-based column measured in UTF-16 code units**,
// matching CodeMirror's document model — because every consumer of a position
// (marker placement, selectRange, revealRange) is CodeMirror.
//
// The parsers do not agree with each other or with CodeMirror on this: `saxes`
// reports code points alongside code units, `yaml` reports code-unit offsets into
// the whole source, `smol-toml` reports its own line/column pair, and JSON.parse
// reports a code-unit index into the whole string when it reports anything at all.
// Each adapter converts at its own boundary; nothing unconverted reaches here.

export const SEVERITY = {
    // Produced by an authoritative format validator. May trigger Save Anyway.
    ERROR: 'error',
    // Produced from recovery markers and renderer failures. Never confirms.
    POSSIBLE: 'possible-issue',
};

export const DIAGNOSTIC_LIMITS = {
    // Validator collection stops here; the drawer still presents only the first
    // `presented` and reports the suppressed remainder.
    collected: 1000,
    presented: 100,
    // YAML: alias and node counts bound a call the fallback executor cannot
    // preempt, so they do more work there than in the worker.
    yamlDocuments: 64,
    yamlAliasesPerDocument: 100,
    yamlNodes: 100000,
    // XML: depth and combined element/attribute events.
    xmlDepth: 256,
    xmlEvents: 100000,
    // CSV/TSV: records and parsed fields. Enforced inside the parse loop, not
    // applied to a finished result — a 2 MiB document of nothing but delimiters is
    // one record holding two million fields, and a cap that trimmed an
    // already-materialized array would have allocated it first. Table
    // *presentation* stops far earlier; see TABLE_LIMITS in delimited.js.
    csvRecords: 100000,
    csvFields: 1000000,
    // Guards the recursive strict-JSON position scanner against a stack overflow.
    jsonDepth: 512,
    // Formatting depth. Indented JSON amplifies quadratically in its nesting
    // depth — every line at depth d carries 2*d spaces — so a valid 64 KiB input of
    // nothing but `[` would ask for a ~2 GB result. That is unbounded work inside a
    // single uninterruptible unit, which is exactly what the fallback executor's
    // byte ceiling cannot save the UI thread from. The pre-existing
    // JSON.stringify(value, null, 2) had the same amplification.
    formatDepth: 256,
    // The real bound on formatting work. Depth alone is not enough: 256 nested
    // arrays holding 32,512 zeros is a valid 64 KiB document that indents to
    // ~17 MB, because every one of those siblings carries 512 spaces. Formatting
    // refuses rather than materializing a result this far beyond its source.
    //
    // Counted in UTF-16 code units, not bytes — that is what the builder actually
    // accumulates, and naming it `Bytes` would overstate the guarantee for
    // non-ASCII documents. Memory stays bounded either way.
    formatOutputUnits: 4 * 1024 * 1024,
};

// A message must never carry a source excerpt into the drawer: several parsers
// build "pretty" messages that embed the offending line, and a `.env` value or a
// large fragment must not leak into the UI or an accessible name.
//
// Dropping everything after the first newline was not enough. Parsers embed document
// content *on that first line*, quoted: `yaml` reports
// `Unexpected scalar token ... "hunter2"`, and JSON.parse quotes the offending token.
// So quoted runs are redacted here, at the one point every diagnostic passes through,
// rather than in each adapter where the next parser upgrade would reintroduce it.
//
// What this deliberately does NOT do is redact unquoted structural identifiers — an
// XML tag name in `unclosed tag: config`, a JSON property name. Those are names, not
// values, they are what makes a message actionable, and stripping everything after a
// colon would leave "unclosed tag:". The guarantee is therefore precise: no quoted
// document content, and nothing longer than `max`. It is not a claim that no token
// from the document can ever appear.
const QUOTED_RUN = /("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`(?:[^`\\]|\\.)*`)/g;

// A quoted run this short is naming a token, not quoting content: the JSON scanner's
// `Unexpected '}'` and `Expected ',' or '}'` are the messages that make a finding
// actionable, and redacting them leaves `Unexpected …`. Two characters is enough for
// every delimiter and escape the bundled parsers name, and far too little to be a
// credential.
const NAMED_TOKEN_MAX = 2;

export function redactQuoted(message) {
    return String(message === undefined || message === null ? '' : message)
        .replace(QUOTED_RUN, (run) => (run.length - 2 <= NAMED_TOKEN_MAX ? run : '…'));
}

export function concise(message, max = 200) {
    const first = redactQuoted(message).split('\n')[0].trim();
    if (!first) return 'Syntax error';
    return first.length > max ? `${first.slice(0, max - 1)}…` : first;
}

/**
 * Maps UTF-16 code-unit offsets in a source string to 1-based line/column pairs.
 *
 * Built once per validation run: a linear scan for line starts plus a binary
 * search per lookup, rather than re-splitting the document for every finding.
 */
export function createPositionIndex(source) {
    const text = typeof source === 'string' ? source : '';
    const starts = [0];
    for (let i = 0; i < text.length; i++) {
        const ch = text[i];
        // LF, CRLF and lone CR all begin a new line. The editor value is always
        // LF-normalized, so in the app only the first case arises — but validators
        // are also driven directly (tests, and any future non-editor caller), and a
        // CR-only document would otherwise report every finding on line 1.
        if (ch === '\n') {
            starts.push(i + 1);
        } else if (ch === '\r') {
            if (text[i + 1] === '\n') i += 1;
            starts.push(i + 1);
        }
    }

    function lineStart(line) {
        const clamped = Math.max(1, Math.min(line, starts.length));
        return starts[clamped - 1];
    }

    function lineEnd(line) {
        const clamped = Math.max(1, Math.min(line, starts.length));
        return clamped === starts.length ? text.length : starts[clamped] - 1;
    }

    return {
        get lineCount() { return starts.length; },
        get length() { return text.length; },

        // offset (UTF-16 code units into the whole source) -> {line, column}
        at(offset) {
            const clamped = Math.max(0, Math.min(Number(offset) || 0, text.length));
            let lo = 0;
            let hi = starts.length - 1;
            while (lo < hi) {
                const mid = (lo + hi + 1) >> 1;
                if (starts[mid] <= clamped) lo = mid;
                else hi = mid - 1;
            }
            return { line: lo + 1, column: clamped - starts[lo] + 1 };
        },

        // For adapters that report a line/column pair directly. Clamped so a
        // parser's off-by-one at end-of-input cannot place a marker outside the
        // document.
        clamp(line, column) {
            const l = Math.max(1, Math.min(Math.trunc(Number(line) || 1), starts.length));
            const width = lineEnd(l) - lineStart(l);
            const c = Math.max(1, Math.min(Math.trunc(Number(column) || 1), width + 1));
            return { line: l, column: c };
        },
    };
}

export function makeDiagnostic({ severity, source, message, start, end }) {
    return {
        severity: severity === SEVERITY.ERROR ? SEVERITY.ERROR : SEVERITY.POSSIBLE,
        source: source || 'unknown',
        message: concise(message),
        line: start ? start.line : 1,
        column: start ? start.column : 1,
        endLine: end ? end.line : null,
        endColumn: end ? end.column : null,
    };
}

/**
 * Bounded diagnostic collection.
 *
 * Stops at `DIAGNOSTIC_LIMITS.collected` findings and records that it did, so a
 * pathological document produces a bounded result rather than an unbounded array.
 */
export function createCollector(format, limit = DIAGNOSTIC_LIMITS.collected) {
    const items = [];
    let truncated = false;

    return {
        get length() { return items.length; },
        /**
         * Adds a finding. Returns false once the limit is reached — which is also
         * what records the truncation, so producers must stop on a false return
         * rather than pre-checking a "full" flag. Pre-checking would stop the
         * producer one finding early and leave the truncation unreported.
         */
        add(diagnostic) {
            if (items.length >= limit) {
                truncated = true;
                return false;
            }
            items.push(diagnostic);
            return true;
        },
        error(source, message, start, end) {
            return this.add(makeDiagnostic({ severity: SEVERITY.ERROR, source, message, start, end }));
        },
        /**
         * A nonblocking finding from a non-authoritative validator.
         *
         * CSV and TSV use only this. It is not a severity a caller chooses per
         * finding: those formats have no single authoritative grammar, so malformed
         * quoting and ragged widths are possible issues whether the delimiter was
         * inferred or explicitly chosen, and none of them can ever reach the save
         * confirmation.
         */
        possible(source, message, start, end) {
            return this.add(makeDiagnostic({ severity: SEVERITY.POSSIBLE, source, message, start, end }));
        },
        result() {
            return { format, diagnostics: items, truncated, unavailable: null };
        },
        /**
         * A resource limit or a parser exception establishes no authoritative
         * answer. It must never be presented as "the document is valid", and it
         * never triggers Save Anyway — findings gathered before the limit are
         * dropped because an incomplete set would make the change-based save
         * comparison lie.
         */
        unavailable(reason) {
            return { format, diagnostics: [], truncated: false, unavailable: reason || 'limit' };
        },
    };
}
