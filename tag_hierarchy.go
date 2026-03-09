package main

import "strings"

// getTagDepth counts the number of "/" separators in a tag name.
// Top-level tags have depth 0, "a/b" has depth 1, "a/b/c" has depth 2, etc.
func getTagDepth(name string) int {
	return strings.Count(name, "/")
}

// getParentTagName returns the parent tag name by stripping the last segment.
// Returns "" for top-level tags (no "/" in the name).
func getParentTagName(name string) string {
	idx := strings.LastIndex(name, "/")
	if idx < 0 {
		return ""
	}
	return name[:idx]
}

// getShortTagName returns the leaf segment of a tag name (after the last "/").
// For top-level tags, returns the full name.
func getShortTagName(name string) string {
	idx := strings.LastIndex(name, "/")
	if idx < 0 {
		return name
	}
	return name[idx+1:]
}

// getAncestorTagNames returns all ancestor tag names from root to immediate parent.
// For "a/b/c" it returns ["a", "a/b"]. For top-level tags, returns nil.
func getAncestorTagNames(name string) []string {
	parts := strings.Split(name, "/")
	if len(parts) <= 1 {
		return nil
	}

	ancestors := make([]string, 0, len(parts)-1)
	for i := 1; i < len(parts); i++ {
		ancestors = append(ancestors, strings.Join(parts[:i], "/"))
	}
	return ancestors
}

// getRootTagName returns the root (top-level) segment of a tag name.
// For "a/b/c" it returns "a". For top-level tags, returns the full name.
func getRootTagName(name string) string {
	idx := strings.Index(name, "/")
	if idx < 0 {
		return name
	}
	return name[:idx]
}

// isDescendantOf returns true if child is a descendant of parent.
// A tag is NOT considered a descendant of itself.
func isDescendantOf(child, parent string) bool {
	return strings.HasPrefix(child, parent+"/")
}

// isImmediateChildOf returns true if child is a direct child of parent.
// When parent is "", checks if child is a top-level tag (no "/" in name).
func isImmediateChildOf(child, parent string) bool {
	if parent == "" {
		return !strings.Contains(child, "/")
	}
	if !strings.HasPrefix(child, parent+"/") {
		return false
	}
	rest := child[len(parent)+1:]
	return !strings.Contains(rest, "/")
}
