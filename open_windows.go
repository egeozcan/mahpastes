//go:build windows

package main

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"go-clipboard/internal/wailsbridge"
)

// Seams for tests: the real Windows calls.
var (
	shellExecute    = windows.ShellExecute
	openWithChooser = openWithDialog
)

// openFileWithDefaultApp hands the path to ShellExecuteW. The path is passed as
// a single wide-string argument, never through cmd.exe, so clip filenames
// containing &, ^, % or ( cannot be interpreted as shell syntax. The verb is
// nil so the type's default verb is used, as Explorer and "start" do. A type
// with no registered handler gets the "Open with" chooser instead of an error.
func openFileWithDefaultApp(path string) error {
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	err = onSTAThread(func() error {
		return shellExecute(0, nil, file, nil, nil, windows.SW_SHOWNORMAL)
	})
	switch {
	case err == nil, errors.Is(err, windows.ERROR_CANCELLED):
		return nil
	case errors.Is(err, windows.ERROR_NO_ASSOCIATION):
		return openWithChooser(path)
	}
	return fmt.Errorf("failed to open file: %w", err)
}

// onSTAThread runs fn on a locked OS thread with a single-threaded COM
// apartment. ShellExecute may activate COM-based handlers and SHOpenWithDialog
// is a modal shell dialog; both need COM initialised on the calling thread.
func onSTAThread(fn func() error) error {
	ch := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		// S_FALSE means COM was already initialised on this thread; the call
		// still takes a reference that CoUninitialize must release.
		if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err != nil && err != syscall.Errno(windows.S_FALSE) {
			ch <- fmt.Errorf("COM initialisation failed: %w", err)
			return
		}
		defer windows.CoUninitialize()
		ch <- fn()
	}()
	return <-ch
}

// openWithCmd builds the command that opens filePath in appPath.
func openWithCmd(filePath, appPath string) *exec.Cmd {
	return exec.Command(appPath, filePath)
}

// checkOpenWithApp refuses batch files as the application: os/exec starts
// .bat and .cmd files through cmd.exe, which re-parses the command line, so a
// clip filename containing & or ^ could run a second command.
func checkOpenWithApp(appPath string) error {
	switch strings.ToLower(filepath.Ext(strings.TrimRight(appPath, ". "))) {
	case ".bat", ".cmd":
		return fmt.Errorf("batch files cannot be used to open clips: %s", appPath)
	}
	return nil
}

// openFileWithApp starts the application and returns at once; the process is
// reaped in the background so a long-lived editor never blocks the caller.
func openFileWithApp(filePath, appPath string) error {
	if err := checkOpenWithApp(appPath); err != nil {
		return err
	}
	cmd := openWithCmd(filePath, appPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to open file with app: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

var procSHOpenWithDialog = modShell32.NewProc("SHOpenWithDialog")

const (
	oaifAllowRegistration = 0x1
	oaifRegisterExt       = 0x2
	oaifExec              = 0x4

	// HRESULT_FROM_WIN32(ERROR_CANCELLED): the user closed the dialog.
	hresultErrorCancelled = 0x800704C7
)

// openAsInfo mirrors OPENASINFO.
type openAsInfo struct {
	file  *uint16
	class *uint16
	flags uint32
}

// openWithDialog shows the native "Open with" chooser and launches the file
// with the chosen handler. The dialog is modal, so it runs on a locked OS
// thread with COM initialised. Cancelling is not an error.
func openWithDialog(path string) error {
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	return onSTAThread(func() error {
		info := openAsInfo{file: file, flags: oaifAllowRegistration | oaifRegisterExt | oaifExec}
		r, _, _ := procSHOpenWithDialog.Call(0, uintptr(unsafe.Pointer(&info)))
		runtime.KeepAlive(&info)
		// The HRESULT is a 32-bit LONG; the upper half of the register is
		// undefined.
		if hr := uint32(r); hr != 0 && hr != hresultErrorCancelled {
			return fmt.Errorf("open with dialog failed (HRESULT=0x%08X)", hr)
		}
		return nil
	})
}

func supportsNativeOpenWith() bool { return true }

func chooseApplicationDialog(b *wailsbridge.Bridge) (string, error) {
	path, err := b.OpenFile(wailsbridge.FileDialogOptions{
		Title:            "Choose Application",
		DefaultDirectory: `C:\Program Files`,
		Filters: []wailsbridge.FileFilter{
			{DisplayName: "Executables", Pattern: "*.exe"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file dialog: %w", err)
	}
	return path, nil
}

// revealInFileManager opens Explorer with the file selected. Explorer parses
// its own command line and does not understand a fully quoted
// "/select,C:\a b\c.txt" argument (Go's default quoting once the path has a
// space), so the command line is written by hand with only the path quoted;
// Windows paths cannot contain '"'. explorer.exe exits non-zero even on
// success, so only a failure to start is an error.
func revealInFileManager(path string) error {
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: explorerSelectCmdLine(path)}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to reveal file: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func explorerSelectCmdLine(path string) string {
	return `explorer.exe /select,"` + filepath.Clean(path) + `"`
}
