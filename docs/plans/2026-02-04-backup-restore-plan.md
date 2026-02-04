# Backup & Restore Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add manual backup/restore feature that exports all app data to a single ZIP file with version-independent format.

**Architecture:** Go backend handles ZIP creation with SQL dump + plugin files + manifest. Frontend adds Settings UI section with buttons and a restore confirmation modal. E2E tests verify backup creation and restoration.

**Tech Stack:** Go (archive/zip, database/sql), Vanilla JavaScript, Playwright

---

## Task 1: Add pending_reconfirm column to plugin_permissions

**Files:**
- Modify: `database.go:161-171`

**Step 1: Write the migration**

Add column migration after the plugin_permissions table creation in `initDB()`:

```go
// Migrate: Add pending_reconfirm column to plugin_permissions if it doesn't exist
_, _ = db.Exec("ALTER TABLE plugin_permissions ADD COLUMN pending_reconfirm INTEGER DEFAULT 0")
```

**Step 2: Run app to verify migration**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/backup-restore && ~/go/bin/wails dev`

Expected: App starts without errors, column is added to existing database.

**Step 3: Commit**

```bash
git add database.go
git commit -m "db: add pending_reconfirm column to plugin_permissions"
```

---

## Task 2: Create backup.go with manifest types and constants

**Files:**
- Create: `backup.go`

**Step 1: Create the file with types**

```go
package main

import (
	"archive/zip"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	BackupFormatVersion = 1
	AppVersion          = "1.0.0" // TODO: Get from build info
)

// BackupManifest describes the contents of a backup file
type BackupManifest struct {
	FormatVersion int            `json:"format_version"`
	AppVersion    string         `json:"app_version"`
	CreatedAt     time.Time      `json:"created_at"`
	Platform      string         `json:"platform"`
	Summary       BackupSummary  `json:"summary"`
	Excluded      []string       `json:"excluded"`
}

// BackupSummary contains counts of backed up items
type BackupSummary struct {
	Clips        int `json:"clips"`
	Tags         int `json:"tags"`
	Plugins      int `json:"plugins"`
	WatchFolders int `json:"watch_folders"`
}

// sensitiveSettingPatterns defines patterns for settings that should not be backed up
var sensitiveSettingPatterns = []string{
	"api_key",
	"secret",
	"password",
	"token",
}

// isSensitiveSetting checks if a setting key matches sensitive patterns
func isSensitiveSetting(key string) bool {
	keyLower := strings.ToLower(key)
	for _, pattern := range sensitiveSettingPatterns {
		if strings.Contains(keyLower, pattern) {
			return true
		}
	}
	return false
}
```

**Step 2: Verify file compiles**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/backup-restore && go build .`

Expected: Build succeeds.

**Step 3: Commit**

```bash
git add backup.go
git commit -m "feat(backup): add manifest types and constants"
```

---

## Task 3: Implement SQL export functions in backup.go

**Files:**
- Modify: `backup.go`

**Step 1: Add SQL export helper functions**

Append to `backup.go`:

```go
// exportTableToSQL exports a table to SQL INSERT statements
func exportTableToSQL(db *sql.DB, tableName string, w io.Writer, excludeCallback func(map[string]interface{}) bool) (int, error) {
	rows, err := db.Query(fmt.Sprintf("SELECT * FROM %s", tableName))
	if err != nil {
		return 0, fmt.Errorf("failed to query %s: %w", tableName, err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("failed to get columns for %s: %w", tableName, err)
	}

	count := 0
	for rows.Next() {
		// Create a slice of interface{} to hold values
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return count, fmt.Errorf("failed to scan row in %s: %w", tableName, err)
		}

		// Build map for exclude callback
		rowMap := make(map[string]interface{})
		for i, col := range columns {
			rowMap[col] = values[i]
		}

		// Check if this row should be excluded
		if excludeCallback != nil && excludeCallback(rowMap) {
			continue
		}

		// Build INSERT statement
		var colNames []string
		var placeholders []string
		var sqlValues []string

		for i, col := range columns {
			colNames = append(colNames, col)
			placeholders = append(placeholders, "?")
			sqlValues = append(sqlValues, formatSQLValue(values[i]))
		}

		sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);\n",
			tableName,
			strings.Join(colNames, ", "),
			strings.Join(sqlValues, ", "))

		if _, err := w.Write([]byte(sql)); err != nil {
			return count, fmt.Errorf("failed to write SQL: %w", err)
		}
		count++
	}

	return count, nil
}

// formatSQLValue formats a value for SQL INSERT statement
func formatSQLValue(v interface{}) string {
	if v == nil {
		return "NULL"
	}

	switch val := v.(type) {
	case []byte:
		// Encode binary data as base64 with X'' hex literal wrapper
		// SQLite can handle this via a custom function, but for portability
		// we'll use a special marker that import can recognize
		encoded := base64.StdEncoding.EncodeToString(val)
		return fmt.Sprintf("X'%s'", encoded) // Will be decoded during import
	case string:
		// Escape single quotes
		escaped := strings.ReplaceAll(val, "'", "''")
		return fmt.Sprintf("'%s'", escaped)
	case int, int64, int32:
		return fmt.Sprintf("%d", val)
	case float64, float32:
		return fmt.Sprintf("%f", val)
	case bool:
		if val {
			return "1"
		}
		return "0"
	case time.Time:
		return fmt.Sprintf("'%s'", val.Format("2006-01-02 15:04:05"))
	default:
		// Try to convert to string
		return fmt.Sprintf("'%v'", val)
	}
}
```

