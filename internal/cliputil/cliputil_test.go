package cliputil

import (
	"os"
	"testing"

	"golang.design/x/clipboard"
)

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
