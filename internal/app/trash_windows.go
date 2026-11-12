//go:build windows && (amd64 || arm64)

package app

import (
	"fmt"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SHFILEOPSTRUCTW has natural alignment on amd64/arm64 (it is packed only on
// 32-bit x86, which is not built), so a plain Go struct matches.
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

const (
	foDelete          = 0x0003
	fofSilent         = 0x0004
	fofNoConfirmation = 0x0010
	fofAllowUndo      = 0x0040
	fofNoErrorUI      = 0x0400
)

var procSHFileOperationW = windows.NewLazySystemDLL("shell32.dll").NewProc("SHFileOperationW")

// trashIsRecoverable is true: deletions go to the Recycle Bin. Files on
// volumes without one (network shares) are refused by moveToTrash rather than
// deleted permanently.
func trashIsRecoverable() bool { return true }

// moveToTrash sends the file to the Recycle Bin with SHFileOperationW.
func moveToTrash(absPath string) error {
	if strings.HasPrefix(absPath, `\\`) {
		return fmt.Errorf("move to Recycle Bin: %s is on a network or extended path with no Recycle Bin", absPath)
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
	if r != 0 {
		return fmt.Errorf("move to Recycle Bin failed (SHFileOperation code 0x%x)", r)
	}
	if op.fAnyOperationsAborted != 0 {
		return fmt.Errorf("move to Recycle Bin was aborted")
	}
	return nil
}
