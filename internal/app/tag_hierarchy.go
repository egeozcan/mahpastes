package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
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
	if err := checkTagNameText(name); err != nil {
		return "", err
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

// checkTagNameText refuses names that are not clean text: invalid UTF-8, or
// control characters (NUL above all — SQLite's length and substr stop at it,
// so such a name lost its subtree to every subtree query, and invalid UTF-8
// made the cascade rename's character offsets disagree and garble children).
func checkTagNameText(name string) error {
	if !utf8.ValidString(name) {
		return fmt.Errorf("tag name is not valid UTF-8")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("tag name contains a control character")
		}
	}
	return nil
}

// servedInSubtree returns the first served tag at or under name, from a
// snapshot of ServeManager.GetStatus, or "".
func servedInSubtree(served []ServeInfo, name string) string {
	prefix := name + "/"
	for _, info := range served {
		if info.TagName == name || strings.HasPrefix(info.TagName, prefix) {
			return info.TagName
		}
	}
	return ""
}

// underTagSQL is a SQL condition that col names a tag strictly under a tag
// path; bind that path to both of its placeholders. It replaces
// `col LIKE path || '/%'`, which SQLite evaluates with ASCII case folded and
// with _ and % in the path as wildcards: a rename, merge, filter or key scope
// on "my_tag" also reached "my-tag/…", "myXtag/…" and "MY_TAG/…". substr and
// length both count characters for text, so the comparison is exact.
func underTagSQL(col string) string {
	return "substr(" + col + ", 1, length(?) + 1) = ? || '/'"
}

// underTagColSQL is underTagSQL with the parent path taken from another
// column rather than a parameter.
func underTagColSQL(col, parentCol string) string {
	return "substr(" + col + ", 1, length(" + parentCol + ") + 1) = " + parentCol + " || '/'"
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

// checkMergeTagPreconditions returns the list of human-readable blockers for
// merging source into destination. Empty slice means proceed.
func (a *App) checkMergeTagPreconditions(sourceID, destID int64, srcName, dstName string) []string {
	if sourceID == destID {
		return []string{"source and destination must be different tags"}
	}
	var blockers []string
	// Destination must not be source or a descendant of source.
	if dstName == srcName || strings.HasPrefix(dstName, srcName+"/") {
		blockers = append(blockers, fmt.Sprintf("destination %q is a descendant of source %q", dstName, srcName))
	}
	// Block on active share (shares row for source).
	var shareCount int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE tag_id = ?`, sourceID).Scan(&shareCount); err == nil && shareCount > 0 {
		blockers = append(blockers, "source tag is actively shared. Stop the share first.")
	}
	// Block on follow.
	var followCount int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM follows WHERE local_tag_id = ?`, sourceID).Scan(&followCount); err == nil && followCount > 0 {
		blockers = append(blockers, "source tag has an incoming share (follow). Retarget or stop it first.")
	}
	// Block on any served tag in source subtree.
	if served := a.tagIsServedInSubtree(srcName); served != "" {
		blockers = append(blockers, fmt.Sprintf("tag %q in source subtree is currently served. Stop the server first.", served))
	}
	// Block on descendant collision with destination subtree.
	rows, err := a.db.Query(`SELECT name FROM tags WHERE `+underTagSQL("name"), srcName, srcName)
	if err == nil {
		defer rows.Close()
		srcPrefix := srcName + "/"
		dstPrefix := dstName + "/"
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				continue
			}
			projected := dstPrefix + strings.TrimPrefix(n, srcPrefix)
			var exists int
			if err := a.db.QueryRow(`SELECT COUNT(*) FROM tags WHERE name = ?`, projected).Scan(&exists); err == nil && exists > 0 {
				blockers = append(blockers, fmt.Sprintf("merge would collide at %q (tag already exists)", projected))
				break
			}
		}
	}
	return blockers
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
		// Missing table means no follows; tolerate minimal test schemas.
		if !isSQLiteNoSuchTable(err) {
			return nil, fmt.Errorf("count follows: %w", err)
		}
	}
	if followCount > 0 {
		blockers = append(blockers, fmt.Sprintf(
			"tag has %d active incoming share (follow). Retarget the follow to a different tag, or stop it, then try again.",
			followCount,
		))
	}
	return blockers, nil
}

