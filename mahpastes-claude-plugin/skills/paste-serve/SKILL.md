---
name: paste-serve
description: "Start or stop tag-serve HTTP hosting for mahpastes tags"
user-invocable: true
allowed-tools:
  - Bash
argument-hint: "<start|stop|status> [tag-name]"
---

# /paste-serve — Tag HTTP Hosting

Start, stop, or check the status of tag-serve HTTP servers that host clips as static files.

## Prerequisites

Requires `MAHPASTES_API_URL` and `MAHPASTES_API_KEY` environment variables. If missing, tell the user to run `/paste-setup` first.

Starting and stopping servers requires an **admin** API key.

## Commands

### `status`

List all running tag servers.

```bash
curl -sf -H "Authorization: Bearer $KEY" "$URL/api/v1/serve"
```

Display as a table:

```
Tag     | Port  | URL                        | Requests
--------|-------|----------------------------|----------
docs    | 9100  | http://127.0.0.1:9100      | 42
assets  | 9101  | http://127.0.0.1:9101      | 7
```

If no servers are running, report that.

### `start [tag-name]`

Start serving a tag's clips as a static HTTP site.

1. If no tag name provided, ask the user.
2. Resolve the tag name to a tag ID:

```bash
TAG_ID=$(curl -sf -H "Authorization: Bearer $KEY" "$URL/api/v1/tags" \
  | python3 -c "import json,sys; tags=json.load(sys.stdin); print(next((t['id'] for t in tags if t['name']=='$TAG_NAME'),''))")
```

3. Start serving:

```bash
curl -sf -X POST \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d "{\"tag_id\":$TAG_ID,\"port\":0}" \
  "$URL/api/v1/serve"
```

4. Report the URL from the response.

**Handle 409 Conflict:** If the tag is already being served, fetch the existing server info from `GET /api/v1/serve` and report the existing URL.

### `stop [tag-name]`

Stop serving a tag.

1. If no tag name provided, list running servers and ask which to stop.
2. Resolve tag name to tag ID.
3. Stop:

```bash
curl -sf -X DELETE \
  -H "Authorization: Bearer $KEY" \
  "$URL/api/v1/serve/$TAG_ID"
```

4. Report success.

## Examples

```
/paste-serve status
/paste-serve start docs
/paste-serve stop docs
```

## Edge Cases

| Condition | Behavior |
|-----------|----------|
| Tag doesn't exist | Report error, list available tags |
| Already serving | Report existing URL (don't error) |
| Not serving (on stop) | Report "not currently served" |
| Port conflict | Auto-port (0) avoids this; report if specific port fails |
| Non-admin key | Report 403, suggest using an admin key |
