package webui

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// A batch of opened files is read into memory whole before it is uploaded, so
// it is bounded by bytes as well as by count, and a file too large to store is
// reported with its own message instead of being read at all.
func TestOpenRequestImportBatchesAreBoundedByBytes(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node,
		filepath.Join("testdata", "open_request_batches_harness.js"),
		filepath.Join("js", "open-requests.js"),
	).CombinedOutput()
	if err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
	type outcome struct {
		Uploads [][]string `json:"uploads"`
		Toasts  []string   `json:"toasts"`
	}
	var r map[string]outcome
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("bad harness output %q: %v", out, err)
	}
	sizes := func(o outcome) []int {
		n := make([]int, len(o.Uploads))
		for i, u := range o.Uploads {
			n[i] = len(u)
		}
		return n
	}
	for name, want := range map[string][]int{
		"small":    {32, 32, 6},
		"medium":   {2, 2, 1},
		"large":    {1, 1, 2},
		"noSizes":  {32, 1},
		"tooLarge": {1},
		"both":     {},
	} {
		if got := sizes(r[name]); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: batch sizes %v, want %v", name, got, want)
		}
	}
	if got, want := r["large"].Uploads, [][]string{{"/in/f0"}, {"/in/f1"}, {"/in/f2", "/in/f3"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("large: batches %v, want %v", got, want)
	}
	if got, want := r["tooLarge"].Toasts, []string{"Skipped 1 file too large to import (over 1 GB): big.mov"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tooLarge: toasts %q, want %q", got, want)
	}
	if got, want := r["both"].Toasts, []string{"Skipped 1 item that can't be imported: Folder. Skipped 2 files too large to import (over 1 GB): a.mov, b.mkv"}; !reflect.DeepEqual(got, want) {
		t.Errorf("both: toasts %q, want %q", got, want)
	}
}
