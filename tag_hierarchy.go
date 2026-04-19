package main

import (
	"fmt"
	"strings"
)

// validateTagName enforces the same rules that App.CreateTag applies, so
// non-UI entry points (e.g. ShareManager.resolveOrCreateTag) can't quietly
// create malformed rows like "incoming/" or "_api/foo".
//
// Returns the trimmed name on success.
func validateTagName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("tag name cannot be empty")
	}
	if len(name) > maxTagNameLength {
		return "", fmt.Errorf("tag name too long (max %d characters)", maxTagNameLength)
	}
	for _, seg := range strings.Split(name, "/") {
		if strings.TrimSpace(seg) == "" {
			return "", fmt.Errorf("tag name contains empty path segment")
		}
		if seg == "_api" {
			return "", fmt.Errorf("tag name contains reserved segment '_api'")
		}
	}
	return name, nil
}

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

// checkTagReferencePreconditions returns a list of human-readable blocker
// strings that would prevent deleting tagID. Empty slice means safe.
// Currently blocks only on active follows (which have ON DELETE RESTRICT
// at the DB level); active share and running serve are handled by the
// post-commit runtime cleanup path and do not need to block here.
func (a *App) checkTagReferencePreconditions(tagID int64) ([]string, error) {
	var blockers []string

	var followCount int
	if err := a.db.QueryRow(
		`SELECT COUNT(*) FROM follows WHERE local_tag_id = ?`, tagID,
	).Scan(&followCount); err != nil {
		return nil, fmt.Errorf("count follows: %w", err)
	}
	if followCount > 0 {
		blockers = append(blockers, fmt.Sprintf(
			"tag has %d active incoming share (follow). Retarget the follow to a different tag, or stop it, then try again.",
			followCount,
		))
	}
	return blockers, nil
}
