//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGTKBookmarkLifecycle(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "cfg", "gtk-3.0", "bookmarks")
	mount := filepath.Join(dir, "home", "Mahpastes")
	added, err := addGTKBookmark(file, mount)
	if err != nil || !added {
		t.Fatalf("add: %v %v", added, err)
	}
	if added, _ := addGTKBookmark(file, mount); added {
		t.Fatal("second add must be a no-op")
	}
	b, _ := os.ReadFile(file)
	if strings.Count(string(b), "Mahpastes\n") != 1 {
		t.Fatalf("bookmarks: %q", b)
	}
	if err := removeGTKBookmark(file, mount); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(file)
	if len(b) != 0 {
		t.Fatalf("left: %q", b)
	}
}

func TestGTKBookmarkPreservesUserEntries(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "bookmarks")
	mount := filepath.Join(dir, "Mahpastes")
	orig := "file:///a Alpha\nsmb://x/y Share"
	os.WriteFile(file, []byte(orig), 0o600)
	if added, err := addGTKBookmark(file, mount); err != nil || !added {
		t.Fatal(added, err)
	}
	if err := removeGTKBookmark(file, mount); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	if string(b) != orig+"\n" {
		t.Fatalf("got %q", b)
	}
	// A user's own entry is reported as not added.
	os.WriteFile(file, []byte(fileURLFromAbsPath(mount)+" Mine\n"), 0o600)
	if added, _ := addGTKBookmark(file, mount); added {
		t.Fatal("must not claim a user entry")
	}
}

const xbelSample = `<?xml version="1.0" encoding="UTF-8"?>
<xbel xmlns:bookmark="http://www.freedesktop.org/standards/desktop-bookmarks" xmlns:kdepriv="http://www.kde.org/kdepriv" xmlns:mime="http://www.freedesktop.org/standards/shared-mime-info">
 <bookmark href="file:///home/u">
  <title>Home</title>
 </bookmark>
</xbel>
`

func TestKDEPlaceLifecycle(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "user-places.xbel")
	mount := filepath.Join(dir, "Mahpastes & co")
	if added, err := addKDEPlace(file, mount); added || err != nil {
		t.Fatalf("missing file: %v %v", added, err)
	}
	if _, err := os.Stat(file); err == nil {
		t.Fatal("must not create the file")
	}
	os.WriteFile(file, []byte(xbelSample), 0o600)
	if added, err := addKDEPlace(file, mount); !added || err != nil {
		t.Fatalf("add: %v %v", added, err)
	}
	if added, _ := addKDEPlace(file, mount); added {
		t.Fatal("second add must be a no-op")
	}
	b, _ := os.ReadFile(file)
	if !strings.Contains(string(b), "<title>Home</title>") || strings.Count(string(b), "<title>Mahpastes</title>") != 1 {
		t.Fatalf("xbel: %s", b)
	}
	if err := removeKDEPlace(file, mount); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(file)
	if string(b) != xbelSample {
		t.Fatalf("not restored: %s", b)
	}
}

func TestKDEPlaceRefusesMalformed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "user-places.xbel")
	bad := "<xbel><bookmark href=\"x\"></xbel>"
	os.WriteFile(file, []byte(bad), 0o600)
	if _, err := addKDEPlace(file, "/m"); err == nil {
		t.Fatal("want error")
	}
	if b, _ := os.ReadFile(file); string(b) != bad {
		t.Fatal("file modified")
	}
}

func TestSidebarPathsHonourXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x/c")
	t.Setenv("XDG_DATA_HOME", "/x/d")
	if gtkBookmarksPath() != "/x/c/gtk-3.0/bookmarks" || kdePlacesPath() != "/x/d/user-places.xbel" {
		t.Fatal(gtkBookmarksPath(), kdePlacesPath())
	}
}
