---
sidebar_position: 3
---

# Backend Architecture

The backend is written in Go using the Wails framework. It handles data storage, clipboard operations, and file system watching.

## File Structure

```
├── main.go                  Entry point, Wails configuration
├── app.go                   Core application logic, exposed APIs
├── database.go              SQLite setup and migrations
├── watcher.go               File system watching
├── backup.go                ZIP backup and restore
├── plugin_service.go        Plugin frontend API (Wails-bound)
├── plugins.go               Plugin install/uninstall helpers
├── serve_manager.go         Tag serve HTTP server lifecycle and routing
├── serve_json_api.go        JSON API handler for served tags (/_api prefix)
├── serve_file_upload.go     File upload handler for served tags (/_api/_upload)
├── serve_service.go         Tag serve Wails service (start/stop/status)
├── api_manager.go           REST API HTTP server and route registration
├── api_service.go           REST API Wails service (start/stop/keys)
├── clipboard_service.go     Clipboard copy service (Wails-bound)
├── clipboard_darwin.go      macOS clipboard implementation
├── clipboard_windows.go     Windows clipboard implementation
├── clipboard_other.go       Fallback clipboard implementation
├── transfer_service.go      Drag-out preparation and native drag initiation
├── transfer_handler.go      HTTP handler for transfer file serving
├── transfer_types.go        Transfer system type definitions
├── app_transfer_helpers.go  Bridge between App and TempClipStore
├── temp_clip_store.go       Leased temp file management
├── native_drag_darwin.go    macOS native drag implementation
├── native_drag_windows.go   Windows native drag implementation
├── native_drag_other.go     Fallback native drag stub
├── open_darwin.go           macOS open-with-default-app
├── open_windows.go          Windows open-with-default-app
├── open_other.go            Fallback open-with-default-app stub
├── tag_hierarchy.go         Tag tree helpers (parent, root, ancestor, descendant checks)
├── plugin/                  Lua plugin system
│   ├── manager.go           Plugin lifecycle, event dispatch
│   ├── manifest.go          Manifest parsing, validation
│   ├── sandbox.go           Sandboxed Lua execution
│   ├── scheduler.go         Scheduled/recurring plugin tasks
│   ├── fetch.go             Plugin source fetching (URL/file)
│   ├── semver.go            Semantic versioning helpers
│   ├── update_checker.go    Automatic plugin update checking
│   ├── permission_diff.go   Permission change detection across versions
│   └── api_*.go             Lua APIs (clips, tags, storage, http, fs, utils, task, toast, image, modal, metadata)
├── go.mod                   Go module definition
└── go.sum                   Dependency checksums
```

## Core Components

### main.go — Entry Point

Initializes and runs the Wails application:

```go
func main() {
    app := NewApp()
    pluginService := NewPluginService(app)
    clipboardService := NewClipboardService(app)
    transferService := NewTransferService(app)
    serveService := NewServeService(app)
    apiService := NewAPIService(app)

    err := wails.Run(&options.App{
        Title:  "mahpastes",
        Width:  1280,
        Height: 800,
        // ... configuration
        Bind: []interface{}{
            app, pluginService, clipboardService,
            transferService, serveService, apiService,
        },
        OnStartup: app.startup,
        OnShutdown: app.shutdown,
    })
}
```

:::note
Multiple service structs exist for organizational clarity. The `App` struct alone has 66+ bound methods — Wails handles this fine. Services group related functionality (plugins, clipboard, transfers, tag serving, REST API) into their own files and structs.
:::

Key configuration:
- Window size and minimum dimensions
- Drag and drop enabled
- Frameless window (macOS)
- Asset embedding

### app.go — Application Logic

The main `App` struct and all exposed methods:

```go
type App struct {
    ctx              context.Context
    db               *sql.DB
    tempDir          string
    tempStore        *TempClipStore
    transferHandler  *TransferFileHandler
    mu               sync.Mutex
    watcherManager   *WatcherManager
    serveManager     *ServeManager
    apiManager       *APIManager
    pluginManager    *plugin.Manager
    clipboardService *ClipboardService
}
```

**Lifecycle methods:**
```go
func (a *App) startup(ctx context.Context)   // Initialize on start
func (a *App) shutdown(ctx context.Context)  // Cleanup on exit
```

**Clip operations:**
```go
func (a *App) GetClips(archived bool, tagIDs []int64, hiddenTagIDs []int64, sortField string, sortDir string) ([]ClipPreview, error)
func (a *App) GetClipData(id int64) (*ClipData, error)
func (a *App) UploadFiles(files []FileData, expirationMinutes int, autoTagID int64) error
func (a *App) DeleteClip(id int64) error
func (a *App) ToggleArchive(id int64) error
```

