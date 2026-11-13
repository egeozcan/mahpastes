//go:build !windows

package app

import "os"

// openSnapshotFile opens a snapshot for reading. Only Windows needs more than
// os.Open; see clip_snapshot_open_windows.go.
func openSnapshotFile(path string) (*os.File, error) {
	return os.Open(path)
}

// reopenSnapshotForPublish is a no-op here: an open file can be renamed.
func reopenSnapshotForPublish(f *os.File) (*os.File, error) {
	return f, nil
}
