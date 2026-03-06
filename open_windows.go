//go:build windows

package main

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func openFileWithDefaultApp(path string) error {
	cmd := exec.Command("cmd", "/c", "start", "", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to open file: %w: %s", err, string(out))
	}
	return nil
}

func openFileWithApp(filePath, appPath string) error {
	cmd := exec.Command("cmd", "/c", "start", "", appPath, filePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to open file with app: %w: %s", err, string(out))
	}
	return nil
}

func chooseApplicationDialog(ctx context.Context) (string, error) {
	path, err := runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title:            "Choose Application",
		DefaultDirectory: `C:\Program Files`,
		Filters: []runtime.FileFilter{
			{DisplayName: "Executables", Pattern: "*.exe"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file dialog: %w", err)
	}
	return path, nil
}