**Tag operations** (AddTagToClip and BulkAddTag enforce tree exclusivity -- adding a tag removes other tags from the same root tree):
```go
func (a *App) CreateTag(name string) (*Tag, error)
func (a *App) UpdateTag(id int64, name, color string) error
func (a *App) DeleteTag(id int64) error
func (a *App) GetTags() ([]Tag, error)
func (a *App) AddTagToClip(clipID, tagID int64) error       // enforces tree exclusivity
func (a *App) RemoveTagFromClip(clipID, tagID int64) error
func (a *App) BulkAddTag(clipIDs []int64, tagID int64) error // enforces tree exclusivity
func (a *App) BulkRemoveTag(clipIDs []int64, tagID int64) error
func (a *App) GetClipTags(clipID int64) ([]Tag, error)
func (a *App) GetHiddenTags() ([]int64, error)
func (a *App) SetHiddenTags(ids []int64) error
```

**Watch folder operations:**
```go
func (a *App) GetWatchedFolders() ([]WatchedFolder, error)
func (a *App) AddWatchedFolder(config WatchedFolderConfig) (*WatchedFolder, error)
func (a *App) RemoveWatchedFolder(id int64) error
```

**Backup operations:**
```go
func (a *App) ShowCreateBackupDialog() (string, error)
func (a *App) ShowRestoreBackupDialog() (*BackupManifest, string, error)
func (a *App) ConfirmRestoreBackup(backupPath string) error
```

### database.go — SQLite Management

Database initialization and migrations:

```go
func initDB() (*sql.DB, error) {
    // Get platform-specific data directory
    dataDir, err := getDataDir()

    // Open SQLite with pragmas in DSN for connection-pool safety
    dsn := dbPath +
        "?_pragma=busy_timeout%3D5000" +
        "&_pragma=journal_mode%3Dwal" +
        "&_pragma=foreign_keys%3Don"
    db, err := sql.Open("sqlite", dsn)

    // Create/migrate tables
    db.Exec(createTableSQL)

    return db, nil
}
```

Pragmas are set via the DSN string (not `db.Exec`) so they apply to all pooled connections.

**Cleanup job:**
```go
func startCleanupJob(db *sql.DB) {
    ticker := time.NewTicker(1 * time.Minute)
    go func() {
        for range ticker.C {
            db.Exec("DELETE FROM clips WHERE expires_at IS NOT NULL AND expires_at <= CURRENT_TIMESTAMP")
        }
    }()
}
```

### watcher.go — File System Watching

Manages folder watching using fsnotify:

```go
type WatcherManager struct {
    watcher       *fsnotify.Watcher
    app           *App
    activeWatches map[int64]string    // folderID -> path
    debounceMap   map[string]*time.Timer
    mu            sync.RWMutex
    running       bool
}
```

**Event handling:**
```go
func (w *WatcherManager) handleEvents() {
    for {
        select {
        case event := <-w.watcher.Events:
            if event.Op&(fsnotify.Create|fsnotify.Write) != 0 {
                w.debounceFile(event.Name)
            }
        case err := <-w.watcher.Errors:
            log.Printf("Watcher error: %v", err)
        }
    }
}
```

## Data Types

### ClipPreview

Lightweight clip data for gallery display:

```go
type ClipPreview struct {
    ID             int64      `json:"id"`
    ContentType    string     `json:"content_type"`
    Filename       string     `json:"filename"`
    CreatedAt      time.Time  `json:"created_at"`
    ExpiresAt      *time.Time `json:"expires_at"`
    Preview        string     `json:"preview"`         // First 500 chars for text
    IsArchived     bool       `json:"is_archived"`
    Tags           []Tag      `json:"tags"`
    Size           int64      `json:"size"`            // Clip size in bytes
    DuplicateCount int        `json:"duplicate_count"`
}
```

### ClipData

Full clip data for retrieval:

```go
type ClipData struct {
    ID          int64  `json:"id"`
    ContentType string `json:"content_type"`
    Data        string `json:"data"`     // Base64 for binary, raw for text
    Filename    string `json:"filename"`
}
```

### FileData

Upload data format:

```go
type FileData struct {
    Name        string `json:"name"`
    ContentType string `json:"content_type"`
    Data        string `json:"data"`     // Base64 encoded
}
```

### WatchedFolder

Watch folder configuration:

