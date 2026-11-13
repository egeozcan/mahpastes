package app

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// Settings keys for the desktop "Start on launch" option. bind_all is never
// persisted: an autostarted listener is always loopback-only.
const (
	SettingAPIAutostart = "api_autostart"
	SettingAPIPort      = "api_port"
	defaultAPIPort      = 44557
)

// isAPILaunchSetting reports whether key controls API autostart; the REST
// settings endpoint refuses to write these so a key cannot arm a listener.
func isAPILaunchSetting(key string) bool {
	return key == SettingAPIAutostart || key == SettingAPIPort
}

// parseAPIPort interprets a stored port, falling back to the default (the
// port mp uses) when it is missing or out of range.
func parseAPIPort(s string) int {
	p, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || p < 1 || p > 65535 {
		return defaultAPIPort
	}
	return p
}

// APILaunchSettings returns the persisted "Start on launch" choice and the
// port an autostarted server listens on.
func (a *App) APILaunchSettings() (enabled bool, port int) {
	en, _ := a.GetSetting(SettingAPIAutostart)
	p, _ := a.GetSetting(SettingAPIPort)
	return en == "true", parseAPIPort(p)
}

// SetAPILaunchSettings persists the "Start on launch" choice. Enabling it also
// records port, so the next launch listens on the port the user saw.
func (a *App) SetAPILaunchSettings(enabled bool, port int) error {
	if enabled {
		if port < 1 || port > 65535 {
			return fmt.Errorf("invalid port %d: must be between 1 and 65535", port)
		}
		if err := a.RememberAPIPort(port); err != nil {
			return err
		}
	}
	return a.SetSetting(SettingAPIAutostart, strconv.FormatBool(enabled))
}

// RememberAPIPort records the port the server was last started on, which is
// the port "Start on launch" uses.
func (a *App) RememberAPIPort(port int) error {
	return a.SetSetting(SettingAPIPort, strconv.Itoa(port))
}

// StartAPIOnLaunch starts the REST API listener, loopback-only, when the user
// enabled "Start on launch", and reports whether it started one. The desktop
// app calls it from OnStartup. It is deliberately not part of the shared
// Bootstrap: mahpastesd starts its own listener, and a second Start fails
// with "already running".
func (a *App) StartAPIOnLaunch() (bool, error) {
	if a.apiManager == nil {
		return false, nil
	}
	enabled, port := a.APILaunchSettings()
	if !enabled {
		return false, nil
	}
	if _, err := a.apiManager.Start(port, false); err != nil {
		return false, fmt.Errorf("start API server on port %d: %w", port, err)
	}
	return true, nil
}

// APILaunchFailure remembers why "Start on launch" could not start the server,
// so the API settings modal can say so: without it a failed launch start looks
// exactly like a server the user stopped. It lives in memory only (never in
// settings or backups) and is cleared by the next manual start or stop.
type APILaunchFailure struct {
	mu  sync.Mutex
	msg string
}

// Record keeps a short description of err, the error StartAPIOnLaunch
// returned for port. A nil err records nothing.
func (f *APILaunchFailure) Record(port int, err error) {
	if err == nil {
		return
	}
	f.mu.Lock()
	f.msg = apiLaunchFailureMessage(port, err)
	f.mu.Unlock()
}

// Clear forgets the failure.
func (f *APILaunchFailure) Clear() {
	f.mu.Lock()
	f.msg = ""
	f.mu.Unlock()
}

// Message returns the recorded failure, or "" when there is none.
func (f *APILaunchFailure) Message() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.msg
}

// apiLaunchFailureMessage turns a listen error into a line short enough for
// the modal's status text; the full error is in the log. The text matches
// cover Windows, whose socket errors do not map onto syscall.EADDRINUSE.
func apiLaunchFailureMessage(port int, err error) string {
	text := err.Error()
	switch {
	case errors.Is(err, syscall.EADDRINUSE), strings.Contains(text, "Only one usage of each socket address"):
		return fmt.Sprintf("port %d is already in use", port)
	case errors.Is(err, os.ErrPermission), strings.Contains(text, "forbidden by its access permissions"):
		return fmt.Sprintf("no permission to listen on port %d", port)
	}
	return fmt.Sprintf("could not listen on port %d", port)
}
