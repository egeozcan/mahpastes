package webui

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// jsFunctionBody returns the source of the named top-level function, found by
// matching braces from its opening one.
func jsFunctionBody(t *testing.T, src, name string) string {
	t.Helper()
	start := strings.Index(src, "function "+name+"(")
	if start < 0 {
		t.Fatalf("function %s not found", name)
	}
	open := strings.Index(src[start:], "{")
	depth := 0
	for i := start + open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : i+1]
			}
		}
	}
	t.Fatalf("function %s has no closing brace", name)
	return ""
}

// The card menu and the lightbox Actions menu are built by one function,
// buildMenuItemList, but dispatched by two switches. An item added to the
// list and to only one switch is a menu entry that silently does nothing
// (Copy > Link in the lightbox was one).
func TestEveryClipMenuActionIsHandledByCardAndLightbox(t *testing.T) {
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("js", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	ui, modals := read("ui.js"), read("modals.js")

	idRe := regexp.MustCompile(`\bid:\s*'([a-z0-9-]+)'`)
	// Dispatched before either switch (renderCardMenu / openLightboxFileMenu).
	preDispatched := map[string]bool{"plugin": true, "open-with-app": true}
	ids := map[string]bool{}
	for _, fn := range []string{"buildMenuItemList", "openWithAppMenuItems"} {
		for _, m := range idRe.FindAllStringSubmatch(jsFunctionBody(t, ui, fn), -1) {
			if !preDispatched[m[1]] {
				ids[m[1]] = true
			}
		}
	}
	if len(ids) < 10 || !ids["copy-link"] {
		t.Fatalf("found only %d menu action ids (%v) — did buildMenuItemList change shape?", len(ids), ids)
	}

	caseRe := regexp.MustCompile(`case\s+'([a-z0-9-]+)'\s*:`)
	handlers := map[string]string{
		"handleCardAction (ui.js)":             jsFunctionBody(t, ui, "handleCardAction"),
		"handleLightboxFileAction (modals.js)": jsFunctionBody(t, modals, "handleLightboxFileAction"),
	}
	for name, body := range handlers {
		handled := map[string]bool{}
		for _, m := range caseRe.FindAllStringSubmatch(body, -1) {
			handled[m[1]] = true
		}
		var missing []string
		for id := range ids {
			if !handled[id] {
				missing = append(missing, id)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s has no case for menu action(s) %v", name, missing)
		}
	}
}

// The macOS menu bar ran its items as keyboard shortcut actions, gated on the
// shortcut's context. Seven of the nine are 'gallery' actions, so from the
// Watch, Serve or Share view — and for every item while the nav drawer was
// open — an enabled menu item silently did nothing. Menu bar items must work
// from every main view, and still never act behind a dialog or a full-screen
// viewer.
func TestMenuBarItemsRunFromEveryMainView(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, filepath.Join("testdata", "menu_action_harness.js"), "js").Output()
	if err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
	type outcome struct {
		Ran          []string            `json:"ran"`
		Switched     map[string][]string `json:"switched"`
		DrawerClosed int                 `json:"drawerClosed"`
	}
	var r map[string]outcome
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("bad harness output %q: %v", out, err)
	}
	all := []string{"import-files", "import-folder", "settings", "archive", "folder-mode", "watch", "serve", "plugins", "share", "api", "maintenance", "shortcuts"}
	toGallery := map[string][]string{"import-files": {"clips"}, "import-folder": {"clips"}, "folder-mode": {"clips"}}

	for _, state := range []string{"gallery", "serve", "watch", "share", "drawer"} {
		o, ok := r[state]
		if !ok {
			t.Fatalf("harness reported no %q state", state)
		}
		if !reflect.DeepEqual(o.Ran, all) {
			t.Errorf("%s: menu items that ran = %v, want all of %v", state, o.Ran, all)
		}
		want := map[string][]string{}
		if state == "serve" || state == "watch" || state == "share" {
			// Imports and Folder Mode bring the gallery up first; Watch
			// Folders, Serve and Archive switch views in their own callbacks.
			want = toGallery
		}
		if !reflect.DeepEqual(o.Switched, want) {
			t.Errorf("%s: switchView calls = %v, want %v", state, o.Switched, want)
		}
	}
	if got := r["drawer"].DrawerClosed; got != len(all) {
		t.Errorf("drawer: closed %d times for %d menu clicks", got, len(all))
	}

	// Already in archive view and folder mode: from another view the two
	// items show the gallery as it is rather than toggling the mode off.
	modes := r["watchAlreadyInModes"]
	for _, id := range []string{"archive", "folder-mode"} {
		if !reflect.DeepEqual(modes.Switched[id], []string{"clips"}) {
			t.Errorf("%s with its mode already on: switchView calls = %v, want [clips]", id, modes.Switched[id])
		}
		for _, ran := range modes.Ran {
			if ran == id {
				t.Errorf("%s with its mode already on toggled it off from the Watch view", id)
			}
		}
	}

	for _, state := range []string{"dialog", "dialogOverDrawer", "lightbox", "editor", "comparison", "wizard"} {
		o, ok := r[state]
		if !ok {
			t.Fatalf("harness reported no %q state", state)
		}
		if len(o.Ran) != 0 || len(o.Switched) != 0 || o.DrawerClosed != 0 {
			t.Errorf("%s: a menu click acted behind it: ran %v, switched %v, drawer closed %d", state, o.Ran, o.Switched, o.DrawerClosed)
		}
	}
}
