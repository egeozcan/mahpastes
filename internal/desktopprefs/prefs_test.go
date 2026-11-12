package desktopprefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestKeepRunningDefaultsPerPlatform(t *testing.T) {
	var p Prefs
	if !p.KeepRunning("darwin") {
		t.Error("macOS keeps running by default")
	}
	for _, goos := range []string{"linux", "windows"} {
		if p.KeepRunning(goos) {
			t.Errorf("%s quits on close by default", goos)
		}
	}
	off := false
	if (Prefs{KeepRunningWhenClosed: &off}).KeepRunning("darwin") {
		t.Error("a stored choice beats the platform default")
	}
}

func TestLoadMissingFileIsDefaults(t *testing.T) {
	p, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.KeepRunningWhenClosed != nil {
		t.Fatalf("missing file must leave the choice unset, got %v", *p.KeepRunningWhenClosed)
	}
}

func TestSetKeepRunningRoundTrips(t *testing.T) {
	dir := t.TempDir()
	for _, want := range []bool{true, false} {
		if err := SetKeepRunningWhenClosed(dir, want); err != nil {
			t.Fatalf("Set(%v): %v", want, err)
		}
		p, err := Load(dir)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if p.KeepRunningWhenClosed == nil || *p.KeepRunningWhenClosed != want {
			t.Fatalf("got %v, want %v", p.KeepRunningWhenClosed, want)
		}
	}
	// No temp files left behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != FileName {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("data dir holds %v, want only %s", names, FileName)
	}
}

func TestSetKeepsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte(`{"window":{"w":900},"keep_running_when_closed":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetKeepRunningWhenClosed(dir, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	var window struct{ W int }
	if err := json.Unmarshal(raw["window"], &window); err != nil || window.W != 900 {
		t.Fatalf("unknown key changed or dropped: %q", raw["window"])
	}
	if string(raw[keepRunningKey]) != "true" {
		t.Fatalf("keep running = %s", raw[keepRunningKey])
	}
}

func TestCorruptFileFallsBackAndCanBeOverwritten(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err == nil {
		t.Fatal("a corrupt file must be reported")
	}
	if p.KeepRunningWhenClosed != nil {
		t.Fatal("a corrupt file must yield the defaults")
	}
	if err := SetKeepRunningWhenClosed(dir, true); err != nil {
		t.Fatalf("saving over a corrupt file: %v", err)
	}
	if p, err := Load(dir); err != nil || p.KeepRunningWhenClosed == nil || !*p.KeepRunningWhenClosed {
		t.Fatalf("after overwrite: %v, %v", p.KeepRunningWhenClosed, err)
	}
}

func TestWrongTypeIsReported(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte(`{"keep_running_when_closed":"yes"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("a non-boolean value must be reported")
	}
}

func TestOversizedOrNonRegularFileIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, FileName), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("a directory in place of the file must be reported")
	}

	dir = t.TempDir()
	big := make([]byte, maxFileSize+1)
	if err := os.WriteFile(Path(dir), big, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("an oversized file must be reported")
	}
}
