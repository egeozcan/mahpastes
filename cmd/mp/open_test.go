package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestOpenFileName(t *testing.T) {
	for _, tt := range []struct {
		filename, contentType, want string
	}{
		{"photo.png", "image/png", "photo.png"},
		{"../../etc/passwd", "text/plain", "passwd"},
		{`C:\Users\x\report.pdf`, "application/pdf", "report.pdf"},
		{"dir/", "text/plain", "clip_7.txt"},
		{"..", "text/plain", "clip_7.txt"},
		{"", "text/plain; charset=utf-8", "clip_7.txt"},
		{"", "image/jpeg", "clip_7.jpg"},
		{"", "application/x-unknown-thing", "clip_7"},
		{"a:b|c?.txt", "text/plain", "a_b_c_.txt"},
		{"trailing. ", "text/plain", "trailing"},
		{"line\nbreak.txt", "text/plain", "line_break.txt"},
		// Windows device names, in any case and with any extension.
		{"NUL.txt", "text/plain", "_NUL.txt"},
		{"con", "text/plain", "_con"},
		{"Com1.tar.gz", "application/gzip", "_Com1.tar.gz"},
		{"console.txt", "text/plain", "console.txt"},
	} {
		if got := openFileName(7, tt.filename, tt.contentType); got != tt.want {
			t.Errorf("openFileName(%q, %q) = %q, want %q", tt.filename, tt.contentType, got, tt.want)
		}
	}
	long := strings.Repeat("x", 300) + ".png"
	if got := openFileName(7, long, "image/png"); len(got) != 200 || !strings.HasSuffix(got, ".png") {
		t.Errorf("long name shortened to %q (%d bytes), want 200 bytes ending in .png", got, len(got))
	}
	// A cut through a multi-byte character would leave a name macOS refuses.
	wide := "x" + strings.Repeat("ü", 150) + ".txt"
	if got := openFileName(7, wide, "text/plain"); len(got) > 200 || !utf8.ValidString(got) || !strings.HasSuffix(got, ".txt") {
		t.Errorf("multi-byte name shortened to %q (%d bytes), want valid UTF-8 within 200 bytes ending in .txt", got, len(got))
	}
}

func TestExecutableExtensionsAreRefused(t *testing.T) {
	for _, name := range []string{"run.py", "Setup.EXE", "help.chm", "go.webloc", "x.AppImage", "a.ps1", "p.msix"} {
		if !executableExtensions[strings.ToLower(filepath.Ext(name))] {
			t.Errorf("%s is not treated as executable", name)
		}
	}
	for _, name := range []string{"photo.png", "notes.txt", "report.pdf", "data.json"} {
		if executableExtensions[strings.ToLower(filepath.Ext(name))] {
			t.Errorf("%s is treated as executable", name)
		}
	}
}

func TestOpenerCommand(t *testing.T) {
	for _, tt := range []struct {
		goos, app string
		name      string
		args      []string
	}{
		{"darwin", "", "open", []string{"/f"}},
		{"darwin", "Preview", "open", []string{"-a", "Preview", "/f"}},
		{"windows", "", "rundll32", []string{"url.dll,FileProtocolHandler", "/f"}},
		{"windows", `C:\Tools\viewer.exe`, `C:\Tools\viewer.exe`, []string{"/f"}},
		{"linux", "", "xdg-open", []string{"/f"}},
		{"linux", "gimp", "gimp", []string{"/f"}},
	} {
		name, args := openerCommand(tt.goos, "/f", tt.app)
		if name != tt.name || !reflect.DeepEqual(args, tt.args) {
			t.Errorf("openerCommand(%s, %q) = %s %q, want %s %q", tt.goos, tt.app, name, args, tt.name, tt.args)
		}
	}
}

type openCall struct {
	name string
	args []string
}

