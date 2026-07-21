package cliptype

import (
	"path/filepath"
	"strings"
)

const MarkdownContentType = "text/markdown"

// IsMarkdownFilename reports whether name has a final Markdown file extension.
func IsMarkdownFilename(name string) bool {
	ext := filepath.Ext(name)
	return strings.EqualFold(ext, ".md") || strings.EqualFold(ext, ".markdown")
}

// PromoteMarkdown preserves the declared content type unless the filename
// identifies a Markdown clip.
func PromoteMarkdown(filename, contentType string) string {
	if IsMarkdownFilename(filename) {
		return MarkdownContentType
	}
	return contentType
}
