//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"go-clipboard/internal/wailsbridge"
)

// openFileWithDefaultApp hands the path to ShellExecuteW. The path is passed as
// a single wide-string argument, never through cmd.exe, so clip filenames
// containing &, ^, % or ( cannot be interpreted as shell syntax.
func openFileWithDefaultApp(path string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	if err := windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	return nil
}

// openWithCmd builds the command that opens filePath in appPath.
func openWithCmd(filePath, appPath string) *exec.Cmd {
	return exec.Command(appPath, filePath)
}

// openFileWithApp starts the application and returns at once; the process is
// reaped in the background so a long-lived editor never blocks the caller.
func openFileWithApp(filePath, appPath string) error {
	cmd := openWithCmd(filePath, appPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to open file with app: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

var (
	procSHOpenWithDialog  = modShell32.NewProc("SHOpenWithDialog")
	hresultErrorCancelled = uintptr(0x800704C7)
)

const (
	oaifAllowRegistration = 0x1
	oaifExec              = 0x4
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
	ch := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err == nil {
			defer windows.CoUninitialize()
		}
		info := openAsInfo{file: file, flags: oaifAllowRegistration | oaifExec}
		hr, _, _ := procSHOpenWithDialog.Call(0, uintptr(unsafe.Pointer(&info)))
		runtime.KeepAlive(file)
		if hr != 0 && hr != hresultErrorCancelled {
			ch <- fmt.Errorf("open with dialog failed (HRESULT=0x%08X)", hr)
			return
		}
		ch <- nil
	}()
	return <-ch
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
