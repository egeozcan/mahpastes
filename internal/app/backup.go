package app

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"go-clipboard/plugin"
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

// BackupInspection holds pre-restore state captured before any restore side effect.
type BackupInspection struct {
	// HasIdentity is true when the backup ZIP contains share_identity.key.
	HasIdentity bool `json:"has_identity"`
	// TargetHasIdentity is true when this install already has share_identity.key on disk.
	TargetHasIdentity bool `json:"target_has_identity"`
	// TargetPublicationTags are tag names of currently-active publications on this
	// install (captured before restore so the UI can warn the user).
	TargetPublicationTags []string `json:"target_publication_tags"`
}

// BackupInspect opens a backup ZIP and captures pre-restore state needed for the
// identity-policy prompt. It performs no write operations — safe to call before
// showing the confirm dialog.
func (a *App) BackupInspect(backupPath string) (*BackupInspection, error) {
	r, err := zip.OpenReader(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open backup: %w", err)
	}
	defer r.Close()

	var hasIdentity bool
	for _, f := range r.File {
		if f.Name == ShareIdentityFile {
			hasIdentity = true
			break
		}
	}

	dataDir, err := getDataDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get data directory: %w", err)
	}
	identityPath := filepath.Join(dataDir, ShareIdentityFile)
	_, statErr := os.Stat(identityPath)
	targetHasIdentity := statErr == nil

	var pubTags []string
	rows, err := a.db.Query(
		`SELECT t.name FROM shares s JOIN tags t ON t.id = s.tag_id WHERE s.status = 'active' ORDER BY t.name`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query active publications: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan publication tag: %w", err)
		}
		pubTags = append(pubTags, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate publication tags: %w", err)
	}

	return &BackupInspection{
		HasIdentity:           hasIdentity,
		TargetHasIdentity:     targetHasIdentity,
		TargetPublicationTags: pubTags,
	}, nil
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

// exportTableToSQL exports a table to SQL INSERT statements. db is an
// sqlQueryer rather than a *sql.DB so exportDatabaseToSQL can hand it a
// transaction and have every table read from one snapshot.
func exportTableToSQL(db sqlQueryer, tableName string, w io.Writer, excludeCallback func(map[string]interface{}) bool) (int, error) {
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
	// Exclude restores. The ZIP pairs two stores that only mean anything
	// together — the database snapshot taken below, and the share_identity.key
	// copied in beside it further down — and a restore slipping between them
	// replaces both, so the backup ends up holding database A's active
	// publications next to install B's identity. That ZIP validates, restores,
	// and then silently kills every share string the captured publications ever
	// handed out, because the peer id they name is no longer the one the
	// restored install runs as. Held for the whole function, not just the
	// export: the identity copy is the second half of the pair.
	//
	// A read lock, because excluding restores is the entire requirement. Two
	// concurrent backups stage into separate temp dirs and neither writes
	// anything the other reads. Taking the write lock instead would stall every
	// tag mutation — which now reads this lock — for the length of a full SQL
	// export, a cost with nothing to buy it.
	a.backupRestoreMu.RLock()
	defer a.backupRestoreMu.RUnlock()

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

	// Copy share identity file if present
	identitySrc := filepath.Join(dataDir, ShareIdentityFile)
	if _, err := os.Stat(identitySrc); err == nil {
		identityDst := filepath.Join(tempDir, ShareIdentityFile)
		if err := copyFile(identitySrc, identityDst); err != nil {
			// Log warning but continue — backup must not fail over identity
			fmt.Printf("Warning: failed to copy %s: %v\n", ShareIdentityFile, err)
		}
	} else if !os.IsNotExist(err) {
		// Unexpected stat error — log and continue
		fmt.Printf("Warning: failed to stat %s: %v\n", ShareIdentityFile, err)
	}

	// Create ZIP file
	if err := createZipFromDir(tempDir, destPath); err != nil {
		return fmt.Errorf("failed to create ZIP: %w", err)
	}

	return nil
}

