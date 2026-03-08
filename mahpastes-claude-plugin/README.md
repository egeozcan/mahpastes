# mahpastes Claude Code Plugin

Use [mahpastes](https://github.com/egecan/mahpastes) as an artifact store, web host, and cross-session memory for Claude Code.

## Features

- **Upload artifacts** — Save generated files, screenshots, and content to mahpastes
- **Search & download** — Find and retrieve clips by name, tag, or content type
- **Web hosting** — Start tag-serve HTTP servers to host clips as static sites
- **Cross-session memory** — Remember and recall knowledge across Claude Code sessions

## Installation

```bash
claude plugin add mahpastes
```

Or install from a local path:

```bash
claude plugin add /path/to/mahpastes-claude-plugin
```

## Setup

After installing the plugin, configure your mahpastes connection:

```
/paste-setup
```

This will:
1. Ask for your mahpastes API URL (default: `http://127.0.0.1:8484`)
2. Ask for an API key (create one in mahpastes → Settings → API → New Key)
3. Save credentials to `~/.claude/settings.json`
4. Create a `claude-memory` tag for cross-session memory

**Note:** Environment variables take effect in the next Claude Code session after setup.

## Commands

| Command | Description |
|---------|-------------|
| `/paste-setup` | Configure mahpastes connection |
| `/paste-upload <file> [--tag name]` | Upload files to mahpastes |
| `/paste-search <query> [--tag name]` | Search and browse clips |
| `/paste-serve <start\|stop\|status> [tag]` | Manage tag-serve HTTP hosting |
| `/paste-remember <save\|recall\|list> [topic]` | Cross-session memory |

## Auto-Detection

The plugin includes an auto-invoke skill that detects when mahpastes could be useful:
- Generating files that should be persisted
- Wanting to host or share static content
- Needing cross-session memory
- Mentioning clips, pastes, or mahpastes

## Examples

### Upload a generated file

```
/paste-upload ./report.html --tag reports
```

### Host a tag as a website

```
/paste-serve start docs
# → http://127.0.0.1:9100
```

### Save a memory

```
/paste-remember save "This project uses PostgreSQL 16 with pgvector"
```

### Recall a memory

```
/paste-remember recall database
```

### Search for clips

```
/paste-search --tag docs --type image/png
```

## Requirements

- mahpastes running with the API server started
- An API key with appropriate permissions (admin recommended for full functionality)
- `curl` available in PATH

## API Key Roles

| Role | Can do |
|------|--------|
| **viewer** | Search, browse, download clips |
| **editor** | + upload, delete, tag clips |
| **admin** | + create/delete tags, manage serve |

For full functionality (including tag creation and serve management), use an admin key.

## License

MIT
