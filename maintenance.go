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

// CompactDatabase runs VACUUM + ANALYZE on clips.db. Returns before/after
// sizes (bytes). VACUUM cannot run inside an explicit transaction.
func (a *App) CompactDatabase() (before, after int64, err error) {
	before, err = a.GetDatabaseSize()
	if err != nil {
		return 0, 0, err
	}
	if _, err := a.db.Exec(`VACUUM`); err != nil {
		return before, 0, fmt.Errorf("vacuum: %w", err)
	}
	if _, err := a.db.Exec(`ANALYZE`); err != nil {
		return before, 0, fmt.Errorf("analyze: %w", err)
	}
	after, err = a.GetDatabaseSize()
	if err != nil {
		return before, 0, err
	}
	return before, after, nil
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

// CleanStaleFiles deletes every file returned by GetStaleFiles and reports
// how many were removed and how many bytes were reclaimed.
func (a *App) CleanStaleFiles() (count int, bytes int64, err error) {
	files, err := a.GetStaleFiles()
	if err != nil {
		return 0, 0, err
	}
	for _, f := range files {
		if err := os.Remove(f.absPath); err != nil {
			// Log but keep going — a single failure shouldn't abort the sweep.
			continue
		}
		count++
		bytes += f.Size
	}
	return count, bytes, nil
}