// pluginStoragePasswordKeys collects, per plugin id, the storage keys declared
// type = "password" in that plugin's manifest, plus an id -> name map for the
// excluded report. Loaded plugins contribute their parsed manifest; a plugin
// present in the DB but not currently loaded is parsed from its manifest file
// in the plugins directory, so an enabled-but-unloaded plugin's secrets are
// excluded too. A plugin whose manifest cannot be read contributes nothing —
// filtering too little is fail-safe for correctness of the backup, while
// filtering by guesswork could silently drop non-secret settings.
func (a *App) pluginStoragePasswordKeys(tx sqlQueryer) (map[int64]map[string]bool, map[int64]string) {
	passwordKeys := make(map[int64]map[string]bool)
	names := make(map[int64]string)

	type pluginRow struct {
		id       int64
		filename string
		name     string
	}
	var rows []pluginRow
	rrows, err := tx.Query("SELECT id, filename, name FROM plugins")
	if err == nil {
		defer rrows.Close()
		for rrows.Next() {
			var r pluginRow
			if rrows.Scan(&r.id, &r.filename, &r.name) == nil {
				rows = append(rows, r)
			}
		}
	}

	loaded := make(map[int64]*plugin.Manifest)
	if a.pluginManager != nil {
		for _, p := range a.pluginManager.GetPlugins() {
			if p.Manifest != nil {
				loaded[p.ID] = p.Manifest
			}
		}
	}

	var pluginsDir string
	if a.pluginManager != nil {
		pluginsDir = a.pluginManager.PluginsDir()
	}

	for _, r := range rows {
		names[r.id] = r.name
		manifest := loaded[r.id]
		if manifest == nil && pluginsDir != "" && r.filename != "" {
			if source, readErr := os.ReadFile(filepath.Join(pluginsDir, r.filename)); readErr == nil {
				manifest, _ = plugin.ParseManifest(string(source))
			}
		}
		if manifest == nil {
			continue
		}
		for _, s := range manifest.Settings {
			if s.Type == "password" {
				if passwordKeys[r.id] == nil {
					passwordKeys[r.id] = make(map[string]bool)
				}
				passwordKeys[r.id][s.Key] = true
			}
		}
	}
	return passwordKeys, names
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

	// Every table below is read through one read transaction so the whole
	// export comes from a single snapshot. Exporting table-by-table off the
	// pool let a write land between two tables and skewed cross-table
	// invariants into the backup — most damagingly the share ones, where a
	// clip emission committing between the "shares" and "share_ring" reads
	// produced a backup whose ring held rows above the last_seq it recorded.
	// Restoring that (identity policy "takeover" keeps the share active) made
	// the publisher's next emission reuse a seq already present in the ring,
	// so the INSERT tripped UNIQUE(publication_id, seq), the emission tx
	// failed, and every clip tagged into that share went unpublished until
	// ring eviction cleared the conflict.
	//
	// The database runs in WAL mode (see the DSN pragmas in initDB), so this
	// reader does not block concurrent writers; it just stops seeing them.
	// The snapshot is pinned by the first read below, not by BEGIN.
	tx, err := a.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to begin export transaction: %w", err)
	}
	// Nothing here writes, so the transaction is always rolled back.
	defer tx.Rollback()

	// Write header
	f.WriteString("-- mahpastes backup\n")
	f.WriteString(fmt.Sprintf("-- Created: %s\n", time.Now().Format(time.RFC3339)))
	f.WriteString(fmt.Sprintf("-- Format version: %d\n\n", BackupFormatVersion))

	// Export clips
	f.WriteString("-- Table: clips\n")
	count, err := exportTableToSQL(tx, "clips", f, nil)
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export clips: %w", err)
	}
	summary.Clips = count
	f.WriteString("\n")

	// Export tags
	f.WriteString("-- Table: tags\n")
	count, err = exportTableToSQL(tx, "tags", f, nil)
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export tags: %w", err)
	}
	summary.Tags = count
	f.WriteString("\n")

	// Export clip_tags
	f.WriteString("-- Table: clip_tags\n")
	_, err = exportTableToSQL(tx, "clip_tags", f, nil)
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export clip_tags: %w", err)
	}
	f.WriteString("\n")

	// Export settings (excluding sensitive ones)
	f.WriteString("-- Table: settings\n")
	_, err = exportTableToSQL(tx, "settings", f, func(row map[string]interface{}) bool {
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
	count, err = exportTableToSQL(tx, "watched_folders", f, nil)
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export watched_folders: %w", err)
	}
	summary.WatchFolders = count
	f.WriteString("\n")

	// Export plugins
	f.WriteString("-- Table: plugins\n")
	count, err = exportTableToSQL(tx, "plugins", f, nil)
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export plugins: %w", err)
	}
	summary.Plugins = count
	f.WriteString("\n")

	// Export plugin_storage (skipping values whose key is declared
	// type = "password" in that plugin's manifest — e.g. mahresources'
	// api_token — so secrets do not land in plaintext backup ZIPs).
	passwordKeysByPlugin, pluginNames := a.pluginStoragePasswordKeys(tx)
	f.WriteString("-- Table: plugin_storage\n")
	_, err = exportTableToSQL(tx, "plugin_storage", f, func(row map[string]interface{}) bool {
		pluginID, _ := row["plugin_id"].(int64)
		key, _ := row["key"].(string)
		if key == "" || pluginID == 0 {
			return false
		}
		if passwordKeys := passwordKeysByPlugin[pluginID]; passwordKeys != nil && passwordKeys[key] {
			excluded = append(excluded, fmt.Sprintf("%s:%s", pluginNames[pluginID], key))
			return true
		}
		return false
	})
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export plugin_storage: %w", err)
	}
	f.WriteString("\n")

	// Export plugin_permissions (will be marked as pending_reconfirm on import)
	f.WriteString("-- Table: plugin_permissions\n")
	_, err = exportTableToSQL(tx, "plugin_permissions", f, nil)
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export plugin_permissions: %w", err)
	}
	f.WriteString("\n")

	// Export share tables (shares, follows, share_ring).
	// These are NOT counted in BackupSummary — extending that struct would change
	// the manifest JSON schema and break older backups. The tables are still fully
	// exported and restored; the summary fields just don't reflect their counts.
	f.WriteString("-- Table: shares\n")
	_, err = exportTableToSQL(tx, "shares", f, nil)
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export shares: %w", err)
	}
	f.WriteString("\n")

	f.WriteString("-- Table: follows\n")
	_, err = exportTableToSQL(tx, "follows", f, nil)
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export follows: %w", err)
	}
	f.WriteString("\n")

	// share_ring references shares(id) via FK. Export after shares so that if the
	// SQL is ever replayed from scratch (not used today but good hygiene) the parent
	// rows exist first.
	f.WriteString("-- Table: share_ring\n")
	_, err = exportTableToSQL(tx, "share_ring", f, nil)
	if err != nil {
		return summary, excluded, fmt.Errorf("failed to export share_ring: %w", err)
	}

	return summary, excluded, nil
}

