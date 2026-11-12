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

By default this installs `mp` into a user-writable bin directory:

- macOS/Linux: `~/.local/bin` unless `GOBIN` is set
- Windows: `GOBIN`, then `%GOPATH%\bin`, then `%USERPROFILE%\go\bin`

If `~/.local/bin` is not in your `PATH`, add it in your shell profile:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

On Windows, make sure the resolved install directory is in your `PATH`.

If you prefer a different install directory, run:

```bash
make mp-install MP_INSTALL_DIR=/usr/local/bin
```

### Configure

Set your API key as an environment variable. Create a key in the desktop app under **Settings** > **API** — or, once you already hold an admin key, with [`mp api key create`](#api-keys).

```bash
export MP_API_KEY=mp_your_key_here
```

Add this line to your shell profile (`~/.zshrc` or `~/.bashrc`) so it persists across sessions.

### Verify

Confirm the CLI can reach the API and that your key is accepted:

```bash
mp api status
```

Expected output:

```
Connected to mahpastes API at http://localhost:44557
```

Shell completion and man pages are installed with `make mp-install`; see [Shell Completion](../getting-started/installation.md#shell-completion). Every command and flag is listed in the [CLI Reference](../reference/cli.md).

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MP_API_KEY` | Yes | -- | API key starting with `mp_` |
| `MP_API_URL` | No | `http://localhost:44557` | API base URL |
| `MP_TIMEOUT` | No | `30s` | How long to wait for the connection and for the server to start responding (e.g. `10s` or `10`). It never limits the transfer itself, so large uploads and downloads are not cut off, and commands that wait on long server-side work only use it for the connection (`backup create`/`restore`, `maintenance`, `dedup all`, `plugin install`/`update`/`run`, `watch process`, `clip download` of several clips). On expiry `mp` exits with code 2 |

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

Upload from stdin. Without `--filename` the clip is named `paste-<timestamp>.<ext>`, with the extension sniffed from the content (`.txt` for text, `.png`, `.pdf`, `.gz` and so on for known binary types, `.bin` otherwise; `--content-type` picks the extension instead). `-` as the file argument means stdin too. Empty input is an error rather than an empty clip, and `mp clip upload` with no arguments on an interactive terminal stops with an error instead of waiting for input (pass `-` to type the clip instead, ending it with Ctrl-D, or Ctrl-Z then Enter on Windows):

```bash
echo "meeting notes" | mp clip upload
echo "meeting notes" | mp clip upload --filename notes.txt
some-command | mp clip upload - --tag logs
```

Use `-q`/`--quiet` on `upload` and `list` to print only bare clip IDs, one per line, for capturing in scripts:

```bash
id=$(echo hi | mp clip upload -q)
mp clip list --tag logs -q | mp clip delete --stdin
```

If any file in a multi-file upload fails (or cannot be tagged or given its expiration), `mp clip upload` still prints the clips that did upload and then exits with code 1.

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

Search by filename or content:

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

Sort by `created_at` (the default), `name`, `size` or `type`, in `desc` (the default) or `asc` order. Unknown values are rejected:

```bash
mp clip list --sort size --dir desc --limit 5
```

## Downloading Clips

Stream raw clip data to a file:

```bash
mp clip data 42 > output.png
```

Print the newest non-archived clip, optionally within a tag, like `pbpaste`:

```bash
mp clip data --latest
mp clip data --latest --tag notes | wc -l
```

When stdout is a terminal, `mp clip data` refuses binary content (an image, an archive) instead of dumping it onto the screen. Redirect the output, or pass `--force` to write it anyway.

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

## Renaming and Unarchiving

Rename a clip:

```bash
mp clip rename 42 new-name.png
```

Unarchive clips:

```bash
mp clip unarchive 42
mp clip unarchive 1 2 3
```

## Clip Metadata

Each clip can have arbitrary key-value metadata:

```bash
mp clip metadata list 42
mp clip metadata get 42 author
mp clip metadata set 42 author "John Doe"
mp clip metadata delete 42 author
```

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

List clips with a specific tag:

```bash
mp tag clips photos --limit 20
```

Update a tag's name or color:

```bash
mp tag update photos --name "my photos" --color "#00ff00"
```

Delete a tag:

```bash
mp tag delete photos
```

View or set hidden tags:

```bash
mp tag hidden
mp tag hidden --set "1,2,3"
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

Update a watch folder's settings:

```bash
mp watch update 1 --filter presets --presets images,videos
```

Remove a watch folder:

```bash
mp watch remove 1
```

Process existing files in a watch folder:

```bash
mp watch process 1
```

## Deduplication

List groups of duplicate clips:

```bash
mp dedup list
```

Merge duplicates for a specific clip (keeps the specified clip, removes others):

```bash
mp dedup merge 42
```

Remove all duplicates across the library:

```bash
mp dedup all
```

## Plugins

List installed plugins:

```bash
mp plugin list
```

Install, enable, disable, or remove:

```bash
mp plugin install https://example.com/my-plugin.lua
mp plugin enable 1
mp plugin disable 1
mp plugin remove 1
```

Run a plugin action:

```bash
mp plugin run 1 process-image --clip 42 --option quality=high
```

Check for updates or update a specific plugin:

```bash
mp plugin update --check
mp plugin update 1
```

Manage plugin storage:

```bash
mp plugin storage list 1
mp plugin storage get 1 api_key
mp plugin storage set 1 api_key sk-12345
```

## Tag Serving

Start an HTTP server for a tag's clips:

```bash
mp serve start my-site --port 8080 --api-access readwrite
```

List running servers:

```bash
mp serve list
```

Stop a server:

```bash
mp serve stop my-site
```

## Clipboard

Copy clip contents or a file reference to the system clipboard (requires the desktop app):

```bash
mp clipboard copy 42
mp clipboard copy-file 42
```

## API Keys

Manage the keys the CLI itself authenticates with. All three commands need an **admin** key in `MP_API_KEY`.

Create a key:

```bash
mp api key create "CI pipeline"
```

The plaintext key is printed once, under `key`, and cannot be retrieved afterwards — only its hash is stored. The "save this key" reminder is written to stderr, so it never pollutes piped output; with `--json` the full result is emitted as JSON and can be read straight off:

```bash
export MP_API_KEY=$(mp api key create ci --role editor --json | jq -r .key)
```

| Flag | Default | Description |
|------|---------|-------------|
| `--role` | `viewer` | `viewer`, `editor`, or `admin` |
| `--scoped-tag` | `0` (unscoped) | Tag **ID** to confine the key to (not a tag name) |

```bash
mp api key create deploy-bot --role editor --scoped-tag 3
```

:::warning
An admin key can mint further keys of any role, scoped or not — key management is not confined by tag scope. Hand out `viewer` or `editor` keys for delegated access, and keep admin keys to yourself.
:::

List keys:

```bash
mp api key list
```

```
ID  NAME         ROLE    PREFIX      SCOPE          STATUS
7   CI pipeline  editor  mp_a1b2...  work/client1   active
4   old-laptop   viewer  mp_9f8e...                 revoked 2026-07-24 08:15:02
```

Revoked keys stay listed for 7 days after revocation and are then deleted; `STATUS` shows when the clock started. Raw keys are never shown — `PREFIX` is the first 8 characters.

Revoke a key by ID:

```bash
mp api key revoke 4
```

Revoking takes effect on the next request. A key that does not exist, or was already revoked, exits non-zero.

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
| 2 | Connection error (API unreachable or not responding within `MP_TIMEOUT`) |
| 3 | Authentication error (missing, invalid or revoked key) |

`mp --version` prints the CLI version; send it along with bug reports.

Use exit codes in scripts:

```bash
mp api status > /dev/null 2>&1
case $? in
  0) ;;
  2) echo "mahpastes is not running or its API is disabled" ;;
  3) echo "MP_API_KEY is missing, invalid or revoked" ;;
  *) echo "mp api status failed" ;;
esac
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
