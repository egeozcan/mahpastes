package openrequest

import (
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestFromArgs(t *testing.T) {
	cwd := filepath.FromSlash("/home/u/work")
	abs := func(p string) string { return filepath.FromSlash(p) }
	if runtime.GOOS == "windows" {
		cwd = `C:\Users\u\work`
		abs = func(p string) string { return `C:` + filepath.FromSlash(p) }
	}

	tests := []struct {
		name string
		args []string
		want Request
	}{
		{
			name: "no args is a plain activate",
			args: nil,
			want: Request{Activate: true},
		},
		{
			name: "absolute and relative paths",
			args: []string{abs("/tmp/a.png"), "notes/b.txt", "./c.md"},
			want: Request{Activate: true, Files: []string{
				abs("/tmp/a.png"),
				filepath.Join(cwd, "notes", "b.txt"),
				filepath.Join(cwd, "c.md"),
			}},
		},
		{
			name: "flags are skipped, including the old macOS process serial",
			args: []string{"-psn_0_12345", "--verbose", "-loglevel", abs("/tmp/a.png")},
			want: Request{Activate: true, Files: []string{abs("/tmp/a.png")}},
		},
		{
			name: "double dash lets a dash-named file through",
			args: []string{"--", "-weird.txt"},
			want: Request{Activate: true, Files: []string{filepath.Join(cwd, "-weird.txt")}},
		},
		{
			name: "mahpastes URLs are kept verbatim, any case",
			args: []string{"mahpastes://clip/12", "MAHPASTES://show"},
			want: Request{Activate: true, URLs: []string{"mahpastes://clip/12", "MAHPASTES://show"}},
		},
		{
			name: "foreign URLs are dropped",
			args: []string{"https://example.com/x.png", "ftp://host/y"},
			want: Request{Activate: true},
		},
		{
			name: "colon in a relative name is not a scheme",
			args: []string{"notes:2024.txt"},
			want: Request{Activate: true, Files: []string{filepath.Join(cwd, "notes:2024.txt")}},
		},
		{
			name: "the paste flag is the one flag that counts",
			args: []string{"--paste-clipboard", abs("/tmp/a.png")},
			want: Request{Activate: true, PasteClipboard: true, Files: []string{abs("/tmp/a.png")}},
		},
		{
			name: "after a double dash the paste flag is a file name",
			args: []string{"--", "--paste-clipboard"},
			want: Request{Activate: true, Files: []string{filepath.Join(cwd, "--paste-clipboard")}},
		},
		{
			name: "empty args are ignored",
			args: []string{"", ""},
			want: Request{Activate: true},
		},
		{
			name: "the hidden flag does not activate",
			args: []string{"--hidden"},
			want: Request{},
		},
		{
			name: "after a double dash the hidden flag is a file name",
			args: []string{"--", "--hidden"},
			want: Request{Activate: true, Files: []string{filepath.Join(cwd, "--hidden")}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromArgs(tt.args, cwd)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("FromArgs(%q)\n got  %#v\n want %#v", tt.args, got, tt.want)
			}
		})
	}
}

func TestFromArgsRelativeWithoutCwdIsDropped(t *testing.T) {
	got := FromArgs([]string{"rel.txt"}, "")
	if len(got.Files) != 0 {
		t.Fatalf("relative path without a working directory must be dropped, got %v", got.Files)
	}
}

func TestFromArgsFileURLs(t *testing.T) {
	if runtime.GOOS == "windows" {
		got := FromArgs([]string{"file:///C:/Users/u/a%20b.png"}, "")
		want := []string{`C:\Users\u\a b.png`}
		if !reflect.DeepEqual(got.Files, want) {
			t.Fatalf("got %v, want %v", got.Files, want)
		}
		return
	}
	// What a Linux launcher passes for %U: local files as file:// URIs, and
	// remote locations (a file manager browsing sftp:// or smb://) as
	// themselves. Remote ones are not files here and must not become
	// cwd-relative paths.
	got := FromArgs([]string{
		"file:///home/u/a%20b.png",
		"file://localhost/home/u/c.txt",
		"file://otherhost/home/u/d.txt",
		"file:///home/u/%C3%BCber%23.txt",
		"sftp://host/home/u/e.txt",
		"smb://server/share/f.txt",
	}, "/cwd")
	want := []string{"/home/u/a b.png", "/home/u/c.txt", "/home/u/über#.txt"}
	if !reflect.DeepEqual(got.Files, want) {
		t.Fatalf("got %v, want %v", got.Files, want)
	}
}

