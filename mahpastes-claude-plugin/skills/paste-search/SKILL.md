---
name: paste-search
description: "Search and browse clips in mahpastes, download clip data"
user-invocable: true
allowed-tools:
  - Bash
  - Write
argument-hint: "<query> [--tag tag-name] [--type content-type] [--download clip-id]"
---

# /paste-search — Search Clips

Search and browse clips in mahpastes. Download clip data to the local filesystem.

## Prerequisites

Requires `MAHPASTES_API_URL` and `MAHPASTES_API_KEY` environment variables. If missing, tell the user to run `/paste-setup` first.

## Steps

### 1. Resolve credentials

```bash
URL="$MAHPASTES_API_URL"
KEY="$MAHPASTES_API_KEY"
```

### 2. Parse arguments

- `<query>` — text to search filenames and content
- `--tag <name>` — filter by tag name (resolve to tag ID first)
- `--type <content-type>` — filter by content type (e.g. `image/png`, `text/html`)
- `--download <clip-id>` — download a specific clip's data to a local file
- No arguments — list tags with clip counts first, then ask what to search

### 3. Resolve tag filter (if `--tag`)

```bash
TAG_ID=$(curl -sf -H "Authorization: Bearer $KEY" "$URL/api/v1/tags" \
  | python3 -c "import json,sys; tags=json.load(sys.stdin); print(next((t['id'] for t in tags if t['name']=='$TAG_NAME'),''))")
```

If tag not found, report and exit.

### 4. Search clips

```bash
curl -sf -H "Authorization: Bearer $KEY" \
  "$URL/api/v1/clips?search=$QUERY&tag=$TAG_ID&content_type=$TYPE&limit=20"
```

### 5. Display results

Format as a table:

```
ID   | Filename          | Type       | Size    | Tags          | Date
-----|-------------------|------------|---------|---------------|------------
42   | screenshot.png    | image/png  | 1.2 MB  | docs, v2      | 2026-03-01
43   | notes.md          | text/md    | 4.5 KB  | claude-memory  | 2026-03-05
```

Report total count and whether there are more results (pagination).

### 6. Download (if `--download`)

```bash
# Get clip metadata for filename
FILENAME=$(curl -sf -H "Authorization: Bearer $KEY" "$URL/api/v1/clips/$CLIP_ID" \
  | python3 -c "import json,sys; print(json.load(sys.stdin)['filename'])")

# Download data
curl -sf -H "Authorization: Bearer $KEY" \
  "$URL/api/v1/clips/$CLIP_ID/data" \
  -o "$FILENAME"
```

Report the downloaded file path and size.

## Examples

```
/paste-search screenshot
/paste-search --tag docs --type image/png
/paste-search --download 42
/paste-search                    # lists tags first
```

## Edge Cases

| Condition | Behavior |
|-----------|----------|
| No results | Report "no clips found" with the search criteria |
| Tag not found | Report error, list available tags |
| Download clip not found | Report 404 |
| Binary content | Download to file, don't display inline |
| Text content | Optionally show preview (first 20 lines) |
