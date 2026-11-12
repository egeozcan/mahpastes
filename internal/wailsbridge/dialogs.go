package wailsbridge

import (
	"errors"
	goruntime "runtime"

	rt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ErrNoDialog is returned by ConfirmDialog when there is no runtime to show
// it with.
var ErrNoDialog = errors.New("no window to show a dialog in")

// ConfirmOptions is a native yes/no question.
type ConfirmOptions struct {
	Title   string
	Message string
	// ConfirmLabel and CancelLabel name the buttons on macOS. Windows and
	// Linux always show Yes and No, so word Message so that "Yes" means
	// ConfirmLabel.
	ConfirmLabel string
	CancelLabel  string
}

// ConfirmDialog asks a native yes/no question and blocks until it is
// answered. It returns true only for the confirm answer; dismissing the
// dialog any other way is a no. An error means the dialog could not be shown.
func (b *Bridge) ConfirmDialog(opts ConfirmOptions) (bool, error) {
	if b != nil && b.testConfirm != nil {
		return b.testConfirm(opts)
	}
	if !b.active() {
		return false, ErrNoDialog
	}
	res, err := rt.MessageDialog(b.ctx, confirmDialogOptions(opts, goruntime.GOOS))
	if err != nil {
		return false, err
	}
	return confirmed(res, opts), nil
}

// confirmDialogOptions builds the Wails options. On macOS the buttons must be
// given: with none, Wails reports "" whatever was clicked.
func confirmDialogOptions(opts ConfirmOptions, goos string) rt.MessageDialogOptions {
	d := rt.MessageDialogOptions{
		Type:    rt.QuestionDialog,
		Title:   opts.Title,
		Message: opts.Message,
	}
	if goos == "darwin" {
		d.Buttons = []string{opts.ConfirmLabel, opts.CancelLabel}
		d.DefaultButton = opts.CancelLabel
		d.CancelButton = opts.CancelLabel
	} else {
		// Windows' MessageBox and GTK's dialog show fixed Yes/No buttons.
		d.DefaultButton = "No"
	}
	return d
}

// confirmed maps a Wails dialog result to the confirm answer: the macOS
// button label, or "Yes" from the fixed Windows/Linux buttons.
func confirmed(result string, opts ConfirmOptions) bool {
	return result == "Yes" || (opts.ConfirmLabel != "" && result == opts.ConfirmLabel)
}

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
	if b != nil && b.testSaveFile != nil {
		return b.testSaveFile(opts)
	}
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
