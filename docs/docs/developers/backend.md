---
sidebar_position: 3
---

# Backend Architecture

The backend is written in Go using the Wails framework. It handles data storage, clipboard operations, and file system watching.

## File Structure

```
├── main.go          Entry point, Wails configuration
├── app.go           Core application logic, exposed APIs
├── database.go      SQLite setup and migrations
├── watcher.go       File system watching
├── go.mod           Go module definition
└── go.sum           Dependency checksums
```

## Core Components

### main.go — Entry Point

Initializes and runs the Wails application:

```go
func main() {
    app := NewApp()

    err := wails.Run(&options.App{
        Title:  "mahpastes",
        Width:  1280,
        Height: 800,
        // ... configuration
        Bind: []interface{}{app},
        OnStartup: app.startup,
        OnShutdown: app.shutdown,
    })
}
```

Key configuration:
- Window size and minimum dimensions
- Drag and drop enabled
- Frameless window (macOS)
- Asset embedding

### app.go — Application Logic

The main `App` struct and all exposed methods:

```go
type App struct {
    ctx            context.Context
    db             *sql.DB
    tempDir        string
    mu             sync.Mutex
    watcherManager *WatcherManager
}
```

**Lifecycle methods:**
```go
func (a *App) startup(ctx context.Context)   // Initialize on start
func (a *App) shutdown(ctx context.Context)  // Cleanup on exit
```

**Clip operations:**
```go
func (a *App) GetClips(archived bool) ([]ClipPreview, error)
func (a *App) GetClipData(id int64) (*ClipData, error)
func (a *App) UploadFiles(files []FileData, expMins int) error
func (a *App) DeleteClip(id int64) error
func (a *App) ToggleArchive(id int64) error
```

**Watch folder operations:**
```go
func (a *App) GetWatchedFolders() ([]WatchedFolder, error)
func (a *App) AddWatchedFolder(config WatchedFolderConfig) error
func (a *App) RemoveWatchedFolder(id int64) error
```

### database.go — SQLite Management

Database initialization and migrations:

```go
func initDB() (*sql.DB, error) {
    // Get platform-specific data directory
    dataDir, err := getDataDir()

    // Open SQLite database
    db, err := sql.Open("sqlite3", dbPath)

    // Enable WAL mode
    db.Exec("PRAGMA journal_mode=WAL")

    // Create/migrate tables
    db.Exec(createTableSQL)

    return db, nil
}
```

**Cleanup job:**
```go
func startCleanupJob(db *sql.DB) {
    ticker := time.NewTicker(1 * time.Minute)
    go func() {
        for range ticker.C {
            db.Exec("DELETE FROM clips WHERE expires_at <= CURRENT_TIMESTAMP")
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
    ID          int64      `json:"id"`
    ContentType string     `json:"content_type"`
    Filename    string     `json:"filename"`
    CreatedAt   time.Time  `json:"created_at"`
    ExpiresAt   *time.Time `json:"expires_at"`
    Preview     string     `json:"preview"`    // First 500 chars for text
    IsArchived  bool       `json:"is_archived"`
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
// Encoding for response
if !strings.HasPrefix(contentType, "text/") {
    clip.Data = base64.StdEncoding.EncodeToString(data)
}

// Decoding from request
data, err := base64.StdEncoding.DecodeString(file.Data)
```

### Content Type Detection

Automatic detection for text content:

```go
func (a *App) UploadFiles(files []FileData, expMins int) error {
    for _, file := range files {
        contentType := file.ContentType

        if contentType == "text/plain" || contentType == "" {
            textData := string(data)
            if strings.HasPrefix(textData, "<!DOCTYPE html") {
                contentType = "text/html"
            } else if isJSON(textData) {
                contentType = "application/json"
            }
        }
        // Insert with detected type
    }
}
```

## Concurrency

### Mutex Usage

Protect shared state with mutexes:

```go
func (a *App) CreateTempFile(id int64) (string, error) {
    // ...
    a.mu.Lock()
    tempFilePath := filepath.Join(a.tempDir, safeName)
    a.mu.Unlock()
    // ...
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
// Emit import notification
runtime.EventsEmit(a.ctx, "watch:import", filename)

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
    github.com/wailsapp/wails/v2 v2.11.0
    github.com/mattn/go-sqlite3 v1.14.22
    github.com/fsnotify/fsnotify v1.7.0
    golang.design/x/clipboard v0.7.0
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

Currently no automated tests. Manual testing covers:

1. Paste various content types
2. Drag and drop files
3. Archive/unarchive operations
4. Expiration and cleanup
5. Watch folder imports
6. Image editing
7. Cross-platform behavior
