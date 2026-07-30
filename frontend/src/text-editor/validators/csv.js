// CSV and TSV findings.
//
// **Nothing here is ever authoritative, in any state.** Malformed quoting and
// inconsistent record widths are possible issues whether the delimiter was
// inferred or explicitly chosen by the user, so none of them can trigger Save
// Anyway. That is not a rule this file remembers — it is structural: every finding
// uses collector.possible(), the descriptor is marked non-authoritative, and the
// save path returns before validating a non-authoritative target at all.
//
// An earlier draft of the design promoted malformed quoting to an authoritative
// error once the user explicitly selected a delimiter. It was cut: it created
// three interacting states (inferred → warnings, explicit → errors, reopen → back
// to warnings) and made a non-persisted presentation choice into a hidden future
// save gate, all to block a save on a heuristic reading of a format with no single
// authoritative grammar. Do not reintroduce it here.
//
// The interpretation itself — the delimiter, the widths — comes from the shared
// dialect module, so these findings always describe the same table Preview shows.

import { createCollector, createPositionIndex } from '../diagnostics.js';
import { analyzeDelimited, ERR_INVALID_QUOTE, ERR_UNTERMINATED_QUOTE, isBlankRecord } from '../delimited.js';

const QUOTE_MESSAGES = {
    [ERR_UNTERMINATED_QUOTE]: 'Quoted field is never closed, so the rest of the document is being read as one field',
    [ERR_INVALID_QUOTE]: 'Text after a closing quote; use "" for a literal quote inside a quoted field',
};

// The ragged-row scan yields this often so the fallback executor can observe its
// deadline between batches. The parse itself is one uninterruptible call, which is
// exactly why that executor also carries a much lower byte ceiling.
const YIELD_EVERY = 1000;

function* validateDelimited(format, source, options, preferredID) {
    const text = typeof source === 'string' ? source : '';
    const collector = createCollector(format);
    yield;

    const analysis = analyzeDelimited(text, {
        preferredID,
        // Only the delimiter reaches here. A header override changes which row is
        // presented as a header and cannot change a quote position or a record
        // width, so it is presentation and nothing else.
        delimiterID: options && options.delimiterID,
    });

    // Hitting a parse cap establishes no complete answer, so it reports the
    // standard nonblocking notice rather than a partial finding set — the same
    // choice the YAML and XML limits make. Saving is unaffected either way,
    // because no CSV finding could have blocked it.
    if (analysis.limit) return collector.unavailable(`csv-limit-${analysis.limit}`);

    const index = createPositionIndex(text);

    for (const error of analysis.errors) {
        const message = QUOTE_MESSAGES[error.code] || 'Malformed quoting';
        if (!collector.possible(format, message, index.at(error.offset))) return collector.result();
    }
    yield;

    // Ragged rows. The table pads missing display cells without touching the
    // source, because ragged tables are frequently intentional — so this reports
    // the inconsistency and stops there.
    if (analysis.modalWidth > 0 && analysis.deviants > 0) {
        for (let r = 0; r < analysis.records.length; r++) {
            if (r > 0 && r % YIELD_EVERY === 0) yield;
            const record = analysis.records[r];
            if (isBlankRecord(record) || record.length === analysis.modalWidth) continue;
            const fields = `${record.length} ${record.length === 1 ? 'field' : 'fields'}`;
            const message = `Record has ${fields}; most records have ${analysis.modalWidth}`;
            if (!collector.possible(format, message, index.at(analysis.starts[r]))) break;
        }
    }

    return collector.result();
}

export function* validateCSV(source, options) {
    return yield* validateDelimited('csv', source, options, null);
}

export function* validateTSV(source, options) {
    // TSV initially prefers tab. An explicit override still wins, and a `.tsv`
    // file that is really comma-separated still falls through to detection.
    return yield* validateDelimited('tsv', source, options, 'tab');
}
