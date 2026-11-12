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

// clipboardFilePayload returns the MIME target and body for copying paths.
// text/uri-list (RFC 2483, CRLF separated) is understood by Dolphin, Nautilus
// 43+, Thunar and most GTK/Qt apps. Nemo and Caja (Cinnamon, MATE) only read
// x-special/gnome-copied-files ("copy\n" followed by LF separated URIs).
func clipboardFilePayload(paths []string, desktop string) (mime string, payload []byte) {
	uris := make([]string, len(paths))
	for i, p := range paths {
		uris[i] = fileURLFromAbsPath(p)
	}
	d := strings.ToUpper(desktop)
	if strings.Contains(d, "CINNAMON") || strings.Contains(d, "MATE") {
		return "x-special/gnome-copied-files", []byte("copy\n" + strings.Join(uris, "\n"))
	}
	return "text/uri-list", []byte(strings.Join(uris, "\r\n") + "\r\n")
}

// copyImageToClipboard publishes PNG data through the Wayland or X11 backend.
func copyImageToClipboard(pngData []byte) error {
	return cliputil.Write(clipboard.FmtImage, pngData)
}
