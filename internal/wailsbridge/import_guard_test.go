package wailsbridge_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoRuntimeImportOutsideBridge enforces that internal/wailsbridge is the
// only place in this module that imports github.com/wailsapp/wails/v2/pkg/runtime.
//
// This guardrail exists so the Wails v2 -> v3 migration swap stays surgical:
// if a future change adds a direct runtime import elsewhere, that code will
// need per-file rework on v3 day. Keep the abstraction intact.
//
// The walk covers frontend/*.go (now a real Go package: sse_broker.go, embed.go)
// — only the frontend's non-Go asset subdirectories are skipped — and ignores
// dot-directories (notably .claude/worktrees, which holds untracked secondary
// checkouts that would otherwise make this invariant depend on local state).
func TestNoRuntimeImportOutsideBridge(t *testing.T) {
	root := "../.."
	// Directories skipped wholesale, by base name or repo-relative path.
	skipDirs := map[string]bool{
		"node_modules":         true,
		"build":                true,
		"e2e":                  true,
		"internal/wailsbridge": true,
		// frontend asset subtrees (no reviewable Go; frontend/*.go IS walked).
		"frontend/js":      true,
		"frontend/css":     true,
		"frontend/dist":    true,
		"frontend/vendor":  true,
		"frontend/fonts":   true,
		"frontend/wailsjs": true,
	}

	const runtimeImport = `"github.com/wailsapp/wails/v2/pkg/runtime"`
	// Files that MUST be reachable by this walk, so a future re-skip of the
	// frontend Go package can't silently un-cover them.
	mustCover := map[string]bool{
		"frontend/sse_broker.go": false,
		"frontend/embed.go":      false,
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if info.IsDir() {
			// Skip dot-directories (.git, .claude, ...) except the walk root.
			if rel != "." && strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			if skipDirs[info.Name()] || skipDirs[rel] {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasPrefix(rel, "internal/wailsbridge") {
			return nil
		}
		if _, ok := mustCover[rel]; ok {
			mustCover[rel] = true
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), runtimeImport) {
			t.Errorf(
				"%s imports wails runtime directly — route through internal/wailsbridge so the v3 swap stays one-package.",
				rel,
			)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk failed: %v", err)
	}

	for file, covered := range mustCover {
		if !covered {
			t.Errorf("guard walk did not reach %s — the frontend Go package must stay covered", file)
		}
	}
}
