package autostart

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeExe creates an executable-looking file and returns its path. Tests run
// from the temp directory, which checkRegistrable refuses, so round trips go
// through the backends directly.
func fakeExe(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDesktopExecQuotingRoundTrips(t *testing.T) {
	for _, exe := range []string{
		"/usr/bin/mahpastes",
		"/home/u/My Apps/mahpastes",
		`/home/u/we"ird/$HOME/back\slash/100%/tick` + "`" + `/mahpastes`,
		"/home/u/ünïcode/mahpastes",
	} {
		for _, hidden := range []bool{false, true} {
			content, err := desktopEntry(Entry{Executable: exe, Hidden: hidden})
			if err != nil {
				t.Fatalf("desktopEntry(%q): %v", exe, err)
			}
			got, disabled := parseDesktopEntry(content)
			if disabled {
				t.Fatalf("a fresh entry must be enabled:\n%s", content)
			}
			if got.Executable != exe || got.Hidden != hidden {
				t.Fatalf("round trip of %q (hidden=%v) gave %+v\n%s", exe, hidden, got, content)
			}
		}
	}
}

func TestDesktopExecEscapingMatchesSpec(t *testing.T) {
	content, err := desktopEntry(Entry{Executable: `/a b/c"d\e%f`, Hidden: true})
	if err != nil {
		t.Fatal(err)
	}
	// Quoting: \" and \\ inside quotes, %% for a literal percent; then the
	// value escaping doubles every backslash.
	want := `Exec="/a b/c\\"d\\\\e%%f" --hidden`
	if !strings.Contains(content, want+"\n") {
		t.Fatalf("Exec line not escaped as the spec asks; want %s in\n%s", want, content)
	}
	for _, line := range []string{"[Desktop Entry]", "Type=Application", "X-GNOME-Autostart-enabled=true"} {
		if !strings.Contains(content, line+"\n") {
			t.Errorf("entry lacks %q", line)
		}
	}
	// Startup-apps settings (GNOME's) hide NoDisplay entries, and the user
	// must be able to switch this one off there.
	if strings.Contains(content, "NoDisplay") {
		t.Errorf("entry must not set NoDisplay:\n%s", content)
	}
}

func TestDesktopExecRefusesControlCharacters(t *testing.T) {
	if _, err := desktopEntry(Entry{Executable: "/a\nb"}); err == nil {
		t.Fatal("a newline in the path must be refused")
	}
}

func TestParseDesktopEntryDisabledByDesktop(t *testing.T) {
	base := "[Desktop Entry]\nType=Application\nExec=\"/usr/bin/mahpastes\" --hidden\n"
	for _, extra := range []string{"Hidden=true\n", "X-GNOME-Autostart-enabled=false\n"} {
		e, disabled := parseDesktopEntry(base + extra)
		if !disabled {
			t.Errorf("%q must count as switched off", strings.TrimSpace(extra))
		}
		if e.Executable != "/usr/bin/mahpastes" || !e.Hidden {
			t.Errorf("entry misread: %+v", e)
		}
	}
	// Keys in other groups do not count.
	e, disabled := parseDesktopEntry(base + "[Desktop Action x]\nHidden=true\nExec=/other\n")
	if disabled || e.Executable != "/usr/bin/mahpastes" {
		t.Errorf("another group leaked into the entry: %+v disabled=%v", e, disabled)
	}
	// Unquoted Exec as a user might write it.
	e, _ = parseDesktopEntry("[Desktop Entry]\nExec=/opt/mahpastes/mahpastes\n")
	if e.Executable != "/opt/mahpastes/mahpastes" || e.Hidden {
		t.Errorf("unquoted Exec misread: %+v", e)
	}
}

func TestXDGBackendStatusAndRemove(t *testing.T) {
	dir := t.TempDir()
	b := xdgBackend{configHome: filepath.Join(dir, "config")}
	current := fakeExe(t, dir, "bin/mahpastes")

	st, err := status(b, current)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Supported || st.Enabled || st.Location != filepath.Join(dir, "config", "autostart", "mahpastes.desktop") {
		t.Fatalf("empty status wrong: %+v", st)
	}

	if err := b.write(Entry{Executable: current, Hidden: true}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(b.path())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		t.Errorf("entry mode %v, want 0644", info.Mode().Perm())
	}
	st, err = status(b, current)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Enabled || !st.Hidden || st.Target != current || st.TargetMissing || st.OtherCopy {
		t.Fatalf("status after write: %+v", st)
	}

	// Another existing copy.
	other := fakeExe(t, dir, "installed/mahpastes")
	if st, _ := status(b, other); !st.OtherCopy || st.TargetMissing {
		t.Fatalf("pointing at another copy not reported: %+v", st)
	}

	// The target disappears (moved or uninstalled).
	if err := os.Remove(current); err != nil {
		t.Fatal(err)
	}
	if st, _ := status(b, other); !st.TargetMissing || !st.Enabled {
		t.Fatalf("missing target not reported: %+v", st)
	}

	if err := b.remove(); err != nil {
		t.Fatal(err)
	}
	if err := b.remove(); err != nil {
		t.Fatalf("removing twice must be fine: %v", err)
	}
	if st, _ := status(b, other); st.Enabled {
		t.Fatalf("still enabled after remove: %+v", st)
	}
	// No temp files left in the autostart directory.
	entries, _ := os.ReadDir(filepath.Dir(b.path()))
	if len(entries) != 0 {
		t.Fatalf("autostart dir not empty after remove: %v", entries)
	}
}

func TestXDGBackendReportsDesktopSwitchOff(t *testing.T) {
	dir := t.TempDir()
	b := xdgBackend{configHome: dir}
	current := fakeExe(t, dir, "bin/mahpastes")
	if err := b.write(Entry{Executable: current}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(b.path())
	if err := os.WriteFile(b.path(), append(data, []byte("Hidden=true\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := status(b, current)
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled || !st.DisabledBySystem {
		t.Fatalf("a desktop-disabled entry must read as off: %+v", st)
	}
	// Turning it on again rewrites a clean entry.
	if err := b.write(Entry{Executable: current}); err != nil {
		t.Fatal(err)
	}
	if st, _ := status(b, current); !st.Enabled || st.DisabledBySystem {
		t.Fatalf("rewrite did not re-enable: %+v", st)
	}
}

func TestXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	if got, _ := xdgConfigHome(); got != "/custom/config" {
		t.Errorf("absolute XDG_CONFIG_HOME ignored: %s", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "relative/config")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got, _ := xdgConfigHome(); got != filepath.Join(home, ".config") {
		t.Errorf("a relative XDG_CONFIG_HOME must be ignored, got %s", got)
	}
}

func TestLaunchAgentRoundTrip(t *testing.T) {
	for _, exe := range []string{
		"/Applications/mahpastes.app/Contents/MacOS/mahpastes",
		"/Users/u/Apps & <Tools>/mahpastes.app/Contents/MacOS/mahpastes",
	} {
		for _, hidden := range []bool{false, true} {
			data := launchAgentPlist(Entry{Executable: exe, Hidden: hidden})
			e, disabled, err := parseLaunchAgent(data)
			if err != nil {
				t.Fatalf("parse: %v\n%s", err, data)
			}
			if disabled || e.Executable != exe || e.Hidden != hidden {
				t.Fatalf("round trip of %q hidden=%v gave %+v disabled=%v\n%s", exe, hidden, e, disabled, data)
			}
		}
	}
}

func TestLaunchAgentPlistContents(t *testing.T) {
	data := string(launchAgentPlist(Entry{Executable: "/Applications/mahpastes.app/Contents/MacOS/mahpastes", Hidden: true}))
	for _, want := range []string{
		"<key>Label</key>\n\t<string>" + LaunchAgentLabel + "</string>",
		"<key>RunAtLoad</key>\n\t<true/>",
		"<key>AssociatedBundleIdentifiers</key>\n\t<array>\n\t\t<string>" + BundleID + "</string>",
		"<string>--hidden</string>",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("plist lacks %q:\n%s", want, data)
		}
	}
	if strings.Contains(data, "KeepAlive") {
		t.Error("the agent must not be KeepAlive: quitting the app must keep it quit")
	}
}

func TestParseLaunchAgentLegacyDisabledAndOrder(t *testing.T) {
	data := []byte(`<?xml version="1.0"?><plist version="1.0"><dict>
		<key>Disabled</key><true/>
		<key>Label</key><string>x</string>
		<key>Nested</key><dict><key>ProgramArguments</key><array><string>/wrong</string></array></dict>
		<key>ProgramArguments</key><array><string>/right</string><string>--hidden</string></array>
	</dict></plist>`)
	e, disabled, err := parseLaunchAgent(data)
	if err != nil {
		t.Fatal(err)
	}
	if !disabled || e.Executable != "/right" || !e.Hidden {
		t.Fatalf("got %+v disabled=%v", e, disabled)
	}
	if _, _, err := parseLaunchAgent([]byte(`<plist><dict><key>Label</key><string>x</string></dict></plist>`)); err == nil {
		t.Fatal("a plist without ProgramArguments must be an error")
	}
}

func TestLaunchAgentBackend(t *testing.T) {
	dir := t.TempDir()
	b := launchAgentBackend{home: dir}
	current := fakeExe(t, dir, "mahpastes.app/Contents/MacOS/mahpastes")
	if err := b.write(Entry{Executable: current, Hidden: true}); err != nil {
		t.Fatal(err)
	}
	if b.path() != filepath.Join(dir, "Library", "LaunchAgents", LaunchAgentLabel+".plist") {
		t.Fatalf("unexpected path %s", b.path())
	}
	st, err := status(b, current)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Enabled || !st.Hidden || st.Target != current {
		t.Fatalf("status: %+v", st)
	}
	// A damaged file still shows up, so it can be turned off.
	if err := os.WriteFile(b.path(), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if st, err := status(b, current); err != nil || !st.Enabled || !st.TargetMissing {
		t.Fatalf("damaged plist: %+v, %v", st, err)
	}
	if err := b.remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(b.path()); !os.IsNotExist(err) {
		t.Fatalf("plist not removed: %v", err)
	}
}

func TestRunKeyCommandRoundTrip(t *testing.T) {
	for _, e := range []Entry{
		{Executable: `C:\Program Files\mahpastes\mahpastes.exe`},
		{Executable: `C:\Users\u\AppData\Local\mahpastes\mahpastes.exe`, Hidden: true},
	} {
		cmd := runKeyCommand(e)
		if !strings.HasPrefix(cmd, `"`) {
			t.Errorf("executable must be quoted: %s", cmd)
		}
		if got := parseRunKeyCommand(cmd); got != e {
			t.Errorf("round trip of %+v via %q gave %+v", e, cmd, got)
		}
	}
	if got := parseRunKeyCommand(`C:\tools\mahpastes.exe --hidden`); got.Executable != `C:\tools\mahpastes.exe` || !got.Hidden {
		t.Errorf("unquoted value misread: %+v", got)
	}
}

func TestStartupApprovedDisabled(t *testing.T) {
	for _, tt := range []struct {
		data []byte
		want bool
	}{
		{nil, false},
		{[]byte{2, 0, 0, 0}, false},
		{[]byte{6, 0, 0, 0}, false},
		{[]byte{3, 0, 0, 0}, true},
		{[]byte{7, 0, 0, 0}, true},
	} {
		if got := startupApprovedDisabled(tt.data); got != tt.want {
			t.Errorf("startupApprovedDisabled(%v) = %v, want %v", tt.data, got, tt.want)
		}
	}
}

func TestCheckRegistrable(t *testing.T) {
	tmpExe := filepath.Join(os.TempDir(), "go-build123", "exe", "mahpastes")
	if err := checkRegistrable(tmpExe); err == nil {
		t.Error("a binary in the temp directory must be refused")
	}
	if err := checkRegistrable("relative/mahpastes"); err == nil {
		t.Error("a relative path must be refused")
	}
	if err := checkRegistrable("/private/var/folders/x/AppTranslocation/ABC/d/mahpastes.app/Contents/MacOS/mahpastes"); err == nil {
		t.Error("a translocated app must be refused")
	}
	if runtime.GOOS != "windows" {
		if err := checkRegistrable("/usr/local/bin/mahpastes"); err != nil {
			t.Errorf("an installed path must be accepted: %v", err)
		}
	}
}

func TestSetRefusesTempExecutable(t *testing.T) {
	tmp, err := os.MkdirTemp("", "autostart-exe")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	b := xdgBackend{configHome: t.TempDir()}
	if err := set(b, fakeExe(t, tmp, "mahpastes"), true); err == nil {
		t.Fatalf("registering an executable in %s must fail", os.TempDir())
	}
	if _, err := os.Stat(b.path()); !os.IsNotExist(err) {
		t.Fatal("a refused registration must not write an entry")
	}
}

func TestStatusWithoutBackend(t *testing.T) {
	st, err := status(nil, "/x")
	if err != nil || st.Supported || st.Enabled {
		t.Fatalf("no backend must be unsupported: %+v, %v", st, err)
	}
}

func TestCurrentExecutablePrefersAppImage(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("AppImage is Linux-only")
	}
	t.Setenv("APPIMAGE", "/home/u/Apps/mahpastes.AppImage")
	if got, _ := CurrentExecutable(); got != "/home/u/Apps/mahpastes.AppImage" {
		t.Fatalf("got %s", got)
	}
}