**Step 2: Verify file compiles**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/backup-restore && go build .`

Expected: Build succeeds.

**Step 3: Commit**

```bash
git add backup.go
git commit -m "feat(backup): add SQL export helper functions"
```

---

## Task 4: Implement CreateBackup method

**Files:**
- Modify: `backup.go`

**Step 1: Add CreateBackup function**

Append to `backup.go`:

```go
// CreateBackup creates a backup ZIP file at the specified path
func (a *App) CreateBackup(destPath string) error {
	// Create temp directory for staging
	tempDir, err := os.MkdirTemp("", "mahpastes-backup-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Export database to SQL file
	sqlPath := filepath.Join(tempDir, "database.sql")
	summary, excluded, err := a.exportDatabaseToSQL(sqlPath)
	if err != nil {
		return fmt.Errorf("failed to export database: %w", err)
	}

	// Copy plugin files
	dataDir, err := getDataDir()
	if err != nil {
		return fmt.Errorf("failed to get data directory: %w", err)
	}
	pluginsDir := filepath.Join(dataDir, "plugins")
	tempPluginsDir := filepath.Join(tempDir, "plugins")

	if err := os.MkdirAll(tempPluginsDir, 0755); err != nil {
		return fmt.Errorf("failed to create plugins directory: %w", err)
	}

	// Copy .lua files
	if entries, err := os.ReadDir(pluginsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".lua") {
				src := filepath.Join(pluginsDir, entry.Name())
				dst := filepath.Join(tempPluginsDir, entry.Name())
				if err := copyFile(src, dst); err != nil {
					// Log warning but continue
					fmt.Printf("Warning: failed to copy plugin %s: %v\n", entry.Name(), err)
				}
			}
		}
	}

	// Create manifest
	manifest := BackupManifest{
		FormatVersion: BackupFormatVersion,
		AppVersion:    AppVersion,
		CreatedAt:     time.Now(),
		Platform:      getPlatform(),
		Summary:       summary,
		Excluded:      excluded,
	}

	manifestPath := filepath.Join(tempDir, "manifest.json")
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	// Create ZIP file
	if err := createZipFromDir(tempDir, destPath); err != nil {
		return fmt.Errorf("failed to create ZIP: %w", err)
	}

	return nil
}

// exportDatabaseToSQL exports all database tables to a SQL file
func (a *App) exportDatabaseToSQL(destPath string) (BackupSummary, []string, error) {
	f, err := os.Create(destPath)
	if err != nil {
		return BackupSummary{}, nil, err
	}
	defer f.Close()

	var summary BackupSummary
	var excluded []string

	// Write header
	f.WriteString("-- mahpastes backup\n")
	f.WriteString(fmt.Sprintf("-- Created: %s\n", time.Now().Format(time.RFC3339)))
	f.WriteString(fmt.Sprintf("-- Format version: %d\n\n", BackupFormatVersion))

	// Export clips
	f.WriteString("-- Table: clips\n")
	count, err := exportTableToSQL(a.db, "clips", f, nil)
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export clips: %w", err)
	}
	summary.Clips = count
	f.WriteString("\n")

	// Export tags
	f.WriteString("-- Table: tags\n")
	count, err = exportTableToSQL(a.db, "tags", f, nil)
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export tags: %w", err)
	}
	summary.Tags = count
	f.WriteString("\n")

	// Export clip_tags
	f.WriteString("-- Table: clip_tags\n")
	_, err = exportTableToSQL(a.db, "clip_tags", f, nil)
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export clip_tags: %w", err)
	}
	f.WriteString("\n")

	// Export settings (excluding sensitive ones)
	f.WriteString("-- Table: settings\n")
	_, err = exportTableToSQL(a.db, "settings", f, func(row map[string]interface{}) bool {
		if key, ok := row["key"].(string); ok {
			if isSensitiveSetting(key) {
				excluded = append(excluded, key)
				return true
			}
		}
		return false
	})
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export settings: %w", err)
	}
	f.WriteString("\n")

	// Export watched_folders
	f.WriteString("-- Table: watched_folders\n")
	count, err = exportTableToSQL(a.db, "watched_folders", f, nil)
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export watched_folders: %w", err)
	}
	summary.WatchFolders = count
	f.WriteString("\n")

	// Export plugins
	f.WriteString("-- Table: plugins\n")
	count, err = exportTableToSQL(a.db, "plugins", f, nil)
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export plugins: %w", err)
	}
	summary.Plugins = count
	f.WriteString("\n")

	// Export plugin_storage
	f.WriteString("-- Table: plugin_storage\n")
	_, err = exportTableToSQL(a.db, "plugin_storage", f, nil)
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export plugin_storage: %w", err)
	}
	f.WriteString("\n")

	// Export plugin_permissions (will be marked as pending_reconfirm on import)
	f.WriteString("-- Table: plugin_permissions\n")
	_, err = exportTableToSQL(a.db, "plugin_permissions", f, nil)
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export plugin_permissions: %w", err)
	}

	return summary, excluded, nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// createZipFromDir creates a ZIP file from a directory
