package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNormalizePastedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix path forms; windows is covered by TestNormalizePastedPathWindows")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	tests := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		{"absolute", "/Users/me/photo.png", "/Users/me/photo.png", true},
		{"surrounding whitespace", "  /Users/me/photo.png \n", "/Users/me/photo.png", true},
		{"double quoted", `"/Users/me/my photo.png"`, "/Users/me/my photo.png", true},
		{"single quoted", `'/Users/me/my photo.png'`, "/Users/me/my photo.png", true},
		{"shell escaped spaces", `/Users/me/my\ photo.png`, "/Users/me/my photo.png", true},
		{"shell escaped parens", `/Users/me/report\ \(final\).png`, "/Users/me/report (final).png", true},
		{"shell escaped ampersand", `/Users/me/a\&b.txt`, "/Users/me/a&b.txt", true},
		{"shell escaped quote", `/Users/me/it\'s.txt`, "/Users/me/it's.txt", true},
		{"escaped backslash kept", `/Users/me/a\\b.png`, `/Users/me/a\b.png`, true},
		{"backslash before letter kept", `/Users/me/a\b.png`, `/Users/me/a\b.png`, true},
		{"backslash before digit kept", `/Users/me/a\2b.png`, `/Users/me/a\2b.png`, true},
		{"tilde", "~/photo.png", filepath.Join(home, "photo.png"), true},
		{"bare tilde", "~", home, true},
		{"file url", "file:///Users/me/photo.png", "/Users/me/photo.png", true},
		{"file url percent encoded", "file:///Users/me/my%20photo.png", "/Users/me/my photo.png", true},
		{"file url localhost", "file://localhost/Users/me/photo.png", "/Users/me/photo.png", true},
		{"uncleaned path", "/Users/me/../me/photo.png", "/Users/me/photo.png", true},

		{"empty", "", "", false},
		{"whitespace only", "   ", "", false},
		{"relative", "photo.png", "", false},
		{"dot relative", "./photo.png", "", false},
		{"parent relative", "../photo.png", "", false},
		{"other user tilde", "~someone/photo.png", "", false},
		{"http url", "https://example.com/photo.png", "", false},
		{"remote file url", "file://otherhost/Users/me/photo.png", "", false},
		{"nul byte", "/Users/me/pho\x00to.png", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizePastedPath(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("normalizePastedPath(%q) ok = %v, want %v (got %q)", tt.in, ok, tt.wantOK, got)
			}
			if ok && got != tt.want {
				t.Errorf("normalizePastedPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizePastedPathWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows path forms")
	}
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{`C:\Users\me\photo.png`, `C:\Users\me\photo.png`, true},
		{`"C:\Users\me\my photo.png"`, `C:\Users\me\my photo.png`, true},
		{`file:///C:/Users/me/photo.png`, `C:\Users\me\photo.png`, true},
		{`photo.png`, "", false},
		{`\Users\me\photo.png`, "", false}, // rooted but volume-less
	}
	for _, tt := range tests {
		got, ok := normalizePastedPath(tt.in)
		if ok != tt.wantOK {
			t.Fatalf("normalizePastedPath(%q) ok = %v, want %v (got %q)", tt.in, ok, tt.wantOK, got)
		}
		if ok && got != tt.want {
			t.Errorf("normalizePastedPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestProbeFilePaths(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	subDir := filepath.Join(dir, "sub")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	a := &App{}
	probes, err := a.ProbeFilePaths([]string{
		filePath,
		subDir,
		filepath.Join(dir, "missing.txt"),
		"just some pasted prose",
	})
	if err != nil {
		t.Fatalf("ProbeFilePaths: %v", err)
	}
	if len(probes) != 4 {
		t.Fatalf("got %d probes, want 4", len(probes))
	}

	if !probes[0].Exists || probes[0].IsDir || !probes[0].IsRegular {
		t.Errorf("file probe = %+v, want an existing regular file", probes[0])
	}
	if probes[0].Name != "note.txt" || probes[0].Size != 5 {
		t.Errorf("file probe name/size = %q/%d, want note.txt/5", probes[0].Name, probes[0].Size)
	}
	if probes[0].Input != filePath {
		t.Errorf("probe echoed input %q, want %q", probes[0].Input, filePath)
	}

	if !probes[1].Exists || !probes[1].IsDir || probes[1].IsRegular {
		t.Errorf("dir probe = %+v, want an existing directory", probes[1])
	}
	if probes[2].Exists {
		t.Errorf("missing-file probe = %+v, want Exists false", probes[2])
	}
	if probes[3].Exists || probes[3].Path != "" {
		t.Errorf("prose probe = %+v, want unresolved", probes[3])
	}
}

// "Copy Path" and drag-out hand the user an absolute path into the app's own
// temp dir. Pasting one back must not offer to import it — that would duplicate
// a clip that already exists, named after a throwaway lease file.
func TestProbeFilePathsMarksOwnTempFiles(t *testing.T) {
	dataDir := t.TempDir()
	tempDir := filepath.Join(dataDir, "clip_temp_files")
	if err := os.Mkdir(tempDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	ourFile := filepath.Join(tempDir, "42_photo.png")
	if err := os.WriteFile(ourFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// A file next to the temp dir, whose path shares its prefix as a string but
	// is not inside it — a naive strings.HasPrefix check would misfire here.
	sibling := filepath.Join(dataDir, "clip_temp_files_backup.png")
	if err := os.WriteFile(sibling, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	outside := filepath.Join(dataDir, "elsewhere.png")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	a := &App{tempDir: tempDir}
	probes, err := a.ProbeFilePaths([]string{ourFile, sibling, outside})
	if err != nil {
		t.Fatalf("ProbeFilePaths: %v", err)
	}
	if !probes[0].IsTemp {
		t.Errorf("temp file %q not marked IsTemp", ourFile)
	}
	if probes[1].IsTemp {
		t.Errorf("sibling %q wrongly marked IsTemp", sibling)
	}
	if probes[2].IsTemp {
		t.Errorf("unrelated file %q wrongly marked IsTemp", outside)
	}
	// The rest of the probe stays truthful — only importability changes.
	if !probes[0].Exists || !probes[0].IsRegular {
		t.Errorf("temp probe = %+v, want an existing regular file", probes[0])
	}
}

// With no temp store initialized, nothing is a temp file.
func TestProbeFilePathsWithoutTempDir(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	a := &App{}
	probes, err := a.ProbeFilePaths([]string{filePath})
	if err != nil {
		t.Fatalf("ProbeFilePaths: %v", err)
	}
	if probes[0].IsTemp {
		t.Errorf("probe = %+v, want IsTemp false when no temp dir is configured", probes[0])
	}
}

func TestIsInsideDir(t *testing.T) {
	sep := string(filepath.Separator)
	root := filepath.Join(sep+"data", "clip_temp_files")
	tests := []struct {
		path string
		want bool
	}{
		{filepath.Join(root, "42_photo.png"), true},
		{filepath.Join(root, "nested", "x.png"), true},
		{root, true}, // the dir itself
		{filepath.Join(sep+"data", "clip_temp_files_backup.png"), false},
		{filepath.Join(sep+"data", "elsewhere.png"), false},
		{filepath.Join(sep + "other"), false},
	}
	for _, tt := range tests {
		if got := isInsideDir(root, tt.path); got != tt.want {
			t.Errorf("isInsideDir(%q, %q) = %v, want %v", root, tt.path, got, tt.want)
		}
	}
	if isInsideDir("", filepath.Join(sep+"data", "x.png")) {
		t.Error("isInsideDir with an empty dir returned true")
	}
}

func TestProbeFilePathsRejectsOversizedBatch(t *testing.T) {
	a := &App{}
	paths := make([]string, maxProbePaths+1)
	for i := range paths {
		paths[i] = "/tmp/x"
	}
	if _, err := a.ProbeFilePaths(paths); err == nil {
		t.Fatal("ProbeFilePaths accepted an oversized batch, want error")
	} else if !strings.Contains(err.Error(), "too many paths") {
		t.Errorf("unexpected error: %v", err)
	}
}
