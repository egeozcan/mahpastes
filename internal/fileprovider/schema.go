package fileprovider

import (
	"context"
	"database/sql"
	"fmt"
)

// Auxiliary tables are deliberately absent from the application's backup
// allowlist. Once enrolled, triggers keep tracking even in a non-native build.
const schema = `
CREATE TABLE IF NOT EXISTS fp_state (singleton INTEGER PRIMARY KEY CHECK(singleton=1), version INTEGER NOT NULL, epoch TEXT NOT NULL);
INSERT OR IGNORE INTO fp_state VALUES(1,1,lower(hex(randomblob(16))));
CREATE TABLE IF NOT EXISTS fp_sources (clip_id INTEGER PRIMARY KEY, uuid TEXT NOT NULL UNIQUE, content_rev INTEGER NOT NULL DEFAULT 1, metadata_rev INTEGER NOT NULL DEFAULT 1, modified TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS fp_dirty (clip_id INTEGER PRIMARY KEY);
CREATE TABLE IF NOT EXISTS fp_items (id TEXT PRIMARY KEY, clip_id INTEGER UNIQUE, parent TEXT NOT NULL, item TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS fp_changes (seq INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT NOT NULL, old_parent TEXT NOT NULL, parent TEXT NOT NULL, item TEXT, created INTEGER NOT NULL DEFAULT (unixepoch()));
CREATE TABLE IF NOT EXISTS fp_snapshots (id TEXT PRIMARY KEY, epoch TEXT NOT NULL, scope TEXT NOT NULL, high INTEGER NOT NULL, expires INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS fp_snapshot_items (snapshot TEXT NOT NULL REFERENCES fp_snapshots(id) ON DELETE CASCADE, ordinal INTEGER NOT NULL, item TEXT NOT NULL, PRIMARY KEY(snapshot,ordinal));
INSERT OR IGNORE INTO fp_sources(clip_id,uuid,modified) SELECT id,lower(hex(randomblob(16))),strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM clips;
INSERT OR IGNORE INTO fp_dirty SELECT id FROM clips;
CREATE TRIGGER IF NOT EXISTS fp_clip_insert AFTER INSERT ON clips BEGIN
 INSERT INTO fp_sources(clip_id,uuid,modified) VALUES(new.id,lower(hex(randomblob(16))),strftime('%Y-%m-%dT%H:%M:%fZ','now'));
 INSERT OR IGNORE INTO fp_dirty VALUES(new.id);
END;
CREATE TRIGGER IF NOT EXISTS fp_clip_update AFTER UPDATE ON clips BEGIN
 UPDATE fp_sources SET content_rev=content_rev+CASE WHEN old.data IS NOT new.data THEN 1 ELSE 0 END, metadata_rev=metadata_rev+1, modified=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE clip_id=new.id;
 INSERT OR IGNORE INTO fp_dirty VALUES(new.id);
END;
CREATE TRIGGER IF NOT EXISTS fp_clip_delete AFTER DELETE ON clips BEGIN
 DELETE FROM fp_sources WHERE clip_id=old.id;
 INSERT OR IGNORE INTO fp_dirty VALUES(old.id);
END;
`

func initialize(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT version FROM fp_state").Scan(&version); err != nil {
		return err
	}
	if version != 1 {
		return fmt.Errorf("unsupported File Provider schema %d", version)
	}
	// Visibility can change without a clips UPDATE (plugins and SQL imports).
	for _, table := range []string{"tags", "clip_tags", "settings"} {
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			when := ""
			if table == "settings" {
				row := "new"
				if event == "DELETE" {
					row = "old"
				}
				when = " WHEN " + row + ".key='hidden_tags'"
			}
			q := fmt.Sprintf("CREATE TRIGGER IF NOT EXISTS fp_%s_%s AFTER %s ON %s%s BEGIN INSERT OR IGNORE INTO fp_dirty SELECT id FROM clips; END", table, event, event, table, when)
			if _, err = tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// ResetAfterRestore must run in the same transaction as replacing clips. Old
// identifiers and anchors can never resolve to a restored numeric clip ID.
// This is safe to call on databases which have never enrolled a provider.
func ResetAfterRestore(tx *sql.Tx) error {
	var exists int
	if err := tx.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='fp_state'").Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	_, err := tx.Exec(`UPDATE fp_state SET epoch=lower(hex(randomblob(16)));
DELETE FROM fp_snapshot_items; DELETE FROM fp_snapshots; DELETE FROM fp_changes; DELETE FROM fp_items; DELETE FROM fp_sources; DELETE FROM fp_dirty;
INSERT INTO fp_sources(clip_id,uuid,modified) SELECT id,lower(hex(randomblob(16))),strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM clips;
INSERT INTO fp_dirty SELECT id FROM clips;`)
	return err
}
