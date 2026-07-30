// Decodes clip payloads into an editor value, records the fidelity profile, and
// re-encodes save bytes. Keeping byte preservation here means neither CodeMirror
// nor any renderer has to know about BOMs, CR line endings, or base64.
//
// The contract this module exists to enforce: an ordinary save of an unedited
// document writes back the exact bytes that were read. That is asserted on bytes,
// never on decoded strings — a string comparison passes happily while a BOM or a
// CR silently vanishes.

// Text clips above this are not editable in either mode. The editor declines to
// open and points at download. This sits above the 2 MiB enhanced-assistance
// threshold and is a separate, harder limit: the editor path would otherwise
// materialize one document as a Blob, a base64 string, a decoded string, a
// CodeMirror document, and possibly a preview simultaneously.
export const MAX_EDITABLE_BYTES = 16 * 1024 * 1024;

// At or below this, highlighting, validation, diagnostics, formatting, and table
// rendering are available. Above it they are all disabled and the document stays
// plainly previewable and editable.
export const ENHANCED_ASSISTANCE_MAX_BYTES = 2 * 1024 * 1024;

// Written as an escape on purpose: a literal U+FEFF in source is invisible and
// survives exactly one careless editor round-trip.
export const BOM = '\uFEFF';
export const NEWLINE_SEQUENCES = { lf: '\n', crlf: '\r\n', cr: '\r' };

// --- base64 ------------------------------------------------------------------

export function base64ToBytes(base64) {
    const binary = atob(base64);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
    return bytes;
}

export function bytesToBase64(bytes) {
    let binary = '';
    const chunk = 0x8000;
    for (let offset = 0; offset < bytes.length; offset += chunk) {
        binary += String.fromCharCode.apply(null, bytes.subarray(offset, offset + chunk));
    }
    return btoa(binary);
}

// --- profile -----------------------------------------------------------------

/**
 * Detects the newline style of a document.
 *
 * Deliberately mixed newline sequences are outside the fidelity guarantee; the
 * dominant style is used when encoding. Ties resolve in the order lf, crlf, cr,
 * and a document with no separators at all defaults to lf.
 */
export function detectNewline(text) {
    let crlf = 0;
    let lf = 0;
    let cr = 0;
    for (let i = 0; i < text.length; i++) {
        const ch = text[i];
        if (ch === '\r') {
            if (text[i + 1] === '\n') { crlf++; i++; } else { cr++; }
        } else if (ch === '\n') {
            lf++;
        }
    }
    if (crlf === 0 && lf === 0 && cr === 0) return 'lf';
    const best = Math.max(crlf, lf, cr);
    if (lf === best) return 'lf';
    if (crlf === best) return 'crlf';
    return 'cr';
}

export function normalizeNewlines(text) {
    return text.replace(/\r\n|\r/g, '\n');
}

/**
 * Builds the fidelity profile from a decoded document and returns the editor
 * value alongside it. The visible editor value excludes the BOM and uses a single
 * line separator, matching CodeMirror's document model.
 */
export function profileFrom(decoded) {
    const bom = decoded.startsWith(BOM);
    const body = bom ? decoded.slice(BOM.length) : decoded;
    const newline = detectNewline(body);
    const value = normalizeNewlines(body);
    return {
        // The full decoded document, BOM and original separators intact. Kept
        // alongside `value` so a caller that has not yet adopted profile-based
        // encoding can round-trip byte-for-byte through the old path.
        document: decoded,
        value,
        profile: {
            bom,
            newline,
            // Tracked for reporting; encoding follows the *current* value so
            // deliberately adding or removing a final newline is respected.
            finalNewline: value.endsWith('\n'),
        },
    };
}

export function defaultProfile() {
    // Newly created or genuinely separator-free documents: LF, no BOM.
    return { bom: false, newline: 'lf', finalNewline: false };
}

// --- decode ------------------------------------------------------------------

/**
 * Decodes a clip payload into an editor value plus its fidelity profile.
 *
 * `dataEncoding` is `base64` or `utf8`. Extension-classified application types
 * arrive as base64 even when valid, so both encodings must be handled after
 * classification rather than inferred from the content type.
 *
 * Invalid UTF-8 is never decoded for editing. `validUTF8 === false` on a `utf8`
 * payload means the bytes were already replacement-decoded before the frontend
 * saw them, so the answer is the same: read-only.
 */