```go
type WatchedFolder struct {
    ID              int64     `json:"id"`
    Path            string    `json:"path"`
    FilterMode      string    `json:"filter_mode"`
    FilterPresets   []string  `json:"filter_presets"`
    FilterRegex     string    `json:"filter_regex"`
    ProcessExisting bool      `json:"process_existing"`
    AutoArchive     bool      `json:"auto_archive"`
    AutoTagID       *int64    `json:"auto_tag_id"`
    IsPaused        bool      `json:"is_paused"`
    CreatedAt       time.Time `json:"created_at"`
    Exists          bool      `json:"exists"`
}
```

## API Patterns

### Error Handling

All exposed methods return errors that Wails propagates to JavaScript:

```go
func (a *App) GetClipData(id int64) (*ClipData, error) {
    row := a.db.QueryRow("SELECT ... WHERE id = ?", id)
    if err := row.Scan(...); err != nil {
        if err == sql.ErrNoRows {
            return nil, fmt.Errorf("clip not found")
        }
        return nil, fmt.Errorf("failed to get clip: %w", err)
    }
    return clip, nil
}
```

### Binary Data Handling

Binary content is base64 encoded for JSON transport:

```go
// Encoding for response — text and JSON are returned raw, everything else is base64
if strings.HasPrefix(contentType, "text/") || contentType == "application/json" {
    clip.Data = string(data)
} else {
    clip.Data = base64.StdEncoding.EncodeToString(data)
}

// Decoding from request
data, err := base64.StdEncoding.DecodeString(file.Data)
```

### Content Type Detection

Automatic detection for text content:

```go
func (a *App) UploadFiles(files []FileData, expirationMinutes int, autoTagID int64) error {
    for _, file := range files {
        contentType := file.ContentType

        if contentType == "text/plain" || contentType == "" {
            trimmedText := strings.TrimSpace(string(data))
            if strings.HasPrefix(trimmedText, "<!DOCTYPE html") {
                contentType = "text/html"
            } else if isJSON(trimmedText) {
                contentType = "application/json"
            }
        }
        // Insert with detected type
    }
}
```

## Concurrency

### Mutex Usage

Protect shared state with mutexes. For example, `CreateTempFile` delegates to `TempClipStore` which manages leased temporary files with its own synchronization:

```go
func (a *App) CreateTempFile(id int64) (string, error) {
    item, err := a.prepareClipTransferItem(id, "legacy_create_temp")
    if err != nil {
        return "", err
    }
    return item.AbsPath, nil
}
```

### Watcher Concurrency

The watcher uses RWMutex for reads vs writes:

```go
func (w *WatcherManager) processFile(filePath string) {
    w.mu.RLock()        // Read lock for map access
    var folderID int64
    for id, path := range w.activeWatches {
        if path == dir {
            folderID = id
            break
        }
    }
    w.mu.RUnlock()
    // ...
}
```

## Event Emission

Send events from Go to JavaScript:

```go
// Emit import notification (sends full ClipPreview for UI refresh)
runtime.EventsEmit(a.ctx, "watch:import", clip)

// Emit error
runtime.EventsEmit(a.ctx, "watch:error", map[string]string{
    "file":  filepath.Base(filePath),
    "error": errMsg,
})
```

## Platform-Specific Code

### Data Directory

```go
func getDataDir() (string, error) {
    switch runtime.GOOS {
    case "darwin":
        return filepath.Join(homeDir, "Library", "Application Support", "mahpastes"), nil
    case "windows":
        return filepath.Join(os.Getenv("APPDATA"), "mahpastes"), nil
    default: // Linux
        return filepath.Join(homeDir, ".config", "mahpastes"), nil
    }
}
```

## Dependencies

```go
require (
    github.com/fsnotify/fsnotify v1.9.0
    github.com/wailsapp/wails/v2 v2.11.0
    github.com/yuin/gopher-lua v1.1.1
    golang.design/x/clipboard v0.7.0
    golang.org/x/image v0.35.0
    modernc.org/sqlite v1.45.0
)
```

## Building

### Development

```bash
wails dev
```

Hot-reloads frontend, rebuilds backend on changes.

### Production

```bash
# Current platform
wails build

# Cross-platform
wails build -platform darwin/universal
wails build -platform windows/amd64
wails build -platform linux/amd64
```

## Testing

The project uses Playwright for end-to-end testing:

```bash
cd e2e && npm test              # Run all tests
cd e2e && npm run test:headed   # Run with browser visible
cd e2e && npm run test:debug    # Debug mode with Playwright inspector
```

Tests are organized by feature in `e2e/tests/` covering clips, tags, search, images, plugins, watch folders, backup, bulk operations, and edge cases.
