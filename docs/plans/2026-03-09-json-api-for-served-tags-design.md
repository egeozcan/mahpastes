# JSON API for Served Tags

**Date**: 2026-03-09
**Status**: Approved

## Summary

Extend the tag serve system so that HTML clips served from a tag can read and write JSON clips under the same tag via REST semantics. The URL prefix `_api` is reserved at the tag creation level to prevent collisions. Authentication uses an HTTP-only same-site cookie set automatically when serving HTML.

## Core Behavior

### URL Routing

Requests to `/_api/{clipStem}/{jsonPath...}` on a tag server are handled by a JSON API handler. The first segment after `_api` maps to a clip named `{clipStem}.json` under the served tag. Remaining segments navigate into the JSON structure — object keys by name, array elements by `id` field (auto-increment integer).

```
GET  /_api/users          → return full contents of users.json clip
GET  /_api/users/3        → find element with id=3 in the array
POST /_api/users          → append to array, auto-assign id
PUT  /_api/users/3        → replace element with id=3
PATCH /_api/users/3       → merge-patch element with id=3
DELETE /_api/users/3      → remove element with id=3
PUT  /_api/config/theme   → set nested path in config.json
```

### HTTP Verbs

| Method | Behavior | Response Status |
|--------|----------|-----------------|
| GET | Read value at path | 200 |
| POST | Append to array, assign `id` = max+1 | 201 |
| PUT | Replace value at path (upsert) | 200 |
| PATCH | JSON Merge Patch (RFC 7396) at path | 200 |
| DELETE | Remove key or array element | 204 |

### Path Resolution

- `/_api/{stem}` — maps to clip named `{stem}.json`
- `/_api/{stem}/{key}` — navigate into object by key
- `/_api/{stem}/{id}` — for arrays, find element where `id` field matches (integer)
- `/_api/{stem}/{key1}/{key2}/...` — deep navigation into nested structures
- PUT to a nonexistent key creates it (upsert semantics)

### Cookie Authentication

When the tag server serves any HTML response, it sets:

```
Set-Cookie: _mp_serve_key={random-token}; HttpOnly; SameSite=Strict; Path=/
```

- Token is a cryptographic random string, generated once per `StartServing` call
- Stored in the `tagServer` struct
- `/_api` handler validates the cookie before processing
- Missing or wrong token returns 401 Unauthorized

### Permission Model

`StartServing` gains a new parameter: `apiAccess string`

| Value | Behavior |
|-------|----------|
| `"none"` | `/_api` prefix returns 404 (default) |
| `"read"` | Only GET allowed; POST/PUT/PATCH/DELETE return 403 |
| `"readwrite"` | Full CRUD |

Stored in `tagServer` struct, checked in the `_api` handler.

### Concurrency

Per-clip `sync.Mutex` stored in a map on `tagServer`. All writes to the same JSON clip are serialized. No ETags or optimistic locking in v1.

### Auto-Increment IDs

When POST targets an array path:
1. Scan array for max `id` field value
2. Assign `max + 1` to new entry
3. Return the created object (with assigned id) in the response body

### Response Format

- All `/_api` responses use `Content-Type: application/json`
- GET: returns the value at the path
- POST: returns the created object (with assigned id), status 201
- PUT/PATCH: returns the updated value, status 200
- DELETE: status 204 (no content)
- Errors: `{"error": "message"}` with appropriate status code (400, 401, 403, 404, 500)

### Tag Name Reservation

`CreateTag` rejects any tag where a path segment equals `_api`. Examples of rejected names:
- `_api`
- `work/_api`
- `docs/_api/foo`

This prevents ambiguity between subtag folder navigation and the JSON API prefix.

## Files Changed

### Go Backend

