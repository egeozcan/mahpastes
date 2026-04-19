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
		Title:            "Save",
		DefaultFilename:  "clip.zip",
		Filters:          []FileFilter{{DisplayName: "ZIP", Pattern: "*.zip"}},
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

	// A Bridge with nil ctx is equally a no-op.
	b = New(nil)
	b.Emit("x")
	b.On("x", func(...any) {})
	if _, err := b.OpenFile(FileDialogOptions{}); err != nil {
		t.Fatalf("nil-ctx bridge OpenFile should return nil err, got %v", err)
	}
}
