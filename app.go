package main

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.design/x/clipboard"
)

// App struct holds the application state
type App struct {
	ctx            context.Context
	db             *sql.DB
	tempDir        string
	mu             sync.Mutex
	watcherManager *WatcherManager
	taskManager    *TaskManager
}

// NewApp creates a new App instance
func NewApp() *App {
	return &App{}
}

// emitWatchError sends an error event to the frontend
func (a *App) emitWatchError(filePath string, errMsg string) {
	runtime.EventsEmit(a.ctx, "watch:error", map[string]string{
		"file":  filepath.Base(filePath),
		"error": errMsg,
	})
}

// emitWatchImport sends an import event to the frontend
func (a *App) emitWatchImport(filename string) {
	runtime.EventsEmit(a.ctx, "watch:import", filename)
}

// RefreshWatches reloads the watcher configuration
func (a *App) RefreshWatches() error {
	if a.watcherManager != nil {
		return a.watcherManager.refreshWatches()
	}
	return nil
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Initialize database
	db, err := initDB()
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	a.db = db

	// Start cleanup job for expired clips
	startCleanupJob(a.db)

	// Initialize temp directory
	if err := a.initTempDir(); err != nil {
		log.Printf("Warning: Failed to initialize temp directory: %v", err)
	}

	// Initialize clipboard
	if err := clipboard.Init(); err != nil {
		log.Printf("Warning: Failed to initialize clipboard: %v", err)
	}

	// Initialize watcher manager
	wm, err := NewWatcherManager(a)
	if err != nil {
		log.Printf("Warning: Failed to initialize watcher manager: %v", err)
	} else {
		a.watcherManager = wm
		if err := wm.Start(); err != nil {
			log.Printf("Warning: Failed to start watcher: %v", err)
		}
	}

	// Initialize task manager
	a.taskManager = NewTaskManager(a)
}

// shutdown is called when the app is closing
func (a *App) shutdown(ctx context.Context) {
	// Stop watcher
	if a.watcherManager != nil {
		a.watcherManager.Stop()
	}

	if a.db != nil {
		a.db.Close()
	}
	// Clean up temp files
	a.DeleteAllTempFiles()
}

// initTempDir creates the directory for storing temporary files
func (a *App) initTempDir() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	dataDir, err := getDataDir()
	if err != nil {
		return err
	}

	a.tempDir = filepath.Join(dataDir, "clip_temp_files")
	if err := os.MkdirAll(a.tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp dir '%s': %w", a.tempDir, err)
	}
	log.Printf("Temporary files will be stored in %s\n", a.tempDir)
	return nil
}

