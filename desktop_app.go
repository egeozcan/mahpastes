package main

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"os"
	"time"

	coreapp "go-clipboard/internal/app"
	"go-clipboard/internal/wailsbridge"

	"golang.design/x/clipboard"
)

type desktopCore interface {
	AddTagToClip(clipID, tagID int64) error
	AddWatchedFolder(config coreapp.WatchedFolderConfig) (*coreapp.WatchedFolder, error)
	BackupInspect(backupPath string) (*coreapp.BackupInspection, error)
	BulkAddTag(clipIDs []int64, tagID int64) error
	BulkArchive(ids []int64) error
	BulkCancelExpiration(ids []int64) error
	BulkDelete(ids []int64) error
	BulkRemoveTag(clipIDs []int64, tagID int64) error
	BulkSetExpiration(ids []int64, minutes int) error
	BulkUnarchive(ids []int64) error
	CancelExpiration(id int64) error
	CleanOrphanDBRows() (coreapp.OrphanReport, error)
	CleanStaleFiles() (coreapp.CleanStaleResult, error)
	CompactDatabase() (coreapp.CompactResult, error)
	ConfirmRestoreBackup(backupPath, identityPolicy string) error
	CreateBackup(destPath string) error
	CreateTag(name string) (*coreapp.Tag, error)
	CreateTempFile(id int64) (string, error)
	DeduplicateAll() (int, error)
	DeleteAllTempFiles() error
	DeleteClip(id int64) error
	DeleteClipMetadata(clipID int64, key string) error
	DeleteTag(id int64) error
	EndImportSession() error
	FindClipsByFilenameAndTag(filenames []string, tagID int64) ([]coreapp.ClipMatch, error)
	GetChildTags(tagID int64) ([]coreapp.Tag, error)
	GetClipData(id int64) (*coreapp.ClipData, error)
	GetClipMetadata(clipID int64) (map[string]string, error)
	GetClipTags(clipID int64) ([]coreapp.Tag, error)
	GetClips(archived bool, tagIDs []int64, hiddenTagIDs []int64, sortField string, sortDir string) ([]coreapp.ClipPreview, error)
	GetClipsDirect(archived bool, tagIDs []int64, hiddenTagIDs []int64, sortField string, sortDir string) ([]coreapp.ClipPreview, error)
	GetDatabaseSize() (int64, error)
	GetDescendantClipCount(tagID int64, archived bool) (int, error)
	GetDuplicateGroups() ([]coreapp.DuplicateGroup, error)
	GetFolderClips(archived bool, tagID int64, sortField string, sortDir string) ([]coreapp.ClipPreview, error)
	GetGlobalWatchPaused() bool
	GetHiddenClipInfo(archived bool, tagIDs []int64, hiddenTagIDs []int64) (coreapp.HiddenClipInfo, error)
	GetHiddenTags() ([]int64, error)
	GetImageDiff(clipIdA, clipIdB int64, threshold int) (*coreapp.DiffResult, error)
	GetOrphanDBRows() (coreapp.OrphanReport, error)
	GetRemovableEmptyTags() ([]coreapp.Tag, error)
	GetSetting(key string) (string, error)
	GetStaleFiles() ([]coreapp.StaleFile, error)
	GetTags() ([]coreapp.Tag, error)
	GetTopLevelTags() ([]coreapp.Tag, error)
	GetUntaggedClips(archived bool, hiddenTagIDs []int64, sortField string, sortDir string) ([]coreapp.ClipPreview, error)
	GetWatchStatus() coreapp.WatchStatus
	GetWatchedFolderByID(id int64) (*coreapp.WatchedFolder, error)
	GetWatchedFolders() ([]coreapp.WatchedFolder, error)
	ImportApply(decisions []coreapp.ImportDecision) (*coreapp.ImportApplySummary, error)
	ImportInspect(relPath string) (*coreapp.ImportInspection, error)
	MergeDuplicates(clipID int64) error
	MergeTag(sourceID, destID int64) error
	PreviewMergeTag(sourceID, destID int64) (coreapp.MergeTagPreview, error)
	ProbeFilePaths(paths []string) ([]coreapp.PathProbe, error)
	ProcessExistingFilesInFolder(folderID int64) error
	ReadFileFromPath(path string) (*coreapp.FileData, error)
	RefreshWatches() error
	RemoveEmptyTags() (int, error)
	RemoveTagFromClip(clipID, tagID int64) error
	RemoveWatchedFolder(id int64) error
	RenameClip(id int64, newFilename string) error
	RestoreBackup(backupPath, identityPolicy string) error
	SetClipMetadata(clipID int64, key string, value string) error
	SetClipMetadataBulk(clipID int64, metadata map[string]string) error
	SetExpiration(id int64, minutes int) error
	SetFolderPaused(id int64, paused bool) error
	SetGlobalWatchPaused(paused bool) error
	SetHiddenTags(ids []int64) error
	SetSetting(key string, value string) error
	StartImportSession(root string, recursive bool) (*coreapp.ImportScanResult, error)
	ToggleArchive(id int64) error
	UpdateClipData(id int64, contentType string, base64Data string, filename string) error
	UpdateTag(id int64, name, color string) error
	UpdateWatchedFolder(id int64, config coreapp.WatchedFolderConfig) error
	UpdateWatchedFolderPartial(id int64, fields map[string]json.RawMessage) error
	UploadFileAndGetID(file coreapp.FileData) (int64, error)
	UploadFiles(files []coreapp.FileData, expirationMinutes int, autoTagID int64) error
}

