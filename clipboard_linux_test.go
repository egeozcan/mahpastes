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

// Opt in: replaces the system clipboard.
func TestCopyFilesToClipboardWayland(t *testing.T) {
	if os.Getenv("MAHPASTES_TEST_CLIPBOARD") != "1" {
		t.Skip("set MAHPASTES_TEST_CLIPBOARD=1 to test the desktop clipboard")
	}
	t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
	if err := copyFilesToClipboard([]string{"/tmp/mahpastes-a.txt"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := exec.CommandContext(ctx, "wl-paste", "--type", "text/uri-list", "--no-newline").Output()
	if err != nil {
		t.Fatalf("read uri-list from independent Wayland client: %v", err)
	}
	if string(got) != "file:///tmp/mahpastes-a.txt\r\n" {
		t.Fatalf("got %q", got)
	}
}
