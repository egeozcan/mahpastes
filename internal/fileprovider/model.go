// Package fileprovider implements the portable, read-only Finder projection.
// It has no dependency on Wails, cgo, or Apple's FileProvider framework.
package fileprovider

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const PageSize = 200

var (
	ErrNoSuchItem = errors.New("no_such_item")
	ErrVersion    = errors.New("version_unavailable")
	ErrPage       = errors.New("page_expired")
	ErrAnchor     = errors.New("anchor_expired")
)

type Item struct {
	ID              string `json:"id"`
	Parent          string `json:"parent"`
	Name            string `json:"name"`
	MIME            string `json:"mime"`
	Size            int64  `json:"size"`
	Created         string `json:"created"`
	Modified        string `json:"modified"`
	ContentVersion  string `json:"contentVersion"`
	MetadataVersion string `json:"metadataVersion"`
	Folder          bool   `json:"folder"`
	Hidden          bool   `json:"hidden"`
}

type Page struct {
	Items   []Item   `json:"items"`
	Deleted []string `json:"deleted"`
	Next    string   `json:"next"`
	Anchor  string   `json:"anchor"`
	More    bool     `json:"more"`
}

type cursor struct {
	Epoch    string `json:"e"`
	Scope    string `json:"s"`
	Sequence int64  `json:"q"`
	High     int64  `json:"h,omitempty"`
	Snapshot string `json:"p,omitempty"`
	Offset   int64  `json:"o,omitempty"`
}

func token(c cursor) string { b, _ := json.Marshal(c); return base64.RawURLEncoding.EncodeToString(b) }
func parseToken(s string, failure error) (cursor, error) {
	var c cursor
	if len(s) > 2048 {
		return c, failure
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(b, &c) != nil || c.Epoch == "" || c.Sequence < 0 || c.Offset < 0 {
		return c, failure
	}
	return c, nil
}

func validScope(s string) bool {
	return s == "root" || s == "active" || s == "archive" || s == "tags" || s == "working" || strings.HasPrefix(s, "tag:")
}

func tagFolderID(epoch string, tagID int64) string {
	return "tag:" + epoch + ":" + strconv.FormatInt(tagID, 10)
}

func parseTagFolderID(id, epoch string) (int64, bool) {
	parts := strings.Split(id, ":")
	if len(parts) != 3 || parts[0] != "tag" || parts[1] != epoch {
		return 0, false
	}
	tagID, err := strconv.ParseInt(parts[2], 10, 64)
	return tagID, err == nil && tagID > 0
}

func taggedItemID(epoch, uuid string, tagID int64) string {
	return epoch + ":" + uuid + ":" + strconv.FormatInt(tagID, 10)
}

// parseFileID returns the source UUID and, for a tag projection, its exact
// tag. Canonical Active/Archive items have no tag ID.
func parseFileID(id, epoch string) (uuid string, tagID int64, tagged bool, ok bool) {
	parts := strings.Split(id, ":")
	if len(parts) != 2 && len(parts) != 3 || parts[0] != epoch || parts[1] == "" {
		return "", 0, false, false
	}
	if len(parts) == 2 {
		return parts[1], 0, false, true
	}
	tagID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || tagID <= 0 {
		return "", 0, false, false
	}
	return parts[1], tagID, true, true
}

func isCanonicalItemID(id string) bool {
	return strings.Count(id, ":") == 1
}

// Folder metadata needs a deterministic value which changes when Finder needs
// to repaint it (rename, move, or hidden state), without putting arbitrary tag
// names into the version's 128-byte File Provider limit.
func folderMetadataVersion(name, parent string, hidden bool) string {
	sum := sha256.Sum256([]byte(name + "\x00" + parent + "\x00" + strconv.FormatBool(hidden)))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func syntheticTagFolderID(epoch, path string) string {
	sum := sha256.Sum256([]byte(path))
	return "tag:synthetic:" + epoch + ":" + base64.RawURLEncoding.EncodeToString(sum[:])
}

func folderCollisionSuffix(id string) string {
	sum := sha256.Sum256([]byte(id))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:8]
}

// folderName adapts one tag-path segment to a legal, portable Finder folder
// name. Callers add a stable suffix when sibling names collide on a
// case-insensitive, Unicode-normalizing volume.
func folderName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, norm.NFC.String(name))
	name = strings.Trim(name, " .")
	if name == "" {
		name = "tag"
	}
	for len([]byte(name)) > 180 {
		r := []rune(name)
		name = string(r[:len(r)-1])
	}
	return name
}

// Always include the full stable UUID. This keeps names deterministic across
// case-insensitive volumes, Unicode normalization, deletion and re-enrollment,
// without renaming existing items when another clip gets a colliding name.
func Filename(original, mime, uuid string) string {
	name := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, norm.NFC.String(original))
	name = strings.Trim(name, " .")
	if name == "" {
		name = "paste"
	}
	ext := path.Ext(name)
	if len(ext) > 20 {
		ext = ""
	}
	stem := strings.TrimSuffix(name, ext)
	if ext == "" {
		switch strings.Split(mime, ";")[0] {
		case "text/plain":
			ext = ".txt"
		case "text/markdown":
			ext = ".md"
		case "text/html":
			ext = ".html"
		case "application/json":
			ext = ".json"
		case "image/png":
			ext = ".png"
		case "image/jpeg":
			ext = ".jpg"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		case "application/pdf":
			ext = ".pdf"
		default:
			ext = ".bin"
		}
	}
	// Leave room for UUID and extension within both UTF-8 and APFS limits.
	for len(stem) > 180 {
		r := []rune(stem)
		stem = string(r[:len(r)-1])
	}
	return stem + " [" + uuid + "]" + ext
}
