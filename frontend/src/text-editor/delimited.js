// The CSV/TSV dialect: parsing, delimiter detection, and header detection.
//
// This module is imported by BOTH bundles, and deliberately so. The table
// Preview renderer runs on the main thread because it produces DOM; the CSV/TSV
// validator runs in the executor because it produces diagnostics. If the two
// interpreted the same bytes differently — one splitting on semicolons, the other
// on commas — the drawer would report ragged rows the table does not show, which
// is worse than either alone. One implementation is what makes that impossible.
//
// Nothing here touches the DOM, `window`, `self`, or the network.
//
// The dialect, stated once: `"` quotes a field, `""` inside a quoted field is one
// literal quote, and LF, CRLF or CR all terminate a record. A record terminator
// at the very end of the document produces no trailing empty record — that record
// is an artifact of the final newline. An interior blank line DOES produce one,
// because a blank line in the middle of a document is data.

import { DIAGNOSTIC_LIMITS } from './diagnostics.js';

const QUOTE = '"';
const CR = '\r';
const LF = '\n';

// The order of this array IS the stable tie order the design specifies for
// delimiter detection: comma, tab, semicolon, pipe. Reordering it changes which
// delimiter wins an otherwise perfect tie, so it is not cosmetic.
export const DELIMITERS = [
    { id: 'comma', value: ',', label: 'Comma' },
    { id: 'tab', value: '\t', label: 'Tab' },
    { id: 'semicolon', value: ';', label: 'Semicolon' },
    { id: 'pipe', value: '|', label: 'Pipe' },
].map((spec, order) => ({ ...spec, order }));

const BY_ID = new Map(DELIMITERS.map((spec) => [spec.id, spec]));

export function delimiterValueFor(id) {
    const spec = BY_ID.get(id);
    return spec ? spec.value : null;
}

export function delimiterLabelFor(value) {
    const spec = DELIMITERS.find((candidate) => candidate.value === value);
    return spec ? spec.label : 'Comma';
}

export function delimiterLabelForID(id) {
    const spec = BY_ID.get(id);
    return spec ? spec.label : 'Comma';
}

// Table limits. Two distinct sets, and the distinction matters: parsing is
// bounded so a pathological document cannot exhaust memory, while *presentation*
// stops far earlier because a 100,000-row table is not a preview. The parse caps
// live in DIAGNOSTIC_LIMITS alongside the YAML node and XML event caps, because
// they are the same kind of thing: a validator resource bound.
export const TABLE_LIMITS = {
    // Delimiter detection parses at most this much of the document, in UTF-8
    // bytes, with each candidate...
    detectionBytes: 64 * 1024,
    // ...or this many nonblank records, whichever comes first.
    detectionRecords: 50,
    // Table rendering stops at the first of these it reaches.
    renderRows: 500,
    renderColumns: 100,
    renderCells: 10000,
};

export const ERR_UNTERMINATED_QUOTE = 'unterminated-quote';
export const ERR_INVALID_QUOTE = 'invalid-quote';

// Blanks between a closing quote and the next delimiter or record end. `"a" ,b` is
// tolerated rather than reported: Papa Parse accepts it, real exporters emit it, and
// inventing an error here feeds straight into the ranking's first criterion and can
// hand the contest to the wrong delimiter.
function isQuotePadding(text, from, end, delimiter) {
    let j = from;
    while (j < end && (text[j] === ' ' || text[j] === '\t') && text[j] !== delimiter) j += 1;
    return j > from && (j >= end || text[j] === delimiter || isRecordEnd(text[j]));
}

function positive(value, fallback) {
    const number = Number(value);
    return Number.isFinite(number) && number > 0 ? number : fallback;
}

// A record consisting of exactly one empty field is a blank line. It is kept as
// data, but excluded from width statistics: a blank line is blank for every
// candidate delimiter, so counting it would add the same noise to all of them
// while dragging every modal width toward 1.
export function isBlankRecord(record) {
    // `quoted` is set by the parser when the record's single field came from a
    // quoted literal. `""` on its own line is a deliberate empty *value*, not a
    // blank line, so it must count toward width statistics and the nonblank-record
    // scan bound — otherwise 50 such records let an unread tail decide the winner.
    return record.length === 1 && record[0] === '' && !record.quoted;
}

/**
 * The detection prefix length, measured in UTF-8 bytes but returned in UTF-16
 * code units — because the bound is specified in bytes and the parser indexes in
 * code units. Slicing at 65,536 code units instead would be up to three times the
 * intended bound for CJK text and four times for astral characters.
 */
