// Package logfile provides the size-capped log file in the data directory.
// GUI builds have no usable stderr (Windows GUI subsystem, Finder-launched
// .app), so without this a startup failure leaves no trace.
package logfile

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
)

// MaxSize is the size at which the active log is rotated to a ".1" file.
const MaxSize = 5 << 20

const fileName = "mahpastes.log"

// Dir returns the log directory inside dataDir.
func Dir(dataDir string) string { return filepath.Join(dataDir, "logs") }

// Seams for tests: the process has a single crash output, and Windows refuses
// to rename a file that is still open somewhere, which tests simulate.
var (
	setCrashOutput = func(f *os.File) error { return debug.SetCrashOutput(f, debug.CrashOptions{}) }
	rename         = os.Rename
)

// Writer appends to the log file and, once rotation is enabled, moves it to
// ".1" when it reaches the cap, so total disk use stays at roughly twice
// MaxSize.
type Writer struct {
	mu     sync.Mutex
	path   string
	max    int64
	f      *os.File
	size   int64
	rotate bool // set by EnableRotation
	crash  bool // set by CaptureCrashes
	closed bool
}

// Open creates the log directory and opens the log for appending. It does not
// rotate: call EnableRotation once this process owns the data directory, so a
// second launch that is about to exit cannot move the running instance's log.
func Open(dataDir string) (*Writer, error) {
	return open(dataDir, MaxSize)
}

func open(dataDir string, max int64) (*Writer, error) {
	dir := Dir(dataDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	w := &Writer{path: filepath.Join(dir, fileName), max: max}
	f, size, err := openFile(w.path, false)
	if err != nil {
		return nil, err
	}
	w.f, w.size = f, size
	return w, nil
}

// Logs can contain file paths, so they are private to the user.
func openFile(path string, truncate bool) (*os.File, int64, error) {
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if truncate {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

// CaptureCrashes sends Go runtime crash traces (unrecovered panics, fatal
// errors) to the log as well as stderr; log.SetOutput does not see them. The
// writer keeps the crash output on the active file across rotations.
func (w *Writer) CaptureCrashes() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return os.ErrClosed
	}
	if err := setCrashOutput(w.f); err != nil {
		return err
	}
	w.crash = true
	return nil
}

// EnableRotation rotates an oversized log now and, from then on, whenever a
// write finds the log at the cap.
func (w *Writer) EnableRotation() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return os.ErrClosed
	}
	w.rotate = true
	if w.size >= w.max {
		return w.reopenLocked(true)
	}
	return nil
}

// reopenLocked closes the active file, optionally moves it to ".1", and opens
// a fresh one.
func (w *Writer) reopenLocked(rotate bool) error {
	// Release every handle on the file first: Windows will not rename a file
	// that is open, and the crash output holds a duplicate handle.
	if w.crash {
		_ = setCrashOutput(nil)
	}
	if w.f != nil {
		w.f.Close()
		w.f = nil
	}
	truncate := false
	if rotate {
		_ = os.Remove(w.path + ".1")
		if err := rename(w.path, w.path+".1"); err != nil && !os.IsNotExist(err) {
			// Something else still has the file open (a second launch on
			// Windows). Start the log over in place so the cap still holds,
			// rather than retrying on every write while it grows.
			truncate = true
		}
	}
	f, size, err := openFile(w.path, truncate)
	if err != nil {
		return err
	}
	w.f, w.size = f, size
	if w.crash {
		_ = setCrashOutput(f)
	}
	return nil
}

// Write implements io.Writer.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if rotate := w.rotate && w.size >= w.max; rotate || w.f == nil {
		// w.f is nil only when an earlier reopen failed; try again.
		if err := w.reopenLocked(rotate); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// Close closes the file and stops sending crash traces to it.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.crash {
		_ = setCrashOutput(nil)
		w.crash = false
	}
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}