// ClipPreview is the struct for JSON responses in the gallery
type ClipPreview struct {
	ID          int64      `json:"id"`
	ContentType string     `json:"content_type"`
	Filename    string     `json:"filename"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	Preview     string     `json:"preview"`
	IsArchived  bool       `json:"is_archived"`
}

// ClipData for full clip retrieval
type ClipData struct {
	ID          int64  `json:"id"`
	ContentType string `json:"content_type"`
	Data        string `json:"data"` // base64 encoded for binary, raw for text
	Filename    string `json:"filename"`
}

// FileData for uploads - binary data as base64
type FileData struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Data        string `json:"data"` // base64 encoded
}

// WatchedFolder represents a folder being watched for new files
type WatchedFolder struct {
	ID              int64     `json:"id"`
	Path            string    `json:"path"`
	FilterMode      string    `json:"filter_mode"`      // "all", "presets", "custom"
	FilterPresets   []string  `json:"filter_presets"`   // ["images", "videos", "documents"]
	FilterRegex     string    `json:"filter_regex"`     // regex pattern for custom mode
	ProcessExisting bool      `json:"process_existing"` // import existing files when added
	AutoArchive     bool      `json:"auto_archive"`     // archive imports immediately
	IsPaused        bool      `json:"is_paused"`        // per-folder pause
	CreatedAt       time.Time `json:"created_at"`
	Exists          bool      `json:"exists"` // whether folder path exists on disk
}

// WatchedFolderConfig for creating/updating watched folders
type WatchedFolderConfig struct {
	Path            string   `json:"path"`
	FilterMode      string   `json:"filter_mode"`
	FilterPresets   []string `json:"filter_presets"`
	FilterRegex     string   `json:"filter_regex"`
	ProcessExisting bool     `json:"process_existing"`
	AutoArchive     bool     `json:"auto_archive"`
}

// GetClips retrieves a list of clips for the gallery
func (a *App) GetClips(archived bool) ([]ClipPreview, error) {
	archivedInt := 0
	if archived {
		archivedInt = 1
	}

	query := `
    SELECT id, content_type, filename, created_at, expires_at, SUBSTR(data, 1, 500), is_archived
    FROM clips
    WHERE is_archived = ? AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
    ORDER BY created_at DESC
    LIMIT 50`

	rows, err := a.db.Query(query, archivedInt)
	if err != nil {
		return nil, fmt.Errorf("failed to query clips: %w", err)
	}
	defer rows.Close()

	var clips []ClipPreview
	for rows.Next() {
		var clip ClipPreview
		var filename sql.NullString
		var expiresAt sql.NullTime
		var previewData []byte
		var isArchivedInt int

		if err := rows.Scan(&clip.ID, &clip.ContentType, &filename, &clip.CreatedAt, &expiresAt, &previewData, &isArchivedInt); err != nil {
			log.Printf("Failed to scan clip row: %v\n", err)
			continue
		}

		clip.Filename = filename.String
		clip.IsArchived = isArchivedInt == 1
		if expiresAt.Valid {
			clip.ExpiresAt = &expiresAt.Time
		}

		// Only set string preview for text-based types
		if strings.HasPrefix(clip.ContentType, "text/") || clip.ContentType == "application/json" {
			clip.Preview = string(previewData)
		} else {
			clip.Preview = ""
		}
		clips = append(clips, clip)
	}

	if clips == nil {
		clips = []ClipPreview{}
	}
	return clips, nil
}

// GetClipData retrieves full clip data by ID
func (a *App) GetClipData(id int64) (*ClipData, error) {
	var contentType string
	var data []byte
	var filename sql.NullString

	row := a.db.QueryRow("SELECT content_type, data, filename FROM clips WHERE id = ?", id)
	if err := row.Scan(&contentType, &data, &filename); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("clip not found")
		}
		return nil, fmt.Errorf("failed to get clip: %w", err)
	}

	clip := &ClipData{
		ID:          id,
		ContentType: contentType,
		Filename:    filename.String,
	}

	// For text content, return as-is; for binary, base64 encode
	if strings.HasPrefix(contentType, "text/") || contentType == "application/json" {
		clip.Data = string(data)
	} else {
		clip.Data = base64.StdEncoding.EncodeToString(data)
	}

	return clip, nil
}

// UploadFileAndGetID uploads a single file and returns the clip ID
func (a *App) UploadFileAndGetID(file FileData) (int64, error) {
	// Decode base64 data
	data, err := base64.StdEncoding.DecodeString(file.Data)
	if err != nil {
		return 0, fmt.Errorf("failed to decode base64 data: %w", err)
	}

	contentType := file.ContentType

	// Special handling for text
	if contentType == "text/plain" || contentType == "" {
		textData := string(data)
		trimmedText := strings.TrimSpace(textData)

		if strings.HasPrefix(trimmedText, "<!DOCTYPE html") {
			contentType = "text/html"
		} else if isJSON(trimmedText) {
			contentType = "application/json"
		} else {
			contentType = "text/plain"
		}
	}

	result, err := a.db.Exec("INSERT INTO clips (content_type, data, filename) VALUES (?, ?, ?)",
		contentType, data, file.Name)
	if err != nil {
		return 0, fmt.Errorf("failed to insert into db: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get inserted ID: %w", err)
	}

	return id, nil
}

// UploadFiles handles file uploads
func (a *App) UploadFiles(files []FileData, expirationMinutes int) error {
	var expiresAt *time.Time
	if expirationMinutes > 0 {
		t := time.Now().Add(time.Duration(expirationMinutes) * time.Minute)
		expiresAt = &t
	}

	for _, file := range files {
		// Decode base64 data
		data, err := base64.StdEncoding.DecodeString(file.Data)
		if err != nil {
			log.Printf("Failed to decode base64 data for file %s: %v", file.Name, err)
			continue
		}

		contentType := file.ContentType

		// Special handling for text
		if contentType == "text/plain" || contentType == "" {
			textData := string(data)
			trimmedText := strings.TrimSpace(textData)

			if strings.HasPrefix(trimmedText, "<!DOCTYPE html") {
				contentType = "text/html"
			} else if isJSON(trimmedText) {
				contentType = "application/json"
			} else {
				contentType = "text/plain"
			}
		}

		_, err = a.db.Exec("INSERT INTO clips (content_type, data, filename, expires_at) VALUES (?, ?, ?, ?)",
			contentType, data, file.Name, expiresAt)
		if err != nil {
			log.Printf("Failed to insert into db: %v\n", err)
			continue
		}
	}

	return nil
}

// DeleteClip deletes a clip by ID
func (a *App) DeleteClip(id int64) error {
	_, err := a.db.Exec("DELETE FROM clips WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete clip: %w", err)
	}
	return nil
}

// ToggleArchive toggles the archived status of a clip
func (a *App) ToggleArchive(id int64) error {
	_, err := a.db.Exec("UPDATE clips SET is_archived = NOT is_archived WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to toggle archive: %w", err)
	}
	return nil
}

// CancelExpiration removes the expiration for a clip
func (a *App) CancelExpiration(id int64) error {
	_, err := a.db.Exec("UPDATE clips SET expires_at = NULL WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to cancel expiration: %w", err)
	}
	return nil
}

// BulkDelete deletes multiple clips at once
func (a *App) BulkDelete(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("DELETE FROM clips WHERE id IN (%s)", strings.Join(placeholders, ","))
	_, err := a.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to bulk delete: %w", err)
	}
	return nil
}

// BulkArchive toggles the archived status of multiple clips
func (a *App) BulkArchive(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("UPDATE clips SET is_archived = NOT is_archived WHERE id IN (%s)", strings.Join(placeholders, ","))
	_, err := a.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to bulk archive: %w", err)
	}
	return nil
}

// BulkDownloadToFile creates a ZIP archive and saves it using native save dialog
func (a *App) BulkDownloadToFile(ids []int64) error {
	if len(ids) == 0 {
		return fmt.Errorf("no IDs provided")
	}

	// Create placeholders for the IN clause
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("SELECT id, content_type, filename, data FROM clips WHERE id IN (%s)", strings.Join(placeholders, ","))
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return fmt.Errorf("failed to query clips: %w", err)
	}
	defer rows.Close()

	// Create ZIP in memory
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

		// Determine a filename for the zip entry
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

		f, err := zw.Create(name)
		if err != nil {
			log.Printf("Failed to create zip entry for %s: %v\n", name, err)
			continue
		}

		if _, err := f.Write(data); err != nil {
			log.Printf("Failed to write data to zip entry for %s: %v\n", name, err)
			continue
		}
	}

	if err := zw.Close(); err != nil {
		return fmt.Errorf("failed to close zip: %w", err)
	}

	// Show save dialog
	defaultFilename := fmt.Sprintf("clips_%s.zip", time.Now().Format("20060102150405"))
	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: defaultFilename,
		Title:           "Save Clips Archive",
		Filters: []runtime.FileFilter{
			{DisplayName: "ZIP Archives", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to show save dialog: %w", err)
	}

	if savePath == "" {
		return nil // User cancelled
	}

	// Write the ZIP file
	if err := os.WriteFile(savePath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// CreateTempFile creates a temporary file from a clip and returns its path
func (a *App) CreateTempFile(id int64) (string, error) {
	var data []byte
	var filename sql.NullString
	var contentType string

	row := a.db.QueryRow("SELECT data, filename, content_type FROM clips WHERE id = ?", id)
	if err := row.Scan(&data, &filename, &contentType); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("clip not found")
		}
		return "", fmt.Errorf("failed to get clip: %w", err)
	}

	// Create a safe filename
	safeName := fmt.Sprintf("%d", id)
	if filename.Valid && filename.String != "" {
		safeName = fmt.Sprintf("%d_%s", id, filepath.Base(filename.String))
	} else {
		exts, _ := mime.ExtensionsByType(contentType)
		if len(exts) > 0 {
			safeName = safeName + exts[0]
		}
	}

	a.mu.Lock()
	tempFilePath := filepath.Join(a.tempDir, safeName)
	a.mu.Unlock()

	if err := os.WriteFile(tempFilePath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	absPath, err := filepath.Abs(tempFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	return absPath, nil
}

// DeleteAllTempFiles deletes all files from the temp directory
func (a *App) DeleteAllTempFiles() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.tempDir == "" {
		return nil
	}

	// Remove the directory and all its contents
	if err := os.RemoveAll(a.tempDir); err != nil {
		return fmt.Errorf("failed to remove temp dir: %w", err)
	}

	// Recreate the directory
	if err := os.MkdirAll(a.tempDir, 0755); err != nil {
		return fmt.Errorf("failed to recreate temp dir: %w", err)
	}

	return nil
}

// CopyToClipboard copies text to the system clipboard
func (a *App) CopyToClipboard(text string) error {
	clipboard.Write(clipboard.FmtText, []byte(text))
	return nil
}

// GetClipboardText gets text from the system clipboard
func (a *App) GetClipboardText() (string, error) {
	data := clipboard.Read(clipboard.FmtText)
	return string(data), nil
}

// GetClipboardImage gets an image from the system clipboard
// Returns base64 data and content type
func (a *App) GetClipboardImage() (string, string, error) {
	data := clipboard.Read(clipboard.FmtImage)
	if len(data) == 0 {
		return "", "", fmt.Errorf("no image in clipboard")
	}
	return base64.StdEncoding.EncodeToString(data), "image/png", nil
}

// ReadFileFromPath reads a file from disk (for drag-drop)
func (a *App) ReadFileFromPath(path string) (*FileData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Detect content type from extension
	ext := filepath.Ext(path)
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return &FileData{
		Name:        filepath.Base(path),
		ContentType: contentType,
		Data:        base64.StdEncoding.EncodeToString(data),
	}, nil
}

// SaveClipToFile saves a single clip to file using native save dialog
func (a *App) SaveClipToFile(id int64) error {
	var data []byte
	var filename sql.NullString
	var contentType string

	row := a.db.QueryRow("SELECT data, filename, content_type FROM clips WHERE id = ?", id)
	if err := row.Scan(&data, &filename, &contentType); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("clip not found")
		}
		return fmt.Errorf("failed to get clip: %w", err)
	}

	// Determine default filename
	defaultFilename := filename.String
	if defaultFilename == "" {
		defaultFilename = fmt.Sprintf("clip_%d", id)
		exts, _ := mime.ExtensionsByType(contentType)
		if len(exts) > 0 {
			defaultFilename += exts[0]
		}
	}

	// Show save dialog
	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: defaultFilename,
		Title:           "Save Clip",
	})
	if err != nil {
		return fmt.Errorf("failed to show save dialog: %w", err)
	}

	if savePath == "" {
		return nil // User cancelled
	}

	// Write the file
	if err := os.WriteFile(savePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// isJSON checks if a string is valid JSON
func isJSON(s string) bool {
	var js json.RawMessage
	return json.Unmarshal([]byte(s), &js) == nil
}

// GetWatchedFolders retrieves all watched folders
func (a *App) GetWatchedFolders() ([]WatchedFolder, error) {
	rows, err := a.db.Query(`
		SELECT id, path, filter_mode, filter_presets, filter_regex,
		       process_existing, auto_archive, is_paused, created_at
		FROM watched_folders
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query watched folders: %w", err)
	}
	defer rows.Close()

	var folders []WatchedFolder
	for rows.Next() {
		var f WatchedFolder
		var filterPresets sql.NullString
		var filterRegex sql.NullString
		var processExisting, autoArchive, isPaused int

		if err := rows.Scan(&f.ID, &f.Path, &f.FilterMode, &filterPresets, &filterRegex,
			&processExisting, &autoArchive, &isPaused, &f.CreatedAt); err != nil {
			log.Printf("Failed to scan watched folder: %v", err)
			continue
		}

		f.ProcessExisting = processExisting == 1
		f.AutoArchive = autoArchive == 1
		f.IsPaused = isPaused == 1
		f.FilterRegex = filterRegex.String

		// Parse filter presets JSON
		if filterPresets.Valid && filterPresets.String != "" {
			_ = json.Unmarshal([]byte(filterPresets.String), &f.FilterPresets)
		}
		if f.FilterPresets == nil {
			f.FilterPresets = []string{}
		}

		// Check if folder exists
		if _, err := os.Stat(f.Path); err == nil {
			f.Exists = true
		}

		folders = append(folders, f)
	}

	if folders == nil {
		folders = []WatchedFolder{}
	}
	return folders, nil
}

