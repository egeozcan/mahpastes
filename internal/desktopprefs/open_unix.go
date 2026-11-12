//go:build unix

package desktopprefs

import (
	"os"
	"syscall"
)

// openFlags opens the prefs file without blocking on a FIFO.
const openFlags = os.O_RDONLY | syscall.O_NONBLOCK
