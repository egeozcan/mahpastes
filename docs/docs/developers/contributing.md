---
sidebar_position: 6
---

# Contributing

This guide covers how to set up the development environment, make changes, and submit them.

## Development Setup

### Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| [Go](https://go.dev/dl/) | 1.24+ | Backend development |
| [Node.js](https://nodejs.org/) | 18+ | Frontend tooling |
| [Wails CLI](https://wails.io/) | 2.x | Build and development |

### Install Wails CLI

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Verify installation:

```bash
wails doctor
```

### Clone and Setup

```bash
# Clone the repository
git clone https://github.com/egeozcan/mahpastes.git
cd mahpastes

# Install frontend dependencies
cd frontend && npm install && cd ..
```

### Run Development Mode

```bash
make dev
```

Or directly:
```bash
~/go/bin/wails dev
```

This starts the app with:
- Hot reload for frontend changes
- Automatic rebuild for Go changes
- Dev tools access

### Available Make Targets

```bash
make dev          # Start dev server with hot reload
make build        # Clean production build
make clean        # Remove build artifacts
make install      # Build, kill running app, install to /Applications, launch
make uninstall    # Remove installed app
make bindings     # Regenerate frontend bindings after Go changes
make test         # Run e2e tests
make test-headed  # Run e2e tests with visible browser
make test-debug   # Run e2e tests with Playwright inspector
make screenshots  # Capture documentation screenshots via Playwright
make mp           # Build mp CLI for current platform
make mp-install   # Install mp to user bin dir (or GOBIN if set)
make mp-cross     # Cross-compile mp for all platforms
make help         # Show all targets
```

## Project Structure

```
mahpastes/
├── Makefile                 # Build, install, test targets
├── wails.json               # Wails build and dev configuration
├── go.mod                   # Go module definition
├── main.go                  # Entry point, Wails setup
├── app.go                   # Core application logic, 72+ API methods
├── database.go              # SQLite setup, schema, migrations
├── watcher.go               # Folder watching, file import
├── backup.go                # ZIP backup and restore
├── tag_hierarchy.go         # Tag tree helpers (parent, root, descendant checks)
├── clipboard_service.go     # Clipboard copy service (Wails-bound)
├── clipboard_darwin.go      # macOS clipboard via NSPasteboard (CGo)
├── clipboard_windows.go     # Windows clipboard via PowerShell
├── clipboard_other.go       # Unsupported platform stub
├── transfer_service.go      # Drag-out preparation and native drag
├── transfer_handler.go      # HTTP handler for transfer file serving
├── transfer_types.go        # Transfer system type definitions
├── app_transfer_helpers.go  # Bridge between App and TempClipStore
├── temp_clip_store.go       # Leased temp file management
├── native_drag_darwin.go    # macOS native drag via CGo
├── native_drag_windows.go   # Windows native drag via COM OLE DoDragDrop
├── native_drag_other.go     # Unsupported platform stub
├── open_darwin.go           # Open file with default app (macOS)
├── open_windows.go          # Open file with default app (Windows)
├── open_other.go            # Unsupported platform stub
├── plugin_service.go        # Plugin frontend API (Wails-bound)
├── plugins.go               # Plugin install/uninstall helpers
├── serve_manager.go         # Tag serve HTTP server lifecycle and routing
├── serve_json_api.go        # JSON API handler for served tags
├── serve_file_upload.go     # File upload handler for served tags
├── serve_service.go         # Tag serve Wails service (start/stop/status)
├── api_manager.go           # REST API HTTP server and route registration
├── api_service.go           # REST API Wails service (start/stop/keys)
├── plugin/                  # Lua plugin system
├── plugins/                 # Bundled plugin files (9 example plugins)
├── cmd/mp/                  # CLI binary (stateless REST API client)
├── examples/                # Example plugins and SPAs
├── docs/                    # Documentation source
├── frontend/
│   ├── index.html           # Main UI (single HTML file)
│   ├── js/                  # JavaScript modules (26 files incl. editor/)
│   │   ├── app.js           # Core app logic
│   │   ├── ui.js            # Gallery rendering
│   │   ├── editor/          # Modular editor tools (11 files)
│   │   └── ...              # Tags, plugins, watch, transfer, etc.
│   ├── css/                 # Styles (main.css, modals.css)
│   └── package.json         # Frontend deps (Tailwind)
├── e2e/                     # Playwright e2e tests
│   ├── tests/               # Test files by feature
│   ├── fixtures/            # Test fixtures (AppHelper)
│   └── helpers/             # Test utilities and selectors
└── build/                   # Build output
```

## Making Changes

### Backend Changes

1. Edit Go files (`*.go`)
2. Wails dev auto-rebuilds
3. Test your changes
4. If adding new API methods, they're automatically exposed

### Frontend Changes

1. Edit HTML/CSS/JS in `frontend/`
2. Changes hot-reload automatically
3. If editing Tailwind classes, ensure `npm run dev` is running

### Adding New API Methods

1. Add method to `app.go` (or `plugin_service.go` for plugin-related APIs):
   ```go
   func (a *App) MyNewMethod(param string) (string, error) {
       // Implementation
       return result, nil
   }
   ```

   **Note:** The App struct currently has 72+ methods and all bind correctly. Multiple services (`PluginService`, `ClipboardService`, `TransferService`, `ServeService`, `APIService`) exist as separate structs to stay under the Wails ~49 method binding limit (see comment in `main.go`), though the limit may not be enforced given the App struct's method count. They also serve as an organizational boundary, grouping related functionality into dedicated files.

2. Regenerate bindings: `make bindings`

3. Call from JavaScript:
   ```javascript
   const result = await window.go.main.App.MyNewMethod("param");

   // For service-specific methods:
   const plugins = await window.go.main.PluginService.GetPlugins();
   await window.go.main.ServeService.StartServing(tagID, port, false, "none");
   ```

## Code Style

### Go

- Follow standard Go conventions
- Use `gofmt` for formatting
- Handle errors explicitly
- Add comments for exported functions

```go
// GetClipData retrieves full clip data by ID.
// Returns an error if the clip is not found.
func (a *App) GetClipData(id int64) (*ClipData, error) {
    // ...
}
```

### JavaScript

- Use async/await for promises
- Cache DOM queries
- Use meaningful variable names
- Add comments for complex logic

```javascript
// Load and render clips for the current view mode
async function loadClips() {
    const clips = await getClips(isViewingArchive);
    renderClips(clips);
}
```

### CSS

- Prefer Tailwind utility classes
- Custom CSS only when necessary
- Keep specificity low
- Mobile-responsive where applicable

## Testing Changes

### E2E Tests (Required)

All changes must pass e2e tests:

```bash
make test              # Run all tests
make test-headed       # Run with browser visible (for debugging)
make test-debug        # Debug mode with Playwright inspector
```

Tests are in `e2e/tests/` organized by feature. Add tests for any new functionality.

### Manual Testing Checklist

Before submitting:

- [ ] Paste text content
- [ ] Paste images
- [ ] Drag and drop files
- [ ] Archive/unarchive clips
- [ ] Set and cancel expiration
- [ ] Bulk operations (select, delete, archive)
- [ ] Image editor (all tools, undo/redo)
- [ ] Text editor
- [ ] Watch folders (add, pause, remove)
- [ ] Lightbox navigation
- [ ] Search filtering
- [ ] Tag operations (create, assign, filter, hidden tags)
- [ ] Drag-out transfer (drag clip to Finder/Slack/Mail on macOS)
- [ ] Copy File to clipboard (via card menu)
- [ ] Copy Contents to clipboard (via card menu)
- [ ] Plugin management
- [ ] Backup and restore

### Cross-Platform

If possible, test on multiple platforms:
- macOS (primary development platform)
- Windows
- Linux

## Submitting Changes

### Fork and Branch

1. Fork the repository
2. Create a feature branch:
   ```bash
   git checkout -b feature/my-new-feature
   ```

### Commit Messages

Write clear, concise commit messages:

```
Add watch folder filter validation

- Validate regex patterns before saving
- Show error message for invalid patterns
- Prevent saving invalid configurations
```

### Pull Request

1. Push your branch:
   ```bash
   git push origin feature/my-new-feature
   ```

2. Open a Pull Request on GitHub

3. Include in description:
   - What the change does
   - Why it's needed
   - How to test it
   - Screenshots for UI changes

### PR Checklist

- [ ] Code follows project style
- [ ] Changes tested locally
- [ ] No console errors or warnings
- [ ] Documentation updated if needed

## Bug Reports

When reporting bugs, include:

1. **Environment**: OS, version, how installed
2. **Steps to reproduce**: Numbered steps
3. **Expected behavior**: What should happen
4. **Actual behavior**: What does happen
5. **Screenshots/logs**: If applicable

## Feature Requests

For new features:

1. **Problem**: What problem does this solve?
2. **Solution**: What should the feature do?
3. **Alternatives**: Other ways to solve the problem?
4. **Additional context**: Mockups, examples, etc.

## Getting Help

- Check existing [issues](https://github.com/egeozcan/mahpastes/issues)
- Read the documentation
- Ask in issue comments

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
