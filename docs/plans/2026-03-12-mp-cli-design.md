# mahpastes CLI (`mp`) — Design Document

**Date:** 2026-03-12
**Status:** Approved

## Goals

Build a CLI (`mp`) for mahpastes that covers the full feature set via the REST API. Primary use cases:

1. **Scripting/automation** — pipe clips in and out, batch operations from shell scripts
2. **Power-user companion** — quick terminal operations alongside the desktop app
3. **Agentic use** — AI agents interacting with mahpastes programmatically

## Architecture

The CLI is a stateless HTTP client that talks to the mahpastes REST API. No direct database access, no local state, no config files.

```
mp (Cobra CLI) → HTTP → mahpastes REST API (/api/v1/*) → App methods → SQLite
```

### Key decisions

- **Framework:** Cobra (Go standard for CLIs)
- **Binary name:** `mp`
- **Auth:** Environment variables only (`MP_API_URL`, `MP_API_KEY`)
- **Output:** Human-friendly text by default, `--json` flag for machine/agent consumption
- **Content access:** `mp clip get` shows metadata, `mp clip data` outputs raw content
- **Cross-platform:** Pure Go, no CGo, builds for macOS/Linux/Windows
- **API-first:** Extend the REST API to full parity, CLI is a thin wrapper

## REST API — Existing Endpoints

These already exist in `api_manager.go` (~15 endpoints):

| Method | Endpoint | Purpose | Role |
|--------|----------|---------|------|
| GET | /api/v1/clips | List clips with filters | viewer |
| POST | /api/v1/clips | Upload clip | editor |
| GET | /api/v1/clips/{id} | Get clip metadata | viewer |
| DELETE | /api/v1/clips/{id} | Delete clip | editor |
| GET | /api/v1/clips/{id}/data | Download raw content | viewer |
| PUT | /api/v1/clips/{id}/archive | Archive clip | editor |
| DELETE | /api/v1/clips/{id}/archive | Unarchive clip | editor |
| GET | /api/v1/tags | List tags | viewer |
| POST | /api/v1/tags | Create tag | admin |
| PUT | /api/v1/tags/{id} | Update tag | admin |
| DELETE | /api/v1/tags/{id} | Delete tag | admin |
| PUT | /api/v1/clips/{id}/tags/{tagId} | Add tag to clip | editor |
| DELETE | /api/v1/clips/{id}/tags/{tagId} | Remove tag from clip | editor |
| GET | /api/v1/serve | List running tag servers | viewer |
| POST | /api/v1/serve | Start tag server | admin |
| DELETE | /api/v1/serve/{tagId} | Stop tag server | admin |

## REST API — New Endpoints (~45)

### Clips — extended operations

| Method | Endpoint | Purpose | Role |
|--------|----------|---------|------|
| PATCH | /api/v1/clips/{id} | Rename clip | editor |
| PUT | /api/v1/clips/{id}/expiration | Set expiration | editor |
| DELETE | /api/v1/clips/{id}/expiration | Cancel expiration | editor |
| POST | /api/v1/clips/bulk/delete | Bulk delete | editor |
| POST | /api/v1/clips/bulk/archive | Bulk archive | editor |
| POST | /api/v1/clips/bulk/unarchive | Bulk unarchive | editor |
| POST | /api/v1/clips/bulk/expire | Bulk set expiration | editor |
| POST | /api/v1/clips/bulk/cancel-expire | Bulk cancel expiration | editor |
| POST | /api/v1/clips/bulk/tag | Bulk add tag | editor |
| POST | /api/v1/clips/bulk/untag | Bulk remove tag | editor |
| POST | /api/v1/clips/bulk/download | Download as ZIP | viewer |
| POST | /api/v1/clips/bulk/copy | Copy files to clipboard | editor |

### Clip metadata

| Method | Endpoint | Purpose | Role |
|--------|----------|---------|------|
| GET | /api/v1/clips/{id}/metadata | List all metadata | viewer |
| PUT | /api/v1/clips/{id}/metadata | Set metadata (bulk) | editor |
| PUT | /api/v1/clips/{id}/metadata/{key} | Set single key | editor |
| DELETE | /api/v1/clips/{id}/metadata/{key} | Delete key | editor |

### Tags — extended

| Method | Endpoint | Purpose | Role |
|--------|----------|---------|------|
| GET | /api/v1/tags/{id}/children | Get child tags | viewer |
| GET | /api/v1/tags/{id}/clips | List clips for tag | viewer |
| GET | /api/v1/tags/hidden | Get hidden tag IDs | viewer |
| PUT | /api/v1/tags/hidden | Set hidden tags | admin |

