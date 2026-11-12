package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// Platform writers for clipboards that hold several flavours of one item.
// Each is nil where the platform has no such writer (clipboard_darwin.go sets
// them on macOS), and tests swap them for fakes so the real clipboard is never
// touched.
var (
	// nativeImageFlavorWriter publishes an image's original bytes under their
	// native pasteboard type, plus pngData when it is non-nil, as one item.
	nativeImageFlavorWriter func(original []byte, nativeType string, pngData []byte) error
	// richTextWriter publishes HTML together with a plain-text fallback.
	richTextWriter func(htmlDoc, plain string) error
	// pngImageWriter publishes PNG data on its own.
	pngImageWriter = copyImageToClipboard
)

// pasteboardImageType is the macOS pasteboard type (UTI) under which a clip's
// original image bytes are offered next to the PNG copy, or "" when the PNG
// alone is published. PNG clips need no second flavour.
func pasteboardImageType(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/jpeg", "image/jpg", "image/pjpeg":
		return "public.jpeg"
	case "image/gif":
		return "com.compuserve.gif"
	case "image/tiff", "image/tif":
		return "public.tiff"
	}
	return ""
}

// encodeClipboardPNG decodes any image format the app registers a decoder for
// and re-encodes it as PNG, the one image flavour every platform publishes.
func encodeClipboardPNG(data []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("failed to encode image as PNG: %w", err)
	}
	return buf.Bytes(), nil
}

// copyImageContents puts an image clip on the clipboard. Everywhere it
// publishes PNG; where the clipboard can hold several flavours (macOS) a JPEG,
// GIF or TIFF clip is offered in its original encoding too, so apps that read
// it get the original compression and animation. When the image cannot be
// converted, the original flavour alone is still published there.
func copyImageContents(contentType string, data []byte) error {
	if strings.EqualFold(contentType, "image/png") {
		return pngImageWriter(data)
	}
	pngData, convErr := encodeClipboardPNG(data)
	nativeType := ""
	if nativeImageFlavorWriter != nil && len(data) > 0 {
		nativeType = pasteboardImageType(contentType)
	}
	if nativeType == "" {
		if convErr != nil {
			return convErr
		}
		return pngImageWriter(pngData)
	}
	if convErr != nil {
		pngData = nil
	}
	return nativeImageFlavorWriter(data, nativeType, pngData)
}

var metaCharsetRE = regexp.MustCompile(`(?i)<meta[^>]+charset`)

// richTextHTML prepares an HTML clip for the pasteboard. Without a charset
// declaration Cocoa's HTML reader assumes a legacy encoding and garbles
// anything outside ASCII, so one is prepended (as WebKit does when it copies
// a selection). NUL bytes cannot cross the C boundary and are dropped, and
// invalid UTF-8 is replaced so the string converts.
func richTextHTML(data []byte) string {
	doc := strings.ToValidUTF8(strings.ReplaceAll(string(data), "\x00", ""), "�")
	if !metaCharsetRE.MatchString(doc) {
		doc = `<meta charset="utf-8">` + doc
	}
	return doc
}

// Elements whose text is not part of the rendered page.
var htmlSkippedElements = map[string]bool{
	"head": true, "title": true, "script": true, "style": true,
	"template": true, "noscript": true,
}

// Elements that start on a new line; those mapped to 2 are set off by a blank
// line, as a browser's innerText does.
var htmlBlockBreaks = map[string]int{
	"p": 2, "h1": 2, "h2": 2, "h3": 2, "h4": 2, "h5": 2, "h6": 2,
	"blockquote": 2, "pre": 2, "hr": 2,
	"div": 1, "li": 1, "ul": 1, "ol": 1, "dl": 1, "dt": 1, "dd": 1,
	"table": 1, "tr": 1, "section": 1, "article": 1, "header": 1,
	"footer": 1, "nav": 1, "aside": 1, "main": 1, "figure": 1,
	"figcaption": 1, "address": 1, "form": 1, "fieldset": 1,
	"details": 1, "summary": 1, "caption": 1,
}

// htmlPlainText renders an HTML document as plain text for the plain-string
// flavour that accompanies rich text: the visible text with whitespace
// collapsed, block elements on their own lines, table cells tab separated,
// and <pre> kept verbatim.
func htmlPlainText(src string) string {
	var b strings.Builder
	z := html.NewTokenizer(strings.NewReader(src))
	skip, pre := 0, 0
	pendingBreaks := 0
	pendingSpace := false
	atLineStart := true

	breakLines := func(n int) {
		if b.Len() > 0 && n > pendingBreaks {
			pendingBreaks = n
		}
		pendingSpace = false
	}
	flush := func() {
		if pendingBreaks > 0 {
			b.WriteString(strings.Repeat("\n", pendingBreaks))
			pendingBreaks = 0
			atLineStart = true
			pendingSpace = false
		}
	}
	writeWord := func(w string) {
		flush()
		if pendingSpace && !atLineStart {
			b.WriteByte(' ')
		}
		b.WriteString(w)
		pendingSpace = false
		atLineStart = false
	}

	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return strings.TrimSpace(b.String())
		case html.TextToken:
			if skip > 0 {
				continue
			}
			text := string(z.Text())
			if pre > 0 {
				flush()
				b.WriteString(text)
				atLineStart = strings.HasSuffix(text, "\n")
				pendingSpace = false
				continue
			}
			if text != "" && strings.TrimLeft(text, " \t\r\n\f") != text {
				pendingSpace = true
			}
			for i, w := range strings.Fields(text) {
				if i > 0 {
					pendingSpace = true
				}
				writeWord(w)
			}
			if text != "" && strings.TrimRight(text, " \t\r\n\f") != text {
				pendingSpace = true
			}
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			nameBytes, _ := z.TagName()
			name := string(nameBytes)
			start := tt != html.EndTagToken
			if name == "body" {
				// A missing </head> must not hide the whole body.
				skip = 0
				continue
			}
			if htmlSkippedElements[name] {
				if tt == html.StartTagToken {
					skip++
				} else if tt == html.EndTagToken && skip > 0 {
					skip--
				}
				continue
			}
			if skip > 0 {
				continue
			}
			switch {
			case name == "br" && start:
				flush()
				b.WriteByte('\n')
				atLineStart = true
				pendingSpace = false
			case (name == "td" || name == "th") && start:
				if !atLineStart && pendingBreaks == 0 {
					b.WriteByte('\t')
					atLineStart = true
					pendingSpace = false
				}
			case name == "pre":
				breakLines(2)
				if tt == html.StartTagToken {
					pre++
				} else if tt == html.EndTagToken && pre > 0 {
					pre--
				}
			case htmlBlockBreaks[name] > 0:
				breakLines(htmlBlockBreaks[name])
			}
		}
	}
}
