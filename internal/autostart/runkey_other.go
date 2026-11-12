//go:build !windows

package autostart

// newRunKeyBackend only exists on Windows.
func newRunKeyBackend() backend { return nil }
