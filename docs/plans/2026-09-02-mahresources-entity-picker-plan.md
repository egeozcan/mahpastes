# mahresources plugin: entity picker, auth, working settings

## Context

`plugins/mahresources.lua` uploads clips to a mahresources instance. Today its parent
group must be typed as a raw numeric ID (`owner_id` text setting), it cannot talk to an
auth-enabled instance at all, and its `content_filter` setting is silently dropped by the
manifest parser. The user wants:

1. A searchable **entity picker** for the parent group, in the plugin settings (default)
   *and* in the manual upload dialog (per-upload override).
2. To be able to **upload as a user** — i.e. support auth-enabled mahresources instances.
3. Auto-upload toggling that demonstrably works.
4. Manual upload from the clip context menu (already exists — verify and extend).

Facts established during exploration that shape the design:

- In mahresources a resource's owner **is a Group**, not a user: the upload field
  `ownerId` takes a group id (`models/resource_model.go:42`). Groups are searchable via
  `GET /v1/groups?Name=<substring>&page=N` → JSON array of `{ID, Name, …}`, fixed 50/page
  (`server/api_handlers/group_api_handlers.go:19`, `constants.MaxResultsPerPage`).
- `createdByUserId` is stamped from the authenticated principal; there is **no `userId`
  upload field**. So "upload as a user" = authenticate as that user. mahresources accepts
  `Authorization: Bearer <token>` (minted at `POST /v1/account/tokens`), and bearer auth is
  **exempt from CSRF** (`server/csrf.go:72`) — the reason to prefer it over session login.
  The user confirmed a bearer token is enough and that listing users is not wanted.
- mahpastes has **no dynamic/searchable field type** anywhere: `SettingField.Options` and
  `FormField.Choices` are parsed statically out of the manifest text by a regex parser
  (`plugin/manifest.go`), and there is no Lua→frontend request/response channel. The user
  chose to add one as a reusable core field type.

## Part 1 — Core: a `search` field type for plugins

A new field type usable in **both** `settings` entries and UI-action `options`. The field
declares a `source` name; the plugin implements one `on_search(source, query)` hook that
returns rows.

```lua
settings = {
    {key = "owner_id", type = "search", source = "groups", label = "Parent group"},
}
ui = { card_actions = { { id = "upload", options = {
    {id = "owner_id", type = "search", source = "groups", label = "Parent group"},
} } } }

function on_search(source, query)   -- returns {{value = "12", label = "Trips"}, ...}
```

### Go

- **`plugin/manifest.go`** — add `Source string \`json:"source,omitempty"\`` to both
  `SettingField` (:58) and `FormField` (:85). Accept `"search"` in the settings type
  whitelist (`parseSettingEntry`/`extractSettings` ~:456-502) and in
  `extractFormFields` (~:613-663); in both, drop the field when `source` is empty (mirrors
  how a `select` with no choices is dropped today). `source` is read with
  `extractStringField(entry, "source")`, but that helper needs a fix first: its pattern is
  unanchored (:244-256), so asking for `source` also matches `data_source = "…"` or
  `resource = "…"`, and a search field carrying no real `source` would sail through
  validation. Anchor the field name on an identifier boundary and add a collision test —
  a pre-existing sharp edge that this new field would be the first to step on.
  `PluginService.GetPluginUIActions` passes `btn.Options` through verbatim
  (`plugin_service.go:414`) and `info.Settings` likewise (:283), so the new field reaches
  the frontend with no further plumbing.
- **`plugin/sandbox.go`** — add `CallSearch(source, query string, timeout time.Duration)
  ([]Choice, error)`, modelled on `CallUIAction` (:238) but with one deliberate difference:
  it acquires the sandbox lock with **`s.mu.TryLock()`** and returns a typed
  `ErrPluginBusy` if the sandbox is occupied, rather than blocking. `CallUIAction` locks
  *before* creating its deadline (:239-252) and an async action may hold that lock for
  `MaxUIActionTime` (`manager.go:863`), so a blocking search would wait far outside its own
  timeout — the picker must never queue behind an upload. Then: context timeout,
  `PCall(2, 1, nil)` on the global `on_search`, walk the returned Lua array converting each
  `{value=…, label=…}` table (numbers coerced to strings, `label` falling back to `value`),
  cap at 50 rows. Missing/non-function `on_search` → error.
