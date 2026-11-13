package main

import (
	"os"

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
	// Skipped are paths that cannot be imported: missing, unreadable, a
	// folder, a device or pipe, or one of the app's own temp files (re-opening a dragged-out
	// clip would only duplicate it).
	Skipped []string `json:"skipped"`
	// Links are the mahpastes:// links to act on, parsed. A link only
	// navigates the window.
	Links []openrequest.Link `json:"links"`
	// UnsupportedLinks are mahpastes: URLs that are not valid links, as
	// received, to be reported.
	UnsupportedLinks []string `json:"unsupported_links"`
	// PasteClipboard asks the frontend to save the system clipboard as a
	// clip (the --paste-clipboard launch flag).
	PasteClipboard bool `json:"paste_clipboard"`
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

// submitRelaunchRequest takes a request a later launch sent over the instance
// socket. If the window does not exist yet, its activation waits for it.
func (d *App) submitRelaunchRequest(req openrequest.Request) {
	if d.openRequests == nil {
		return
	}
	d.openRequests.SubmitRelaunch(req)
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
	out := OpenRequests{Files: []string{}, Skipped: []string{}, Links: []openrequest.Link{}, UnsupportedLinks: []string{}}
	if d.openRequests == nil {
		return out, nil
	}
	req := d.openRequests.Take()
	out.PasteClipboard = req.PasteClipboard
	for _, raw := range req.URLs {
		link, err := openrequest.ParseLink(raw)
		if err != nil {
			out.UnsupportedLinks = append(out.UnsupportedLinks, raw)
			continue
		}
		out.Links = append(out.Links, link)
	}
	if len(req.Files) == 0 {
		return out, nil
	}
	for _, probe := range d.core.ProbeOpenedFiles(req.Files) {
		if probe.Exists && probe.IsRegular && !probe.IsTemp && readable(probe.Path) {
			out.Files = append(out.Files, probe.Path)
		} else {
			out.Skipped = append(out.Skipped, probe.Input)
		}
	}
	return out, nil
}

// readable reports whether path can be opened for reading. The frontend reads
// a batch of opened files before uploading any of them and gives up on the
// whole batch at the first failure, so a file without read permission in a
// Send To selection must be reported as skipped here, not fail the rest.
func readable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	f.Close()
	return true
}