// setupClipOpen points mp at a fake API serving one clip, keeps every cache
// folder inside the test's temp dir and records openers instead of running
// them.
func setupClipOpen(t *testing.T, filename string, data string) (*[]openCall, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/clips/42":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":42,"filename":` + quoteJSON(filename) + `,"content_type":"image/png"}`))
		case "/api/v1/clips/42/data":
			_, _ = w.Write([]byte(data))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MP_API_URL", srv.URL)
	t.Setenv("MP_API_KEY", "test-key")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("LocalAppData", filepath.Join(home, "cache"))
	cacheBase, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}

	var calls []openCall
	prev := startOpener
	startOpener = func(name string, args ...string) error {
		calls = append(calls, openCall{name, append([]string(nil), args...)})
		return nil
	}
	t.Cleanup(func() {
		startOpener = prev
		clipOpenApp = ""
		clipOpenInApp = false
	})
	return &calls, filepath.Join(cacheBase, "mahpastes", "mp-open")
}

func quoteJSON(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func TestClipOpenDownloadsToCacheAndOpens(t *testing.T) {
	calls, cacheDir := setupClipOpen(t, "../outside/photo.png", "PNGDATA")

	if err := runClipOpen(clipOpenCmd, []string{"42"}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("opener calls = %v, want one", *calls)
	}
	call := (*calls)[0]
	path := call.args[len(call.args)-1]
	if filepath.Base(path) != "photo.png" {
		t.Errorf("opened %s, want a file named photo.png", path)
	}
	if rel, err := filepath.Rel(cacheDir, path); err != nil || strings.HasPrefix(rel, "..") {
		t.Errorf("opened %s, outside the cache folder %s", path, cacheDir)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "PNGDATA" {
		t.Fatalf("copy = %q, %v; want the clip data", got, err)
	}
	if runtime.GOOS != "windows" {
		for _, p := range []string{path, filepath.Dir(path), cacheDir} {
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if perm := info.Mode().Perm(); perm&0o077 != 0 {
				t.Errorf("%s is readable by others (%v)", p, perm)
			}
		}
	}
	wantName, _ := openerCommand(runtime.GOOS, path, "")
	if call.name != wantName {
		t.Errorf("opener = %s, want %s", call.name, wantName)
	}

	// A second open of the same clip gets its own copy.
	if err := runClipOpen(clipOpenCmd, []string{"42"}); err != nil {
		t.Fatal(err)
	}
	second := (*calls)[1].args[len((*calls)[1].args)-1]
	if second == path {
		t.Errorf("second open reused %s", path)
	}
}

func TestClipOpenWithApp(t *testing.T) {
	calls, _ := setupClipOpen(t, "photo.png", "x")
	clipOpenApp = "viewer"
	if err := runClipOpen(clipOpenCmd, []string{"42"}); err != nil {
		t.Fatal(err)
	}
	call := (*calls)[0]
	wantName, wantArgs := openerCommand(runtime.GOOS, call.args[len(call.args)-1], "viewer")
	if call.name != wantName || !reflect.DeepEqual(call.args, wantArgs) {
		t.Errorf("opener = %s %q, want %s %q", call.name, call.args, wantName, wantArgs)
	}
}

func TestClipOpenRefusesExecutables(t *testing.T) {
	calls, cacheDir := setupClipOpen(t, "Setup.EXE", "MZ")
	err := runClipOpen(clipOpenCmd, []string{"42"})
	if err == nil || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("opener ran for an executable: %v", *calls)
	}
	if entries, _ := os.ReadDir(cacheDir); len(entries) != 0 {
		t.Fatalf("executable was written to the cache: %v", entries)
	}

	// Naming the application is an explicit choice and is allowed.
	clipOpenApp = "hexviewer"
	if err := runClipOpen(clipOpenCmd, []string{"42"}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("opener calls = %v, want one", *calls)
	}
}

func TestClipOpenRejectsBadIDs(t *testing.T) {
	calls, _ := setupClipOpen(t, "photo.png", "x")
	for _, id := range []string{"0", "-3", "abc"} {
		if err := runClipOpen(clipOpenCmd, []string{id}); err == nil {
			t.Errorf("id %q accepted", id)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("opener ran: %v", *calls)
	}
}

func TestClipOpenInAppUsesTheLink(t *testing.T) {
	calls, _ := setupClipOpen(t, "photo.png", "x")
	// --in-app talks to the local app, not the API.
	t.Setenv("MP_API_KEY", "")
	prevRegistered := windowsSchemeRegistered
	windowsSchemeRegistered = func() bool { return true }
	t.Cleanup(func() { windowsSchemeRegistered = prevRegistered })

	clipOpenInApp = true
	if err := runClipOpen(clipOpenCmd, []string{"42"}); err != nil {
		t.Fatal(err)
	}
	wantName, wantArgs := openerCommand(runtime.GOOS, "mahpastes://clip/42", "")
	if len(*calls) != 1 || (*calls)[0].name != wantName || !reflect.DeepEqual((*calls)[0].args, wantArgs) {
		t.Fatalf("opener calls = %v, want %s %q", *calls, wantName, wantArgs)
	}

	if runtime.GOOS == "windows" {
		windowsSchemeRegistered = func() bool { return false }
		if err := runClipOpen(clipOpenCmd, []string{"42"}); err == nil {
			t.Fatal("--in-app without a registered scheme must fail")
		}
	}
}