// GetWatchedFolderByID retrieves a single watched folder by ID
func (a *App) GetWatchedFolderByID(id int64) (*WatchedFolder, error) {
	var f WatchedFolder
	var filterPresets sql.NullString
	var filterRegex sql.NullString
	var processExisting, autoArchive, isPaused int

	err := a.db.QueryRow(`
		SELECT id, path, filter_mode, filter_presets, filter_regex,
		       process_existing, auto_archive, is_paused, created_at
		FROM watched_folders
		WHERE id = ?
	`, id).Scan(&f.ID, &f.Path, &f.FilterMode, &filterPresets, &filterRegex,
		&processExisting, &autoArchive, &isPaused, &f.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query watched folder: %w", err)
	}

	f.ProcessExisting = processExisting == 1
	f.AutoArchive = autoArchive == 1
	f.IsPaused = isPaused == 1
	f.FilterRegex = filterRegex.String

	if filterPresets.Valid && filterPresets.String != "" {
		_ = json.Unmarshal([]byte(filterPresets.String), &f.FilterPresets)
	}
	if f.FilterPresets == nil {
		f.FilterPresets = []string{}
	}

	if _, err := os.Stat(f.Path); err == nil {
		f.Exists = true
	}

	return &f, nil
}

// AddWatchedFolder adds a new folder to watch
func (a *App) AddWatchedFolder(config WatchedFolderConfig) (*WatchedFolder, error) {
	// Validate path exists
	if _, err := os.Stat(config.Path); os.IsNotExist(err) {
		return nil, fmt.Errorf("folder does not exist: %s", config.Path)
	}

	// Default filter mode
	if config.FilterMode == "" {
		config.FilterMode = "all"
	}

	// Serialize presets to JSON
	var presetsJSON []byte
	if len(config.FilterPresets) > 0 {
		presetsJSON, _ = json.Marshal(config.FilterPresets)
	}

	result, err := a.db.Exec(`
		INSERT INTO watched_folders (path, filter_mode, filter_presets, filter_regex, process_existing, auto_archive)
		VALUES (?, ?, ?, ?, ?, ?)
	`, config.Path, config.FilterMode, string(presetsJSON), config.FilterRegex,
		boolToInt(config.ProcessExisting), boolToInt(config.AutoArchive))
	if err != nil {
		return nil, fmt.Errorf("failed to add watched folder: %w", err)
	}

	id, _ := result.LastInsertId()

	return &WatchedFolder{
		ID:              id,
		Path:            config.Path,
		FilterMode:      config.FilterMode,
		FilterPresets:   config.FilterPresets,
		FilterRegex:     config.FilterRegex,
		ProcessExisting: config.ProcessExisting,
		AutoArchive:     config.AutoArchive,
		IsPaused:        false,
		Exists:          true,
	}, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// UpdateWatchedFolder updates an existing watched folder config
func (a *App) UpdateWatchedFolder(id int64, config WatchedFolderConfig) error {
	var presetsJSON []byte
	if len(config.FilterPresets) > 0 {
		presetsJSON, _ = json.Marshal(config.FilterPresets)
	}

	_, err := a.db.Exec(`
		UPDATE watched_folders
		SET filter_mode = ?, filter_presets = ?, filter_regex = ?, auto_archive = ?
		WHERE id = ?
	`, config.FilterMode, string(presetsJSON), config.FilterRegex,
		boolToInt(config.AutoArchive), id)
	if err != nil {
		return fmt.Errorf("failed to update watched folder: %w", err)
	}
	return nil
}

// RemoveWatchedFolder removes a watched folder
func (a *App) RemoveWatchedFolder(id int64) error {
	_, err := a.db.Exec("DELETE FROM watched_folders WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to remove watched folder: %w", err)
	}
	return nil
}

