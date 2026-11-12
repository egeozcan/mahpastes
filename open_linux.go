//go:build linux

package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go-clipboard/internal/wailsbridge"
)

// launchGrace is how long a launcher gets to fail before it is taken to have
// handed the file off. xdg-open in its generic mode, and the application a
// launcher spawns, can run until the user closes the file, so a launcher is
// never waited on past this; a quick non-zero exit (no handler, gio too old
// for "launch", unknown desktop id) moves on to the next command instead.
var launchGrace = 2 * time.Second

// startLauncher starts argv and reports a failure only when the command
// cannot start or exits non-zero within launchGrace. A command still running
// after that is reaped in the background.
func startLauncher(argv []string) error {
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	// Stdout/Stderr stay nil (/dev/null): an output pipe would be inherited
	// by the launched application and keep Wait from returning until it exits.
	cmd := exec.Command(bin, argv[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(launchGrace)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("%s: %w", argv[0], err)
		}
		return nil
	case <-timer.C:
		return nil
	}
}

// startFirstAvailable runs the commands in order until one launches.
func startFirstAvailable(cmds [][]string) error {
	var errs []string
	for _, argv := range cmds {
		err := startLauncher(argv)
		if err == nil {
			return nil
		}
		errs = append(errs, err.Error())
	}
	if len(errs) == 0 {
		return fmt.Errorf("no launcher available")
	}
	return fmt.Errorf("%s", strings.Join(errs, "; "))
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
// appPath, which is either a .desktop file or an executable. gio launch takes
// the .desktop path, so Flatpak and Snap exports work wherever they live;
// gtk-launch only takes a desktop id and is the fallback for GLib < 2.72.
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

// applicationDirs are the places .desktop files live: system and per-user
// Flatpak exports, Snap, the system directory and per-user entries.
func applicationDirs(home string) []string {
	dirs := []string{
		"/usr/share/applications",
		"/var/lib/flatpak/exports/share/applications",
		"/var/lib/snapd/desktop/applications",
	}
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".local/share/flatpak/exports/share/applications"),
			filepath.Join(home, ".local/share/applications"))
	}
	return dirs
}

// defaultApplicationDir picks the directory holding the most .desktop files,
// so the picker opens where most apps are: /usr/share/applications on a
// classic distro, the Flatpak exports on an atomic one. Ties keep the
// earlier entry.
func defaultApplicationDir(dirs []string) string {
	best, bestCount := "/usr/share/applications", -1
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		n := 0
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".desktop") {
				n++
			}
		}
		if n > bestCount {
			best, bestCount = d, n
		}
	}
	return best
}

func chooseApplicationDialog(b *wailsbridge.Bridge) (string, error) {
	home, _ := os.UserHomeDir()
	path, err := b.OpenFile(wailsbridge.FileDialogOptions{
		Title:            "Choose Application",
		DefaultDirectory: defaultApplicationDir(applicationDirs(home)),
		Filters: []wailsbridge.FileFilter{
			{DisplayName: "Applications (*.desktop)", Pattern: "*.desktop"},
			// A plain executable is accepted too; openFileWithApp runs it
			// with the file as its argument.
			{DisplayName: "All Files", Pattern: "*"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file dialog: %w", err)
	}
	return path, nil
}

// fileURI is path as a file:// URI. Commas are escaped as well: dbus-send
// splits array:string: arguments on them and has no escape of its own.
func fileURI(path string) string {
	u := (&url.URL{Scheme: "file", Path: path}).String()
	return strings.ReplaceAll(u, ",", "%2C")
}

// revealDBusCommand asks the session's file manager to show path selected,
// through the org.freedesktop.FileManager1 interface Dolphin, Nautilus,
// Nemo, Caja and Thunar implement (D-Bus activation starts one if needed).
func revealDBusCommand(path string) []string {
	return []string{"dbus-send", "--session", "--print-reply", "--reply-timeout=10000",
		"--dest=org.freedesktop.FileManager1",
		"/org/freedesktop/FileManager1", "org.freedesktop.FileManager1.ShowItems",
		"array:string:" + fileURI(path), "string:"}
}

// revealFallbackCommands open the containing directory without selecting the
// file, for sessions without a FileManager1 service.
func revealFallbackCommands(path string) [][]string {
	dir := filepath.Dir(path)
	return [][]string{
		{"xdg-open", dir},
		{"gio", "open", dir},
	}
}

func revealInFileManager(path string) error {
	// dbus-send returns as soon as the file manager answers, and its exit
	// status says whether one did, so it is waited on before falling back.
	argv := revealDBusCommand(path)
	if bin, err := exec.LookPath(argv[0]); err == nil {
		if exec.Command(bin, argv[1:]...).Run() == nil {
			return nil
		}
	}
	if err := startFirstAvailable(revealFallbackCommands(path)); err != nil {
		return fmt.Errorf("failed to reveal file: %w", err)
	}
	return nil
}
