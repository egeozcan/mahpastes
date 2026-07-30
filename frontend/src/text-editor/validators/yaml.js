// YAML 1.2 core validation.
//
// Two properties of this adapter are load-bearing:
//
//   * `prettyErrors: false`. The pretty form embeds the offending source line and
//     a caret in the message text, and a diagnostic must never echo document
//     content into the drawer or an accessible name.
//   * Documents are composed and *counted*, never resolved. `toJS()` is what
//     expands aliases and materializes an unrestricted JavaScript object graph, so
//     not calling it means no alias is ever expanded — the alias limit below
//     bounds the count, not an expansion.
//
// The streaming Parser/Composer pair is used rather than `parseAllDocuments` so
// each document is a unit of work the driver can yield between.

import { Composer, Parser, visit } from 'yaml';
import {
    DIAGNOSTIC_LIMITS,
    createCollector,
    createPositionIndex,
} from '../diagnostics.js';

// `yaml` reports `err.pos` as a [start, end] pair of UTF-16 code-unit offsets into
// the source string it was given, which is what the position index consumes.
//
// Returns false once the collector is full, which is the signal to stop composing.
function collect(collector, index, errors) {
    for (const err of errors) {
        const start = index.at(err.pos ? err.pos[0] : 0);
        const end = err.pos ? index.at(err.pos[1]) : null;
        if (!collector.error('yaml', err.message, start, end)) return false;
    }
    return true;
}

export function* validateYAML(source) {
    const collector = createCollector('yaml');
    const index = createPositionIndex(source);
    const limits = DIAGNOSTIC_LIMITS;

    let composer;
    let tokens;
    try {
        // uniqueKeys is the default and is stated explicitly because duplicate
        // mapping keys being errors is a documented part of the contract.
        composer = new Composer({ uniqueKeys: true, prettyErrors: false });
        tokens = new Parser().parse(String(source === undefined || source === null ? '' : source));
    } catch (err) {
        return collector.unavailable('parser-failed');
    }

    let documents = 0;
    let nodes = 0;

    try {
        for (const doc of composer.compose(tokens, false)) {
            documents++;
            if (documents > limits.yamlDocuments) return collector.unavailable('documents');

            let aliases = 0;
            let exceeded = null;
            const count = (isAlias) => {
                nodes++;
                if (isAlias) aliases++;
                if (aliases > limits.yamlAliasesPerDocument) { exceeded = 'aliases'; return visit.BREAK; }
                if (nodes > limits.yamlNodes) { exceeded = 'nodes'; return visit.BREAK; }
                return undefined;
            };
            // Both visitors: `yaml` dispatches an alias node to `Alias` when that
            // visitor exists, so `Node` alone would not see it and `Alias` alone
            // would not count ordinary nodes.
            visit(doc, {
                Node: () => count(false),
                Alias: () => count(true),
            });
            if (exceeded) return collector.unavailable(exceeded);

            if (!collect(collector, index, doc.errors)) return collector.result();
            yield;
        }
        // Trailing errors — an unterminated document at end of input — are reported
        // by end(), not by the last composed document.
        for (const doc of composer.end(false)) {
            if (!collect(collector, index, doc.errors)) break;
        }
    } catch (err) {
        return collector.unavailable('parser-failed');
    }

    return collector.result();
}
