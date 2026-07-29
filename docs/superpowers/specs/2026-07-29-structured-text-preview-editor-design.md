# Structured Text Preview and Editor Design

**Date:** 2026-07-29

**Status:** Approved

## Summary

Expand Markdown's Preview/Edit experience into a general structured-text workspace. Recognized valid UTF-8 text clips open in Preview by default, while an explicit **Edit** action opens a lightweight CodeMirror 6 editor with syntax highlighting, line numbers, bracket matching, indentation support, and diagnostics.

The feature uses a centralized filename/MIME registry, safe read-only source previews, local format validators, explicit JSON formatting, and a bounded CSV/TSV table preview. Markdown keeps its existing secure rendered-preview pipeline. HTML and all other code-like formats are displayed only as inert source.

The implementation must preserve existing editor behavior, protect invalid UTF-8 bytes, retain document text conventions when saving, and degrade predictably for large inputs.

## Problem

The current editor already permits most `text/*` clips and JSON, but its richer Preview/Edit mode is hard-coded to `.md` and `.markdown` filenames. Other text formats open directly into a native textarea without syntax highlighting or line numbers. JSON has separate validation and formatting controls, while YAML, TOML, XML, CSV, and other structured formats provide no indication that an edit damaged their grammar.

The current implementation also has several constraints that make a simple format list insufficient:

- Editor eligibility is mostly MIME-based, so common application MIME types and extension-classified files can fail to open as text.
- Markdown filename detection is duplicated across frontend and backend paths.
- The native textarea owns search, selection, wrapping, drafts, status, and scroll behavior directly.
- Invalid UTF-8 byte protection is currently specialized to Markdown.
- Tests and helpers interact directly with the textarea.
- The frontend uses ordered classic scripts and has no general JavaScript bundle step.

The goal is not to create an IDE. It is to make quick developer-focused inspection and editing safer and more useful.

## Goals

1. Give common text, configuration, data, and web source formats consistent Preview/Edit behavior.
2. Open recognized text clips in Preview by default while preserving a direct Edit entry point.
3. Provide a lightweight code-editor experience without autocomplete or project-level tooling.
4. Detect reliable grammar errors in JSON, JSON Lines, YAML, TOML, XML, CSV, and TSV.
5. Continue showing source when syntax is invalid and make diagnostics easy to navigate.
6. Preserve UTF-8 BOM presence, document line-ending style, and final-newline state across ordinary saves.
7. Keep Markdown rendering, links, images, sanitization, and security boundaries intact.
8. Render untrusted source without executing code, injecting source HTML, or loading remote resources.
9. Degrade safely for large or pathological documents.
10. Isolate CodeMirror and parser dependencies behind small app-owned interfaces.
11. Preserve existing drafts, save flows, shortcuts, accessibility, and dirty-close behavior.
12. Cover every registered type and validation contract with automated tests.

## Non-goals

- Rich-text or WYSIWYG editing.
- Autocomplete, language servers, type checking, project context, or schema validation.
- Code execution or shell evaluation.
- Rendered HTML, CSS, JavaScript, TypeScript, XML, or configuration-file previews.
- Automatic formatting on save.
- Initial formatters other than JSON and JSON Lines.
- Authoritative syntax validation for HTML, CSS, JavaScript, TypeScript, shell, `.env`, INI, CFG, or CONF.
- Guessing whether arbitrary binary bytes are text.
- Editing invalid UTF-8 or UTF-16 documents.
- Exact preservation of deliberately mixed newline sequences; the fidelity contract applies to a document's detected newline style.
- Persisting cursor or scroll position after closing and reopening a clip.
- Changing downloads, REST responses, public links, tag servers, or CLI data output.

## Chosen approach

Adopt CodeMirror 6 for Edit mode, keep Preview as a separate app-owned panel, and perform validation locally in the frontend.

Two alternatives were rejected:

1. **Keep the textarea and layer highlighting over it.** This preserves current tests but makes source highlighting, selection synchronization, diagnostics, search highlights, wrapping, and proportional overlays fragile.
2. **Generalize previews first and migrate editing later.** This lowers short-term risk but duplicates editor integration work and temporarily misses the requested developer-focused experience.

CodeMirror has the largest migration cost, but it provides the cleanest long-term foundation for language modes, accessible source navigation, diagnostics, and future format additions. A dedicated adapter prevents the surrounding editor shell from depending directly on CodeMirror APIs.

