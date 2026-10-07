package webui

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

// Plugin event handlers run inside the App call that emitted the event: a
// clip:archived handler that archives a second clip has done so before
// ToggleArchive returns. An in-place patch only knows about the clip it was
// asked about, so a patch after such a call left the other clip's card on
// screen and the count wrong, where a reload would have shown both changes.
// Every patch path must reload when GetPluginLibraryWrites moved since the
// listing was read (or cannot be read), and still patch when it did not.
func TestGalleryPatchesReloadAfterPluginSideEffects(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node,
		filepath.Join("testdata", "patch_plugin_side_effects_harness.js"),
		filepath.Join("js", "wails-api.js"),
	).CombinedOutput()
	if err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
	type outcome struct {
		Loads   int   `json:"loads"`
		Removed int   `json:"removed"`
		Result  *bool `json:"result"`
	}
	var r map[string]outcome
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("bad harness output %q: %v", out, err)
	}
	reloaded := func(name string) {
		t.Helper()
		if o := r[name]; o.Loads != 1 || o.Removed != 0 {
			t.Errorf("%s: loads=%d removed=%d, want a reload and no in-place removal", name, o.Loads, o.Removed)
		}
	}
	patched := func(name string) {
		t.Helper()
		if o := r[name]; o.Loads != 0 || o.Removed != 1 {
			t.Errorf("%s: loads=%d removed=%d, want an in-place removal and no reload", name, o.Loads, o.Removed)
		}
	}
	reloaded("archivePlugin")
	reloaded("archiveUnknown")
	patched("archiveQuiet")
	reloaded("deletePlugin")
	patched("deleteQuiet")
	reloaded("renamePlugin")
	if o := r["renameQuiet"]; o.Loads != 0 {
		t.Errorf("renameQuiet: reloaded with no plugin side effect")
	}
	if o := r["refreshMoved"]; o.Result == nil || *o.Result {
		t.Errorf("refreshClipInPlace patched although a plugin changed the library since the listing")
	}
	if o := r["refreshQuiet"]; o.Result == nil || !*o.Result {
		t.Errorf("refreshClipInPlace refused to patch with no plugin side effect")
	}
}
