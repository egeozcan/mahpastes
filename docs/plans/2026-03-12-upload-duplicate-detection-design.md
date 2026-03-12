# Upload Duplicate Detection Design

## Problem

When files are uploaded (dropped, pasted, or picked) into a tag/folder that already contains clips with the same filename, the app silently creates duplicates. Users need a way to detect and resolve these conflicts before upload.

## Scope

All upload paths across all modes — not just folder drops in folder mode.

## Detection Logic

Before uploading, check if any incoming filenames match existing clips in the target tag:

1. **No match** → upload normally
2. **Filename match, identical content** (same SHA-256 hash) → silently skip
3. **Filename match, different content** → add to conflict list for user resolution

## Backend Changes

### New method: `FindClipsByFilenameAndTag`

```go
type ClipMatch struct {
    ID          int64  `json:"id"`
    Filename    string `json:"filename"`
    ContentHash string `json:"content_hash"`
}

func (a *App) FindClipsByFilenameAndTag(filenames []string, tagID int64) ([]ClipMatch, error)
```

- When `tagID > 0`: queries clips that have that exact tag and matching filenames
- When `tagID == 0`: queries clips with zero tags (untagged) and matching filenames
- Returns `{ID, Filename, ContentHash}` for each match

### Existing method reused

`UpdateClipData(id, contentType, base64Data, filename)` already replaces clip content in-place, recalculating the content hash. This is the "overwrite" action.

## Conflict Dialog UX

When conflicts with different content exist, show a single modal with all conflicting filenames and three global actions:

- **Overwrite** — replace content of each existing clip via `UpdateClipData`
- **Keep Both** — upload conflicting files as new clips (current behavior)
- **Skip** — don't upload the conflicting files

The dialog lists all conflicting filenames. Styled as a three-button variant of the existing `showConfirmDialog` pattern, matching the stone-based design system.

## Frontend Changes

### Shared conflict resolution function

```javascript
async function checkAndResolveConflicts(fileDataArray, tagID)
```

1. Extracts filenames from `fileDataArray`
2. Calls `FindClipsByFilenameAndTag(filenames, tagID)` on backend
3. Computes SHA-256 of incoming file data, compares against returned `contentHash`
4. Silently removes identical-content matches from upload list
5. If different-content conflicts remain, shows conflict dialog
6. Returns `{ toUpload: FileData[], toOverwrite: {clipID, fileData}[], skippedCount: number }`

### Three integration points

1. **`upload()` in `wails-api.js`** — single/multi file drops, paste, file picker. Calls `checkAndResolveConflicts` before `UploadFiles`.

2. **`handleFolderDrop()` in `app.js`** — folder drops. Calls `checkAndResolveConflicts` per tag group before each `UploadFiles` call.

3. **`handleText()` in `app.js`** — text paste (creates `pasted_text.txt`). Calls `checkAndResolveConflicts` before upload.

### Content hashing in frontend

Use Web Crypto API (`crypto.subtle.digest('SHA-256', data)`) to compute hashes of incoming files for comparison against existing clip hashes. This avoids uploading data just to check for duplicates.

## Dialog HTML

Add a three-button conflict dialog to `index.html`, styled identically to the existing confirm dialog but with Overwrite / Keep Both / Skip buttons. Reuse the same overlay/animation patterns.

## Edge Cases

- **Empty conflict list after hash comparison**: All matches are identical content → silently skip all, no dialog shown, toast says "N identical files skipped"
- **Mixed batch**: Some files conflict, some don't. Non-conflicting files upload immediately. Conflicting files wait for dialog resolution.
- **Folder drop with multiple tag groups**: Each group checks independently against its target tag
- **No target tag (root upload in non-folder-mode)**: Check against untagged clips only
