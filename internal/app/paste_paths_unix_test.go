//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A FIFO stats fine and is not a directory, but reading one blocks forever.
// The probe must mark it non-regular so it is never offered for import.
//
// Lives in a !windows file because syscall.Mkfifo does not exist on Windows —
// a runtime GOOS skip would still fail to compile there.
//
// This is deliberately the only coverage for the FIFO case. An e2e version was
// tried and removed: creating a named pipe mid-suite destabilized the shared
// `wails dev` build directory the e2e workers share, and it tested nothing the
// probe-level assertion below does not already pin down.
func TestProbeFilePathsMarksFIFONonRegular(t *testing.T) {
	fifoPath := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}

	a := &App{}
	probes, err := a.ProbeFilePaths([]string{fifoPath})
	if err != nil {
		t.Fatalf("ProbeFilePaths: %v", err)
	}
	if !probes[0].Exists || probes[0].IsDir {
		t.Fatalf("fifo probe = %+v, want an existing non-directory", probes[0])
	}
	if probes[0].IsRegular {
		t.Errorf("fifo probe reported IsRegular; reading it would block forever")
	}
}

// A symlink to a regular file is importable: os.Stat follows it, so the clip
// gets the target's bytes. This pins that the IsRegular gate does not
// accidentally exclude the common "alias on the Desktop" case.
func TestProbeFilePathsFollowsSymlinkToRegularFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	a := &App{}
	probes, err := a.ProbeFilePaths([]string{link})
	if err != nil {
		t.Fatalf("ProbeFilePaths: %v", err)
	}
	if !probes[0].Exists || !probes[0].IsRegular {
		t.Errorf("symlink probe = %+v, want an existing regular file", probes[0])
	}
}
