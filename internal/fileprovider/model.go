// Package fileprovider implements the portable, read-only Finder projection.
// It has no dependency on Wails, cgo, or Apple's FileProvider framework.
package fileprovider

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"path"
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
	return s == "root" || s == "active" || s == "archive" || s == "working"
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