## Type classification

### Registry

Add one frontend registry that classifies text modes from normalized filenames and MIME types. Each descriptor supplies:

```text
id
filename matchers
MIME matchers
CodeMirror language mode
preview renderer
validator
formatter, when available
```

The classification order is:

1. A recognized filename or extension.
2. A recognized MIME type when the filename has no recognized match.
3. Generic text for any remaining `text/*` MIME type.
4. Not a text-editor type.

A recognized filename therefore wins when MIME disagrees. An unknown filename does not suppress a known MIME fallback. Classification changes presentation and editor eligibility; it does not silently rewrite stored content type.

Before matching, the registry:

- Extracts the basename by treating both `/` and `\\` as path separators.
- Preserves filename whitespace, then applies Unicode normalization and locale-independent case folding for comparison.
- Matches only the final extension or an explicit whole-basename pattern.
- Trims MIME whitespace, lowercases the ASCII type/subtype, and removes parameters beginning with `;`, so `Application/JSON; charset=utf-8` becomes `application/json`.
- Treats an empty or malformed MIME value as unknown.

Markdown remains the sole exception: its existing backend filename promotion to `text/markdown` remains unchanged.

### Initial registry

| Mode | Filename matches | MIME fallback | Preview | Validation | Formatting |
|---|---|---|---|---|---|
| Markdown | `.md`, `.markdown` | Existing Markdown behavior | Secure rendered GFM | Renderer failure only | None |
| Plain text | `.txt`, `.text`, `.log` | Remaining `text/*` | Source | None | None |
| JSON | `.json` | `application/json`, `application/*+json` | Source | Strict JSON | Two-space JSON |
| JSON Lines | `.jsonl`, `.ndjson` | `application/x-ndjson`, `application/jsonl` | Source | One JSON value per nonblank line | Compact value per line |
| YAML | `.yaml`, `.yml` | `application/yaml`, `application/x-yaml`, `text/yaml`, `text/x-yaml` | Source | YAML 1.2 core | None |
| TOML | `.toml` | `application/toml`, `text/toml` | Source | TOML 1.0 | None |
| XML | `.xml` | `application/xml`, `text/xml`, `application/*+xml` | Source | Well-formedness; no DOCTYPE | None |
| CSV | `.csv` | `text/csv` | Table | CSV structure | None |
| TSV | `.tsv` | `text/tab-separated-values` | Table | TSV structure | None |
| HTML | `.html`, `.htm` | `text/html` | Source | Possible issues only | None |
| CSS | `.css` | `text/css` | Source | Possible issues only | None |
| JavaScript | `.js`, `.mjs`, `.cjs` | `text/javascript`, `application/javascript` | Source | Possible issues only | None |
| TypeScript | `.ts`, `.mts`, `.cts` | `text/typescript`, `application/typescript` | Source | Possible issues only | None |
| Shell | `.sh`, `.bash`, `.zsh` | `text/x-shellscript`, `application/x-sh` | Source | Possible issues only | None |
| Environment | exact `.env` and `.env.*` names | None | Source | None | None |
| Properties | `.ini`, `.cfg`, `.conf` | `text/x-ini`, `text/x-properties` | Source | None | None |

Wildcard MIME entries such as `application/*+json` and `application/*+xml` mean a structured suffix match, not a general glob implementation.

`.env.*` means common environment-file variants such as `.env.local` and `.env.example`; it does not match unrelated filenames containing `.env` in the middle.

## Interaction model

### Opening

There are three intentional entry states:

- Opening a text clip from its gallery preview or a generic open action starts in **Preview**.
- Choosing an explicit **Edit** action starts in **Edit**.
- Recovering an unsaved draft starts in **Edit**, labels the draft as recovered, and opens diagnostics when the recovered source has authoritative errors.

The Preview and Edit tabs retain the existing accessible tab semantics and `Cmd/Ctrl+Shift+P` toggle. Each mode retains its own scroll position while the modal remains open. Closing resets both positions.

Invalid UTF-8 is the only state that prevents both Preview and Edit. The editor shows a format-neutral byte-safety explanation and leaves Save and Save As unavailable.

### Edit mode

CodeMirror provides:

