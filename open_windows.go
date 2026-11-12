//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"

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

// revealInFileManager opens Explorer with the file selected. Explorer parses
// its own command line and does not understand a fully quoted
// "/select,C:\a b\c.txt" argument (Go's default quoting once the path has a
// space), so the command line is written by hand with only the path quoted;
// Windows paths cannot contain '"'. explorer.exe exits non-zero even on
// success, so only a failure to start is an error.
func revealInFileManager(path string) error {
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: explorerSelectCmdLine(path)}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to reveal file: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func explorerSelectCmdLine(path string) string {
	return `explorer.exe /select,"` + filepath.Clean(path) + `"`
}
