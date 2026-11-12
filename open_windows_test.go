//go:build windows

package main

import (
	"errors"
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

// stubOpen replaces the ShellExecute and chooser seams for one test.
func stubOpen(t *testing.T, exec func(verb, file, args *uint16) error) *[]string {
	t.Helper()
	origExec, origChooser := shellExecute, openWithChooser
	t.Cleanup(func() { shellExecute, openWithChooser = origExec, origChooser })
	shellExecute = func(_ windows.Handle, verb, file, args, _ *uint16, _ int32) error {
		return exec(verb, file, args)
	}
	var chooser []string
	openWithChooser = func(path string) error {
		chooser = append(chooser, path)
		return nil
	}
	return &chooser
}

// The clip path reaches ShellExecuteW verbatim as lpFile, with no parameters
// and the default verb: nothing re-parses shell metacharacters in it.
func TestOpenDefaultPassesPathVerbatim(t *testing.T) {
	const path = `C:\t\12_x&calc.exe&^%(a).txt`
	var gotFile string
	var gotVerb, gotArgs *uint16
	chooser := stubOpen(t, func(verb, file, args *uint16) error {
		gotVerb, gotArgs, gotFile = verb, args, windows.UTF16PtrToString(file)
		return nil
	})
	if err := openFileWithDefaultApp(path); err != nil {
		t.Fatal(err)
	}
	if gotFile != path || gotVerb != nil || gotArgs != nil {
		t.Fatalf("file %q verb %v args %v", gotFile, gotVerb, gotArgs)
	}
	if len(*chooser) != 0 {
		t.Fatalf("chooser shown: %q", *chooser)
	}
}

// A type with no registered handler falls back to the Open With chooser, as
// double-clicking it in Explorer does.
func TestOpenDefaultNoAssociationShowsChooser(t *testing.T) {
	chooser := stubOpen(t, func(_, _, _ *uint16) error { return windows.ERROR_NO_ASSOCIATION })
	if err := openFileWithDefaultApp(`C:\t\1_clip.unknownext`); err != nil {
		t.Fatal(err)
	}
	if len(*chooser) != 1 || (*chooser)[0] != `C:\t\1_clip.unknownext` {
		t.Fatalf("chooser %q", *chooser)
	}
}

func TestOpenDefaultErrors(t *testing.T) {
	stubOpen(t, func(_, _, _ *uint16) error { return windows.ERROR_CANCELLED })
	if err := openFileWithDefaultApp(`C:\t\a.txt`); err != nil {
		t.Fatalf("cancel reported as error: %v", err)
	}
	stubOpen(t, func(_, _, _ *uint16) error { return windows.ERROR_FILE_NOT_FOUND })
	if err := openFileWithDefaultApp(`C:\t\a.txt`); !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		t.Fatalf("got %v", err)
	}
}

// os/exec runs batch files through cmd.exe, which would re-parse the clip path.
func TestCheckOpenWithApp(t *testing.T) {
	for _, app := range []string{`C:\x\run.bat`, `C:\x\RUN.CMD`, `C:\x\run.bat.`, `C:\x\run.cmd `} {
		if checkOpenWithApp(app) == nil {
			t.Errorf("%q accepted", app)
		}
		if openFileWithApp(`C:\t\a&calc&.txt`, app) == nil {
			t.Errorf("openFileWithApp started %q", app)
		}
	}
	for _, app := range []string{`C:\Apps\ed.exe`, `C:\Apps\batch.exe`} {
		if err := checkOpenWithApp(app); err != nil {
			t.Errorf("%q rejected: %v", app, err)
		}
	}
}
