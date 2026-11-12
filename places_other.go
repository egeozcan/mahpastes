//go:build !linux

package main

// File-manager sidebar entries exist only for the Linux FUSE mount; macOS
// gets its Finder location from the File Provider domain.

func surfaceMount(string) (gtk, kde bool, err error) { return false, false, nil }

func unsurfaceMount(string, bool, bool) (gtkLeft, kdeLeft bool, err error) {
	return false, false, nil
}