| File | Changes |
|------|---------|
| `serve_manager.go` | Add `apiAccess` field and `serveKey` to `tagServer` struct. Cookie generation on HTML responses. New `/_api` route handler with JSON path navigation, CRUD operations. Per-clip mutex map. |
| `serve_service.go` | Update `StartServing` signature to accept `apiAccess string` parameter. |
| `app.go` | Add `_api` segment check in `CreateTag` — reject tag names containing `_api` as a path segment. |

### Frontend

| File | Changes |
|------|---------|
| `serve.js` | Add API access dropdown (None / Read-only / Read-Write) to tag serve cards. Pass selection to `StartServing`. Only configurable when server is stopped. |
| `wails-api.js` | Update `StartServing` binding call to include `apiAccess` parameter. |

### Documentation (docs/docs/)

| File | Changes |
|------|---------|
| `features/tag-serve.md` | New "JSON API" section covering: enabling API access, cookie auth, URL structure, HTTP verbs, path navigation, examples, `_api` reservation note. |
| `features/rest-api.md` | Update POST /serve body to include `api_access` field. Cross-reference to tag-serve JSON API docs. |
| `developers/architecture.md` | Update serve_manager.go description to mention JSON API handler. |

### CLAUDE.md

New section **"Tag Serve JSON API"** (after "Tag Hierarchy & Folder Mode"):
- `_api` prefix routing and path resolution rules
- Cookie auth mechanism
- Permission model (`none` / `read` / `readwrite`)
- Reserved tag name constraint
- Per-clip mutex for concurrency
- CRUD operations and response format

Update `StartServing` signature references to include `apiAccess` parameter.

### Skill & Commands

| File | Changes |
|------|---------|
| `skills/mahpastes/SKILL.md` | Update description triggers to include JSON API. Update "Start Tag-Serve" curl to include `api_access`. Add workflow pattern "4. Interactive Web App with JSON API". Add "JSON API" section under Core Operations. |
| `skills/mahpastes/references/api-reference.md` | Add `api_access` field to "Start Serving" endpoint body docs. Note that it controls JSON API on the tag server, not the REST API. |
| `commands/serve.md` | Add optional `$3` argument for API access mode. Include `api_access` in Start Serving curl body. Add note explaining JSON API when enabled. |

### E2E Tests

New file `e2e/tests/serve/json-api.spec.ts`:
- Start serving a tag with `readwrite` access
- GET full JSON clip via `/_api/{stem}`
- GET nested path and array element by id
- POST to array — verify auto-increment id
- PUT to replace and upsert
- PATCH with merge semantics
- DELETE key and array element
- Cookie auth enforcement (no cookie = 401)
- Permission `none` returns 404 for `/_api`
- Permission `read` blocks POST/PUT/PATCH/DELETE with 403
- Nested path navigation (e.g., `/_api/config/theme/colors`)
- `_api` tag name reservation (creating tag with `_api` segment fails)

## What Doesn't Change

- REST API system (`api_manager.go`) — completely separate
- Plugin system — unaffected
- Existing file serving — `_api` prefix is the only new route
- Directory listing — `_api` does not appear as a directory entry
- Existing `StartServing` callers with only 3 args continue to work (default `apiAccess = "none"`)

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| JSON backing store | Existing JSON clips | Keeps everything in the data model, no new storage mechanism |
| Route disambiguation | `/_api` prefix | Clear, unambiguous, conventional |
| PATCH semantics | JSON Merge Patch (RFC 7396) | Covers 95% of cases, trivial to implement, easy to use from fetch |
| Concurrency control | Per-clip mutex only | Single-user browser context makes ETags unnecessary for v1 |
| ID assignment | Auto-increment integer (max id + 1) | Predictable, stable references, maps cleanly to URL paths |
| Auth mechanism | HTTP-only same-site cookie | Automatic for served HTML, no JS access to token, prevents CSRF |
| Permission model | Configurable per-tag at serve time | Flexibility without complexity — user decides on start |
| Reserved prefix | Block at tag creation | Prevents ambiguity permanently, not just at serve time |
