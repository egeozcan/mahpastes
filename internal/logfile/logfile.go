// Package logfile provides the size-capped log file in the data directory.
// GUI builds have no usable stderr (Windows GUI subsystem, Finder-launched
// .app), so without this a startup failure leaves no trace.
package logfile

import (
	"os"
	"path/filepath"
	"sync"
)

// MaxSize is the size at which the active log is rotated to a ".1" file.
const MaxSize = 5 << 20

const fileName = "mahpastes.log"

// Dir returns the log directory inside dataDir.
func Dir(dataDir string) string { return filepath.Join(dataDir, "logs") }

// Writer appends to the log file and rotates it when it reaches the cap, so
// total disk use stays at roughly twice MaxSize.
type Writer struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

// Open creates the log directory, rotates an oversized existing log and opens
// the file for appending.
func Open(dataDir string) (*Writer, error) {
	return open(dataDir, MaxSize)
}

func open(dataDir string, max int64) (*Writer, error) {
	dir := Dir(dataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	w := &Writer{path: filepath.Join(dir, fileName), max: max}
	if err := w.openLocked(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) openLocked() error {
	if st, err := os.Stat(w.path); err == nil && st.Size() >= w.max {
		_ = os.Remove(w.path + ".1")
		_ = os.Rename(w.path, w.path+".1")
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.f, w.size = f, st.Size()
	return nil
}

// Write implements io.Writer.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size >= w.max {
		w.f.Close()
		if err := w.openLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// File returns the underlying file (for debug.SetCrashOutput).
func (w *Writer) File() *os.File {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f
}

// Close closes the file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}
