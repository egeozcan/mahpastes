// Strict JSON and JSON Lines validation, plus their formatters.
//
// `JSON.parse` is the authority on the verdict — comments, trailing commas, `NaN`
// and `Infinity` are errors because it rejects them. It is *not* a usable source
// of positions: V8 omits the offset entirely for short inputs
// (`Unexpected token '}', "{"broken": }" is not valid JSON`) and the format of
// the message it does produce has changed twice. So the position comes from a
// small strict scanner in this file, which runs only after JSON.parse has already
// established that the document is invalid. It can therefore never turn a valid
// document into a reported error.

import {
    DIAGNOSTIC_LIMITS,
    createCollector,
    createPositionIndex,
} from '../diagnostics.js';

const WHITESPACE = new Set([' ', '\t', '\n', '\r']);

function isDigit(ch) {
    return ch >= '0' && ch <= '9';
}

function describe(ch) {
    if (ch === undefined) return 'end of input';
    // One character only. A longer excerpt would put document content — a token
    // from a `.env`-adjacent file, a secret in a JSON string — into the drawer.
    return `'${ch}'`;
}

/**
 * Finds the first strict-JSON syntax error and returns `{ index, message }`,
 * where `index` is a UTF-16 code-unit offset into `source`, or null when the
 * scanner finds nothing to complain about.
 *
 * Returning null is a legitimate outcome: the caller falls back to the engine's
 * own reported offset, or to the start of the document. It must never guess.
 */
export function findJSONSyntaxError(source) {
    const text = typeof source === 'string' ? source : '';
    const n = text.length;
    let i = 0;
    let error = null;

    function fail(message, at) {
        if (!error) error = { index: Math.min(at === undefined ? i : at, n), message };
        return false;
    }

    function skipWhitespace() {
        while (i < n && WHITESPACE.has(text[i])) i++;
    }

    function scanString() {
        const start = i;
        i++; // opening quote
        for (;;) {
            if (i >= n) return fail('Unterminated string', start);
            const ch = text[i];
            if (ch === '"') { i++; return true; }
            if (ch === '\\') {
                i++;
                if (i >= n) return fail('Unterminated escape sequence');
                const esc = text[i];
                if (esc === '"' || esc === '\\' || esc === '/' || esc === 'b' || esc === 'f'
                    || esc === 'n' || esc === 'r' || esc === 't') {
                    i++;
                    continue;
                }
                if (esc === 'u') {
                    i++;
                    for (let k = 0; k < 4; k++) {
                        const hex = text[i];
                        if (hex === undefined || !/[0-9a-fA-F]/.test(hex)) return fail('Invalid \\u escape sequence');
                        i++;
                    }
                    continue;
                }
                return fail(`Invalid escape sequence \\${esc}`);
            }
            if (text.charCodeAt(i) < 0x20) return fail('Unescaped control character in string');
            i++;
        }
    }

    function scanNumber() {
        const start = i;
        if (text[i] === '-') i++;
        if (text[i] === '0') {
            i++;
        } else if (isDigit(text[i])) {
            while (i < n && isDigit(text[i])) i++;
        } else {
            return fail('Invalid number', start);
        }
        if (text[i] === '.') {
            i++;
            if (!isDigit(text[i])) return fail('Missing digits after the decimal point');
            while (i < n && isDigit(text[i])) i++;
        }
        if (text[i] === 'e' || text[i] === 'E') {
            i++;
            if (text[i] === '+' || text[i] === '-') i++;
            if (!isDigit(text[i])) return fail('Missing digits in the exponent');
            while (i < n && isDigit(text[i])) i++;
        }
        return true;
    }

    function scanValue(depth) {
        if (depth > DIAGNOSTIC_LIMITS.jsonDepth) return fail('JSON nesting is too deep to check');
        skipWhitespace();
        if (i >= n) return fail('Unexpected end of input');
        const ch = text[i];
        if (ch === '{') return scanObject(depth + 1);
        if (ch === '[') return scanArray(depth + 1);
        if (ch === '"') return scanString();
        if (ch === '-' || isDigit(ch)) return scanNumber();
        if (text.startsWith('true', i)) { i += 4; return true; }
        if (text.startsWith('false', i)) { i += 5; return true; }
        if (text.startsWith('null', i)) { i += 4; return true; }
        // Comments, NaN, Infinity, single quotes, bare keys — every extension
        // strict JSON forbids lands here.
        return fail(`Unexpected ${describe(ch)}`);
    }

    function scanObject(depth) {
        i++; // {
        skipWhitespace();
        if (text[i] === '}') { i++; return true; }
        for (;;) {
            skipWhitespace();
            if (i >= n) return fail('Unexpected end of input: unclosed object');
            if (text[i] !== '"') return fail(`Expected a double-quoted property name, found ${describe(text[i])}`);
            if (!scanString()) return false;
            skipWhitespace();
            if (text[i] !== ':') return fail(`Expected ':' after the property name, found ${describe(text[i])}`);
            i++;
            if (!scanValue(depth)) return false;
            skipWhitespace();
            if (text[i] === ',') { i++; continue; }
            if (text[i] === '}') { i++; return true; }
            return fail(`Expected ',' or '}' in object, found ${describe(text[i])}`);
        }
    }

    function scanArray(depth) {
        i++; // [
        skipWhitespace();
        if (text[i] === ']') { i++; return true; }
        for (;;) {
            if (!scanValue(depth)) return false;
            skipWhitespace();
            if (text[i] === ',') { i++; continue; }
            if (text[i] === ']') { i++; return true; }
            return fail(`Expected ',' or ']' in array, found ${describe(text[i])}`);
        }
    }

    try {
        skipWhitespace();
        if (i >= n) {
            // JSON.parse('') and JSON.parse('   ') both reject; say so at the end.
            return { index: n, message: 'Unexpected end of input: the document is empty' };
        }
        if (scanValue(0)) {
            skipWhitespace();
            if (i < n) fail(`Unexpected ${describe(text[i])} after the top-level value`);
        }
    } catch (err) {
        // A stack overflow on pathological nesting is not a diagnostic. The caller
        // falls back to the engine's own position rather than inventing one.
        return null;
    }
    return error;
}

