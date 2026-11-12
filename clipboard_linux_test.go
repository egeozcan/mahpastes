//go:build linux

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// Opt in on a real Wayland desktop: this test replaces the system clipboard.
func TestCopyImageToClipboardWayland(t *testing.T) {
	if os.Getenv("MAHPASTES_TEST_CLIPBOARD") != "1" {
		t.Skip("set MAHPASTES_TEST_CLIPBOARD=1 to test the desktop clipboard")
	}
	pngData, err := os.ReadFile("build/appicon.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := copyImageToClipboard(pngData); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := exec.CommandContext(ctx, "wl-paste", "--type", "image/png", "--no-newline").Output()
	if err != nil {
		t.Fatalf("read image from independent Wayland client: %v", err)
	}
	if !bytes.Equal(got, pngData) {
		t.Fatalf("clipboard PNG differs: got %d bytes, want %d", len(got), len(pngData))
	}
}

func TestClipboardFilePayload(t *testing.T) {
	paths := []string{"/tmp/a b.txt", "/tmp/c.png"}
	mime, body := clipboardFilePayload(paths, "KDE")
	if mime != "text/uri-list" || string(body) != "file:///tmp/a%20b.txt\r\nfile:///tmp/c.png\r\n" {
		t.Fatalf("uri-list: %q %q", mime, body)
	}
	mime, body = clipboardFilePayload(paths, "X-Cinnamon")
	if mime != "x-special/gnome-copied-files" || string(body) != "copy\nfile:///tmp/a%20b.txt\nfile:///tmp/c.png" {
		t.Fatalf("gnome: %q %q", mime, body)
	}
}

func TestClipboardFilePayloadDesktopSelection(t *testing.T) {
	cases := map[string]string{
		"":              "text/uri-list",
		"KDE":           "text/uri-list",
		"GNOME":         "text/uri-list",
		"ubuntu:GNOME":  "text/uri-list",
		"LXQt":          "text/uri-list",
		"X-Cinnamon":    "x-special/gnome-copied-files",
		"MATE":          "x-special/gnome-copied-files",
		"XFCE":          "x-special/gnome-copied-files",
		"Budgie:GNOME":  "x-special/gnome-copied-files",
		"Pantheon":      "x-special/gnome-copied-files",
		"ULTIMATE":      "text/uri-list",
		"KDE:X-MATEish": "text/uri-list",
	}
	for desktop, want := range cases {
		if mime, _ := clipboardFilePayload([]string{"/tmp/x"}, desktop); mime != want {
			t.Errorf("XDG_CURRENT_DESKTOP=%q: got %s, want %s", desktop, mime, want)
		}
	}
}

func TestClipboardFilePayloadEscapesURIs(t *testing.T) {
	// A newline, '#' or '?' in a file name must not split or truncate the URI.
	_, body := clipboardFilePayload([]string{"/tmp/a\nb#c?d é.txt"}, "KDE")
	if want := "file:///tmp/a%0Ab%23c%3Fd%20%C3%A9.txt\r\n"; string(body) != want {
		t.Fatalf("got %q, want %q", body, want)
	}
}

// Opt in: replaces the system clipboard.
func TestCopyFilesToClipboardWayland(t *testing.T) {
	if os.Getenv("MAHPASTES_TEST_CLIPBOARD") != "1" {
		t.Skip("set MAHPASTES_TEST_CLIPBOARD=1 to test the desktop clipboard")
	}
	for _, tc := range []struct{ desktop, mime, want string }{
		{"KDE", "text/uri-list", "file:///tmp/mahpastes-a.txt\r\nfile:///tmp/mahpastes-b.txt\r\n"},
		{"XFCE", "x-special/gnome-copied-files", "copy\nfile:///tmp/mahpastes-a.txt\nfile:///tmp/mahpastes-b.txt"},
	} {
		t.Setenv("XDG_CURRENT_DESKTOP", tc.desktop)
		if err := copyFilesToClipboard([]string{"/tmp/mahpastes-a.txt", "/tmp/mahpastes-b.txt"}); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		got, err := exec.CommandContext(ctx, "wl-paste", "--type", tc.mime, "--no-newline").Output()
		cancel()
		if err != nil {
			t.Fatalf("%s: read %s from independent Wayland client: %v", tc.desktop, tc.mime, err)
		}
		if string(got) != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.desktop, got, tc.want)
		}
	}
}
