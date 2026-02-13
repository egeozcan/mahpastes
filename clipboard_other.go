//go:build !darwin && !windows

package main

import (
	"fmt"
	"runtime"
)

func copyFilesToClipboard(_ []string) error {
	return fmt.Errorf("copy-as-file is not supported on %s", runtime.GOOS)
}
