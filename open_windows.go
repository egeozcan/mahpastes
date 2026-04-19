//go:build windows

package main

import (
	"fmt"
	"os/exec"

	"go-clipboard/internal/wailsbridge"
)

func openFileWithDefaultApp(path string) error {
	cmd := exec.Command("cmd", "/c", "start", "", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to open file: %w: %s", err, string(out))
	}
	return nil
}

func openFileWithApp(filePath, appPath string) error {
	cmd := exec.Command(appPath, filePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to open file with app: %w: %s", err, string(out))
	}
	return nil
}

func chooseApplicationDialog(b *wailsbridge.Bridge) (string, error) {
	path, err := b.OpenFile(wailsbridge.FileDialogOptions{
		Title:            "Choose Application",
		DefaultDirectory: `C:\Program Files`,
		Filters: []wailsbridge.FileFilter{
			{DisplayName: "Executables", Pattern: "*.exe"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file dialog: %w", err)
	}
	return path, nil
}
