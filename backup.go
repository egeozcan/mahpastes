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

// Suppress unused import warnings - these will be used in later tasks
var _ = zip.Store
var _ = sql.ErrNoRows
var _ = base64.StdEncoding
var _ = json.Marshal
var _ = fmt.Sprint
var _ = io.Copy
var _ = os.Create
var _ = filepath.Join
var _ = time.Now
