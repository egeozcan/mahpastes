//go:build darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestInstallBundleNeverExposesAppWithoutExecutable(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source", "mahpastes.app")
	executable := filepath.Join(source, "Contents", "MacOS", "mahpastes")
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("test executable"), 0o755); err != nil {
		t.Fatal(err)
	}

	fakeBin := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(fakeBin, "cp"), `#!/bin/sh
set -eu
for destination do :; done
mkdir -p "$destination"
sleep 0.2
rm -rf "$destination"
exec /bin/cp "$@"
`)
	writeExecutable(t, filepath.Join(fakeBin, "xattr"), "#!/bin/sh\nexit 0\n")

	installDir := filepath.Join(tmp, "Applications")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(installDir, "mahpastes.app")
	targetExecutable := filepath.Join(target, "Contents", "MacOS", "mahpastes")

	cmd := exec.Command("make", "install-bundle", "APP_NAME=mahpastes", "APP_BUNDLE="+source, "INSTALL_DIR="+installDir)
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var observedIncomplete atomic.Bool
	done := make(chan struct{})
	watcherStopped := make(chan struct{})
	go func() {
		defer close(watcherStopped)
		for {
			select {
			case <-done:
				return
			default:
				if info, err := os.Stat(target); err == nil && info.IsDir() {
					if _, err := os.Stat(targetExecutable); os.IsNotExist(err) {
						observedIncomplete.Store(true)
					}
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()
	output, err := cmd.CombinedOutput()
	close(done)
	<-watcherStopped
	if err != nil {
		t.Fatalf("install-bundle failed: %v\n%s", err, output)
	}
	if observedIncomplete.Load() {
		t.Fatal("install exposed the .app bundle before its executable existed")
	}
	if info, err := os.Stat(targetExecutable); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("installed executable is missing or not executable: %v", err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
