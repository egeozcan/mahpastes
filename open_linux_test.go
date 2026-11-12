//go:build linux

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOpenWithCommands(t *testing.T) {
	got := openWithCommands("/tmp/a b.png", "/var/lib/flatpak/exports/share/applications/org.kde.gwenview.desktop")
	want := [][]string{
		{"gio", "launch", "/var/lib/flatpak/exports/share/applications/org.kde.gwenview.desktop", "/tmp/a b.png"},
		{"gtk-launch", "org.kde.gwenview", "/tmp/a b.png"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	got = openWithCommands("/tmp/x", "/usr/bin/gimp")
	if !reflect.DeepEqual(got, [][]string{{"/usr/bin/gimp", "/tmp/x"}}) {
		t.Fatalf("plain exec: %v", got)
	}
}

func TestDefaultOpenCommands(t *testing.T) {
	want := [][]string{{"xdg-open", "/tmp/x"}, {"gio", "open", "/tmp/x"}}
	if got := defaultOpenCommands("/tmp/x"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestRevealCommands(t *testing.T) {
	c := revealDBusCommand("/tmp/my dir/a#b,c.txt")
	// dbus-send splits array:string: values on commas, so a comma in the
	// name must arrive escaped or the file manager gets two bogus URIs.
	if got := c[len(c)-2]; got != "array:string:file:///tmp/my%20dir/a%23b%2Cc.txt" {
		t.Fatalf("bad uri argument: %q", got)
	}
	if c[0] != "dbus-send" || c[len(c)-1] != "string:" {
		t.Fatalf("bad dbus-send argv: %v", c)
	}
	want := [][]string{{"xdg-open", "/tmp/my dir"}, {"gio", "open", "/tmp/my dir"}}
	if got := revealFallbackCommands("/tmp/my dir/a#b,c.txt"); !reflect.DeepEqual(got, want) {
		t.Fatalf("bad fallback: %v", got)
	}
}

// writeScript puts an executable shell script named name in dir.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStartFirstAvailableFallsBack(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	fail := writeScript(t, dir, "fail", "exit 3")
	ok := writeScript(t, dir, "ok", `echo "$1" > "`+marker+`"`)

	// A launcher that is missing or exits non-zero at once (xdg-open with no
	// handler, gio without "launch") hands over to the next command.
	err := startFirstAvailable([][]string{
		{"definitely-not-a-binary-xyz", "a"},
		{fail, "a"},
		{ok, "picked"},
	})
	if err != nil {
		t.Fatalf("startFirstAvailable: %v", err)
	}
	if b, _ := os.ReadFile(marker); strings.TrimSpace(string(b)) != "picked" {
		t.Fatalf("fallback did not run, marker = %q", b)
	}
}

func TestStartFirstAvailableReportsEveryFailure(t *testing.T) {
	dir := t.TempDir()
	fail := writeScript(t, dir, "fail", "exit 4")
	err := startFirstAvailable([][]string{{"definitely-not-a-binary-xyz"}, {fail}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-binary-xyz") || !strings.Contains(err.Error(), "exit status 4") {
		t.Fatalf("error does not name both failures: %v", err)
	}
	if err := startFirstAvailable(nil); err == nil {
		t.Fatal("expected an error for no commands")
	}
}

// A launcher still running after the grace period (xdg-open's generic mode
// waits for the handler) counts as launched, and the next command is not run.
func TestStartFirstAvailableDoesNotWaitForHandler(t *testing.T) {
	old := launchGrace
	launchGrace = 100 * time.Millisecond
	t.Cleanup(func() { launchGrace = old })

	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	slow := writeScript(t, dir, "slow", "sleep 5")
	next := writeScript(t, dir, "next", `touch "`+marker+`"`)

	start := time.Now()
	if err := startFirstAvailable([][]string{{slow}, {next}}); err != nil {
		t.Fatalf("startFirstAvailable: %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("waited %v for a running launcher", d)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("next command ran although the first one launched")
	}
}

func TestDefaultApplicationDirPicksFullest(t *testing.T) {
	root := t.TempDir()
	mk := func(name string, files ...string) string {
		d := filepath.Join(root, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(d, f), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return d
	}
	system := mk("system", "a.desktop", "mimeinfo.cache")
	flatpak := mk("flatpak", "org.a.desktop", "org.b.desktop")
	user := mk("user", "x.desktop")
	missing := filepath.Join(root, "missing")

	if got := defaultApplicationDir([]string{system, missing, flatpak, user}); got != flatpak {
		t.Fatalf("got %s want %s", got, flatpak)
	}
	// Ties keep the earlier directory.
	if got := defaultApplicationDir([]string{user, system}); got != user {
		t.Fatalf("tie: got %s want %s", got, user)
	}
	if got := defaultApplicationDir([]string{missing}); got != "/usr/share/applications" {
		t.Fatalf("nothing found: got %s", got)
	}
}

func TestApplicationDirsCoverSandboxedAndUserApps(t *testing.T) {
	dirs := applicationDirs("/home/u")
	for _, want := range []string{
		"/usr/share/applications",
		"/var/lib/flatpak/exports/share/applications",
		"/var/lib/snapd/desktop/applications",
		"/home/u/.local/share/flatpak/exports/share/applications",
		"/home/u/.local/share/applications",
	} {
		found := false
		for _, d := range dirs {
			found = found || d == want
		}
		if !found {
			t.Errorf("missing %s in %v", want, dirs)
		}
	}
	if len(applicationDirs("")) != 3 {
		t.Errorf("no home should drop the per-user dirs: %v", applicationDirs(""))
	}
}
