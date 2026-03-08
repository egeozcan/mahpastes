---
description: Host files via mahpastes tag-serve — upload, tag, and start an HTTP server
argument-hint: "<tag-name> [port]"
allowed-tools: Bash, Read, Glob, Write
---

Set up web hosting for files via mahpastes tag-serve.

## Prerequisites

Verify `MAHPASTES_API_URL` and `MAHPASTES_API_KEY` environment variables are set. If not, tell the user to run `/mahpastes:setup` first and stop.

## Parse Arguments

Extract from `$ARGUMENTS`:
- `$1` — Tag name for the site (required). If missing, ask the user.
- `$2` — Port number (optional). If not provided, pick a random port in the 3000-9000 range.

## Find or Create Tag

List existing tags and look for one matching `$1`:

```bash
curl -s -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  "$MAHPASTES_API_URL/api/v1/tags"
```

If not found, create it:

```bash
curl -s -X POST \
  -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"TAG_NAME"}' \
  "$MAHPASTES_API_URL/api/v1/tags"
```

Save the tag ID for later steps.

## Determine Files to Serve

Ask the user which files to include. Use conversation context — if files were recently generated, suggest those. Use the Glob tool to expand patterns if given.

For websites, ensure at least one file is named `index.html` so it serves at the root path.

## Upload and Tag Files

For each file:

1. Upload:
   ```bash
   curl -s -X POST \
     -H "Authorization: Bearer $MAHPASTES_API_KEY" \
     -F "file=@FILE_PATH" \
     "$MAHPASTES_API_URL/api/v1/clips"
   ```

2. Tag the uploaded clip:
   ```bash
   curl -s -X PUT \
     -H "Authorization: Bearer $MAHPASTES_API_KEY" \
     "$MAHPASTES_API_URL/api/v1/clips/CLIP_ID/tags/TAG_ID"
   ```

## Start Serving

```bash
curl -s -X POST \
  -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"tag_id":TAG_ID,"port":PORT,"bind_all":false}' \
  "$MAHPASTES_API_URL/api/v1/serve"
```

## Report

Tell the user the site is live and provide:
- URL: `http://127.0.0.1:PORT/`
- List of all served files with their URLs
- How to stop serving from the mahpastes app, or by calling `DELETE /api/v1/serve/TAG_ID`
