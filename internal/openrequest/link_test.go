package openrequest

import (
	"errors"
	"testing"
)

func TestParseLink(t *testing.T) {
	ok := []struct {
		raw  string
		want Link
	}{
		{"mahpastes://", Link{Kind: LinkShow, URL: "mahpastes://"}},
		{"mahpastes://show", Link{Kind: LinkShow, URL: "mahpastes://show"}},
		{"mahpastes://show/", Link{Kind: LinkShow, URL: "mahpastes://show/"}},
		{"MAHPASTES://Activate", Link{Kind: LinkShow, URL: "MAHPASTES://Activate"}},
		{"mahpastes:show", Link{Kind: LinkShow, URL: "mahpastes:show"}},
		{"mahpastes://clip/42", Link{Kind: LinkClip, ClipID: 42, URL: "mahpastes://clip/42"}},
		{"mahpastes://CLIP/42/", Link{Kind: LinkClip, ClipID: 42, URL: "mahpastes://CLIP/42/"}},
		{"mahpastes:clip/7", Link{Kind: LinkClip, ClipID: 7, URL: "mahpastes:clip/7"}},
		{"mahpastes://clip/9?from=notes#x", Link{Kind: LinkClip, ClipID: 9, URL: "mahpastes://clip/9?from=notes#x"}},
		{"  mahpastes://clip/3 ", Link{Kind: LinkClip, ClipID: 3, URL: "mahpastes://clip/3"}},
		{"mahpastes://clip/9007199254740991", Link{Kind: LinkClip, ClipID: 1<<53 - 1, URL: "mahpastes://clip/9007199254740991"}},
	}
	for _, tc := range ok {
		got, err := ParseLink(tc.raw)
		if err != nil {
			t.Errorf("ParseLink(%q) error: %v", tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseLink(%q) = %#v, want %#v", tc.raw, got, tc.want)
		}
	}

	bad := []string{
		"https://example.com/clip/1",
		"mahpastes://clip",
		"mahpastes://clip/",
		"mahpastes://clip/0",
		"mahpastes://clip/-1",
		"mahpastes://clip/+5",
		"mahpastes://clip/1.5",
		"mahpastes://clip/abc",
		"mahpastes://clip/9007199254740992",
		"mahpastes://clip/1/2",
		"mahpastes://clip/%31",
		"mahpastes://user@clip/1",
		"mahpastes://clip:80/1",
		"mahpastes://delete/1",
		"mahpastes://show/extra",
		"mahpastes://upload?path=/etc/passwd",
	}
	for _, raw := range bad {
		if got, err := ParseLink(raw); err == nil {
			t.Errorf("ParseLink(%q) = %#v, want an error", raw, got)
		} else if !errors.Is(err, ErrUnsupportedLink) {
			t.Errorf("ParseLink(%q) error %v does not wrap ErrUnsupportedLink", raw, err)
		}
	}
}

func TestClipURLRoundTrips(t *testing.T) {
	u := ClipURL(1234)
	if u != "mahpastes://clip/1234" {
		t.Fatalf("ClipURL = %q", u)
	}
	got, err := ParseLink(u)
	if err != nil || got.Kind != LinkClip || got.ClipID != 1234 {
		t.Fatalf("ParseLink(ClipURL) = %#v, %v", got, err)
	}
}
