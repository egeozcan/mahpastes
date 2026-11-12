//go:build windows

package main

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// Explorer only selects the file when the path alone is quoted after
// "/select,"; a quoted "/select,..." argument opens the default folder.
func TestExplorerSelectCmdLine(t *testing.T) {
	got := explorerSelectCmdLine(`C:\Users\me\My Files\a,b.txt`)
	want := `explorer.exe /select,"C:\Users\me\My Files\a,b.txt"`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

// The file path must reach the app as one argv element, and the app launch
// must never go through cmd.exe.
func TestOpenWithCmdArgs(t *testing.T) {
	cmd := openWithCmd(`C:\t\x&calc.exe&.txt`, `C:\Apps\ed.exe`)
	if len(cmd.Args) != 2 || cmd.Args[1] != `C:\t\x&calc.exe&.txt` {
		t.Fatalf("args %q", cmd.Args)
	}
	if filepath.Base(cmd.Path) == "cmd.exe" {
		t.Fatal("must not use cmd.exe")
	}
}

func TestHiddenCommandNoWindow(t *testing.T) {
	cmd := hiddenCommand("powershell", "-NoProfile")
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatalf("attrs %+v", cmd.SysProcAttr)
	}
}
