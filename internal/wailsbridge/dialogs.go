package wailsbridge

import rt "github.com/wailsapp/wails/v2/pkg/runtime"

// FileFilter mirrors runtime.FileFilter to keep the wails runtime import local.
type FileFilter struct {
	DisplayName string
	Pattern     string
}

// FileDialogOptions is the superset used for Open/OpenMultiple/Save/OpenDirectory.
// Map one-to-one to runtime.OpenDialogOptions / SaveDialogOptions. We use the
// superset to keep call sites from juggling two types.
type FileDialogOptions struct {
	Title                      string
	DefaultDirectory           string
	DefaultFilename            string
	CanCreateDirectories       bool
	ResolvesAliases            bool
	ShowHiddenFiles            bool
	TreatPackagesAsDirectories bool
	Filters                    []FileFilter
}

// OpenFile shows the native "select a file" dialog. Returns "" if cancelled.
func (b *Bridge) OpenFile(opts FileDialogOptions) (string, error) {
	if !b.active() {
		return "", nil
	}
	return rt.OpenFileDialog(b.ctx, toOpenRT(opts))
}

// OpenFiles shows the native "select multiple files" dialog. Returns an empty
// slice if cancelled.
func (b *Bridge) OpenFiles(opts FileDialogOptions) ([]string, error) {
	if !b.active() {
		return nil, nil
	}
	return rt.OpenMultipleFilesDialog(b.ctx, toOpenRT(opts))
}

// SaveFile shows the native "save as" dialog. Returns "" if cancelled.
func (b *Bridge) SaveFile(opts FileDialogOptions) (string, error) {
	if !b.active() {
		return "", nil
	}
	return rt.SaveFileDialog(b.ctx, toSaveRT(opts))
}

// OpenDirectory shows the native "select a folder" dialog. Returns "" if cancelled.
func (b *Bridge) OpenDirectory(opts FileDialogOptions) (string, error) {
	if !b.active() {
		return "", nil
	}
	return rt.OpenDirectoryDialog(b.ctx, toOpenRT(opts))
}

func toOpenRT(o FileDialogOptions) rt.OpenDialogOptions {
	return rt.OpenDialogOptions{
		Title:                      o.Title,
		DefaultDirectory:           o.DefaultDirectory,
		DefaultFilename:            o.DefaultFilename,
		CanCreateDirectories:       o.CanCreateDirectories,
		ResolvesAliases:            o.ResolvesAliases,
		ShowHiddenFiles:            o.ShowHiddenFiles,
		TreatPackagesAsDirectories: o.TreatPackagesAsDirectories,
		Filters:                    toRTFilters(o.Filters),
	}
}

func toSaveRT(o FileDialogOptions) rt.SaveDialogOptions {
	return rt.SaveDialogOptions{
		Title:                      o.Title,
		DefaultDirectory:           o.DefaultDirectory,
		DefaultFilename:            o.DefaultFilename,
		CanCreateDirectories:       o.CanCreateDirectories,
		TreatPackagesAsDirectories: o.TreatPackagesAsDirectories,
		Filters:                    toRTFilters(o.Filters),
	}
}

func toRTFilters(in []FileFilter) []rt.FileFilter {
	if len(in) == 0 {
		return nil
	}
	out := make([]rt.FileFilter, len(in))
	for i, f := range in {
		out[i] = rt.FileFilter{DisplayName: f.DisplayName, Pattern: f.Pattern}
	}
	return out
}
