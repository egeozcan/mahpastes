//go:build linux

package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go-clipboard/internal/wailsbridge"
)

// startDetached starts cmd and reaps it in the background. xdg-open can block
// until the handler exits, so the caller must never wait on it.
func startDetached(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// defaultOpenCommands lists the commands to try, in order, to open path with
// the default handler.
func defaultOpenCommands(path string) [][]string {
	return [][]string{
		{"xdg-open", path},
		{"gio", "open", path},
	}
}

// openWithCommands lists the commands to try, in order, to open filePath with
// appPath, which is either a .desktop file or an executable.
func openWithCommands(filePath, appPath string) [][]string {
	if strings.HasSuffix(appPath, ".desktop") {
		id := strings.TrimSuffix(filepath.Base(appPath), ".desktop")
		return [][]string{
			{"gio", "launch", appPath, filePath},
			{"gtk-launch", id, filePath},
		}
	}
	return [][]string{{appPath, filePath}}
}

// startFirstAvailable starts the first command whose binary can be found.
func startFirstAvailable(cmds [][]string) error {
	var lastErr error
	for _, argv := range cmds {
		bin, err := exec.LookPath(argv[0])
		if err != nil {
			lastErr = err
			continue
		}
		if err := startDetached(exec.Command(bin, argv[1:]...)); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no launcher available")
	}
	return lastErr
}

func openFileWithDefaultApp(path string) error {
	if err := startFirstAvailable(defaultOpenCommands(path)); err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	return nil
}

func openFileWithApp(filePath, appPath string) error {
	if err := startFirstAvailable(openWithCommands(filePath, appPath)); err != nil {
		return fmt.Errorf("failed to open file with app: %w", err)
	}
	return nil
}

// applicationDirs are the places .desktop files live, in picker order:
// Flatpak exports come first because most GUI apps on atomic distros are
// Flatpaks.
func applicationDirs() []string {
	dirs := []string{"/var/lib/flatpak/exports/share/applications"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(home, ".local/share/flatpak/exports/share/applications"),
			filepath.Join(home, ".local/share/applications"))
	}
	return append(dirs, "/var/lib/snapd/desktop/applications", "/usr/share/applications")
}

func defaultApplicationDir() string {
	for _, d := range applicationDirs() {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d
		}
	}
	return "/usr/share/applications"
}

func chooseApplicationDialog(b *wailsbridge.Bridge) (string, error) {
	path, err := b.OpenFile(wailsbridge.FileDialogOptions{
		Title:            "Choose Application",
		DefaultDirectory: defaultApplicationDir(),
		Filters: []wailsbridge.FileFilter{
			{DisplayName: "Applications", Pattern: "*.desktop"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file dialog: %w", err)
	}
	return path, nil
}

// revealCommands lists the commands to try, in order, to show path in the
// file manager: the FileManager1 D-Bus interface selects the file, and
// opening the parent directory is the fallback.
func revealCommands(path string) [][]string {
	u := (&url.URL{Scheme: "file", Path: path}).String()
	return [][]string{
		{"dbus-send", "--session", "--print-reply", "--dest=org.freedesktop.FileManager1",
			"/org/freedesktop/FileManager1", "org.freedesktop.FileManager1.ShowItems",
			"array:string:" + u, "string:"},
		{"xdg-open", filepath.Dir(path)},
	}
}

func revealInFileManager(path string) error {
	cmds := revealCommands(path)
	// dbus-send is quick and reports whether a file manager answered, so it
	// is waited on; only then is the xdg-open fallback tried.
	if bin, err := exec.LookPath(cmds[0][0]); err == nil {
		if exec.Command(bin, cmds[0][1:]...).Run() == nil {
			return nil
		}
	}
	if err := startFirstAvailable(cmds[1:]); err != nil {
		return fmt.Errorf("failed to reveal file: %w", err)
	}
	return nil
}