### Deduplication

| Method | Endpoint | Purpose | Role |
|--------|----------|---------|------|
| GET | /api/v1/dedup | List duplicate groups | viewer |
| POST | /api/v1/dedup/{clipId}/merge | Merge duplicates for clip | editor |
| POST | /api/v1/dedup/all | Deduplicate everything | admin |

### Watch folders

| Method | Endpoint | Purpose | Role |
|--------|----------|---------|------|
| GET | /api/v1/watch | List watched folders | viewer |
| GET | /api/v1/watch/{id} | Get single watch folder | viewer |
| POST | /api/v1/watch | Add watch folder | admin |
| PUT | /api/v1/watch/{id} | Update watch config | admin |
| DELETE | /api/v1/watch/{id} | Remove watch folder | admin |
| PUT | /api/v1/watch/{id}/pause | Pause folder | admin |
| DELETE | /api/v1/watch/{id}/pause | Resume folder | admin |
| PUT | /api/v1/watch/global-pause | Pause all | admin |
| DELETE | /api/v1/watch/global-pause | Resume all | admin |
| POST | /api/v1/watch/{id}/process | Process existing files | admin |
| GET | /api/v1/watch/status | Watch status summary | viewer |

### Plugins

| Method | Endpoint | Purpose | Role |
|--------|----------|---------|------|
| GET | /api/v1/plugins | List plugins | viewer |
| POST | /api/v1/plugins | Install plugin (URL or upload) | admin |
| DELETE | /api/v1/plugins/{id} | Remove plugin | admin |
| PUT | /api/v1/plugins/{id}/enable | Enable plugin | admin |
| PUT | /api/v1/plugins/{id}/disable | Disable plugin | admin |
| GET | /api/v1/plugins/{id}/storage | Get all plugin storage | admin |
| GET | /api/v1/plugins/{id}/storage/{key} | Get storage value | admin |
| PUT | /api/v1/plugins/{id}/storage/{key} | Set storage value | admin |
| POST | /api/v1/plugins/{id}/actions/{actionId} | Execute plugin action | editor |
| GET | /api/v1/plugins/actions | List all UI actions | viewer |
| POST | /api/v1/plugins/check-updates | Check for updates | admin |
| POST | /api/v1/plugins/{id}/update | Update plugin | admin |

### Backup

| Method | Endpoint | Purpose | Role |
|--------|----------|---------|------|
| GET | /api/v1/backup | Download backup ZIP | admin |
| POST | /api/v1/backup/restore | Upload and restore backup | admin |

### Clipboard

| Method | Endpoint | Purpose | Role |
|--------|----------|---------|------|
| POST | /api/v1/clipboard/copy | Copy clip content to clipboard | editor |
| POST | /api/v1/clipboard/copy-file | Copy clip as file ref | editor |

## CLI Command Structure

```
mp
├── clip
│   ├── list          # List clips (--tag, --archived, --sort, --limit, --offset)
│   ├── get           # Show clip metadata by ID
│   ├── data          # Output raw clip content to stdout
│   ├── upload        # Upload files (positional args or stdin)
│   ├── delete        # Delete clip(s)
│   ├── rename        # Rename clip
│   ├── archive       # Archive clip(s)
│   ├── unarchive     # Unarchive clip(s)
│   ├── expire        # Set expiration (--cancel to remove)
│   ├── download      # Download clip(s) to file/ZIP
│   ├── open          # Open with default app (--app to specify)
│   └── metadata
│       ├── list      # List all metadata for clip
│       ├── get       # Get single metadata value
│       ├── set       # Set metadata key=value
│       └── delete    # Delete metadata key
├── tag
│   ├── list          # List tags (--children-of for subtree)
│   ├── create        # Create tag (supports hierarchical a/b/c)
│   ├── update        # Update name/color
│   ├── delete        # Delete tag
│   ├── assign        # Add tag to clip(s)
│   ├── remove        # Remove tag from clip(s)
│   ├── clips         # List clips for a tag
│   └── hidden        # Get/set hidden tags
├── dedup
│   ├── list          # Show duplicate groups
│   ├── merge         # Merge specific duplicate group
│   └── all           # Deduplicate everything
├── watch
│   ├── list          # List watched folders
│   ├── add           # Add watch folder
│   ├── update        # Update watch config
│   ├── remove        # Remove watch folder
│   ├── pause         # Pause folder or all (--global)
│   ├── resume        # Resume folder or all (--global)
│   ├── status        # Watch status summary
│   └── process       # Process existing files in folder
├── plugin
│   ├── list          # List installed plugins
│   ├── install       # Install from URL or path
│   ├── remove        # Uninstall plugin
│   ├── enable        # Enable plugin
│   ├── disable       # Disable plugin
│   ├── run           # Execute a plugin action
│   ├── storage       # Get/set plugin storage (subcommands)
│   └── update        # Check for / apply updates
├── serve
│   ├── list          # List running tag servers
│   ├── start         # Start serving a tag
│   └── stop          # Stop serving a tag
├── api
│   ├── status        # API server status
│   ├── start         # Start API server
│   ├── stop          # Stop API server
│   └── key
│       ├── create    # Create API key
│       ├── list      # List API keys
│       └── revoke    # Revoke API key
├── backup
│   ├── create        # Export backup ZIP
│   └── restore       # Restore from backup ZIP
├── clipboard
│   ├── copy          # Copy clip content to clipboard
│   └── copy-file     # Copy clip as file reference to clipboard
└── completion        # Shell completion (bash, zsh, fish, powershell)
```

