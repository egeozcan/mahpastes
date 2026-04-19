package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// GetDatabaseSize returns the combined byte size of clips.db + -wal + -shm.
func (a *App) GetDatabaseSize() (int64, error) {
	dataDir, err := getDataDir()
	if err != nil {
		return 0, err
	}
	var total int64
	for _, suffix := range []string{"clips.db", "clips.db-wal", "clips.db-shm"} {
		info, err := os.Stat(filepath.Join(dataDir, suffix))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, fmt.Errorf("stat %s: %w", suffix, err)
		}
		total += info.Size()
	}
	return total, nil
}

// CompactResult reports before/after DB size for a VACUUM + ANALYZE run.
type CompactResult struct {
	Before int64 `json:"before"`
	After  int64 `json:"after"`
}

// CompactDatabase runs VACUUM + ANALYZE on clips.db. Returns before/after
// sizes (bytes). VACUUM cannot run inside an explicit transaction.
//
// The result is returned as a struct rather than separate values because
// Wails v2 frontend bindings only marshal `(value, error)` signatures —
// extra return values are silently dropped.
func (a *App) CompactDatabase() (CompactResult, error) {
	before, err := a.GetDatabaseSize()
	if err != nil {
		return CompactResult{}, err
	}
	if _, err := a.db.Exec(`VACUUM`); err != nil {
		return CompactResult{Before: before}, fmt.Errorf("vacuum: %w", err)
	}
	if _, err := a.db.Exec(`ANALYZE`); err != nil {
		return CompactResult{Before: before}, fmt.Errorf("analyze: %w", err)
	}
	after, err := a.GetDatabaseSize()
	if err != nil {
		return CompactResult{Before: before}, err
	}
	return CompactResult{Before: before, After: after}, nil
}

// StaleFile describes a single file found by the stale-file sweep.
type StaleFile struct {
	Source   string  `json:"source"` // "clip_temp_files" | "share-staging"
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	AgeHours float64 `json:"age_hours"`
	absPath  string
}

const (
	staleTempLeaseWindow    = 60 * time.Minute  // matches defaultTempLeaseTTL
	staleShareStagingWindow = 24 * time.Hour
)

// GetStaleFiles scans clip_temp_files/ (past 60-min lease) and share-staging/
// (past 24 h) for files safe to remove.
func (a *App) GetStaleFiles() ([]StaleFile, error) {
	now := time.Now()
	var out []StaleFile

	dataDir, err := getDataDir()
	if err != nil {
		return nil, err
	}

	sources := []struct {
		key    string
		dir    string
		window time.Duration
	}{
		{"clip_temp_files", filepath.Join(dataDir, "clip_temp_files"), staleTempLeaseWindow},
		{"share-staging", filepath.Join(dataDir, "share-staging"), staleShareStagingWindow},
	}
	for _, s := range sources {
		entries, err := os.ReadDir(s.dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", s.dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			age := now.Sub(info.ModTime())
			if age < s.window {
				continue
			}
			out = append(out, StaleFile{
				Source:   s.key,
				Name:     e.Name(),
				Size:     info.Size(),
				AgeHours: age.Hours(),
				absPath:  filepath.Join(s.dir, e.Name()),
			})
		}
	}
	return out, nil
}

// CleanStaleResult reports how many files were removed and how many bytes
// were reclaimed by a stale-file sweep.
type CleanStaleResult struct {
	Count int   `json:"count"`
	Bytes int64 `json:"bytes"`
}

// CleanStaleFiles deletes every file returned by GetStaleFiles and reports
// how many were removed and how many bytes were reclaimed.
//
// The result is returned as a struct rather than separate values because
// Wails v2 frontend bindings only marshal `(value, error)` signatures.
func (a *App) CleanStaleFiles() (CleanStaleResult, error) {
	files, err := a.GetStaleFiles()
	if err != nil {
		return CleanStaleResult{}, err
	}
	var r CleanStaleResult
	for _, f := range files {
		if err := os.Remove(f.absPath); err != nil {
			// Log but keep going — a single failure shouldn't abort the sweep.
			continue
		}
		r.Count++
		r.Bytes += f.Size
	}
	return r, nil
}