// GetGlobalWatchPaused returns whether global watching is paused
func (a *App) GetGlobalWatchPaused() bool {
	var value string
	err := a.db.QueryRow("SELECT value FROM settings WHERE key = 'global_watch_paused'").Scan(&value)
	if err != nil {
		return false
	}
	return value == "true"
}

// SetGlobalWatchPaused sets the global watch pause state
func (a *App) SetGlobalWatchPaused(paused bool) error {
	value := "false"
	if paused {
		value = "true"
	}
	_, err := a.db.Exec("UPDATE settings SET value = ? WHERE key = 'global_watch_paused'", value)
	return err
}

// SetFolderPaused sets the pause state for a specific folder
func (a *App) SetFolderPaused(id int64, paused bool) error {
	_, err := a.db.Exec("UPDATE watched_folders SET is_paused = ? WHERE id = ?", boolToInt(paused), id)
	return err
}

// SelectFolder opens a native folder picker dialog
func (a *App) SelectFolder() (string, error) {
	path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Folder to Watch",
	})
	if err != nil {
		return "", fmt.Errorf("failed to open folder dialog: %w", err)
	}
	return path, nil
}

// IsDirectory checks if a path is a directory
func (a *App) IsDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// ProcessExistingFilesInFolder processes existing files in a watched folder
func (a *App) ProcessExistingFilesInFolder(folderID int64) error {
	if a.watcherManager != nil {
		return a.watcherManager.ProcessExistingFiles(folderID)
	}
	return nil
}

