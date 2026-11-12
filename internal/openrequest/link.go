package openrequest

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// mahpastes:// links.
//
// A link only ever navigates the window: any web page, chat message or note
// can contain one, and the OS hands it to the app after at most a "open this
// app?" prompt. So no link imports, changes, deletes, copies or opens a clip
// in another application.
//
// Grammar (scheme and the first segment are case-insensitive; a trailing
// slash, a query and a fragment are ignored):
//
//	mahpastes://               bring the window forward
//	mahpastes://show           same
//	mahpastes://activate       same
//	mahpastes://clip/<id>      bring the window forward and show clip <id>
//
// The opaque form (mahpastes:clip/42) is accepted too. Anything else is
// reported as unsupported.

// Link kinds.
const (
	// LinkShow brings the window forward and does nothing else.
	LinkShow = "show"
	// LinkClip shows one clip.
	LinkClip = "clip"
)

// maxLinkClipID is the largest clip ID a link may name. The frontend handles
// IDs as JavaScript numbers, which are exact only up to 2^53-1.
const maxLinkClipID = 1<<53 - 1

// Link is a parsed mahpastes:// link.
type Link struct {
	// Kind is LinkShow or LinkClip.
	Kind string `json:"kind"`
	// ClipID is the clip a LinkClip names.
	ClipID int64 `json:"clip_id,omitempty"`
	// URL is the link as received, for messages.
	URL string `json:"url"`
}

// ErrUnsupportedLink is wrapped by every ParseLink error.
var ErrUnsupportedLink = errors.New("unsupported mahpastes link")

// ClipURL is the link that shows clip id.
func ClipURL(id int64) string {
	return URLScheme + "://" + LinkClip + "/" + strconv.FormatInt(id, 10)
}

// ParseLink parses a mahpastes:// link.
func ParseLink(raw string) (Link, error) {
	s, ok := ParseURL(raw)
	if !ok {
		return Link{}, fmt.Errorf("%w: not a %s: URL", ErrUnsupportedLink, URLScheme)
	}
	u, err := url.Parse(s)
	if err != nil {
		return Link{}, fmt.Errorf("%w: %v", ErrUnsupportedLink, err)
	}
	if u.User != nil || u.Port() != "" {
		return Link{}, fmt.Errorf("%w: unexpected user or port", ErrUnsupportedLink)
	}

	// mahpastes://clip/42 has host "clip" and path "/42"; mahpastes:clip/42
	// is opaque. Either way the segments are "clip", "42".
	var target string
	if u.Opaque != "" {
		target = u.Opaque
	} else {
		// The escaped path, so a percent-encoded digit is not a digit.
		target = u.Host + u.EscapedPath()
	}
	target = strings.TrimSuffix(target, "/")
	segments := strings.Split(target, "/")
	head := strings.ToLower(segments[0])

	switch {
	case len(segments) == 1 && (head == "" || head == "show" || head == "activate"):
		return Link{Kind: LinkShow, URL: s}, nil
	case len(segments) == 2 && head == LinkClip:
		id, err := strconv.ParseInt(segments[1], 10, 64)
		// ParseInt accepts a sign; a clip ID is plain digits.
		if err != nil || id < 1 || id > maxLinkClipID || segments[1][0] == '+' {
			return Link{}, fmt.Errorf("%w: invalid clip ID %q", ErrUnsupportedLink, segments[1])
		}
		return Link{Kind: LinkClip, ClipID: id, URL: s}, nil
	}
	return Link{}, fmt.Errorf("%w: %s", ErrUnsupportedLink, s)
}
