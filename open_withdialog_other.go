//go:build !windows

package main

import "fmt"

// The native "Open with" chooser exists only on Windows; other platforms keep
// the application-picker flow.
func openWithDialog(path string) error {
	return fmt.Errorf("native open-with dialog is not available on this platform")
}

func supportsNativeOpenWith() bool { return false }
