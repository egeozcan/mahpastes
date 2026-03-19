//go:build !darwin && !windows

package main

import (
	"fmt"
	"runtime"
)

func copyFilesToClipboard(_ []string) error {
	return fmt.Errorf("copy-as-file is not supported on %s", runtime.GOOS)
}

func copyImageToClipboard(_ []byte) error {
	return fmt.Errorf("image clipboard copy is not supported on %s", runtime.GOOS)
}
