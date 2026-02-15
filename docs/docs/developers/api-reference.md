---
sidebar_position: 5
---

# API Reference

Complete reference for all Go functions exposed to the frontend via Wails bindings.

## Clip Operations

### GetClips

Retrieve a list of clips for the gallery.

```go
func (a *App) GetClips(archived bool, tagIDs []int64, hiddenTagIDs []int64) ([]ClipPreview, error)
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `archived` | bool | true for archived clips, false for active |
| `tagIDs` | []int64 | Tag IDs to filter by (AND logic - clip must have ALL tags). Empty for no filter |
| `hiddenTagIDs` | []int64 | Tag IDs whose clips should be excluded. Empty for no hiding |

**Returns:**
| Type | Description |
|------|-------------|
| `[]ClipPreview` | Array of clip previews |
| `error` | Error if query fails |

**ClipPreview structure:**
```go
type ClipPreview struct {
    ID          int64      `json:"id"`
    ContentType string     `json:"content_type"`
    Filename    string     `json:"filename"`
    CreatedAt   time.Time  `json:"created_at"`
    ExpiresAt   *time.Time `json:"expires_at"`
    Preview     string     `json:"preview"`      // Text preview (500 chars max)
    IsArchived  bool       `json:"is_archived"`
    Tags        []Tag      `json:"tags"`          // Tags assigned to this clip
    Size        int64      `json:"size"`          // Clip size in bytes
}
```

**JavaScript usage:**
```javascript
const clips = await GetClips(false, [], []);      // Active clips, no filters
const archived = await GetClips(true, [], []);     // Archived clips
const tagged = await GetClips(false, [1, 2], []);  // Clips with tags 1 AND 2
const filtered = await GetClips(false, [], [3]);   // Active clips, hide tag 3
```

---

### GetClipData

Retrieve full clip data by ID.

```go
func (a *App) GetClipData(id int64) (*ClipData, error)
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `id` | int64 | Clip ID |

**Returns:**
| Type | Description |
|------|-------------|
| `*ClipData` | Full clip data |
| `error` | Error if not found |

**ClipData structure:**
```go
type ClipData struct {
    ID          int64  `json:"id"`
    ContentType string `json:"content_type"`
    Data        string `json:"data"`     // Base64 for binary, raw for text
    Filename    string `json:"filename"`
}
```

**Note:** Binary data is base64 encoded. Text content is returned as-is.

---

### UploadFiles

Upload one or more files.

