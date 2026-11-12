// Package desktopprefs keeps the few desktop settings the app needs before
// wails.Run, when the database is not open yet (it opens in OnStartup). They
// live in a small JSON file in the data directory, not in the settings table,
// so they are not part of backups.
//
// Keys this build does not know are kept when the file is rewritten, so an
// older build does not drop what a newer one stored.
package desktopprefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// FileName is the prefs file inside the data directory.
const FileName = "desktop-prefs.json"

// keepRunningKey stores whether closing the window hides it (the process and
// everything it hosts keep running) instead of quitting.
const keepRunningKey = "keep_running_when_closed"

// maxFileSize bounds what Load reads: the file holds a handful of flags.
const maxFileSize = 64 << 10

// Prefs is the parsed prefs file.
type Prefs struct {
	// KeepRunningWhenClosed is nil when the user never chose; KeepRunning
	// then falls back to the platform default.
	KeepRunningWhenClosed *bool
}

// KeepRunning reports whether closing the window should leave the app
// running. Unset means on for macOS, where closing a window and quitting an
// app are different things, and off elsewhere: without a tray the hidden
// window is only reachable by launching the app again.
func (p Prefs) KeepRunning(goos string) bool {
	if p.KeepRunningWhenClosed != nil {
		return *p.KeepRunningWhenClosed
	}
	return DefaultKeepRunning(goos)
}

// DefaultKeepRunning is the keep-running value for a user who never chose.
func DefaultKeepRunning(goos string) bool {
	return goos == "darwin"
}

// mu serializes read-modify-write cycles within the process. Only the
// desktop app writes the file, and the instance lock keeps it to one process
// per data directory.
var mu sync.Mutex

// Path returns the prefs file path for dataDir.
func Path(dataDir string) string {
	return filepath.Join(dataDir, FileName)
}

// Load reads the prefs. A missing file is not an error. A file that cannot
// be parsed returns the defaults together with the error, so a caller can log
// it and carry on: a broken prefs file must never stop the app starting.
func Load(dataDir string) (Prefs, error) {
	mu.Lock()
	defer mu.Unlock()
	raw, err := readRaw(dataDir)
	if err != nil {
		return Prefs{}, err
	}
	return fromRaw(raw)
}

// SetKeepRunningWhenClosed stores the keep-running choice.
func SetKeepRunningWhenClosed(dataDir string, keep bool) error {
	return update(dataDir, func(raw map[string]json.RawMessage) error {
		b, err := json.Marshal(keep)
		if err != nil {
			return err
		}
		raw[keepRunningKey] = b
		return nil
	})
}

func update(dataDir string, apply func(map[string]json.RawMessage) error) error {
	mu.Lock()
	defer mu.Unlock()
	raw, err := readRaw(dataDir)
	if err != nil {
		// Start over rather than refuse to save: the user is setting a
		// value now, and an unreadable file holds nothing usable.
		raw = map[string]json.RawMessage{}
	}
	if err := apply(raw); err != nil {
		return err
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(Path(dataDir), append(data, '\n'))
}

func readRaw(dataDir string) (map[string]json.RawMessage, error) {
	raw := map[string]json.RawMessage{}
	f, err := os.Open(Path(dataDir))
	if errors.Is(err, fs.ErrNotExist) {
		return raw, nil
	}
	if err != nil {
		return raw, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return raw, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileSize {
		return raw, fmt.Errorf("%s is not a small regular file", FileName)
	}
	data := make([]byte, info.Size())
	if _, err := io.ReadFull(f, data); err != nil {
		return raw, err
	}
	if len(data) == 0 {
		return raw, nil
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return map[string]json.RawMessage{}, fmt.Errorf("parse %s: %w", FileName, err)
	}
	if raw == nil {
		raw = map[string]json.RawMessage{}
	}
	return raw, nil
}

func fromRaw(raw map[string]json.RawMessage) (Prefs, error) {
	var p Prefs
	if v, ok := raw[keepRunningKey]; ok {
		var keep bool
		if err := json.Unmarshal(v, &keep); err != nil {
			return Prefs{}, fmt.Errorf("parse %s: %s: %w", FileName, keepRunningKey, err)
		}
		p.KeepRunningWhenClosed = &keep
	}
	return p, nil
}

// writeAtomic replaces path with data through a temp file in the same
// directory, so a crash mid-write leaves the old file or the new one.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+FileName+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
