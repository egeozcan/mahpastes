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

**Note:** When `tagIDs` are provided, the filter automatically expands to include all descendant subtags. Filtering by a parent tag returns clips tagged with that tag or any of its children. Use `GetClipsDirect` if you need exact (non-expanded) tag matching.

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
func (a *App) UploadFiles(files []FileData, expirationMinutes int, autoTagID int64) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `files` | []FileData | Array of file data |
| `expirationMinutes` | int | Minutes until auto-delete (0 = never) |
| `autoTagID` | int64 | Tag ID to auto-assign to each clip (0 = no auto-tag). Used by folder mode to tag uploads into the current folder |

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
await UploadFiles([fileData], 0, 0);  // No expiration, no auto-tag
await UploadFiles([fileData], 30, 0); // Expires in 30 min, no auto-tag
await UploadFiles([fileData], 0, 5);  // No expiration, auto-tag with tag ID 5
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

### RenameClip

Rename a clip's filename.

```go
func (a *App) RenameClip(id int64, newFilename string) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `id` | int64 | Clip ID |
| `newFilename` | string | New filename for the clip |

---

### UpdateClipData

Replace the content of an existing clip.

```go
func (a *App) UpdateClipData(id int64, contentType string, base64Data string, filename string) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `id` | int64 | Clip ID |
| `contentType` | string | MIME type for the new content |
| `base64Data` | string | New content as a base64-encoded string |
| `filename` | string | New filename for the clip |

---

### OpenClipWithDefaultApp

Open a clip with the system's default application for its content type.

```go
func (a *App) OpenClipWithDefaultApp(id int64) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `id` | int64 | Clip ID |

Creates a temp file from the clip and opens it with the OS default handler (e.g., Preview for images on macOS).

---

### OpenClipWithApp

Open a clip with a specific application.

```go
func (a *App) OpenClipWithApp(id int64, appPath string) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `id` | int64 | Clip ID |
| `appPath` | string | Absolute path to the application (e.g., `/Applications/Photoshop.app`) |

---

### ChooseApplication

Open a macOS file dialog to pick an application.

```go
func (a *App) ChooseApplication() (string, error)
```

**Returns:** The absolute path to the selected `.app` bundle, or an empty string if cancelled.

:::note
macOS only. Returns an error on other platforms.
:::

---

### FindClipsByFilenameAndTag

Search for clips matching any of the given filenames within a specific tag.

```go
func (a *App) FindClipsByFilenameAndTag(filenames []string, tagID int64) ([]ClipMatch, error)
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `filenames` | []string | Filenames to match |
| `tagID` | int64 | Tag ID to scope the search. When 0, matches untagged clips only |

**Returns:** Clips that match both the filename and tag.

**ClipMatch structure:**
```go
type ClipMatch struct {
    ID          int64  `json:"id"`
    Filename    string `json:"filename"`
    ContentHash string `json:"content_hash"`
}
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

Merge the duplicates of a specific clip, keeping it and removing the rest.

```go
func (a *App) MergeDuplicates(clipID int64) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `clipID` | int64 | The clip to keep |

Finds all clips sharing the same content hash as `clipID`, merges their tags onto it (INSERT OR IGNORE), deletes the duplicates, and bumps the survivor's `created_at` to now.

---

### DeduplicateAll

Merge all duplicate groups at once.

```go
func (a *App) DeduplicateAll() (int, error)
```

**Returns:** The total number of duplicate clips removed, and an error if any.

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

### UpdateWatchedFolderPartial

Partially update a watch folder — only the fields present in the map are changed.

```go
func (a *App) UpdateWatchedFolderPartial(id int64, fields map[string]json.RawMessage) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `id` | int64 | Watch folder ID |
| `fields` | map[string]json.RawMessage | Key-value pairs to update. Valid keys match `WatchedFolderConfig` fields. Values are raw JSON to be unmarshalled into the correct types |

**JavaScript usage:**
```javascript
// Pause a folder without touching other settings
await UpdateWatchedFolderPartial(1, { is_paused: true });

// Change filter mode and regex at once
await UpdateWatchedFolderPartial(1, {
  filter_mode: 'regex',
  filter_regex: '.*\\.png$',
});
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

Create a new tag. If the name contains `/` separators, intermediate parent tags are auto-created.

```go
func (a *App) CreateTag(name string) (*Tag, error)
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `name` | string | Tag name (must be unique). Use `/` to create subtags (e.g., `"work/client1/projectABC"`) |