- **`plugin/api_http.go`** — give the search path, and *only* the search path, a real
  network deadline. Today the Lua deadline cannot interrupt an in-flight request: requests
  are built with `http.NewRequest` (:206) and the client timeout is `HTTPTimeout = 5 min`
  (:18), so a `search` hook against a black-holed server would hold the sandbox for five
  minutes whatever `MaxSearchTime` says.
  **Do not simply wire `L.Context()` into every plugin request.** Every sandbox entry point
  sets a context before running Lua (`sandbox.go:82,117,160,260`), and for event handlers
  and scheduled tasks that deadline is `MaxExecutionTime = 30 s` (:13). Blocking HTTP
  currently survives past it, because a gopher-lua deadline only fires at VM instruction
  boundaries — so making all requests context-aware would newly abort any auto-upload
  (`on_clip_created`) of a clip that takes more than 30 seconds to POST. That is a
  regression, not a fix.
  Instead: `HTTPAPI` takes an optional `deadline func() (time.Duration, bool)` supplied by
  the sandbox. `CallSearch` sets it for the duration of the call; every other entry point
  leaves it unset and behavior is byte-for-byte unchanged. When set, the request is built
  with `http.NewRequestWithContext` on that budget.
  Plumbing: `httpAPI` is currently a local in `loadPlugin` (`manager.go:253`). Construct it
  before the sandbox needs it and hand the sandbox a reference (`sandbox.SetHTTPDeadline`
  or a small shared holder), so `CallSearch` can set and clear the budget around its call.
- **`plugin/manager.go`** — `SearchOptions(pluginID int64, source, query string)
  ([]Choice, error)` next to `ExecuteUIAction` (:840): same plugin-exists / enabled /
  sandbox-initialised guards, then `CallSearch` with a new `MaxSearchTime` constant
  (~15 s — the hook makes an HTTP call).
  **Validate the source against the manifest**, the way `ExecuteUIAction` validates an
  action id via `findUIAction` (:858-862): collect the `source` values declared by the
  plugin's settings and by every UI action's options, and reject anything else. Without
  that, `POST /api/v1/plugins/{id}/search {"source": "anything"}` invokes `on_search` with
  an attacker-chosen source, reaching branches the UI never offers. Cap the query length
  (256 chars) in this same shared path so both the desktop and REST callers inherit it.
- **`plugin_service.go`** — `SearchPluginOptions(pluginID int64, source, query string)
  ([]plugin.Choice, error)` delegating to the manager.
- **`internal/app/api_manager.go`** — register
  `POST /api/v1/plugins/{id}/search` (role `editor`, alongside the action route at :365)
  with body `{source, query}` for server-mode parity.
- Run `make bindings` after the Go changes.

Note for the docs: a picker query is best-effort. While the plugin's sandbox is busy
running an action, `on_search` returns "busy" rather than queueing, and the dropdown says
so; the next keystroke retries.

### Frontend

- **New `frontend/js/plugin-search-field.js`** — a reusable async combobox. Structure it on
  `frontend/js/tag-autocomplete.js` (dropdown injected as a sibling, `mousedown`
  `preventDefault` so the click beats blur, ArrowUp/Down/Enter/Esc, outside-click close,
  `openToken` race guard), but with the ranked local filter replaced by a 250 ms debounced
  call to `SearchPluginOptions`, plus loading / no-results / busy / error rows, and full
  combobox ARIA (`role="combobox"`, `aria-expanded`, `aria-controls`,
  `aria-activedescendant`) which the tag version lacks. Kept as a **separate module**, not
  a refactor of `TagAutocomplete`: that one is a local ranked filter with create-new
  semantics used by the import wizard and tag inputs, and merging the two would put a
  regression in those paths to save ~150 lines of well-understood dropdown code.
  Register the script in `frontend/index.html` next to the other plugin JS.
  - The visible input shows the **label**; the selected **value** lives in a hidden input
    beside it. **Any keystroke in the visible input clears the hidden value**, so a form can
    only ever submit a value that came from an actual selection — the options dialog
    collects with `new FormData(e.target)` (`plugins.js:966-981`), which would otherwise
    happily submit a stale id under an edited label.
- **`frontend/js/plugins.js`**
  - `renderSettingField` (:256) — a `case 'search'` rendering the label input +
    hidden value input, reusing the existing form-input classes, with
    `data-setting-type="search"`, `data-setting-key`, `data-setting-source`.
  - `setupSettingListeners` (:446) — attach the combobox to each search field. On select,
    `saveSetting(key, value)` writes the id into plugin storage as any other setting does.
    The **label is UI-only state and must not go into plugin storage**: a `<key>__label`
    twin would collide with the plugin's own freely-writable keyspace
    (`plugin/api_storage.go:36-84`) and could drift out of sync with the value. Instead
    persist it exactly the way option memory already persists its map
    (`plugins.js:773-826`): an app setting `plugin_setting_labels`, a JSON object keyed
    `<plugin_name>::<key>`, read through `App.GetSetting`/`SetSetting`.
    Store **`{value, label}` pairs, and display the label only when its `value` still equals
    what plugin storage holds** — otherwise fall back to showing the raw id. Plugin storage
    is independently writable from Lua (`plugin/api_storage.go:36-84`), so a remembered
    label alone would let the panel read "Trips" over an `owner_id` the plugin has since
    rewritten to something else. Same rule for the remembered option value in the action
    dialog; note that `rememberedOptionValue` (:796) validates a stale `select` against the
    manifest choices, which a `search` field has none of — the pairing check is what stands
    in for that.
    Clearing must be explicit and must persist: the field gets a small × that writes `""`
    to the setting. Clearing only the hidden input would leave auto-upload still using the
    previously stored owner.
  - `openPluginOptionsDialog` (:835) — same `case 'search'` in the option form; the submit
    collector (:961) reads the hidden input's value as a string.
  - Option memory (:768-830) — treat `search` like `select`/`checkbox`/`range`, persisting
    `{value, label}` so the upload dialog reopens on the last group used.
