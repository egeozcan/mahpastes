package webui

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

// The Markdown preview loads images through a bounded pool. A superseded
// render cannot cancel the backend reads it already started, so a pool per
// render let each new render start IMAGE_LOAD_CONCURRENCY more reads on top
// of the stale ones still running. The scheduler is session-wide: a slot is
// freed only when its read settles, and stale queued loads never start.
func TestMarkdownImageLoadsStayBoundedAcrossRenders(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node,
		filepath.Join("testdata", "markdown_pool_harness.js"),
		filepath.Join("js", "editor", "markdown-preview.js"),
	).CombinedOutput()
	if err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
	var result struct {
		MaxInFlight int `json:"maxInFlight"`
		Calls       int `json:"calls"`
		InFlight    int `json:"inFlight"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("bad harness output %q: %v", out, err)
	}
	const limit = 4 // IMAGE_LOAD_CONCURRENCY
	if result.MaxInFlight > limit {
		t.Fatalf("%d GetLocalImage calls in flight at once across renders, want <= %d", result.MaxInFlight, limit)
	}
	if result.Calls == 0 {
		t.Fatal("harness made no GetLocalImage calls; it no longer exercises the pool")
	}
	if result.InFlight != 0 {
		t.Fatalf("%d reads never settled", result.InFlight)
	}
}
