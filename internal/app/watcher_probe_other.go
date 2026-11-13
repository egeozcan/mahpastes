//go:build !darwin

package app

// openPlatformDirProbe returns nil: inotify reports an unmount and drops the
// watch, and on Windows a watched directory keeps its volume from going away.
func openPlatformDirProbe(string) dirProbe { return nil }