// WatchStatus represents the current watching state
type WatchStatus struct {
	GlobalPaused bool `json:"global_paused"`
	ActiveCount  int  `json:"active_count"`
	TotalCount   int  `json:"total_count"`
	IsWatching   bool `json:"is_watching"` // true if any folder is actively being watched
}

// GetWatchStatus returns the current watch status
func (a *App) GetWatchStatus() WatchStatus {
	globalPaused := a.GetGlobalWatchPaused()
	folders, err := a.GetWatchedFolders()
	if err != nil {
		log.Printf("Warning: Failed to get watched folders for status: %v", err)
		return WatchStatus{GlobalPaused: globalPaused}
	}

	activeCount := 0
	for _, f := range folders {
		if !f.IsPaused && f.Exists {
			activeCount++
		}
	}

	return WatchStatus{
		GlobalPaused: globalPaused,
		ActiveCount:  activeCount,
		TotalCount:   len(folders),
		IsWatching:   !globalPaused && activeCount > 0,
	}
}

// GetSetting retrieves a setting value by key
func (a *App) GetSetting(key string) (string, error) {
	var value string
	err := a.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// SetSetting stores a setting value (insert or update)
func (a *App) SetSetting(key string, value string) error {
	_, err := a.db.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

// HasFalApiKey returns whether a fal.ai API key is configured
func (a *App) HasFalApiKey() bool {
	key, err := a.GetSetting("fal_api_key")
	return err == nil && key != ""
}

// FalTaskType represents a type of AI processing task
type FalTaskType string

const (
	FalTaskColorize   FalTaskType = "colorize"
	FalTaskUpscale    FalTaskType = "upscale"
	FalTaskRestore    FalTaskType = "restore"
	FalTaskEdit       FalTaskType = "edit"
	FalTaskVectorize  FalTaskType = "vectorize"
)

// FalTaskOptions contains options for AI processing
type FalTaskOptions struct {
	Task            FalTaskType `json:"task"`
	Model           string      `json:"model,omitempty"`
	Prompt          string      `json:"prompt,omitempty"`
	Strength        float64     `json:"strength,omitempty"`
	FixColors       bool        `json:"fix_colors,omitempty"`
	RemoveScratches bool        `json:"remove_scratches,omitempty"`
}

// FalProcessingResult contains the result of processing
type FalProcessingResult struct {
	Success    bool   `json:"success"`
	ClipID     int64  `json:"clip_id,omitempty"`
	Error      string `json:"error,omitempty"`
	OriginalID int64  `json:"original_id"`
}

// FalModelInfo describes an available model
type FalModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// GetAvailableFalModels returns available models for each task
func (a *App) GetAvailableFalModels() map[string][]FalModelInfo {
	return map[string][]FalModelInfo{
		"colorize": {
			{ID: FalColorize, Name: "DDColor", Description: "Automatic colorization"},
		},
		"upscale": {
			{ID: FalClarityUpscale, Name: "Clarity Upscaler", Description: "High-fidelity upscaling (default)"},
			{ID: FalESRGAN, Name: "ESRGAN", Description: "Fast 4x upscaling"},
			{ID: FalCreativeUp, Name: "Creative Upscaler", Description: "AI-enhanced with prompt support"},
		},
		"restore": {
			{ID: FalRestore, Name: "Photo Restoration", Description: "Fix scratches, colors, resolution (default)"},
			{ID: FalCodeFormer, Name: "CodeFormer", Description: "Face & image restoration"},
		},
		"edit": {
			{ID: FalFlux2Edit, Name: "FLUX.2 Turbo", Description: "Fast text-guided editing (default)"},
			{ID: FalFlux2ProEdit, Name: "FLUX.2 Pro", Description: "Professional quality editing"},
			{ID: FalFlux1DevEdit, Name: "FLUX.1 Dev", Description: "Development model with strength control"},
		},
	}
}

// ProcessImageWithFal processes a single image with fal.ai
func (a *App) ProcessImageWithFal(clipID int64, options FalTaskOptions) (*FalProcessingResult, error) {
	return a.processImageWithFalCtx(context.Background(), clipID, options)
}

// processImageWithFalCtx processes a single image with fal.ai with cancellation support
func (a *App) processImageWithFalCtx(ctx context.Context, clipID int64, options FalTaskOptions) (*FalProcessingResult, error) {
	apiKey, err := a.GetSetting("fal_api_key")
	if err != nil || apiKey == "" {
		return nil, fmt.Errorf("fal.ai API key not configured")
	}

	clipData, err := a.GetClipData(clipID)
	if err != nil {
		return nil, fmt.Errorf("failed to get clip: %w", err)
	}

	if !strings.HasPrefix(clipData.ContentType, "image/") {
		return nil, fmt.Errorf("clip is not an image")
	}

	dataURI := fmt.Sprintf("data:%s;base64,%s", clipData.ContentType, clipData.Data)
	client := NewFalClient(apiKey)

	var resultImage *FalImage

	switch options.Task {
	case FalTaskColorize:
		resultImage, err = client.ColorizeWithContext(ctx, dataURI)
	case FalTaskUpscale:
		resultImage, err = client.UpscaleWithContext(ctx, dataURI, options.Model)
	case FalTaskRestore:
		resultImage, err = client.RestoreWithContext(ctx, dataURI, options.Model, options.FixColors, options.RemoveScratches)
	case FalTaskEdit:
		if options.Prompt == "" {
			return nil, fmt.Errorf("prompt required for edit task")
		}
		resultImage, err = client.EditWithContext(ctx, dataURI, options.Model, options.Prompt, options.Strength)
	case FalTaskVectorize:
		resultImage, err = client.VectorizeWithContext(ctx, dataURI)
	default:
		return nil, fmt.Errorf("unknown task: %s", options.Task)
	}

	if err != nil {
		return &FalProcessingResult{
			Success:    false,
			Error:      err.Error(),
			OriginalID: clipID,
		}, nil
	}

	if resultImage == nil || resultImage.URL == "" {
		return &FalProcessingResult{
			Success:    false,
			Error:      "no image URL returned from API",
			OriginalID: clipID,
		}, nil
	}

	imageData, contentType, err := client.DownloadImageWithContext(ctx, resultImage.URL)
	if err != nil {
		return &FalProcessingResult{
			Success:    false,
			Error:      fmt.Sprintf("failed to download result: %v", err),
			OriginalID: clipID,
		}, nil
	}

	newFilename := generateProcessedFilename(clipData.Filename, clipID, options.Task)
	newClipID, err := a.saveProcessedImage(imageData, contentType, newFilename)
	if err != nil {
		return &FalProcessingResult{
			Success:    false,
			Error:      fmt.Sprintf("failed to save result: %v", err),
			OriginalID: clipID,
		}, nil
	}

	return &FalProcessingResult{
		Success:    true,
		ClipID:     newClipID,
		OriginalID: clipID,
	}, nil
}

// BulkProcessImagesWithFal processes multiple images with fal.ai
func (a *App) BulkProcessImagesWithFal(clipIDs []int64, options FalTaskOptions) []FalProcessingResult {
	results := make([]FalProcessingResult, 0, len(clipIDs))

	for i, id := range clipIDs {
		result, err := a.ProcessImageWithFal(id, options)
		if err != nil {
			results = append(results, FalProcessingResult{
				Success:    false,
				Error:      err.Error(),
				OriginalID: id,
			})
		} else {
			results = append(results, *result)
		}

		runtime.EventsEmit(a.ctx, "fal:progress", map[string]interface{}{
			"current": i + 1,
			"total":   len(clipIDs),
		})
	}

	successCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		}
	}

	runtime.EventsEmit(a.ctx, "fal:complete", map[string]interface{}{
		"total":     len(clipIDs),
		"succeeded": successCount,
	})

	return results
}

