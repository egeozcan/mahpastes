//go:build windows

package main

import "testing"

// Explorer only selects the file when the path alone is quoted after
// "/select,"; a quoted "/select,..." argument opens the default folder.
func TestExplorerSelectCmdLine(t *testing.T) {
	got := explorerSelectCmdLine(`C:\Users\me\My Files\a,b.txt`)
	want := `explorer.exe /select,"C:\Users\me\My Files\a,b.txt"`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}