## CLI Project Layout

```
cmd/mp/
├── main.go              # Cobra root command, env var setup
├── clip.go              # clip subcommands
├── tag.go               # tag subcommands
├── dedup.go             # dedup subcommands
├── watch.go             # watch subcommands
├── plugin.go            # plugin subcommands
├── serve.go             # serve subcommands
├── api.go               # api + api key subcommands
├── backup.go            # backup subcommands
├── clipboard.go         # clipboard subcommands
├── output.go            # human/JSON formatting helpers
└── client/
    └── client.go        # HTTP client (auth, base URL, error handling)
```

## Authentication

Environment variables only:

| Variable | Purpose | Default |
|----------|---------|---------|
| `MP_API_URL` | API base URL | `http://localhost:44557` |
| `MP_API_KEY` | Bearer token | (required) |

No config files. Agents set env vars; users `export` them in shell profile.

## Output Format

- **Default:** Human-friendly tables and key-value display
- **`--json`:** Raw JSON from API (passthrough), suitable for `jq` piping
- **Stderr:** Progress messages, errors
- **Stdout:** Data only — so `mp clip data 42 > file.png` works cleanly

## Piping Patterns

| Command | Stdin | Stdout |
|---------|-------|--------|
| `mp clip upload` | File content (no args) | Clip metadata / ID |
| `mp clip data 42` | — | Raw clip content |
| `mp clip list` | — | Table or JSON array |
| `mp clip delete --stdin` | IDs from stdin | Confirmation |
| `mp backup create -` | — | ZIP to stdout |
| `mp backup restore -` | ZIP from stdin | Progress |

## Bulk Operations

Commands accepting IDs take multiple positional args:

```bash
mp clip delete 1 2 3
mp clip archive 10 11 12
```

For large batches, `--stdin` reads IDs line-by-line:

```bash
mp clip list --tag old --json | jq -r '.[].id' | mp clip delete --stdin
```

## Tag References

Tags can be referenced by name or ID:

```bash
mp tag assign 42 --tag screenshots      # by name
mp tag assign 42 --tag 7                # by ID
mp tag assign 42 --tag work/client1     # hierarchical name
```

CLI resolves names to IDs client-side via `GET /api/v1/tags`.

## Duration Parsing

`--expire` accepts: `30m`, `2h`, `7d`, `1h30m`. Converted to minutes for the API.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error (API error, validation) |
| 2 | Connection error (app not running) |
| 3 | Authentication error |

## Help Text

Every command includes:
- **Short:** One-line description
- **Long:** 2-3 sentences on purpose and usage
- **Example:** 2-4 real-world examples with comments
- **Flags:** Each with description and default

Designed for agent self-discovery — an LLM can run `mp --help`, then `mp clip --help`, then `mp clip upload --help` to learn the full interface.

## Build

Added to Makefile:

```makefile
mp:            # Build CLI for current platform
mp-install:    # Install to /usr/local/bin (macOS/Linux) or PATH-accessible location (Windows)
mp-cross:      # Cross-compile for darwin/amd64, darwin/arm64, linux/amd64, windows/amd64
```

Pure Go, no CGo — `GOOS=windows go build ./cmd/mp` works.

## What This Does NOT Include

- No TUI or interactive mode
- No local config files or cached state
- No direct database access
- No platform-specific CLI behavior
- No Wails frontend changes
