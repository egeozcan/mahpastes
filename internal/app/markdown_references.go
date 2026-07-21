package app

import (
	"database/sql"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// MarkdownReferenceCandidate is a clip that can satisfy a relative Markdown link.
type MarkdownReferenceCandidate struct {
	ClipID          int64    `json:"clip_id"`
	Filename        string   `json:"filename"`
	ContentType     string   `json:"content_type"`
	IsArchived      bool     `json:"is_archived"`
	MatchedTagPaths []string `json:"matched_tag_paths"`
}

// MarkdownReferenceResult reports deterministic resolution for one source path.
type MarkdownReferenceResult struct {
	Reference  string                       `json:"reference"`
	Path       string                       `json:"path"`
	Fragment   string                       `json:"fragment"`
	Status     string                       `json:"status"` // unique, missing, ambiguous, invalid
	Error      string                       `json:"error,omitempty"`
	Candidates []MarkdownReferenceCandidate `json:"candidates"`
}

type markdownReferencePath struct {
	path        string
	fragment    string
	directories []string
	filename    string
}

func parseMarkdownReference(raw string) (markdownReferencePath, error) {
	var parsed markdownReferencePath
	parsed.path = raw

	pathPart := raw
	if before, after, found := strings.Cut(pathPart, "#"); found {
		pathPart = before
		parsed.fragment = after
	}
	if strings.Contains(pathPart, "?") {
		return parsed, fmt.Errorf("query parameters are not supported")
	}
	decoded, err := url.PathUnescape(pathPart)
	if err != nil {
		return parsed, fmt.Errorf("invalid path encoding")
	}
	if strings.HasPrefix(decoded, "/") || strings.Contains(decoded, "\\") {
		return parsed, fmt.Errorf("absolute and filesystem paths are not supported")
	}
	for strings.HasPrefix(decoded, "./") {
		decoded = strings.TrimPrefix(decoded, "./")
	}
	segments := strings.Split(decoded, "/")
	if len(segments) == 0 || decoded == "" {
		return parsed, fmt.Errorf("reference path is empty")
	}
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return parsed, fmt.Errorf("invalid path segment %q", segment)
		}
		if strings.ContainsRune(segment, 0) {
			return parsed, fmt.Errorf("path contains a null byte")
		}
	}
	if strings.Contains(segments[0], ":") {
		return parsed, fmt.Errorf("URL schemes are not local references")
	}

	parsed.path = decoded
	parsed.filename = segments[len(segments)-1]
	parsed.directories = segments[:len(segments)-1]
	return parsed, nil
}

const (
	maxMarkdownReferencesPerRequest = 128
	maxMarkdownReferenceBases       = 16
	maxMarkdownReferenceCandidates  = 100
)