// tagIsServedInSubtree returns the name of any tag in the subtree rooted at
// oldName that is currently being served, or "" if none. ServeManager caches
// tag names at start, so renames/merges of a served subtree leave the server
// resolving against stale prefixes.
func (a *App) tagIsServedInSubtree(oldName string) string {
	if a.serveManager == nil {
		return ""
	}
	return servedInSubtree(a.serveManager.GetStatus(), oldName)
}

// getHiddenTagsTx reads and parses the hidden_tags setting inside the given
// transaction so read+write participate in the same snapshot. The
// non-tx helpers a.GetHiddenTags/a.SetHiddenTags use a.db directly and
// would escape the caller's tx.
func getHiddenTagsTx(tx *sql.Tx) ([]int64, error) {
	var value string
	err := tx.QueryRow(`SELECT value FROM settings WHERE key = 'hidden_tags'`).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		// Missing table means no settings; tolerate minimal test schemas.
		if isSQLiteNoSuchTable(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read hidden_tags: %w", err)
	}
	if value == "" {
		return nil, nil
	}
	var ids []int64
	if err := json.Unmarshal([]byte(value), &ids); err != nil {
		return nil, fmt.Errorf("parse hidden_tags: %w", err)
	}
	return ids, nil
}

// setHiddenTagsTx writes the hidden_tags setting inside the given transaction.
func setHiddenTagsTx(tx *sql.Tx, ids []int64) error {
	if ids == nil {
		ids = []int64{}
	}
	payload, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("marshal hidden_tags: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES ('hidden_tags', ?)
        ON CONFLICT(key) DO UPDATE SET value = excluded.value`, string(payload)); err != nil {
		return fmt.Errorf("write hidden_tags: %w", err)
	}
	return nil
}

// migrateTagReferences moves all non-networked ID-keyed references from
// source to destination inside an existing transaction. Networked state
// (shares, follows, serves) is refused at the precondition phase and is
// never touched here.
func (a *App) migrateTagReferences(tx *sql.Tx, fromID, toID int64) error {
	// API keys — migrate scope (preserves user intent).
	if _, err := tx.Exec(`UPDATE api_keys SET scoped_tag_id = ? WHERE scoped_tag_id = ?`, toID, fromID); err != nil {
		return fmt.Errorf("migrate api_keys: %w", err)
	}
	// Watched folders — migrate auto-tag target.
	if _, err := tx.Exec(`UPDATE watched_folders SET auto_tag_id = ? WHERE auto_tag_id = ?`, toID, fromID); err != nil {
		return fmt.Errorf("migrate watched_folders: %w", err)
	}
	// Hidden tag list — swap membership. Uses tx-aware helpers (getHiddenTagsTx,
	// setHiddenTagsTx) so the read+write participate in the caller's snapshot.
	hiddenIDs, err := getHiddenTagsTx(tx)
	if err != nil {
		return fmt.Errorf("get hidden: %w", err)
	}
	newHidden := make([]int64, 0, len(hiddenIDs))
	sawSrc := false
	sawDst := false
	for _, id := range hiddenIDs {
		if id == fromID {
			sawSrc = true
			continue
		}
		if id == toID {
			sawDst = true
		}
		newHidden = append(newHidden, id)
	}
	if sawSrc && !sawDst {
		newHidden = append(newHidden, toID)
	}
	if sawSrc {
		if err := setHiddenTagsTx(tx, newHidden); err != nil {
			return fmt.Errorf("update hidden_tags: %w", err)
		}
	}
	return nil
}
