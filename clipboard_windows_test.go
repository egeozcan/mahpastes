//go:build windows

package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Names PowerShell would read as code if they were ever written into a
// script: it takes U+2018..U+201B as single-quote delimiters as well as '.
var hostileClipNames = []string{
	"Bob’s notes.txt",
	"x’;Start-Process calc;‘.png",
	"it's $(calc) `n.txt",
	"a‚b‛c.txt",
}

func TestClipboardScriptsNeverContainPaths(t *testing.T) {
	for _, script := range []string{fileDropScript, imageCopyScript} {
		if strings.Contains(script, "%") {
			t.Errorf("script takes formatted input: %s", script)
		}
	}
	paths := make([]string, len(hostileClipNames))
	for i, name := range hostileClipNames {
		paths[i] = filepath.Join(`C:\clips`, name)
	}
	t.Setenv(clipboardPathEnvPrefix+"7", "stale")
	env := clipboardScriptEnv(paths)
	for i, p := range paths {
		want := clipboardPathEnvPrefix + string(rune('0'+i)) + "=" + p
		found := false
		for _, kv := range env {
			if kv == want {
				found = true
			}
		}
		if !found {
			t.Errorf("environment lacks %q", want)
		}
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, clipboardPathEnvPrefix+"7=") {
			t.Errorf("stale path variable inherited: %s", kv)
		}
	}
}

// Puts real files on the real clipboard, so it only runs when asked:
// MAHPASTES_TEST_REAL_CLIPBOARD=1 go test -run TestCopyFilesToClipboardWithHostileNames .
func TestCopyFilesToClipboardWithHostileNames(t *testing.T) {
	if os.Getenv("MAHPASTES_TEST_REAL_CLIPBOARD") != "1" {
		t.Skip("overwrites the clipboard; set MAHPASTES_TEST_REAL_CLIPBOARD=1 to run")
	}
	dir := t.TempDir()
	// A name that is a statement list once a typographic quote closes the
	// string it was written into. Only characters a file name may hold (no
	// colon), so the marker would land in the working directory.
	markerName := "mahpastes-injected-" + filepath.Base(filepath.Dir(dir))
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(cwd, markerName)
	t.Cleanup(func() { os.Remove(marker) })
	var paths []string
	for _, name := range append([]string{"x’;New-Item -Name " + markerName + " -ItemType File;‘.txt"}, hostileClipNames...) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	if err := copyFilesToClipboard(paths); err != nil {
		t.Fatalf("copyFilesToClipboard: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a file name was executed as PowerShell")
	}
	out, err := hiddenCommand("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Add-Type -AssemblyName System.Windows.Forms; [Console]::OutputEncoding = [Text.Encoding]::UTF8; [System.Windows.Forms.Clipboard]::GetFileDropList() | ForEach-Object { $_ }").Output()
	if err != nil {
		t.Fatalf("read clipboard: %v", err)
	}
	got := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(out), "\r\n", "\n")), "\n")
	if len(got) != len(paths) {
		t.Fatalf("clipboard holds %d files, want %d: %q", len(got), len(paths), got)
	}
	for i := range paths {
		if got[i] != paths[i] {
			t.Errorf("file %d = %q, want %q", i, got[i], paths[i])
		}
	}
}

// Also overwrites the clipboard; see TestCopyFilesToClipboardWithHostileNames.
func TestCopyImageToClipboardRoundTrip(t *testing.T) {
	if os.Getenv("MAHPASTES_TEST_REAL_CLIPBOARD") != "1" {
		t.Skip("overwrites the clipboard; set MAHPASTES_TEST_REAL_CLIPBOARD=1 to run")
	}
	img := image.NewRGBA(image.Rect(0, 0, 7, 5))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := copyImageToClipboard(buf.Bytes()); err != nil {
		t.Fatalf("copyImageToClipboard: %v", err)
	}
	out, err := hiddenCommand("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Add-Type -AssemblyName System.Windows.Forms; $i = [System.Windows.Forms.Clipboard]::GetImage(); if ($i) { '' + $i.Width + 'x' + $i.Height } else { 'none' }").Output()
	if err != nil {
		t.Fatalf("read clipboard: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "7x5" {
		t.Fatalf("clipboard image = %q, want 7x5", got)
	}
}
