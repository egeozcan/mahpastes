//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"
)

// clipboardPathEnvPrefix names the environment variables that carry paths to
// the PowerShell clipboard scripts: <prefix>0, <prefix>1, ...
const clipboardPathEnvPrefix = "MAHPASTES_CLIPBOARD_PATH_"

// The scripts are constants. Paths reach them through the environment and are
// never written into script text: a path holds the clip's own name, which can
// come from a followed share, a REST client, a plugin or a watch folder, and
// PowerShell reads not only ' but also the typographic quotes U+2018..U+201B
// as string delimiters, so no quoting of a name inside a script is safe.
const (
	fileDropScript = `Add-Type -AssemblyName System.Windows.Forms; ` +
		`$list = New-Object System.Collections.Specialized.StringCollection; ` +
		`for ($i = 0; ; $i++) { $p = [Environment]::GetEnvironmentVariable('` + clipboardPathEnvPrefix + `' + $i); ` +
		`if ([string]::IsNullOrEmpty($p)) { break }; [void]$list.Add($p) }; ` +
		`[System.Windows.Forms.Clipboard]::SetFileDropList($list)`
	imageCopyScript = `Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; ` +
		`$img = [System.Drawing.Image]::FromFile([Environment]::GetEnvironmentVariable('` + clipboardPathEnvPrefix + `0')); ` +
		`[System.Windows.Forms.Clipboard]::SetImage($img); $img.Dispose()`
)

// clipboardScriptEnv is the environment for a clipboard script: this
// process's, without any stale path variables, plus one variable per path.
func clipboardScriptEnv(paths []string) []string {
	env := make([]string, 0, len(paths)+64)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(kv), clipboardPathEnvPrefix) {
			env = append(env, kv)
		}
	}
	for i, p := range paths {
		env = append(env, fmt.Sprintf("%s%d=%s", clipboardPathEnvPrefix, i, p))
	}
	return env
}

// runClipboardScript runs one of the constant scripts with paths in its
// environment.
func runClipboardScript(script string, paths []string) error {
	for _, p := range paths {
		// An environment value ends at a NUL; no Windows path contains one.
		if p == "" || strings.ContainsRune(p, 0) {
			return fmt.Errorf("invalid path %q", p)
		}
	}
	cmd := hiddenCommand("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = clipboardScriptEnv(paths)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("powershell failed: %w: %s", err, string(out))
	}
	return nil
}

// copyFilesToClipboard places file references on the Windows clipboard via PowerShell.
func copyFilesToClipboard(paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no paths provided")
	}
	return runClipboardScript(fileDropScript, paths)
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

	return runClipboardScript(imageCopyScript, []string{tmpPath})
}