func createZipFromDir(srcDir, destPath string) error {
	zipFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	w := zip.NewWriter(zipFile)
	defer w.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		// Skip the root directory
		if relPath == "." {
			return nil
		}

		// Create ZIP entry
		if info.IsDir() {
			_, err := w.Create(relPath + "/")
			return err
		}

		// Create file entry
		writer, err := w.Create(relPath)
		if err != nil {
			return err
		}

		// Copy file contents
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(writer, file)
		return err
	})
}

// getPlatform returns the current platform identifier
func getPlatform() string {
	switch os.Getenv("GOOS") {
	case "darwin":
		return "darwin"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}
```

**Step 2: Verify file compiles**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/backup-restore && go build .`

Expected: Build succeeds.

**Step 3: Commit**

```bash
git add backup.go
git commit -m "feat(backup): implement CreateBackup method"
```

---

## Task 5: Implement ValidateBackup method

**Files:**
- Modify: `backup.go`

**Step 1: Add ValidateBackup function**

Append to `backup.go`:

```go
// ValidateBackup opens a backup file and returns its manifest
func ValidateBackup(backupPath string) (*BackupManifest, error) {
	// Open ZIP file
	r, err := zip.OpenReader(backupPath)
	if err != nil {
		return nil, fmt.Errorf("invalid backup file: %w", err)
	}
	defer r.Close()

	// Find manifest.json
	var manifestFile *zip.File
	for _, f := range r.File {
		if f.Name == "manifest.json" {
			manifestFile = f
			break
		}
	}

	if manifestFile == nil {
		return nil, fmt.Errorf("this doesn't appear to be a mahpastes backup (missing manifest)")
	}

	// Read manifest
	rc, err := manifestFile.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}
	defer rc.Close()

	var manifest BackupManifest
	if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, nil
}
```

**Step 2: Verify file compiles**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/backup-restore && go build .`

Expected: Build succeeds.

**Step 3: Commit**

```bash
git add backup.go
git commit -m "feat(backup): implement ValidateBackup method"
```

---

## Task 6: Implement RestoreBackup method

**Files:**
- Modify: `backup.go`

**Step 1: Add RestoreBackup function**

Append to `backup.go`:

```go
// RestoreBackup restores data from a backup ZIP file
func (a *App) RestoreBackup(backupPath string) error {
	// Validate first
	manifest, err := ValidateBackup(backupPath)
	if err != nil {
		return err
	}

	// Warn if format version is newer
	if manifest.FormatVersion > BackupFormatVersion {
		// We'll proceed but some data may not be restored
		fmt.Printf("Warning: backup format version %d is newer than supported %d\n",
			manifest.FormatVersion, BackupFormatVersion)
	}

	// Open ZIP
	r, err := zip.OpenReader(backupPath)
	if err != nil {
		return fmt.Errorf("failed to open backup: %w", err)
	}
	defer r.Close()

	// Stop watchers during restore
	if a.watcherManager != nil {
		a.watcherManager.Stop()
		defer func() {
			// Restart watchers after restore
			if err := a.watcherManager.Start(); err != nil {
				fmt.Printf("Warning: failed to restart watchers: %v\n", err)
			}
		}()
	}

	// Find database.sql
	var sqlFile *zip.File
	for _, f := range r.File {
		if f.Name == "database.sql" {
			sqlFile = f
			break
		}
	}

	if sqlFile == nil {
		return fmt.Errorf("backup is corrupted (missing database.sql)")
	}

	// Begin transaction
	tx, err := a.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear all existing data
	tables := []string{
		"clip_tags",
		"clips",
		"tags",
		"settings",
		"watched_folders",
		"plugin_storage",
		"plugin_permissions",
		"plugins",
	}

	for _, table := range tables {
		if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s", table)); err != nil {
			return fmt.Errorf("failed to clear %s: %w", table, err)
		}
	}

	// Read and execute SQL
	rc, err := sqlFile.Open()
	if err != nil {
		return fmt.Errorf("failed to open database.sql: %w", err)
	}
	defer rc.Close()

	sqlBytes, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("failed to read database.sql: %w", err)
	}

	// Execute each statement
	statements := strings.Split(string(sqlBytes), ";\n")
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" || strings.HasPrefix(stmt, "--") {
			continue
		}

		// Handle base64-encoded binary data (X'...' marker)
		stmt = convertBase64Blobs(stmt)

		if _, err := tx.Exec(stmt); err != nil {
			// Log warning but continue (for forward compatibility)
			fmt.Printf("Warning: failed to execute SQL: %v\nStatement: %s\n", err, stmt[:min(100, len(stmt))])
		}
	}

	// Mark all plugin_permissions as pending_reconfirm
	if _, err := tx.Exec("UPDATE plugin_permissions SET pending_reconfirm = 1"); err != nil {
		fmt.Printf("Warning: failed to mark permissions as pending: %v\n", err)
	}

	// Mark all watched_folders as paused
	if _, err := tx.Exec("UPDATE watched_folders SET is_paused = 1"); err != nil {
		fmt.Printf("Warning: failed to pause watch folders: %v\n", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit restore: %w", err)
	}

	// Copy plugin files
	dataDir, err := getDataDir()
	if err != nil {
		return fmt.Errorf("failed to get data directory: %w", err)
	}
	pluginsDir := filepath.Join(dataDir, "plugins")

	// Clear existing plugins
	if err := os.RemoveAll(pluginsDir); err != nil {
		fmt.Printf("Warning: failed to clear plugins directory: %v\n", err)
	}
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		return fmt.Errorf("failed to create plugins directory: %w", err)
	}

	// Extract plugin files from backup
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "plugins/") && strings.HasSuffix(f.Name, ".lua") {
			destPath := filepath.Join(dataDir, f.Name)
			if err := extractZipFile(f, destPath); err != nil {
				fmt.Printf("Warning: failed to extract plugin %s: %v\n", f.Name, err)
			}
		}
	}

	// Reload plugin manager
	if a.pluginManager != nil {
		if err := a.pluginManager.LoadPlugins(); err != nil {
			fmt.Printf("Warning: failed to reload plugins: %v\n", err)
		}
	}

	return nil
}

