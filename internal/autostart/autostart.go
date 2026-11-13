// Package autostart registers the desktop app to start when the user logs
// in, using each platform's own per-user mechanism and nothing that needs
// administrator rights:
//
//   - Linux: an XDG autostart entry, ${XDG_CONFIG_HOME:-~/.config}/autostart/mahpastes.desktop.
//   - Windows: a value under HKCU\Software\Microsoft\Windows\CurrentVersion\Run.
//   - macOS: a per-user LaunchAgent, ~/Library/LaunchAgents/<label>.plist,
//     with RunAtLoad. It is not loaded when written (RunAtLoad would start a
//     second copy right away); launchd picks it up at the next login.
//
// The OS entry is the only record: whether launch at login is on, which
// executable it starts and whether it starts hidden are all read back from
// it, so a user who removes it by hand (or through the desktop's own startup
// settings) sees that reflected.
package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// HiddenFlag is the command-line flag the entry passes when the app should
// start without showing its window. It matches openrequest.StartHiddenFlag
// (a test keeps them in step; this package does not import the desktop one).
const HiddenFlag = "--hidden"

// BundleID is the macOS bundle identifier (build/darwin/Info.plist). The
// LaunchAgent names it so Login Items lists the entry under mahpastes.
const BundleID = "io.github.egeozcan.mahpastes"

// LaunchAgentLabel is the launchd label and the plist file's base name.
const LaunchAgentLabel = BundleID + ".login"

// ErrUnsupported is returned where no autostart mechanism is implemented.
var ErrUnsupported = errors.New("launch at login is not supported on this platform")

// Entry is what an autostart registration starts.
type Entry struct {
	// Executable is the absolute path of the program to start.
	Executable string
	// Hidden passes HiddenFlag.
	Hidden bool
}

// Status describes the current registration for the settings UI.
type Status struct {
	// Supported is false where this platform has no implementation; nothing
	// else is meaningful then.
	Supported bool `json:"supported"`
	// Enabled is true when an entry exists and the desktop has not switched
	// it off.
	Enabled bool `json:"enabled"`
	// Hidden is true when the entry starts the app without its window.
	Hidden bool `json:"hidden"`
	// Target is the executable the entry starts ("" when there is none).
	Target string `json:"target"`
	// Current is the executable of the running app.
	Current string `json:"current"`
	// TargetMissing is true when the entry names a file that no longer
	// exists (the app was moved or uninstalled): it will do nothing at login.
	TargetMissing bool `json:"target_missing"`
	// OtherCopy is true when the entry starts a different, existing copy of
	// the app (for instance the installed one while this is a dev build).
	OtherCopy bool `json:"other_copy"`
	// DisabledBySystem is true when an entry exists but the desktop's own
	// startup settings switched it off (Windows Startup apps, Hidden=true or
	// X-GNOME-Autostart-enabled=false in the Linux entry).
	DisabledBySystem bool `json:"disabled_by_system"`
	// Location is where the entry lives, for display.
	Location string `json:"location"`
}

// backend is one platform mechanism.
type backend interface {
	// read returns the stored entry. found is false when there is none.
	read() (entry Entry, found bool, disabledBySystem bool, err error)
	// write creates or replaces the entry, enabled.
	write(Entry) error
	// remove deletes the entry; a missing entry is not an error.
	remove() error
	// location describes where the entry lives.
	location() string
}

// platformBackend returns the mechanism for this OS, or nil.
func platformBackend() (backend, error) {
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd", "dragonfly":
		dir, err := xdgConfigHome()
		if err != nil {
			return nil, err
		}
		return xdgBackend{configHome: dir}, nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		return launchAgentBackend{home: home}, nil
	case "windows":
		if b := newRunKeyBackend(); b != nil {
			return b, nil
		}
	}
	return nil, nil
}

// xdgConfigHome is $XDG_CONFIG_HOME when it is an absolute path (the spec
// says to ignore a relative one), else ~/.config.
func xdgConfigHome() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" && filepath.IsAbs(dir) {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

