package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatesAtStartup(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(Dir(dir), fileName)
	os.WriteFile(p, []byte(strings.Repeat("x", 100)), 0o644)
	w, err := open(dir, 50)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Fatalf("expected rotated file: %v", err)
	}
	if w.size != 0 {
		t.Fatalf("size = %d, want 0", w.size)
	}
}

func TestRotatesWhileRunning(t *testing.T) {
	dir := t.TempDir()
	w, err := open(dir, 20)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for i := 0; i < 5; i++ {
		if _, err := w.Write([]byte("0123456789012345678901\n")); err != nil {
			t.Fatal(err)
		}
	}
	p := filepath.Join(Dir(dir), fileName)
	st, err := os.Stat(p)
	if err != nil || st.Size() > 30 {
		t.Fatalf("active log not capped: %v %v", st, err)
	}
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Fatalf("missing rotated file: %v", err)
	}
}
