//go:build linux

package main

import (
	"fmt"
	"os"
	"strings"

	"go-clipboard/internal/cliputil"

	"golang.design/x/clipboard"
)

// copyFilesToClipboard publishes file references the way Linux file managers
// expect. The clipboard library serves one MIME target per write, so a single
// flavour is chosen from the desktop environment.
func copyFilesToClipboard(paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no files to copy")
	}
	mime, payload := clipboardFilePayload(paths, os.Getenv("XDG_CURRENT_DESKTOP"))
	return cliputil.Write(clipboard.Register(mime), payload)
}

// gnomeCopiedFilesDesktops lists the XDG_CURRENT_DESKTOP entries whose default
// file manager pastes only x-special/gnome-copied-files: Nemo (Cinnamon, and
// Budgie on most distributions), Caja (MATE), Thunar (Xfce) and elementary
// Files (Pantheon).
var gnomeCopiedFilesDesktops = map[string]bool{
	"X-CINNAMON": true,
	"CINNAMON":   true,
	"MATE":       true,
	"XFCE":       true,
	"BUDGIE":     true,
	"PANTHEON":   true,
}

// clipboardFilePayload returns the MIME target and body for copying paths.
// text/uri-list (RFC 2483, CRLF separated) is what Dolphin and other Qt apps,
// Nautilus 43+, PCManFM and browsers read when pasting files. The desktops in
// gnomeCopiedFilesDesktops get x-special/gnome-copied-files instead ("copy\n"
// followed by LF separated URIs), which their file managers require.
// desktop is the colon-separated XDG_CURRENT_DESKTOP value.
func clipboardFilePayload(paths []string, desktop string) (mime string, payload []byte) {
	uris := make([]string, len(paths))
	for i, p := range paths {
		uris[i] = fileURLFromAbsPath(p)
	}
	for _, name := range strings.Split(desktop, ":") {
		if gnomeCopiedFilesDesktops[strings.ToUpper(strings.TrimSpace(name))] {
			return "x-special/gnome-copied-files", []byte("copy\n" + strings.Join(uris, "\n"))
		}
	}
	return "text/uri-list", []byte(strings.Join(uris, "\r\n") + "\r\n")
}

// copyImageToClipboard publishes PNG data through the Wayland or X11 backend.
func copyImageToClipboard(pngData []byte) error {
	return cliputil.Write(clipboard.FmtImage, pngData)
}
