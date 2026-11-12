// Package openrequest is the desktop app's single entry point for "the
// operating system or the user asked mahpastes to open something": files
// passed on the command line (Send To, a drop onto the exe or launcher, Open
// With), mahpastes:// URLs, a macOS Finder/Dock open, or a plain relaunch that
// should just bring the window forward.
//
// Every source produces a Request. A Request reaches the running window in one
// of two ways:
//
//   - In the process that owns the data directory, it goes straight into the
//     Queue, which buffers it until the frontend drains it.
//   - In a second process (the instance lock is already held), it is sent over
//     the instance socket (Send) to the process that owns the lock, whose
//     Server feeds it into that process's Queue. The second process then exits.
package openrequest

import (
	"net/url"
	"path/filepath"
	"strings"
)

// URLScheme is the custom URL scheme mahpastes answers to.
const URLScheme = "mahpastes"

// Request is one "open" action. An empty Request with Activate set is a plain
// "bring the window forward".
type Request struct {
	// Files are absolute paths of files to import as clips. They are not
	// trusted: the receiving side checks every one before importing it.
	Files []string `json:"files,omitempty"`
	// URLs are mahpastes:// URLs, passed through verbatim. They may navigate
	// the window, never import or change anything.
	URLs []string `json:"urls,omitempty"`
	// Activate asks for the window to be shown, unminimised and raised.
	Activate bool `json:"activate"`
	// PasteClipboard asks the window to save what is on the system clipboard
	// as a new clip, as if the user had pasted it. It comes only from the
	// PasteClipboardFlag on the command line (a desktop action bound to a
	// global shortcut), never from a URL: links must not change anything.
	PasteClipboard bool `json:"paste_clipboard,omitempty"`
}

// PasteClipboardFlag is the command-line flag that sets
// Request.PasteClipboard.
const PasteClipboardFlag = "--paste-clipboard"

// StartHiddenFlag starts the app without showing its window (the launch at
// login entry passes it). A launch with it never activates a running window:
// a login item that finds mahpastes already running must not pop it open.
const StartHiddenFlag = "--hidden"

// Empty reports whether the request carries nothing for the frontend to do.
// An empty request may still activate the window.
func (r Request) Empty() bool {
	return len(r.Files) == 0 && len(r.URLs) == 0 && !r.PasteClipboard
}

// FromArgs classifies command-line arguments (os.Args[1:]) into a Request that
// activates the window:
//
//   - Arguments that start with "-" are flags, never paths: Wails dev builds
//     take flags, and older macOS passes -psn_<serial>. PasteClipboardFlag
//     and StartHiddenFlag (which clears Activate) are the flags that mean
//     something here. A lone "--" ends flag skipping, so a file whose name
//     starts with "-" can still be passed after it.
//   - mahpastes: URLs are kept as URLs.
//   - file:// URLs (what desktop launchers pass for %U) become paths.
//   - Anything else is a path; relative paths are resolved against cwd, the
//     working directory of the process that received them.
//
// Nothing is checked against the filesystem here: the sender and the receiver
// are different processes, and the receiver decides what is importable.
func FromArgs(args []string, cwd string) Request {
	req := Request{Activate: true}
	flagsDone := false
	for _, arg := range args {
		if arg == "" {
			continue
		}
		if !flagsDone {
			if arg == "--" {
				flagsDone = true
				continue
			}
			if arg == PasteClipboardFlag {
				req.PasteClipboard = true
				continue
			}
			if arg == StartHiddenFlag {
				req.Activate = false
				continue
			}
			if strings.HasPrefix(arg, "-") {
				continue
			}
		}
		if u, ok := ParseURL(arg); ok {
			req.URLs = append(req.URLs, u)
			continue
		}
		if p, ok := fileURLPath(arg); ok {
			req.Files = append(req.Files, p)
			continue
		}
		if hasURLScheme(arg) {
			// Some other scheme (http:, ftp:): not ours to open.
			continue
		}
		p := arg
		if !filepath.IsAbs(p) {
			if cwd == "" {
				continue
			}
			p = filepath.Join(cwd, p)
		}
		req.Files = append(req.Files, filepath.Clean(p))
	}
	return req
}

// StripOwnFlags returns args (a full os.Args, program name first) without the
// flags FromArgs consumes. Wails dev and bindings builds parse os.Args with the
// flag package and fail on a flag they do not know, so the app removes its own
// before wails.Run. Arguments after "--" are left alone.
func StripOwnFlags(args []string) []string {
	out := make([]string, 0, len(args))
	for i, arg := range args {
		if arg == "--" {
			return append(out, args[i:]...)
		}
		if i > 0 && (arg == PasteClipboardFlag || arg == StartHiddenFlag) {
			continue
		}
		out = append(out, arg)
	}
	return out
}

// WantsStartHidden reports whether args (os.Args[1:]) carry StartHiddenFlag
// before any "--".
func WantsStartHidden(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == StartHiddenFlag {
			return true
		}
	}
	return false
}

// ParseURL reports whether raw is a mahpastes: URL and returns it trimmed.
// The rest of the URL is not interpreted here.
func ParseURL(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	scheme, _, ok := strings.Cut(s, ":")
	if !ok || !strings.EqualFold(scheme, URLScheme) {
		return "", false
	}
	if _, err := url.Parse(s); err != nil {
		return "", false
	}
	return s, true
}

// fileURLPath turns a local file:// URL into a filesystem path.
func fileURLPath(raw string) (string, bool) {
	if len(raw) < len("file:") || !strings.EqualFold(raw[:len("file:")], "file:") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	// A host names another machine; only the empty and localhost hosts name
	// this filesystem.
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		return "", false
	}
	p := u.Path
	// file:///C:/x parses to /C:/x; drop the slash so the volume is kept.
	if len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	p = filepath.FromSlash(p)
	if p == "" || !filepath.IsAbs(p) {
		return "", false
	}
	return filepath.Clean(p), true
}

// hasURLScheme reports whether s starts with "<scheme>://" for a scheme of two
// or more characters ("http://x"). A single letter followed by ":" is a
// Windows drive ("C:\x"), and a colon without "//" can be part of a Unix file
// name ("notes:2024.txt"), so neither counts.
func hasURLScheme(s string) bool {
	scheme, rest, ok := strings.Cut(s, ":")
	if !ok || len(scheme) < 2 || !strings.HasPrefix(rest, "//") {
		return false
	}
	for i, c := range scheme {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
		default:
			return false
		}
	}
	return true
}