function utf8BoundedLength(text, maxBytes) {
    let bytes = 0;
    let i = 0;
    while (i < text.length) {
        const code = text.charCodeAt(i);
        let size = 3;
        let step = 1;
        if (code < 0x80) size = 1;
        else if (code < 0x800) size = 2;
        else if (code >= 0xd800 && code <= 0xdbff && i + 1 < text.length) { size = 4; step = 2; }
        if (bytes + size > maxBytes) return i;
        bytes += size;
        i += step;
    }
    return text.length;
}

/**
 * Parses `source` in the supported dialect.
 *
 * Every bound is enforced inside the loop, not applied to a finished result. That
 * is the whole point: a 2 MiB document of nothing but delimiters is one record
 * holding two million fields, and a cap that only trims an already-materialized
 * array would have allocated it first.
 *
 * Options:
 *   delimiter          - the single character to split fields on (default `,`)
 *   maxRecords         - stop after this many records
 *   maxFields          - stop after this many parsed fields
 *   maxNonblankRecords - stop after this many records that are not blank lines
 *   stopAtOffset       - stop at this UTF-16 offset (the detection prefix)
 *
 * Returns `{ records, starts, errors, fieldCount, limit, bounded }` where
 * `starts[n]` is the UTF-16 offset of record n's first character, `errors` carry
 * raw offsets for the caller to convert, `limit` names the bound that stopped the
 * parse (or null), and `bounded` reports that `stopAtOffset` cut the document.
 */
export function parseDelimited(source, options = {}) {
    const text = typeof source === 'string' ? source : '';
    const delimiter = typeof options.delimiter === 'string' && options.delimiter.length === 1
        ? options.delimiter
        : ',';
    const maxRecords = positive(options.maxRecords, DIAGNOSTIC_LIMITS.csvRecords);
    const maxFields = positive(options.maxFields, DIAGNOSTIC_LIMITS.csvFields);
    const maxNonblank = positive(options.maxNonblankRecords, Infinity);
    const end = Math.min(text.length, positive(options.stopAtOffset, text.length));

    const records = [];
    const starts = [];
    const errors = [];
    let fields = [];
    let recordStart = 0;
    let fieldCount = 0;
    let nonblank = 0;
    let stopReason = null;
    let i = 0;
    // True whenever the cursor sits at the start of a field that has not been read
    // yet. It exists for one case the loop condition cannot see: a document ending
    // in a delimiter, where the final empty field is real data with no character of
    // its own to be read at.
    let fieldPending = true;

    const isRecordEnd = (ch) => ch === LF || ch === CR;

    // Set for each field as it is read, so a record of one *quoted* empty field can
    // be told apart from a physically blank line. See isBlankRecord().
    let fieldWasQuoted = false;

    while (i < end) {
        // --- one field --------------------------------------------------------
        let value;
        fieldWasQuoted = text[i] === QUOTE;
        if (text[i] === QUOTE) {
            const openQuote = i;
            i += 1;
            let start = i;
            // Only allocated when a doubled quote forces the field to be rebuilt;
            // the common case is a single slice.
            let pieces = null;
            let closed = false;
            while (i < end) {
                if (text[i] !== QUOTE) { i += 1; continue; }
                if (i + 1 < end && text[i + 1] === QUOTE) {
                    // The slice deliberately includes the first of the pair, which
                    // is the one literal quote the escape stands for.
                    if (!pieces) pieces = [];
                    pieces.push(text.slice(start, i + 1));
                    i += 2;
                    start = i;
                    continue;
                }
                value = pieces ? pieces.concat(text.slice(start, i)).join('') : text.slice(start, i);
                i += 1;
                closed = true;
                break;
            }
            if (!closed) {
                value = pieces ? pieces.concat(text.slice(start, end)).join('') : text.slice(start, end);
                errors.push({ code: ERR_UNTERMINATED_QUOTE, offset: openQuote });
                i = end;
            } else if (i < end && text[i] !== delimiter && !isRecordEnd(text[i])
                       && !isQuotePadding(text, i, end, delimiter)) {
                // Text after the closing quote. Absorbing it into the same field is
                // the only recovery that keeps every later record aligned; bailing
                // out here would turn one malformed field into a wrong table.
                errors.push({ code: ERR_INVALID_QUOTE, offset: i });
                const tailStart = i;
                while (i < end && text[i] !== delimiter && !isRecordEnd(text[i])) i += 1;
                value += text.slice(tailStart, i);
            }
        } else {
            const start = i;
            while (i < end && text[i] !== delimiter && !isRecordEnd(text[i])) i += 1;
            value = text.slice(start, i);
        }

        fields.push(value);
        if (fieldWasQuoted) fields.quoted = true;
        fieldCount += 1;
        fieldPending = false;
        if (fieldCount >= maxFields) { stopReason = 'fields'; break; }

        // --- what follows it --------------------------------------------------
        if (i >= end) break;
        if (text[i] === delimiter) { i += 1; fieldPending = true; continue; }

        // A record terminator: LF, CR, or CRLF counted once.
        i += (text[i] === CR && i + 1 < end && text[i + 1] === LF) ? 2 : 1;
        records.push(fields);
        starts.push(recordStart);
        if (!isBlankRecord(fields)) nonblank += 1;
        fields = [];
        recordStart = i;
        fieldPending = true;
        if (records.length >= maxRecords) { stopReason = 'records'; break; }
        if (nonblank >= maxNonblank) { stopReason = 'nonblank'; break; }
    }

    // `a,b,` ends on a delimiter, so its third field is empty and has no character
    // to be read at. The `fields.length > 0` guard is what keeps this from
    // manufacturing a field for an empty document or straight after a terminator,
    // where the same flag is set for a record that never started.
    if (fieldPending && fields.length > 0) {
        fields.push('');
        fieldCount += 1;
    }

    // Reaching here with fields still buffered means the document did not end on a
    // record terminator — every terminator clears the buffer. That is exactly the
    // distinction between a real final record and the empty one a trailing newline
    // would otherwise manufacture.
    if (fields.length > 0) {
        records.push(fields);
        starts.push(recordStart);
    }

    return {
        records,
        starts,
        errors,
        fieldCount,
        // A bound only truncated the document if there was still input left when it
        // fired. A document of exactly maxRecords records is complete, not capped.
        limit: stopReason && i < end ? stopReason : null,
        bounded: end < text.length,
        // True when the scan consumed the whole prefix and the last record ended on
        // a terminator, so nothing was cut mid-record.
        completedAtEnd: i >= end && fields.length === 0,
    };
}

