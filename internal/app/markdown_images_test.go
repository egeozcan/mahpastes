package app

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"
)

func encodeTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return buf.Bytes()
}

func makePNGChunk(chunkType string, data []byte) []byte {
	chunk := make([]byte, 12+len(data))
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(data)))
	copy(chunk[4:8], chunkType)
	copy(chunk[8:8+len(data)], data)
	binary.BigEndian.PutUint32(chunk[8+len(data):], crc32.ChecksumIEEE(chunk[4:8+len(data)]))
	return chunk
}

func insertMarkdownImageTestClip(t *testing.T, app *App, filename, contentType string, data []byte) int64 {
	t.Helper()
	result, err := app.db.Exec(`INSERT INTO clips (filename, content_type, data) VALUES (?, ?, ?)`, filename, contentType, data)
	if err != nil {
		t.Fatalf("insert image clip: %v", err)
	}
	id, _ := result.LastInsertId()
	return id
}

func TestGetMarkdownImageValidatesAndReturnsRasterBytes(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	data := encodeTestPNG(t, 2, 3)
	id := insertMarkdownImageTestClip(t, app, "chart.png", "image/png", data)

	result, err := app.GetMarkdownImage(id)
	if err != nil {
		t.Fatalf("GetMarkdownImage: %v", err)
	}
	if result.ContentType != "image/png" || result.Width != 2 || result.Height != 3 || result.Size != int64(len(data)) || result.DecodedSize != 24 {
		t.Fatalf("result metadata = %+v", result)
	}
	decoded, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !bytes.Equal(decoded, data) {
		t.Fatal("returned image bytes changed")
	}
}

func TestGetMarkdownImageRejectsUnsafeOrMismatchedImages(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	pngData := encodeTestPNG(t, 1, 1)
	mismatchedID := insertMarkdownImageTestClip(t, app, "wrong.jpg", "image/jpeg", pngData)
	if _, err := app.GetMarkdownImage(mismatchedID); err == nil {
		t.Fatal("expected MIME mismatch error")
	}

	animatedControl := make([]byte, 8)
	binary.BigEndian.PutUint32(animatedControl[:4], 2)
	staticPNG := encodeTestPNG(t, 1, 1)
	animatedPNG := append([]byte{}, staticPNG[:33]...)
	animatedPNG = append(animatedPNG, makePNGChunk("acTL", animatedControl)...)
	animatedPNG = append(animatedPNG, staticPNG[33:]...)
	animatedID := insertMarkdownImageTestClip(t, app, "animated.png", "image/png", animatedPNG)
	if _, err := app.GetMarkdownImage(animatedID); err == nil {
		t.Fatal("expected animated PNG rejection")
	}

	animatedWebPHeader := append([]byte("RIFF\x0a\x00\x00\x00WEBP"), []byte("ANIM\x00\x00\x00\x00")...)
	if !isAnimatedWebP(animatedWebPHeader) {
		t.Fatal("animated WebP chunk was not detected")
	}

	svgID := insertMarkdownImageTestClip(t, app, "vector.svg", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`))
	if _, err := app.GetMarkdownImage(svgID); err == nil {
		t.Fatal("expected SVG rejection")
	}

	largeID := insertMarkdownImageTestClip(t, app, "large.png", "image/png", make([]byte, maxMarkdownImageBytes+1))
	if _, err := app.GetMarkdownImage(largeID); err == nil {
		t.Fatal("expected encoded-size rejection")
	}
}

func TestGetMarkdownImageRejectsDimensionsAndGIFFrameCount(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	wideID := insertMarkdownImageTestClip(t, app, "wide.png", "image/png", encodeTestPNG(t, 8193, 1))
	if _, err := app.GetMarkdownImage(wideID); err == nil {
		t.Fatal("expected dimension rejection")
	}

	frames := make([]*image.Paletted, 201)
	delays := make([]int, len(frames))
	palette := color.Palette{color.Black, color.White}
	for i := range frames {
		frames[i] = image.NewPaletted(image.Rect(0, 0, 1, 1), palette)
	}
	var gifData bytes.Buffer
	if err := gif.EncodeAll(&gifData, &gif.GIF{Image: frames, Delay: delays}); err != nil {
		t.Fatalf("encode GIF: %v", err)
	}
	gifID := insertMarkdownImageTestClip(t, app, "animated.gif", "image/gif", gifData.Bytes())
	if _, err := app.GetMarkdownImage(gifID); err == nil {
		t.Fatal("expected GIF frame-count rejection")
	}

	largeFrames := make([]*image.Paletted, 26)
	largeDelays := make([]int, len(largeFrames))
	for i := range largeFrames {
		largeFrames[i] = image.NewPaletted(image.Rect(0, 0, 1000, 1000), palette)
	}
	gifData.Reset()
	if err := gif.EncodeAll(&gifData, &gif.GIF{Image: largeFrames, Delay: largeDelays}); err != nil {
		t.Fatalf("encode cumulative GIF: %v", err)
	}
	cumulativeID := insertMarkdownImageTestClip(t, app, "cumulative.gif", "image/gif", gifData.Bytes())
	if _, err := app.GetMarkdownImage(cumulativeID); err == nil {
		t.Fatal("expected cumulative GIF pixel-cost rejection")
	}
}
