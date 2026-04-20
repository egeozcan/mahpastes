package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
		FOREIGN KEY (scoped_tag_id) REFERENCES tags(id) ON DELETE SET NULL
	)`); err != nil {
		log.Printf("Warning: Failed to create api_keys table: %v", err)
	}

	// Migrate: api_keys.scoped_tag_id was originally ON DELETE CASCADE, which
	// silently deleted user API keys when a scoped tag was deleted. Rebuild the
	// table with ON DELETE SET NULL so the key row is preserved for audit.
	// SQLite can't change FK actions in place — use the table-rebuild pattern.
	if needsAPIKeysScopedTagMigration(db) {
		if err := migrateAPIKeysScopedTagSetNull(db); err != nil {
			log.Printf("Warning: api_keys FK migration failed: %v", err)
		}
	}

	// Trigger: auto-revoke any key whose scope gets NULLed out (migration or
	// future tag deletes). Preserves the row for audit; denies access because
	// is_revoked = 1 is checked in the auth middleware.
	if _, err := db.Exec(`
		CREATE TRIGGER IF NOT EXISTS api_keys_revoke_on_scope_null
		AFTER UPDATE OF scoped_tag_id ON api_keys
		WHEN NEW.scoped_tag_id IS NULL AND OLD.scoped_tag_id IS NOT NULL
		BEGIN
			UPDATE api_keys SET is_revoked = 1 WHERE id = NEW.id;
		END;
	`); err != nil {
		log.Printf("Warning: failed to create api_keys_revoke_on_scope_null trigger: %v", err)
	}

	// Create shares table (publisher-side publications)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS shares (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		tag_id      INTEGER NOT NULL,
		symkey      BLOB    NOT NULL,
		share_id    BLOB    NOT NULL UNIQUE,
		last_seq    INTEGER NOT NULL DEFAULT 0,
		clips_sent  INTEGER NOT NULL DEFAULT 0,
		status      TEXT    NOT NULL DEFAULT 'active',
		created_at  INTEGER NOT NULL,
		FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
	)`); err != nil {
		log.Printf("Warning: Failed to create shares table: %v", err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_shares_tag_id ON shares(tag_id)`); err != nil {
		log.Printf("Warning: Failed to create idx_shares_tag_id: %v", err)
	}

	// Create follows table (follower-side subscriptions)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS follows (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		remote_peer_id  TEXT    NOT NULL,
		symkey          BLOB    NOT NULL,
		local_tag_id    INTEGER NOT NULL,
		last_seq        INTEGER NOT NULL DEFAULT 0,
		clips_received  INTEGER NOT NULL DEFAULT 0,
		last_seen_at    INTEGER,
		created_at      INTEGER NOT NULL,
		FOREIGN KEY (local_tag_id) REFERENCES tags(id) ON DELETE RESTRICT
	)`); err != nil {
		log.Printf("Warning: Failed to create follows table: %v", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_follows_peer ON follows(remote_peer_id)`); err != nil {
		log.Printf("Warning: Failed to create idx_follows_peer: %v", err)
	}

	// Create share_ring table (persisted envelope ciphertext for catch-up)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS share_ring (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		publication_id INTEGER NOT NULL,
		seq            INTEGER NOT NULL,
		kind           TEXT    NOT NULL,
		envelope_bytes BLOB    NOT NULL,
		ts             INTEGER NOT NULL,
		FOREIGN KEY (publication_id) REFERENCES shares(id) ON DELETE CASCADE
	)`); err != nil {
		log.Printf("Warning: Failed to create share_ring table: %v", err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_share_ring_pub_seq ON share_ring(publication_id, seq)`); err != nil {
		log.Printf("Warning: Failed to create idx_share_ring_pub_seq: %v", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_share_ring_ts ON share_ring(ts)`); err != nil {
		log.Printf("Warning: Failed to create idx_share_ring_ts: %v", err)
	}

	// Add paused column to follows (idempotent — ALTER fails silently if column exists).
	// Shares use the existing status column: status="paused" is a first-class value.
	_, _ = db.Exec("ALTER TABLE follows ADD COLUMN paused INTEGER NOT NULL DEFAULT 0")

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

// migrateFailHook, if non-nil, is invoked inside migrateAPIKeysScopedTagSetNull
// AFTER PRAGMA foreign_keys=OFF but BEFORE the table rebuild commits.
// Returning a non-nil error forces the migration's error path so tests can
// verify the deferred PRAGMA foreign_keys=ON still runs and the connection
// can't be returned to the pool with FK checks disabled. Production code
// leaves this nil.
var migrateFailHook func() error

// needsAPIKeysScopedTagMigration returns true iff the api_keys table's SQL
// still contains "ON DELETE CASCADE" on scoped_tag_id.
func needsAPIKeysScopedTagMigration(db *sql.DB) bool {
	var tableSQL string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='api_keys'`).Scan(&tableSQL)
	if err != nil {
		return false
	}
	return strings.Contains(tableSQL, "scoped_tag_id") && strings.Contains(tableSQL, "ON DELETE CASCADE")
}

// migrateAPIKeysScopedTagSetNull rebuilds api_keys with SET NULL instead of
// CASCADE on scoped_tag_id. Uses the standard SQLite table-rebuild pattern.
// Grabs a dedicated connection so PRAGMA foreign_keys=OFF doesn't leak to
// the pool — and guarantees we re-enable FK checks before the conn returns
// to the pool, even on the error path.
func migrateAPIKeysScopedTagSetNull(db *sql.DB) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn: %w", err)
	}
	// CRITICAL: restore foreign_keys BEFORE conn.Close(). conn.Close() returns
	// the connection to the pool; a FK-disabled pooled conn would silently
	// undermine every future query. LIFO defer: the inner defer runs first.
	defer conn.Close()
	defer func() {
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
			log.Printf("CRITICAL: failed to re-enable foreign_keys on migration conn: %v", err)
		}
	}()

	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return fmt.Errorf("disable FK: %w", err)
	}

	if migrateFailHook != nil {
		if err := migrateFailHook(); err != nil {
			return fmt.Errorf("test hook: %w", err)
		}
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE api_keys_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			key_prefix TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'viewer',
			scoped_tag_id INTEGER,
			is_revoked INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used_at DATETIME,
			FOREIGN KEY (scoped_tag_id) REFERENCES tags(id) ON DELETE SET NULL
		)`); err != nil {
		return fmt.Errorf("create new: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO api_keys_new (id, name, key_hash, key_prefix, role, scoped_tag_id, is_revoked, created_at, last_used_at)
		SELECT id, name, key_hash, key_prefix, role, scoped_tag_id, is_revoked, created_at, last_used_at FROM api_keys`); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE api_keys`); err != nil {
		return fmt.Errorf("drop old: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE api_keys_new RENAME TO api_keys`); err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	// Deferred PRAGMA foreign_keys=ON runs next, then conn.Close().
	return nil
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