/**
 * The most frequent record width, and how many records differ from it.
 *
 * A frequency tie takes the wider width, so the answer never depends on Map
 * insertion order — which would make delimiter detection depend on the order
 * widths happened to appear in the document.
 */
function modalWidthOf(widths) {
    if (!widths.length) return { modalWidth: 0, deviants: 0 };
    const counts = new Map();
    for (const width of widths) counts.set(width, (counts.get(width) || 0) + 1);
    let modalWidth = 0;
    let best = -1;
    for (const [width, count] of counts) {
        if (count > best || (count === best && width > modalWidth)) {
            best = count;
            modalWidth = width;
        }
    }
    return { modalWidth, deviants: widths.length - best };
}

function scoreCandidate(text, spec, stopAtOffset) {
    const parsed = parseDelimited(text, {
        delimiter: spec.value,
        stopAtOffset,
        maxNonblankRecords: TABLE_LIMITS.detectionRecords,
    });

    let records = parsed.records;
    let errors = parsed.errors;
    // The byte prefix can cut mid-record, producing a short final record and
    // possibly a phantom unterminated quote. Both would corrupt the contest — the
    // ranking's first two criteria are error count and width deviation — so the
    // partial record and anything reported inside it are discarded. This does not
    // apply when the parse stopped on its own record bound, where the last record
    // is complete.
    if (parsed.bounded && !parsed.limit && records.length > 0) {
        // Only discard a final record that was actually cut. One that ended exactly
        // at the boundary is complete, and throwing it away was enough on its own to
        // make an otherwise-clear winner look inconclusive.
        const cut = parsed.starts[records.length - 1];
        if (parsed.completedAtEnd) {
            // Nothing was truncated; keep every record and every finding.
        } else {
            records = records.slice(0, -1);
            errors = errors.filter((error) => error.offset < cut);
        }
    }

    const widths = records.filter((record) => !isBlankRecord(record)).map((record) => record.length);
    const { modalWidth, deviants } = modalWidthOf(widths);
    return {
        id: spec.id,
        delimiter: spec.value,
        order: spec.order,
        errors: errors.length,
        modalWidth,
        deviants,
        records: widths.length,
    };
}