func TestParseURL(t *testing.T) {
	for _, tt := range []struct {
		in   string
		ok   bool
		want string
	}{
		{"mahpastes://clip/1", true, "mahpastes://clip/1"},
		{"  mahpastes:show  ", true, "mahpastes:show"},
		{"Mahpastes://tag/a%2Fb", true, "Mahpastes://tag/a%2Fb"},
		{"mahpaste://x", false, ""},
		{"http://mahpastes/x", false, ""},
		{"/abs/path", false, ""},
		{"mahpastes://bad%zz", false, ""},
	} {
		got, ok := ParseURL(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("ParseURL(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestStripOwnFlags(t *testing.T) {
	for _, tt := range []struct {
		in, want []string
	}{
		{[]string{"mahpastes"}, []string{"mahpastes"}},
		{[]string{"mahpastes", "--paste-clipboard"}, []string{"mahpastes"}},
		{[]string{"mahpastes", "-loglevel", "debug", "--paste-clipboard", "a.png"}, []string{"mahpastes", "-loglevel", "debug", "a.png"}},
		{[]string{"mahpastes", "--", "--paste-clipboard"}, []string{"mahpastes", "--", "--paste-clipboard"}},
		{[]string{"mahpastes", "--hidden", "-loglevel", "debug"}, []string{"mahpastes", "-loglevel", "debug"}},
		{[]string{"mahpastes", "--", "--hidden"}, []string{"mahpastes", "--", "--hidden"}},
		// The program name is never touched, whatever it is called.
		{[]string{"--paste-clipboard"}, []string{"--paste-clipboard"}},
	} {
		if got := StripOwnFlags(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("StripOwnFlags(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWantsStartHidden(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"--hidden"}, true},
		{[]string{"-loglevel", "debug", "--hidden"}, true},
		{[]string{"--", "--hidden"}, false},
		{[]string{"--hidden-not"}, false},
		// A link's own text can add the flag on Windows; links never hide.
		{[]string{"mahpastes://clip/1", "--hidden"}, false},
		{[]string{"--hidden", "mahpastes:show"}, false},
	} {
		if got := WantsStartHidden(tt.args); got != tt.want {
			t.Errorf("WantsStartHidden(%q) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestPasteOnlyRequestIsNotEmpty(t *testing.T) {
	if (Request{PasteClipboard: true}).Empty() {
		t.Fatal("a paste request must count as work for the frontend")
	}
	if !(Request{Activate: true}).Empty() {
		t.Fatal("a plain activate carries nothing for the frontend")
	}
}

// A mahpastes: link reaches the app as an argument of the command the OS
// registered for the scheme. Windows pastes the link into that command line
// unescaped, so a link containing a quote can add arguments of its own: a
// launch that carries a link carries nothing else.
func TestFromArgsLinkLaunchCarriesNothingElse(t *testing.T) {
	secret := filepath.FromSlash("/home/u/secret.txt")
	if runtime.GOOS == "windows" {
		secret = `C:\Users\u\secret.txt`
	}
	tests := []struct {
		name string
		args []string
	}{
		{"flag after the link", []string{"mahpastes://clip/1", PasteClipboardFlag}},
		{"flag before the link", []string{PasteClipboardFlag, "mahpastes://clip/1"}},
		{"file after the link", []string{"mahpastes://clip/1", secret}},
		{"file URL after the link", []string{"mahpastes://clip/1", "file:///etc/passwd"}},
		{"after a flag terminator", []string{"mahpastes://clip/1", "--", secret}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FromArgs(tc.args, filepath.Dir(secret))
			want := Request{Activate: true, URLs: []string{"mahpastes://clip/1"}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("FromArgs(%q) = %#v, want %#v", tc.args, got, want)
			}
		})
	}
}

// Clicking a toast after the app that showed it has gone makes Windows start
// the registered COM activator: `mahpastes.exe %1 -Embedding`, with the %1
// left as it is. That launch only brings the window forward.
func TestFromArgsCOMActivationOnlyActivates(t *testing.T) {
	cwd := filepath.FromSlash("/home/u")
	if runtime.GOOS == "windows" {
		cwd = `C:\Users\u`
	}
	for _, args := range [][]string{
		{"%1", "-Embedding"},
		{"-Embedding"},
		{"%1", "-embedding"},
	} {
		got := FromArgs(args, cwd)
		if want := (Request{Activate: true}); !reflect.DeepEqual(got, want) {
			t.Errorf("FromArgs(%q) = %#v, want %#v", args, got, want)
		}
	}
	// COM's other spelling is an ordinary absolute path outside Windows.
	got := FromArgs([]string{"/Embedding"}, cwd)
	if runtime.GOOS == "windows" {
		if want := (Request{Activate: true}); !reflect.DeepEqual(got, want) {
			t.Errorf("FromArgs(/Embedding) = %#v, want %#v", got, want)
		}
	} else if !reflect.DeepEqual(got.Files, []string{"/Embedding"}) {
		t.Errorf("a file named /Embedding was dropped: %#v", got)
	}
}