// OrphanReport counts orphaned rows found across the DB.
type OrphanReport struct {
	PluginStorage     int `json:"plugin_storage"`
	PluginPermissions int `json:"plugin_permissions"`
	StaleFollows      int `json:"stale_follows"`
	StaleAutoTags     int `json:"stale_auto_tags"`
	StaleHiddenTagIDs int `json:"stale_hidden_tag_ids"`
}

// GetOrphanDBRows counts rows whose parent reference is missing, without
// mutating anything.
func (a *App) GetOrphanDBRows() (OrphanReport, error) {
	var r OrphanReport
	queries := map[string]*int{
		`SELECT COUNT(*) FROM plugin_storage WHERE plugin_id NOT IN (SELECT id FROM plugins)`:         &r.PluginStorage,
		`SELECT COUNT(*) FROM plugin_permissions WHERE plugin_id NOT IN (SELECT id FROM plugins)`:     &r.PluginPermissions,
		`SELECT COUNT(*) FROM follows WHERE local_tag_id NOT IN (SELECT id FROM tags)`:                &r.StaleFollows,
		`SELECT COUNT(*) FROM watched_folders WHERE auto_tag_id IS NOT NULL AND auto_tag_id NOT IN (SELECT id FROM tags)`: &r.StaleAutoTags,
	}
	for q, dst := range queries {
		if err := a.db.QueryRow(q).Scan(dst); err != nil {
			return r, fmt.Errorf("count query: %w", err)
		}
	}

	// Stale hidden-tag IDs.
	hidden, err := a.GetHiddenTags()
	if err != nil {
		return r, fmt.Errorf("get hidden: %w", err)
	}
	for _, id := range hidden {
		var exists int
		a.db.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, id).Scan(&exists)
		if exists == 0 {
			r.StaleHiddenTagIDs++
		}
	}
	return r, nil
}

// CleanOrphanDBRows deletes (or NULLs) orphan rows inside a single
// transaction and returns per-category cleaned counts.
func (a *App) CleanOrphanDBRows() (OrphanReport, error) {
	var r OrphanReport
	tx, err := a.db.Begin()
	if err != nil {
		return r, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(`DELETE FROM plugin_storage WHERE plugin_id NOT IN (SELECT id FROM plugins)`)
	if err != nil {
		return r, fmt.Errorf("clean plugin_storage: %w", err)
	}
	n, _ := res.RowsAffected()
	r.PluginStorage = int(n)

	res, err = tx.Exec(`DELETE FROM plugin_permissions WHERE plugin_id NOT IN (SELECT id FROM plugins)`)
	if err != nil {
		return r, fmt.Errorf("clean plugin_permissions: %w", err)
	}
	n, _ = res.RowsAffected()
	r.PluginPermissions = int(n)

	res, err = tx.Exec(`DELETE FROM follows WHERE local_tag_id NOT IN (SELECT id FROM tags)`)
	if err != nil {
		return r, fmt.Errorf("clean follows: %w", err)
	}
	n, _ = res.RowsAffected()
	r.StaleFollows = int(n)

	res, err = tx.Exec(`UPDATE watched_folders SET auto_tag_id = NULL
		WHERE auto_tag_id IS NOT NULL AND auto_tag_id NOT IN (SELECT id FROM tags)`)
	if err != nil {
		return r, fmt.Errorf("null stale auto_tag_id: %w", err)
	}
	n, _ = res.RowsAffected()
	r.StaleAutoTags = int(n)

	// Hidden-tag list — prune stale IDs. Use tx-aware helpers (added in Task 4).
	hidden, err := getHiddenTagsTx(tx)
	if err != nil {
		return r, fmt.Errorf("get hidden: %w", err)
	}
	newHidden := make([]int64, 0, len(hidden))
	for _, id := range hidden {
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, id).Scan(&exists); err != nil {
			return r, fmt.Errorf("count tags (id=%d): %w", id, err)
		}
		if exists > 0 {
			newHidden = append(newHidden, id)
		}
	}
	if len(newHidden) != len(hidden) {
		if err := setHiddenTagsTx(tx, newHidden); err != nil {
			return r, fmt.Errorf("update hidden_tags: %w", err)
		}
		r.StaleHiddenTagIDs = len(hidden) - len(newHidden)
	}

	if err := tx.Commit(); err != nil {
		return r, fmt.Errorf("commit: %w", err)
	}
	return r, nil
}
