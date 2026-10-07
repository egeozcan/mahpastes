package webui

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

// Picking a candidate for an ambiguous Markdown image must load it through
// the same session-wide pool as the render's automatic loads, under the
// render that drew the chooser. Calling GetLocalImage directly let a click
// start a fifth backend read while four automatic ones held every slot, and
// a pick on a superseded render's (detached) placeholder still read the clip.
func TestMarkdownChooserImageLoadUsesSharedPool(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node,
		filepath.Join("testdata", "markdown_chooser_harness.js"),
		filepath.Join("js", "editor", "markdown-preview.js"),
	).CombinedOutput()
	if err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
	var result struct {
		MaxInFlight  int `json:"maxInFlight"`
		InFlight     int `json:"inFlight"`
		ChosenLoaded int `json:"chosenLoaded"`
		StaleCalls   int `json:"staleCalls"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("bad harness output %q: %v", out, err)
	}
	const limit = 4 // IMAGE_LOAD_CONCURRENCY
	if result.MaxInFlight > limit {
		t.Fatalf("%d GetLocalImage calls in flight after a chooser pick, want <= %d", result.MaxInFlight, limit)
	}
	if result.ChosenLoaded != 1 {
		t.Fatalf("chosen image read %d times, want exactly 1 (pick lost, or a double click queued a second read)", result.ChosenLoaded)
	}
	if result.StaleCalls != 0 {
		t.Fatalf("a pick on a superseded render's placeholder started %d reads, want 0", result.StaleCalls)
	}
	if result.InFlight != 0 {
		t.Fatalf("%d reads never settled", result.InFlight)
	}
}
