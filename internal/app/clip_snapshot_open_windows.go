//go:build windows

package app

import (
	"os"

	"golang.org/x/sys/windows"
)

// openSnapshotFile opens a snapshot for reading with FILE_SHARE_DELETE. Go's
// os.Open leaves that flag out, and Windows then refuses to rename or delete
// the file for as long as the handle is open: a fresh copy could never be
// renamed to its published name (every request copied the clip again), and a
// snapshot being streamed could not be dropped when its clip was edited or
// deleted.
func openSnapshotFile(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := windows.CreateFile(p,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// reopenSnapshotForPublish swaps the handle a snapshot was written through,
// which blocks a rename, for a read handle that allows one. The file keeps
// its name; on failure it is removed.
func reopenSnapshotForPublish(f *os.File) (*os.File, error) {
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return nil, err
	}
	nf, err := openSnapshotFile(name)
	if err != nil {
		_ = os.Remove(name)
		return nil, err
	}
	return nf, nil
}