// normalizeShareSeqs lifts every shares.last_seq to at least the highest seq
// its publication holds in share_ring, restoring the invariant that the ring
// never contains rows above the publication's durable boundary.
//
// Emissions allocate seqs as last_seq+1 and both writes commit together, so
// the only way the ring can outrun last_seq is a backup that captured the two
// tables at different points in time. Raising last_seq (rather than deleting
// ring rows) keeps the captured history replayable to followers and makes the
// next emission continue above it.
func normalizeShareSeqs(db sqlExecer) error {
	_, err := db.Exec(`
		UPDATE shares
		SET last_seq = MAX(
			last_seq,
			COALESCE((SELECT MAX(seq) FROM share_ring WHERE publication_id = shares.id), 0)
		)`)
	return err
}

// captureShareStatuses records, grouped by status, the id of every shares row
// that is not already invalid. RestoreBackup calls it inside the restore
// transaction, after the backup's rows have been replayed, so what it captures
// is what the backup said — and what the rows must go back to once the identity
// those shares belong to is installed.
//
// Grouping by the status string rather than reading it as a boolean keeps a
// paused publication paused. Backup SQL is externally supplied, so a status
// this build does not know about is carried through verbatim rather than being
// mapped onto one that is.
func captureShareStatuses(db sqlQueryer) (map[string][]int64, error) {
	rows, err := db.Query(`SELECT id, status FROM shares WHERE status != 'invalid'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byStatus := make(map[string][]int64)
	for rows.Next() {
		var id int64
		var status string
		if err := rows.Scan(&id, &status); err != nil {
			return nil, err
		}
		byStatus[status] = append(byStatus[status], id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return byStatus, nil
}

// restoreShareStatuses puts the captured statuses back on the rows they came
// from. It runs in one transaction: a restore that has adopted the identity
// either has all of its publications back or none of them, never a set of rows
// half of which the user has to notice are missing.
func restoreShareStatuses(db *sql.DB, byStatus map[string][]int64) error {
	if len(byStatus) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for status, ids := range byStatus {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		args := make([]interface{}, 0, len(ids)+1)
		args = append(args, status)
		for _, id := range ids {
			args = append(args, id)
		}
		if _, err := tx.Exec(
			fmt.Sprintf(`UPDATE shares SET status = ? WHERE id IN (%s)`, placeholders), args...,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
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

// RestoreBackup restores data from a backup ZIP file.
// identityPolicy controls how a conflict between the backup's identity and
// this install's identity is resolved:
//   - "takeover": extract identity from backup, overwrite local identity file.
//     Restored shares rows are valid under the adopted identity — no invalidation.
//   - "keep": discard backup identity. Mark restored shares rows invalid because
//     they came from a different peer id.
//   - "none": install is expected to have no prior identity. Extract the
//     backup's identity if the file is still absent on disk; if it is not
//     absent (or the backup carries none) nothing is adopted and the restored
//     shares are invalidated, exactly as under "keep".
//
// Whatever the policy, restored shares rows survive as 'active' only when the
// identity they were created under is the identity the rebuilt manager will
// load. See the decision matrix at the invalidation below.
//
// Any other value returns an error.
//
// The return is named because the deferred share-manager rebuild reports
// through it: a restore that leaves the app unable to share is not a success.
func (a *App) RestoreBackup(backupPath, identityPolicy string) (err error) {
	// Take the write lock: this is the only writer, and it excludes the other
	// restores, the backup exports, and the tag mutations all at once.
	// Everything below is a global swap of state the whole process shares, and
	// the unlock is deferred first so it runs last — after the share-manager
	// rebuild and the hook resume — leaving no instant in which a second
	// restore can observe this one's torn-down manager or its half-replaced
	// database, and none in which a tag mutation can straddle the replacement.
	// See backupRestoreMu's comment for what each of those interleavings
	// produces.
	//
	// Deadlock audit for the span below, which holds the write lock while
	// waiting on other work:
	//
	//   - The hook drain waits on publication goroutines that call
	//     ShareManager.OnClipCreated. ShareManager holds no App reference and
	//     re-enters no App method, so no drained goroutine can need this lock.
	//   - ShareManager.Stop waits on the manager's own loop goroutines, which
	//     likewise only touch the manager, its host and the DB handle.
	//   - WatcherManager.Stop does not wait for its goroutines at all, and
	//     processFile releases the watcher lock before importFile reaches
	//     AddTagToClip, so a watcher import parked at RLock cannot hold
	//     anything Stop needs.
	//   - No reader holds the read lock while waiting on the writer: readers
	//     take it first and only then touch the DB, the hook gate, or files.
	//     In particular the tag mutations release it before EmitEvent, so no
	//     Lua handler ever runs underneath it.
	//
	// Go's RWMutex queues later RLock acquisitions behind a blocked Lock, so
	// once this returns from Lock every tag mutation is either finished or
	// parked outside its transaction — which is exactly the guarantee the row
	// replacement below needs.
	a.backupRestoreMu.Lock()
	defer a.backupRestoreMu.Unlock()

	// Validate identity policy before doing any work.
	switch identityPolicy {
	case "takeover", "keep", "none":
		// valid
	default:
		return fmt.Errorf("invalid identity policy %q: must be \"takeover\", \"keep\", or \"none\"", identityPolicy)
	}

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

	// Resolve the data directory up front: the identity handling and the share
	// manager rebuild both need it, and failing to resolve it must happen
	// before anything destructive runs.
	dataDir, err := getDataDir()
	if err != nil {
		return fmt.Errorf("failed to get data directory: %w", err)
	}

	// Locate the backup's peer identity once. Whether the ZIP carries one is
	// what decides, inside the restore transaction below, whether the restored
	// publications can be valid at all.
	var identityFile *zip.File
	for _, f := range r.File {
		if f.Name == ShareIdentityFile {
			identityFile = f
			break
		}
	}

	// Settle now, before anything destructive, whether this restore will adopt
	// the backup's identity. The restored shares may only stay active if it
	// does, and that verdict has to be available inside the transaction below
	// while the adoption itself can only happen after the commit.
	//
	// For "none" the adoption is conditional on the identity file still being
	// absent, so it is decided by a stat here rather than at extraction time.
	// One stat is enough: the only code that ever creates share_identity.key is
	// LoadOrCreateIdentity, which runs when a ShareManager is constructed — at
	// startup, or in this function's own deferred rebuild — so no writer can
	// appear between this stat and the extraction below.
	identityPath := filepath.Join(dataDir, ShareIdentityFile)
	adoptIdentity := false
	switch identityPolicy {
	case "takeover":
		adoptIdentity = identityFile != nil
	case "none":
		_, statErr := os.Stat(identityPath)
		adoptIdentity = identityFile != nil && errors.Is(statErr, os.ErrNotExist)
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

	// Quiesce the share-publication hooks and tear the running manager down
	// before the database is replaced. A hook admitted before this point holds
	// publication objects built from the pre-restore rows; running it after the
	// restore commits would seal envelopes with the old symkey, insert them
	// into the restored ring under whatever publication now owns that id, and
	// bump its last_seq — silently breaking catch-up for that share's
	// followers. It would also read a.shareManager across the unsynchronized
	// reassignment below.
	resumeHooks := a.quiesceShareHooksForRestore()
	defer resumeHooks()

	if a.shareManager != nil {
		oldSM := a.shareManager
		a.shareManager = nil
		oldSM.Stop()
	}
	// Rebuild on every exit path, not only success: a failed restore rolls its
	// transaction back, so the manager comes back up on the untouched
	// pre-restore rows instead of leaving the app with sharing silently dead.
	// Registered after the resume defer so the new manager is in place before
	// hooks are readmitted.
	//
	// A rebuild failure fails the restore. Without this the call returned
	// success while a.shareManager stayed nil, so every later publication hook
	// silently no-opped and sharing was dead until the next app start with
	// nothing having said so. The gate is still resumed either way: leaving it
	// suspended would strand a suspension no code path can release, and would
	// also gag a manager that a later restore does bring back up — the nil
	// manager already makes the hooks a safe no-op, so the missing piece was
	// only ever the error.
	defer func() {
		rerr := a.rebuildShareManagerAfterRestore(dataDir)
		if rerr == nil {
			return
		}
		if err != nil {
			// The restore itself already failed and that is the more useful
			// error; the rebuild failure is a log line on top of it.
			fmt.Printf("Warning: share manager rebuild after failed restore: %v\n", rerr)
			return
		}
		err = fmt.Errorf("backup restored, but sharing is now disabled because the share manager could not be restarted (restarting the app may recover it): %w", rerr)
	}()

	// Begin transaction
	tx, err := a.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear all existing data. Order matters: child tables before parent tables to
	// respect FK constraints. share_ring references shares(id), so it goes first;
	// clip_tags references both clips and tags, so it precedes them.
	tables := []string{
		"share_ring", // FK → shares(id)
		"shares",     // FK → tags(id)
		"follows",    // FK → tags(id)
		"clip_tags",  // FK → clips, tags
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
		if stmt == "" {
			continue
		}

		// Skip comment-only blocks
		// A statement might have leading comments, so filter them out
		lines := strings.Split(stmt, "\n")
		var sqlLines []string
		for _, line := range lines {
			trimmedLine := strings.TrimSpace(line)
			if trimmedLine != "" && !strings.HasPrefix(trimmedLine, "--") {
				sqlLines = append(sqlLines, line)
			}
		}
		if len(sqlLines) == 0 {
			continue
		}
		stmt = strings.Join(sqlLines, "\n")

		if _, err := tx.Exec(stmt); err != nil {
			// Log warning but continue (for forward compatibility)
			fmt.Printf("Warning: failed to execute SQL: %v\nStatement: %s\n", err, stmt[:min(100, len(stmt))])
		}
	}

	// Backup SQL may contain legacy or externally supplied MIME values. Apply
	// the same filename-authoritative Markdown promotion used at startup before
	// exposing restored rows.
	if err := promoteMarkdownClipTypes(tx); err != nil {
		return fmt.Errorf("promote restored Markdown clips: %w", err)
	}

	// Backups written before the export became snapshot-consistent can carry
	// share_ring rows whose seq is above the shares.last_seq recorded beside
	// them, because each table was read in its own query and an emission could
	// commit in between. Repair the invariant here so it holds regardless of
	// backup vintage — otherwise the next emission on a restored-active share
	// reuses a seq the ring already holds and dies on
	// UNIQUE(publication_id, seq).
	if err := normalizeShareSeqs(tx); err != nil {
		return fmt.Errorf("normalize restored share sequences: %w", err)
	}

	// A restored publication is only usable if the peer id in its share strings
	// is the one this install will run as after the restore, so unless the
	// backup's identity is being adopted the rows have to be invalidated. Doing
	// it inside the transaction means a failure to invalidate rolls the whole
	// restore back, rather than leaving shares active — and publishing — under
	// an identity that does not match them.
	//
	// policy   | backup identity | local identity file | adopted? | shares
	// ---------+-----------------+---------------------+----------+---------
	// takeover | present         | any (overwritten)   | yes      | active
	// takeover | absent          | any                 | no       | invalid
	// keep     | any             | any                 | no       | invalid
	// none     | present         | absent              | yes      | active
	// none     | present         | present             | no       | invalid
	// none     | absent          | any                 | no       | invalid
	//
	// The "none" rows are the ones this used to get wrong. The frontend sends
	// "none" whenever it does not see a collision — including when the backup
	// has no identity at all, and including when BackupInspect failed and a
	// real collision went unnoticed — so "none" is not a promise that the
	// backup's identity will be installed. The two non-adopting "none" cases
	// are the "keep" situation under another name and are treated as such.
	// Where the backup carries no shares at all the UPDATE is a no-op, so the
	// harmless cases cost nothing.
	//
	// The adopting rows go through the same invalidation, because the identity
	// they need can only be installed after this transaction commits. Every
	// compensation for a failed install is an in-process error path, so a
	// SIGKILL or a power cut in that window used to leave the restored shares
	// committed as 'active' while the old identity was still on disk: the next
	// start resumed them under a peer id their share strings do not name, which
	// is precisely the state all of this exists to prevent. Committing them
	// invalid and reactivating only after a successful install means the
	// crash window resolves to shares that are invalid — visibly re-shareable,
	// never silently wrong. That is why no marker or recovery pass is needed:
	// 'invalid' is an acceptable resting state and 'active' under the wrong
	// identity is not.
	var restoredShareStatuses map[string][]int64
	if adoptIdentity {
		restoredShareStatuses, err = captureShareStatuses(tx)
		if err != nil {
			return fmt.Errorf("capture restored share statuses (%s policy): %w", identityPolicy, err)
		}
	}
	if _, err := tx.Exec("UPDATE shares SET status = 'invalid'"); err != nil {
		return fmt.Errorf("invalidate restored shares (%s policy): %w", identityPolicy, err)
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

	// Install the adopted identity FIRST, ahead of every other post-commit
	// step. The restored shares are active on disk from the commit onwards, and
	// they are only correct once the identity they were created under is the
	// one installed. Anything fallible in between — the plugin directory setup
	// below used to sit here — can return early and strand the process in
	// exactly the state this ordering exists to prevent: active publications
	// sealing envelopes under a peer id their share strings do not name.
	//
	// The transaction cannot be undone at this point, but it does not need to
	// be: it committed every share invalid. A failed install therefore needs no
	// compensating UPDATE — the rows already say what is true — it only has to
	// report, and the reactivation below is simply never reached. The same
	// holds for a process that dies anywhere between the commit and that
	// reactivation. Everything after this line either succeeds or fails with
	// the shares and the identity already agreeing.
	if adoptIdentity {
		if err := installBackupIdentity(identityFile, identityPath, dataDir); err != nil {
			return fmt.Errorf("install identity file (%s); restored shares stay invalid: %w", identityPolicy, err)
		}
		// The identity in the share strings is now the identity on disk, so the
		// publications the backup had may run again. Nothing can have touched
		// shares since the commit — the hook gate is suspended, the manager is
		// down, and the restore mutex is held — so this puts back exactly what
		// was captured. A failure here is reported rather than swallowed: the
		// rows stay invalid, which is recoverable by re-sharing, where a silent
		// half-reactivation would not be.
		if err := restoreShareStatuses(a.db, restoredShareStatuses); err != nil {
			return fmt.Errorf("identity adopted (%s), but the restored shares could not be reactivated and stay invalid: %w", identityPolicy, err)
		}
	} else if identityPolicy == "takeover" {
		// Reachable only programmatically — the UI offers "takeover" just when
		// the backup has an identity. The shares were already marked invalid
		// inside the transaction, so the restored data is self-consistent.
		fmt.Printf("Warning: backup contains no %s; restored shares were marked invalid (takeover)\n", ShareIdentityFile)
	}

	// Clear transfer temp files to avoid stale clip references after restore.
	if err := a.DeleteAllTempFiles(); err != nil {
		fmt.Printf("Warning: failed to clear temp files after restore: %v\n", err)
	}

	// Copy plugin files
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
			if err := extractZipFile(f, destPath, dataDir, 0o644); err != nil {
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

	// The identity was installed immediately after the commit, and the
	// ShareManager is rebuilt by the deferred rebuildShareManagerAfterRestore,
	// which runs on this return and on every error path above — reporting a
	// rebuild failure through the named return.
	return nil
}

// rebuildShareManagerAfterRestore brings a fresh ShareManager up on the current
// database and identity file. RestoreBackup stops the previous one before it
// replaces the database and defers this, so the manager always reflects the
// rows and identity that are actually on disk when the restore returns.
//
// The frontend reloads the webview after a successful restore, but that only
// reloads JS; the Go process stays alive, so without the rebuild the old
// manager would keep serving the old identity and the old in-memory
// publications/follows maps. Rebuilding mirrors what app.startup does.
//
// It returns an error only when no manager could be constructed at all, which
// is the case that leaves a.shareManager nil and sharing dead for the rest of
// the process — RestoreBackup turns that into a failed restore. A ResumeAll
// failure stays a warning: the manager exists, hooks publish through it, and
// individual publications can be resumed by other means.
//
// A nil ctx means the app was never started (focused tests, headless paths).
// There was no manager to lose, so there is nothing to report.
func (a *App) rebuildShareManagerAfterRestore(dataDir string) error {
	if a.ctx == nil {
		return nil
	}
	sm, err := NewShareManager(a.ctx, a.db, dataDir)
	if err != nil {
		return fmt.Errorf("rebuild share manager after restore: %w", err)
	}
	sm.SetEventFn(a.bridge.Emit)
	if err := sm.ResumeAll(); err != nil {
		fmt.Printf("Warning: ShareManager ResumeAll after restore: %v\n", err)
	}
	sm.StartSweepers()
	a.shareManager = sm
	return nil
}

// installBackupIdentity adopts the backup's peer identity, replacing the one at
// destPath. It extracts the entry to a sibling staging path, checks the bytes
// are an identity this app can run as, and only then renames them into place.
//
// The check is why the staging step exists. extractZipFile guarantees the file
// that lands is exactly the bytes the archive holds — the ZIP's own CRC covers
// that — but it has no opinion on whether those bytes are a key. A backup whose
// share_identity.key entry is CRC-valid garbage, truncated when it was written
// or tampered with afterwards, used to sail through and replace the machine's
// only identity. Nothing noticed until the deferred share-manager rebuild ran
// LoadOrCreateIdentity against it and failed, by which point the old key was
// gone: the rebuild fails identically on every subsequent app start, and every
// share string ever handed out for this install names a peer id no file on disk
// can produce. There is no recovery from that short of an older backup.
//
// Validating before the rename turns the whole thing into a failed restore with
// the local identity untouched, which the caller reports and repairs by
// invalidating the restored shares.
//
// The staging file is removed on every failure path; after a successful rename
// there is nothing left at that path to remove.
func installBackupIdentity(identityFile *zip.File, destPath, baseDir string) error {
	// Beside the destination, so the rename below stays within one filesystem
	// and therefore atomic. extractZipFile re-checks this path against baseDir.
	stagingPath := destPath + ".adopting"
	if err := extractZipFile(identityFile, stagingPath, baseDir, 0o600); err != nil {
		return err
	}
	defer os.Remove(stagingPath)

	b, err := os.ReadFile(stagingPath)
	if err != nil {
		return fmt.Errorf("read extracted identity: %w", err)
	}
	if _, err := ValidateIdentityBytes(b); err != nil {
		return fmt.Errorf("backup identity failed validation: %w", err)
	}

	if err := os.Rename(stagingPath, destPath); err != nil {
		return err
	}
	// The rename must be DURABLE before the caller reactivates the restored
	// shares: the reactivation is a SQLite commit (WAL-fsynced), so without a
	// directory fsync a power loss could keep the committed active statuses
	// while losing the directory-entry replacement — resuming publications
	// under the old or absent identity, the exact state the two-phase
	// adoption exists to rule out.
	return syncDir(filepath.Dir(destPath))
}

// syncDir fsyncs a directory so a preceding rename within it is durable.
// Windows has no directory-handle fsync; rename durability there is left to
// the filesystem, matching the standard Go practice for this pattern.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open dir for sync: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		if runtime.GOOS == "windows" {
			return nil
		}
		return fmt.Errorf("sync dir: %w", err)
	}
	return nil
}

// extractZipFile extracts a single file from a ZIP archive to destPath with
// explicit file permissions (perm). Callers must always supply a mode because
// zip.Writer.Create does not preserve Unix mode bits — so the mode stored in
// the ZIP entry cannot be trusted on extraction.
//
// baseDir is used to validate the path stays within the allowed directory
// (prevents path traversal attacks).
//
// The write is atomic: the entry is copied into a temp file beside destPath and
// renamed over it only once the bytes are complete and on disk. Truncating the
// destination first and copying into it meant a corrupt archive — a truncated
// entry, a CRC failure partway through — destroyed the existing file and left a
// partial one in its place. That matters most for share_identity.key, where the
// old identity is the peer id every existing share string names and there is no
// second copy of it anywhere.
func extractZipFile(f *zip.File, destPath string, baseDir string, perm os.FileMode) (err error) {
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

	// Same directory as the destination: a rename across filesystems is not
	// atomic (and may not be permitted at all), and the traversal check above
	// has already established that directory is inside baseDir.
	tmp, err := os.CreateTemp(filepath.Dir(destPath), "."+filepath.Base(destPath)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		// Only on failure. After a successful rename there is nothing at
		// tmpPath, and the double Close is a harmless ErrClosed.
		if err != nil {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()

	// CreateTemp makes the file 0600; callers pass the mode the destination
	// needs because zip.Writer.Create does not preserve Unix mode bits.
	if err = tmp.Chmod(perm); err != nil {
		return err
	}
	if _, err = io.Copy(tmp, rc); err != nil {
		return err
	}
	// Checked, not deferred: the copy is only durable once the data and the
	// close have both succeeded, and a rename over a good file must not be
	// reached on the strength of bytes that never landed.
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, destPath)
}