- Syntax coloring selected from the trusted registry result, never automatic language detection.
- Line numbers.
- Matching-bracket feedback.
- Language-appropriate indentation.
- Undo and redo.
- Find and replace.
- Selectable wrapping using the existing persisted wrap preference.
- Cursor line/column and document character count.
- Inline diagnostic markers.

The outer editor shell continues to own filename display, Preview/Edit mode, draft status, Save, Save As, close confirmation, focus trapping, and toasts. CodeMirror does not own persistence or clip lifecycle.

No completion popup, suggestion source, execution command, hover documentation, or project-aware service is registered.

### Preview mode

Preview always renders the current unsaved editor value.

#### Source preview

All non-Markdown, non-table formats use a read-only source renderer with:

- The same trusted registry language mode as Edit.
- Line numbers.
- Selectable text.
- Preserved whitespace.
- The existing wrap preference.
- Safe app-owned token classes.

The renderer converts source tokens into text nodes and app-owned `<span>` elements. It never assigns source text to `innerHTML`. URLs are not automatically activated. HTML remains inert highlighted source.

Invalid syntax does not replace Preview with an error screen. Recoverable syntax trees still produce highlighting; parser or highlighter failure falls back to a plain `<pre>` plus a diagnostic.

#### Markdown preview

Markdown continues to use the existing `MarkdownRenderer` and `MarkdownPreview` pipeline, including DOMPurify, reference resolution, local and remote image policy, request generations, budgets, and the Markdown service. A renderer exception falls back to inert source and a diagnostic instead of a format-specific unavailable screen. Invalid UTF-8 remains unavailable.

#### CSV and TSV preview

CSV and TSV render as bounded tables.

CSV delimiter detection considers comma, tab, semicolon, and pipe. TSV defaults explicitly to tab. The supported dialect uses `"` for quoting, doubled `""` for an escaped quote, and LF, CRLF, or CR record endings. A final empty record caused solely by the document's final newline is ignored; an interior blank record remains data.

CSV detection parses at most the first 64 KiB or 50 nonblank records with each candidate. Candidates that produce fewer than two fields in their modal-width record are discarded. The winner is chosen by, in order: fewest parse errors, fewest records differing from the modal width, largest modal width, then the stable tie order comma, tab, semicolon, pipe. If no candidate qualifies, comma is used and the preview explains that detection was inconclusive.

The first record is treated as a header only when there are at least two records, all records have the same width of at least two fields, and every trimmed first-record field is nonempty and unique. A compact preview control lets the user temporarily override delimiter and header interpretation for the current open clip. Delimiter overrides drive both table parsing and authoritative diagnostics, including the validation performed before Save. Header overrides affect presentation only. Neither override changes source or persists after close.

Malformed quoting is an authoritative error and falls back to source Preview with diagnostics. Inconsistent record widths are warnings because ragged tables can be intentional; the table pads missing display cells without changing source.

Table rendering stops at the first reached limit:

- 500 rows.
- 100 columns.
- 10,000 total cells.

A visible notice reports truncation and directs the user to Edit for complete source.

## Diagnostics

### Levels

Diagnostics have two user-visible levels:

- **Error:** Produced by an authoritative format validator. Any error triggers Save Anyway confirmation.
- **Possible issue:** Produced from CodeMirror/Lezer recovery markers for non-authoritative languages. It never triggers confirmation.

CSV/TSV inconsistent-width findings are nonblocking warnings and use the possible-issue presentation.

Each diagnostic contains:

```text
severity
format/source
message
start line and column
optional end line and column
```

Messages are concise and must not echo `.env` values or large source fragments.

### Drawer

The approved UI is a collapsible bottom diagnostics drawer beneath the editor or preview panel.

- It starts collapsed for ordinary opens.
- A recovered draft with authoritative errors is the one exception: the drawer starts expanded so the recovered invalid state is not hidden.
- New errors do not expand it while the user types.
- The collapsed status reports issue counts.
- It expands when the user activates the count, activates an inline marker, or attempts to save authoritative errors.
- Rows are keyboard-activatable.
- Activating a row switches to Edit when necessary, focuses CodeMirror, selects or places the cursor at the source range, and reveals it.
- The drawer displays at most 100 diagnostics and reports the suppressed remainder.

Validation is debounced during editing, reruns when entering Preview, and runs synchronously from the user's perspective before Save or Save As continues. Asynchronous results carry a source generation and are discarded when stale.

## Validation contracts

### Authoritative formats

