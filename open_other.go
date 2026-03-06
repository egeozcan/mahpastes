//go:build !darwin && !windows

package main

import (
	"context"
	"fmt"
	"runtime"
)

func openFileWithDefaultApp(_ string) error {
	return fmt.Errorf("open file is not supported on %s", runtime.GOOS)
}

func openFileWithApp(_, _ string) error {
	return fmt.Errorf("open file with app is not supported on %s", runtime.GOOS)
}

func chooseApplicationDialog(_ context.Context) (string, error) {
	return "", fmt.Errorf("choose application is not supported on %s", runtime.GOOS)
}
