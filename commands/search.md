---
description: Search and retrieve clips from mahpastes
argument-hint: "<query>"
allowed-tools: Bash, Read, Write
---

Search for clips in mahpastes and optionally retrieve their content.

## Prerequisites

Verify `MAHPASTES_API_URL` and `MAHPASTES_API_KEY` environment variables are set. If not, tell the user to run `/mahpastes:setup` first and stop.

## Search

Use `$ARGUMENTS` as the search query:

```bash
curl -s -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  "$MAHPASTES_API_URL/api/v1/clips?search=QUERY&limit=20"
```

URL-encode the query value. If `$ARGUMENTS` is empty, ask the user what to search for, or list recent clips without a search filter.

## Display Results

Format results as a table showing:
- ID
- Filename
- Content type
- Size (human-readable)
- Creation date
- Tags

## Retrieve Content

If the user wants to view or use a specific clip, download its data:

```bash
curl -s -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  "$MAHPASTES_API_URL/api/v1/clips/ID/data"
```

- For text content (`text/*`, `application/json`, `application/xml`): display inline
- For binary content (images, PDFs, archives): save to a local file and report the path

## Filter Options

Additional query parameters for narrowing results:
- `tag=TAG_ID` — filter by tag (list tags first with `GET /api/v1/tags` to find the ID)
- `content_type=TYPE` — filter by MIME type (e.g., `text/plain`, `image/png`)
- `archived=true` — include archived clips (excluded by default)
- `limit=N` — max results per page (1-200, default 50)
- `offset=N` — pagination offset
