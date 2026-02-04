package main

import (
	"archive/zip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	BackupFormatVersion = 1
	AppVersion          = "1.0.0" // TODO: Get from build info
)

// BackupManifest describes the contents of a backup file
type BackupManifest struct {
	FormatVersion int           `json:"format_version"`
	AppVersion    string        `json:"app_version"`
	CreatedAt     time.Time     `json:"created_at"`
	Platform      string        `json:"platform"`
	Summary       BackupSummary `json:"summary"`
	Excluded      []string      `json:"excluded"`
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
		var sqlValues []string

		for i, col := range columns {
			colNames = append(colNames, col)
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

	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("error iterating rows in %s: %w", tableName, err)
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
	return runtime.GOOS
}

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
			if err := extractZipFile(f, destPath, dataDir); err != nil {
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

// extractZipFile extracts a single file from a ZIP archive to destPath.
// baseDir is used to validate the path stays within allowed directory (security).
func extractZipFile(f *zip.File, destPath string, baseDir string) error {
	// Security: Validate path doesn't escape base directory (prevent path traversal)
	cleanDest := filepath.Clean(destPath)
	cleanBase := filepath.Clean(baseDir)
	if !strings.HasPrefix(cleanDest, cleanBase+string(os.PathSeparator)) {
		return fmt.Errorf("invalid file path in ZIP: %s (path traversal attempt)", f.Name)
	}

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
