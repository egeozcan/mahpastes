# mahresources entity picker — execution checklist

Branch: `feat/mahresources-entity-picker`

## Part 1 — Core: `search` field type
- [ ] plugin/manifest.go: fix extractStringField anchoring + collision test; `Source` on SettingField/FormField; accept `search` (drop when source empty)
- [ ] plugin/sandbox.go: `CallSearch` (TryLock → ErrPluginBusy, choice conversion, 50 cap), HTTP budget holder
- [ ] plugin/api_http.go: optional deadline func → NewRequestWithContext (search only)
- [ ] plugin/manager.go: construct httpAPI+budget in loadPlugin; `MaxSearchTime`; `SearchOptions` with source validation + 256 query cap
- [ ] plugin_service.go: `SearchPluginOptions`
- [ ] internal/app/api_manager.go: `POST /api/v1/plugins/{id}/search` (editor)
- [ ] Unit tests: manifest_test.go, sandbox_test.go (new), REST parity test
- [ ] `make bindings`

## Part 1 — Frontend
- [ ] frontend/js/plugin-search-field.js (new combobox module)
- [ ] frontend/js/plugins.js: settings `search` case, listeners, label persistence (`plugin_setting_labels`), × clear, options dialog case, option memory {value,label}
- [ ] frontend/index.html: script tag
- [ ] frontend/js/rest-glue.js: SearchPluginOptions shim

## Reviewer checkpoint #1 (gpt-5.6-sol)

## Part 2 — mahresources plugin + backup
- [ ] plugins/mahresources.lua: manifest (network GET+POST localhost/127.0.0.1, api_token password, owner_id search, content_filter fix, upload options), on_search, auth_headers + plaintext guard, URL parse/loopback, sanitize multipart filename, owner precedence, 401/403 mapping, error body parsing, Accept header
- [ ] internal/app/backup.go: exclude password-typed plugin_storage rows + excluded list
- [ ] backup_test.go
- [ ] Docs: plugin-manifest.md (search type + on_search; fix `setting:` prefix bug), settings-storage.md (fix prefix)

## Tests
- [ ] e2e/test-plugins/search-field-test.lua
- [ ] e2e/tests/plugins/search-field.spec.ts
- [ ] e2e/tests/plugins/mahresources.spec.ts extensions (fake server)
- [ ] e2e/helpers/selectors.ts additions

## Verification
- [ ] go build ./... && go test ./plugin/... ./internal/...
- [ ] make bindings; e2e plugins suite; full e2e (tail -50)
- [ ] Review loop with gpt-5.6-sol until no majors
