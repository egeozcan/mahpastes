//go:build unix

package openrequest

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// checkSocketOwner refuses a socket that belongs to another user. The socket
// normally sits in the data directory, which only its owner can write, but
// the fallback for long data dirs can be the shared temp directory, where
// another local user could create a socket at the predictable name first and
// collect the file paths a launch sends. A missing path is not an error here:
// dialing it fails on its own.
func checkSocketOwner(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("instance socket %s belongs to another user (uid %d)", path, st.Uid)
	}
	return nil
}
