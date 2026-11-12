package app

import "strconv"

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

// ParseAutostart interprets the stored settings, returning the port to listen
// on and whether autostart is enabled. An invalid port falls back to default.
func ParseAutostart(enabled, port string) (int, bool) {
	if enabled != "true" {
		return 0, false
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		p = defaultAPIPort
	}
	return p, true
}
