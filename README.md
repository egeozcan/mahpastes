# mahpastes

A local clipboard manager for macOS, Windows, and Linux. Store, organize, and quickly access your copied content.

![mahpastes](build/appicon.png)

## Features

- **Paste Anything** — Images, text, code, JSON, HTML, and files
- **Drag & Drop** — Drop files directly into the app
- **Auto-Delete** — Set clips to expire after 5, 10, 30 minutes, or 2 hours
- **Archive** — Keep important clips separate from your active workspace
- **Image Editor** — Annotate images with brush, shapes, and text tools
- **Text Editor** — Edit text and code clips directly
- **Image Comparison** — Compare two images with fade or slider modes
- **Bulk Actions** — Select multiple clips to archive, download, or delete
- **Search** — Filter clips by filename or type
- **Copy Path** — Create a temp file and copy its path to clipboard
- **Export** — Save individual clips or download multiple as a ZIP

## Installation

### Download Release

Download the latest release from the [Releases](https://github.com/yourusername/mahpastes/releases) page.

### Build from Source

#### Prerequisites

- [Go](https://go.dev/dl/) 1.21+
- [Wails CLI](https://wails.io/docs/gettingstarted/installation)
- [Node.js](https://nodejs.org/) 18+

```bash
# Install Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

#### Build

```bash
# Clone the repository
git clone https://github.com/yourusername/mahpastes.git
cd mahpastes

# Build for your platform
wails build

# The app will be in build/bin/
```

#### Platform-specific builds

```bash
# macOS (Universal - Intel + Apple Silicon)
wails build -platform darwin/universal

# Windows
wails build -platform windows/amd64

# Linux
wails build -platform linux/amd64
```

### Install on macOS

After building, copy to Applications:

```bash
cp -R build/bin/mahpastes.app /Applications/
```

## Development

```bash
# Install dependencies
cd frontend && npm install && cd ..

# Run in development mode
wails dev
```

The app will open and hot-reload when you make changes to the frontend.

### Project Structure

```
mahpastes/
├── main.go           # App entry point
├── app.go            # Core application logic and API
├── database.go       # SQLite database setup and cleanup
├── wails.json        # Wails configuration
├── build/            # Build assets and output
│   ├── appicon.png
│   └── bin/          # Built binaries
└── frontend/
    ├── index.html    # Main UI
    ├── css/          # Stylesheets
    ├── js/           # JavaScript modules
    └── tailwind.config.js
```

### Frontend Stack

- Vanilla JavaScript (no framework)
- Tailwind CSS
- IBM Plex Mono font

### Backend

- Go with Wails v2
- SQLite database (WAL mode)
- System clipboard integration via `golang.design/x/clipboard`

## Data Storage

Data is stored in platform-specific locations:

| Platform | Location |
|----------|----------|
| macOS    | `~/Library/Application Support/mahpastes/` |
| Windows  | `%APPDATA%\mahpastes\` |
| Linux    | `~/.config/mahpastes/` |

The database file is `clips.db`. Temporary files are stored in `clip_temp_files/` and cleaned up on app exit.

## Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| `Cmd/Ctrl + V` | Paste from clipboard |
| `Escape` | Close modals |

### Image Editor

| Shortcut | Tool |
|----------|------|
| `B` | Brush |
| `L` | Line |
| `R` | Rectangle |
| `C` | Circle |
| `T` | Text |
| `E` | Eraser |
| `Cmd/Ctrl + Z` | Undo |
| `Cmd/Ctrl + Y` | Redo |

## License

MIT
