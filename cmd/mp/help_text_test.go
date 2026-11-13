package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The help and docs sent users to "Settings > API", which does not exist: the
// API server and its keys live in their own panel, opened from the menu
// drawer's API button.
func TestNothingPointsAtSettingsAPI(t *testing.T) {
	stale := regexp.MustCompile(`Settings\*{0,2}( \([^)]*\))?\s*(>|→)\s*\*{0,2}API|API button in settings`)
	if stale.MatchString(rootCmd.Long) {
		t.Error("mp --help points at Settings > API; the panel is menu drawer > API")
	}
	root := filepath.Join("..", "..")
	for _, dir := range []string{"cmd/mp", "docs/docs", "examples", "commands"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".go", ".md", ".mdx", ".sh", ".ps1", ".lua", ".html", ".js":
			default:
				return nil
			}
			if strings.HasSuffix(path, "help_text_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if m := stale.Find(raw); m != nil {
				t.Errorf("%s says %q; the API panel is menu drawer > API", path, m)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