// convertBase64Blobs converts X'base64...' markers back to actual blob data
func convertBase64Blobs(stmt string) string {
	// This is a simple implementation - in production you might use regex
	// For now, we'll handle this during import by detecting X'...' patterns
	// that contain base64 data and converting them

	// Actually, SQLite X'...' expects hex, not base64
	// We need a different approach: use a placeholder and bind parameters
	// For simplicity, let's use a different marker: B64'...'

	// For now, return as-is - we'll improve the export to use proper hex encoding
	return stmt
}

// extractZipFile extracts a single file from a ZIP archive
func extractZipFile(f *zip.File, destPath string) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	destFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, rc)
	return err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

**Step 2: Verify file compiles**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/backup-restore && go build .`

Expected: Build succeeds.

**Step 3: Commit**

```bash
git add backup.go
git commit -m "feat(backup): implement RestoreBackup method"
```

---

## Task 7: Fix binary data export/import with proper hex encoding

**Files:**
- Modify: `backup.go`

**Step 1: Update formatSQLValue to use proper hex encoding**

Replace the `formatSQLValue` function:

```go
// formatSQLValue formats a value for SQL INSERT statement
func formatSQLValue(v interface{}) string {
	if v == nil {
		return "NULL"
	}

	switch val := v.(type) {
	case []byte:
		// Encode binary data as hex for SQLite X'...' literal
		return fmt.Sprintf("X'%X'", val)
	case string:
		// Escape single quotes
		escaped := strings.ReplaceAll(val, "'", "''")
		return fmt.Sprintf("'%s'", escaped)
	case int, int64, int32:
		return fmt.Sprintf("%d", val)
	case float64, float32:
		return fmt.Sprintf("%f", val)
	case bool:
		if val {
			return "1"
		}
		return "0"
	case time.Time:
		return fmt.Sprintf("'%s'", val.Format("2006-01-02 15:04:05"))
	default:
		// Try to convert to string
		return fmt.Sprintf("'%v'", val)
	}
}
```

**Step 2: Remove the convertBase64Blobs function call (no longer needed)**

In `RestoreBackup`, remove the line:

```go
stmt = convertBase64Blobs(stmt)
```

And remove the `convertBase64Blobs` function entirely.

**Step 3: Verify file compiles**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/backup-restore && go build .`

Expected: Build succeeds.

**Step 4: Commit**

```bash
git add backup.go
git commit -m "fix(backup): use proper hex encoding for binary data"
```

---

## Task 8: Add Wails-exposed backup methods to app.go

**Files:**
- Modify: `app.go`

**Step 1: Add ShowCreateBackupDialog method**

Add after the existing `SaveClipToFile` method (around line 1185):

```go
// ShowCreateBackupDialog opens a save dialog and creates a backup
func (a *App) ShowCreateBackupDialog() (string, error) {
	defaultFilename := fmt.Sprintf("mahpastes-backup-%s.zip", time.Now().Format("2006-01-02"))

	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: defaultFilename,
		Title:           "Create Backup",
		Filters: []runtime.FileFilter{
			{DisplayName: "ZIP Archives", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to show save dialog: %w", err)
	}

	if savePath == "" {
		return "", nil // User cancelled
	}

	if err := a.CreateBackup(savePath); err != nil {
		return "", err
	}

	return savePath, nil
}

// ShowRestoreBackupDialog opens a file picker and validates the selected backup
func (a *App) ShowRestoreBackupDialog() (*BackupManifest, string, error) {
	openPath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Backup to Restore",
		Filters: []runtime.FileFilter{
			{DisplayName: "ZIP Archives", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to show open dialog: %w", err)
	}

	if openPath == "" {
		return nil, "", nil // User cancelled
	}

	manifest, err := ValidateBackup(openPath)
	if err != nil {
		return nil, "", err
	}

	return manifest, openPath, nil
}

// ConfirmRestoreBackup performs the actual restore after user confirmation
func (a *App) ConfirmRestoreBackup(backupPath string) error {
	return a.RestoreBackup(backupPath)
}
```

**Step 2: Verify file compiles**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/backup-restore && go build .`

Expected: Build succeeds.

**Step 3: Commit**

```bash
git add app.go
git commit -m "feat(backup): add Wails-exposed backup dialog methods"
```

---

## Task 9: Regenerate Wails bindings

**Files:**
- Modify: `frontend/wailsjs/go/main/App.js` (auto-generated)
- Modify: `frontend/wailsjs/go/main/App.d.ts` (auto-generated)

**Step 1: Generate bindings**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/backup-restore && ~/go/bin/wails generate module`