**Returns:**
| Type | Description |
|------|-------------|
| `*Tag` | The created (leaf) tag |
| `error` | Error if the leaf tag name already exists |

When given a path like `work/client1/projectABC`, this method creates `work`, `work/client1`, and `work/client1/projectABC` (skipping any that already exist). Intermediate tags inherit the color of the root parent.

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

Removes the tag and all clip associations. Deleting a parent tag does **not** delete its children -- they remain as orphaned subtags.

**Internal:** The `deleteTagIfOrphaned` helper (used by auto-cleanup) never deletes subtags (tags with `/` in their name). For flat tags, it checks whether the tag has child tags before removing it. A flat tag with zero clips but one or more children is kept alive.

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

Update a tag's name and color. If the tag has descendants, renaming cascades: all children have their name prefix updated to match.

```go
func (a *App) UpdateTag(id int64, name string, color string) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `id` | int64 | Tag ID |
| `name` | string | New tag name |
| `color` | string | Hex color code (e.g., "#ef4444") |

For example, renaming tag `work` to `office` also renames `work/client1` to `office/client1` and `work/client1/projectABC` to `office/client1/projectABC`.

---

### AddTagToClip

Add a tag to a clip. **Enforces tree exclusivity:** any existing tags from the same root tree are automatically removed before adding the new tag. For example, adding `work/client2` to a clip that has `work/client1` removes `work/client1`. Tags from different trees are unaffected.

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

Add a tag to multiple clips at once. Enforces tree exclusivity per clip (same as `AddTagToClip`).

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

### GetChildTags

Get the immediate child tags of a given tag.

```go
func (a *App) GetChildTags(tagID int64) ([]Tag, error)
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `tagID` | int64 | Parent tag ID |

**Returns:** Tags whose name is one level below the given tag. For example, if tag ID 5 is `work`, this returns `work/client1` and `work/client2` but not `work/client1/projectABC`.

---

### GetTopLevelTags

Get all tags that have no parent (no `/` in their name).

```go
func (a *App) GetTopLevelTags() ([]Tag, error)
```

**Returns:** Tags whose names contain no `/` separator.

---

### GetDescendantClipCount

Get the total number of clips tagged with a tag or any of its descendants.

```go
func (a *App) GetDescendantClipCount(tagID int64) (int, error)
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `tagID` | int64 | Root tag ID |

**Returns:** The count of distinct clips tagged with this tag or any subtag beneath it.

---

### GetClipsDirect

Retrieve clips without expanding tag filters to include descendants. Unlike `GetClips`, filtering by a tag ID returns only clips directly tagged with that exact tag.

```go
func (a *App) GetClipsDirect(archived bool, tagIDs []int64, hiddenTagIDs []int64, sortField string, sortDir string) ([]ClipPreview, error)
```

Parameters and return type are identical to `GetClips`. The only difference is that tag filters are not expanded to include subtags.

---

### GetFolderClips

Retrieve clips tagged with a specific tag but **not** tagged with any descendant of that tag. Used by folder mode so clips appear only at the level of their most specific tag.

```go
func (a *App) GetFolderClips(archived bool, tagID int64, hiddenTagIDs []int64, sortField string, sortDir string) ([]ClipPreview, error)
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `archived` | bool | true for archived clips, false for active |
| `tagID` | int64 | The folder-level tag ID |
| `hiddenTagIDs` | []int64 | Tag IDs whose clips should be excluded |
| `sortField` | string | Sort field: `"date"`, `"name"`, `"size"`, or `"type"` |
| `sortDir` | string | Sort direction: `"asc"` or `"desc"` |

Due to tree exclusivity, a clip can only have one tag per tree (e.g., `work/client1` but not both `work` and `work/client1`). Clips appear only at the level of their assigned tag -- a clip tagged `work/client1` does not appear in the `work` folder.

---

### GetUntaggedClips

Retrieve clips that have no tags at all. Used by folder mode at root level to show only truly untagged clips alongside folder cards.

