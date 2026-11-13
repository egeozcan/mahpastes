//go:build windows

package app

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isFileInUseError reports whether err is Windows refusing an operation on a
// file because another program has it open (or has a range of it locked).
// That is an ordinary state on Windows, where a file open without
// FILE_SHARE_DELETE cannot be deleted or replaced, and it ends by itself when
// that program closes the file.
func isFileInUseError(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