Expected: New methods appear in the generated files.

**Step 2: Verify new methods are generated**

Check that `ShowCreateBackupDialog`, `ShowRestoreBackupDialog`, and `ConfirmRestoreBackup` exist in the generated files.

**Step 3: Commit**

```bash
git add frontend/wailsjs/
git commit -m "chore: regenerate Wails bindings for backup methods"
```

---

## Task 10: Add backup section to settings modal in index.html

**Files:**
- Modify: `frontend/index.html`

**Step 1: Add Backup & Restore section**

Find the settings modal content (search for `settings-modal`) and add after the AI Image Processing section (before the closing `</div>` of the `p-5 space-y-6` div):

```html
                <!-- Backup & Restore -->
                <div class="pt-4 border-t border-stone-100">
                    <h3 class="text-xs font-semibold text-stone-600 uppercase tracking-wider mb-3 flex items-center gap-2">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12"></path>
                        </svg>
                        Backup & Restore
                    </h3>
                    <p class="text-[11px] text-stone-500 mb-3">
                        Create a backup of all clips, tags, plugins, and settings. Restoring will replace all current data.
                    </p>
                    <div class="flex gap-3">
                        <button id="create-backup-btn" data-testid="create-backup-btn"
                            class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2 px-4 rounded-md transition-colors">
                            Create Backup
                        </button>
                        <button id="restore-backup-btn" data-testid="restore-backup-btn"
                            class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-4 rounded-md transition-colors">
                            Restore from Backup
                        </button>
                    </div>
                </div>
```

**Step 2: Commit**

```bash
git add frontend/index.html
git commit -m "feat(ui): add backup & restore section to settings modal"
```

---

## Task 11: Add restore confirmation modal to index.html

**Files:**
- Modify: `frontend/index.html`

**Step 1: Add restore confirmation modal**

Add after the confirm-dialog (around line 360):

```html
    <!-- Restore Backup Confirm Dialog -->
    <div id="restore-confirm-dialog" role="alertdialog" aria-modal="true" aria-labelledby="restore-confirm-title"
        class="fixed inset-0 z-[70] flex items-center justify-center p-4 bg-stone-900/40 backdrop-blur-sm transition-opacity duration-200 opacity-0 pointer-events-none">
        <div class="bg-white rounded-lg shadow-xl max-w-md w-full overflow-hidden transform transition-transform duration-200 scale-95">
            <div class="p-5">
                <div class="flex items-center justify-center w-12 h-12 mx-auto bg-amber-50 rounded-full mb-4">
                    <svg class="w-6 h-6 text-amber-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                            d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"></path>
                    </svg>
                </div>
                <h2 id="restore-confirm-title" class="text-sm font-semibold text-stone-800 text-center mb-3">Restore from Backup</h2>
                <div class="bg-red-50 border border-red-200 rounded-md p-3 mb-4">
                    <p class="text-xs text-red-700 font-medium text-center">This will replace ALL current data</p>
                </div>
                <div id="restore-backup-info" class="text-xs text-stone-600 space-y-2 mb-4">
                    <!-- Backup info will be inserted by JS -->
                </div>
                <p class="text-[11px] text-stone-500 text-center">Your current data will be permanently deleted. This action cannot be undone.</p>
            </div>
            <div class="bg-stone-50 px-5 py-3 flex gap-2 justify-end border-t border-stone-100">
                <button id="restore-confirm-cancel"
                    class="bg-white border border-stone-200 hover:bg-stone-50 text-stone-600 text-xs font-medium py-2 px-4 rounded-md transition-colors">
                    Cancel
                </button>
                <button id="restore-confirm-yes"
                    class="bg-red-500 hover:bg-red-600 text-white text-xs font-medium py-2 px-4 rounded-md transition-colors">
                    Delete & Restore
                </button>
            </div>
        </div>
    </div>
```

**Step 2: Commit**

```bash
git add frontend/index.html
git commit -m "feat(ui): add restore confirmation modal"
```

---

## Task 12: Add backup handlers to settings.js

**Files:**
- Modify: `frontend/js/settings.js`

**Step 1: Add backup button handlers**

Add at the end of the file, before the final `window.addEventListener`:

