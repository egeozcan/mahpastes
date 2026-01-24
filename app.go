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
	ctx     context.Context
	db      *sql.DB
	tempDir string
	mu      sync.Mutex
}

// NewApp creates a new App instance
func NewApp() *App {
	return &App{}
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
}

// shutdown is called when the app is closing
func (a *App) shutdown(ctx context.Context) {
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
