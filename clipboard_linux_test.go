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
