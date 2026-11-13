package main

import (
	"context"
	"log"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"go-clipboard/internal/autostart"
	"go-clipboard/internal/desktopprefs"
	"go-clipboard/internal/wailsbridge"
)

// signalQuitGrace is how long a SIGTERM/SIGINT-initiated shutdown may take
// before the process exits anyway. scripts/linux/install.sh and the macOS
// Makefile wait five seconds and then SIGKILL; this only matters when the
// graceful path itself is stuck.
const signalQuitGrace = 10 * time.Second

// desktopLifecycle is how the desktop app ends and starts: the unsaved-work
// prompt on quit, quitting on SIGTERM/SIGINT, keep running when the window is
// closed, and the hidden start a login launch asks for.
type desktopLifecycle struct {
	// dataDir holds the pre-Run prefs file (desktopprefs).
	dataDir string
	// keepRunningActive is the HideWindowOnClose this process was started
	// with. Wails reads it once, so a changed setting applies on next launch.
	keepRunningActive bool
	// saveGeometryOnHide is keepRunningActive for a window that is ever
	// shown: WindowHidden then saves the geometry (window_geometry.go).
	saveGeometryOnHide bool
	// hideAfterLoad hides the app once the page has loaded (macOS hidden start).
	hideAfterLoad bool
	hideOnce      sync.Once

	// unsaved is the editor's dirty state, reported by the frontend.
	unsaved atomic.Bool
	// forceQuit skips the unsaved-work prompt: set for SIGTERM/SIGINT, where
	// no one may be at the screen to answer it.
	forceQuit atomic.Bool
	// prompting is set while the unsaved-work question is up, so a second
	// quit request does not stack another dialog on top.
	prompting atomic.Bool
}

// DesktopLifecycle is the settings view of the app's lifetime options.
type DesktopLifecycle struct {
	// Platform is runtime.GOOS, for platform-specific wording.
	Platform string `json:"platform"`
	// KeepRunningWhenClosed is the stored choice (or its platform default).
	KeepRunningWhenClosed bool `json:"keep_running_when_closed"`
	// KeepRunningActive is what this running window does when closed. It
	// differs from KeepRunningWhenClosed until the app is restarted.
	KeepRunningActive bool `json:"keep_running_active"`
	// LaunchAtLogin is the OS registration.
	LaunchAtLogin autostart.Status `json:"launch_at_login"`
	// LaunchAtLoginError explains why the registration could not be read.
	LaunchAtLoginError string `json:"launch_at_login_error"`
}

// GetDesktopLifecycle reports the keep-running and launch-at-login settings.
func (d *App) GetDesktopLifecycle() (DesktopLifecycle, error) {
	lc := d.lifecycle
	out := DesktopLifecycle{Platform: runtime.GOOS, KeepRunningActive: lc.keepRunningActive}
	prefs, err := desktopprefs.Load(lc.dataDir)
	if err != nil {
		log.Printf("desktop prefs: %v", err)
	}
	out.KeepRunningWhenClosed = prefs.KeepRunning(runtime.GOOS)
	st, err := autostart.Get()
	if err != nil {
		out.LaunchAtLoginError = err.Error()
	}
	out.LaunchAtLogin = st
	return out, nil
}

// SetKeepRunningWhenClosed stores whether closing the window keeps the app
// running. It takes effect the next time the app starts.
func (d *App) SetKeepRunningWhenClosed(keep bool) error {
	return desktopprefs.SetKeepRunningWhenClosed(d.lifecycle.dataDir, keep)
}

// SetLaunchAtLogin registers this copy of the app to start at login (hidden:
// without showing its window), or removes the registration, and returns the
// resulting status.
func (d *App) SetLaunchAtLogin(enabled, hidden bool) (autostart.Status, error) {
	if err := autostart.Set(enabled, hidden); err != nil {
		return autostart.Status{}, err
	}
	return autostart.Get()
}

// SetUnsavedChanges is how the frontend reports that the editor holds
// changes that are not saved yet, so quitting can ask first.
func (d *App) SetUnsavedChanges(unsaved bool) {
	d.lifecycle.unsaved.Store(unsaved)
}

// QuitApp quits the app, as the Quit menu entry and Ctrl+Q do. Unsaved
// editor changes are asked about first (beforeClose).
func (d *App) QuitApp() {
	go d.bridge.Quit()
}