// App exposes the stable Wails-bound desktop API and delegates shared behavior
// to the core application without promoting server-only internals.
type App struct {
	desktopCore
	core   *coreapp.App
	bridge *wailsbridge.Bridge
}

func NewApp() *App {
	core := coreapp.NewApp()
	return &App{desktopCore: core, core: core}
}

func (d *App) setBridge(b *wailsbridge.Bridge) {
	d.bridge = b
	d.core.SetBridge(b)
}

// BulkDownloadToFile creates a ZIP archive and saves it using native save dialog.
func (d *App) BulkDownloadToFile(ids []int64) error {
	if len(ids) == 0 {
		return fmt.Errorf("no clips selected")
	}

	db := d.core.DB()
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("SELECT id, content_type, filename, data FROM clips WHERE id IN (%s)", stringsJoin(placeholders, ","))
	rows, err := db.Query(query, args...)
	if err != nil {
		return fmt.Errorf("failed to query clips: %w", err)
	}
	defer rows.Close()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for rows.Next() {
		var id int64
		var contentType string
		var filename sql.NullString
		var data []byte

		if err := rows.Scan(&id, &contentType, &filename, &data); err != nil {
			log.Printf("Failed to scan clip for download: %v\n", err)
			continue
		}

		name := filename.String
		if name == "" {
			name = fmt.Sprintf("clip_%d", id)
			exts, _ := mime.ExtensionsByType(contentType)
			if len(exts) > 0 {
				name += exts[0]
			}
		} else {
			name = fmt.Sprintf("%d_%s", id, name)
		}

		fw, err := zw.Create(name)
		if err != nil {
			log.Printf("Failed to create zip entry for %s: %v\n", name, err)
			continue
		}
		if _, err := fw.Write(data); err != nil {
			log.Printf("Failed to write data to zip entry for %s: %v\n", name, err)
			continue
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("failed to close zip: %w", err)
	}

	defaultFilename := fmt.Sprintf("clips_%s.zip", time.Now().Format("20060102150405"))
	savePath, err := d.bridge.SaveFile(wailsbridge.FileDialogOptions{
		DefaultFilename: defaultFilename,
		Title:           "Save Clips Archive",
		Filters: []wailsbridge.FileFilter{
			{DisplayName: "ZIP Archives", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to show save dialog: %w", err)
	}
	if savePath == "" {
		return nil
	}
	if err := os.WriteFile(savePath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

func (d *App) OpenClipWithDefaultApp(id int64) error {
	item, err := d.core.PrepareClipTransferItem(id, "open_default")
	if err != nil {
		return err
	}
	return openFileWithDefaultApp(item.AbsPath)
}

func (d *App) OpenClipWithApp(id int64, appPath string) error {
	if appPath == "" {
		return fmt.Errorf("application path is required")
	}
	item, err := d.core.PrepareClipTransferItem(id, "open_with")
	if err != nil {
		return err
	}
	return openFileWithApp(item.AbsPath, appPath)
}

func (d *App) ChooseApplication() (string, error) {
	return chooseApplicationDialog(d.bridge)
}

func (d *App) CopyToClipboard(text string) error {
	clipboard.Write(clipboard.FmtText, []byte(text))
	return nil
}

func (d *App) GetClipboardText() (string, error) {
	data := clipboard.Read(clipboard.FmtText)
	return string(data), nil
}

func (d *App) GetClipboardImage() (string, string, error) {
	data := clipboard.Read(clipboard.FmtImage)
	if len(data) == 0 {
		return "", "", fmt.Errorf("no image in clipboard")
	}
	return base64.StdEncoding.EncodeToString(data), "image/png", nil
}

func (d *App) SaveClipToFile(id int64) error {
	var data []byte
	var filename sql.NullString
	var contentType string

	row := d.core.DB().QueryRow("SELECT data, filename, content_type FROM clips WHERE id = ?", id)
	if err := row.Scan(&data, &filename, &contentType); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("clip not found")
		}
		return fmt.Errorf("failed to get clip: %w", err)
	}

	defaultFilename := filename.String
	if defaultFilename == "" {
		defaultFilename = fmt.Sprintf("clip_%d", id)
		exts, _ := mime.ExtensionsByType(contentType)
		if len(exts) > 0 {
			defaultFilename += exts[0]
		}
	}

	savePath, err := d.bridge.SaveFile(wailsbridge.FileDialogOptions{
		DefaultFilename: defaultFilename,
		Title:           "Save Clip",
	})
	if err != nil {
		return fmt.Errorf("failed to show save dialog: %w", err)
	}
	if savePath == "" {
		return nil
	}
	if err := os.WriteFile(savePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

func (d *App) ShowCreateBackupDialog() (string, error) {
	defaultFilename := fmt.Sprintf("mahpastes-backup-%s.zip", time.Now().Format("2006-01-02"))

	savePath, err := d.bridge.SaveFile(wailsbridge.FileDialogOptions{
		DefaultFilename: defaultFilename,
		Title:           "Create Backup",
		Filters: []wailsbridge.FileFilter{
			{DisplayName: "ZIP Archives", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to show save dialog: %w", err)
	}
	if savePath == "" {
		return "", nil
	}
	if err := d.core.CreateBackup(savePath); err != nil {
		return "", err
	}
	return savePath, nil
}

func (d *App) ShowRestoreBackupDialog() (*coreapp.BackupManifest, string, error) {
	openPath, err := d.bridge.OpenFile(wailsbridge.FileDialogOptions{
		Title: "Select Backup to Restore",
		Filters: []wailsbridge.FileFilter{
			{DisplayName: "ZIP Archives", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to show open dialog: %w", err)
	}
	if openPath == "" {
		return nil, "", nil
	}

	manifest, err := coreapp.ValidateBackup(openPath)
	if err != nil {
		return nil, "", err
	}
	return manifest, openPath, nil
}

func (d *App) SelectFolder() (string, error) {
	path, err := d.bridge.OpenDirectory(wailsbridge.FileDialogOptions{
		Title: "Select Folder to Watch",
	})
	if err != nil {
		return "", fmt.Errorf("failed to open folder dialog: %w", err)
	}
	return path, nil
}

// BeginImportSession opens the folder picker and scans the chosen folder.
// Returns (nil, nil) when the user cancels.
//
// The picker result never round-trips through JS, so the normal path never
// accepts a caller-supplied root. StartImportSession stays bound separately
// because Playwright cannot dismiss a native folder dialog and the e2e suite
// has to drive the wizard against a temp directory.
func (d *App) BeginImportSession(recursive bool) (*coreapp.ImportScanResult, error) {
	path, err := d.bridge.OpenDirectory(wailsbridge.FileDialogOptions{
		Title: "Select Folder to Import",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open folder dialog: %w", err)
	}
	if path == "" {
		return nil, nil
	}
	// Approve through the concrete core, not the embedded interface: this
	// method is not bound to JS, so the approval cannot be forged from the
	// frontend. StartImportSession then accepts this root (and only this root).
	d.core.ApproveImportRoot(path)
	return d.core.StartImportSession(path, recursive)
}

func (d *App) IsDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func stringsJoin(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, part := range parts[1:] {
		out += sep + part
	}
	return out
}
