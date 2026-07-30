// TOML 1.0 syntax validation.
//
// `smol-toml` throws a single `TomlError` carrying a 1-based line and a 1-based
// column measured in UTF-16 code units, plus a `codeblock` and a message whose
// tail embeds that codeblock. `concise()` in the shared diagnostics module cuts
// the message at the first newline, which is what keeps the source excerpt out of
// the drawer; the line/column pair is clamped to the document rather than trusted
// blindly, so a parser off-by-one at end of input cannot place a marker outside it.

import { parse as parseTOML } from 'smol-toml';
import { createCollector, createPositionIndex } from '../diagnostics.js';

// TOML has no natural sub-document split, so it is one unit — which is why the
// fallback executor's byte ceiling, not its deadline, is the real bound here.
export function* validateTOML(source) {
    const collector = createCollector('toml');
    try {
        parseTOML(String(source === undefined || source === null ? '' : source));
    } catch (err) {
        const index = createPositionIndex(source);
        const line = Number.isFinite(err && err.line) ? err.line : 1;
        const column = Number.isFinite(err && err.column) ? err.column : 1;
        collector.error('toml', (err && err.message) || 'Invalid TOML', index.clamp(line, column));
    }
    return collector.result();
}
