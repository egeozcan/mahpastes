//go:build !darwin && !windows && !linux

package main

import (
	"fmt"
	"runtime"

	"go-clipboard/internal/wailsbridge"
)

func openFileWithDefaultApp(_ string) error {
	return fmt.Errorf("open file is not supported on %s", runtime.GOOS)
}

func openFileWithApp(_, _ string) error {
	return fmt.Errorf("open file with app is not supported on %s", runtime.GOOS)
}

func chooseApplicationDialog(_ *wailsbridge.Bridge) (string, error) {
	return "", fmt.Errorf("choose application is not supported on %s", runtime.GOOS)
}

func revealInFileManager(_ string) error {
	return fmt.Errorf("reveal in file manager is not supported on %s", runtime.GOOS)
}
