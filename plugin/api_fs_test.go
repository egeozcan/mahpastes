package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWithinRoot(t *testing.T) {
	root := filepath.FromSlash("/data/app")
	cases := []struct {
		target string
		want   bool
	}{
		{filepath.FromSlash("/data/app"), true},
		{filepath.FromSlash("/data/app/sub/file"), true},
		{filepath.FromSlash("/data/other"), false},
		{filepath.FromSlash("/data"), false},
		{filepath.FromSlash("/data/app-evil"), false}, // sibling sharing a prefix
	}
	for _, c := range cases {
		if got := withinRoot(root, c.target); got != c.want {
			t.Errorf("withinRoot(%q, %q) = %v, want %v", root, c.target, got, c.want)
		}
	}
}

// TestCheckPermissionConfinementDeniesCachedOutsidePath covers L10: a grant in
// the (shared, desktop-populated) permissions cache must NOT let a confined
// headless instance reach a path outside its confinement root.
func TestCheckPermissionConfinementDeniesCachedOutsidePath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	canonRoot, err := canonicalizePluginPath(root)
	if err != nil {
		t.Fatalf("canonicalize root: %v", err)
	}

	f := &FilesystemAPI{
		wantsRead:       true,
		confinementRoot: canonRoot,
		approvedPaths: map[string]string{
			// A broad grant the desktop build could have stored, covering a dir
			// outside this server's confinement root.
			"fs_read:" + outside: outside,
			// A legitimate grant for the confinement root itself.
			"fs_read:" + root: root,
		},
	}

	// Cached grant covers it, but confinement must still deny it.
	if _, err := f.checkPermission("fs_read", filepath.Join(outside, "secret.txt")); err == nil {
		t.Fatal("confinement must deny an outside path even when a cached grant covers it")
	}

	// A path inside the root is allowed and returns a canonical op path.
	opPath, err := f.checkPermission("fs_read", filepath.Join(root, "sub", "a.txt"))
	if err != nil {
		t.Fatalf("in-confinement path should be allowed: %v", err)
	}
	if !withinRoot(canonRoot, opPath) {
		t.Fatalf("returned op path %q is not under the confinement root %q", opPath, canonRoot)
	}
}

// TestCheckPermissionConfinementDeniesSymlinkEscape covers L11: a symlink under
// the root that points outside it must be denied (resolved at check time, and
// the canonical path — not the un-resolved one — is what would be used).
func TestCheckPermissionConfinementDeniesSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	canonRoot, err := canonicalizePluginPath(root)
	if err != nil {
		t.Fatalf("canonicalize root: %v", err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	f := &FilesystemAPI{
		wantsRead:       true,
		confinementRoot: canonRoot,
		approvedPaths:   map[string]string{"fs_read:" + root: root},
	}

	if _, err := f.checkPermission("fs_read", filepath.Join(link, "secret.txt")); err == nil {
		t.Fatal("a symlink escaping the confinement root must be denied")
	}
}

func TestCheckPermissionUnconfinedKeepsDesktopBehavior(t *testing.T) {
	// With no confinement root (desktop), a cached ancestor grant is honored and
	// the (un-canonicalized) abs path is returned, as before.
	root := t.TempDir()
	f := &FilesystemAPI{
		wantsRead:     true,
		approvedPaths: map[string]string{"fs_read:" + root: root},
	}
	target := filepath.Join(root, "x.txt")
	got, err := f.checkPermission("fs_read", target)
	if err != nil {
		t.Fatalf("unconfined cached grant should be honored: %v", err)
	}
	if got != target {
		t.Fatalf("unconfined op path = %q, want %q", got, target)
	}
}
