package app

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Pasting a filesystem path is ambiguous: the text may be the clip the user
// wants, or it may be a reference to the file they want. Only the local disk
// can settle which reading is even possible, so the frontend hands the
// path-shaped lines it found in a paste to ProbeFilePaths and asks the user
// only when they resolve to real files.

// PathProbe reports what a single pasted path-like string resolves to on disk.
type PathProbe struct {
	Input  string `json:"input"` // candidate exactly as it was pasted
	Path   string `json:"path"`  // normalized absolute path ("" when unresolvable)
	Exists bool   `json:"exists"`
	IsDir  bool   `json:"is_dir"`
	// IsRegular gates import. Reading a FIFO blocks forever and reading a
	// character device (/dev/zero, /dev/urandom) never ends, so only ordinary
	// files may be offered — "not a directory" is not a strong enough test.
	IsRegular bool `json:"is_regular"`
	// IsTemp marks a file this app itself materialized — the paths handed out
	// by "Copy Path" and drag-out. Importing one would duplicate a clip that
	// already exists, under a throwaway lease filename.
	IsTemp bool   `json:"is_temp"`
	Name   string `json:"name"` // base name of Path
	Size   int64  `json:"size"`
}

// maxProbePaths bounds a single probe call. A paste with more lines than this
// is not a file reference anyone typed on purpose.
const maxProbePaths = 32

// ProbeFilePaths normalizes and stats each candidate. Unresolvable candidates
// come back with Exists false rather than an error, so one bad line in a
// multi-line paste does not discard the rest.
func (a *App) ProbeFilePaths(paths []string) ([]PathProbe, error) {
	if len(paths) > maxProbePaths {
		return nil, fmt.Errorf("too many paths: %d (max %d)", len(paths), maxProbePaths)
	}

	tempDir := a.TempDir()
	// The temp dir may sit under a symlinked root (/var → /private/var on
	// macOS), so keep a resolved copy to compare against paths that arrived in
	// the other spelling.
	resolvedTempDir := tempDir
	if evaluated, err := filepath.EvalSymlinks(tempDir); err == nil {
		resolvedTempDir = evaluated
	}

	probes := make([]PathProbe, 0, len(paths))
	for _, raw := range paths {
		probe := PathProbe{Input: raw}
		if resolved, ok := normalizePastedPath(raw); ok {
			probe.Path = resolved
			probe.Name = filepath.Base(resolved)
			if info, err := os.Stat(resolved); err == nil {
				probe.Exists = true
				probe.IsDir = info.IsDir()
				probe.IsRegular = info.Mode().IsRegular()
				probe.Size = info.Size()
				probe.IsTemp = isInsideDir(tempDir, resolved)
				if !probe.IsTemp {
					if evaluated, err := filepath.EvalSymlinks(resolved); err == nil {
						probe.IsTemp = isInsideDir(resolvedTempDir, evaluated)
					}
				}
			}
		}
		probes = append(probes, probe)
	}
	return probes, nil
}

// isInsideDir reports whether path is dir itself or lives beneath it. Both
// arguments must already be cleaned absolute paths.
func isInsideDir(dir, path string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// normalizePastedPath turns one pasted line into an absolute filesystem path.
// It accepts the forms clipboards actually produce — bare absolute paths,
// quoted paths, shell-escaped paths from a terminal drag, ~-relative paths and
// file:// URLs — and rejects everything else, including relative paths (a GUI
// app has no meaningful working directory to resolve them against).
func normalizePastedPath(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	s = stripSurroundingQuotes(s)

	if strings.HasPrefix(strings.ToLower(s), "file://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", false
		}
		// A host means the URL points at another machine; only the empty and
		// localhost hosts name this filesystem.
		if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
			return "", false
		}
		s = u.Path
		if runtime.GOOS == "windows" {
			// file:///C:/x parses to /C:/x — drop the leading slash so the
			// volume name is recognized.
			if len(s) > 2 && s[0] == '/' && s[2] == ':' {
				s = s[1:]
			}
		}
	} else if runtime.GOOS != "windows" {
		s = unescapeShellPath(s)
	}

	if strings.HasPrefix(s, "~") {
		expanded, ok := expandHome(s)
		if !ok {
			return "", false
		}
		s = expanded
	}

	if s == "" || strings.ContainsRune(s, 0) || !filepath.IsAbs(s) {
		return "", false
	}
	return filepath.Clean(s), true
}

// stripSurroundingQuotes removes one matching pair of wrapping quotes, the way
// a path copied out of a shell command or a "Copy as Path" menu arrives.
func stripSurroundingQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	first, last := s[0], s[len(s)-1]
	if first == last && (first == '"' || first == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// unescapeShellPath undoes the backslash escaping a terminal applies when a
// file is dragged into it. Terminals escape every shell metacharacter, not just
// spaces — "report (final).png" arrives as `report\ \(final\).png` — so a
// backslash before any space or ASCII punctuation is treated as an escape.
// A backslash before a letter or digit is left intact, since there it is far
// more likely to be a literal character in the filename than an escape.
//
// Getting this wrong is safe in one direction: an over- or under-unescaped path
// simply fails to stat, and the paste falls back to a text clip.
func unescapeShellPath(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && isShellEscapable(s[i+1]) {
			b.WriteByte(s[i+1])
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// isShellEscapable reports whether c is a byte a shell would escape with a
// backslash: a space or any ASCII punctuation.
func isShellEscapable(c byte) bool {
	if c == ' ' {
		return true
	}
	if c >= 0x80 || c <= ' ' {
		return false // control bytes and non-ASCII are never escapes
	}
	isAlnum := (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
	return !isAlnum
}

// expandHome resolves a leading ~ against the current user's home directory.
// ~otheruser is deliberately not expanded — resolving it needs a user database
// lookup that is not portable, and it is not a form clipboards produce.
func expandHome(s string) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false
	}
	if s == "~" {
		return home, true
	}
	if strings.HasPrefix(s, "~/") || (runtime.GOOS == "windows" && strings.HasPrefix(s, `~\`)) {
		return filepath.Join(home, s[2:]), true
	}
	return "", false
}
