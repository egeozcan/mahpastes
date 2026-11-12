//go:build linux

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every test here works on temp files only; none reads or writes the real
// user's bookmarks or Places.

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
	if string(b) != fileURLFromAbsPath(mount)+" Mahpastes\n" {
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
	if err := os.WriteFile(file, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	if added, err := addGTKBookmark(file, mount); err != nil || !added {
		t.Fatal(added, err)
	}
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode changed to %v", st.Mode().Perm())
	}
	if err := removeGTKBookmark(file, mount); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	if string(b) != orig+"\n" {
		t.Fatalf("got %q", b)
	}

	// A user's own entry, labelled or not and however it is escaped, is
	// reported as not added and survives removal.
	for _, own := range []string{
		fileURLFromAbsPath(mount) + " Mine\n",
		fileURLFromAbsPath(mount) + "/\n",
	} {
		if err := os.WriteFile(file, []byte(own), 0o600); err != nil {
			t.Fatal(err)
		}
		if added, _ := addGTKBookmark(file, mount); added {
			t.Fatalf("must not claim a user entry %q", own)
		}
		if err := removeGTKBookmark(file, mount); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(file); string(b) != own {
			t.Fatalf("user entry removed: %q", b)
		}
	}
}

func TestSidebarEditsFollowSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "bookmarks")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("file:///a Alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "gtk-3.0", "bookmarks")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	mount := filepath.Join(dir, "Mahpastes")
	if added, err := addGTKBookmark(link, mount); err != nil || !added {
		t.Fatal(added, err)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink replaced: %v %v", st, err)
	}
	if b, _ := os.ReadFile(target); !strings.Contains(string(b), "Mahpastes") {
		t.Fatalf("target not updated: %q", b)
	}
	if err := removeGTKBookmark(link, mount); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink replaced on remove: %v %v", st, err)
	}
	if b, _ := os.ReadFile(target); string(b) != "file:///a Alpha\n" {
		t.Fatalf("target not restored: %q", b)
	}
}

const xbelSample = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE xbel>
<xbel xmlns:bookmark="http://www.freedesktop.org/standards/desktop-bookmarks" xmlns:kdepriv="http://www.kde.org/kdepriv" xmlns:mime="http://www.freedesktop.org/standards/shared-mime-info">
 <info>
  <metadata owner="http://www.kde.org">
   <kde_places_version>4</kde_places_version>
  </metadata>
 </info>
 <bookmark href="file:///home/u">
  <title>Home</title>
  <info>
   <metadata owner="http://freedesktop.org">
    <bookmark:icon name="user-home"/>
   </metadata>
   <metadata owner="http://www.kde.org">
    <ID>1700000000/0</ID>
    <isSystemItem>true</isSystemItem>
   </metadata>
  </info>
 </bookmark>
 <separator/>
 <bookmark href="trash:/"/>
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
	if err := os.WriteFile(file, []byte(xbelSample), 0o600); err != nil {
		t.Fatal(err)
	}
	if added, err := addKDEPlace(file, mount); !added || err != nil {
		t.Fatalf("add: %v %v", added, err)
	}
	if added, _ := addKDEPlace(file, mount); added {
		t.Fatal("second add must be a no-op")
	}
	b, _ := os.ReadFile(file)
	places, err := parseXBEL(b)
	if err != nil || len(places) != 3 {
		t.Fatalf("parse after add: %v %+v", err, places)
	}
	last := places[2]
	if !sameFileURL(last.href, fileURLFromAbsPath(mount)) || last.id != kdePlaceID(fileURLFromAbsPath(mount)) {
		t.Fatalf("added place = %+v", last)
	}
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

func TestKDEPlaceIDsDifferPerMount(t *testing.T) {
	a, b := fileURLFromAbsPath("/home/u/Mahpastes"), fileURLFromAbsPath("/home/u/dev/Mahpastes")
	if kdePlaceID(a) == kdePlaceID(b) {
		t.Fatal("two mounts share one Places ID")
	}
}

// After Dolphin rewrote the file, the app's place may be laid out
// differently; removal still finds it by ID and leaves the user's own place
// for the same folder (another ID) and every neighbour untouched.
func TestKDEPlaceRemovesOnlyOwnEntry(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "user-places.xbel")
	mount := filepath.Join(dir, "Mahpastes")
	href := fileURLFromAbsPath(mount)
	user := ` <bookmark href="` + href + `/">
  <title>My clips</title>
  <info>
   <metadata owner="http://www.kde.org">
    <ID>1700000001/3</ID>
   </metadata>
  </info>
 </bookmark>
`
	ours := `<bookmark  href='` + href + `' ><title>Mahpastes</title><info><metadata owner="http://www.kde.org"><ID>` + kdePlaceID(href) + `</ID></metadata></info></bookmark>`
	doc := strings.Replace(xbelSample, " <separator/>\n", user+" <separator/>\n "+ours+"\n", 1)
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeKDEPlace(file, mount); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	want := strings.Replace(xbelSample, " <separator/>\n", user+" <separator/>\n", 1)
	if string(b) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", b, want)
	}
	// Only the user's place is left: nothing more to remove.
	if err := removeKDEPlace(file, mount); err != nil {
		t.Fatal(err)
	}
	if b2, _ := os.ReadFile(file); string(b2) != want {
		t.Fatal("user place removed")
	}
	// And it counts as an existing entry, so add claims nothing.
	if added, err := addKDEPlace(file, mount); added || err != nil {
		t.Fatalf("add over a user place: %v %v", added, err)
	}
}