// CurrentExecutable is the path an entry made now should start. Inside an
// AppImage that is the AppImage file, not the binary in its temporary mount.
func CurrentExecutable() (string, error) {
	if runtime.GOOS == "linux" {
		if p := os.Getenv("APPIMAGE"); p != "" && filepath.IsAbs(p) {
			return filepath.Clean(p), nil
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Clean(exe), nil
}

// checkRegistrable refuses executables that will not be there at the next
// login: `go run` and test binaries in the temp directory, anything else
// under a system temp directory, a macOS app started from a translocated
// (quarantined, not yet moved) location, and a macOS app run from a mounted
// disk image.
func checkRegistrable(exe string) error {
	if !filepath.IsAbs(exe) {
		return fmt.Errorf("executable path %q is not absolute", exe)
	}
	if strings.ContainsAny(exe, "\x00\r\n") {
		return fmt.Errorf("executable path %q contains control characters", exe)
	}
	if strings.Contains(filepath.ToSlash(exe), "/AppTranslocation/") {
		return errors.New("mahpastes is running from a temporary copy macOS made of a downloaded app; move it to Applications and open it from there first")
	}
	for _, tmp := range tempRoots() {
		if isWithin(exe, tmp) {
			return fmt.Errorf("this copy of mahpastes runs from a temporary location (%s); install it before turning on launch at login", exe)
		}
	}
	// /Volumes also holds external and secondary disks, where an app may
	// live for good, so only a read-only volume there (what an attached disk
	// image is) is refused.
	if runtime.GOOS == "darwin" && isWithin(exe, "/Volumes") && volumeReadOnly(exe) {
		return fmt.Errorf("this copy of mahpastes runs from a read-only volume, most likely a disk image (%s); copy it to Applications before turning on launch at login", exe)
	}
	return nil
}

// tempRoots lists the directories whose contents the system clears: the
// process's own temp directory and, outside Windows, the shared ones (macOS
// empties /tmp at boot; isWithin follows /tmp -> /private/tmp).
func tempRoots() []string {
	var roots []string
	if tmp := os.TempDir(); tmp != "" {
		roots = append(roots, tmp)
	}
	if runtime.GOOS != "windows" {
		roots = append(roots, "/tmp", "/var/tmp")
	}
	return roots
}

// volumeReadOnly reports whether the volume holding path is mounted
// read-only. A variable so tests can stand in for a mounted disk image.
var volumeReadOnly = platformVolumeReadOnly

// isWithin reports whether path is inside dir, after resolving symlinks in
// both (macOS's temp directory sits behind /var -> /private/var).
func isWithin(path, dir string) bool {
	rel, err := filepath.Rel(resolveExisting(dir), resolveExisting(path))
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveExisting resolves the symlinks in p. Where p does not exist it
// resolves the deepest ancestor that does and re-appends the rest, so the
// answer does not depend on the leaf being there.
func resolveExisting(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for cur := p; ; {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// Get reports the current registration.
func Get() (Status, error) {
	b, err := platformBackend()
	if err != nil {
		return Status{}, err
	}
	current, _ := CurrentExecutable()
	return status(b, current)
}

func status(b backend, current string) (Status, error) {
	st := Status{Current: current}
	if b == nil {
		return st, nil
	}
	st.Supported = true
	st.Location = b.location()
	entry, found, disabled, err := b.read()
	if err != nil {
		return st, err
	}
	if !found {
		return st, nil
	}
	st.Enabled = !disabled
	st.DisabledBySystem = disabled
	st.Hidden = entry.Hidden
	st.Target = entry.Executable
	if entry.Executable == "" {
		st.TargetMissing = true
		return st, nil
	}
	if _, err := os.Stat(entry.Executable); err != nil {
		st.TargetMissing = true
	} else if current != "" && !sameFile(entry.Executable, current) {
		st.OtherCopy = true
	}
	return st, nil
}

func sameFile(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// Set turns launch at login on for the running executable (hidden says
// whether it starts without its window), or off. Turning it on replaces any
// existing entry, including one the desktop switched off or one pointing at
// another copy.
func Set(enabled, hidden bool) error {
	b, err := platformBackend()
	if err != nil {
		return err
	}
	if b == nil {
		return ErrUnsupported
	}
	if !enabled {
		return b.remove()
	}
	exe, err := CurrentExecutable()
	if err != nil {
		return fmt.Errorf("find the app's executable: %w", err)
	}
	return set(b, exe, hidden)
}

func set(b backend, exe string, hidden bool) error {
	if err := checkRegistrable(exe); err != nil {
		return err
	}
	return b.write(Entry{Executable: exe, Hidden: hidden})
}
