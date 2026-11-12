package wailsbridge

import "testing"

func TestToOpenRTMapsFields(t *testing.T) {
	in := FileDialogOptions{
		Title:            "Pick",
		DefaultDirectory: "/tmp",
		Filters:          []FileFilter{{DisplayName: "Images", Pattern: "*.png"}},
	}
	got := toOpenRT(in)
	if got.Title != "Pick" || got.DefaultDirectory != "/tmp" {
		t.Fatalf("basic fields not copied: %+v", got)
	}
	if len(got.Filters) != 1 || got.Filters[0].Pattern != "*.png" {
		t.Fatalf("filter not copied: %+v", got.Filters)
	}
}

func TestToSaveRTMapsFields(t *testing.T) {
	in := FileDialogOptions{
		Title:           "Save",
		DefaultFilename: "clip.zip",
		Filters:         []FileFilter{{DisplayName: "ZIP", Pattern: "*.zip"}},
	}
	got := toSaveRT(in)
	if got.Title != "Save" || got.DefaultFilename != "clip.zip" {
		t.Fatalf("basic fields not copied: %+v", got)
	}
	if len(got.Filters) != 1 || got.Filters[0].Pattern != "*.zip" {
		t.Fatalf("filter not copied: %+v", got.Filters)
	}
}

func TestToRTFiltersEmpty(t *testing.T) {
	if got := toRTFilters(nil); got != nil {
		t.Fatalf("nil input should produce nil, got %+v", got)
	}
	if got := toRTFilters([]FileFilter{}); got != nil {
		t.Fatalf("empty input should produce nil, got %+v", got)
	}
}

func TestBridgeNilSafety(t *testing.T) {
	// A nil *Bridge must not panic — mirrors the existing `a.ctx != nil`
	// guard at app.go:62 and plugin/api_task.go:77.
	var b *Bridge
	b.Emit("x")
	b.On("x", func(...any) {})
	b.Once("x", func(...any) {})
	b.Off("x")
	b.ShowWindow()
	NewForTesting().ShowWindow()

	path, err := b.OpenFile(FileDialogOptions{})
	if path != "" || err != nil {
		t.Fatalf("nil bridge OpenFile should return (\"\", nil), got (%q, %v)", path, err)
	}
	paths, err := b.OpenFiles(FileDialogOptions{})
	if paths != nil || err != nil {
		t.Fatalf("nil bridge OpenFiles should return (nil, nil), got (%v, %v)", paths, err)
	}
	path, err = b.SaveFile(FileDialogOptions{})
	if path != "" || err != nil {
		t.Fatalf("nil bridge SaveFile should return (\"\", nil), got (%q, %v)", path, err)
	}
	path, err = b.OpenDirectory(FileDialogOptions{})
	if path != "" || err != nil {
		t.Fatalf("nil bridge OpenDirectory should return (\"\", nil), got (%q, %v)", path, err)
	}

	if _, ok := b.WindowGeometry(); ok {
		t.Fatal("nil bridge WindowGeometry should report ok=false")
	}
	b.WindowSetPosition(1, 2)
	b.WindowSetSize(800, 600)
	if _, _, ok := b.CurrentScreenSize(); ok {
		t.Fatal("nil bridge CurrentScreenSize should report ok=false")
	}

	// A Bridge with nil ctx is equally a no-op.
	b = New(nil)
	if _, ok := b.WindowGeometry(); ok {
		t.Fatal("nil-ctx bridge WindowGeometry should report ok=false")
	}
	b.Emit("x")
	b.On("x", func(...any) {})
	if _, err := b.OpenFile(FileDialogOptions{}); err != nil {
		t.Fatalf("nil-ctx bridge OpenFile should return nil err, got %v", err)
	}
}

func TestShowWindowTestHook(t *testing.T) {
	b := NewForTesting()
	calls := 0
	b.SetTestShowWindow(func() { calls++ })
	b.ShowWindow()
	if calls != 1 {
		t.Fatalf("ShowWindow hook called %d times, want 1", calls)
	}
}

func TestConfirmDialogOptionsPerPlatform(t *testing.T) {
	opts := ConfirmOptions{Title: "T", Message: "M", ConfirmLabel: "Quit", CancelLabel: "Keep Editing"}

	mac := confirmDialogOptions(opts, "darwin")
	if len(mac.Buttons) != 2 || mac.Buttons[0] != "Quit" || mac.Buttons[1] != "Keep Editing" {
		t.Fatalf("macOS needs explicit buttons, got %v", mac.Buttons)
	}
	if mac.DefaultButton != "Keep Editing" || mac.CancelButton != "Keep Editing" {
		t.Fatalf("macOS default/cancel must be the safe answer: %+v", mac)
	}

	for _, goos := range []string{"linux", "windows"} {
		d := confirmDialogOptions(opts, goos)
		if len(d.Buttons) != 0 || d.DefaultButton != "No" {
			t.Fatalf("%s uses fixed Yes/No with No as default: %+v", goos, d)
		}
	}
}

func TestConfirmedResults(t *testing.T) {
	opts := ConfirmOptions{ConfirmLabel: "Quit", CancelLabel: "Keep Editing"}
	for res, want := range map[string]bool{
		"Yes": true, "Quit": true,
		"No": false, "Keep Editing": false, "Cancel": false, "": false, "OK": false,
	} {
		if got := confirmed(res, opts); got != want {
			t.Errorf("confirmed(%q) = %v, want %v", res, got, want)
		}
	}
}

func TestConfirmDialogWithoutRuntime(t *testing.T) {
	var nilBridge *Bridge
	if ok, err := nilBridge.ConfirmDialog(ConfirmOptions{}); ok || err == nil {
		t.Fatalf("a nil bridge must report that it cannot ask: %v, %v", ok, err)
	}
	b := NewForTesting()
	b.SetTestConfirm(func(ConfirmOptions) (bool, error) { return true, nil })
	if ok, err := b.ConfirmDialog(ConfirmOptions{}); !ok || err != nil {
		t.Fatalf("test stand-in not used: %v, %v", ok, err)
	}
	b.HideApp() // no runtime: must not panic
}