- **JSON:** Strict JSON. Comments, trailing commas, `NaN`, and `Infinity` are errors.
- **JSON Lines / NDJSON:** Every nonblank physical line contains exactly one complete strict JSON value. Blank lines are allowed.
- **YAML:** YAML 1.2 core behavior. Duplicate mapping keys are errors. Alias expansion and document complexity are bounded before materialization.
- **TOML:** TOML 1.0 syntax.
- **XML:** Well-formedness plus an app safety rule that any `DOCTYPE` declaration is an authoritative error, whether its subset is internal or external. The parser never resolves external entities or fetches resources. XInclude elements are accepted as inert ordinary markup and are never processed. No schema validation is performed.
- **CSV/TSV:** Quoting and record parsing are authoritative. Inconsistent field counts are warnings rather than errors.

The implementation will use locally bundled parsers behind format adapters. The intended dependency set is:

- CodeMirror/Lezer JSON parsing plus strict `JSON.parse` for JSON and per-line JSON Lines validation.
- `yaml` for YAML 1.2 documents and positioned errors.
- `smol-toml` for TOML 1.0.
- `saxes` for non-resolving XML well-formedness with positions.
- Papa Parse for bounded CSV/TSV parsing and delimiter detection.
- `unbash` for non-authoritative shell parse hints; CodeMirror's legacy shell mode supplies highlighting only.

Dependency versions are pinned by the frontend lockfile. Parser adapters, rather than callers, translate library-specific failures into the shared diagnostic structure.

### Best-effort formats

HTML, CSS, JavaScript, and TypeScript use CodeMirror/Lezer error recovery. Shell uses `unbash` parse failures because CodeMirror has no official shell Lezer grammar. Shell parsing assumes Bash syntax even for `.sh` and `.zsh`, so every finding remains a possible issue rather than an authoritative error. These findings never trigger Save Anyway.

`.env`, INI, CFG, CONF, plain text, and logs receive highlighting only. Their dialects are too ambiguous for reliable initial grammar promises.

## Formatting

Formatting is an explicit action and is never run during preview or save.

- JSON documents format with two-space indentation.
- JSON Lines documents format each nonblank JSON value compactly on one line and preserve blank lines.
- The action is disabled while authoritative syntax errors exist.
- Formatting preserves the document's UTF-8 BOM choice, detected newline style, and final-newline state.
- No YAML, TOML, XML, table, web, shell, or properties formatter is included initially.

Formatting updates the editor value as a normal undoable transaction, marks the clip dirty, schedules draft persistence, and reruns diagnostics.

## Text decoding and fidelity

### Retrieval

`GetClipData` already reports `valid_utf8` and `data_encoding`. Generalize invalid-text handling so any invalid UTF-8 text candidate crosses the Wails/JSON boundary as base64 rather than a potentially replacement-decoded string. Extension-classified application types may also arrive as base64 even when valid, so the frontend text decoder must handle both encodings after classification.

Opening a valid document records a text profile:

```text
UTF-8 BOM present or absent
detected newline style: LF, CRLF, or CR
final newline present or absent
```

The visible editor value excludes the BOM and uses CodeMirror's internal line separator. Save re-encodes from the current value using the recorded profile. Newly created or genuinely separator-free documents default to LF without a BOM.

A normal save preserves a homogeneous source document's newline style. Deliberately mixed newline sequences are outside the fidelity guarantee; their detected dominant style is used when encoding. The final-newline state follows the current editor value, so deliberately adding or removing it is respected.

Save and Save As continue to send base64 bytes through the existing persistence paths. Downloads and other raw-data surfaces remain unchanged.

### Drafts

Drafts remain app-local in `localStorage`, including `.env` drafts. The existing 250 ms draft delay and original-content matching rules remain. Draft records add any profile data required to reconstruct bytes consistently. Successful Save, Save As, or confirmed discard clears the draft; failed or cancelled saves retain it.

## Save flow

Save and Save As use the same sequence:

1. Flush pending editor changes and validation.
2. If authoritative errors exist, expand the diagnostics drawer and show a confirmation with **Cancel** and **Save Anyway**.
3. Ignore possible issues for confirmation purposes.
4. Encode the current value according to the text profile.
5. Call the existing update or upload path.
6. On success, clear the draft, close, and refresh the gallery.
7. On failure, retain value, selection, diagnostics, dirty state, and draft; re-enable actions and show the existing failure toast.