```go
func (a *App) UploadFiles(files []FileData, expirationMinutes int) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `files` | []FileData | Array of file data |
| `expirationMinutes` | int | Minutes until auto-delete (0 = never) |

**FileData structure:**
```go
type FileData struct {
    Name        string `json:"name"`
    ContentType string `json:"content_type"`
    Data        string `json:"data"`     // Base64 encoded
}
```

**JavaScript usage:**
```javascript
const fileData = {
    name: 'screenshot.png',
    content_type: 'image/png',
    data: base64Data
};
await UploadFiles([fileData], 0); // No expiration
await UploadFiles([fileData], 30); // Expires in 30 min
```

---

### UploadFileAndGetID

Upload a single file and return its clip ID.

```go
func (a *App) UploadFileAndGetID(file FileData) (int64, error)
```

**Returns:** The ID of the created clip.

Used internally by watch folder imports.

---

### DeleteClip

Delete a clip by ID.

```go
func (a *App) DeleteClip(id int64) error
```

---

### ToggleArchive

Toggle the archived status of a clip.

```go
func (a *App) ToggleArchive(id int64) error
```

---

### CancelExpiration

Remove the expiration from a clip.

```go
func (a *App) CancelExpiration(id int64) error
```

---

### BulkDelete

Delete multiple clips at once.

```go
func (a *App) BulkDelete(ids []int64) error
```

---

### BulkArchive

Toggle archive status for multiple clips.

```go
func (a *App) BulkArchive(ids []int64) error
```

---

### BulkDownloadToFile

Create a ZIP archive and save via dialog.

```go
func (a *App) BulkDownloadToFile(ids []int64) error
```

Opens a native save dialog. Returns nil if user cancels.

---

## Clipboard Operations

### CopyToClipboard

Copy text to system clipboard.

```go
func (a *App) CopyToClipboard(text string) error
```

---

### GetClipboardText

Get text from system clipboard.

```go
func (a *App) GetClipboardText() (string, error)
```

---

### GetClipboardImage

Get image from system clipboard.

```go
func (a *App) GetClipboardImage() (string, string, error)
```

**Returns:**
| Position | Type | Description |
|----------|------|-------------|
| 1 | string | Base64 encoded image data |
| 2 | string | Content type (always "image/png") |
| 3 | error | Error if no image in clipboard |

---

## File Operations

### CreateTempFile

Create a temporary file from a clip and return its path.

```go
func (a *App) CreateTempFile(id int64) (string, error)
```

**Returns:** Absolute path to the temporary file.

Temp files are stored in `{dataDir}/clip_temp_files/` and cleaned up on app exit.

---

### DeleteAllTempFiles

Delete all temporary files.

```go
func (a *App) DeleteAllTempFiles() error
```

---

### SaveClipToFile

Save a clip to file using native save dialog.

```go
func (a *App) SaveClipToFile(id int64) error
```

---

### ReadFileFromPath

Read a file from disk (for drag-drop).

```go
func (a *App) ReadFileFromPath(path string) (*FileData, error)
```

---

### IsDirectory

Check if a path is a directory.

```go
func (a *App) IsDirectory(path string) bool
```

---

### SelectFolder

Open a native folder picker dialog.

```go
func (a *App) SelectFolder() (string, error)
```

---

## Watch Folder Operations

### GetWatchedFolders

Get all configured watch folders.

```go
func (a *App) GetWatchedFolders() ([]WatchedFolder, error)
```

**WatchedFolder structure:**
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

---

### GetWatchedFolderByID

Get a single watch folder by ID.

```go
func (a *App) GetWatchedFolderByID(id int64) (*WatchedFolder, error)
```

---

### AddWatchedFolder

Add a new folder to watch.

```go
func (a *App) AddWatchedFolder(config WatchedFolderConfig) (*WatchedFolder, error)
```

**WatchedFolderConfig structure:**
```go
type WatchedFolderConfig struct {
    Path            string   `json:"path"`
    FilterMode      string   `json:"filter_mode"`
    FilterPresets   []string `json:"filter_presets"`
    FilterRegex     string   `json:"filter_regex"`
    ProcessExisting bool     `json:"process_existing"`
    AutoArchive     bool     `json:"auto_archive"`
    AutoTagID       *int64   `json:"auto_tag_id"`
}
```

---

### UpdateWatchedFolder

Update a watch folder configuration.

```go
func (a *App) UpdateWatchedFolder(id int64, config WatchedFolderConfig) error
```

---

### RemoveWatchedFolder

Remove a watch folder.

```go
func (a *App) RemoveWatchedFolder(id int64) error
```

---

### RefreshWatches

Reload watcher configuration from database.

```go
func (a *App) RefreshWatches() error
```

---

### ProcessExistingFilesInFolder

Import existing files in a watched folder.

```go
func (a *App) ProcessExistingFilesInFolder(folderID int64) error
```

---

### SetFolderPaused

Set pause state for a specific folder.

```go
func (a *App) SetFolderPaused(id int64, paused bool) error
```

---

### GetGlobalWatchPaused

Get global watch pause state.

```go
func (a *App) GetGlobalWatchPaused() bool
```

---

### SetGlobalWatchPaused

Set global watch pause state.

```go
func (a *App) SetGlobalWatchPaused(paused bool) error
```

---

### GetWatchStatus

Get current watch status.

```go
func (a *App) GetWatchStatus() WatchStatus
```

**WatchStatus structure:**
```go
type WatchStatus struct {
    GlobalPaused bool `json:"global_paused"`
    ActiveCount  int  `json:"active_count"`
    TotalCount   int  `json:"total_count"`
    IsWatching   bool `json:"is_watching"`
}
```

---

## Tag Operations

### CreateTag

Create a new tag.

```go
func (a *App) CreateTag(name string) (*Tag, error)
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `name` | string | Tag name (must be unique) |

**Returns:**
| Type | Description |
|------|-------------|
| `*Tag` | The created tag |
| `error` | Error if name already exists |

**Tag structure:**
```go
type Tag struct {
    ID    int64  `json:"id"`
    Name  string `json:"name"`
    Color string `json:"color"`
    Count int    `json:"count"` // Number of clips using this tag
}
```

---

### DeleteTag

Delete a tag by ID.

```go
func (a *App) DeleteTag(id int64) error
```

Removes the tag and all clip associations.

---

### GetTags

Get all tags with usage counts.

```go
func (a *App) GetTags() ([]Tag, error)
```

**Returns:**
| Type | Description |
|------|-------------|
| `[]Tag` | Array of tags with clip counts |
| `error` | Error if query fails |

The `Count` field on each Tag contains the number of clips using that tag.

---

### UpdateTag

Update a tag's name and color.

```go
func (a *App) UpdateTag(id int64, name string, color string) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `id` | int64 | Tag ID |
| `name` | string | New tag name |
| `color` | string | Hex color code (e.g., "#ef4444") |

---

### AddTagToClip

Add a tag to a clip.

```go
func (a *App) AddTagToClip(clipID int64, tagID int64) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `clipID` | int64 | Clip ID |
| `tagID` | int64 | Tag ID to add |

---

### RemoveTagFromClip

Remove a tag from a clip.

```go
func (a *App) RemoveTagFromClip(clipID int64, tagID int64) error
```

---

### GetClipTags

Get all tags for a specific clip.

```go
func (a *App) GetClipTags(clipID int64) ([]Tag, error)
```

**Returns:**
| Type | Description |
|------|-------------|
| `[]Tag` | Array of tags assigned to the clip |
| `error` | Error if query fails |

---

### BulkAddTag

Add a tag to multiple clips at once.

