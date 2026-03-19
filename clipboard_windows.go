//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// copyFilesToClipboard places file references on the Windows clipboard via PowerShell.
func copyFilesToClipboard(paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no paths provided")
	}
	var items []string
	for _, p := range paths {
		escaped := strings.ReplaceAll(p, "'", "''")
		items = append(items, fmt.Sprintf("'%s'", escaped))
	}
	script := fmt.Sprintf(
		`$list = New-Object System.Collections.Specialized.StringCollection; @(%s) | ForEach-Object { $list.Add($_) }; [System.Windows.Forms.Clipboard]::SetFileDropList($list)`,
		strings.Join(items, ","),
	)
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		"Add-Type -AssemblyName System.Windows.Forms; "+script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("powershell failed: %w: %s", err, string(out))
	}
	return nil
}

// copyImageToClipboard places image data on the Windows clipboard via PowerShell.
// Uses System.Drawing to load the image and System.Windows.Forms.Clipboard.SetImage
// instead of golang.design/x/clipboard which has broken CF_DIBV5 conversion.
func copyImageToClipboard(pngData []byte) error {
	if len(pngData) == 0 {
		return fmt.Errorf("no image data provided")
	}

	tmpFile, err := os.CreateTemp("", "mahpastes-clip-*.png")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(pngData); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	escaped := strings.ReplaceAll(tmpPath, "'", "''")
	script := fmt.Sprintf(
		`$img = [System.Drawing.Image]::FromFile('%s'); [System.Windows.Forms.Clipboard]::SetImage($img); $img.Dispose()`,
		escaped,
	)
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		"Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; "+script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("powershell failed: %w: %s", err, string(out))
	}
	return nil
}
