// The validator and formatter registry.
//
// Keys are the descriptor `validator` / `formatter` ids from TextFileTypes, so a
// descriptor names its adapter and nothing has to switch on a format elsewhere.
//
// Absence is a real answer, not a gap: shell, `.env`, INI/CFG/CONF, plain text and
// logs get highlighting only and produce no diagnostics at all, and the remaining
// non-authoritative ids (`lezer`, `markdown-renderer`) are answered outside the
// executor — Lezer recovery markers come from the editor and renderer failures
// from the preview.
//
// `csv` and `tsv` DO run here, and being non-authoritative is not a reason for
// them not to. Findings that only appeared while Preview happened to be open would
// be stale the moment the user typed in Edit; running them on the ordinary
// debounced, generation-guarded, worker-bounded path is what keeps them honest.
// Their severity, not their location, is what makes them nonblocking.

import { validateJSON, validateJSONL, formatJSONSource, formatJSONLSource } from './json.js';
import { validateYAML } from './yaml.js';
import { validateTOML } from './toml.js';
import { validateXML } from './xml.js';
import { validateCSV, validateTSV } from './csv.js';

const VALIDATORS = {
    json: validateJSON,
    jsonl: validateJSONL,
    yaml: validateYAML,
    toml: validateTOML,
    xml: validateXML,
    csv: validateCSV,
    tsv: validateTSV,
};

const FORMATTERS = {
    json: formatJSONSource,
    jsonl: formatJSONLSource,
};

export function validatorFor(format) {
    return Object.prototype.hasOwnProperty.call(VALIDATORS, format) ? VALIDATORS[format] : null;
}

export function formatterFor(format) {
    return Object.prototype.hasOwnProperty.call(FORMATTERS, format) ? FORMATTERS[format] : null;
}
