//go:build windows

package main

import (
	"fmt"
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