export function decodeClipPayload({ data, dataEncoding, validUTF8 } = {}) {
    const encoding = dataEncoding === 'utf8' ? 'utf8' : 'base64';
    const payload = typeof data === 'string' ? data : '';

    if (encoding === 'utf8') {
        // A utf8 payload is already a JavaScript string; there are no bytes left
        // to check. Trust the backend's flag, which was computed on the bytes.
        if (validUTF8 === false) {
            return { ok: false, reason: 'invalid-utf8', byteLength: byteLengthOf(payload) };
        }
        const byteLength = byteLengthOf(payload);
        if (byteLength > MAX_EDITABLE_BYTES) {
            return { ok: false, reason: 'too-large', byteLength };
        }
        const { document: doc, value, profile } = profileFrom(payload);
        return { ok: true, document: doc, value, profile, byteLength, encoding };
    }

    let bytes;
    try {
        bytes = base64ToBytes(payload);
    } catch (err) {
        return { ok: false, reason: 'decode-failed', message: String((err && err.message) || err), byteLength: 0 };
    }
    if (bytes.length > MAX_EDITABLE_BYTES) {
        return { ok: false, reason: 'too-large', byteLength: bytes.length };
    }
    if (validUTF8 === false) {
        return { ok: false, reason: 'invalid-utf8', byteLength: bytes.length };
    }
    let decoded;
    try {
        // Two options, both load-bearing:
        //   fatal      — a lossy decode is exactly the silent byte replacement
        //                this module exists to prevent, and this decode is what
        //                makes a recognized text filename under
        //                application/octet-stream conditional rather than blocked.
        //   ignoreBOM  — TextDecoder *strips* a leading BOM by default, so
        //                without this the profile can never observe one and every
        //                BOM'd document silently loses it on save.
        decoded = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(bytes);
    } catch {
        return { ok: false, reason: 'invalid-utf8', byteLength: bytes.length };
    }
    const { document: doc, value, profile } = profileFrom(decoded);
    return { ok: true, document: doc, value, profile, byteLength: bytes.length, encoding };
}

// The transitional `decodeForDisplay` shim is gone. It existed only because the
// editor shell had no way to present a refusal: it reproduced the old replacing
// decode so a save could not overwrite the clip with nothing. Now that the
// format-neutral byte-safety screen and the 16 MiB decline exist, every caller
// routes through decodeClipPayload and a refusal is shown as a refusal.

function byteLengthOf(text) {
    try {
        return new TextEncoder().encode(text).length;
    } catch {
        return text.length;
    }
}

// --- encode ------------------------------------------------------------------

/**
 * Finds the first unpaired surrogate in a string, or null.
 *
 * Positions are 1-based lines and 1-based columns measured in UTF-16 code units,
 * matching CodeMirror's document model and every other position in this feature.
 */
export function findUnpairedSurrogate(text) {
    let line = 1;
    let column = 1;
    for (let i = 0; i < text.length; i++) {
        const code = text.charCodeAt(i);
        if (code >= 0xd800 && code <= 0xdbff) {
            const next = text.charCodeAt(i + 1);
            if (!(next >= 0xdc00 && next <= 0xdfff)) {
                return { line, column, codeUnit: code, index: i };
            }
            // A well-formed pair advances two code units on the same line.
            i++;
            column += 2;
            continue;
        }
        if (code >= 0xdc00 && code <= 0xdfff) {
            return { line, column, codeUnit: code, index: i };
        }
        if (text[i] === '\n') { line++; column = 1; continue; }
        column++;
    }
    return null;
}

/**
 * Re-encodes an editor value to bytes using a recorded fidelity profile.
 *
 * One case cannot be preserved and is refused rather than mangled: an unpaired
 * surrogate. CodeMirror holds JavaScript strings, so a lone \uD800 can exist in
 * the value, and TextEncoder would silently write EF BF BD for it — the exact
 * byte replacement this design exists to prevent.
 */
export function encodeForSave(value, profile) {
    const text = typeof value === 'string' ? value : '';
    const effective = profile || defaultProfile();

    const unpaired = findUnpairedSurrogate(text);
    if (unpaired) {
        return {
            ok: false,
            reason: 'unpaired-surrogate',
            position: { line: unpaired.line, column: unpaired.column },
            codeUnit: `U+${unpaired.codeUnit.toString(16).toUpperCase().padStart(4, '0')}`,
        };
    }

    const separator = NEWLINE_SEQUENCES[effective.newline] || NEWLINE_SEQUENCES.lf;
    // The value always holds LF separators; expand them to the document's style.
    const body = separator === '\n' ? text : text.split('\n').join(separator);
    const withBOM = effective.bom ? BOM + body : body;
    const bytes = new TextEncoder().encode(withBOM);
    return { ok: true, bytes, base64: bytesToBase64(bytes), byteLength: bytes.length };
}

export const TextCodec = {
    MAX_EDITABLE_BYTES,
    ENHANCED_ASSISTANCE_MAX_BYTES,
    BOM,
    NEWLINE_SEQUENCES,
    base64ToBytes,
    bytesToBase64,
    detectNewline,
    normalizeNewlines,
    profileFrom,
    defaultProfile,
    decodeClipPayload,
    findUnpairedSurrogate,
    encodeForSave,
};