// Used only when the scanner declines. Modern V8 sometimes includes
// `at position N (line L column C)`; N is a code-unit index into the whole string,
// which is exactly what the position index consumes.
function offsetFromEngineMessage(message) {
    const match = /position\s+(\d+)/i.exec(String(message || ''));
    return match ? Number(match[1]) : 0;
}

// Returns false once the collector is full, which is the signal to stop.
function report(collector, index, source, text, err, baseOffset) {
    const found = findJSONSyntaxError(text);
    const relative = found ? found.index : offsetFromEngineMessage(err && err.message);
    const message = found ? found.message : (err && err.message) || 'Invalid JSON';
    return collector.error(source, message, index.at(baseOffset + relative));
}

// JSON has no natural sub-document split, so it is one unit. That is exactly why
// the fallback executor carries a much lower byte ceiling than the worker: the
// ceiling, not the deadline, is what bounds a single un-preemptible parse.
export function* validateJSON(source) {
    const collector = createCollector('json');
    try {
        JSON.parse(source);
    } catch (err) {
        report(collector, createPositionIndex(source), 'json', source, err, 0);
    }
    return collector.result();
}

// One yield per batch of lines rather than per line: in the worker the driver
// drains the generator without awaiting, but in the fallback every yield is a
// MessageChannel hop, and a hop per line would dominate the run.
const JSONL_LINES_PER_UNIT = 64;

/**
 * Every nonblank physical line must contain exactly one complete strict JSON
 * value. Blank lines are allowed, including the empty final element a trailing
 * newline produces.
 */
export function* validateJSONL(source) {
    const collector = createCollector('jsonl');
    const index = createPositionIndex(source);
    const lines = String(source === undefined || source === null ? '' : source).split('\n');
    let offset = 0;

    for (let l = 0; l < lines.length; l++) {
        const line = lines[l];
        if (line.trim() !== '') {
            try {
                JSON.parse(line);
            } catch (err) {
                if (!report(collector, index, 'jsonl', line, err, offset)) break;
            }
        }
        offset += line.length + 1;
        if (l % JSONL_LINES_PER_UNIT === JSONL_LINES_PER_UNIT - 1) yield;
    }

    return collector.result();
}

// --- formatters ---------------------------------------------------------------
//
// Formatting is explicit and never runs during preview or save. The value it
// receives already excludes the BOM and uses LF separators; the BOM and the
// document's newline style are reapplied by TextCodec at save time, so the only
// profile element a formatter has to preserve itself is the final newline.

// Formatting re-indents the document's own tokens; it never round-trips through
// JavaScript values.
//
// This is not fastidiousness. `JSON.parse` + `JSON.stringify` silently rewrites
// data an explicit formatting action has no business touching: 9007199254740993
// comes back as 9007199254740992 because it exceeds Number.MAX_SAFE_INTEGER, and
// 1e400 comes back as `null` because it overflows to Infinity. Both are valid JSON
// that a formatter must leave alone. Strings are re-emitted verbatim too, so an
// escape the author chose (é rather than é) survives.
//
// The scanner assumes well-formed input, which is guaranteed: formatting is
// disabled while authoritative errors exist, and every caller validates first.

// Refusing to format is the correct answer for a document whose indented form would
// be orders of magnitude larger than its source. It surfaces as a toast, and the
// document is left exactly as it was.
class FormatTooDeep extends Error {
    constructor(depth) {
        super(`nesting depth ${depth} exceeds the ${DIAGNOSTIC_LIMITS.formatDepth}-level formatting limit`);
        this.depth = depth;
    }
}

