package app

import (
	"fmt"
	"strconv"
	"strings"
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
