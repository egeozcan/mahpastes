package app

import (
	"errors"
	"path/filepath"
)

// errProjectionMount is returned when a watch folder or import would read the
// app's own file manager mount.
var errProjectionMount = errors.New("this folder is Mahpastes' own clips folder; choose a different folder")

// SetProjectionMount records where the read-only file manager mount of the
// clips is ("" when nothing is mounted). Watch folders and imports refuse
// paths inside it: they would re-import the app's own clips, and a watch on
// it would feed on the change events the mount raises for each new clip.
func (a *App) SetProjectionMount(dir string) {
	var roots []string
	if dir != "" {
		roots = append(roots, filepath.Clean(dir))
		if resolved, err := filepath.EvalSymlinks(dir); err == nil && resolved != roots[0] {
			roots = append(roots, resolved)
		}
	}
	a.projectionMount.Store(&roots)
}

// insideProjectionMount reports whether path is the mount or lies inside it,
// comparing both the given and the symlink-resolved form.
func (a *App) insideProjectionMount(path string) bool {
	roots := a.projectionMount.Load()
	if roots == nil || len(*roots) == 0 || path == "" {
		return false
	}
	candidates := []string{path}
	if abs, err := filepath.Abs(path); err == nil {
		candidates[0] = abs
	}
	if resolved, err := filepath.EvalSymlinks(candidates[0]); err == nil {
		candidates = append(candidates, resolved)
	}
	for _, root := range *roots {
		for _, c := range candidates {
			if isInsideDir(root, c) {
				return true
			}
		}
	}
	return false
}
