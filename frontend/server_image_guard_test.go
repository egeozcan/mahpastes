package webui

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type guardAttempt struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

type cardGuardResult struct {
	NoCardOversized  guardAttempt `json:"noCardOversized"`
	NoCardMetaFails  guardAttempt `json:"noCardMetaFails"`
	NoCardSmall      guardAttempt `json:"noCardSmall"`
	CardSmall        guardAttempt `json:"cardSmall"`
	CardSmallFetches int          `json:"cardSmallFetches"`
	CardOversized    guardAttempt `json:"cardOversized"`
	FooterLabel      string       `json:"footerLabel"`
	CentreLabel      string       `json:"centreLabel"`
}

func runCardGuardHarness(t *testing.T) cardGuardResult {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node,
		filepath.Join("testdata", "card_guard_harness.js"),
		filepath.Join("js", "ui.js"),
	).CombinedOutput()
	if err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
	var r cardGuardResult
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("bad harness output %q: %v", out, err)
	}
	return r
}

// Server mode hands an image's /data URL to the browser only under the 64 MiB
// inline ceiling. The size came from the gallery card, so an image opened
// without one (a Markdown reference to a clip outside the loaded page) read
// as zero bytes and got an unrestricted URL. The guard must look the size up
// and fail closed when it cannot.
func TestServerImageGuardFailsClosedWithoutCard(t *testing.T) {
	r := runCardGuardHarness(t)
	if r.NoCardOversized.Error == "" {
		t.Errorf("100 MB image with no gallery card got URL %q, want a too-large error", r.NoCardOversized.URL)
	}
	if r.NoCardMetaFails.Error == "" {
		t.Errorf("unknown-size image with no gallery card got URL %q, want an error", r.NoCardMetaFails.URL)
	}
	if r.NoCardSmall.URL == "" || r.NoCardSmall.Error != "" {
		t.Errorf("small image with no gallery card: %+v, want a URL", r.NoCardSmall)
	}
	if r.CardSmall.URL == "" || r.CardSmall.Error != "" {
		t.Errorf("small image with a card: %+v, want a URL", r.CardSmall)
	}
	if r.CardSmallFetches != 0 {
		t.Errorf("card carried the size but the guard made %d metadata requests", r.CardSmallFetches)
	}
	if !strings.Contains(r.CardOversized.Error, "too large") {
		t.Errorf("oversized image with a card: %+v, want a too-large error", r.CardOversized)
	}
}

// A rename patched in place must refresh every label derived from the
// filename. A generic-file card shows its type twice — in the footer and in
// the centre of the preview — and the patch used to update only the footer.
func TestRenamePatchRefreshesCentreTypeLabel(t *testing.T) {
	r := runCardGuardHarness(t)
	if r.FooterLabel != "DAT" {
		t.Errorf("footer type label = %q after rename to a.dat, want DAT", r.FooterLabel)
	}
	if r.CentreLabel != "DAT" {
		t.Errorf("centre type label = %q after rename to a.dat, want DAT", r.CentreLabel)
	}
}
