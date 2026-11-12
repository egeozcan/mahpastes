---
description: Upload files or generated content to mahpastes
argument-hint: "<file-path or glob>"
allowed-tools: Bash, Read, Glob
---

Upload files to mahpastes with optional tagging.

## Prerequisites

Verify `MP_API_URL` and `MP_API_KEY` environment variables are set. If not, tell the user to run `/mahpastes:setup` first and stop.

If `mp` is on PATH (`command -v mp`), prefer `mp clip upload FILE... --tag NAME` (it accepts several files and applies the tag in one step). The curl examples below are the fallback.

## Determine What to Upload

Parse `$ARGUMENTS`:
- If a file path, upload that file
- If a glob pattern, expand with the Glob tool and upload each match
- If empty or a description, ask the user which file(s) to upload

## Upload Each File

For each file:

```bash
curl -s -X POST \
  -H "Authorization: Bearer $MP_API_KEY" \
  -F "file=@FILE_PATH" \
  "$MP_API_URL/api/v1/clips"
```

Parse the JSON response to extract the clip `id` and `filename`.

To override the filename, append `?filename=custom-name.ext` to the URL.

## Optional: Tag Uploads

After uploading, ask if the user wants to tag the clips. If yes:

1. List existing tags:
   ```bash
   curl -s -H "Authorization: Bearer $MP_API_KEY" \
     "$MP_API_URL/api/v1/tags"
   ```

2. Let the user pick an existing tag or create a new one:
   ```bash
   curl -s -X POST \
     -H "Authorization: Bearer $MP_API_KEY" \
     -H "Content-Type: application/json" \
     -d '{"name":"TAG_NAME"}' \
     "$MP_API_URL/api/v1/tags"
   ```

3. Apply the tag to each uploaded clip:
   ```bash
   curl -s -X PUT \
     -H "Authorization: Bearer $MP_API_KEY" \
     "$MP_API_URL/api/v1/clips/CLIP_ID/tags/TAG_ID"
   ```

## Report

Show a summary of uploaded clips: IDs, filenames, sizes, and applied tags (if any).