func generateProcessedFilename(original string, clipID int64, task FalTaskType) string {
	if original == "" {
		original = fmt.Sprintf("clip_%d", clipID)
	}
	ext := filepath.Ext(original)
	name := strings.TrimSuffix(original, ext)
	if ext == "" {
		ext = ".png"
	}
	return fmt.Sprintf("%s_%s%s", name, task, ext)
}

func (a *App) saveProcessedImage(data []byte, contentType, filename string) (int64, error) {
	result, err := a.db.Exec(
		"INSERT INTO clips (content_type, data, filename) VALUES (?, ?, ?)",
		contentType, data, filename,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// --- Task Manager for Background Processing ---

// AITask represents a background AI processing task
type AITask struct {
	ID        string                `json:"id"`
	TaskName  string                `json:"task_name"`
	Status    string                `json:"status"` // pending, running, completed, cancelled, failed
	ClipIDs   []int64               `json:"clip_ids"`
	Options   FalTaskOptions        `json:"options"`
	Progress  int                   `json:"progress"`
	Total     int                   `json:"total"`
	Results   []FalProcessingResult `json:"results,omitempty"`
	Error     string                `json:"error,omitempty"`
	CreatedAt time.Time             `json:"created_at"`
}

// TaskManager manages background AI processing tasks
type TaskManager struct {
	tasks     map[string]*AITask
	cancelFns map[string]context.CancelFunc
	mu        sync.RWMutex
	app       *App
}

// NewTaskManager creates a new TaskManager
func NewTaskManager(app *App) *TaskManager {
	return &TaskManager{
		tasks:     make(map[string]*AITask),
		cancelFns: make(map[string]context.CancelFunc),
		app:       app,
	}
}

// StartAITask starts a new background AI processing task
func (a *App) StartAITask(clipIDs []int64, options FalTaskOptions, taskName string) (string, error) {
	if a.taskManager == nil {
		return "", fmt.Errorf("task manager not initialized")
	}

	// Generate unique task ID
	taskID := fmt.Sprintf("task_%d", time.Now().UnixNano())

	task := &AITask{
		ID:        taskID,
		TaskName:  taskName,
		Status:    "pending",
		ClipIDs:   clipIDs,
		Options:   options,
		Progress:  0,
		Total:     len(clipIDs),
		Results:   make([]FalProcessingResult, 0),
		CreatedAt: time.Now(),
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	a.taskManager.mu.Lock()
	a.taskManager.tasks[taskID] = task
	a.taskManager.cancelFns[taskID] = cancel
	a.taskManager.mu.Unlock()

	// Start processing in goroutine
	go a.taskManager.runTask(ctx, task)

	return taskID, nil
}

// runTask executes the AI processing task
func (tm *TaskManager) runTask(ctx context.Context, task *AITask) {
	// Update status to running
	tm.mu.Lock()
	task.Status = "running"
	tm.mu.Unlock()

	// Emit task started event
	runtime.EventsEmit(tm.app.ctx, "task:started", task)

	// Get API key
	apiKey, err := tm.app.GetSetting("fal_api_key")
	if err != nil || apiKey == "" {
		tm.failTask(task, "fal.ai API key not configured")
		return
	}

	// Process each clip
	for i, clipID := range task.ClipIDs {
		// Check for cancellation
		select {
		case <-ctx.Done():
			tm.cancelTask(task)
			return
		default:
		}

		// Process single image with context for cancellation support
		result, err := tm.app.processImageWithFalCtx(ctx, clipID, task.Options)

		// Update results with mutex protection to prevent race condition
		tm.mu.Lock()
		if err != nil {
			task.Results = append(task.Results, FalProcessingResult{
				Success:    false,
				Error:      err.Error(),
				OriginalID: clipID,
			})
		} else {
			task.Results = append(task.Results, *result)
		}
		task.Progress = i + 1
		tm.mu.Unlock()

		// Emit progress event
		runtime.EventsEmit(tm.app.ctx, "task:progress", map[string]interface{}{
			"taskId":   task.ID,
			"progress": task.Progress,
			"total":    task.Total,
		})
	}

	// Check if any results failed and update status (all under lock for consistency)
	tm.mu.Lock()
	failedCount := 0
	for _, r := range task.Results {
		if !r.Success {
			failedCount++
		}
	}

	if failedCount == len(task.Results) {
		// All failed
		task.Status = "failed"
		task.Error = fmt.Sprintf("All %d images failed to process", failedCount)
	} else if failedCount > 0 {
		// Some failed
		task.Status = "failed"
		task.Error = fmt.Sprintf("%d of %d images failed", failedCount, len(task.Results))
	} else {
		// All succeeded
		task.Status = "completed"
	}
	tm.mu.Unlock()

	// Emit completed event (frontend uses task.status to determine icon)
	runtime.EventsEmit(tm.app.ctx, "task:completed", task)
}

// failTask marks a task as failed
func (tm *TaskManager) failTask(task *AITask, errMsg string) {
	tm.mu.Lock()
	task.Status = "failed"
	task.Error = errMsg
	tm.mu.Unlock()

	runtime.EventsEmit(tm.app.ctx, "task:failed", map[string]interface{}{
		"taskId": task.ID,
		"error":  errMsg,
	})
}

// cancelTask marks a task as cancelled
func (tm *TaskManager) cancelTask(task *AITask) {
	tm.mu.Lock()
	task.Status = "cancelled"
	tm.mu.Unlock()

	runtime.EventsEmit(tm.app.ctx, "task:cancelled", map[string]interface{}{
		"taskId": task.ID,
	})
}

// GetTasks returns all tasks
func (a *App) GetTasks() []*AITask {
	if a.taskManager == nil {
		return []*AITask{}
	}

	a.taskManager.mu.RLock()
	defer a.taskManager.mu.RUnlock()

	tasks := make([]*AITask, 0, len(a.taskManager.tasks))
	for _, task := range a.taskManager.tasks {
		tasks = append(tasks, task)
	}
	return tasks
}

// CancelTask cancels a running task
func (a *App) CancelTask(taskID string) error {
	if a.taskManager == nil {
		return fmt.Errorf("task manager not initialized")
	}

	a.taskManager.mu.RLock()
	cancel, exists := a.taskManager.cancelFns[taskID]
	task, taskExists := a.taskManager.tasks[taskID]
	a.taskManager.mu.RUnlock()

	if !exists || !taskExists {
		return fmt.Errorf("task not found: %s", taskID)
	}

	if task.Status != "running" && task.Status != "pending" {
		return fmt.Errorf("task is not running: %s", task.Status)
	}

	cancel()
	return nil
}

// ClearCompletedTasks removes completed, cancelled, and failed tasks
func (a *App) ClearCompletedTasks() {
	if a.taskManager == nil {
		return
	}

	a.taskManager.mu.Lock()
	defer a.taskManager.mu.Unlock()

	for id, task := range a.taskManager.tasks {
		if task.Status == "completed" || task.Status == "cancelled" || task.Status == "failed" {
			delete(a.taskManager.tasks, id)
			delete(a.taskManager.cancelFns, id)
		}
	}
}
