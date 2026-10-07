package app

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"testing"
)

// pngChunk builds one PNG chunk with its CRC.
func pngChunk(typ string, body []byte) []byte {
	c := make([]byte, 12+len(body))
	binary.BigEndian.PutUint32(c[0:], uint32(len(body)))
	copy(c[4:], typ)
	copy(c[8:], body)
	binary.BigEndian.PutUint32(c[8+len(body):], crc32.ChecksumIEEE(c[4:8+len(body)]))
	return c
}

// withTRNS inserts a grayscale tRNS chunk right after IHDR (bytes 8..33).
func withTRNS(data []byte) []byte {
	out := append([]byte{}, data[:33]...)
	out = append(out, pngChunk("tRNS", []byte{0, 0})...)
	return append(out, data[33:]...)
}

// setPNGDims rewrites IHDR's width and height and its CRC.
func setPNGDims(data []byte, w, h int) {
	binary.BigEndian.PutUint32(data[16:], uint32(w))
	binary.BigEndian.PutUint32(data[20:], uint32(h))
	binary.BigEndian.PutUint32(data[29:], crc32.ChecksumIEEE(data[12:29]))
}

// png.DecodeConfig reports Gray16 for a 16-bit grayscale PNG with a tRNS
// chunk, but image/png decodes it to NRGBA64: 8 bytes a pixel, not 2. An
// 8000x8000 one was budgeted at 128 MB and allocated 512 MB.
func TestRenderThumbnailBudgetsTransparentGrayscalePNG(t *testing.T) {
	small := withTRNS(encodePNG(t, image.NewGray16(image.Rect(0, 0, 8, 8))))
	img, _, err := image.Decode(bytes.NewReader(small))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := img.(*image.NRGBA64); !ok {
		t.Fatalf("gray16+tRNS decodes to %T; this test assumes *image.NRGBA64", img)
	}

	bomb := append([]byte{}, small...)
	setPNGDims(bomb, 8000, 8000)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(bomb))
	if err != nil || cfg.ColorModel != color.Gray16Model {
		t.Fatalf("crafted header: model %v err %v", cfg.ColorModel, err)
	}
	var rerr error
	got := allocatedBy(func() { _, _, rerr = renderThumbnail("image/png", bomb) })
	if !errors.Is(rerr, errThumbPassthrough) {
		t.Fatalf("err = %v, want passthrough", rerr)
	}
	if got > 16<<20 {
		t.Fatalf("refusing an 8000x8000 gray16+tRNS PNG allocated %d bytes; the 512 MB NRGBA64 buffer must be refused from the header", got)
	}
}

func TestPNGDecodedModelWidensOnlyTransparentGray(t *testing.T) {
	gray := encodePNG(t, image.NewGray(image.Rect(0, 0, 8, 8)))
	gray16 := encodePNG(t, image.NewGray16(image.Rect(0, 0, 8, 8)))
	for _, c := range []struct {
		name string
		m    color.Model
		data []byte
		want color.Model
	}{
		{"gray opaque", color.GrayModel, gray, color.GrayModel},
		{"gray16 opaque", color.Gray16Model, gray16, color.Gray16Model},
		{"gray tRNS", color.GrayModel, withTRNS(gray), color.NRGBAModel},
		{"gray16 tRNS", color.Gray16Model, withTRNS(gray16), color.NRGBA64Model},
		{"gray16 truncated", color.Gray16Model, gray16[:40], color.NRGBA64Model},
		{"rgba untouched", color.RGBAModel, withTRNS(gray), color.RGBAModel},
	} {
		if got := pngDecodedModel(c.m, c.data); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	// A tRNS after the first IDAT cannot change the buffer the decoder sized.
	tail := append(append([]byte{}, gray16[:len(gray16)-12]...), pngChunk("tRNS", []byte{0, 0})...)
	tail = append(tail, gray16[len(gray16)-12:]...)
	if got := pngDecodedModel(color.Gray16Model, tail); got != color.Gray16Model {
		t.Errorf("tRNS after IDAT: got %v, want Gray16", got)
	}

	// An opaque 8000x8000 gray16 PNG (128 MB) still fits the budget.
	cfg := image.Config{ColorModel: pngDecodedModel(color.Gray16Model, gray16), Width: 8000, Height: 8000}
	if !withinDecodeBudget(cfg) {
		t.Error("opaque 8000x8000 gray16 refused")
	}
}
