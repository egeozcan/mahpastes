---
name: paste-upload
description: "Upload files or generated content to mahpastes with optional tagging"
user-invocable: true
allowed-tools:
  - Bash
  - Read
  - Write
  - Glob
argument-hint: "<file-or-content> [--tag tag-name]"
---

# /paste-upload — Upload to mahpastes

Upload files or generated content to mahpastes with optional tagging.

## Prerequisites

Requires `MAHPASTES_API_URL` and `MAHPASTES_API_KEY` environment variables. If missing, tell the user to run `/paste-setup` first.

## Steps

### 1. Resolve credentials

```bash
URL="$MAHPASTES_API_URL"
KEY="$MAHPASTES_API_KEY"
```

If either is empty, stop and suggest `/paste-setup`.

### 2. Determine what to upload

Parse the argument:

- **File path** (e.g. `./output.png`, `/tmp/report.html`): Upload the file directly.
- **Glob pattern** (e.g. `dist/*.js`): Expand and upload each matching file.
- **Inline content**: If the argument doesn't match a file, treat it as content to upload. Write it to a temp file first.
- **No argument**: Ask the user what to upload.

### 3. Upload each file

```bash
curl -sf -X POST \
  -F "file=@$FILEPATH" \
  -H "Authorization: Bearer $KEY" \
  "$URL/api/v1/clips?filename=$(basename "$FILEPATH")"
```

Capture the response JSON to get the clip ID.

### 4. Apply tag (if `--tag` specified)

First, find or create the tag:

```bash
# List tags and find by name
TAG_ID=$(curl -sf -H "Authorization: Bearer $KEY" "$URL/api/v1/tags" \
  | python3 -c "import json,sys; tags=json.load(sys.stdin); print(next((t['id'] for t in tags if t['name']=='$TAG_NAME'),''))")

# If tag doesn't exist, create it (requires admin key)
if [ -z "$TAG_ID" ]; then
  TAG_ID=$(curl -sf -X POST \
    -H "Authorization: Bearer $KEY" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"$TAG_NAME\"}" \
    "$URL/api/v1/tags" | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
fi

# Apply tag to clip
curl -sf -X PUT \
  -H "Authorization: Bearer $KEY" \
  "$URL/api/v1/clips/$CLIP_ID/tags/$TAG_ID"
```

### 5. Report results

For each uploaded file, report:
- Clip ID
- Filename
- Size (human-readable)
- Content type
- Tags applied
- Whether it was a duplicate (API returns existing clip for matching content hash)

Format as a table for multiple files.

## Examples

```
/paste-upload ./screenshot.png --tag docs
/paste-upload dist/**/*.js --tag release-v2
/paste-upload "Hello world content"
```

## Edge Cases

| Condition | Behavior |
|-----------|----------|
| File doesn't exist | Report error, skip |
| Duplicate content | API returns existing clip (200 instead of 201) — report "already exists" |
| Tag creation fails | Key may not be admin — report and suggest using an admin key |
| Large file (>100MB) | API rejects — report size limit |
| No API connection | Report connection error, suggest checking mahpastes is running |
