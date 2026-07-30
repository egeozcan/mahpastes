# Structured Text Preview and Editor Design

**Date:** 2026-07-29

**Status:** Approved

## Summary

Expand Markdown's Preview/Edit experience into a general structured-text workspace. Every recognized valid UTF-8 text clip gains both a Preview and an Edit mode backed by a lightweight CodeMirror 6 editor with syntax highlighting, line numbers, bracket matching, indentation support, and diagnostics. Formats whose Preview is a genuinely different artifact — Markdown and CSV/TSV — open in Preview; every other format opens in Edit, because their Preview is read-only source that looks almost identical to the editor.

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
2. Give every recognized text clip both modes, defaulting to whichever mode is actually useful for its format.
3. Provide a lightweight code-editor experience without autocomplete or project-level tooling.
4. Detect reliable grammar errors in JSON, JSON Lines, YAML, TOML, and XML.
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
- Shell parse hints of any kind. Shell files receive highlighting only in this release; see [Best-effort formats](#best-effort-formats).
- Guessing whether arbitrary binary bytes are text.
- Editing invalid UTF-8 or UTF-16 documents. Existing lossy editing of non-UTF-8 `text/*` clips is intentionally removed so saving cannot replace unknown bytes. A user-initiated one-way "reinterpret as Windows-1252 and convert to UTF-8" action is the named follow-up that restores a recovery path for those clips; it is deliberately out of scope here so that this release never writes bytes the user did not see.
- Exact preservation of deliberately mixed newline sequences; the fidelity contract applies to a document's detected newline style.
- Persisting cursor or scroll position after closing and reopening a clip.
- Changing downloads, REST responses, public links, tag servers, or CLI data output.

## Chosen approach

Adopt CodeMirror 6 for Edit mode, keep Preview as a separate app-owned panel, and perform validation locally in the frontend.

Four alternatives were rejected:

1. **Keep the textarea and layer highlighting over it.** This preserves current tests but makes source highlighting, selection synchronization, diagnostics, search highlights, wrapping, and proportional overlays fragile.
2. **Generalize previews first and migrate editing later.** This lowers short-term risk but duplicates editor integration work and temporarily misses the requested developer-focused experience.
3. **Validate in Go instead of JavaScript.** Go parsers behind a Wails binding would shrink both bundles, delete the worker sandboxing question outright, and make the un-interruptible-parse problem someone else's thread. It was rejected because validation is a per-keystroke interaction: every debounced check becomes a bridge round-trip on desktop and an HTTP round-trip in served mode, diagnostics stop being instant, and the position-conversion problem described under [Diagnostics](#diagnostics) has to be solved twice — once in Go and once for CodeMirror. It also adds backend surface for a purely presentational concern. The same reasoning rejects Go validation as the *fallback* for a failed worker spike: it would mean maintaining two complete validator implementations to cover a case that may never occur.
4. **Ship a reduced release one** — CodeMirror, classification, Markdown, and JSON/JSONL only, deferring YAML, TOML, XML, and CSV. Rejected because those parsers share one worker path, one diagnostic shape, and one set of limits with JSON; once that machinery exists, each additional format is an adapter and a test table, not new architecture. Deferring them would ship "JSON editing with highlighting" under the name of a structured-text workspace, and YAML in particular is a format where a missed indentation error costs real time. The formats stay in scope; the risk is managed by the milestone order in [Delivery sequence](#delivery-sequence), where every parser lands after the executor is proven.

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

1. A specific recognized filename or extension, including Markdown, structured data, web/code, shell, `.env`, and properties formats.
2. A specific recognized MIME type.
3. A generic plain-text filename: `.txt`, `.text`, or `.log`.
4. Generic text for any remaining `text/*` MIME type.
5. Not a text-editor type.

Specific filename matches therefore win when MIME disagrees. The generic plain-text family deliberately does not outrank a specific MIME: a pasted `pasted_text_<timestamp>.txt` clip that the backend identifies as `application/json` or `text/html` retains JSON or HTML behavior. An unknown filename likewise does not suppress a known MIME fallback. Classification changes presentation and editor eligibility; it does not silently rewrite stored content type.

One guard limits filename-first matching: a filename match does not make a clip a text candidate when the MIME is a **known media family** — `image/*`, `audio/*`, or `video/*`. This matters because extensions collide across domains: `.ts` is both TypeScript and MPEG transport stream, and several platforms sniff `.ts` as `video/mp2t`. Without the guard, opening a transport-stream video would enter the text editor and dead-end on the byte-safety screen. With it, such clips take the ordinary non-text open path.

`application/octet-stream` is deliberately **not** in that list. It is the generic "I don't know" type that uploads and watch folders routinely produce, so blocking it would make a perfectly good `config.yaml` or `notes.md` uneditable — a regression, not a guard. Instead, a recognized text filename under `application/octet-stream` is a candidate *conditionally*: the retrieval path's fatal UTF-8 decode decides. Valid UTF-8 opens normally; invalid bytes get the byte-safety screen, which is the correct answer for a genuinely binary file that happened to be named `.yaml`. This costs nothing extra, because `valid_utf8` is already computed for every clip.

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
| XML | `.xml` | `application/xml`, `text/xml`, `application/*+xml` | Source | Well-formedness | None |
| CSV | `.csv` | `text/csv` | Table | CSV structure | None |
| TSV | `.tsv` | `text/tab-separated-values` | Table | TSV structure | None |
| HTML | `.html`, `.htm` | `text/html` | Source | Possible issues only | None |
| CSS | `.css` | `text/css` | Source | Possible issues only | None |
| JavaScript | `.js`, `.mjs`, `.cjs` | `text/javascript`, `application/javascript` | Source | Possible issues only | None |
| TypeScript | `.ts`, `.mts`, `.cts` | `text/typescript`, `application/typescript` | Source | Possible issues only | None |
| Shell | `.sh`, `.bash`, `.zsh` | `text/x-shellscript`, `application/x-sh` | Source | None | None |
| Environment | exact `.env` and `.env.*` names | None | Source | None | None |
| Properties | `.ini`, `.cfg`, `.conf` | `text/x-ini`, `text/x-properties` | Source | None | None |

The **Preview** column also determines the default open mode: the three modes with a non-source preview — Markdown, CSV, and TSV — open in Preview, and every `Source` row opens in Edit. See [Opening](#opening).

Wildcard MIME entries such as `application/*+json` and `application/*+xml` mean a structured suffix match, not a general glob implementation.

`.env.*` means common environment-file variants such as `.env.local` and `.env.example`; it does not match unrelated filenames containing `.env` in the middle.

## Interaction model

### Opening

Every registered text clip has both modes. The default mode is a property of the descriptor, not of the entry point:

- **Preview-default formats: Markdown, CSV, and TSV.** Their Preview is a materially different artifact — rendered GFM or a parsed table — so landing there is worth a click to get back to source.
- **Edit-default formats: everything else.** For plain text, logs, `.env`, INI/CFG/CONF, shell, JSON, JSON Lines, YAML, TOML, XML, HTML, CSS, JavaScript, and TypeScript, Preview is read-only highlighted source that looks nearly identical to the editor. Defaulting those to Preview would cost a click and invite "why can't I type here". Preview remains one keystroke or one tab click away.

Three entry states layer on top of that default:

- A generic open action — gallery card click, the linked-text reference path — uses the descriptor's default mode.
- An explicit **Edit** action always starts in **Edit**, including for Markdown and CSV/TSV.
- Recovering an unsaved draft always starts in **Edit**, labels the draft as recovered, and opens diagnostics when the recovered source has authoritative errors.

Callers express this through `openEditor(clipID, { initialMode: 'preview' | 'edit' | 'default' })`; they do not simulate a tab click after opening. Generic open actions pass `default` (or omit the option) and let the descriptor decide. The explicit **Edit** entry points — the card context menu (`frontend/js/ui.js:579`), the lightbox file menu (`frontend/js/modals.js:308`), and the lightbox controller's `editClip` callback (`frontend/js/app.js:176`) — pass `edit`.

The Preview and Edit tabs retain the existing accessible tab semantics and `Cmd/Ctrl+Shift+P` toggle. The generalized shortcut registers as `editor.preview_toggle` with the label **Toggle Preview/Edit**. On first load, shortcut storage copies any custom override from the legacy `editor.markdown_preview` ID when the new ID has no override, then removes the legacy entry. The default binding remains unchanged.

Each mode retains its own scroll position while the modal remains open. Closing resets both positions.

Invalid UTF-8 is the only state that prevents both Preview and Edit. This intentionally replaces today's lossy editing of some Latin-1 or otherwise invalid `text/*` clips. The loss happens in Go, not the frontend: `internal/app/app.go:922-927` converts the bytes with `string(data)` and labels the payload `utf8` for every `text/*` clip that is not invalid Markdown, so a `text/plain` clip containing byte `FF` crosses the bridge already replacement-decoded. `frontend/js/editor.js:183-184` merely forwards what it was given. Generalizing the base64 path is therefore a backend change, and no amount of frontend work can recover bytes that were destroyed before the frontend saw them. The editor shows a format-neutral byte-safety explanation and leaves Save and Save As unavailable; user documentation calls out the behavior change.

Because the app has no encoding conversion anywhere, this leaves a real gap: a Windows-1252 `.txt` clip becomes permanently uneditable with no recovery path. The accepted resolution is a **named follow-up**, not silence — a user-initiated, one-way "reinterpret as Windows-1252 → convert to UTF-8" action that shows the decoded result before writing anything. The byte-safety screen in this release names that follow-up so the state reads as deliberate rather than broken.

### Edit mode

CodeMirror provides:

- Syntax coloring selected from the trusted registry result, never automatic language detection.
- Line numbers.
- Matching-bracket feedback.
- Language-appropriate indentation.
- Undo and redo.
- Find and replace through the existing stone-styled app panel.
- Selectable wrapping using the existing persisted wrap preference.
- Cursor line/column and document character count.
- Inline diagnostic markers.

The outer editor shell continues to own filename display, Preview/Edit mode, the existing find/replace panel and its `N of M` status, draft status, Save, Save As, close confirmation, focus trapping, and toasts. The panel calls app-owned adapter methods backed by `@codemirror/search`; CodeMirror's default search panel is not mounted. CodeMirror does not own persistence or clip lifecycle.

Two search changes are deliberate rather than incidental:

- **Case-sensitive and whole-word toggles are new.** Today's panel (`frontend/index.html:992-1004`) has no such controls and matches unconditionally case-insensitively (`matchStarts` lowercases both sides; `replaceAll` uses `/giu`). `@codemirror/search` supports both natively via `SearchQuery`, so they are added as two new toggle buttons in the existing panel. Both default **off**, preserving today's behavior for anyone who never touches them. The toggles are not persisted across opens.
- **The mirrored highlight layer is retired.** `#text-editor-highlight-layer` and its `syncHighlightLayer`/`revealActiveSearchMatch`/`renderSearchHighlights` machinery exist only because a native textarea stops painting its selection when focus moves into the find input (see the comment at `frontend/js/editor/text-clip-editor.js:296-298`). CodeMirror's `drawSelection` extension paints selection from its own layer and must be enabled so matches stay visible while the query field holds focus. The adapter mounts `drawSelection` and `highlightSelectionMatches`; the layer element, its CSS, and its `ResizeObserver` are deleted. Losing this without a replacement would be a visible find/replace regression, so a test asserts the active match is visible while focus is in the query input.

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

CSV delimiter detection considers comma, tab, semicolon, and pipe. TSV initially prefers tab. The supported dialect uses `"` for quoting, doubled `""` for an escaped quote, and LF, CRLF, or CR record endings. A final empty record caused solely by the document's final newline is ignored; an interior blank record remains data.

CSV detection parses at most the first 64 KiB or 50 nonblank records with each candidate. Candidates that produce fewer than two fields in their modal-width record are discarded. The winner is chosen by, in order: fewest parse errors, fewest records differing from the modal width, largest modal width, then the stable tie order comma, tab, semicolon, pipe. If no candidate qualifies, comma is used and the preview explains that detection was inconclusive.

The first record is treated as a header only when there are at least two records, all records have the same width of at least two fields, and every trimmed first-record field is nonempty and unique. A compact preview control lets the user temporarily choose a delimiter and override header interpretation for the current open clip. Header overrides affect presentation only. Neither override changes source or persists after close.

**No CSV or TSV finding is ever authoritative in this release.** Malformed quoting and inconsistent record widths are possible issues regardless of whether the delimiter was inferred or explicitly chosen. They may change the table or fall back to source Preview, but they never trigger Save Anyway. The table pads missing display cells without changing source, because ragged tables are frequently intentional.

An earlier draft promoted malformed quoting to an authoritative error once the user explicitly selected a delimiter. That was cut: it introduced three interacting states (inferred → warnings, explicit → errors, reopen → back to warnings), required reasoning about whether a non-persisted choice could create a hidden future save gate, and added roughly four test cases — all to block a save on a heuristic reading of a file format that has no single authoritative grammar. Promotion remains available as a later change if real usage shows it is wanted; nothing here forecloses it.

Table rendering stops at the first reached limit:

- 500 rows.
- 100 columns.
- 10,000 total cells.

A visible notice reports truncation and directs the user to Edit for complete source.

## Diagnostics

### Levels

Diagnostics have two user-visible levels:

- **Error:** Produced by an authoritative format validator. Errors may trigger Save Anyway confirmation — see [Save confirmation policy](#save-confirmation-policy).
- **Possible issue:** Produced from CodeMirror/Lezer recovery markers for non-authoritative languages. It never triggers confirmation.

All CSV/TSV findings are nonblocking and use the possible-issue presentation.

### Save confirmation policy

Save Anyway confirmation fires only when the current save would introduce or change authoritative errors relative to the document as it was opened:

- The document was **valid at open** and now has authoritative errors → confirm. This is the case that matters: the user's edit broke the file.
- The document was **already invalid at open** and the error set is unchanged → save without confirmation.
- The document was already invalid at open and the error set **changed** (different messages, positions, or count) → confirm, because the user moved the damage rather than leaving it alone.

The baseline is the **persisted source** — the bytes as stored — validated once per open **before** draft recovery, and compared by normalized message plus start position. A successful save resets the baseline to the saved state, so the next edit is measured against what is now on disk.

Capturing the baseline after draft recovery would defeat the entire policy. Stored source `{}` plus a recovered draft of `{"x":}` would classify the user's own error as pre-existing and let it silently overwrite valid stored bytes — precisely the case the confirmation exists to catch. Recovered drafts are edits like any other; they are measured against what is on disk, not treated as the starting point.

This is a deliberate change to existing behavior, not a preservation of it. Today any invalid JSON prompts on every save (`frontend/js/editor/text-clip-editor.js:629-642`); under this policy a JSON clip that was already invalid when opened saves without a prompt. The existing e2e case (`e2e/tests/clips/text-editor.spec.ts:49-77`) opens a valid document and breaks it, so it still prompts and still passes — but the change must be stated in the release notes rather than described as a no-op.

The reason is that a whole class of real files is invalid by construction and never becomes valid: Helm `templates/*.yaml` full of `{{ }}`, GitHub Actions expressions, JSON-with-comments saved as `.json`, XML fragments without a single root. Confirming on every save of such a file trains the user to click through the dialog, which destroys its value for the case it exists to catch. Errors are still shown, still counted, still navigable — the confirmation is simply reserved for damage the user just did.

The confirmation dialog states which case it is ("This edit introduced 3 YAML errors" versus "3 YAML errors changed"), exposes the error count, and offers Cancel and Save Anyway.

Each diagnostic contains:

```text
severity
format/source
message
start line and column
optional end line and column
```

**Positions are 1-based lines and 1-based columns measured in UTF-16 code units**, matching CodeMirror's document model, because every consumer of a position — marker placement, `selectRange`, `revealRange` — is CodeMirror. The parsers do not agree with each other or with CodeMirror on this: `saxes` and `yaml` report byte or code-point offsets, and `JSON.parse` reports a code-unit index into the whole string. Each adapter converts to the shared unit at its boundary; no unconverted offset crosses into `TextDiagnostics`. The canonical test input is `{"😀": }` — an astral character before the error position, where byte, code-point, and code-unit columns all differ, so an unconverted offset lands the cursor in the wrong place instead of failing loudly.

Messages are concise and must not echo `.env` values or large source fragments.

### Drawer

The approved UI is a collapsible bottom diagnostics drawer beneath the editor or preview panel.

- It starts collapsed for ordinary opens.
- A recovered draft with authoritative errors is the one exception: the drawer starts expanded so the recovered invalid state is not hidden.
- New errors do not expand it while the user types.
- The collapsed status reports issue counts.
- It expands when the user activates the count, activates an inline marker, or attempts a save that triggers confirmation. A save that proceeds without confirmation — unchanged pre-existing errors — leaves the drawer as the user left it.
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
- **XML:** Well-formedness only. Well-formed DOCTYPE declarations are accepted as inert syntax. The parser never resolves internal or external entities into app behavior, fetches resources, or processes XInclude elements. No schema validation is performed.

  Entity handling needs an explicit rule, since "inert DOCTYPE" and "well-formedness" pull in opposite directions. For `<!DOCTYPE r [<!ENTITY x "ok">]><r>&x;</r>`: the internal subset is parsed for syntax, the declaration is recorded but **never expanded**, and the reference `&x;` is accepted without expansion. A reference to an entity that was never declared is also accepted rather than reported as an error, because distinguishing the two requires exactly the entity table this design refuses to build. The five predefined entities and numeric character references behave normally. The practical consequence is stated plainly: XML validation catches structural malformation — unclosed tags, mismatched names, bad nesting, illegal characters — and deliberately does not catch entity-level mistakes.
- **CSV/TSV:** Not authoritative in any state. All findings are possible issues.

The implementation will use locally bundled parsers behind format adapters. The intended dependency set is:

- CodeMirror/Lezer JSON parsing plus strict `JSON.parse` for JSON and per-line JSON Lines validation.
- `yaml` for YAML 1.2 documents and positioned errors.
- `smol-toml` for TOML 1.0.
- `saxes` for non-resolving XML well-formedness with positions.
- Papa Parse for bounded CSV/TSV parsing and delimiter detection.

No shell parser is bundled. An earlier draft included `unbash` for shell parse hints; it was cut because every finding it can produce is non-authoritative by design, so it bought advisory squiggles at the cost of a parser in both the editor bundle and the worker bundle. Shell files get highlighting from CodeMirror's legacy stream mode, which is where the value actually is.

Dependency versions are pinned by the frontend lockfile. Parser adapters, rather than callers, translate library-specific failures into the shared diagnostic structure.

### Best-effort formats

HTML, CSS, JavaScript, and TypeScript use CodeMirror/Lezer error recovery. These findings never trigger Save Anyway.

Shell, `.env`, INI, CFG, CONF, plain text, and logs receive highlighting only. Their dialects are too ambiguous for reliable initial grammar promises — for shell specifically, a bundled parser would have to assume Bash syntax even for `.zsh`, producing findings that could never be more than advisory.

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

Desktop `GetClipData` already reports `valid_utf8` and `data_encoding`. Generalize invalid-text handling so any invalid UTF-8 text candidate crosses the Wails/JSON boundary as base64 rather than a potentially replacement-decoded string. Extension-classified application types may also arrive as base64 even when valid, so the frontend text decoder must handle both encodings after classification.

Server mode must expose the same logical contract. `frontend/js/rest-glue.js` retrieves clip metadata from `GET /api/v1/clips/{id}` alongside the raw data response, returns the exact filename and content type, marks the payload as base64, and computes `valid_utf8` with a fatal UTF-8 decode over the bytes. It does not infer text from the base64 string. Desktop and server tests run the same `TextCodec` contract fixtures.

Two properties of the server path need to be designed rather than assumed:

- **Atomicity.** Desktop reads content type, bytes, and filename in one query (`internal/app/app.go:904`). The server path as described issues two independent requests — metadata from `GET /api/v1/clips/{id}` (`internal/app/api_manager.go:1334-1359`) and bytes from the data route (`1398-1420`) — so a concurrent update can pair one clip's metadata with another revision's bytes, e.g. `config.json` metadata over PNG bytes. Rather than paper over this, server mode gains one additive endpoint, `GET /api/v1/clips/{id}/text`, returning `{filename, content_type, data (base64), valid_utf8}` from a single query — the exact shape `TextCodec` already consumes on desktop. `rest-glue.js` calls that one endpoint; the two-request composition is not used for editor opens.
- **A hard editable-byte cap.** `frontend/js/rest-glue.js:101-116` currently permits 64 MiB inline, and the editor path would materialize that as a Blob, a base64 string, a decoded string, a CodeMirror document, and possibly a preview simultaneously. Text clips above **16 MiB** are not editable in either mode; the editor declines to open with the existing "too large" explanation and points at download. This sits above the 2 MiB enhanced-assistance threshold and is a separate, harder limit.

**The decoding itself is a bug fix, not just a contract alignment.** Server-mode text editing is broken today: `frontend/js/rest-glue.js:116` returns base64 in `data` and a hardcoded `filename: ''`, while `frontend/js/editor.js:179-185` passes `clipData.data` straight through as the editor's text. Opening a text clip in server mode therefore shows base64, and Markdown detection can never fire because the filename is empty. The work is written up here as part of the codec contract, but it must land with a deliberate regression test — a text clip opened in server mode shows its decoded text and its real filename — rather than being absorbed silently into the refactor.

Opening a valid document records a text profile:

```text
UTF-8 BOM present or absent
detected newline style: LF, CRLF, or CR
final newline present or absent
```

The visible editor value excludes the BOM and uses CodeMirror's internal line separator. Save re-encodes from the current value using the recorded profile. Newly created or genuinely separator-free documents default to LF without a BOM.

A normal save preserves a homogeneous source document's newline style. Deliberately mixed newline sequences are outside the fidelity guarantee; their detected dominant style is used when encoding. The final-newline state follows the current editor value, so deliberately adding or removing it is respected.

The fidelity contract is asserted **on bytes, not on decoded text**. Opening `EF BB BF 61 0D 0A` and saving without editing must write back exactly `EF BB BF 61 0D 0A`; a test comparing decoded strings would pass while the BOM or the CR silently vanished.

One case cannot be preserved and must be refused rather than mangled: a document containing an **unpaired surrogate**. CodeMirror holds JavaScript strings, so a lone `\uD800` can exist in the editor value, and `TextEncoder` encodes it as `EF BF BD` — exactly the silent byte replacement this design exists to prevent. Save and Save As therefore refuse a value containing unpaired surrogates, naming the offending position, instead of writing replacement bytes. This is rare enough to be a guard rather than a workflow, but it is the same principle as the invalid-UTF-8 read-only state.

Save and Save As continue to send base64 bytes through the existing persistence paths. Downloads and other raw-data surfaces remain unchanged.

### Drafts

Drafts remain app-local in `localStorage`, including `.env` drafts. The existing 250 ms draft delay and original-content matching rules remain. New records use the `mahpastes:text-editor-draft:v2:` prefix and include the text profile required to reconstruct bytes consistently. Recovery checks v2 first, then performs a one-time tolerant migration from the existing v1 key by deriving the profile from `originalText`, writing v2, and removing v1. Successful Save, Save As, or confirmed discard clears both possible keys; failed or cancelled saves retain the active v2 record.

Migration has one trap that must be handled explicitly. A v1 record stores `originalText` exactly as the backend handed it over — `internal/app/app.go:926` returns `string(data)`, so a BOM'd document's v1 `originalText` **begins with U+FEFF**. The v2 editor value excludes the BOM. Recovery matches a draft to its clip by comparing `draft.originalText === originalText` (`frontend/js/editor/text-clip-editor.js:117-118`), so unless migration strips a leading U+FEFF from both the v1 `originalText` and the v1 `text` before comparing and rewriting, every draft on a BOM'd document silently fails to match and is discarded. The migration therefore:

- Detects a leading U+FEFF in the v1 `originalText`, records `bom: true` in the v2 profile, and strips it from both stored strings.
- Derives the newline style from the dominant sequence in the v1 `originalText`, defaulting to LF.
- Derives final-newline state from the v1 `text`.

A test covers migration of a CRLF + BOM draft specifically, since that combination exercises every derived field at once.

## Save flow

Save and Save As operate on an **immutable snapshot**, not on live editor state. Activating Save captures `{ source, target filename, target content type, generation }` and every later step — validation, confirmation, encoding, upload — refers only to that snapshot. Without this, the 1.5-second validation window is a race: the user clicks Save on `{}`, types `,` while validation is pending, and generation N's approval is applied to generation N+1's bytes. Editing during a pending save is allowed; it simply does not affect the save in flight, and the editor stays dirty afterward relative to the newer value.

The sequence is:

1. Flush pending editor changes and capture the snapshot.
2. Resolve the target filename: the current filename for Save, or the user-proposed filename for Save As.
3. Reclassify from the target filename and target content type, then validate the current source with that target descriptor. A Save As from `notes.txt` to `notes.json`, for example, receives strict JSON validation before upload.
4. If authoritative errors exist **and** they are new or changed relative to the open-time baseline (see [Save confirmation policy](#save-confirmation-policy)), expand the diagnostics drawer and show a confirmation with **Cancel** and **Save Anyway**. Errors that were already present at open and are unchanged expand nothing and block nothing.
5. Ignore possible issues for confirmation purposes.
6. Encode the current value according to the text profile.
7. Call the existing update or upload path. Reclassification controls validation and presentation; the frontend does not itself rewrite stored MIME.

   Note that the *backend* already can. `internal/app/app.go:947-957` sniffs uploads whose content type is `text/plain` or empty and promotes them to `application/json` or `text/html`. Save As from `notes.txt` to `copy.json` sends the original `text/plain` and the backend stores `application/json` — a rewrite this design neither introduces nor prevents. The acceptance criterion is that *this feature* adds no new silent rewriting; existing sniffing behavior is unchanged and is asserted as-is by test rather than described as absent.
8. On success, clear the draft, close, and refresh the gallery.
9. On failure, retain value, selection, diagnostics, dirty state, and draft; re-enable actions and show the existing failure toast.

Save Anyway applies only to the current attempted value; a later save whose errors changed again requires confirmation again. A successful save resets the open-time error baseline to what was just written.

Step 3's validation may wait on the worker for up to the 1.5-second deadline. Save and Save As therefore enter their existing `Saving…` disabled state as soon as the user activates them, before validation resolves, so the wait never reads as an unresponsive button. A validation timeout produces the nonblocking notice described in [Large-input and abuse limits](#large-input-and-abuse-limits) and the save proceeds.

## Large-input and abuse limits

The existing 2 MiB Markdown source threshold becomes the common enhanced-preview threshold.

Above 2 MiB:

- Preview uses plain inert source.
- Edit remains available through CodeMirror without a language extension.
- Highlighting, validation, diagnostics, formatting, and table rendering are disabled.
- A short explanation identifies the disabled assistance.

At or below 2 MiB, authoritative validators run in a dedicated Web Worker. Each validation generation has a 1.5-second deadline; timeout terminates and recreates the worker. A timed-out or resource-limited validator reports one nonblocking **Validation unavailable within safety limits** notice. It does not claim that the document is valid and does not trigger Save Anyway. Saving remains available because no authoritative syntax error was established.

The worker and renderers enforce these concrete limits:

- YAML: at most 64 documents, 100 aliases per document, and 100,000 parsed nodes; validation does not convert documents into unrestricted JavaScript object graphs.
- XML: maximum element nesting depth 256 and 100,000 combined element/attribute events; DOCTYPE and XInclude remain inert, and no entity or resource resolution is performed.
- CSV/TSV validation: at most 100,000 records and 1,000,000 parsed fields. Table presentation retains the lower 500-row, 100-column, 10,000-cell limits.
- Validator collection: stop after 1,000 findings; the drawer still presents only the first 100 and reports the suppressed count.
- Highlighted source: at most 100,000 token spans. Exceeding the cap falls back to plain inert source with an explanation.

JSON and TOML remain bounded by the common byte limit and worker deadline. Formatting also runs in the worker and uses the same deadline. CodeMirror language parsing is enabled only within the common byte limit; an adapter failure removes the language extension and retains plain editing.

Every request and response carries a source generation. Stale worker results are ignored, and a newer request may terminate superseded work rather than merely waiting for it. Generation guards protect UI state; worker termination supplies the interruption boundary for synchronous parser libraries.

### Fallback when the worker is unavailable

The worker is the preferred execution context, not a requirement. If the milestone 1 spike shows a supported surface cannot load a static worker — the plausible case is a macOS WKWebView custom-scheme restriction, and this codebase has no `new Worker` anywhere today, so there is no prior evidence either way — validation degrades to a **time-sliced main-thread executor** rather than blocking the release:

- The scheduler processes one unit of work at a time, yielding between units.
- A per-generation deadline of 250 ms of cumulative main-thread time (lower than the worker's 1.5 s, since the UI thread is shared) abandons the run and emits the same **Validation unavailable within safety limits** notice.
- Generation guards carry over verbatim; only the interruption boundary changes, from worker termination to cooperative abandonment between units.

**The deadline in this mode is best-effort, not a hard interrupt, and the design must not pretend otherwise.** `yaml`, `smol-toml`, `saxes`, and `JSON.parse` are synchronous and cannot be preempted mid-call; a deadline is only observed *between* units, so one pathological document can block the UI for the duration of a single parse regardless of the 250 ms budget. The mitigations are therefore about bounding the worst single call, not about interrupting it:

- The byte ceiling for running validators at all drops to **64 KiB** in fallback mode — small enough that a worst-case single parse stays in the low tens of milliseconds for every bundled parser. Above it, the format behaves exactly like an over-2-MiB document: plain editing, no diagnostics.
- Units are split as finely as each format allows: JSON Lines per line, YAML per document, CSV per chunk. JSON, TOML, and XML have no natural sub-document split and are one unit each — which is exactly why the byte ceiling, not the deadline, is the real bound.
- Parser-specific limits (alias counts, nesting depth, event counts) apply unchanged and do more work here than in the worker, since they bound a call that cannot be killed.

If a supported surface both fails the worker probe *and* proves unable to meet these bounds in practice, the honest fallback is to disable validation on that surface — showing highlighting and editing with no diagnostics — rather than to accept UI stalls. Moving synchronous parsing of arbitrary-size documents onto the UI thread remains forbidden in every configuration.

Because the executor sits behind the same request/response interface as the worker, the choice is a construction-time detail invisible to `TextDiagnostics`. The fallback is selected per surface at startup by a capability probe, not by hardcoding a platform. The probe constructs the worker from a **root-absolute** URL (`/dist/text-validator.worker.js`), exchanges one round-trip, and falls back on failure, timeout, **or a response that is not the expected handshake** — the last case matters because a relative URL resolved against a deep path can hit the SPA fallback and return `index.html`, which constructs a "successful" worker that does nothing useful. Formatting follows the same path. This means a spike failure costs one platform some responsiveness and a lower byte ceiling; it does not stall the feature and does not put unbounded synchronous parsing on the UI thread.

No validator executes source. XML external resources are disabled. HTML is never rendered. Syntax languages are selected from trusted classification rather than expensive automatic detection.

## Component architecture

### `TextFileTypes`

Owns registry normalization and classification. Callers ask for a descriptor; they do not repeat extension or MIME checks.

Representative interface:

```javascript
TextFileTypes.classify({ filename, contentType })
TextFileTypes.isTextCandidate({ filename, contentType })
```

`isTextCandidate` applies the media-MIME guard described in [Registry](#registry): a filename match does not confer candidacy when the content type is `image/*`, `audio/*`, or `video/*`, while `application/octet-stream` stays conditional on the UTF-8 decode. Callers that gate the editor as a whole rather than the text path specifically must OR this with their own image check — see the `frontend/js/ui.js:426` note in [Integration changes](#integration-changes).

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
setSearchQuery(query, { caseSensitive, wholeWord })
findNext()
findPrevious()
replaceCurrent(replacement)
replaceAll(replacement)
clearSearch()
setDiagnostics(items)
```

The adapter translates CodeMirror transactions into existing dirty-state, draft, status, and save-state callbacks. The existing app-owned find panel remains the only search UI and delegates these semantic operations to `@codemirror/search`; CodeMirror's stock search panel stays disabled. The adapter's mounted extension set must include `drawSelection`, without which search matches stop painting whenever focus sits in the find input — the regression the retired highlight layer used to prevent.

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

Add a reproducible frontend bundling script using an exact, non-range esbuild version in `frontend/package.json` and `frontend/package-lock.json`. It produces two committed files:

```text
frontend/dist/text-editor.bundle.js
frontend/dist/text-validator.worker.js
```

The IIFE editor bundle exposes only the narrow app-owned surface required by classic scripts. The static worker artifact contains validators and receives source through messages; it is never constructed from `blob:` or `data:`. CodeMirror, language packages, validators, and their licenses are pinned in the lockfile. Adding dependencies also updates Wails' `frontend/package.json.md5` install sentinel.

Normal application startup and production use the committed generated files without network access. `npm run build:text-editor` regenerates them.

Two things keep the committed artifacts from drifting:

- **The bundle build is chained into the build Wails already runs.** `wails.json` sets `frontend:build` to `npm run build`, which today is Tailwind only — nothing would regenerate the bundles during `wails build`, so a developer who edits bundle sources and forgets `npm run build:text-editor` ships stale artifacts that pass every test. `npm run build` therefore becomes `build:css && build:text-editor`. This adds no new class of requirement: `wails.json` already declares `frontend:install: npm install`, gated by the `frontend/package.json.md5` sentinel, so a build machine already needs npm and network for the first install.
- **`npm run check:text-editor-bundle`** builds into a temporary directory and byte-compares both outputs with the committed files. It is the verification net for the case where someone commits a hand-edited or partially-rebuilt artifact. The implementation plan adds this command to local verification and release checks rather than claiming a nonexistent general test CI job.

Byte comparison requires `.gitattributes` entries marking `frontend/dist/*.js` as `-text` (no EOL conversion). esbuild output is deterministic for a pinned version and fixed options, but git is not: a Windows checkout with `core.autocrlf=true` rewrites line endings on checkout and the comparison fails for reasons unrelated to the build.

"Deterministic" also requires pinning the inputs, not just the esbuild version. The build script fixes, and the spec treats as part of the artifact contract:

- Exact entrypoints — one per output, no glob expansion whose order could vary.
- `format: iife` for the editor bundle, `format: iife` for the worker, an explicit shared `target` (the oldest engine among WKWebView, WebView2, and supported browsers), `bundle: true`, `minify: true`, `sourcemap: false`.
- `legalComments: 'inline'`, so bundled MIT/BSD notices ship inside the artifact rather than being stripped — these are third-party licenses, not incidental comments.
- A generated-file banner naming the source command and warning against hand edits.
- `frontend:install` in `wails.json` becomes `npm ci` rather than `npm install`, so a build resolves the lockfile exactly instead of drifting within semver ranges. This is the difference between a reproducible artifact and one that changes because a transitive dependency published a patch.

The release check is `npm run build && git diff --exit-code frontend/dist`, which fails if a regenerated artifact differs from the committed one for any reason — stale commit, wrong Node version, or drifted dependency.

**Size budget.** CodeMirror core, six language packages, and four parsers appear in two artifacts (`text-editor.bundle.js` carries CodeMirror and the languages; `text-validator.worker.js` carries the parsers; JSON handling appears in both). The combined committed size must stay under **1.5 MB minified**, measured by `check:text-editor-bundle` and reported when it fails. These files are embedded into the binary via `frontend/embed.go` (`all:dist`) and re-committed on every dependency bump, so the budget is both a runtime and a repository-churn constraint. Exceeding it is a signal to drop a language, not to raise the number silently.

Before CodeMirror/parser integration, milestone 1 proves a static worker can load, exchange a request, time out, terminate, and restart in production-style Wails v2 builds on macOS, Windows, and Linux and in served web mode. Server CSP needs no change for this: `spaCSP` at `internal/app/api_manager.go:241` already begins `default-src 'self'`, which `worker-src` inherits, and no `blob:` allowance is wanted. An explicit `worker-src 'self'` may be added as documentation, but its absence is not a spike failure and the spike's pass/fail must not be attributed to it.

A surface that fails the spike does **not** stop the release. It switches to the time-sliced main-thread executor described in [Fallback when the worker is unavailable](#fallback-when-the-worker-is-unavailable), which is capability-probed at startup rather than hardcoded per platform. What remains forbidden is the thing the gate was written to prevent: unbounded synchronous parsing on the UI thread.

The bundle contains only selected languages and parser adapters; it does not include the TypeScript compiler, Prettier, autocomplete packages, language-server clients, or any shell parser.

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

- `frontend/js/editor/text-clip-editor.js`: descriptor-driven mode orchestration, draft-v2 migration, validation, formatting, custom search-panel coordination, and CodeMirror adapter usage. Delete the mirrored highlight layer and its sync/reveal helpers.
- `frontend/js/editor.js`: widen eligibility, accept `openEditor(clipID, { initialMode })`, decode either payload encoding, reclassify Save As targets, and produce fidelity-aware save bytes. **`isEditableType` cannot simply be replaced by `TextFileTypes.isTextCandidate`:** the existing helper (`frontend/js/editor.js:18-22`) also returns true for `image/*`, and two call sites depend on that. The replacement is `isTextCandidate({ filename, contentType }) || isImageType(contentType)` wherever the check gates the editor as a whole, and bare `isTextCandidate` only where the text path is already isolated.
- `frontend/js/ui.js`: pass both filename and content type to eligibility checks. `ui.js:426` gates the card context menu's **Edit** item for *every* editable clip including images — narrowing it to text-only silently removes image editing from the card menu, so it takes the combined check. `ui.js:1177` sits in the non-image `else` branch and takes the text-only check. `ui.js:579` (card menu Edit) passes `initialMode: 'edit'`.
- `frontend/js/modals.js`: lightbox file-menu Edit (`modals.js:308`) opens directly in Edit.
- `frontend/js/rest-glue.js`: provide filename, `data_encoding`, and `valid_utf8` for server-mode clip data, replacing the current `filename: ''` and raw-base64 `data` at `rest-glue.js:116`.
- `frontend/js/app.js` and `frontend/js/shortcuts.js`: register the generalized preview toggle and migrate the legacy shortcut override ID (currently `editor.markdown_preview` at `app.js:1733`). `app.js:176` is a third explicit Edit entry point — the lightbox controller's `editClip` callback — and passes `initialMode: 'edit'`.
- New focused frontend modules for type classification, codec, CodeMirror adapter, diagnostics, validators, and preview renderers.
- `frontend/index.html`: generalized tabs/panels, editor mount, diagnostics drawer, table controls, the two new search toggles, and local generated bundle load. Remove `#text-editor-textarea` and `#text-editor-highlight-layer`.
- `frontend/css/modals.css`: CodeMirror, source preview, table, drawer, focus, and large-file states; remove the highlight-layer rules.
- `frontend/package.json`, `frontend/package-lock.json`, and `frontend/package.json.md5`: exact dependencies, reproducible scripts, `npm run build` chained to include the bundle build, and Wails install-sentinel update.
- `.gitattributes`: mark `frontend/dist/*.js` as `-text` so committed artifacts survive a Windows checkout intact.
- `frontend/dist/text-editor.bundle.js` and `frontend/dist/text-validator.worker.js`: committed generated artifacts.
- `internal/app/app.go`: generalized invalid-UTF-8 bridge protection and any additional clip-data metadata needed by `TextCodec`.
- `internal/app/api_manager.go`: explicit same-origin worker CSP for served web mode.
- `frontend/js/utils.js`: use the shared classification result where presentation depends on a recognized file type; keep existing badge semantics unless explicitly format-specific.
- `frontend/js/editor/markdown-preview.js` and `frontend/js/markdown-renderer.js`: retain specialized behavior behind the generalized preview dispatcher.
- E2E selectors and fixtures: replace direct textarea assumptions with editor helper operations while continuing to test user-visible behavior.
- `docs/docs/features/text-editor.md`: supported types, previews, diagnostics, formatting, limits, fidelity, shortcuts, and invalid-UTF-8 behavior.

No new Wails runtime import is permitted outside `internal/wailsbridge`. The design does not require a new Wails service or runtime event channel.

## Delivery sequence

The feature ships as one coherent user-facing release but is implemented in test-first milestones:

1. Spike the committed static worker in production-style Wails v2 builds on macOS, Windows, and Linux plus served web mode; build the capability probe and the time-sliced fallback executor behind the same interface; add the generated-asset and size checks and the `.gitattributes` entries. The spike determines which executor each surface uses; it no longer gates whether parser work happens.
2. Add classification and desktop/server codec tests, then the registry, the binary-MIME guard, plain-filename MIME precedence, and byte-safe text retrieval — including the server-mode text-decoding bug fix and its regression test.
3. Add the CodeMirror adapter contract and migrate the existing editor, custom find panel (including `drawSelection` and the new case/whole-word toggles), drafts, and shortcut override without changing user-visible Markdown behavior.
4. Generalize Preview/Edit entry modes and tabs, wire every `ui.js`/`modals.js`/`app.js` caller with the combined image-or-text eligibility check, and add safe source preview.
5. Add validation adapters, diagnostics state, drawer UI, the change-based save confirmation, target-filename reclassification, and JSON formatting.
6. Add bounded CSV/TSV table preview and temporary interpretation controls, all findings nonblocking.
7. Add all language modes and registry variants.
8. Update documentation and run Markdown security/regression verification plus the complete suite.

Each milestone leaves the editor usable. Save semantics change in exactly two intended ways, both landing in milestone 5 and both belonging in the release notes: invalid JSON that was already invalid when opened no longer prompts on save (see [Save confirmation policy](#save-confirmation-policy)), and saves operate on an immutable snapshot so edits made during a pending save no longer affect the bytes being written. Everything else about save — targets, encoding, failure handling, draft clearing — is preserved.

## Test strategy

Development follows red-green-refactor slices.

### Where unit-shaped tests run

Several contracts below — the classification matrix, `TextCodec` round-trips, and per-validator diagnostic shapes — are unit tests, not user-journey tests. This repository has no JavaScript unit runner: `e2e/package.json` is Playwright-only and `frontend/package.json` has no `test` script.

**These tests run inside Playwright via `page.evaluate` against the loaded app page**, in a dedicated `e2e/tests/clips/text-editor-contracts.spec.ts`. No new test framework is introduced. The tradeoff is accepted deliberately: assertions are slower and failure output is less precise than a real unit runner would give, but the project keeps a single test stack. To keep failures diagnosable, each contract table is driven as one `test()` per format with the case table inside it, so a failure names the format and the specific input rather than collapsing into one opaque assertion.

**This is not the environment that ships.** Playwright runs Chromium only (`e2e/playwright.config.ts:49-57`) against `wails dev` over an `http://localhost` origin — not WKWebView on macOS, not WebView2 on Windows, not the production custom-scheme origin. That is fine for pure-logic contracts like classification tables and codec round-trips, whose behavior does not vary by engine. It is *not* evidence for anything engine- or origin-dependent: worker construction, worker URL resolution, CSP enforcement, and clipboard or focus behavior. Those need the native smoke harness described in [Dependency packaging](#dependency-packaging), and no amount of green Playwright output substitutes for it.

The modules being tested must therefore be reachable from the page — the editor bundle's app-owned surface exposes `TextFileTypes` and `TextCodec` for this purpose, which it needs to do for the classic scripts regardless.

### Classification

- Every filename variant is case-insensitive and maps to the expected descriptor.
- Specific recognized filenames win over conflicting MIME.
- Specific MIME wins over generic `.txt`, `.text`, and `.log` filenames, including backend-sniffed pasted JSON and HTML clips named `pasted_text_<timestamp>.txt`.
- Recognized MIME handles extensionless and unknown-extension files.
- Unknown valid `text/*` becomes generic text.
- Unsupported binary MIME without a recognized filename remains ineligible.
- The binary-MIME guard holds: a `.ts` clip typed `video/mp2t` is not a text candidate and opens through the ordinary non-text path, while a `.ts` clip typed `text/plain`, `application/typescript`, or nothing at all classifies as TypeScript.
- Rename changes frontend mode without unintended backend MIME rewriting.
- Existing Markdown promotion tests remain unchanged.

### Opening and modes

- Generic open starts Markdown, CSV, and TSV in Preview.
- Generic open starts every other registered format, including plain text, JSON, YAML, and HTML, in Edit.
- All three explicit Edit entry points — card menu (`ui.js:579`), lightbox file menu (`modals.js:308`), and the lightbox `editClip` callback (`app.js:176`) — start in Edit without a post-open tab click, including for Markdown and CSV.
- Linked text references use the descriptor default.
- Recovered drafts start in Edit and are labeled, overriding a Preview-default descriptor.
- The card context menu still offers **Edit** for image clips.
- Preview reflects unsaved source.
- Independent scroll positions survive mode toggles but reset after close.
- The generalized toggle shortcut and accessible tab behavior work for every previewable text clip.
- A stored `editor.markdown_preview` override migrates to `editor.preview_toggle` without changing the user's binding.

### CodeMirror compatibility

- Editing, selection, undo/redo, wrapping, line/column status, character count, keyboard save, Save As, and dirty close remain functional.
- The existing stone-styled find panel retains query, `N of M`, next/previous, replace, replace-all, and Cmd/Ctrl+F behavior while delegating matching to `@codemirror/search`; the stock CodeMirror panel never appears.
- The new case-sensitive and whole-word toggles default off, so an untouched panel matches exactly as it does today; each toggle changes match count and `N of M` when enabled.
- The active match stays visually highlighted while keyboard focus is in the find input (the `drawSelection` requirement).
- Draft v1 records migrate once to v2 without losing recoverable source or fidelity information, including a CRLF document with a BOM.
- Drafts persist and restore through the adapter.
- Focus enters the intended panel and remains inside the editor modal.
- Existing tests stop relying on a native textarea and use helper methods that represent user behavior.

### Validation and formatting

For every authoritative format, include valid, invalid, positioned-diagnostic, and save-confirmation cases.

Additional cases cover:

- Duplicate YAML keys, document/node/alias limits, and worker timeout.
- TOML 1.0 syntax and worker timeout.
- XML well-formed DOCTYPE acceptance, inert XInclude, depth/event limits, and absence of entity expansion or external fetches.
- JSON Lines blank lines and per-line errors.
- CSV/TSV malformed quotes, ragged-row warnings, deterministic delimiter tie-breaking, deterministic header detection, override effects, parse limits, and every finding remaining nonblocking under both inferred and user-selected delimiters.
- Shell, `.env`, INI/CFG/CONF, and log clips producing highlighting and no diagnostics at all.
- Possible issues never triggering save confirmation.
- Save confirmation firing on newly introduced errors, staying silent for a document that was already invalid at open and is unchanged, firing again when the error set changes, and re-baselining after a successful save. The canonical fixture is a Helm-style `templates/deployment.yaml` full of `{{ }}`: it saves without a prompt until an edit changes its error set.
- The baseline coming from persisted source, not the recovered draft: stored `{}` plus a recovered draft of `{"x":}` must still prompt before it can overwrite valid bytes.
- Save operating on its snapshot: typing during a pending save does not change the bytes written, and the editor remains dirty against the newer value afterward.
- Diagnostic positions landing correctly for `{"😀": }`, where byte, code-point, and UTF-16 columns differ.
- XML entity policy: `<!DOCTYPE r [<!ENTITY x "ok">]><r>&x;</r>` is well-formed and unexpanded, an undeclared reference is likewise not an error, and an unclosed tag still is.
- Diagnostic navigation, recovered-draft expansion, 100-item presentation cap, 1,000-item collection cap, stale-result rejection, and drawer expansion rules.
- Timeout/resource-limit notices allowing save without claiming validity.
- JSON and JSON Lines formatting, undoability, disabled invalid formatting, and preserved text profile.
- Save As reclassifying and validating against the proposed target extension before upload.

### Preview and security

- Source HTML and scripts remain inert text.
- Registry-selected highlighting does not perform automatic language detection.
- Invalid syntax remains visible as source.
- Renderer failures fall back safely.
- CSV/TSV truncation honors row, column, and total-cell limits.
- Existing Markdown sanitization, links, references, local images, remote loading, budgets, cache, and stale-render tests continue to pass.

### Byte fidelity and surfaces

Round-trip contract tests run through both desktop `GetClipData` and server `rest-glue.js` and cover:

- Exact filename, MIME, `data_encoding`, and `valid_utf8` shape.
- UTF-8 with and without BOM.
- LF, CRLF, and CR documents.
- Presence and absence of a final newline.
- Valid extension-classified text arriving as base64.
- Invalid UTF-8 across `text/*`, application MIME, and extension-classified files.
- A Latin-1 text clip becoming explicitly read-only rather than replacement-decoded.
- Failed and cancelled saves retaining original bytes and drafts.
- Server mode showing decoded text and the real filename for a text clip — the regression test for the `rest-glue.js:116` bug — retrieved through the single `/text` endpoint.
- An exact-byte no-op round trip of `EF BB BF 61 0D 0A`, asserted on bytes rather than decoded text.
- A value containing an unpaired surrogate being refused at save rather than encoded as `EF BF BD`.
- A `config.yaml` typed `application/octet-stream` opening normally when its bytes are valid UTF-8, and getting the byte-safety screen when they are not.
- Save As from `notes.txt` to `copy.json` validating as JSON, and the backend's existing `text/plain` → `application/json` promotion (`internal/app/app.go:947-957`) being asserted as current behavior rather than assumed absent.
- Text clips above the 16 MiB editable cap declining to open with the download explanation.

### Worker, limits, and accessibility

- A static worker loads and responds in production-style macOS, Windows, and Linux Wails builds and in served web mode under the existing `default-src 'self'` policy.
- The capability probe falls back cleanly: with the worker forced to fail, diagnostics, formatting, and save confirmation still work through the time-sliced executor, at its lower 256 KiB ceiling, without freezing the UI.
- Worker timeout, termination, restart, generation rejection, and resource-limit behavior are exercised, and the same generation-rejection assertions run against the fallback executor.
- `npm run check:text-editor-bundle` detects stale committed output and a combined size over budget.
- `npm run build` regenerates the bundles, so a Wails build cannot ship stale artifacts.
- Documents above 2 MiB remain plainly previewable/editable while enhanced assistance is disabled.
- Parser-specific limits prevent excessive work below 2 MiB.
- CodeMirror, tabs, table controls, drawer rows, and save confirmation are keyboard operable and named.
- Diagnostic navigation focuses the exact source location.
- Status announcements are useful without becoming keystroke-noisy.

### Verification commands

Run focused editor suites during development, then the complete required suite:

The bundle check lives in `frontend/package.json`, next to the esbuild dependency it drives; the Playwright suites live in `e2e/`. They are separate working directories and must not be run from one `cd`:

```bash
# Generated artifacts (from the repository root)
npm --prefix frontend run check:text-editor-bundle

# Focused editor suites, then the complete required suite
cd e2e
npx playwright test tests/clips/text-editor.spec.ts
npx playwright test tests/clips/markdown.spec.ts
npx playwright test tests/clips/structured-text-editor.spec.ts
npx playwright test tests/clips/text-editor-contracts.spec.ts
npm test 2>&1 | tail -50

# Server mode is a SEPARATE suite with its own config — `npm test` does not include it
npm run test:server 2>&1 | tail -30
```

`e2e/package.json:6-10` defines `test` as the desktop suite plus the share suite; `test:server` runs `playwright.server.config.ts` independently. The server codec contract, the served-mode CSP, and worker loading in served mode are only covered by that second command, so a claim that "the complete suite passes" means both were run.

The native worker smoke check is a third thing again: a production `wails build` launched on each supported desktop OS, exercising worker construction, one round-trip, a forced timeout, termination, and restart. It is manual for macOS/Windows/Linux at this stage and is recorded per platform in the milestone 1 notes.

Run Go tests for changed backend data retrieval, CSP, and classification behavior as part of the relevant milestone. Run the worker smoke test on each supported desktop platform before parser implementation proceeds.

## Acceptance criteria

The feature is complete when:

1. Markdown, CSV, and TSV open in Preview by default; every other registered valid UTF-8 format, including plain text, opens in Edit by default; all three explicit Edit entry points open in Edit; and every registered format can reach both modes.
2. Specific MIME types retain their behavior when paired with generic `.txt`, `.text`, or `.log` filenames; unlisted valid `text/*` clips receive generic Preview/Edit behavior.
3. Desktop and served web mode expose the same filename, encoding, and UTF-8-validity contract to `TextCodec`.
4. CodeMirror provides the agreed lightweight developer experience without IDE features; the app's existing find/replace UI and behavior remain intact, with case-sensitive and whole-word toggles added and defaulted off.
5. Invalid authoritative formats show navigable diagnostics while source remains visible.
6. Saving requires explicit Save Anyway only when authoritative errors are new or changed relative to the document as opened; unchanged pre-existing errors and possible issues never prompt.
7. All CSV/TSV findings remain nonblocking.
8. JSON and JSON Lines formatting follows the agreed explicit, undoable contract.
9. CSV and TSV table previews detect or apply delimiters, support temporary header controls, and honor all render limits.
10. HTML and all other source previews remain inert and cannot execute or load content; well-formed XML DOCTYPE remains inert and does not block saving.
11. Invalid UTF-8 cannot be edited and crosses desktop and server bridges without byte replacement, and the byte-safety screen names the conversion follow-up.
12. Server mode shows decoded text and the real filename for text clips, fixing today's base64/empty-filename behavior.
13. Ordinary saves preserve UTF-8 BOM choice, newline style, and final-newline state, verified byte-for-byte; unpaired surrogates are refused rather than replaced.
14. Save operates on an immutable snapshot, so a validation result can never be applied to bytes other than the ones it validated.
15. Save As validates against the proposed target descriptor before upload; the frontend adds no new MIME rewriting, and the backend's existing upload sniffing is documented rather than assumed absent.
16. Diagnostic positions are 1-based lines and UTF-16 columns, correct for astral characters.
17. Draft migration — including a BOM'd CRLF v1 draft — save flows, shortcut migration, search, wrapping, dirty close, scroll behavior, and Markdown features remain intact.
18. Every supported surface runs validation either in the static worker or in the bounded fallback executor, selected by capability probe, with no unbounded synchronous parsing on the UI thread.
19. Large documents degrade to plain editing/preview without parsing or highlighting, and text above the 16 MiB editable cap declines to open at all.
20. Diagnostics and all editor controls meet the keyboard and screen-reader contract.
21. Image clips retain their existing Edit entry points in the card and lightbox menus.
22. Every registry mode and authoritative parser has automated coverage.
23. Committed bundles are regenerated by `npm run build`, byte-match a fresh build, and stay within the size budget.
24. Focused tests, Go tests, the server suite, the generated-bundle check, native worker/fallback smoke tests, and the complete E2E suite pass.
