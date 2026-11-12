package logfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func logPath(dataDir string) string { return filepath.Join(Dir(dataDir), fileName) }

func seed(t *testing.T, dataDir string, n int) {
	t.Helper()
	if err := os.MkdirAll(Dir(dataDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath(dataDir), []byte(strings.Repeat("x", n)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func size(t *testing.T, path string) int64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Size()
}

// stubCrashOutput records SetCrashOutput calls instead of touching the
// process-wide crash output.
func stubCrashOutput(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	orig := setCrashOutput
	setCrashOutput = func(f *os.File) error {
		if f == nil {
			calls = append(calls, "")
		} else {
			calls = append(calls, f.Name())
		}
		return nil
	}
	t.Cleanup(func() { setCrashOutput = orig })
	return &calls
}

func TestOpenAppendsWithoutRotating(t *testing.T) {
	dir := t.TempDir()
	seed(t, dir, 100)
	w, err := open(dir, 50)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.Write([]byte("second launch\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(logPath(dir) + ".1"); !os.IsNotExist(err) {
		t.Fatalf("log rotated before EnableRotation: %v", err)
	}
	if got := size(t, logPath(dir)); got != 114 {
		t.Fatalf("size = %d, want 114 (appended)", got)
	}
}

func TestEnableRotationRotatesOversizedLog(t *testing.T) {
	dir := t.TempDir()
	seed(t, dir, 100)
	w, err := open(dir, 50)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.EnableRotation(); err != nil {
		t.Fatal(err)
	}
	if got := size(t, logPath(dir)+".1"); got != 100 {
		t.Fatalf("rotated size = %d, want 100", got)
	}
	if w.size != 0 || size(t, logPath(dir)) != 0 {
		t.Fatalf("active log not fresh: w.size=%d", w.size)
	}
}

func TestRotatesWhileRunning(t *testing.T) {
	dir := t.TempDir()
	w, err := open(dir, 20)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.EnableRotation(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := w.Write([]byte("0123456789012345678901\n")); err != nil {
			t.Fatal(err)
		}
	}
	if got := size(t, logPath(dir)); got > 23 {
		t.Fatalf("active log not capped: %d bytes", got)
	}
	if got := size(t, logPath(dir)+".1"); got != 23 {
		t.Fatalf("rotated log = %d bytes, want 23", got)
	}
}

func TestCrashOutputFollowsRotation(t *testing.T) {
	calls := stubCrashOutput(t)
	dir := t.TempDir()
	w, err := open(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.CaptureCrashes(); err != nil {
		t.Fatal(err)
	}
	if err := w.EnableRotation(); err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("0123456789\n")) // reaches the cap
	w.Write([]byte("next\n"))       // rotates first
	w.Close()

	p := logPath(dir)
	// Capture, then for the rotation: release before the rename (Windows
	// cannot rename the file while the duplicate handle is open) and re-point
	// at the new file; Close releases it again.
	want := []string{p, "", p, ""}
	if strings.Join(*calls, "|") != strings.Join(want, "|") {
		t.Fatalf("crash output calls = %q, want %q", *calls, want)
	}
}

func TestRenameFailureTruncatesInsteadOfGrowing(t *testing.T) {
	orig := rename
	renames := 0
	rename = func(_, _ string) error {
		renames++
		return errors.New("file in use")
	}
	t.Cleanup(func() { rename = orig })

	dir := t.TempDir()
	w, err := open(dir, 20)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.EnableRotation(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := w.Write([]byte("0123456789012345678901\n")); err != nil {
			t.Fatal(err)
		}
	}
	if got := size(t, logPath(dir)); got > 23 {
		t.Fatalf("log grew past the cap after a failed rename: %d bytes", got)
	}
	// One rotation attempt per write that found the log full, not a retry on
	// every write.
	if renames != 9 {
		t.Fatalf("rename attempts = %d, want 9", renames)
	}
}

func TestWriteAfterCloseFails(t *testing.T) {
	w, err := open(t.TempDir(), MaxSize)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	if _, err := w.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("err = %v, want os.ErrClosed", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrInvalid }

func TestTeeWritesFileWhenConsoleFails(t *testing.T) {
	dir := t.TempDir()
	w, err := open(dir, MaxSize)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	n, err := w.Tee(failingWriter{}).Write([]byte("hello\n"))
	if err != nil || n != 6 {
		t.Fatalf("Write = %d, %v; want 6, nil", n, err)
	}
	got, err := os.ReadFile(logPath(dir))
	if err != nil || string(got) != "hello\n" {
		t.Fatalf("log = %q, %v", got, err)
	}
}
