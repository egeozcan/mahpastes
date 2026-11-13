//go:build !(darwin && cgo)

package app

import (
	"os"
	"time"
)

// fileBusyCheckSupported says fileOpenForWriting can tell.
const fileBusyCheckSupported = false

// fileOpenForWriting cannot tell here. On Windows a file a writer holds open
// usually cannot be deleted anyway.
func fileOpenForWriting(string, os.FileInfo, time.Duration) bool { return false }
