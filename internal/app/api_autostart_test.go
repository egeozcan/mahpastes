package app

import "testing"

func TestParseAutostart(t *testing.T) {
	cases := []struct {
		en, port string
		p        int
		ok       bool
	}{
		{"", "1234", 0, false},
		{"false", "1234", 0, false},
		{"true", "1234", 1234, true},
		{"true", "", 44557, true},
		{"true", "99999", 44557, true},
		{"true", "abc", 44557, true},
	}
	for _, c := range cases {
		p, ok := ParseAutostart(c.en, c.port)
		if p != c.p || ok != c.ok {
			t.Errorf("%+v: got %d,%v", c, p, ok)
		}
	}
	if !isAPILaunchSetting("api_port") || isAPILaunchSetting("theme") {
		t.Error("isAPILaunchSetting wrong")
	}
	if !isSensitiveSetting("api_autostart") || !isSensitiveSetting("api_port") {
		t.Error("launch settings must be excluded from backups")
	}
}
