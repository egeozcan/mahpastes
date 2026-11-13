package webui

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The port field was read with `parseInt(value, 10) || 44557`: an empty or 0
// field saved 44557 and "1e3" saved port 1, while the field next to "Start on
// launch ... on the port above" kept showing what the user typed. Anything
// that is not a whole number in 1..65535 must be refused, never replaced.
func TestAPIPortFieldIsNeverSilentlyReplaced(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node,
		filepath.Join("testdata", "api_port_harness.js"),
		filepath.Join("js", "api-settings.js"),
	).CombinedOutput()
	if err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
	type outcome struct {
		Saves   [][]any  `json:"saves"`
		Starts  [][]any  `json:"starts"`
		Toasts  []string `json:"toasts"`
		Checked bool     `json:"checked"`
	}
	var r map[string]outcome
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("bad harness output %q: %v", out, err)
	}
	refused := func(name string, o outcome) {
		t.Helper()
		if len(o.Saves) != 0 || len(o.Starts) != 0 {
			t.Errorf("%s: saved %v started %v, want nothing", name, o.Saves, o.Starts)
		}
		if len(o.Toasts) != 1 || !strings.Contains(o.Toasts[0], "invalid port") {
			t.Errorf("%s: toasts %q, want one invalid port error", name, o.Toasts)
		}
	}
	for _, text := range []string{"0", "", "1e3", "80.5", "70000", "-5"} {
		on := r["switchOn:"+text]
		refused("switchOn:"+text, on)
		if on.Checked {
			t.Errorf("switchOn:%s: the switch stayed on after a refused save", text)
		}
		edit := r["editWhileOn:"+text]
		refused("editWhileOn:"+text, edit)
		if !edit.Checked {
			t.Errorf("editWhileOn:%s: a refused port edit switched Start on launch off", text)
		}
		refused("start:"+text, r["start:"+text])
	}

	if o := r["switchOn:45123"]; len(o.Saves) != 1 || o.Saves[0][0] != true || o.Saves[0][1] != float64(45123) || !o.Checked || len(o.Toasts) != 0 {
		t.Errorf("switchOn:45123 = %+v, want one save of (true, 45123)", o)
	}
	if o := r["editWhileOn:45123"]; len(o.Saves) != 1 || o.Saves[0][1] != float64(45123) {
		t.Errorf("editWhileOn:45123 = %+v, want the new port saved", o)
	}
	if o := r["start:45123"]; len(o.Starts) != 1 || o.Starts[0][0] != float64(45123) {
		t.Errorf("start:45123 = %+v, want the server started on 45123", o)
	}
	// Switching off stores no port, so a bad field must not block it.
	if o := r["switchOff:"]; len(o.Saves) != 1 || o.Saves[0][0] != false || o.Checked || len(o.Toasts) != 0 {
		t.Errorf("switchOff with an empty port = %+v, want it saved as off", o)
	}
}
