package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
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
	if strings.EqualFold(strings.TrimSpace(contentType), "image/png") {
		// Published as stored (APNG frames and metadata survive), but only if
		// it is a PNG: an empty or mislabelled clip goes through the decode
		// path below, which converts it or reports the failure.
		if _, err := png.DecodeConfig(bytes.NewReader(data)); err == nil {
			return pngImageWriter(data)
		}
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

const utf8CharsetMeta = `<meta charset="utf-8">`

var contentCharsetRE = regexp.MustCompile(`(?i)charset\s*=\s*["']?([^"'\s;]*)`)

// charsetDecl is a <meta> tag that declares the document's encoding.
type charsetDecl struct {
	start, end int // byte range of the tag in the scanned string
	label      string
}

// htmlCharsetDecls finds the real charset declarations in doc: a <meta> with
// a charset attribute, or an http-equiv Content-Type whose content names one.
// The tokenizer keeps comments, script text and other attributes that merely
// mention "charset" out of it.
func htmlCharsetDecls(doc string) []charsetDecl {
	var decls []charsetDecl
	z := html.NewTokenizer(strings.NewReader(doc))
	pos := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return decls
		}
		start := pos
		pos += len(z.Raw())
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		name, hasAttr := z.TagName()
		if string(name) != "meta" {
			continue
		}
		var label, httpEquiv, content string
		hasCharset := false
		for hasAttr {
			var key, val []byte
			key, val, hasAttr = z.TagAttr()
			switch string(key) {
			case "charset":
				if !hasCharset {
					hasCharset, label = true, string(val)
				}
			case "http-equiv":
				httpEquiv = string(val)
			case "content":
				content = string(val)
			}
		}
		if !hasCharset && strings.EqualFold(strings.TrimSpace(httpEquiv), "content-type") {
			if m := contentCharsetRE.FindStringSubmatch(content); m != nil {
				hasCharset, label = true, m[1]
			}
		}
		if hasCharset {
			decls = append(decls, charsetDecl{start: start, end: pos, label: strings.TrimSpace(label)})
		}
	}
}

func isUTF8Label(label string) bool {
	_, name := charset.Lookup(label)
	return name == "utf-8"
}

// decodeHTMLClip returns an HTML clip's bytes as a UTF-8 string. A byte order
// mark decides; otherwise bytes that are valid UTF-8 are taken as UTF-8
// whatever the document claims, and only bytes that are not are decoded with
// the charset the document declares. NUL bytes are dropped once any UTF-16
// has been decoded.
func decodeHTMLClip(data []byte) string {
	bomName := ""
	switch {
	case bytes.HasPrefix(data, []byte("\xEF\xBB\xBF")):
		data = data[3:]
	case bytes.HasPrefix(data, []byte("\xFF\xFE")):
		bomName, data = "utf-16le", data[2:]
	case bytes.HasPrefix(data, []byte("\xFE\xFF")):
		bomName, data = "utf-16be", data[2:]
	}
	if bomName != "" {
		if enc, _ := charset.Lookup(bomName); enc != nil {
			if out, err := enc.NewDecoder().Bytes(data); err == nil {
				data = out
			}
		}
	}
	doc := strings.ReplaceAll(string(data), "\x00", "")
	if bomName != "" || utf8.ValidString(doc) {
		return doc
	}
	for _, d := range htmlCharsetDecls(doc) {
		enc, name := charset.Lookup(d.label)
		// A meta cannot truthfully declare UTF-16: it was read as ASCII.
		if enc == nil || name == "utf-8" || strings.HasPrefix(name, "utf-16") {
			continue
		}
		if out, err := enc.NewDecoder().String(doc); err == nil {
			return out
		}
		break
	}
	return doc
}

// richTextHTML prepares an HTML clip for the pasteboard, which takes it as
// UTF-8. The clip is transcoded from its own encoding, and its charset
// declarations must then say UTF-8: Cocoa's HTML reader decodes by the
// declaration, and without one assumes a legacy encoding, garbling anything
// outside ASCII. So unless the document already declares UTF-8 (and nothing
// else), its declarations are removed and one is prepended (as WebKit does
// when it copies a selection). NUL bytes cannot cross the C boundary and are
// dropped, and invalid UTF-8 is replaced so the string converts.
func richTextHTML(data []byte) string {
	doc := strings.ToValidUTF8(decodeHTMLClip(data), "\uFFFD")
	decls := htmlCharsetDecls(doc)
	truthful := len(decls) > 0
	for _, d := range decls {
		if !isUTF8Label(d.label) {
			truthful = false
		}
	}
	if truthful {
		return doc
	}
	var b strings.Builder
	b.WriteString(utf8CharsetMeta)
	last := 0
	for _, d := range decls {
		b.WriteString(doc[last:d.start])
		last = d.end
	}
	b.WriteString(doc[last:])
	return b.String()
}

// Elements whose text is not part of the rendered page.
var htmlSkippedElements = map[string]bool{
	"title": true, "script": true, "style": true,
	"template": true, "noscript": true,
}

// Elements that may sit in <head>; any other start tag ends the head, as it
// does in a browser.
var htmlHeadElements = map[string]bool{
	"html": true, "head": true, "title": true, "meta": true, "link": true,
	"style": true, "script": true, "noscript": true, "base": true,
	"template": true,
}

// htmlEdgeSpace reports whether text starts and ends with white space, by the
// definition strings.Fields splits on (which includes U+00A0).
func htmlEdgeSpace(text string) (leading, trailing bool) {
	first, _ := utf8.DecodeRuneInString(text)
	last, _ := utf8.DecodeLastRuneInString(text)
	return text != "" && unicode.IsSpace(first), text != "" && unicode.IsSpace(last)
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
	inHead := false
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
			if inHead {
				if strings.TrimSpace(text) == "" {
					continue
				}
				inHead = false
			}
			if pre > 0 {
				flush()
				b.WriteString(text)
				atLineStart = strings.HasSuffix(text, "\n")
				pendingSpace = false
				continue
			}
			leading, trailing := htmlEdgeSpace(text)
			if leading {
				pendingSpace = true
			}
			for i, w := range strings.Fields(text) {
				if i > 0 {
					pendingSpace = true
				}
				writeWord(w)
			}
			if trailing {
				pendingSpace = true
			}
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			nameBytes, _ := z.TagName()
			name := string(nameBytes)
			start := tt != html.EndTagToken
			if htmlSkippedElements[name] {
				// The "/" of a self-closing tag means nothing on these: a
				// <script/> still swallows text up to its end tag.
				if start {
					skip++
				} else if skip > 0 {
					skip--
				}
				continue
			}
			if skip > 0 {
				continue
			}
			if name == "head" {
				inHead = start
				continue
			}
			// A missing </head> must not hide the body: <body>, or any
			// element that cannot be in a head, ends it.
			if start && !htmlHeadElements[name] {
				inHead = false
			}
			if name == "body" {
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