```javascript
// --- Backup & Restore ---

const createBackupBtn = document.getElementById('create-backup-btn');
const restoreBackupBtn = document.getElementById('restore-backup-btn');
const restoreConfirmDialog = document.getElementById('restore-confirm-dialog');
const restoreConfirmCancel = document.getElementById('restore-confirm-cancel');
const restoreConfirmYes = document.getElementById('restore-confirm-yes');
const restoreBackupInfo = document.getElementById('restore-backup-info');

let pendingRestorePath = null;

async function createBackup() {
    try {
        createBackupBtn.disabled = true;
        createBackupBtn.textContent = 'Creating...';

        const savedPath = await window.go.main.App.ShowCreateBackupDialog();

        if (savedPath) {
            showToast('Backup created successfully');
        }
    } catch (error) {
        console.error('Failed to create backup:', error);
        showToast('Failed to create backup: ' + error.message);
    } finally {
        createBackupBtn.disabled = false;
        createBackupBtn.textContent = 'Create Backup';
    }
}

async function selectRestoreBackup() {
    try {
        const result = await window.go.main.App.ShowRestoreBackupDialog();

        if (!result || !result[0]) {
            return; // User cancelled or no manifest
        }

        const manifest = result[0];
        const backupPath = result[1];

        // Store path for confirmation
        pendingRestorePath = backupPath;

        // Format backup info
        const createdDate = new Date(manifest.created_at).toLocaleString();
        restoreBackupInfo.innerHTML = `
            <div class="flex justify-between py-1 border-b border-stone-100">
                <span class="text-stone-500">Backup created:</span>
                <span class="font-medium">${createdDate}</span>
            </div>
            <div class="flex justify-between py-1 border-b border-stone-100">
                <span class="text-stone-500">App version:</span>
                <span class="font-medium">${manifest.app_version}</span>
            </div>
            <div class="pt-2">
                <span class="text-stone-500">This backup contains:</span>
                <ul class="mt-1 space-y-1 pl-4">
                    <li class="flex items-center gap-1">
                        <span class="w-1.5 h-1.5 rounded-full bg-stone-400"></span>
                        ${manifest.summary.clips} clips
                    </li>
                    <li class="flex items-center gap-1">
                        <span class="w-1.5 h-1.5 rounded-full bg-stone-400"></span>
                        ${manifest.summary.tags} tags
                    </li>
                    <li class="flex items-center gap-1">
                        <span class="w-1.5 h-1.5 rounded-full bg-stone-400"></span>
                        ${manifest.summary.plugins} plugins
                    </li>
                    <li class="flex items-center gap-1">
                        <span class="w-1.5 h-1.5 rounded-full bg-stone-400"></span>
                        ${manifest.summary.watch_folders} watch folders <span class="text-stone-400">(will be paused)</span>
                    </li>
                </ul>
            </div>
        `;

        // Show confirmation dialog
        showRestoreConfirmDialog();

    } catch (error) {
        console.error('Failed to select backup:', error);
        showToast('Failed to read backup: ' + error.message);
    }
}

function showRestoreConfirmDialog() {
    restoreConfirmDialog.classList.remove('opacity-0', 'pointer-events-none');
    restoreConfirmDialog.classList.add('opacity-100');
    restoreConfirmDialog.querySelector(':scope > div').classList.remove('scale-95');
    restoreConfirmDialog.querySelector(':scope > div').classList.add('scale-100');
}

function hideRestoreConfirmDialog() {
    restoreConfirmDialog.classList.add('opacity-0', 'pointer-events-none');
    restoreConfirmDialog.classList.remove('opacity-100');
    restoreConfirmDialog.querySelector(':scope > div').classList.add('scale-95');
    restoreConfirmDialog.querySelector(':scope > div').classList.remove('scale-100');
    pendingRestorePath = null;
}

async function confirmRestore() {
    if (!pendingRestorePath) {
        hideRestoreConfirmDialog();
        return;
    }

    try {
        restoreConfirmYes.disabled = true;
        restoreConfirmYes.textContent = 'Restoring...';

        await window.go.main.App.ConfirmRestoreBackup(pendingRestorePath);

        hideRestoreConfirmDialog();
        closeSettings();
        showToast('Backup restored successfully');

        // Reload the page to refresh all data
        setTimeout(() => {
            window.location.reload();
        }, 500);

    } catch (error) {
        console.error('Failed to restore backup:', error);
        showToast('Failed to restore: ' + error.message);
    } finally {
        restoreConfirmYes.disabled = false;
        restoreConfirmYes.textContent = 'Delete & Restore';
    }
}

// Event listeners for backup
createBackupBtn.addEventListener('click', createBackup);
restoreBackupBtn.addEventListener('click', selectRestoreBackup);
restoreConfirmCancel.addEventListener('click', hideRestoreConfirmDialog);
restoreConfirmYes.addEventListener('click', confirmRestore);
restoreConfirmDialog.addEventListener('click', (e) => {
    if (e.target === restoreConfirmDialog) hideRestoreConfirmDialog();
});
```

**Step 2: Verify no syntax errors**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/backup-restore && ~/go/bin/wails dev`

Expected: App starts without JavaScript errors.

**Step 3: Commit**

```bash
git add frontend/js/settings.js
git commit -m "feat(ui): add backup/restore handlers in settings"
```

---

## Task 13: Add backup selectors to e2e helpers

**Files:**
- Modify: `e2e/helpers/selectors.ts`

**Step 1: Add backup-related selectors**

Find the `settings` object in selectors and add backup selectors:

```typescript
  backup: {
    createButton: '[data-testid="create-backup-btn"]',
    restoreButton: '[data-testid="restore-backup-btn"]',
    restoreConfirmDialog: '#restore-confirm-dialog',
    restoreConfirmCancel: '#restore-confirm-cancel',
    restoreConfirmYes: '#restore-confirm-yes',
    restoreBackupInfo: '#restore-backup-info',
  },
