package app

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"math/rand"
	"runtime"
	"testing"
)

// allocatedBy reports the bytes f allocates on the heap (cumulative, so
// garbage collected mid-run still counts).
func allocatedBy(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// boxShrink's scratch space must not grow with the source width: a
// 64,000,000 x 1 grayscale PNG passes the decode budget, and source-width
// arrays (an int per column plus a whole RGBA row) cost 768 MB on top of the
// decoded pixels.
func TestBoxShrinkWorkspaceIndependentOfSourceWidth(t *testing.T) {
	const sw = 4_000_000
	src := image.NewGray(image.Rect(0, 0, sw, 1))
	factor := sw / (2 * thumbMaxEdge)
	var dst *image.RGBA
	got := allocatedBy(func() { dst = boxShrink(src, factor) })
	dstBytes := uint64(len(dst.Pix))
	// Width-proportional scratch would be sw*8 + sw*4 = 48 MB here.
	if limit := dstBytes + 1<<20; got > limit {
		t.Fatalf("boxShrink of a %dx1 image allocated %d bytes, want <= %d (dst is %d)", sw, got, limit, dstBytes)
	}
}

// An extreme aspect ratio is refused from the header alone: the reviewer's
// 64,000,000 x 1 grayscale PNG is within the pixel and byte caps, but the PNG
// decoder's two full-width row buffers would double its cost again.
func TestRenderThumbnailRefusesExtremeAspectRatio(t *testing.T) {
	for _, dims := range [][2]int{{thumbMaxSourcePixels, 1}, {1, thumbMaxSourcePixels}, {thumbMaxSourceEdge + 1, 2}} {
		hdr := encodePNG(t, image.NewGray(image.Rect(0, 0, 8, 8)))
		binary.BigEndian.PutUint32(hdr[16:], uint32(dims[0]))
		binary.BigEndian.PutUint32(hdr[20:], uint32(dims[1]))
		binary.BigEndian.PutUint32(hdr[29:], crc32.ChecksumIEEE(hdr[12:29]))
		cfg, _, err := image.DecodeConfig(bytes.NewReader(hdr))
		if err != nil || cfg.Width != dims[0] || cfg.ColorModel != color.GrayModel {
			t.Fatalf("crafted %dx%d header: %+v %v", dims[0], dims[1], cfg, err)
		}
		if withinDecodeBudget(cfg) {
			t.Errorf("%dx%d is within the decode budget", dims[0], dims[1])
		}
		if _, _, err := renderThumbnail("image/png", hdr); !errors.Is(err, errThumbPassthrough) {
			t.Errorf("%dx%d: err = %v, want passthrough", dims[0], dims[1], err)
		}
	}
}

// A panorama at the edge cap is still thumbnailed, within a budget of its
// decoded pixels plus small change.
func TestRenderThumbnailWidePanoramaAtEdgeCap(t *testing.T) {
	const w, h = thumbMaxSourceEdge, 40
	img := image.NewGray(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(1))
	for i := range img.Pix {
		img.Pix[i] = uint8(rng.Intn(256))
	}
	data := encodePNG(t, img)
	var (
		thumb []byte
		err   error
	)
	got := allocatedBy(func() { thumb, _, err = renderThumbnail("image/png", data) })
	if err != nil {
		t.Fatalf("renderThumbnail: %v", err)
	}
	if tw, th, _ := decodeDims(t, thumb); tw != thumbMaxEdge || th < 1 {
		t.Fatalf("thumbnail %dx%d, want %d wide", tw, th, thumbMaxEdge)
	}
	if limit := uint64(w*h) + 8<<20; got > limit {
		t.Fatalf("renderThumbnail allocated %d bytes, want <= %d", got, limit)
	}
}

// boxShrinkReference is the straightforward per-column version boxShrink
// replaced; chunked reads must produce byte-identical output.
func boxShrinkReference(src image.Image, factor int) *image.RGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dw := max(1, (sw+factor-1)/factor)
	dh := max(1, (sh+factor-1)/factor)
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for dy := 0; dy < dh; dy++ {
		y0, y1 := dy*sh/dh, (dy+1)*sh/dh
		sums := make([]uint32, dw*4)
		counts := make([]uint32, dw)
		for y := y0; y < y1; y++ {
			for x := 0; x < sw; x++ {
				dx := x * dw / sw
				r, g, bl, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
				sums[dx*4] += r >> 8
				sums[dx*4+1] += g >> 8
				sums[dx*4+2] += bl >> 8
				sums[dx*4+3] += a >> 8
				counts[dx]++
			}
		}
		for dx := 0; dx < dw; dx++ {
			n := counts[dx]
			if n == 0 {
				continue
			}
			for k := 0; k < 4; k++ {
				dst.Pix[dst.PixOffset(dx, dy)+k] = uint8((sums[dx*4+k] + n/2) / n)
			}
		}
	}
	return dst
}

func TestBoxShrinkMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	fill := func(pix []uint8) {
		for i := range pix {
			pix[i] = uint8(rng.Intn(256))
		}
	}
	gray := func(w, h int) image.Image {
		m := image.NewGray(image.Rect(0, 0, w, h))
		fill(m.Pix)
		return m
	}
	rgba := func(w, h int) image.Image {
		m := image.NewRGBA(image.Rect(0, 0, w, h))
		for i := 0; i < len(m.Pix); i += 4 {
			a := uint8(rng.Intn(256))
			m.Pix[i+3] = a
			for k := 0; k < 3; k++ {
				m.Pix[i+k] = uint8(rng.Intn(int(a) + 1))
			}
		}
		return m
	}
	opaqueNRGBA := func(w, h int) image.Image {
		m := image.NewNRGBA(image.Rect(0, 0, w, h))
		fill(m.Pix)
		for i := 3; i < len(m.Pix); i += 4 {
			m.Pix[i] = 255
		}
		return m
	}
	paletted := func(w, h int) image.Image {
		m := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White, color.RGBA{200, 10, 30, 255}})
		for i := range m.Pix {
			m.Pix[i] = uint8(rng.Intn(3))
		}
		return m
	}
	ycbcr := func(w, h int) image.Image {
		m := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
		fill(m.Y)
		fill(m.Cb)
		fill(m.Cr)
		return m
	}
	sub := func(make func(w, h int) image.Image) func(w, h int) image.Image {
		return func(w, h int) image.Image {
			full := make(w+13, h+5)
			return full.(interface {
				SubImage(image.Rectangle) image.Image
			}).SubImage(image.Rect(7, 3, 7+w, 3+h))
		}
	}
	cases := []struct {
		name   string
		make   func(w, h int) image.Image
		w, h   int
		factor int
	}{
		{"gray-wide", gray, 3*boxShrinkChunk + 17, 3, 5},
		{"gray-tall", gray, 9, 4000, 3},
		{"rgba", rgba, 2*boxShrinkChunk + 1, 7, 4},
		{"nrgba", opaqueNRGBA, boxShrinkChunk - 1, 9, 3},
		{"paletted", paletted, boxShrinkChunk + 5, 6, 2},
		{"ycbcr", ycbcr, boxShrinkChunk + 99, 8, 7},
		{"gray-sub", sub(gray), boxShrinkChunk + 3, 5, 4},
		{"ycbcr-sub", sub(ycbcr), boxShrinkChunk + 11, 6, 3},
		{"factor-exceeds-width", gray, 3, 50, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.make(tc.w, tc.h)
			got := boxShrink(src, tc.factor)
			want := boxShrinkReference(src, tc.factor)
			if got.Bounds() != want.Bounds() {
				t.Fatalf("bounds %v, want %v", got.Bounds(), want.Bounds())
			}
			if !bytes.Equal(got.Pix, want.Pix) {
				t.Fatalf("pixels differ from reference")
			}
		})
	}
}
