---
sidebar_position: 5
---

# API Reference

Complete reference for all Go functions exposed to the frontend via Wails bindings.

## Clip Operations

### GetClips

Retrieve a list of clips for the gallery.

```go
func (a *App) GetClips(archived bool, tagIDs []int64, hiddenTagIDs []int64, sortField string, sortDir string) ([]ClipPreview, error)
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `archived` | bool | true for archived clips, false for active |
| `tagIDs` | []int64 | Tag IDs to filter by (AND logic - clip must have ALL tags). Empty for no filter |
| `hiddenTagIDs` | []int64 | Tag IDs whose clips should be excluded. Empty for no hiding |
| `sortField` | string | Sort field: `"date"`, `"name"`, `"size"`, or `"type"` |
| `sortDir` | string | Sort direction: `"asc"` or `"desc"` |

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
const clips = await GetClips(false, [], [], 'date', 'desc');      // Active clips, newest first
const archived = await GetClips(true, [], [], 'date', 'desc');     // Archived clips
const tagged = await GetClips(false, [1, 2], [], 'name', 'asc');   // Clips with tags 1 AND 2, sorted by name
const filtered = await GetClips(false, [], [3], 'date', 'desc');   // Active clips, hide tag 3
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

### SetExpiration

Set an expiration time on a clip.

```go
func (a *App) SetExpiration(id int64, minutes int) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `id` | int64 | Clip ID |
| `minutes` | int | Minutes from now until auto-delete |

---

### BulkSetExpiration

Set expiration on multiple clips.

```go
func (a *App) BulkSetExpiration(ids []int64, minutes int) error
```

---

### BulkCancelExpiration

Remove expiration from multiple clips.

```go
func (a *App) BulkCancelExpiration(ids []int64) error
```

---

### GetClipMetadata

Get all metadata key-value pairs for a clip.

```go
func (a *App) GetClipMetadata(id int64) (map[string]string, error)
```

---

### SetClipMetadata

Set a single metadata key-value pair on a clip.

```go
func (a *App) SetClipMetadata(id int64, key string, value string) error
```

Limits: key max 256 chars, value max 4096 chars, max 50 pairs per clip.

---

### DeleteClipMetadata

Delete a single metadata key from a clip.

```go
func (a *App) DeleteClipMetadata(id int64, key string) error
```

---

### SetClipMetadataBulk

Atomically replace all metadata on a clip.

```go
func (a *App) SetClipMetadataBulk(id int64, metadata map[string]string) error
```

---

### GetDuplicateGroups

Get groups of clips that share the same content hash.

```go
func (a *App) GetDuplicateGroups() ([]DuplicateGroup, error)
```

Returns groups where two or more clips have the same SHA-256 content hash.

---

### MergeDuplicates

Merge a group of duplicate clips, keeping the oldest.

```go
func (a *App) MergeDuplicates(ids []int64) error
```

Keeps the clip with the lowest ID, merges tags from all duplicates onto it (INSERT OR IGNORE), deletes the rest, and bumps the survivor's `created_at` to now.

---

### DeduplicateAll

Merge all duplicate groups at once.

```go
func (a *App) DeduplicateAll() error
```

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

## Image Operations

### GetImageDiff

Compare two image clips and return a visual diff plus similarity score.

```go
func (a *App) GetImageDiff(clipIdA, clipIdB int64, threshold int) (*DiffResult, error)
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `clipIdA` | int64 | First image clip ID |
| `clipIdB` | int64 | Second image clip ID |
| `threshold` | int | Sensitivity (10-50, default 30). Lower = more sensitive |

**Returns:**
| Type | Description |
|------|-------------|
| `*DiffResult` | Diff result with similarity score and visual diff |
| `error` | Error if either clip is not found or not an image |

**DiffResult structure:**
```go
type DiffResult struct {
    Similarity  float64 `json:"similarity"`
    DiffDataUrl string  `json:"diff_data_url"` // data:image/png;base64,...
}
```

`Similarity` ranges from 0.0 to 1.0 where 1.0 means identical images. `DiffDataUrl` contains a data URL with the visual diff as a PNG.

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

## ClipboardService Operations

Clipboard operations are on the `ClipboardService` struct (accessed via `window.go.main.ClipboardService.*` in JavaScript), separate from `App` to stay within Wails method binding limits.

### CopyFileToClipboard

Copy a clip as a file reference to the system clipboard.

```go
func (s *ClipboardService) CopyFileToClipboard(id int64) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `id` | int64 | Clip ID |

Creates a temp file from the clip data and places a file reference on the system clipboard. The receiving app gets the file as if it were copied from Finder.

---

### BulkCopyFilesToClipboard

Copy multiple clips as file references to the system clipboard.

```go
func (s *ClipboardService) BulkCopyFilesToClipboard(ids []int64) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `ids` | []int64 | Array of clip IDs |

---

### CopyClipContents

Copy the raw content of a clip to the system clipboard.

```go
func (s *ClipboardService) CopyClipContents(id int64) error
```

For text clips, copies the text directly. For image clips, copies as PNG image data.

---

## TransferService Operations

Transfer operations handle dragging clips out of mahpastes into other apps. Accessed via `window.go.main.TransferService.*` in JavaScript.

### GetTransferCapabilities

Returns platform-specific capability flags for drag and clipboard operations.

```go
func (s *TransferService) GetTransferCapabilities() TransferCapabilities
```

**Returns:** A capabilities value (not pointer, no error) indicating which drag/clipboard features are available on the current platform.

---

### PrepareClipForTransfer

Create or refresh a temp file for a clip, preparing it for drag-out.

```go
func (s *TransferService) PrepareClipForTransfer(req PrepareTransferRequest) (*PreparedTransferItem, error)
```

Returns the temp file path and metadata needed to start a native drag.

---

### GetExistingPreparedClipForTransfer

Check if a temp file already exists for a clip without creating a new one.

```go
func (s *TransferService) GetExistingPreparedClipForTransfer(req PrepareTransferRequest) (*PreparedTransferItem, error)
```

Returns the existing prepared transfer if available, or nil if the clip hasn't been prepared yet.

---

### StartNativeDragOut

Initiate a native OS drag operation for a prepared clip.

```go
func (s *TransferService) StartNativeDragOut(req StartNativeDragRequest) (bool, error)
```

**Parameters:** `StartNativeDragRequest` with `ClipID` and `AbsPath` fields.

**Returns:** `bool` indicating whether the drag was accepted, and an error if the operation failed.

On macOS, this triggers a native pasteboard drag with the file URI. The user sees the file "leave" the app and can drop it into Finder, Mail, Slack, or other apps.

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