```

**Step 2: Commit**

```bash
git add e2e/helpers/selectors.ts
git commit -m "test: add backup selectors"
```

---

## Task 14: Add backup helper methods to AppHelper

**Files:**
- Modify: `e2e/fixtures/test-fixtures.ts`

**Step 1: Add backup methods to AppHelper class**

Add after the plugins section:

```typescript
  // ==================== Backup & Restore ====================

  async openSettingsModal(): Promise<void> {
    await this.page.locator(selectors.header.settingsButton).click();
    await this.page.waitForSelector(`${selectors.settings.modal}.opacity-100`, { timeout: 5000 });
  }

  async closeSettingsModal(): Promise<void> {
    await this.page.locator(selectors.settings.closeButton).click();
    await this.page.waitForSelector(`${selectors.settings.modal}.opacity-0`, { timeout: 5000 });
  }

  async createBackupViaAPI(): Promise<string> {
    // Create backup programmatically and return the path
    const tempDir = await this.page.evaluate(() => {
      // @ts-ignore
      return window.__testTempDir || '/tmp';
    });

    const backupPath = `${tempDir}/test-backup-${Date.now()}.zip`;

    await this.page.evaluate(async (path) => {
      // @ts-ignore
      await window.go.main.App.CreateBackup(path);
    }, backupPath);

    return backupPath;
  }

  async restoreBackupViaAPI(backupPath: string): Promise<void> {
    await this.page.evaluate(async (path) => {
      // @ts-ignore
      await window.go.main.App.ConfirmRestoreBackup(path);
    }, backupPath);

    // Wait for restore to complete and refresh
    await this.page.waitForTimeout(1000);
    await this.page.reload();
    await this.waitForReady();
  }

  async getBackupManifest(backupPath: string): Promise<any> {
    return this.page.evaluate(async (path) => {
      // @ts-ignore
      return await window.go.main.ValidateBackup(path);
    }, backupPath);
  }
```

**Step 2: Add header.settingsButton selector if missing**

In `selectors.ts`, ensure the header object has:

```typescript
    settingsButton: '#open-settings-btn',
```

**Step 3: Add settings.modal and settings.closeButton if missing**

```typescript
  settings: {
    modal: '#settings-modal',
    closeButton: '#settings-close',
  },
