package webui

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A failed "Start on launch" (port taken at launch) was only logged: the API
// modal showed a plain "Stopped" with the switch on, the same as a server the
// user had stopped. The status line must carry the reason, as text.
func TestAPIModalShowsWhyStartOnLaunchFailed(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node,
		filepath.Join("testdata", "api_launch_error_harness.js"),
		filepath.Join("js", "api-settings.js"),
	).CombinedOutput()
	if err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
	type status struct {
		Text    string   `json:"text"`
		HTML    string   `json:"html"`
		Classes []string `json:"classes"`
	}
	var r map[string]status
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("bad harness output %q: %v", out, err)
	}
	warned := func(s status) bool { return slices.Contains(s.Classes, "text-amber-700") }

	failed := r["failed"]
	if !strings.HasPrefix(failed.Text, "Stopped") || !strings.Contains(failed.Text, "start on launch failed: <b>port 45871</b> is already in use") {
		t.Errorf("failed launch start: status %q, want Stopped plus the reason", failed.Text)
	}
	if !warned(failed) {
		t.Errorf("failed launch start: classes %v, want the amber warning colour", failed.Classes)
	}
	if failed.HTML != "" {
		t.Errorf("the reason was written as markup: %q", failed.HTML)
	}

	for _, name := range []string{"none", "serverMode", "unreadable", "cleared"} {
		if s := r[name]; s.Text != "Stopped" || warned(s) {
			t.Errorf("%s: status %q classes %v, want a plain Stopped", name, s.Text, s.Classes)
		}
	}
	if s := r["running"]; strings.Contains(s.Text+s.HTML, "failed") || warned(s) {
		t.Errorf("running server: status %q / %q classes %v, want no launch failure", s.Text, s.HTML, s.Classes)
	}
}
