//go:build !darwin

package autostart

func platformVolumeReadOnly(string) bool { return false }