Save Anyway applies only to the current attempted value. Later invalid saves require confirmation again.

## Large-input and abuse limits

The existing 2 MiB Markdown source threshold becomes the common enhanced-preview threshold.

Above 2 MiB:

- Preview uses plain inert source.
- Edit remains available through CodeMirror without a language extension.
- Highlighting, validation, diagnostics, formatting, and table rendering are disabled.
- A short explanation identifies the disabled assistance.

At or below 2 MiB, authoritative validators and the shell hint parser run in a dedicated Web Worker. Each validation generation has a 1.5-second deadline; timeout terminates and recreates the worker. A timed-out or resource-limited validator reports one nonblocking **Validation unavailable within safety limits** notice. It does not claim that the document is valid and does not trigger Save Anyway. Saving remains available because no authoritative syntax error was established.

The worker and renderers enforce these concrete limits:

- YAML: at most 64 documents, 100 aliases per document, and 100,000 parsed nodes; validation does not convert documents into unrestricted JavaScript object graphs.
- XML: maximum element nesting depth 256 and 100,000 combined element/attribute events; every DOCTYPE is rejected before any entity processing.
- CSV/TSV validation: at most 100,000 records and 1,000,000 parsed fields. Table presentation retains the lower 500-row, 100-column, 10,000-cell limits.
- Validator collection: stop after 1,000 findings; the drawer still presents only the first 100 and reports the suppressed count.
- Highlighted source: at most 100,000 token spans. Exceeding the cap falls back to plain inert source with an explanation.

JSON and TOML remain bounded by the common byte limit and worker deadline. Formatting also runs in the worker and uses the same deadline. CodeMirror language parsing is enabled only within the common byte limit; an adapter failure removes the language extension and retains plain editing.

Every request and response carries a source generation. Stale worker results are ignored, and a newer request may terminate superseded work rather than merely waiting for it. Generation guards protect UI state; worker termination supplies the interruption boundary for synchronous parser libraries.

No validator executes source. XML external resources are disabled. HTML is never rendered. Syntax languages are selected from trusted classification rather than expensive automatic detection.

## Component architecture

### `TextFileTypes`

Owns registry normalization and classification. Callers ask for a descriptor; they do not repeat extension or MIME checks.

Representative interface:

```javascript
TextFileTypes.classify({ filename, contentType })
TextFileTypes.isTextCandidate({ filename, contentType })
```

### `CodeEditorAdapter`

Owns CodeMirror state, view, extensions, and commands. `TextClipEditor` depends only on semantic operations such as:

```javascript
mount({ container, value, language, wrap, diagnostics, callbacks })
destroy()
getValue()
setValue(value, { undoable })
focus()
getSelection()
selectRange(range)
revealRange(range)
setWrap(enabled)
openSearch()
setDiagnostics(items)
```

The adapter translates CodeMirror transactions into existing dirty-state, draft, status, and save-state callbacks.

### `TextPreview`

Dispatches by descriptor to:

- Existing Markdown preview.
- `SourcePreviewRenderer`.
- `TablePreviewRenderer`.

All renderers accept the current source and a generation token and return app-owned DOM nodes plus renderer diagnostics.

### `TextDiagnostics`

Debounces validation, owns generations, merges authoritative validator results with possible CodeMirror issues, caps presentation, and controls the drawer. Validators expose one normalized interface and do not manipulate UI.

### `TextCodec`

Decodes `utf8` or base64 clip payloads, rejects invalid UTF-8 for editing, records the fidelity profile, and re-encodes save bytes. This keeps byte preservation out of CodeMirror and renderer modules.

### `TextClipEditor`

Remains the orchestration shell. It owns the active clip, descriptor, mode, drafts, preview/edit scroll, wrap preference, save readiness, and integration with the outer editor controller. Markdown-specific booleans and DOM selectors are replaced by descriptor-driven behavior.

## Dependency packaging

The app will not use a CDN.

Add a reproducible frontend bundling script using esbuild. It produces a checked-in IIFE editor bundle and a checked-in validation-worker artifact exposing only the narrow app-owned surfaces required by classic scripts. CodeMirror, language packages, validators, and their licenses are pinned in `frontend/package-lock.json`.

Normal application startup and production use the checked-in generated bundle without network access. The frontend build command regenerates both Tailwind output and the text-editor bundle. CI verifies that a clean regeneration does not change the committed artifact.

