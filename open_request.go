package main

import (
	"os"
	"path/filepath"
	"sync"

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
	// FileSizes are the sizes in bytes of Files, index for index, as measured
	// when the request was taken. The frontend reads a batch of files into
	// memory before uploading it and uses these to keep a batch small.
	FileSizes []int64 `json:"file_sizes"`
	// Skipped are paths that cannot be imported: missing, unreadable, a
	// folder, a device or pipe, or one of the app's own temp files (re-opening a dragged-out
	// clip would only duplicate it).
	Skipped []string `json:"skipped"`
	// TooLarge are regular files over maxOpenedFileBytes, which could never
	// be stored as a clip. They are reported separately from Skipped so the
	// user is told why.
	TooLarge []string `json:"too_large"`
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

// hideUnlessWindowAsked is the load-time hide of a macOS hidden start. It is
// skipped when a later launch or a file/URL open already asked for the
// window: that request was acknowledged, so the window must stay.
func (d *App) hideUnlessWindowAsked(hide func()) {
	if d.openRequests == nil {
		hide()
		return
	}
	d.openRequests.HideUnlessAsked(hide)
}

// launchFileEcho recognises this process's own file arguments coming back
// through macOS's application:openFile:. AppKit delivers the files of a
// command line that way too, and such a delivery is not somebody asking for
// the window: a hidden start with files (--hidden a.txt b.txt) must stay
// hidden. Each argument is expected back once.
type launchFileEcho struct {
	mu    sync.Mutex
	files []string
}

// newLaunchFileEcho watches for the files of launch, unless launch asks for
// the window itself: then an echo may activate like any other open.
func newLaunchFileEcho(launch openrequest.Request) *launchFileEcho {
	e := &launchFileEcho{}
	if !launch.Activate {
		e.files = append(e.files, launch.Files...)
	}
	return e
}

// request is the open request for a file macOS asked the app to open.
func (e *launchFileEcho) request(path string) openrequest.Request {
	return openrequest.Request{Files: []string{path}, Activate: !e.take(path)}
}

// take reports whether path is a launch argument not yet echoed, and forgets it.
func (e *launchFileEcho) take(path string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.files) == 0 {
		return false
	}
	clean := filepath.Clean(path)
	info, statErr := os.Stat(path)
	for i, f := range e.files {
		same := f == clean
		if !same && statErr == nil {
			// AppKit may spell the path differently (a resolved symlink).
			if fi, err := os.Stat(f); err == nil {
				same = os.SameFile(info, fi)
			}
		}
		if same {
			e.files = append(e.files[:i], e.files[i+1:]...)
			return true
		}
	}
	return false
}

// maxOpenedFileBytes is the largest file an open request imports. SQLite
// refuses a row of 1,000,000,000 bytes or more ("string or blob too big"), so
// a bigger file can never become a clip; the megabyte below that leaves room
// for the row's other columns. Without this check such a file would still be
// read whole, base64-encoded and sent through the WebView and back (several
// times its size in memory) only for the insert to fail. The macOS bundle is
// offered under Open With for every file, so a multi-gigabyte video is an
// ordinary input here. A variable so tests can lower it.
var maxOpenedFileBytes int64 = 1_000_000_000 - 1<<20

// TakeOpenRequests hands the frontend everything queued since the last call
// and empties the queue. Files are checked here, on the receiving side, with
// the same rules as a pasted path: only existing regular files outside the
// app's own temp directory are importable. Files too large to store are
// reported in TooLarge instead of being read.
func (d *App) TakeOpenRequests() (OpenRequests, error) {
	out := OpenRequests{Files: []string{}, FileSizes: []int64{}, Skipped: []string{}, TooLarge: []string{}, Links: []openrequest.Link{}, UnsupportedLinks: []string{}}
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
		switch {
		case !probe.Exists || !probe.IsRegular || probe.IsTemp || !readable(probe.Path):
			out.Skipped = append(out.Skipped, probe.Input)
		case probe.Size > maxOpenedFileBytes:
			out.TooLarge = append(out.TooLarge, probe.Input)
		default:
			out.Files = append(out.Files, probe.Path)
			out.FileSizes = append(out.FileSizes, probe.Size)
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
