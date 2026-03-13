---
sidebar_position: 4
---

# CLI Automation

Use the `mp` command-line tool to manage clips, tags, and backups from your terminal. All operations go through the mahpastes REST API -- the desktop app must be running.

## Setup

### Install

Build and install the binary:

```bash
make mp-install
```

This places `mp` in `/usr/local/bin`.

### Configure

Set your API key as an environment variable. Create a key in the desktop app under **Settings** > **API**.

```bash
export MP_API_KEY=mp_your_key_here
```

Add this line to your shell profile (`~/.zshrc` or `~/.bashrc`) so it persists across sessions.

### Verify

Confirm the CLI can reach the API:

```bash
mp api status
```

Expected output:

```
Connected to mahpastes API at http://localhost:44557
```

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MP_API_KEY` | Yes | -- | API key starting with `mp_` |
| `MP_API_URL` | No | `http://localhost:44557` | API base URL |

## Uploading Files

Upload a single file:

```bash
mp clip upload photo.png
```

Upload with a tag:

```bash
mp clip upload photo.png --tag screenshots
```

Upload multiple files at once:

```bash
mp clip upload *.png --tag batch
```

Upload from stdin (requires `--filename`):

```bash
echo "meeting notes" | mp clip upload --filename notes.txt
```

Upload with an expiration:

```bash
mp clip upload temp.txt --expire 30m
```

:::tip
Duration formats: `30m` (minutes), `2h` (hours), `7d` (days), `1d12h` (combined).
:::

## Listing and Filtering

List recent clips:

```bash
mp clip list
```

Filter by tag:

```bash
mp clip list --tag work --limit 10
```

Search by filename:

```bash
mp clip list --search report
```

Filter by content type:

```bash
mp clip list --content-type image/png
```

Show archived clips:

```bash
mp clip list --tag old-project --archived
```

Paginate through results:

```bash
mp clip list --limit 20 --offset 40
```

## Downloading Clips

Stream raw clip data to a file:

```bash
mp clip data 42 > output.png
```

Download with the original filename:

```bash
mp clip download 42
```

Download to a specific path:

```bash
mp clip download 42 -o ~/Downloads/photo.png
```

Download multiple clips as a ZIP:

```bash
mp clip download 1 2 3 -o clips.zip
```

## Bulk Operations

Delete multiple clips by ID:

```bash
mp clip delete 1 2 3
```

Pipe IDs from another command:

```bash
mp clip list --tag temp --json | jq -r '.clips[].id' | mp clip delete --stdin
```

Archive in bulk:

```bash
mp clip archive 10 11 12
```

Set expiration on multiple clips:

```bash
mp clip expire 5 6 7 --duration 7d
```

:::note
Bulk commands accept `--stdin` to read one ID per line from stdin. This pairs well with `jq` and other pipeline tools.
:::

## Tag Management

Create a tag (parent tags are created automatically):

```bash
mp tag create work/projects
```

Assign a tag to a clip:

```bash
mp tag assign 42 --tag work
```

Assign a tag to multiple clips:

```bash
mp tag assign 1 2 3 --tag work/client1
```

Remove a tag from a clip:

```bash
mp tag remove 42 --tag work
```

List all tags:

```bash
mp tag list
```

List children of a tag:

```bash
mp tag list --children-of work
```

## Watch Folders

Add a watch folder for images:

```bash
mp watch add ~/Screenshots --filter presets --presets images
```

Add a folder with auto-tagging:

```bash
mp watch add ~/Downloads --auto-tag downloads
```

List configured watch folders:

```bash
mp watch list
```

Pause and resume:

```bash
mp watch pause 1
mp watch resume 1
```

Pause all folders globally:

```bash
mp watch pause --global
```

Check watch system status:

```bash
mp watch status
```

## Backups

Create a backup:

```bash
mp backup create ~/backups/mahpastes-$(date +%Y%m%d).zip
```

Pipe a backup to cloud storage:

```bash
mp backup create - | aws s3 cp - s3://bucket/mahpastes-backup.zip
```

Restore from a backup:

```bash
mp backup restore ~/backups/mahpastes-20260313.zip
```

## JSON Output

Every command supports `--json` for machine-readable output:

```bash
mp clip list --json | jq '.clips[].id'
```

```bash
mp tag list --json | jq '.[] | select(.count > 10)'
```

```bash
mp clip get 42 --json | jq '.filename'
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Connection error (app not running or API not enabled) |
| 3 | Authentication error (invalid or revoked key) |

Use exit codes in scripts:

```bash
mp api status > /dev/null 2>&1
if [ $? -eq 2 ]; then
  echo "mahpastes is not running"
fi
```

## Example: Daily Backup Script

```bash
#!/bin/bash
set -e

BACKUP_DIR="$HOME/backups/mahpastes"
mkdir -p "$BACKUP_DIR"

FILENAME="mahpastes-$(date +%Y%m%d-%H%M%S).zip"

mp backup create "$BACKUP_DIR/$FILENAME"

# Keep only the last 7 backups
ls -t "$BACKUP_DIR"/mahpastes-*.zip | tail -n +8 | xargs rm -f

echo "Backup saved: $FILENAME"
```

## Related

- [Clipboard Management](../features/clipboard-management.md) -- feature reference
- [Watch Folders](../features/watch-folders.md) -- watch folder details
- [Troubleshooting](../reference/troubleshooting.md) -- CLI error solutions