The bundle contains only selected languages and parser adapters; it does not include the TypeScript compiler, Prettier, autocomplete packages, or language-server clients.

## Accessibility

- Preview/Edit remain an accessible tablist with roving selection and Left/Right navigation.
- CodeMirror is configured for keyboard and screen-reader access and has a programmatic editor label.
- Visible focus treatment follows the stone design system.
- Diagnostic counts are announced without announcing every keystroke-level change.
- Drawer rows are buttons or equivalent keyboard-operable controls with severity, location, and message in their accessible names.
- Activating a diagnostic moves focus and selection to the reported source range.
- Save Anyway confirmation exposes the error count and clear Cancel/Save Anyway actions.
- Large-file and invalid-UTF-8 states are associated with the affected panel and announced politely.
- The editor focus trap includes CodeMirror, Preview/Edit tabs, table controls, the diagnostics drawer, and save controls.

## Visual design

The editor continues using the existing stone palette and IBM Plex Mono.

- Preview/Edit tabs generalize the current Markdown tabs.
- Source Preview and CodeMirror share compact line-number gutters and neutral token colors.
- The diagnostics drawer sits below the active panel and is collapsed to a narrow status strip by default.
- Errors use the existing red exception palette; possible issues and table warnings remain neutral stone rather than introducing a new warning color.
- CSV/TSV controls use the existing secondary-button and form-input patterns.
- Controls keep current compact typography and visible focus styling.

## Integration changes

Expected integration areas are:

- `frontend/js/editor/text-clip-editor.js`: descriptor-driven mode orchestration, drafts, validation, formatting, and CodeMirror adapter usage.
- `frontend/js/editor.js`: extension/MIME-aware text eligibility, requested initial mode, base64 text payload handling, and fidelity-aware save bytes.
- New focused frontend modules for type classification, codec, CodeMirror adapter, diagnostics, validators, and preview renderers.
- `frontend/index.html`: generalized tabs/panels, editor mount, diagnostics drawer, table controls, and local generated bundle load.
- `frontend/css/modals.css`: CodeMirror, source preview, table, drawer, focus, and large-file states.
- `frontend/package.json` and lockfile: reproducible editor bundle dependencies and scripts.
- `internal/app/app.go`: generalized invalid-UTF-8 bridge protection and any additional clip-data metadata needed by `TextCodec`.
- `frontend/js/utils.js`: use the shared classification result where presentation depends on a recognized file type; keep existing badge semantics unless explicitly format-specific.
- `frontend/js/editor/markdown-preview.js` and `frontend/js/markdown-renderer.js`: retain specialized behavior behind the generalized preview dispatcher.
- E2E selectors and fixtures: replace direct textarea assumptions with editor helper operations while continuing to test user-visible behavior.
- `docs/docs/features/text-editor.md`: supported types, previews, diagnostics, formatting, limits, fidelity, and shortcuts.

No new Wails runtime import is permitted outside `internal/wailsbridge`. The design does not require a new Wails service or runtime event channel.

## Delivery sequence

The feature ships as one coherent user-facing release but is implemented in test-first milestones:

1. Add classification and codec tests, then the registry and byte-safe text retrieval.
2. Add an adapter contract and migrate the existing text editor to CodeMirror without changing user-visible Markdown behavior.
3. Generalize Preview/Edit tabs and add safe source preview.
4. Add validation adapters, diagnostics state, drawer UI, Save Anyway, and JSON formatting.
5. Add bounded CSV/TSV table preview and temporary interpretation controls.
6. Add all language modes and registry variants.
7. Update documentation and run Markdown security/regression verification plus the complete suite.

Each milestone leaves the editor usable and preserves existing save semantics.

## Test strategy

Development follows red-green-refactor slices.

### Classification

- Every filename variant is case-insensitive and maps to the expected descriptor.
- Recognized filename wins over conflicting MIME.
- Recognized MIME handles extensionless and unknown-extension files.
- Unknown valid `text/*` becomes generic text.
- Unsupported binary MIME without a recognized filename remains ineligible.
- Rename changes frontend mode without unintended backend MIME rewriting.
- Existing Markdown promotion tests remain unchanged.

### Opening and modes

