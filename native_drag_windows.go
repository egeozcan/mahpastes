//go:build windows

package main

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
)

var (
	modShell32 = syscall.NewLazyDLL("shell32.dll")
	modOle32   = syscall.NewLazyDLL("ole32.dll")

	procSHParseDisplayName = modShell32.NewProc("SHParseDisplayName")
	procSHCreateDataObject = modShell32.NewProc("SHCreateDataObject")
	procILClone            = modShell32.NewProc("ILClone")
	procILFindLastID       = modShell32.NewProc("ILFindLastID")
	procILRemoveLastID     = modShell32.NewProc("ILRemoveLastID")
	procDoDragDrop         = modOle32.NewProc("DoDragDrop")
	procCoTaskMemFree      = modOle32.NewProc("CoTaskMemFree")
	procOleInitialize      = modOle32.NewProc("OleInitialize")
	procOleUninitialize    = modOle32.NewProc("OleUninitialize")
)

// COM constants
const (
	dropEffectCopy = 1
	dropEffectMove = 2
	dropEffectLink = 4

	dragDropSUseDefaultCursors = 0x00040102
)

// COM interface IIDs
var (
	iidIDropSource  = ole.NewGUID("00000121-0000-0000-C000-000000000046")
	iidIDataObject  = ole.NewGUID("0000010e-0000-0000-C000-000000000046")
)

// dropSource implements the COM IDropSource interface via a manual vtable.
type dropSource struct {
	vtbl     *dropSourceVtbl
	refCount int32
}

type dropSourceVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	QueryContinueDrag uintptr
	GiveFeedback     uintptr
}

func newDropSource() *dropSource {
	ds := &dropSource{refCount: 1}
	ds.vtbl = &dropSourceVtbl{
		QueryInterface:    syscall.NewCallback(dsQueryInterface),
		AddRef:            syscall.NewCallback(dsAddRef),
		Release:           syscall.NewCallback(dsRelease),
		QueryContinueDrag: syscall.NewCallback(dsQueryContinueDrag),
		GiveFeedback:      syscall.NewCallback(dsGiveFeedback),
	}
	return ds
}

func dsQueryInterface(this unsafe.Pointer, riid *ole.GUID, ppv *unsafe.Pointer) uintptr {
	if ole.IsEqualGUID(riid, ole.IID_IUnknown) || ole.IsEqualGUID(riid, iidIDropSource) {
		*ppv = this
		dsAddRef(this)
		return ole.S_OK
	}
	*ppv = nil
	return ole.E_NOINTERFACE
}

func dsAddRef(this unsafe.Pointer) uintptr {
	ds := (*dropSource)(this)
	ds.refCount++
	return uintptr(ds.refCount)
}

func dsRelease(this unsafe.Pointer) uintptr {
	ds := (*dropSource)(this)
	ds.refCount--
	return uintptr(ds.refCount)
}

// dsQueryContinueDrag is called by OLE during the drag loop.
// fEscapePressed: nonzero if Escape was pressed.
// grfKeyState: current key/button state (MK_LBUTTON = 0x0001).
func dsQueryContinueDrag(_ unsafe.Pointer, fEscapePressed int32, grfKeyState uint32) uintptr {
	if fEscapePressed != 0 {
		// DRAGDROP_S_CANCEL
		return 0x00040101
	}
	// If the left mouse button is released, drop.
	const mkLButton = 0x0001
	if grfKeyState&mkLButton == 0 {
		// DRAGDROP_S_DROP
		return 0x00040100
	}
	return ole.S_OK
}

func dsGiveFeedback(_ unsafe.Pointer, _ uint32) uintptr {
	return uintptr(dragDropSUseDefaultCursors)
}

func startNativeFileDrag(absPath string) error {
	if absPath == "" {
		return fmt.Errorf("native drag failed (code 2)")
	}
	if _, err := os.Stat(absPath); err != nil {
		return fmt.Errorf("native drag failed (code 3)")
	}

	// Channel to collect the result from the COM thread.
	type result struct {
		err error
	}
	ch := make(chan result, 1)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		// OleInitialize is required (not just CoInitializeEx) because DoDragDrop
		// depends on the OLE drag-and-drop subsystem that OleInitialize sets up.
		hr, _, _ := procOleInitialize.Call(0)
		if hr != 0 && hr != 1 { // S_OK = 0, S_FALSE = 1 (already initialized)
			ch <- result{fmt.Errorf("native drag failed (code 1)")}
			return
		}
		defer procOleUninitialize.Call()

		pathUTF16, err := syscall.UTF16PtrFromString(absPath)
		if err != nil {
			ch <- result{fmt.Errorf("native drag failed (code 2)")}
			return
		}

		// Parse the full path into a PIDL.
		var fullPIDL uintptr
		hr, _, _ = procSHParseDisplayName.Call(
			uintptr(unsafe.Pointer(pathUTF16)),
			0,
			uintptr(unsafe.Pointer(&fullPIDL)),
			0,
			0,
		)
		if hr != 0 || fullPIDL == 0 {
			ch <- result{fmt.Errorf("native drag failed (code 4)")}
			return
		}
		defer procCoTaskMemFree.Call(fullPIDL)

		// Clone the PIDL, then split into parent + child.
		parentPIDL, _, _ := procILClone.Call(fullPIDL)
		if parentPIDL == 0 {
			ch <- result{fmt.Errorf("native drag failed (code 4)")}
			return
		}
		defer procCoTaskMemFree.Call(parentPIDL)

		procILRemoveLastID.Call(parentPIDL)

		childPIDL, _, _ := procILFindLastID.Call(fullPIDL)
		if childPIDL == 0 {
			ch <- result{fmt.Errorf("native drag failed (code 4)")}
			return
		}
		// childPIDL points into fullPIDL — do not free separately.

		// Create IDataObject from the shell item.
		var dataObj *ole.IUnknown
		hr, _, _ = procSHCreateDataObject.Call(
			parentPIDL,
			1,
			uintptr(unsafe.Pointer(&childPIDL)),
			0,
			uintptr(unsafe.Pointer(iidIDataObject)),
			uintptr(unsafe.Pointer(&dataObj)),
		)
		if hr != 0 || dataObj == nil {
			ch <- result{fmt.Errorf("native drag failed (code 5)")}
			return
		}
		defer dataObj.Release()

		// Create our IDropSource implementation.
		ds := newDropSource()

		// Call DoDragDrop — this blocks until the user drops or cancels.
		var dwEffect uint32
		allowedEffects := uint32(dropEffectCopy | dropEffectMove | dropEffectLink)
		hr, _, _ = procDoDragDrop.Call(
			uintptr(unsafe.Pointer(dataObj)),
			uintptr(unsafe.Pointer(ds)),
			uintptr(allowedEffects),
			uintptr(unsafe.Pointer(&dwEffect)),
		)

		// DoDragDrop returns S_OK on successful drop, DRAGDROP_S_DROP on drop,
		// DRAGDROP_S_CANCEL on cancel. All are acceptable outcomes.
		const dragDropSDrop = 0x00040100
		const dragDropSCancel = 0x00040101
		if hr != 0 && hr != uintptr(dragDropSDrop) && hr != uintptr(dragDropSCancel) {
			ch <- result{fmt.Errorf("native drag failed (code 6)")}
			return
		}

		ch <- result{nil}
	}()

	res := <-ch
	return res.err
}
