package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

type imageWrite struct {
	kind       string // "png" or "flavors"
	original   []byte
	nativeType string
	png        []byte
}

// fakeImageWriters swaps the platform image writers for recorders. withFlavors
// selects whether the platform can hold several flavours (macOS).
func fakeImageWriters(t *testing.T, withFlavors bool) *[]imageWrite {
	t.Helper()
	var writes []imageWrite
	origPNG, origFlavors := pngImageWriter, nativeImageFlavorWriter
	t.Cleanup(func() { pngImageWriter, nativeImageFlavorWriter = origPNG, origFlavors })
	pngImageWriter = func(data []byte) error {
		writes = append(writes, imageWrite{kind: "png", png: data})
		return nil
	}
	nativeImageFlavorWriter = nil
	if withFlavors {
		nativeImageFlavorWriter = func(original []byte, nativeType string, pngData []byte) error {
			writes = append(writes, imageWrite{kind: "flavors", original: original, nativeType: nativeType, png: pngData})
			return nil
		}
	}
	return &writes
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func isPNG(data []byte) bool {
	_, err := png.Decode(bytes.NewReader(data))
	return err == nil
}

func TestPasteboardImageType(t *testing.T) {
	cases := map[string]string{
		"image/jpeg":  "public.jpeg",
		"IMAGE/JPEG":  "public.jpeg",
		"image/jpg":   "public.jpeg",
		"image/gif":   "com.compuserve.gif",
		"image/tiff":  "public.tiff",
		"image/png":   "",
		"image/webp":  "",
		"text/plain":  "",
		"image/x-bmp": "",
	}
	for contentType, want := range cases {
		if got := pasteboardImageType(contentType); got != want {
			t.Errorf("pasteboardImageType(%q) = %q, want %q", contentType, got, want)
		}
	}
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestCopyImageContentsPNGGoesStraightThrough(t *testing.T) {
	writes := fakeImageWriters(t, true)
	// Trailing bytes stand in for anything a re-encode would lose.
	data := append(testPNG(t), "trailing, never decoded"...)
	if err := copyImageContents(" Image/PNG ", data); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if len(*writes) != 1 || (*writes)[0].kind != "png" || !bytes.Equal((*writes)[0].png, data) {
		t.Fatalf("writes = %+v, want the PNG bytes unchanged", *writes)
	}
}

func TestCopyImageContentsRejectsEmptyOrBrokenPNG(t *testing.T) {
	for name, data := range map[string][]byte{"empty": nil, "garbage": []byte("not a png")} {
		t.Run(name, func(t *testing.T) {
			writes := fakeImageWriters(t, true)
			err := copyImageContents("image/png", data)
			if err == nil || !strings.Contains(err.Error(), "decode") {
				t.Fatalf("err = %v, want a decode error", err)
			}
			if len(*writes) != 0 {
				t.Fatalf("nothing may be written, got %+v", *writes)
			}
		})
	}
}

func TestCopyImageContentsConvertsMislabelledPNG(t *testing.T) {
	writes := fakeImageWriters(t, true)
	if err := copyImageContents("image/png", testJPEG(t)); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if len(*writes) != 1 || (*writes)[0].kind != "png" || !isPNG((*writes)[0].png) {
		t.Fatalf("writes = %+v, want the JPEG converted to a single PNG", *writes)
	}
}

func TestCopyImageContentsOffersOriginalAlongsidePNG(t *testing.T) {
	writes := fakeImageWriters(t, true)
	original := testJPEG(t)
	if err := copyImageContents("image/jpeg", original); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if len(*writes) != 1 {
		t.Fatalf("got %d writes, want one item", len(*writes))
	}
	w := (*writes)[0]
	if w.kind != "flavors" || w.nativeType != "public.jpeg" {
		t.Fatalf("write = %s/%s, want flavors/public.jpeg", w.kind, w.nativeType)
	}
	if !bytes.Equal(w.original, original) {
		t.Fatal("the original JPEG bytes must be published unchanged")
	}
	if !isPNG(w.png) {
		t.Fatal("a PNG copy must accompany the original")
	}
}

func TestCopyImageContentsWithoutFlavorWriterPublishesPNGOnly(t *testing.T) {
	writes := fakeImageWriters(t, false)
	if err := copyImageContents("image/jpeg", testJPEG(t)); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if len(*writes) != 1 || (*writes)[0].kind != "png" || !isPNG((*writes)[0].png) {
		t.Fatalf("writes = %+v, want a single PNG", *writes)
	}
}

func TestCopyImageContentsUndecodableKeepsNativeFlavor(t *testing.T) {
	// No TIFF decoder is registered: the original alone still pastes on a
	// clipboard that takes native flavours.
	writes := fakeImageWriters(t, true)
	tiff := []byte("II*\x00not really a tiff")
	if err := copyImageContents("image/tiff", tiff); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if len(*writes) != 1 {
		t.Fatalf("got %d writes, want 1", len(*writes))
	}
	w := (*writes)[0]
	if w.kind != "flavors" || w.nativeType != "public.tiff" || w.png != nil || !bytes.Equal(w.original, tiff) {
		t.Fatalf("write = %+v, want the TIFF alone", w)
	}
}

func TestCopyImageContentsUndecodableWithoutNativeTypeFails(t *testing.T) {
	writes := fakeImageWriters(t, true)
	err := copyImageContents("image/webp", []byte("garbage"))
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("err = %v, want a decode error", err)
	}
	if len(*writes) != 0 {
		t.Fatalf("nothing may be written, got %+v", *writes)
	}

	fakeImageWriters(t, false)
	if err := copyImageContents("image/tiff", []byte("garbage")); err == nil {
		t.Fatal("without native flavours an undecodable image must fail")
	}
}

func TestEncodeClipboardPNGKeepsPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var src bytes.Buffer
	if err := png.Encode(&src, img); err != nil {
		t.Fatal(err)
	}
	out, err := encodeClipboardPNG(src.Bytes())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r, _, _, _ := decoded.At(1, 1).RGBA(); r>>8 != 255 {
		t.Fatalf("pixel lost: r=%d", r>>8)
	}
}

