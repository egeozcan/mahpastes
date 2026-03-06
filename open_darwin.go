//go:build darwin

package main

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func openFileWithDefaultApp(path string) error {
	cmd := exec.Command("open", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to open file: %w: %s", err, string(out))
	}
	return nil
}

func openFileWithApp(filePath, appPath string) error {
	cmd := exec.Command("open", "-a", appPath, filePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to open file with app: %w: %s", err, string(out))
	}
	return nil
}

func chooseApplicationDialog(ctx context.Context) (string, error) {
	path, err := runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title:            "Choose Application",
		DefaultDirectory: "/Applications",
		Filters: []runtime.FileFilter{
			{DisplayName: "Applications", Pattern: "*.app"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file dialog: %w", err)
	}
	return path, nil
}
