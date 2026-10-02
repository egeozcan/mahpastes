package webui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Tag colors are free text in the database — written before validation
// existed, restored from backups, copied to new subtags — and the views build
// markup with template literals. A color interpolated raw into
// style="background-color: ${...}" closes the attribute and injects elements
// into a webview that exposes every bound Go method; escapeHTML only stops the
// breakout, not `red; background-image: url(...)`. safeTagColor is the one
// helper that admits nothing but a hex color or a bare keyword.
//
// This is a static guard because the e2e suite cannot plant a hostile color
// once the backend validates every write, so a missed sink would never render
// one under test.
func TestTagColorsReachMarkupOnlyThroughSafeTagColor(t *testing.T) {
	// Whole-file scan, so a ${...} split across lines is still one span.
	interpolation := regexp.MustCompile(`(?s)\$\{([^{}]*)\}`)
	colorAccess := regexp.MustCompile(`\.color\b|\[\s*['"]color['"]\s*\]`)
	safe := regexp.MustCompile(`(?s)^safeTagColor\((.*)\)$`)

	files, err := filepath.Glob(filepath.Join("js", "*.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{filepath.Join("js", "*", "*.js"), "*.html"} {
		more, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, more...)
	}
	if len(files) < 10 {
		t.Fatalf("found only %d frontend scripts — is the glob still pointing at frontend/js?", len(files))
	}

	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(src)
		for _, m := range interpolation.FindAllStringSubmatchIndex(text, -1) {
			expr := strings.TrimSpace(text[m[2]:m[3]])
			if !colorAccess.MatchString(expr) {
				continue
			}
			if inner := safe.FindStringSubmatch(expr); inner != nil && !strings.Contains(inner[1], ")") {
				continue
			}
			line := strings.Count(text[:m[0]], "\n") + 1
			t.Errorf("%s:%d interpolates %q into a template literal; wrap it in safeTagColor()", path, line, "${"+expr+"}")
		}
	}
}