func TestKDEPlaceRefusesMalformed(t *testing.T) {
	dir := t.TempDir()
	for i, bad := range []string{
		"<xbel><bookmark href=\"x\"></xbel>",
		"<xbel><bookmark href=\"x\"></bookmark>",
		"",
		"<notxbel></notxbel>",
	} {
		file := filepath.Join(dir, "user-places-"+string(rune('a'+i))+".xbel")
		if err := os.WriteFile(file, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := addKDEPlace(file, "/m"); err == nil {
			t.Fatalf("add %q: want error", bad)
		}
		if err := removeKDEPlace(file, "/m"); err == nil {
			t.Fatalf("remove %q: want error", bad)
		}
		if b, _ := os.ReadFile(file); string(b) != bad {
			t.Fatalf("%q modified", bad)
		}
	}
}

func TestSidebarPathsHonourXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x/c")
	t.Setenv("XDG_DATA_HOME", "/x/d")
	if gtkBookmarksPath() != "/x/c/gtk-3.0/bookmarks" || kdePlacesPath() != "/x/d/user-places.xbel" {
		t.Fatal(gtkBookmarksPath(), kdePlacesPath())
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "relative")
	t.Setenv("XDG_DATA_HOME", "")
	if gtkBookmarksPath() != filepath.Join(home, ".config/gtk-3.0/bookmarks") || kdePlacesPath() != filepath.Join(home, ".local/share/user-places.xbel") {
		t.Fatal(gtkBookmarksPath(), kdePlacesPath())
	}
}

// The service records what it added and removes exactly that; a test or
// development profile (MAHPASTES_DATA_DIR) never edits the sidebars.
func TestFileProviderSidebarEnrollment(t *testing.T) {
	cfg, data := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("MAHPASTES_DATA_DIR", "")
	places := filepath.Join(data, "user-places.xbel")
	if err := os.WriteFile(places, []byte(xbelSample), 0o600); err != nil {
		t.Fatal(err)
	}
	gtkFile := filepath.Join(cfg, "gtk-3.0", "bookmarks")
	dataDir := t.TempDir()
	mount := filepath.Join(t.TempDir(), "Mahpastes")
	s := &FileProviderService{dataDir: dataDir, enrollment: providerEnrollment{Enabled: true, MountPath: mount}}

	saved := func() providerEnrollment {
		t.Helper()
		var e providerEnrollment
		b, err := os.ReadFile(filepath.Join(dataDir, "file-provider.json"))
		if err == nil {
			err = json.Unmarshal(b, &e)
		}
		if err != nil {
			t.Fatal(err)
		}
		return e
	}

	s.surfaceInSidebars()
	if e := saved(); !e.GTKBookmarkAdded || !e.KDEPlaceAdded {
		t.Fatalf("enrollment after surface = %+v", e)
	}
	if b, _ := os.ReadFile(gtkFile); !strings.Contains(string(b), fileURLFromAbsPath(mount)) {
		t.Fatalf("gtk bookmarks = %q", b)
	}
	s.unsurfaceFromSidebars()
	if e := saved(); e.GTKBookmarkAdded || e.KDEPlaceAdded {
		t.Fatalf("enrollment after unsurface = %+v", e)
	}
	if b, _ := os.ReadFile(places); string(b) != xbelSample {
		t.Fatalf("places not restored: %s", b)
	}
	if b, _ := os.ReadFile(gtkFile); len(b) != 0 {
		t.Fatalf("gtk bookmarks left: %q", b)
	}

	// A failed removal keeps only that entry's flag, so the next disable
	// retries it.
	s.surfaceInSidebars()
	if err := os.WriteFile(places, []byte("<xbel>"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.unsurfaceFromSidebars()
	if e := saved(); e.GTKBookmarkAdded || !e.KDEPlaceAdded {
		t.Fatalf("enrollment after partial unsurface = %+v", e)
	}

	// A test or development profile leaves the sidebars alone.
	s2 := &FileProviderService{dataDir: t.TempDir(), enrollment: providerEnrollment{Enabled: true, MountPath: filepath.Join(t.TempDir(), "Mahpastes")}}
	t.Setenv("MAHPASTES_DATA_DIR", s2.dataDir)
	before, _ := os.ReadFile(gtkFile)
	s2.surfaceInSidebars()
	if after, _ := os.ReadFile(gtkFile); string(after) != string(before) || s2.enrollment.GTKBookmarkAdded || s2.enrollment.KDEPlaceAdded {
		t.Fatalf("dev profile edited the sidebars: %q", after)
	}
}
