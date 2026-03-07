package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	_ "modernc.org/sqlite"
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

	// Configure pragmas in the DSN so they are applied to every pooled
	// connection, not just the first one (db.Exec("PRAGMA ...") only
	// targets whichever connection the pool picks for that call).
	dsn := dbPath +
		"?_pragma=busy_timeout%3D5000" +
		"&_pragma=journal_mode%3Dwal" +
		"&_pragma=foreign_keys%3Don"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
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
	// Migrate: Add content_hash column for deduplication
	_, _ = db.Exec("ALTER TABLE clips ADD COLUMN content_hash TEXT DEFAULT ''")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_clips_content_hash ON clips(content_hash)")
	// Migrate: Add metadata column for key-value metadata (JSON)
	_, _ = db.Exec("ALTER TABLE clips ADD COLUMN metadata TEXT DEFAULT '{}'")

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

	// Create tags table
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS tags (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		color TEXT NOT NULL
	)`); err != nil {
		log.Printf("Warning: Failed to create tags table: %v", err)
	}

	// Create clip_tags join table
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS clip_tags (
		clip_id INTEGER NOT NULL,
		tag_id INTEGER NOT NULL,
		PRIMARY KEY (clip_id, tag_id),
		FOREIGN KEY (clip_id) REFERENCES clips(id) ON DELETE CASCADE,
		FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
	)`); err != nil {
		log.Printf("Warning: Failed to create clip_tags table: %v", err)
	}

	// Migrate: Add auto_tag_id column to watched_folders if it doesn't exist
	_, _ = db.Exec("ALTER TABLE watched_folders ADD COLUMN auto_tag_id INTEGER")

	// Create plugins table
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS plugins (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		filename TEXT UNIQUE NOT NULL,
		name TEXT NOT NULL,
		version TEXT,
		enabled INTEGER DEFAULT 1,
		status TEXT DEFAULT 'enabled',
		error_count INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		log.Printf("Warning: Failed to create plugins table: %v", err)
	}

	// Create plugin_permissions table
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS plugin_permissions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		plugin_id INTEGER NOT NULL,
		permission_type TEXT NOT NULL,
		path TEXT NOT NULL,
		granted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (plugin_id) REFERENCES plugins(id) ON DELETE CASCADE
	)`); err != nil {
		log.Printf("Warning: Failed to create plugin_permissions table: %v", err)
	}

	// Migrate: Add pending_reconfirm column to plugin_permissions if it doesn't exist
	_, _ = db.Exec("ALTER TABLE plugin_permissions ADD COLUMN pending_reconfirm INTEGER DEFAULT 0")

	// Create plugin_storage table
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS plugin_storage (
		plugin_id INTEGER NOT NULL,
		key TEXT NOT NULL,
		value BLOB,
		PRIMARY KEY (plugin_id, key),
		FOREIGN KEY (plugin_id) REFERENCES plugins(id) ON DELETE CASCADE
	)`); err != nil {
		log.Printf("Warning: Failed to create plugin_storage table: %v", err)
	}

	// Migrate: Add source_url column to plugins if it doesn't exist
	_, _ = db.Exec("ALTER TABLE plugins ADD COLUMN source_url TEXT DEFAULT ''")

	// Create app_settings table
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS app_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`); err != nil {
		log.Printf("Warning: Failed to create app_settings table: %v", err)
	}

	// Create api_keys table
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS api_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		key_hash TEXT NOT NULL UNIQUE,
		key_prefix TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'viewer',
		scoped_tag_id INTEGER,
		is_revoked INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_used_at DATETIME,
		FOREIGN KEY (scoped_tag_id) REFERENCES tags(id) ON DELETE CASCADE
	)`); err != nil {
		log.Printf("Warning: Failed to create api_keys table: %v", err)
	}

	// Backfill content hashes for existing clips that don't have one
	backfillContentHashes(db)

	return db, nil
}

// backfillContentHashes computes SHA-256 hashes for any clips missing a content_hash.
// This runs on startup after migrations so that existing clips get hashes for deduplication.
func backfillContentHashes(db *sql.DB) {
	rows, err := db.Query("SELECT id, data FROM clips WHERE content_hash = ''")
	if err != nil {
		log.Printf("Warning: Failed to query clips for hash backfill: %v", err)
		return
	}
	defer rows.Close()

	updated := 0
	for rows.Next() {
		var id int64
		var data []byte
		if err := rows.Scan(&id, &data); err != nil {
			log.Printf("Warning: Failed to scan clip %d for hash backfill: %v", id, err)
			continue
		}

		hash := sha256.Sum256(data)
		hashHex := hex.EncodeToString(hash[:])

		if _, err := db.Exec("UPDATE clips SET content_hash = ? WHERE id = ?", hashHex, id); err != nil {
			log.Printf("Warning: Failed to update content_hash for clip %d: %v", id, err)
			continue
		}
		updated++
	}

	if err := rows.Err(); err != nil {
		log.Printf("Warning: Error during hash backfill iteration: %v", err)
	}

	if updated > 0 {
		log.Printf("Backfilled content_hash for %d clips", updated)
	}
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