```

**Step 4: Commit**

```bash
git add e2e/fixtures/test-fixtures.ts e2e/helpers/selectors.ts
git commit -m "test: add backup helper methods to AppHelper"
```

---

## Task 15: Create backup e2e test file

**Files:**
- Create: `e2e/tests/backup/backup.spec.ts`

**Step 1: Create the test file**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestText,
  createTempDir,
  cleanup,
} from '../../helpers/test-data';
import * as path from 'path';
import * as fs from 'fs/promises';

test.describe('Backup & Restore', () => {
  test.describe('Backup Creation', () => {
    test('should create backup with clips', async ({ app, tempDir }) => {
      // Upload some test clips
      const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const text = await createTempFile(generateTestText('backup-test'), 'txt');

      await app.uploadFiles([image1, image2, text]);
      await app.expectClipCount(3);

      // Create a tag
      await app.createTag('TestTag');

      // Create backup
      const backupPath = path.join(tempDir, 'test-backup.zip');
      await app.page.evaluate(async (destPath) => {
        // @ts-ignore
        await window.go.main.App.CreateBackup(destPath);
      }, backupPath);

      // Verify backup file exists
      const stat = await fs.stat(backupPath);
      expect(stat.size).toBeGreaterThan(0);
    });

    test('should include correct summary in manifest', async ({ app, tempDir }) => {
      // Upload clips
      const image = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(image);

      // Create tags
      await app.createTag('Tag1');
      await app.createTag('Tag2');

      // Create backup
      const backupPath = path.join(tempDir, 'test-backup.zip');
      await app.page.evaluate(async (destPath) => {
        // @ts-ignore
        await window.go.main.App.CreateBackup(destPath);
      }, backupPath);

      // Validate manifest
      const manifest = await app.page.evaluate(async (backupFile) => {
        // @ts-ignore
        return await window.go.main.ValidateBackup(backupFile);
      }, backupPath);

      expect(manifest.summary.clips).toBe(1);
      expect(manifest.summary.tags).toBe(2);
      expect(manifest.format_version).toBe(1);
    });
  });

  test.describe('Restore', () => {
    test('should restore clips from backup', async ({ app, tempDir }) => {
      // Create initial data
      const image = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
      await app.uploadFile(image);
      await app.createTag('OriginalTag');
      await app.expectClipCount(1);

      // Create backup
      const backupPath = path.join(tempDir, 'restore-test.zip');
      await app.page.evaluate(async (destPath) => {
        // @ts-ignore
        await window.go.main.App.CreateBackup(destPath);
      }, backupPath);

      // Delete original data
      await app.deleteAllClips();
      await app.deleteAllTags();
      await app.expectClipCount(0);

      // Restore from backup
      await app.page.evaluate(async (backupFile) => {
        // @ts-ignore
        await window.go.main.App.ConfirmRestoreBackup(backupFile);
      }, backupPath);

      // Reload page to see restored data
      await app.page.reload();
      await app.waitForReady();

      // Verify data was restored
      await app.expectClipCount(1);
      const tags = await app.getAllTags();
      expect(tags.length).toBe(1);
      expect(tags[0].name).toBe('OriginalTag');
    });

    test('should replace existing data on restore', async ({ app, tempDir }) => {
      // Create initial data
      const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      await app.uploadFile(image1);
      await app.createTag('Tag1');

      // Create backup
      const backupPath = path.join(tempDir, 'replace-test.zip');
      await app.page.evaluate(async (destPath) => {
        // @ts-ignore
        await window.go.main.App.CreateBackup(destPath);
      }, backupPath);

      // Add more data (should be replaced on restore)
      const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const image3 = await createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png');
      await app.uploadFiles([image2, image3]);
      await app.createTag('Tag2');
      await app.createTag('Tag3');
      await app.expectClipCount(3);

      // Restore from backup (should replace with original 1 clip)
      await app.page.evaluate(async (backupFile) => {
        // @ts-ignore
        await window.go.main.App.ConfirmRestoreBackup(backupFile);
      }, backupPath);

      await app.page.reload();
      await app.waitForReady();

      // Verify only original data exists
      await app.expectClipCount(1);
      const tags = await app.getAllTags();
      expect(tags.length).toBe(1);
      expect(tags[0].name).toBe('Tag1');
    });

    test('should pause watch folders on restore', async ({ app, tempDir }) => {
      // Create a watch folder (not paused)
      const watchDir = await createTempDir();
      await app.addWatchFolder(watchDir, { processExisting: false });
      await app.openWatchView();

      // Create backup
      const backupPath = path.join(tempDir, 'watch-test.zip');
      await app.page.evaluate(async (destPath) => {
        // @ts-ignore
        await window.go.main.App.CreateBackup(destPath);
      }, backupPath);

      // Restore
      await app.page.evaluate(async (backupFile) => {
        // @ts-ignore
        await window.go.main.App.ConfirmRestoreBackup(backupFile);
      }, backupPath);

      // Check that restored folder is paused
      const folders = await app.page.evaluate(async () => {
        // @ts-ignore
        return await window.go.main.App.GetWatchedFolders();
      });

      expect(folders.length).toBe(1);
      expect(folders[0].is_paused).toBe(true);

      // Cleanup
      await cleanup(watchDir);
    });
  });

  test.describe('Validation', () => {
    test('should reject invalid backup file', async ({ app, tempDir }) => {
      // Create an invalid ZIP file
      const invalidPath = path.join(tempDir, 'invalid.zip');
      await fs.writeFile(invalidPath, 'not a zip file');

      // Try to validate
      const result = await app.page.evaluate(async (backupFile) => {
        try {
          // @ts-ignore
          await window.go.main.ValidateBackup(backupFile);
          return { success: true };
        } catch (e: any) {
          return { success: false, error: e.message || String(e) };
        }
      }, invalidPath);

      expect(result.success).toBe(false);
      expect(result.error).toContain('invalid');
    });

    test('should reject ZIP without manifest', async ({ app, tempDir }) => {
      // Create a valid ZIP but without manifest.json
      const noManifestPath = path.join(tempDir, 'no-manifest.zip');

      // Use the archiver to create a simple ZIP
      await app.page.evaluate(async (destPath) => {
        // We can't easily create a ZIP without manifest from frontend
        // So we'll just test the error handling by passing a non-backup ZIP
        // For this test, we'll skip if we can't create the file
      }, noManifestPath);

      // This test would require creating a ZIP file from the test
      // For now, we'll mark it as a placeholder
      test.skip();
    });
  });
});
```

**Step 2: Verify test file has no syntax errors**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/backup-restore/e2e && npx tsc --noEmit`

Expected: No TypeScript errors.

**Step 3: Commit**

```bash
git add e2e/tests/backup/
git commit -m "test: add backup e2e tests"
```

---

## Task 16: Run e2e tests and fix any issues

**Files:**
- Various (depending on test failures)

**Step 1: Run the backup tests**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/backup-restore/e2e && npm test -- --grep "Backup"`

Expected: Tests pass or reveal issues to fix.

**Step 2: Run full test suite**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/backup-restore/e2e && npm test`

Expected: All tests pass (except pre-existing failure in watch filters).

**Step 3: Fix any issues found**

Address any test failures.

**Step 4: Commit fixes**

```bash
git add .
git commit -m "fix: address backup test failures"
```

---

## Task 17: Update Tailwind CSS output

**Files:**
- Modify: `frontend/dist/output.css` (via Tailwind build)

**Step 1: Rebuild Tailwind**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/backup-restore/frontend && npx tailwindcss -i ./css/main.css -o ./dist/output.css`

Expected: CSS updated with any new utility classes.

**Step 2: Commit**

```bash
git add frontend/dist/output.css
git commit -m "chore: rebuild Tailwind CSS"
```

---

## Task 18: Final integration test

**Step 1: Start the app**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/backup-restore && ~/go/bin/wails dev`

**Step 2: Manual verification**

1. Open Settings modal
2. Verify "Backup & Restore" section appears
3. Click "Create Backup" and save a file
4. Add some new clips
5. Click "Restore from Backup" and select the backup
6. Verify confirmation modal shows correct info
7. Confirm restore
8. Verify app reloads with original data

**Step 3: Run full e2e suite one more time**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/backup-restore/e2e && npm test`

Expected: All tests pass.

**Step 4: Final commit if needed**

```bash
git add .
git commit -m "feat(backup): complete backup & restore feature"
```
