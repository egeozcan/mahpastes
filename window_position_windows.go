//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"

	"go-clipboard/internal/windowstate"
)

// nativeWindowPositioning is true on Windows because Wails' getter and setter
// disagree there: WindowGetPosition returns absolute virtual-desktop pixels
// (GetWindowRect), while WindowSetPosition adds the current monitor's
// work-area origin. Feeding one into the other drifts the window by the
// taskbar's size on each launch when the taskbar is on the left or top, and
// could never put it back on a second monitor. The saved absolute position is
// therefore checked against the real monitor layout and set with
// SetWindowPos directly.
const nativeWindowPositioning = true

// mainWindowClass is the Wails v2 default window class (options.Windows
// WindowClassName is not set in main.go).
const mainWindowClass = "wailsWindow"

const (
	monitorDefaultToNull = 0
	swpNoSize            = 0x0001
	swpNoZOrder          = 0x0004
	swpNoActivate        = 0x0010
)

var (
	modUser32                    = syscall.NewLazyDLL("user32.dll")
	procFindWindowExW            = modUser32.NewProc("FindWindowExW")
	procGetWindowThreadProcessID = modUser32.NewProc("GetWindowThreadProcessId")
	procGetWindowRect            = modUser32.NewProc("GetWindowRect")
	procMonitorFromRect          = modUser32.NewProc("MonitorFromRect")
	procGetMonitorInfoW          = modUser32.NewProc("GetMonitorInfoW")
	procSetWindowPos             = modUser32.NewProc("SetWindowPos")
)

type winRect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	CbSize    uint32
	RcMonitor winRect
	RcWork    winRect
	DwFlags   uint32
}

// findMainWindow returns this process's top-level Wails window. The class
// name alone is not enough: any other Wails app uses the same one.
func findMainWindow() uintptr {
	class, err := syscall.UTF16PtrFromString(mainWindowClass)
	if err != nil {
		return 0
	}
	pid := uint32(os.Getpid())
	var hwnd uintptr
	for {
		hwnd, _, _ = procFindWindowExW.Call(0, hwnd, uintptr(unsafe.Pointer(class)), 0)
		if hwnd == 0 {
			return 0
		}
		var owner uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner == pid {
			return hwnd
		}
	}
}

// moveWindowNative moves the main window's top-left corner to the absolute
// position x,y, but only when its title area would land on the work area of
// a connected monitor; otherwise the window keeps Wails' centring.
func moveWindowNative(x, y int) {
	hwnd := findMainWindow()
	if hwnd == 0 {
		return
	}
	var cur winRect
	if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&cur))); ok == 0 {
		return
	}
	w := int(cur.Right - cur.Left)
	// Probe with the title strip: the monitor it overlaps most is the one the
	// window would be grabbed on.
	title := winRect{Left: int32(x), Top: int32(y), Right: int32(x + w), Bottom: int32(y + 32)}
	mon, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&title)), monitorDefaultToNull)
	if mon == 0 {
		return
	}
	mi := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	if ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return
	}
	work := windowstate.Rect{
		X:      int(mi.RcWork.Left),
		Y:      int(mi.RcWork.Top),
		Width:  int(mi.RcWork.Right - mi.RcWork.Left),
		Height: int(mi.RcWork.Bottom - mi.RcWork.Top),
	}
	if !windowstate.TitleVisible(x, y, w, work) {
		return
	}
	procSetWindowPos.Call(hwnd, 0, uintptr(int32(x)), uintptr(int32(y)), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
}