- **`frontend/js/rest-glue.js`** — add `SearchPluginOptions` mapped to the new REST route,
  next to the existing plugin storage shims (:298).

Styling: stone palette, `text-xs`/`text-[11px]`, existing input and dropdown classes copied
from `tag-autocomplete.js` — no new colors.

### Docs

- `docs/docs/plugins/writing-plugins/plugin-manifest.md` — document the `search` type, the
  `source` field and the `on_search` hook, for both settings and option forms.
- Same file (:288) and `settings-storage.md` (:150) currently tell plugin authors to read
  settings as `storage.get("setting:api_key")`. The code stores the **raw key**
  (`plugins.js:497` → `SetPluginStorage(pluginId, key, …)`); fix the docs while there.

## Part 2 — `plugins/mahresources.lua`

- **Manifest**
  - `network`: `["localhost"] = {"GET", "POST"}` — the picker needs GET. (The allowlist is
    host-only, so remote instances still require editing this block; keep the comment.)
  - Settings: `server_url` (text), **`api_token` (password)** — "Bearer token from
    Account → API tokens; leave empty for an instance with auth disabled",
    `owner_id` → `type = "search", source = "groups"`, `auto_upload` (checkbox),
    `content_filter` — **fix the existing bug**: it declares `choices = {{value=…}}`, but
    settings selects only parse a flat `options = {"…"}` array (`manifest.go` +
    `plugins.js:322`), so the field is dropped today and never renders. Rewrite as
    `options = {"all", "images", "text"}` and map those strings in `matches_filter`.
  - `card_actions.upload` gains `options = {{id = "owner_id", type = "search",
    source = "groups", label = "Parent group"}}` so a manual upload can override the
    default; empty means "use the settings default".
- **`on_search(source, query)`** — for `source == "groups"`, GET
  `http://<server_url>/v1/groups?Name=<utils.url_encode(query)>&page=1` with the auth
  header, `json.decode` the array, return `{value = tostring(g.ID), label = g.Name}`.
  Unknown source → empty list.
- **Auth** — one `auth_headers()` helper adding `Authorization: Bearer <token>` when
  `api_token` is set; used by both `on_search` and `upload_clip`. No CSRF handling needed
  (bearer requests are exempt — `mahresources/server/csrf.go:26-37`). Also send
  `Accept: application/json` on upload so the handler returns JSON instead of a 303
  redirect (`RedirectIfHTMLAccepted`).
- **Don't leak the token over plaintext.** The plugin hardcodes `"http://" .. server_url`
  (`plugins/mahresources.lua:115`) and mahpastes only forces https on *redirects*, not on the initial
  request (`plugin/api_http.go:47-64`). Change `server_url` to a full base URL including
  scheme (default `http://localhost:8181`), and refuse to attach the bearer header when the
  scheme is `http` and the host is not loopback — toast "refusing to send your API token
  over plain HTTP" instead. Document it in the setting's description.
  Parse the URL properly rather than pattern-matching the string: `http://localhost@evil.example`
  has hostname `evil.example`, and `localhost.` and `[::1]` must be handled. Treat exactly
  `localhost`, `127.0.0.0/8` and `::1` as loopback. Note also that the manifest allowlist
  matches on `Hostname()` alone (`plugin/api_http.go:109-117`) and currently lists only
  `localhost`, so `127.0.0.1` is rejected outright — add `["127.0.0.1"]` to the plugin's
  `network` block, and have the e2e fake server be addressed as `localhost`.
- **Keep the token out of backups.** `internal/app/backup.go` filters sensitive rows out of
  the `settings` table (:384-394) but exports `plugin_storage` with a nil filter (:418-423),
  so the new `api_token` would land in plaintext in every backup ZIP — as fal-ai's `api_key`
  already does today. Fix it generically: skip `plugin_storage` rows whose key is declared
  `type = "password"` in that plugin's manifest, and report them in the existing `excluded`
  list so the restore UI can say what was dropped.
