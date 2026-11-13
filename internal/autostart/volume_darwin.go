package autostart

import "syscall"

// mntRdonly is MNT_RDONLY from <sys/mount.h>; package syscall does not export it.
const mntRdonly = 0x1

func platformVolumeReadOnly(path string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false
	}
	return st.Flags&mntRdonly != 0
}
