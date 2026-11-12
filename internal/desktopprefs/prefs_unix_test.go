//go:build unix

package desktopprefs

import (
	"syscall"
	"testing"
	"time"
)

// A FIFO in place of the prefs file must be refused, not opened: opening it
// would block the app's start until something wrote to it.
func TestFIFODoesNotBlockLoad(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(Path(dir), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := Load(dir)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a FIFO in place of the file must be reported")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Load blocked on a FIFO")
	}
}