- **Sanitize the multipart filename.** `build_multipart` interpolates the clip filename
  straight into a `Content-Disposition` header (`plugins/mahresources.lua:49`); a clip named
  with a `"` or a CRLF can inject headers or break the body. Strip CR/LF and escape quotes.
- **`upload_clip(clip_id, silent, owner_override)`** — owner precedence: per-upload option
  → `owner_id` setting → omit the field. Map auth failures accurately rather than lumping
  them together: **401** = missing or invalid token; **403** = the token is valid but the
  role cannot write, or the chosen owner group is outside that user's scope (a scoped user
  cannot upload ownerless or outside its subtree —
  `mahresources/application_context/scoping.go:462-469`). Parse the error body, which is
  `{"error", "details":[…]}` (`resource_api_handlers.go:294-301`), and surface `error`.
  Success is a JSON **array** of resources for file uploads (:328), not a single object.
- **`on_ui_action`** — pass `options.owner_id` through to `upload_clip`; auto-upload
  (`on_clip_created`) keeps using the setting. Auto-upload already reads
  `storage.get("auto_upload") == "true"`, which matches what the checkbox writes; the work
  here is proving it (below), not rewriting it.

## Tests

- **`plugin/manifest_test.go`** — parsing a `search` settings field and a `search` option
  field keeps `Source`; a `search` field with no `source` is dropped.
- **`plugin/sandbox_test.go`** — `CallSearch` against a fixture returning: well-formed rows;
  malformed rows (missing `label`, non-table entries, a non-table return) → no panic;
  more than 50 rows → capped; a sandbox already locked → `ErrPluginBusy` rather than a
  block; an undeclared `source` → rejected before Lua runs. Plus a deadline test against a
  hung `httptest` server: a search gives up at `MaxSearchTime`, while an event handler's
  HTTP call over the same API is left on the old five-minute budget.
- **REST parity test** — `POST /api/v1/plugins/{id}/search` rejects an unknown/undeclared
  `source`, enforces the `editor` role, and caps the query length.
- **`internal/app/backup_test.go`** — a `password`-typed plugin setting does not appear in
  the exported SQL and is named in `excluded`.
- **`e2e/test-plugins/search-field-test.lua`** — new fixture with one search setting, one
  action with a search option, and an `on_search` returning static rows filtered by query.
- **`e2e/tests/plugins/search-field.spec.ts`** — picker renders in settings; typing queries
  the hook and lists rows; selecting writes **only** the id to plugin storage (assert via
  `app.getPluginStorage` — no `__label` twin) while the label round-trips through the
  `plugin_setting_labels` app setting; the label survives closing and reopening the plugins
  modal; a value the plugin rewrites behind the panel's back displays as the raw id rather
  than the stale label; the × clears the stored value; the action dialog's picker delivers
  the chosen value into `on_ui_action` options; a search while the sandbox is busy reports
  busy instead of hanging.
- **`e2e/tests/plugins/mahresources.spec.ts`** — extend, backed by a **fake mahresources
  server** stood up in the spec with `http.createServer` (the pattern
  `plugin-url-install.spec.ts:21` already uses; the plugin's `localhost` allowlist covers
  it). Assert: `GET /v1/groups` receives the `Name=` query URL-encoded and the
  `Authorization: Bearer` header when a token is set and *no* header when it isn't; the
  upload posts multipart with `resource` + `ownerId` and `Accept: application/json`; owner
  precedence (dialog option beats setting beats omitted); a 401 and a 403 produce different
  messages. Plus, without the server: the `content_filter` select now renders with its three
  options (regression for the silently-dropped field), and toggling the `auto_upload`
  checkbox writes `'true'`/`'false'` to storage and back.
- **`e2e/helpers/selectors.ts`** — add search-field selectors beside `pluginSettings` (:512)
  and `pluginOptions` (:523).

## Verification

1. `go build ./... && go test ./plugin/... ./internal/...`
2. `make bindings` (Go signatures changed), then `make dev`.
3. `cd e2e && npx playwright test tests/plugins/ 2>&1 | tail -40`, then the full
   `npm test 2>&1 | tail -50` (wrap long runs in `caffeinate -dims`).
4. Manual, against a real mahresources at `localhost:8181`: open the plugin settings, type
   in the Parent group field and confirm live results; pick one and confirm the name sticks
   after reopening the modal; toggle auto-upload on, paste a clip, confirm the resource
   lands under that group; right-click a clip → Upload to mahresources, override the group
   in the dialog, confirm it lands in the overridden group.
5. Against an auth-enabled instance (`-auth`): with no token, expect a clear 401 message;
   with a token from Account → API tokens, expect the upload to succeed and the resource's
   `createdByUserId` to be that account.
