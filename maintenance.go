package main

import (
	"fmt"
	"os"
	"path/filepath"
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
