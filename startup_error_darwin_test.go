//go:build darwin

package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"go-clipboard/internal/logfile"
	"go-clipboard/internal/openrequest"
)

func TestStartupAlertWanted(t *testing.T) {
	env := func(kv ...string) func(string) string {
		return func(k string) string {
			for i := 0; i+1 < len(kv); i += 2 {
				if kv[i] == k {
					return kv[i+1]
				}
			}
			return ""
		}
	}
	cases := []struct {
		name      string
		getenv    func(string) string
		args      []string
		discarded bool
		want      bool
	}{
		{"finder launch", env(), nil, true, true},
		{"finder launch with a file", env(), []string{"/tmp/a.png"}, true, true},
		{"stderr read by a terminal, pipe or file", env(), nil, false, false},
		{"e2e harness", env("MAHPASTES_START_HIDDEN", "1"), nil, true, false},
		{"worker self-test", env(selfTestWorkerEnv, "1"), nil, true, false},
		{"login item", env(), []string{"--hidden"}, true, false},
		{"link launch carrying --hidden", env(), []string{"--hidden", "mahpastes://clip/1"}, true, true},
	}
	for _, tc := range cases {
		if got := startupAlertWanted(tc.getenv, tc.args, tc.discarded); got != tc.want {
			t.Errorf("%s: startupAlertWanted = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// withStderr points os.Stderr at f for the test.
func withStderr(t *testing.T, f *os.File) {
	t.Helper()
	old := os.Stderr
	os.Stderr = f
	t.Cleanup(func() { os.Stderr = old })
}

func recordStartupAlerts(t *testing.T) *[]string {
	t.Helper()
	var shown []string
	old := presentStartupAlert
	presentStartupAlert = func(m string) { shown = append(shown, m) }
	t.Cleanup(func() { presentStartupAlert = old })
	t.Setenv("MAHPASTES_START_HIDDEN", "")
	t.Setenv(selfTestWorkerEnv, "")
	oldArgs, oldStartup := os.Args, startupArgs
	os.Args = []string{"mahpastes"}
	startupArgs = nil
	t.Cleanup(func() { os.Args, startupArgs = oldArgs, oldStartup })
	return &shown
}

func TestShowStartupErrorAlertsWhenStderrIsDiscarded(t *testing.T) {
	shown := recordStartupAlerts(t)
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	withStderr(t, null)

	showStartupError("mahpastes cannot use its data folder: not a directory")
	if len(*shown) != 1 || !strings.Contains((*shown)[0], "not a directory") {
		t.Fatalf("alerts = %q, want the message once", *shown)
	}

	t.Setenv("MAHPASTES_START_HIDDEN", "1")
	showStartupError("again")
	if len(*shown) != 1 {
		t.Fatalf("a headless run raised an alert: %q", *shown)
	}
}

// main strips --hidden from os.Args before the instance-lock failure is
// reported; a login item must stay silent there too.
func TestShowStartupErrorSilentForLoginItemAfterArgsAreStripped(t *testing.T) {
	shown := recordStartupAlerts(t)
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	withStderr(t, null)

	os.Args = []string{"mahpastes", openrequest.StartHiddenFlag}
	startupArgs = os.Args[1:]
	os.Args = openrequest.StripOwnFlags(os.Args)

	showStartupError("another mahpastes instance is already running")
	if len(*shown) != 0 {
		t.Fatalf("a login-item launch raised an alert: %q", *shown)
	}
}

func TestShowStartupErrorSilentWhenStderrIsRead(t *testing.T) {
	shown := recordStartupAlerts(t)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	withStderr(t, w)

	showStartupError("boom")
	if len(*shown) != 0 {
		t.Fatalf("a launch with a piped stderr raised an alert: %q", *shown)
	}
}

func TestStartupAlertCommandKeepsMessageOutOfScript(t *testing.T) {
	msg := `-e "; do shell script "id" --` + "\n/Users/x/data"
	cmd := startupAlertCommand(context.Background(), msg)
	want := []string{"/usr/bin/osascript", "-", msg}
	if len(cmd.Args) != len(want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("args = %q, want %q", cmd.Args, want)
		}
	}
	script, err := io.ReadAll(cmd.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(script), "do shell script") || !strings.Contains(string(script), "giving up after") {
		t.Fatalf("script = %q: message spliced in, or no timeout", script)
	}
}

func TestStartupFailureMessageNamesLogFolder(t *testing.T) {
	if got := startupFailureMessage("locked", false, "/data"); got != "locked" {
		t.Fatalf("without a log: %q", got)
	}
	got := startupFailureMessage("locked", true, "/data")
	if !strings.HasPrefix(got, "locked") || !strings.Contains(got, logfile.Dir("/data")) {
		t.Fatalf("with a log: %q", got)
	}
}
