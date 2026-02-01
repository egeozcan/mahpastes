package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// getDataDir returns the OS-appropriate data directory for the app
// If MAHPASTES_DATA_DIR is set, it overrides the default location (useful for testing)
func getDataDir() (string, error) {
	// Check for test/custom override
	if customDir := os.Getenv("MAHPASTES_DATA_DIR"); customDir != "" {
		if err := os.MkdirAll(customDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create custom data directory: %w", err)
		}
		return customDir, nil
	}

	var baseDir string

	switch runtime.GOOS {
	case "darwin":
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		baseDir = filepath.Join(homeDir, "Library", "Application Support", "mahpastes")
	case "windows":
		baseDir = filepath.Join(os.Getenv("APPDATA"), "mahpastes")
	default: // Linux and others
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		baseDir = filepath.Join(homeDir, ".config", "mahpastes")
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create data directory: %w", err)
	}

	return baseDir, nil
}

// initDB initializes the SQLite database and creates the 'clips' table if it doesn't exist
func initDB() (*sql.DB, error) {
	dataDir, err := getDataDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "clips.db")
	log.Printf("Using database at: %s", dbPath)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	// Enable WAL mode for better concurrent access
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		log.Printf("Warning: Failed to enable WAL mode: %v", err)
	}

	createTableSQL := `
    CREATE TABLE IF NOT EXISTS clips (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        content_type TEXT NOT NULL,
        data BLOB NOT NULL,
        filename TEXT,
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );`

	if _, err = db.Exec(createTableSQL); err != nil {
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	// Migrate: Add is_archived column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE clips ADD COLUMN is_archived INTEGER DEFAULT 0")
	// Migrate: Add expires_at column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE clips ADD COLUMN expires_at DATETIME")

	// Create settings table
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`); err != nil {
		log.Printf("Warning: Failed to create settings table: %v", err)
	}

	// Create watched_folders table
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS watched_folders (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		path TEXT NOT NULL UNIQUE,
		filter_mode TEXT NOT NULL DEFAULT 'all',
		filter_presets TEXT,
		filter_regex TEXT,
		process_existing INTEGER DEFAULT 0,
		auto_archive INTEGER DEFAULT 0,
		is_paused INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		log.Printf("Warning: Failed to create watched_folders table: %v", err)
	}

	// Initialize global watch pause setting if not exists
	if _, err := db.Exec(`INSERT OR IGNORE INTO settings (key, value) VALUES ('global_watch_paused', 'false')`); err != nil {
		log.Printf("Warning: Failed to initialize global_watch_paused setting: %v", err)
	}

	return db, nil
}

// startCleanupJob deletes expired clips every minute
func startCleanupJob(db *sql.DB) {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for range ticker.C {
			result, err := db.Exec("DELETE FROM clips WHERE expires_at IS NOT NULL AND expires_at <= CURRENT_TIMESTAMP")
			if err != nil {
				log.Printf("Failed to delete expired clips: %v\n", err)
			} else {
				rows, _ := result.RowsAffected()
				if rows > 0 {
					log.Printf("Cleaned up %d expired clips\n", rows)
				}
			}
		}
	}()
}
