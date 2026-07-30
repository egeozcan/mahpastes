// XML well-formedness only. No schema validation, no entity or resource
// resolution, no XInclude processing.
//
// The entity policy is exact, because "inert DOCTYPE" and "well-formedness" pull
// in opposite directions. For `<!DOCTYPE r [<!ENTITY x "ok">]><r>&x;</r>`:
// the internal subset is parsed for syntax, the declaration is recorded but never
// expanded, and the reference `&x;` is accepted without expansion. A reference to
// an entity that was never declared is **also accepted** rather than reported,
// because distinguishing the two requires exactly the entity table this design
// refuses to build. The five predefined entities and numeric character references
// behave normally.
//
// The practical consequence, stated plainly: this catches structural malformation
// — unclosed tags, mismatched names, bad nesting, illegal characters — and
// deliberately does not catch entity-level mistakes.
//
// That boundary includes the internal subset's own syntax. `saxes` treats the
// subset as opaque, so `<!DOCTYPE r [<!ENTITY>]><r/>` and `<!DOCTYPE r [garbage]>`
// report nothing.
//
// This is a KNOWN, DELIBERATE deviation from a literal reading of one sentence in
// the design ("the internal subset is parsed for syntax"), resolved in favour of
// the sentence that states the scope ("deliberately does not catch entity-level
// mistakes"). The ambiguity was noticed rather than missed. Closing it would mean
// writing a DTD-subset parser — new machinery the same design's non-goals rule out
// ("no schema validation", no entity table) — and the cost of leaving it is a
// missed warning on a rare malformed DOCTYPE, never a wrong verdict on a
// well-formed document and never a data-safety issue. A test pins the behavior so
// a future parser change cannot alter it silently.

import { SaxesParser } from 'saxes';
import {
    DIAGNOSTIC_LIMITS,
    createCollector,
    createPositionIndex,
} from '../diagnostics.js';

// saxes prefixes every message with `line:column: `, or `fileName:line:column: `
// when a fileName was configured. The location is already carried structurally,
// so repeating it in the text would only read as noise. The filename group is
// optional because this adapter deliberately configures no fileName.
const LOCATION_PREFIX = /^(?:[^:\s]+:)?\d+:\d+:\s*/;

// Thrown from an event handler to abort the parse. saxes calls handlers
// synchronously from write(), so this genuinely stops the work rather than merely
// marking a flag and letting a pathological document run to completion — which
// matters most in the fallback executor, where nothing can preempt the call.
class LimitExceeded extends Error {
    constructor(reason) {
        super(`xml limit exceeded: ${reason}`);
        this.reason = reason;
    }
}

// Distinct from LimitExceeded on purpose. Hitting the collection cap is not a
// resource failure: 1,000 real findings were established, so the answer is a
// truncated *result*, not `unavailable`. Only depth/event limits — which mean the
// document was never fully examined — establish nothing.
class CollectionComplete extends Error {}

// XML has no natural sub-document split, so it is one unit.
export function* validateXML(source) {
    const collector = createCollector('xml');
    const index = createPositionIndex(source);
    const limits = DIAGNOSTIC_LIMITS;

    let depth = 0;
    let events = 0;

    let parser;
    try {
        // No fileName: it would only be echoed into every message. xmlns stays off
        // (the default), so an undeclared namespace prefix is not a
        // well-formedness error — namespace-awareness is a separate contract this
        // release does not promise.
        parser = new SaxesParser({ fragment: false });
    } catch (err) {
        return collector.unavailable('parser-failed');
    }

    parser.on('error', (err) => {
        const message = String((err && err.message) || 'Invalid XML').replace(LOCATION_PREFIX, '');
        if (/undefined entity/i.test(message)) return;
        // saxes reports `line` 1-based, `column` in code points, and `columnIndex`
        // in UTF-16 code units — the unit every position in this feature uses.
        //
        // A false return means the 1,000-finding cap is reached. The spec says
        // collection *stops* there, so the parse is aborted rather than left to run
        // to completion discarding everything it finds — which matters most in the
        // fallback executor, where nothing can preempt the call.
        if (!collector.error('xml', message, index.clamp(parser.line, parser.columnIndex))) {
            throw new CollectionComplete();
        }
    });

    parser.on('opentag', (tag) => {
        depth++;
        events += 1 + Object.keys((tag && tag.attributes) || {}).length;
        if (depth > limits.xmlDepth) throw new LimitExceeded('depth');
        if (events > limits.xmlEvents) throw new LimitExceeded('events');
    });

    parser.on('closetag', () => {
        if (depth > 0) depth--;
    });

    try {
        parser.write(String(source === undefined || source === null ? '' : source));
        parser.close();
    } catch (err) {
        // The cap is a complete answer about the first 1,000 problems, so the
        // findings are kept and reported as truncated.
        if (err instanceof CollectionComplete) return collector.result();
        if (err instanceof LimitExceeded) return collector.unavailable(err.reason);
        return collector.unavailable('parser-failed');
    }

    return collector.result();
}