/**
 * Picks a delimiter.
 *
 * Candidates are comma, tab, semicolon and pipe. Each is scored against at most
 * the first 64 KiB or 50 nonblank records. A candidate whose modal-width record
 * holds fewer than two fields is discarded, because "one field per line" is what
 * every wrong delimiter looks like.
 *
 * The winner is chosen by, in this order: fewest parse errors, fewest records
 * differing from the modal width, largest modal width, then the stable tie order
 * comma, tab, semicolon, pipe. If nothing qualifies, comma is used and the caller
 * is told detection was inconclusive so Preview can say so.
 *
 * `preferredID` is the descriptor's own bias — tab for TSV. A preferred candidate
 * that qualifies at all wins outright rather than merely breaking ties: a `.tsv`
 * file whose rows also contain commas is still tab-separated, and running the
 * contest would let the commas win it. A preferred candidate that does NOT
 * qualify falls through to the ordinary contest, which is what rescues a `.tsv`
 * file that is really comma-separated.
 */
export function detectDelimiter(source, options = {}) {
    const text = typeof source === 'string' ? source : '';
    const stopAtOffset = utf8BoundedLength(text, TABLE_LIMITS.detectionBytes);
    const candidates = DELIMITERS.map((spec) => scoreCandidate(text, spec, stopAtOffset));
    const qualified = candidates.filter((candidate) => candidate.modalWidth >= 2);

    if (!qualified.length) {
        return { delimiter: ',', delimiterID: 'comma', inconclusive: true, candidates };
    }

    const preferred = options.preferredID ? qualified.find((c) => c.id === options.preferredID) : null;
    const winner = preferred || qualified.slice().sort((a, b) => (
        (a.errors - b.errors) ||
        (a.deviants - b.deviants) ||
        (b.modalWidth - a.modalWidth) ||
        (a.order - b.order)
    ))[0];

    return { delimiter: winner.delimiter, delimiterID: winner.id, inconclusive: false, candidates };
}

/**
 * Whether the first record is a header.
 *
 * Deliberately strict: at least two records, every record the same width of at
 * least two fields, and every trimmed first-record field nonempty and unique.
 * Anything looser starts eating the first row of ordinary data, which is the one
 * mistake a table preview must not make silently.
 *
 * Note that a blank line anywhere has width 1 and therefore rules out a header.
 * That is the literal rule and also the useful one: a document with blank lines
 * in it is not a clean table.
 */
export function detectHeader(records) {
    if (!Array.isArray(records) || records.length < 2) return false;
    const width = records[0].length;
    if (width < 2) return false;
    for (const record of records) {
        if (record.length !== width) return false;
    }
    const seen = new Set();
    for (const field of records[0]) {
        const value = String(field).trim();
        if (!value || seen.has(value)) return false;
        seen.add(value);
    }
    return true;
}

/**
 * The single interpretation of a delimited document.
 *
 * Both the validator and the table renderer call this, which is what keeps the
 * drawer's findings and the rendered table talking about the same table.
 *
 * `delimiterID` is the user's temporary override; `headerMode` is `'auto'`,
 * `'on'` or `'off'`. Neither changes the source, and neither is persisted — they
 * are arguments, not state, precisely so nothing can accidentally keep them.
 */
export function analyzeDelimited(source, options = {}) {
    const text = typeof source === 'string' ? source : '';
    const chosen = options.delimiterID ? delimiterValueFor(options.delimiterID) : null;
    // Detection runs even when the user has overridden it. Skipping it would be
    // marginally cheaper and would leave the control unable to say what detection
    // thought — an "auto" option labelled with the user's own choice reads as
    // agreement, which is exactly what the user is trying to check.
    const detection = detectDelimiter(text, { preferredID: options.preferredID });
    const delimiter = chosen || detection.delimiter;
    const delimiterID = chosen ? options.delimiterID : detection.delimiterID;

    const parsed = parseDelimited(text, { delimiter });
    const widths = parsed.records.filter((record) => !isBlankRecord(record)).map((record) => record.length);
    const { modalWidth, deviants } = modalWidthOf(widths);
    const headerDetected = detectHeader(parsed.records);
    const mode = options.headerMode === 'on' || options.headerMode === 'off' ? options.headerMode : 'auto';
    const header = parsed.records.length > 0 && (mode === 'on' || (mode === 'auto' && headerDetected));

    return {
        ...parsed,
        delimiter,
        delimiterID,
        delimiterExplicit: !!chosen,
        detectedDelimiterID: detection.delimiterID,
        detectionInconclusive: !!detection.inconclusive,
        // An explicit choice resolves the question, so the "detection was
        // inconclusive" explanation stops applying once the user has answered it —
        // even though detection itself is still inconclusive, which is what
        // detectionInconclusive above keeps available for the control's label.
        inconclusive: !chosen && !!detection.inconclusive,
        modalWidth,
        deviants,
        headerDetected,
        headerMode: mode,
        header,
    };
}
