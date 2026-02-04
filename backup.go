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

	return count, nil
}

// Suppress unused import warnings - these will be used in later tasks
var (
	_ = zip.Store
	_ = json.Marshal
	_ = os.Create
	_ = filepath.Join
)

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