- Generic open starts each registered format in Preview.
- Explicit Edit starts in Edit.
- Recovered drafts start in Edit and are labeled.
- Preview reflects unsaved source.
- Independent scroll positions survive mode toggles but reset after close.
- Existing toggle shortcut and accessible tab behavior work for every previewable text clip.

### CodeMirror compatibility

- Editing, selection, undo/redo, find/replace, wrapping, line/column status, character count, keyboard save, Save As, and dirty close remain functional.
- Drafts persist and restore through the adapter.
- Focus enters the intended panel and remains inside the editor modal.
- Existing tests stop relying on a native textarea and use helper methods that represent user behavior.

### Validation and formatting

For every authoritative format, include valid, invalid, positioned-diagnostic, and Save Anyway cases.

Additional cases cover:

- Duplicate YAML keys, document/node/alias limits, and worker timeout.
- TOML 1.0 syntax and worker timeout.
- XML DOCTYPE rejection, inert XInclude, depth/event limits, and absence of external fetches.
- JSON Lines blank lines and per-line errors.
- CSV/TSV malformed quotes, ragged-row warnings, deterministic delimiter tie-breaking, deterministic header detection, override effects, and parse limits.
- Shell parse failures remaining non-authoritative for `.sh`, `.bash`, and `.zsh`.
- Possible issues never triggering Save Anyway.
- Diagnostic navigation, recovered-draft expansion, 100-item presentation cap, 1,000-item collection cap, stale-result rejection, and drawer expansion rules.
- Timeout/resource-limit notices allowing save without claiming validity.
- JSON and JSON Lines formatting, undoability, disabled invalid formatting, and preserved text profile.

### Preview and security

- Source HTML and scripts remain inert text.
- Registry-selected highlighting does not perform automatic language detection.
- Invalid syntax remains visible as source.
- Renderer failures fall back safely.
- CSV/TSV truncation honors row, column, and total-cell limits.
- Existing Markdown sanitization, links, references, local images, remote loading, budgets, cache, and stale-render tests continue to pass.

### Byte fidelity

Round-trip tests cover:

- UTF-8 with and without BOM.
- LF, CRLF, and CR documents.
- Presence and absence of a final newline.
- Valid extension-classified text arriving as base64.
- Invalid UTF-8 across `text/*`, application MIME, and extension-classified files.
- Failed and cancelled saves retaining original bytes and drafts.

### Limits and accessibility

- Documents above 2 MiB remain plainly previewable/editable while enhanced assistance is disabled.
- Parser-specific limits prevent excessive work below 2 MiB.
- CodeMirror, tabs, table controls, drawer rows, and save confirmation are keyboard operable and named.
- Diagnostic navigation focuses the exact source location.
- Status announcements are useful without becoming keystroke-noisy.

### Verification commands

Run focused editor suites during development, then the complete required suite:

```bash
cd e2e
npx playwright test tests/clips/text-editor.spec.ts
npx playwright test tests/clips/markdown.spec.ts
npx playwright test tests/clips/structured-text-editor.spec.ts
npm test 2>&1 | tail -50
```

Run Go tests for changed backend data retrieval and classification behavior as part of the relevant milestone.

## Acceptance criteria

The feature is complete when:

1. Every registered valid UTF-8 format opens in Preview by default and explicit Edit opens in Edit.
2. Unlisted valid `text/*` clips receive generic Preview/Edit behavior.
3. CodeMirror provides the agreed lightweight developer experience without IDE features.
4. Invalid authoritative formats show navigable diagnostics while source remains visible.
5. Saving authoritative errors requires explicit Save Anyway; possible issues do not.
6. JSON and JSON Lines formatting follows the agreed explicit, undoable contract.
7. CSV and TSV table previews detect or apply delimiters, support temporary header controls, and honor all render limits.
8. HTML and all other source previews remain inert and cannot execute or load content.
9. Invalid UTF-8 cannot be edited and crosses the bridge without byte replacement.
10. Ordinary saves preserve UTF-8 BOM choice, newline style, and final-newline state.
11. Drafts, save flows, shortcuts, search, wrapping, dirty close, scroll behavior, and Markdown features remain intact.
12. Large documents degrade to plain editing/preview without parsing or highlighting.
13. Diagnostics and all editor controls meet the keyboard and screen-reader contract.
14. Every registry mode and authoritative parser has automated coverage.
15. Focused tests, Go tests, a clean generated-bundle check, and the complete E2E suite pass.