```go
func (a *App) BulkAddTag(clipIDs []int64, tagID int64) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `clipIDs` | []int64 | Array of clip IDs |
| `tagID` | int64 | Tag ID to add |

---

### BulkRemoveTag

Remove a tag from multiple clips at once.

```go
func (a *App) BulkRemoveTag(clipIDs []int64, tagID int64) error
```

---

### GetHiddenTags

Get the list of hidden tag IDs.

```go
func (a *App) GetHiddenTags() ([]int64, error)
```

**Returns:** Array of tag IDs that are currently hidden from the gallery.

---

### SetHiddenTags

Save the list of hidden tag IDs.

```go
func (a *App) SetHiddenTags(ids []int64) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `ids` | []int64 | Array of tag IDs to hide |

---

## Settings Operations

### GetSetting

Retrieve a setting value by key.

```go
func (a *App) GetSetting(key string) (string, error)
```

---

### SetSetting

Store a setting value (insert or update).

```go
func (a *App) SetSetting(key string, value string) error
```

---

## Backup Operations

### ShowCreateBackupDialog

Open a save dialog and create a backup ZIP.

```go
func (a *App) ShowCreateBackupDialog() (string, error)
```

**Returns:** The path where the backup was saved, or empty string if cancelled.

---

### ShowRestoreBackupDialog

Open a file picker and validate the selected backup.

```go
func (a *App) ShowRestoreBackupDialog() (*BackupManifest, string, error)
```

**Returns:** The backup manifest, file path, and any error. Returns nil/empty if user cancels.

---

### ConfirmRestoreBackup

Perform the actual restore after user confirmation.

```go
func (a *App) ConfirmRestoreBackup(backupPath string) error
```

---

## Plugin Service Operations

Plugin-related APIs are on the `PluginService` struct (accessed via `window.go.main.PluginService.*` in JavaScript) due to Wails method binding limits.

### GetPlugins

```go
func (s *PluginService) GetPlugins() ([]PluginInfo, error)
```

### ImportPlugin

Opens a file dialog to import a `.lua` plugin file.

```go
func (s *PluginService) ImportPlugin() (*PluginInfo, error)
```

### ImportPluginFromPath

Import a plugin from a specific file path.

```go
func (s *PluginService) ImportPluginFromPath(path string) (*PluginInfo, error)
```

### EnablePlugin / DisablePlugin / RemovePlugin

```go
func (s *PluginService) EnablePlugin(id int64) error
func (s *PluginService) DisablePlugin(id int64) error
func (s *PluginService) RemovePlugin(id int64) error
```

### GetPluginPermissions / RevokePluginPermission

```go
func (s *PluginService) GetPluginPermissions(id int64) ([]map[string]string, error)
func (s *PluginService) RevokePluginPermission(pluginID int64, permType, path string) error
```

### GetPluginStorage / SetPluginStorage / GetAllPluginStorage

```go
func (s *PluginService) GetPluginStorage(pluginID int64, key string) (string, error)
func (s *PluginService) SetPluginStorage(pluginID int64, key, value string) error
func (s *PluginService) GetAllPluginStorage(pluginID int64) (map[string]string, error)
```

### GetPluginUIActions

Returns all UI actions (lightbox buttons, card actions) from enabled plugins.

```go
func (s *PluginService) GetPluginUIActions() (*UIActionsResponse, error)
```

**Response structure:**
```go
type UIActionsResponse struct {
    LightboxButtons []PluginUIAction `json:"lightbox_buttons"`
    CardActions     []PluginUIAction `json:"card_actions"`
}

type PluginUIAction struct {
    PluginID   int64       `json:"plugin_id"`
    PluginName string      `json:"plugin_name"`
    ID         string      `json:"id"`
    Label      string      `json:"label"`
    Icon       string      `json:"icon,omitempty"`
    Async      bool        `json:"async,omitempty"`
    Options    []FormField `json:"options,omitempty"`
    FileTypes  []string    `json:"file_types,omitempty"` // MIME filters, e.g. image/* or text/plain
    MaxSize    int64       `json:"max_size,omitempty"`   // Max clip size in bytes (0 = no limit)
}
```

`file_types` supports exact MIME matches and wildcard prefixes (for example `image/*`).  
`max_size` is compared against clip byte size and hides actions for larger clips.

### ExecutePluginAction

Calls a plugin's `on_ui_action` handler.

```go
func (s *PluginService) ExecutePluginAction(pluginID int64, actionID string, clipIDs []int64, options map[string]interface{}) (*ActionResult, error)
```

---

## Events

Events emitted from Go to JavaScript:

### watch:import

Emitted when a file is imported from a watched folder.

```go
runtime.EventsEmit(ctx, "watch:import", filename)
```

**Payload:** `string` - The filename that was imported.

### watch:error

Emitted when a watch folder import fails.

```go
runtime.EventsEmit(ctx, "watch:error", map[string]string{
    "file":  filename,
    "error": errorMessage,
})
```

**Payload:** `object` with `file` and `error` properties.

**JavaScript listener:**
```javascript
import { EventsOn } from '../wailsjs/runtime/runtime';

EventsOn('watch:import', (filename) => {
    console.log(`Imported: ${filename}`);
});

EventsOn('watch:error', (data) => {
    console.error(`Error importing ${data.file}: ${data.error}`);
});
```
