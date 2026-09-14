//go:build darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	writeExecutable(t, filepath.Join(fakeBin, "codesign"), "#!/bin/sh\nexit 0\n")

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

func TestInstallBundleKeepsExistingAppWhenStagingSignatureIsInvalid(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source", "mahpastes.app")
	sourceExecutable := filepath.Join(source, "Contents", "MacOS", "mahpastes")
	if err := os.MkdirAll(filepath.Dir(sourceExecutable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceExecutable, []byte("new executable"), 0o755); err != nil {
		t.Fatal(err)
	}

	installDir := filepath.Join(tmp, "Applications")
	targetExecutable := filepath.Join(installDir, "mahpastes.app", "Contents", "MacOS", "mahpastes")
	if err := os.MkdirAll(filepath.Dir(targetExecutable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetExecutable, []byte("known good executable"), 0o755); err != nil {
		t.Fatal(err)
	}

	fakeBin := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(fakeBin, "xattr"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(fakeBin, "codesign"), `#!/bin/sh
case "$*" in
  *installing*) exit 1 ;;
esac
exit 0
`)

	cmd := exec.Command("make", "install-bundle", "APP_NAME=mahpastes", "APP_BUNDLE="+source, "INSTALL_DIR="+installDir)
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("install-bundle unexpectedly accepted an invalid staged bundle: %s", output)
	}
	got, err := os.ReadFile(targetExecutable)
	if err != nil {
		t.Fatalf("known-good installed executable was not preserved: %v", err)
	}
	if string(got) != "known good executable" {
		t.Fatalf("installed app was replaced despite staging signature failure: %q", got)
	}
}

func TestInstallBundleRestoresExistingAppWhenBackupStepFails(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source", "mahpastes.app")
	sourceExecutable := filepath.Join(source, "Contents", "MacOS", "mahpastes")
	if err := os.MkdirAll(filepath.Dir(sourceExecutable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceExecutable, []byte("new executable"), 0o755); err != nil {
		t.Fatal(err)
	}

	installDir := filepath.Join(tmp, "Applications")
	targetExecutable := filepath.Join(installDir, "mahpastes.app", "Contents", "MacOS", "mahpastes")
	if err := os.MkdirAll(filepath.Dir(targetExecutable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetExecutable, []byte("known good executable"), 0o755); err != nil {
		t.Fatal(err)
	}

	fakeBin := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(fakeBin, "xattr"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(fakeBin, "codesign"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(fakeBin, "mv"), `#!/bin/sh
set -eu
/bin/mv "$@"
case "$2" in
  *.mahpastes.previous.*) exit 1 ;;
esac
`)

	cmd := exec.Command("make", "install-bundle", "APP_NAME=mahpastes", "APP_BUNDLE="+source, "INSTALL_DIR="+installDir)
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("install-bundle unexpectedly completed after backup failure: %s", output)
	}
	got, err := os.ReadFile(targetExecutable)
	if err != nil {
		t.Fatalf("known-good installed executable was not restored after backup failure: %v", err)
	}
	if string(got) != "known good executable" {
		t.Fatalf("installed app was not restored after backup failure: %q", got)
	}
}

func TestInstallBundleKeepsVerifiedAppWhenBackupCleanupFails(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	source := filepath.Join(tmp, "source", "mahpastes.app")
	sourceExecutable := filepath.Join(source, "Contents", "MacOS", "mahpastes")
	if err := os.MkdirAll(filepath.Dir(sourceExecutable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceExecutable, []byte("verified new executable"), 0o755); err != nil {
		t.Fatal(err)
	}

	installDir := filepath.Join(tmp, "Applications")
	targetExecutable := filepath.Join(installDir, "mahpastes.app", "Contents", "MacOS", "mahpastes")
	if err := os.MkdirAll(filepath.Dir(targetExecutable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetExecutable, []byte("old executable"), 0o755); err != nil {
		t.Fatal(err)
	}

	fakeBin := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(fakeBin, "xattr"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(fakeBin, "codesign"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(fakeBin, "rm"), `#!/bin/sh
case "$*" in
  *.mahpastes.previous.*) exit 1 ;;
esac
exec /bin/rm "$@"
`)

	cmd := exec.Command("make", "install-bundle", "APP_NAME=mahpastes", "APP_BUNDLE="+source, "INSTALL_DIR="+installDir)
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("install-bundle unexpectedly completed after backup cleanup failure: %s", output)
	}
	got, err := os.ReadFile(targetExecutable)
	if err != nil {
		t.Fatalf("verified installed executable was not preserved after backup cleanup failure: %v", err)
	}
	if string(got) != "verified new executable" {
		t.Fatalf("verified installed app was rolled back after backup cleanup failure: %q", got)
	}
}

func TestStopInstalledAppStopsFileProviderExtension(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	fakeBin := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(tmp, "calls")
	writeExecutable(t, filepath.Join(fakeBin, "pkill"), `#!/bin/sh
printf '%s\n' "$*" >> "$CALLS"
`)
	writeExecutable(t, filepath.Join(fakeBin, "pgrep"), "#!/bin/sh\nexit 1\n")

	cmd := exec.Command("make", "stop-installed-app", "APP_PROCESS_PATTERN=/host", "FILE_PROVIDER_PROCESS_PATTERN=/extension")
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"), "CALLS="+calls)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("stop-installed-app failed: %v\n%s", err, output)
	}
	got, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-TERM -f /host", "-TERM -f /extension"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("missing %q from stop commands: %s", want, got)
		}
	}
}

func TestInstallReadyBundleStopsExtensionAfterCopy(t *testing.T) {
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
	calls := filepath.Join(tmp, "calls")
	writeExecutable(t, filepath.Join(fakeBin, "cp"), `#!/bin/sh
printf 'copy\n' >> "$CALLS"
exec /bin/cp "$@"
`)
	writeExecutable(t, filepath.Join(fakeBin, "xattr"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(fakeBin, "codesign"), `#!/bin/sh
printf 'codesign %s\n' "$*" >> "$CALLS"
`)
	writeExecutable(t, filepath.Join(fakeBin, "pkill"), `#!/bin/sh
printf 'pkill %s\n' "$*" >> "$CALLS"
`)
	writeExecutable(t, filepath.Join(fakeBin, "pgrep"), "#!/bin/sh\nexit 1\n")
	writeExecutable(t, filepath.Join(fakeBin, "open"), `#!/bin/sh
printf 'open\n' >> "$CALLS"
`)

	installDir := filepath.Join(tmp, "Applications")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(tmp, "plugins")
	cmd := exec.Command("make", "install-ready-bundle", "APP_NAME=mahpastes", "APP_BUNDLE="+source, "INSTALL_DIR="+installDir, "PLUGIN_DIR="+pluginDir, "APP_PROCESS_PATTERN=/host", "FILE_PROVIDER_PROCESS_PATTERN=/extension")
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"), "CALLS="+calls)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install-ready-bundle failed: %v\n%s", err, output)
	}
	got, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	copyAt := strings.Index(string(got), "copy\n")
	firstSignatureCheck := strings.Index(string(got), "codesign --verify --deep --strict")
	openAt := strings.Index(string(got), "open\n")
	postCopyExtensionStop := strings.LastIndex(string(got), "pkill -TERM -f /extension")
	if copyAt < 0 || firstSignatureCheck < copyAt || openAt < firstSignatureCheck || postCopyExtensionStop < openAt {
		t.Fatalf("bundle was not verified before launch and extension cleanup: %s", got)
	}
	if _, err = os.Stat(filepath.Join(pluginDir, "ascii-art.lua")); err != nil {
		t.Fatalf("bundled plugins were not installed before launch: %v", err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