class FormatTooLarge extends Error {
    constructor(size) {
        super(`formatted output of ${size} units exceeds the ${DIAGNOSTIC_LIMITS.formatOutputUnits}-unit formatting limit`);
        this.size = size;
    }
}

// Reads one JSON string literal starting at `i` (which must be the opening quote)
// and returns its exact source text, escapes untouched.
function readString(text, i) {
    let j = i + 1;
    while (j < text.length) {
        const ch = text[j];
        if (ch === '\\') { j += 2; continue; }
        if (ch === '"') { j += 1; break; }
        j += 1;
    }
    return text.slice(i, j);
}

/**
 * Re-indents a JSON document with two spaces, preserving every literal verbatim.
 */
function reindentJSON(text, indentUnit = '  ') {
    const out = [];
    let depth = 0;
    let maxDepth = 0;
    let i = 0;
    // True immediately after `{` or `[`, so an empty container stays on one line.
    let freshContainer = false;

    let produced = 0;
    const emit = (chunk) => {
        produced += chunk.length;
        // Checked as it is produced, so a pathological document is abandoned early
        // rather than after building the whole string.
        if (produced > DIAGNOSTIC_LIMITS.formatOutputUnits) throw new FormatTooLarge(produced);
        out.push(chunk);
    };
    const newline = () => { emit('\n'); emit(indentUnit.repeat(depth)); };

    while (i < text.length) {
        const ch = text[i];

        if (WHITESPACE.has(ch)) { i += 1; continue; }

        if (ch === '"') {
            if (freshContainer) { newline(); freshContainer = false; }
            const literal = readString(text, i);
            emit(literal);
            i += literal.length;
            continue;
        }

        if (ch === '{' || ch === '[') {
            if (freshContainer) { newline(); }
            emit(ch);
            depth += 1;
            if (depth > maxDepth) {
                maxDepth = depth;
                // Refused rather than attempted: see DIAGNOSTIC_LIMITS.formatDepth.
                if (maxDepth > DIAGNOSTIC_LIMITS.formatDepth) throw new FormatTooDeep(maxDepth);
            }
            freshContainer = true;
            i += 1;
            continue;
        }

        if (ch === '}' || ch === ']') {
            depth -= 1;
            // An empty container: nothing was emitted between the brackets.
            if (freshContainer) emit(ch);
            else { newline(); emit(ch); }
            freshContainer = false;
            i += 1;
            continue;
        }

        if (ch === ',') {
            emit(',');
            freshContainer = false;
            newline();
            i += 1;
            continue;
        }

        if (ch === ':') {
            emit(': ');
            i += 1;
            continue;
        }

        // A number, or one of true/false/null — emitted exactly as written.
        if (freshContainer) { newline(); freshContainer = false; }
        let j = i;
        while (j < text.length && !WHITESPACE.has(text[j]) && !',:{}[]"'.includes(text[j])) j += 1;
        emit(text.slice(i, j));
        i = j;
    }

    return out.join('');
}

// Collapses one JSON value onto a single line, again preserving literals.
function compactJSON(text) {
    const out = [];
    let i = 0;
    while (i < text.length) {
        const ch = text[i];
        if (WHITESPACE.has(ch)) { i += 1; continue; }
        if (ch === '"') {
            const literal = readString(text, i);
            out.push(literal);
            i += literal.length;
            continue;
        }
        if (',:{}[]'.includes(ch)) { out.push(ch); i += 1; continue; }
        let j = i;
        while (j < text.length && !WHITESPACE.has(text[j]) && !',:{}[]"'.includes(text[j])) j += 1;
        out.push(text.slice(i, j));
        i = j;
    }
    return out.join('');
}

export function* formatJSONSource(source) {
    const text = String(source === undefined || source === null ? '' : source);
    // Parsed only to reject anything malformed before rewriting it; the value it
    // returns is deliberately discarded.
    JSON.parse(text);
    yield;
    const formatted = reindentJSON(text);
    return { text: text.endsWith('\n') ? `${formatted}\n` : formatted };
}

export function* formatJSONLSource(source) {
    const text = String(source === undefined || source === null ? '' : source);
    const lines = text.split('\n');
    const out = [];
    for (let l = 0; l < lines.length; l++) {
        const line = lines[l];
        // Blank lines are preserved: they are legal JSON Lines and may be
        // deliberate separators.
        if (line.trim() === '') {
            out.push(line);
        } else {
            JSON.parse(line);
            out.push(compactJSON(line));
        }
        if (l % JSONL_LINES_PER_UNIT === JSONL_LINES_PER_UNIT - 1) yield;
    }
    return { text: out.join('\n') };
}
