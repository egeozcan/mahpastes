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
			name: "empty args are ignored",
			args: []string{"", ""},
			want: Request{Activate: true},
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
	got := FromArgs([]string{
		"file:///home/u/a%20b.png",
		"file://localhost/home/u/c.txt",
		"file://otherhost/home/u/d.txt",
	}, "/cwd")
	want := []string{"/home/u/a b.png", "/home/u/c.txt"}
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
