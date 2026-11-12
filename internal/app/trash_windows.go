//go:build windows && (amd64 || arm64)

package app

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SHFILEOPSTRUCTW has natural alignment on amd64/arm64 (it is packed only on
// 32-bit x86, which trash_windows_other.go covers), so a plain Go struct
// matches.
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

// Compile-time layout checks against the 64-bit SHFILEOPSTRUCTW: each array
// length goes negative, failing the build, if a size or offset drifts.
var (
	_ [56 - unsafe.Sizeof(shFileOpStruct{})]byte
	_ [unsafe.Sizeof(shFileOpStruct{}) - 56]byte
	_ [32 - unsafe.Offsetof(shFileOpStruct{}.fFlags)]byte
	_ [unsafe.Offsetof(shFileOpStruct{}.fFlags) - 32]byte
	_ [36 - unsafe.Offsetof(shFileOpStruct{}.fAnyOperationsAborted)]byte
	_ [unsafe.Offsetof(shFileOpStruct{}.fAnyOperationsAborted) - 36]byte
	_ [40 - unsafe.Offsetof(shFileOpStruct{}.hNameMappings)]byte
	_ [unsafe.Offsetof(shFileOpStruct{}.hNameMappings) - 40]byte
)

const (
	foDelete          = 0x0003
	fofSilent         = 0x0004
	fofNoConfirmation = 0x0010
	fofAllowUndo      = 0x0040
	fofNoErrorUI      = 0x0400
)

var procSHFileOperationW = windows.NewLazySystemDLL("shell32.dll").NewProc("SHFileOperationW")

// trashIsRecoverable is true: deletions go to the Recycle Bin. Paths that
// SHFileOperation would delete permanently instead are refused by
// moveToTrash (see recycleBinRefusal).
func trashIsRecoverable() bool { return true }

// recycleBinRefusal reports why absPath cannot be recycled, or nil. With
// FOF_NOCONFIRMATION, SHFileOperation silently deletes permanently anything on
// a volume without a Recycle Bin (network shares, mapped network drives,
// removable media, RAM disks) and paths it cannot handle, so those are refused
// up front: the wizard promised the Recycle Bin, and a refused file is
// reported as failed and left in place. driveType is GetDriveTypeW, injected
// for tests.
func recycleBinRefusal(absPath string, driveType func(root string) uint32) error {
	if strings.HasPrefix(absPath, `\\`) || strings.HasPrefix(absPath, `//`) {
		return fmt.Errorf("move to Recycle Bin: %s is a network or device path with no Recycle Bin", absPath)
	}
	if !filepath.IsAbs(absPath) {
		return fmt.Errorf("move to Recycle Bin: %q is not an absolute path", absPath)
	}
	// pFrom treats these as wildcards; they cannot occur in a real file name.
	if strings.ContainsAny(absPath, "*?") {
		return fmt.Errorf("move to Recycle Bin: %q contains wildcard characters", absPath)
	}
	if len(windows.StringToUTF16(absPath)) > windows.MAX_PATH {
		return fmt.Errorf("move to Recycle Bin: %s is longer than the %d characters the Recycle Bin supports", absPath, windows.MAX_PATH-1)
	}
	root := filepath.VolumeName(absPath) + `\`
	switch dt := driveType(root); dt {
	case windows.DRIVE_FIXED:
		return nil
	case windows.DRIVE_REMOTE:
		return fmt.Errorf("move to Recycle Bin: %s is on a network drive, which has no Recycle Bin", absPath)
	case windows.DRIVE_REMOVABLE:
		return fmt.Errorf("move to Recycle Bin: %s is on a removable drive, which has no Recycle Bin", absPath)
	default:
		return fmt.Errorf("move to Recycle Bin: %s is on a drive with no Recycle Bin (drive type %d)", absPath, dt)
	}
}

func getDriveType(root string) uint32 {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return windows.DRIVE_UNKNOWN
	}
	return windows.GetDriveType(p)
}

// moveToTrash sends the file to the Recycle Bin with SHFileOperationW.
func moveToTrash(absPath string) error {
	if err := recycleBinRefusal(absPath, getDriveType); err != nil {
		return err
	}
	from, err := windows.UTF16FromString(absPath)
	if err != nil {
		return err
	}
	from = append(from, 0) // double-NUL terminated list

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI,
	}
	r, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	runtime.KeepAlive(from)
	if r != 0 {
		return fmt.Errorf("move to Recycle Bin failed (SHFileOperation code 0x%x)", r)
	}
	if op.fAnyOperationsAborted != 0 {
		return fmt.Errorf("move to Recycle Bin was aborted")
	}
	return nil
}
