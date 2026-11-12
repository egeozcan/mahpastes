//go:build windows && (amd64 || arm64)

package app

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// These exercise only the refusal rules; nothing reaches SHFileOperation, so
// the developer's Recycle Bin is untouched.
func TestRecycleBinRefusal(t *testing.T) {
	fixed := func(string) uint32 { return windows.DRIVE_FIXED }
	cases := []struct {
		name  string
		path  string
		drive func(string) uint32
		ok    bool
	}{
		{"fixed drive", `C:\Users\me\Pictures\a.png`, fixed, true},
		{"unc share", `\\server\share\a.png`, fixed, false},
		{"extended path", `\\?\C:\a.png`, fixed, false},
		{"relative", `Pictures\a.png`, fixed, false},
		{"wildcard", `C:\Users\me\*.png`, fixed, false},
		{"too long", `C:\` + strings.Repeat("a", windows.MAX_PATH), fixed, false},
		{"mapped network drive", `Z:\a.png`, func(string) uint32 { return windows.DRIVE_REMOTE }, false},
		{"removable", `E:\a.png`, func(string) uint32 { return windows.DRIVE_REMOVABLE }, false},
		{"ram disk", `R:\a.png`, func(string) uint32 { return windows.DRIVE_RAMDISK }, false},
		{"unknown", `Q:\a.png`, func(string) uint32 { return windows.DRIVE_UNKNOWN }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := recycleBinRefusal(tc.path, tc.drive)
			if tc.ok && err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("accepted a path the Recycle Bin cannot take")
			}
		})
	}
}

func TestRecycleBinRefusalQueriesTheVolumeRoot(t *testing.T) {
	var got string
	_ = recycleBinRefusal(`D:\photos\a.png`, func(root string) uint32 {
		got = root
		return windows.DRIVE_FIXED
	})
	if got != `D:\` {
		t.Fatalf("drive type queried for %q, want D:\\", got)
	}
}
