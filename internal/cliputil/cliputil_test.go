package cliputil

import (
	"errors"
	"os"
	"strings"
	"testing"

	"golang.design/x/clipboard"
)

// fakeClipboard swaps the library entry points for the duration of a test so
// the real system clipboard is never touched. It returns the write count.
func fakeClipboard(t *testing.T, initErr error, written bool) *int {
	t.Helper()
	calls := 0
	origInit, origWrite := initClipboard, writeClipboard
	t.Cleanup(func() { initClipboard, writeClipboard = origInit, origWrite })
	initClipboard = func() error { return initErr }
	writeClipboard = func(clipboard.Format, []byte) <-chan struct{} {
		calls++
		if !written {
			return nil
		}
		return make(chan struct{})
	}
	return &calls
}

func TestWriteReportsInitFailure(t *testing.T) {
	initErr := errors.New("no display")
	calls := fakeClipboard(t, initErr, true)
	err := WriteText("x")
	if !errors.Is(err, initErr) || !strings.Contains(err.Error(), "initialize") {
		t.Fatalf("got %v, want a wrapped init error", err)
	}
	if *calls != 0 {
		t.Fatalf("clipboard.Write called %d times after a failed Init", *calls)
	}
}

func TestWriteReportsRejectedWrite(t *testing.T) {
	calls := fakeClipboard(t, nil, false)
	if err := Write(clipboard.FmtImage, []byte("png")); err == nil {
		t.Fatal("a nil channel from clipboard.Write must be reported as an error")
	}
	if *calls != 1 {
		t.Fatalf("clipboard.Write called %d times, want 1", *calls)
	}
}

func TestWriteSucceeds(t *testing.T) {
	calls := fakeClipboard(t, nil, true)
	if err := WriteText("x"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("clipboard.Write called %d times, want 1", *calls)
	}
}

// Opt in only: this replaces the real system clipboard.
func TestWriteTextRoundTrip(t *testing.T) {
	if os.Getenv("MAHPASTES_TEST_CLIPBOARD") != "1" {
		t.Skip("set MAHPASTES_TEST_CLIPBOARD=1 to test the desktop clipboard")
	}
	if err := WriteText("mahpastes cliputil"); err != nil {
		t.Fatal(err)
	}
	if got := string(clipboard.Read(clipboard.FmtText)); got != "mahpastes cliputil" {
		t.Fatalf("got %q", got)
	}
}
