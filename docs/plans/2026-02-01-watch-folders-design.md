# Watch Folders Feature Design

## Overview

Add watched folder support to mahpastes. Users can designate folders to be monitored for new files, which are automatically imported into the app and removed from the file system.

## Requirements

- Watch folders for new files using file system events (immediate, via fsnotify)
- Per-folder configuration for:
  - Process existing files when watch is added
  - File type filtering (presets or custom regex)
  - Auto-archive imported files
  - Pause/resume watching
- Global pause to stop all watching at once
- Top-level only (no recursive subfolder watching)
- UI: new Watch button in header with active indicator, dedicated watch view

## Data Model

### New table: `watched_folders`

```sql
CREATE TABLE watched_folders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT NOT NULL UNIQUE,
    filter_mode TEXT NOT NULL DEFAULT 'all',  -- 'all', 'presets', 'custom'
    filter_presets TEXT,                       -- JSON array: ["images", "videos"]
    filter_regex TEXT,                         -- regex pattern for custom mode
    process_existing INTEGER DEFAULT 0,
    auto_archive INTEGER DEFAULT 0,
    is_paused INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
)
```

### Settings table addition

```sql
-- Global pause state
INSERT INTO settings (key, value) VALUES ('global_watch_paused', 'false');
```

### Preset definitions

- **Images:** jpg, jpeg, png, gif, webp, heic, bmp, tiff, svg
- **Documents:** pdf, doc, docx, txt, md, rtf, odt, xls, xlsx
- **Videos:** mp4, mov, avi, mkv, webm, m4v, wmv

Filter behavior:
- "All" is exclusive (selecting it disables other presets)
- Other presets can be combined (e.g., Images + Videos)
- Custom regex can be used for advanced filtering

## Backend Architecture

### New file: `watcher.go`

```go
type WatcherManager struct {
    watcher     *fsnotify.Watcher
    app         *App
    activeWatch map[int64]string  // folderID -> path
    mu          sync.RWMutex
}

func NewWatcherManager(app *App) *WatcherManager
func (w *WatcherManager) Start() error
func (w *WatcherManager) Stop()
func (w *WatcherManager) AddFolder(id int64, path string) error
func (w *WatcherManager) RemoveFolder(id int64) error
func (w *WatcherManager) handleEvents()  // goroutine processing fsnotify events
```

### New methods in `app.go`

```go
// Folder management
func (a *App) GetWatchedFolders() []WatchedFolder
func (a *App) AddWatchedFolder(path string, config FolderConfig) error
func (a *App) UpdateWatchedFolder(id int64, config FolderConfig) error
func (a *App) RemoveWatchedFolder(id int64) error

// Pause controls
func (a *App) GetGlobalWatchPaused() bool
func (a *App) SetGlobalWatchPaused(paused bool) error
func (a *App) SetFolderPaused(id int64, paused bool) error

// Internal
func (a *App) processWatchedFile(folderID int64, filePath string) error
func (a *App) processExistingFiles(folderID int64) error
```

### Startup sequence

1. Load watched folders from DB
2. Load global pause state from settings
3. If not globally paused, start watching non-paused folders
4. Process existing files for folders with `process_existing=1` on first run

## File Processing Flow

1. fsnotify detects create/write event
2. Debounce 500ms (wait for file to finish writing)
3. Check global pause → if paused, ignore
4. Check folder pause → if paused, ignore
5. Skip hidden files (starting with `.`)
6. Check file against folder's filter:
   - `all`: accept any file
   - `presets`: check extension against selected preset extensions
   - `custom`: match filename against regex
7. Read file content into memory
8. Call existing `UploadFiles()` logic with auto_archive flag
9. On success: delete original file from watched folder
10. On failure: show toast notification, leave file in place

## Frontend UI

### Header changes

- New "Watch" button before Archive button
- Eye icon
- Green dot badge (top-right corner) when watching is active:
  - Visible when: not globally paused AND at least one folder not paused

### Watch view (replaces gallery)

```
┌─────────────────────────────────────────────────────────────┐
│  [Global Pause Toggle]  Watching 3 folders                  │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ ~/Screenshots                             [⏸] [✕]  │   │
│  │ Filter: Images  •  Auto-archive: On                 │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ ~/Downloads/imports                       [▶] [✕]  │   │
│  │ Filter: All  •  Auto-archive: Off         (paused)  │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  ┌ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┐   │
│    Drop folder here or [+ Add Folder]                      │
│  └ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┘   │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Add/Edit folder modal

- Folder path (read-only after selection)
- Filter presets: checkbox group (All, Images, Documents, Videos)
  - "All" is exclusive
  - Others can be multi-selected
- Custom regex field (optional, for advanced filtering)
- Toggle: "Process existing files now"
- Toggle: "Auto-archive imported files"
- Helper text: "Files in subfolders are not watched"
- Save / Cancel buttons

### New file: `frontend/js/watch.js`

- Watch view rendering and folder card creation
- Add/edit modal logic
- Global pause toggle
- Dot badge visibility updates
- API calls to Go backend

## Edge Cases

### Folder path handling

- Store absolute paths in DB
- On startup, verify each folder still exists
- If folder missing: show "Folder not found" badge in watch view
- User can remove or update the path

### App lifecycle

- On startup: restore watchers for non-paused folders (respecting global pause)
- On shutdown: stop all watchers gracefully

### Pause behavior

- Global and per-folder pause are independent
- Global pause stops all watching
- When global pause is lifted, folders resume their individual states
- A folder that was individually paused stays paused

### File handling

- Hidden files (starting with `.`): skip automatically
- Filename conflicts: append timestamp suffix (existing behavior)
- Failed imports: show toast, leave file in place

## Implementation Order

1. Database schema (new table + settings)
2. Go watcher manager (fsnotify integration)
3. Go API methods for folder CRUD and pause controls
4. Frontend watch view and folder cards
5. Add/edit folder modal
6. Header button with dot badge
7. File processing with filters
8. Process existing files flow
9. Error handling and toasts