```go
func (a *App) GetUntaggedClips(archived bool, hiddenTagIDs []int64, sortField string, sortDir string) ([]ClipPreview, error)
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `archived` | bool | true for archived clips, false for active |
| `hiddenTagIDs` | []int64 | Tag IDs whose clips should be excluded |
| `sortField` | string | Sort field: `"date"`, `"name"`, `"size"`, or `"type"` |
| `sortDir` | string | Sort direction: `"asc"` or `"desc"` |

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

Plugin-related APIs are on the `PluginService` struct (accessed via `window.go.main.PluginService.*` in JavaScript).

### GetPlugins

```go
func (s *PluginService) GetPlugins() ([]PluginInfo, error)
```

### ImportPlugin

Opens a file dialog and returns a preview for review. The frontend should call `ConfirmPluginInstall(path)` after user approves.

```go
func (s *PluginService) ImportPlugin() (*PluginPreview, error)
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

### PreviewPluginFromURL

Fetch a plugin from a URL and return a preview without installing it.

```go
func (s *PluginService) PreviewPluginFromURL(rawURL string) (*PluginPreview, error)
```

**Returns:** A `PluginPreview` containing the plugin's manifest, permissions, and source. Pass the source URL to `ConfirmPluginInstall` to finalize installation.

### PreviewPluginFromPath

Read a plugin from a local file and return a preview without installing it.

```go
func (s *PluginService) PreviewPluginFromPath(path string) (*PluginPreview, error)
```

### ConfirmPluginInstall

Install a plugin after the user has reviewed its preview.

```go
func (s *PluginService) ConfirmPluginInstall(source string) (*PluginInfo, error)
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `source` | string | The plugin source — a URL (`http://` or `https://`) or a local file path |

**Returns:** The installed plugin's info.

### GetUpdateCheckInterval

Get the current plugin update check interval setting.

```go
func (s *PluginService) GetUpdateCheckInterval() (string, error)
```

**Returns:** The interval as a string (e.g., `"startup"`, `"6h"`, `"24h"`, `"disabled"`). Defaults to `"24h"`.

### SetUpdateCheckInterval

Set the automatic plugin update check interval.

```go
func (s *PluginService) SetUpdateCheckInterval(interval string) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `interval` | string | One of `"startup"`, `"6h"`, `"24h"`, or `"disabled"` |

### CheckForUpdates

Check all installed plugins for available updates.

```go
func (s *PluginService) CheckForUpdates() ([]*plugin.PluginUpdateInfo, error)
```

**Returns:** A list of plugins with pending updates, and an error if the check fails.

### UpdatePlugin

Fetch the latest version of a plugin from its source URL. If new permissions are required, returns a preview for re-review instead of applying the update.

```go
func (s *PluginService) UpdatePlugin(pluginID int64) (*UpdateResult, error)
```

**UpdateResult structure:**
```go
type UpdateResult struct {
    Success     bool           `json:"success"`
    NeedsReview bool           `json:"needs_review"`
    Preview     *PluginPreview `json:"preview,omitempty"`
    PluginInfo  *PluginInfo    `json:"plugin_info,omitempty"`
    Error       string         `json:"error,omitempty"`
}
```

### ConfirmPluginUpdate

Apply a previously downloaded plugin update after user review of new permissions.

```go
func (s *PluginService) ConfirmPluginUpdate(pluginID int64) (*PluginInfo, error)
```

**Returns:** The updated plugin's info.

---

## ClipboardService Operations

Clipboard operations are on the `ClipboardService` struct (accessed via `window.go.main.ClipboardService.*` in JavaScript).

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

## ServeService Operations

Tag HTTP server management, accessed via `window.go.main.ServeService.*` in JavaScript.

### StartServing

Start an HTTP server for a tag's clips.

```go
func (s *ServeService) StartServing(tagID int64, port int, bindAll bool, apiAccess string) (ServeInfo, error)
```

**Returns:** A `ServeInfo` describing the running server (tag ID, port, URL, etc.), and an error if the server could not be started.

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `tagID` | int64 | Tag whose clips to serve |
| `port` | int | TCP port to listen on |
| `bindAll` | bool | `true` to bind `0.0.0.0` (LAN-accessible), `false` for `127.0.0.1` only |
| `apiAccess` | string | JSON API access level: `"none"`, `"read"`, or `"readwrite"` |

---

### StopServing

Stop the HTTP server for a tag.

```go
func (s *ServeService) StopServing(tagID int64) error
```

---

### GetServeStatus

Get the status of all running tag servers.

```go
func (s *ServeService) GetServeStatus() []ServeInfo
```

**Returns:** A list of `ServeInfo` entries, one per running tag server, containing the tag ID, port, bind address, and API access level.

---

### GetRandomPort

Get an available random TCP port for a new tag server.

```go
func (s *ServeService) GetRandomPort() (int, error)
```

**Returns:** An unused port number.

---

## APIService Operations

REST API server management, accessed via `window.go.main.APIService.*` in JavaScript. The REST API exposes clip, tag, and other operations over HTTP for use by the `mp` CLI and external tools.

### StartAPI

Start the REST API HTTP server.

```go
func (s *APIService) StartAPI(port int, bindAll bool) (APIStatus, error)
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `port` | int | TCP port to listen on |
| `bindAll` | bool | `true` to bind `0.0.0.0` (LAN-accessible), `false` for `127.0.0.1` only |

