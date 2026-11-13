package app

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
)

// newSettingsTestApp returns an App over a test DB that also has the settings
// table (newServerTestDB's schema does not include it).
func newSettingsTestApp(t *testing.T) *App {
	t.Helper()
	db := newServerTestDB(t)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("create settings table: %v", err)
	}
	return &App{db: db}
}

func TestParseAPIPort(t *testing.T) {
	cases := map[string]int{
		"1234":  1234,
		" 80 ":  80,
		"":      defaultAPIPort,
		"0":     defaultAPIPort,
		"99999": defaultAPIPort,
		"abc":   defaultAPIPort,
	}
	for in, want := range cases {
		if got := parseAPIPort(in); got != want {
			t.Errorf("parseAPIPort(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestAPILaunchSettingsRoundTrip(t *testing.T) {
	app := newSettingsTestApp(t)

	if enabled, port := app.APILaunchSettings(); enabled || port != defaultAPIPort {
		t.Fatalf("fresh install: got (%v, %d), want (false, %d)", enabled, port, defaultAPIPort)
	}

	if err := app.SetAPILaunchSettings(true, 45123); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if enabled, port := app.APILaunchSettings(); !enabled || port != 45123 {
		t.Fatalf("after enable: got (%v, %d), want (true, 45123)", enabled, port)
	}

	// Disabling keeps the remembered port and ignores the one passed in.
	if err := app.SetAPILaunchSettings(false, 0); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if enabled, port := app.APILaunchSettings(); enabled || port != 45123 {
		t.Fatalf("after disable: got (%v, %d), want (false, 45123)", enabled, port)
	}

	for _, bad := range []int{0, -1, 65536} {
		if err := app.SetAPILaunchSettings(true, bad); err == nil {
			t.Errorf("enable with port %d: want error", bad)
		}
	}
	if enabled, _ := app.APILaunchSettings(); enabled {
		t.Fatal("a rejected enable must not turn autostart on")
	}

	if err := app.RememberAPIPort(46000); err != nil {
		t.Fatalf("RememberAPIPort: %v", err)
	}
	if _, port := app.APILaunchSettings(); port != 46000 {
		t.Fatalf("remembered port = %d, want 46000", port)
	}
}

func TestLaunchSettingsExcludedFromBackups(t *testing.T) {
	for _, key := range []string{SettingAPIAutostart, SettingAPIPort} {
		if !isAPILaunchSetting(key) {
			t.Errorf("isAPILaunchSetting(%q) = false", key)
		}
		if !isSensitiveSetting(key) {
			t.Errorf("%q must be excluded from backups", key)
		}
	}
	if isAPILaunchSetting("theme") {
		t.Error("isAPILaunchSetting(theme) = true")
	}
}

// freeLoopbackPort returns a port nothing is listening on right now.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func TestStartAPIOnLaunch(t *testing.T) {
	app := newSettingsTestApp(t)

	// No API manager (e.g. bindings generation): nothing to start.
	if started, err := app.StartAPIOnLaunch(); started || err != nil {
		t.Fatalf("nil manager: got (%v, %v)", started, err)
	}

	app.apiManager = NewAPIManager(app)
	t.Cleanup(func() { app.apiManager.Stop() })

	if started, err := app.StartAPIOnLaunch(); started || err != nil {
		t.Fatalf("autostart off: got (%v, %v), want nothing started", started, err)
	}
	if app.apiManager.GetStatus().Running {
		t.Fatal("autostart off must not start the server")
	}

	port := freeLoopbackPort(t)
	if err := app.SetAPILaunchSettings(true, port); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// A persisted bind-all must never be honoured; plant one to prove it.
	if err := app.SetSetting("api_bind_all", "true"); err != nil {
		t.Fatalf("seed api_bind_all: %v", err)
	}
	started, err := app.StartAPIOnLaunch()
	if !started || err != nil {
		t.Fatalf("autostart on: got (%v, %v)", started, err)
	}
	status := app.apiManager.GetStatus()
	if !status.Running || status.Port != port || status.BindAll {
		t.Fatalf("status = %+v, want running loopback-only on %d", status, port)
	}

	// A second start (e.g. the port already taken) reports an error rather
	// than panicking or binding elsewhere.
	if started, err := app.StartAPIOnLaunch(); started || err == nil {
		t.Fatalf("second start: got (%v, %v), want an error", started, err)
	}
}

// A launch start that fails must leave a reason the modal can show: it used to
// be logged only, so the modal read "Stopped" as if the user had stopped it.
func TestAPILaunchFailureRemembersWhyTheStartFailed(t *testing.T) {
	app := newSettingsTestApp(t)
	app.apiManager = NewAPIManager(app)
	t.Cleanup(func() { app.apiManager.Stop() })

	// Another program holds the port at launch.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	if err := app.SetAPILaunchSettings(true, port); err != nil {
		t.Fatalf("enable: %v", err)
	}

	var failure APILaunchFailure
	started, err := app.StartAPIOnLaunch()
	if started || err == nil {
		t.Fatalf("start on a taken port: got (%v, %v), want an error", started, err)
	}
	failure.Record(port, err)
	if want := fmt.Sprintf("port %d is already in use", port); failure.Message() != want {
		t.Fatalf("Message() = %q, want %q", failure.Message(), want)
	}
	if app.apiManager.GetStatus().Running {
		t.Fatal("a failed launch start must leave the server stopped")
	}
	enabled, savedPort := app.APILaunchSettings()
	if !enabled || savedPort != port {
		t.Fatalf("launch settings = (%v, %d), want them left as (true, %d)", enabled, savedPort, port)
	}

	failure.Clear()
	if failure.Message() != "" {
		t.Fatalf("after Clear: %q", failure.Message())
	}
	failure.Record(port, nil)
	if failure.Message() != "" {
		t.Fatalf("a nil error recorded %q", failure.Message())
	}
}

func TestAPILaunchFailureMessage(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("start: %w", syscall.EADDRINUSE), "port 80 is already in use"},
		{errors.New("listen tcp 127.0.0.1:80: bind: Only one usage of each socket address (protocol/network address/port) is normally permitted."), "port 80 is already in use"},
		{fmt.Errorf("start: %w", syscall.EACCES), "no permission to listen on port 80"},
		{errors.New("bind: An attempt was made to access a socket in a way forbidden by its access permissions."), "no permission to listen on port 80"},
		{errors.New("API server is already running"), "could not listen on port 80"},
	}
	for _, c := range cases {
		if got := apiLaunchFailureMessage(80, c.err); got != c.want {
			t.Errorf("apiLaunchFailureMessage(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestSetSettingRefusesAPILaunchKeys(t *testing.T) {
	app := newSettingsTestApp(t)
	manager := NewAPIManager(app)

	put := func(key, value string) int {
		req := httptest.NewRequest("PUT", "/api/v1/settings/"+key, strings.NewReader(`{"value":"`+value+`"}`))
		req.SetPathValue("key", key)
		rec := httptest.NewRecorder()
		manager.handleSetSetting(rec, withKey(req, &apiKeyContext{KeyID: 1, Role: "admin"}))
		return rec.Code
	}

	for _, key := range []string{SettingAPIAutostart, SettingAPIPort} {
		if code := put(key, "true"); code != http.StatusForbidden {
			t.Errorf("PUT %s: code = %d, want 403", key, code)
		}
		if v, _ := app.GetSetting(key); v != "" {
			t.Errorf("PUT %s stored %q despite the refusal", key, v)
		}
	}
	if code := put("theme", "dark"); code != http.StatusNoContent {
		t.Errorf("PUT theme: code = %d, want 204", code)
	}
}