func TestRichTextHTML(t *testing.T) {
	if got := richTextHTML([]byte("<b>é</b>")); got != `<meta charset="utf-8"><b>é</b>` {
		t.Fatalf("got %q, want a charset prepended", got)
	}
	withCharset := `<html><head><META http-equiv="Content-Type" content="text/html; charset=utf-8"></head><body>x</body></html>`
	if got := richTextHTML([]byte(withCharset)); got != withCharset {
		t.Fatalf("a document with a charset must be unchanged, got %q", got)
	}
	got := richTextHTML([]byte("<p>a\x00b\xff</p>"))
	if strings.ContainsRune(got, 0) || !strings.Contains(got, "ab�") {
		t.Fatalf("got %q, want NUL dropped and invalid UTF-8 replaced", got)
	}
}

func TestRichTextHTMLTranscodesDeclaredCharset(t *testing.T) {
	utf16 := []byte{0xFF, 0xFE}
	for _, r := range `<meta charset="utf-16"><p>café naïve</p>` {
		utf16 = append(utf16, byte(r), byte(r>>8))
	}
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"windows-1252 http-equiv",
			[]byte("<html><head><meta http-equiv=\"Content-Type\" content=\"text/html; charset=windows-1252\"></head><body><p>caf\xe9 na\xefve</p></body></html>"),
			`<meta charset="utf-8"><html><head></head><body><p>café naïve</p></body></html>`},
		{"iso-8859-1 meta charset",
			[]byte("<head><META CharSet=ISO-8859-1></head><p>caf\xe9 na\xefve</p>"),
			`<meta charset="utf-8"><head></head><p>café naïve</p>`},
		{"utf-8 bytes mislabelled",
			[]byte(`<head><meta charset="iso-8859-1"></head><p>café naïve</p>`),
			`<meta charset="utf-8"><head></head><p>café naïve</p>`},
		{"utf-16 with BOM", utf16, `<meta charset="utf-8"><p>café naïve</p>`},
		{"utf-8 BOM", []byte("\xEF\xBB\xBF<p>café naïve</p>"), `<meta charset="utf-8"><p>café naïve</p>`},
		{"conflicting declarations",
			[]byte(`<meta charset="utf-8"><meta charset="latin1"><p>café naïve</p>`),
			`<meta charset="utf-8"><p>café naïve</p>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := richTextHTML(tc.in)
			if got != tc.want {
				t.Fatalf("richTextHTML = %q, want %q", got, tc.want)
			}
			if plain := htmlPlainText(got); plain != "café naïve" {
				t.Fatalf("plain = %q, want the text intact", plain)
			}
		})
	}
}

func TestRichTextHTMLIgnoresCharsetMentions(t *testing.T) {
	// None of these declares a charset, so each must gain the declaration.
	for _, in := range []string{
		`<html><head><meta name="description" content="all about charset"></head><body><p>café 日本</p></body></html>`,
		`<!-- <meta x charset --><p>café 日本</p>`,
		`<meta property="og:title" content="Understanding charset=x declarations"><p>café 日本</p>`,
		`<script>var s = '<meta charset="utf-8">';</script><p>café 日本</p>`,
	} {
		if got := richTextHTML([]byte(in)); got != `<meta charset="utf-8">`+in {
			t.Errorf("richTextHTML(%q) = %q, want a charset prepended", in, got)
		}
	}
	real := `<html><head><meta name="description" content="charset"><meta charset="UTF-8"></head><body>é</body></html>`
	if got := richTextHTML([]byte(real)); got != real {
		t.Errorf("a document that declares utf-8 must be unchanged, got %q", got)
	}
}

func TestHTMLPlainText(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"inline", `<b>Hello</b>   <a href="x">world</a>`, "Hello world"},
		{"entities", `Fish &amp; chips &lt;3`, "Fish & chips <3"},
		{"paragraphs", `<p>One</p><p>Two</p>`, "One\n\nTwo"},
		{"divs and br", `<div>a</div><div>b<br>c</div>`, "a\nb\nc"},
		{"list", `<ul><li>x</li><li>y</li></ul>`, "x\ny"},
		{"table", `<table><tr><td>1</td><td>2</td></tr><tr><td>3</td><td>4</td></tr></table>`, "1\t2\n3\t4"},
		{"pre", "<p>code:</p><pre>a  b\n  c</pre>", "code:\n\na  b\n  c"},
		{"skipped", `<html><head><title>T</title><style>p{}</style></head><body><script>x()</script><p>Body</p></body></html>`, "Body"},
		{"missing head end", `<head><meta charset="utf-8"><body>Shown`, "Shown"},
		{"whitespace collapse", "  lots\n\n of\t space  ", "lots of space"},
		{"charset meta prefix", `<meta charset="utf-8"><span>Hi</span> there`, "Hi there"},
		{"self-closing script and style", `<p>a</p><script src="x.js"/>var s=1;</script><style/>p{}</style><p>b</p>`, "a\n\nb"},
		{"head ended by body content", `<html><head><title>T</title><p>Hello body</p>`, "Hello body"},
		{"style in unclosed head", `<head><meta charset="utf-8"><style>p{}</style><p>x</p>`, "x"},
		{"head only", `<head><title>T</title><script>x()</script>`, ""},
		{"body after script", `<script>x</script><body>y`, "y"},
		{"body tag in script text", `<script>var a = "<body>";</script><p>y</p>`, "y"},
		{"body tag in template", `<p>a</p><template><body>hidden</template><p>b</p>`, "a\n\nb"},
		{"nbsp-only element", `<p>a<span>&nbsp;</span>b</p>`, "a b"},
		{"trailing nbsp", `<p>a&nbsp;<b>b</b></p>`, "a b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := htmlPlainText(tc.in); got != tc.want {
				t.Fatalf("htmlPlainText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCopyClipAsRichText(t *testing.T) {
	tempDir := t.TempDir()
	db := newTransferServiceTestDB(t, tempDir)
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES
		(1, 'text/html', '<p>Hi <b>there</b></p>', 'a.html'),
		(2, 'text/plain', 'plain', 'b.txt')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	svc := NewClipboardService(newTransferServiceTestApp(t, db, tempDir))

	orig := richTextWriter
	t.Cleanup(func() { richTextWriter = orig })

	richTextWriter = nil
	if err := svc.CopyClipAsRichText(1); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("err = %v, want unsupported without a platform writer", err)
	}

	var gotHTML, gotPlain string
	richTextWriter = func(htmlDoc, plain string) error {
		gotHTML, gotPlain = htmlDoc, plain
		return nil
	}
	if err := svc.CopyClipAsRichText(1); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if gotHTML != `<meta charset="utf-8"><p>Hi <b>there</b></p>` {
		t.Fatalf("html = %q", gotHTML)
	}
	if gotPlain != "Hi there" {
		t.Fatalf("plain = %q, want the rendered text", gotPlain)
	}

	// A legacy-encoded clip reaches both flavours as the text it holds.
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (3, 'text/html', ?, 'c.html')`,
		[]byte("<meta charset=windows-1252><p>caf\xe9 na\xefve</p>")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := svc.CopyClipAsRichText(3); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if gotHTML != `<meta charset="utf-8"><p>café naïve</p>` || gotPlain != "café naïve" {
		t.Fatalf("html = %q, plain = %q, want the text transcoded", gotHTML, gotPlain)
	}

	if err := svc.CopyClipAsRichText(2); err == nil || !strings.Contains(err.Error(), "only HTML") {
		t.Fatalf("err = %v, want a refusal for a non-HTML clip", err)
	}
	if err := svc.CopyClipAsRichText(99); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want clip not found", err)
	}
}