**Returns:** An `APIStatus` describing the running server, and an error if the server could not be started.

---

### StopAPI

Stop the REST API HTTP server.

```go
func (s *APIService) StopAPI() error
```

---

### GetAPIStatus

Get the current status of the REST API server.

```go
func (s *APIService) GetAPIStatus() APIStatus
```

**Returns:** An `APIStatus` value indicating whether the server is running, its port, and address.

---

### CreateAPIKey

Create a new API key for authenticating REST API requests.

```go
func (s *APIService) CreateAPIKey(name string, role string, scopedTagID int64) (*APIKeyCreateResult, error)
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `name` | string | Human-readable label for the key |
| `role` | string | Permission role (e.g., `"admin"`, `"read"`) |
| `scopedTagID` | int64 | Tag ID to scope access to (0 for unrestricted) |

**APIKeyCreateResult structure:**
```go
type APIKeyCreateResult struct {
    Key  string     `json:"key"`   // The raw API key (prefixed mp_), shown only once
    Info APIKeyInfo `json:"info"`  // Key metadata
}
```

**Returns:** The result containing the raw API key string (prefixed `mp_`) and key metadata. This is the only time the full key is returned — store it securely.

:::tip
Pass the key as `MP_API_KEY` environment variable or as a `Bearer` token in the `Authorization` header.
:::

---

### ListAPIKeys

List all API keys (without revealing the raw key values).

```go
func (s *APIService) ListAPIKeys() ([]APIKeyInfo, error)
```

**Returns:** Metadata for each key — ID, name, role, scoped tag, creation date, and a masked key prefix.

---

### RevokeAPIKey

Permanently revoke an API key.

```go
func (s *APIService) RevokeAPIKey(id int64) error
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `id` | int64 | API key ID (from `ListAPIKeys`) |

---

## Serve File Upload API

The tag serve system exposes a file upload endpoint for HTML apps hosted within a served tag. This is not a Wails binding — it's an HTTP endpoint on the tag's serve port.

### POST /_api/_upload

Upload a file as a new clip, tagged to the served tag or a subtag.

**Requirements:** `apiAccess` must be `"readwrite"`, valid `_mp_serve_key` cookie.

**Request:** `multipart/form-data` with fields:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `file` | binary | Yes | File data (max 10 MB) |
| `tag` | string | No | Relative subtag path under served tag |
| `content_type` | string | No | Override auto-detected content type |

**Response (201 Created):**
```json
{
  "id": 42,
  "filename": "photo.png",
  "content_type": "image/png",
  "tag": "myapp/exports",
  "tag_id": 7
}
```

**Errors:**

| Status | Body | Condition |
|--------|------|-----------|
| 400 | `{"error": "..."}` | Missing file, invalid tag path |
| 401 | `{"error": "..."}` | No auth cookie |
| 403 | `{"error": "..."}` | Read-only mode |
| 413 | `{"error": "..."}` | File > 10 MB |

**JavaScript usage:**
```javascript
const formData = new FormData();
formData.append('file', blob, 'screenshot.png');
formData.append('tag', 'renders');  // optional subtag

const res = await fetch('/_api/_upload', {
  method: 'POST',
  credentials: 'include',
  body: formData,
});
const { id, filename, content_type, tag, tag_id } = await res.json();
```

---

## Events

Events emitted from Go to JavaScript:

### watch:import

Emitted when a file is imported from a watched folder.

```go
runtime.EventsEmit(ctx, "watch:import", clip)
```

**Payload:** `ClipPreview` - The full clip preview data for the imported clip (see `ClipPreview` structure under `GetClips`).

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

EventsOn('watch:import', (clip) => {
    console.log(`Imported: ${clip.filename} (id=${clip.id})`);
});

EventsOn('watch:error', (data) => {
    console.error(`Error importing ${data.file}: ${data.error}`);
});
```
