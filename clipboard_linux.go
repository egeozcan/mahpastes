//go:build linux

package main

import (
	"fmt"

	"golang.design/x/clipboard"
)

func copyFilesToClipboard(_ []string) error {
	return fmt.Errorf("copy-as-file is not supported on linux")
}

// copyImageToClipboard publishes PNG data through the Wayland or X11 backend.
func copyImageToClipboard(pngData []byte) error {
	if err := clipboard.Init(); err != nil {
		return fmt.Errorf("failed to initialize clipboard: %w", err)
	}
	if clipboard.Write(clipboard.FmtImage, pngData) == nil {
		return fmt.Errorf("failed to write image to clipboard")
	}
	return nil
}
