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
func TestNoRuntimeImportOutsideBridge(t *testing.T) {
	root := "../.."
	skipDirs := map[string]bool{
		".git":                        true,
		"node_modules":                true,
		"build":                       true,
		"frontend":                    true,
		"e2e":                         true,
		"internal/wailsbridge":        true,
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		if info.IsDir() {
			if skipDirs[info.Name()] || skipDirs[rel] {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.Contains(rel, "internal/wailsbridge") || strings.Contains(rel, "internal\\wailsbridge") {
			return nil
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), `"github.com/wailsapp/wails/v2/pkg/runtime"`) {
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
}