// ResolveMarkdownReferences resolves relative paths independently of the
// gallery/folder view from which the source clip was opened.
func (a *App) ResolveMarkdownReferences(sourceClipID int64, references []string) ([]MarkdownReferenceResult, error) {
	if len(references) > maxMarkdownReferencesPerRequest {
		return nil, fmt.Errorf("too many Markdown references: maximum %d", maxMarkdownReferencesPerRequest)
	}
	bases, err := a.markdownReferenceBases(sourceClipID)
	if err != nil {
		return nil, err
	}

	results := make([]MarkdownReferenceResult, 0, len(references))
	for _, raw := range references {
		parsed, parseErr := parseMarkdownReference(raw)
		result := MarkdownReferenceResult{
			Reference:  raw,
			Path:       parsed.path,
			Fragment:   parsed.fragment,
			Status:     "missing",
			Candidates: []MarkdownReferenceCandidate{},
		}
		if parseErr != nil {
			result.Status = "invalid"
			result.Error = parseErr.Error()
			results = append(results, result)
			continue
		}

		byID := map[int64]*MarkdownReferenceCandidate{}
		for _, base := range bases {
			tagPath := strings.Join(parsed.directories, "/")
			if base != "" && tagPath != "" {
				tagPath = base + "/" + tagPath
			} else if base != "" {
				tagPath = base
			}
			candidates, queryErr := a.findMarkdownReferenceCandidates(tagPath, parsed.filename)
			if queryErr != nil {
				return nil, queryErr
			}
			for _, candidate := range candidates {
				existing := byID[candidate.ClipID]
				if existing == nil {
					candidate.MatchedTagPaths = []string{tagPath}
					copy := candidate
					byID[candidate.ClipID] = &copy
					continue
				}
				if !containsString(existing.MatchedTagPaths, tagPath) {
					existing.MatchedTagPaths = append(existing.MatchedTagPaths, tagPath)
				}
			}
		}

		ids := make([]int64, 0, len(byID))
		for id := range byID {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		if len(ids) > maxMarkdownReferenceCandidates {
			ids = ids[:maxMarkdownReferenceCandidates]
		}
		for _, id := range ids {
			candidate := *byID[id]
			sort.Strings(candidate.MatchedTagPaths)
			result.Candidates = append(result.Candidates, candidate)
		}
		switch len(result.Candidates) {
		case 0:
			result.Status = "missing"
		case 1:
			result.Status = "unique"
		default:
			result.Status = "ambiguous"
		}
		results = append(results, result)
	}
	return results, nil
}

func (a *App) markdownReferenceBases(sourceClipID int64) ([]string, error) {
	var exists int
	if err := a.db.QueryRow(`SELECT 1 FROM clips WHERE id = ?`, sourceClipID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("source clip not found")
		}
		return nil, fmt.Errorf("query source clip: %w", err)
	}

	rows, err := a.db.Query(`
		SELECT t.name
		FROM tags t
		JOIN clip_tags ct ON ct.tag_id = t.id
		WHERE ct.clip_id = ?
		ORDER BY t.name COLLATE BINARY`, sourceClipID)
	if err != nil {
		return nil, fmt.Errorf("query source tags: %w", err)
	}
	defer rows.Close()

	var bases []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan source tag: %w", err)
		}
		bases = append(bases, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source tags: %w", err)
	}
	if len(bases) == 0 {
		bases = []string{""}
	}
	if len(bases) > maxMarkdownReferenceBases {
		return nil, fmt.Errorf("source clip has too many tag bases: maximum %d", maxMarkdownReferenceBases)
	}
	return bases, nil
}

func (a *App) findMarkdownReferenceCandidates(tagPath, filename string) ([]MarkdownReferenceCandidate, error) {
	var rows *sql.Rows
	var err error
	if tagPath == "" {
		rows, err = a.db.Query(`
			SELECT c.id, c.filename, c.content_type, c.is_archived
			FROM clips c
			WHERE c.filename = ? COLLATE BINARY
			  AND NOT EXISTS (SELECT 1 FROM clip_tags ct WHERE ct.clip_id = c.id)
			  AND (c.expires_at IS NULL OR datetime(c.expires_at) > CURRENT_TIMESTAMP)
			ORDER BY c.id
			LIMIT ?`, filename, maxMarkdownReferenceCandidates+1)
	} else {
		rows, err = a.db.Query(`
			SELECT c.id, c.filename, c.content_type, c.is_archived
			FROM clips c
			JOIN clip_tags ct ON ct.clip_id = c.id
			JOIN tags t ON t.id = ct.tag_id
			WHERE t.name = ? COLLATE BINARY
			  AND c.filename = ? COLLATE BINARY
			  AND (c.expires_at IS NULL OR datetime(c.expires_at) > CURRENT_TIMESTAMP)
			ORDER BY c.id
			LIMIT ?`, tagPath, filename, maxMarkdownReferenceCandidates+1)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve Markdown reference: %w", err)
	}
	defer rows.Close()

	var candidates []MarkdownReferenceCandidate
	for rows.Next() {
		var candidate MarkdownReferenceCandidate
		if err := rows.Scan(&candidate.ClipID, &candidate.Filename, &candidate.ContentType, &candidate.IsArchived); err != nil {
			return nil, fmt.Errorf("scan Markdown reference: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
