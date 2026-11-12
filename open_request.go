package main

import (
	"go-clipboard/internal/openrequest"
	"go-clipboard/internal/wailsbridge"
)

// openRequestEvent tells the frontend there are open requests to take. It
// carries no payload: the frontend calls TakeOpenRequests, so a request is
// handled by exactly one window even when several frontends are attached.
const openRequestEvent = "app:open-request"

// OpenRequests is what the frontend takes from the open-request queue.
type OpenRequests struct {
	// Files are regular files that may be imported as clips.
	Files []string `json:"files"`
	// Skipped are paths that cannot be imported: missing, a folder, a device
	// or pipe, or one of the app's own temp files (re-opening a dragged-out
	// clip would only duplicate it).
	Skipped []string `json:"skipped"`
	// URLs are mahpastes:// links to act on.
	URLs []string `json:"urls"`
}

// submitOpenRequest is the single entry point for everything the OS or a
// second launch asks this window to open: command-line files and URLs, a
// forwarded relaunch, and macOS Finder/Dock opens.
func (d *App) submitOpenRequest(req openrequest.Request) {
	if d.openRequests == nil {
		return
	}
	d.openRequests.Submit(req)
}

// attachOpenRequests connects the queue to the running window once Wails has
// started. Requests that arrived earlier are announced now.
func (d *App) attachOpenRequests(b *wailsbridge.Bridge) {
	if d.openRequests == nil {
		return
	}
	d.openRequests.Attach(b.ShowWindow, func() { b.Emit(openRequestEvent) })
}

// TakeOpenRequests hands the frontend everything queued since the last call
// and empties the queue. Files are checked here, on the receiving side, with
// the same rules as a pasted path: only existing regular files outside the
// app's own temp directory are importable.
func (d *App) TakeOpenRequests() (OpenRequests, error) {
	out := OpenRequests{Files: []string{}, Skipped: []string{}, URLs: []string{}}
	if d.openRequests == nil {
		return out, nil
	}
	req := d.openRequests.Take()
	if len(req.URLs) > 0 {
		out.URLs = req.URLs
	}
	if len(req.Files) == 0 {
		return out, nil
	}
	for _, probe := range d.core.ProbeOpenedFiles(req.Files) {
		if probe.Exists && probe.IsRegular && !probe.IsTemp {
			out.Files = append(out.Files, probe.Path)
		} else {
			out.Skipped = append(out.Skipped, probe.Input)
		}
	}
	return out, nil
}