// beforeClose is Wails' OnBeforeClose. Wails consults it for the window's
// close button (unless the window hides on close), for Cmd+Q and the Dock's
// Quit on macOS, and for every runtime Quit; returning true cancels the quit.
//
// The app must stay quittable: only an answer to the question cancels
// ("Keep Editing", or dismissing the dialog, which keeps the unsaved work).
// A dialog that cannot be shown lets the quit go ahead, as does a
// signal-initiated quit, and a second quit request while the question is up
// is dropped rather than stacking another dialog.
//
// A signal can also arrive while the question is up. Its Quit ends only the
// dialog's modal loop on macOS (NSApp stop + abortModal), which reads as a
// dismissal, so forceQuit is checked again after the dialog: the caller that
// asked then quits with no modal in the way.
func (d *App) beforeClose(ctx context.Context) (prevent bool) {
	lc := d.lifecycle
	if lc.forceQuit.Load() || !lc.unsaved.Load() {
		return false
	}
	if !lc.prompting.CompareAndSwap(false, true) {
		// Dropped, unless a signal set forceQuit after the question went up.
		return !lc.forceQuit.Load()
	}
	defer lc.prompting.Store(false)

	// The question must be visible: on macOS Cmd+Q also reaches a hidden app.
	d.bridge.ShowWindow()
	discard, err := d.bridge.ConfirmDialog(wailsbridge.ConfirmOptions{
		Title:        "Unsaved changes",
		Message:      "The editor has changes that are not saved. Quit anyway and discard them?",
		ConfirmLabel: "Quit",
		CancelLabel:  "Keep Editing",
	})
	if lc.forceQuit.Load() {
		// A signal arrived meanwhile: whatever the dialog returned is its
		// being torn down, not an answer.
		return false
	}
	if err != nil {
		log.Printf("quit: could not ask about unsaved changes, quitting anyway: %v", err)
		return false
	}
	if discard {
		return false
	}
	return true
}

// domReady is Wails' OnDomReady. A freshly loaded page has no unsaved
// changes, whatever the previous page reported. A macOS hidden start hides
// the app here, once (see Bridge.HideApp for why not StartHidden).
func (d *App) domReady(ctx context.Context) {
	lc := d.lifecycle
	lc.unsaved.Store(false)
	if lc.hideAfterLoad {
		lc.hideOnce.Do(func() { d.hideUnlessWindowAsked(d.bridge.HideApp) })
	}
}

// quitOnSignal ends the app gracefully on the first signal from sigs: quit
// runs (it must bypass the unsaved-work prompt), and OnShutdown tears the app
// down as for any other quit. If the process is still alive after grace, or a
// second signal arrives, exit ends it: a stuck shutdown must not leave the
// app impossible to stop.
func quitOnSignal(sigs <-chan os.Signal, quit func(), exit func(int), grace time.Duration) {
	sig, ok := <-sigs
	if !ok {
		return
	}
	log.Printf("received %v; quitting", sig)
	// The deadline runs from the signal and quit runs on its own goroutine:
	// a quit that blocks must not keep the deadline from firing.
	timer := time.NewTimer(grace)
	defer timer.Stop()
	go quit()
	select {
	case sig = <-sigs:
		log.Printf("received %v again; exiting without finishing shutdown", sig)
	case <-timer.C:
		log.Printf("shutdown did not finish within %v; exiting", grace)
	}
	exit(1)
}

// logLoginEntryProblems checks the launch at login entry once at startup: one
// that starts a missing or different copy of the app is reported in the log
// (and in Settings), never rewritten behind the user's back.
func logLoginEntryProblems() {
	st, err := autostart.Get()
	if err != nil {
		log.Printf("launch at login: could not read the entry: %v", err)
		return
	}
	switch {
	case !st.Enabled:
	case st.TargetMissing:
		log.Printf("launch at login: the entry at %s starts %q, which does not exist; turn the setting off and on again", st.Location, st.Target)
	case st.OtherCopy:
		log.Printf("launch at login: the entry at %s starts another copy of mahpastes (%s), not this one (%s)", st.Location, st.Target, st.Current)
	}
}

// signalQuit is the quit quitOnSignal runs: no prompt, then a runtime Quit.
func (d *App) signalQuit() {
	d.lifecycle.forceQuit.Store(true)
	d.bridge.Quit()
}
