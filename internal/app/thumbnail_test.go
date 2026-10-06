package app

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// noisyImage returns a w x h image of random pixels, so its encodings are
// large (a flat image would compress to almost nothing and pass through).
func noisyImage(w, h int, alpha bool) *image.NRGBA {
	rng := rand.New(rand.NewSource(int64(w*7919 + h)))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = uint8(rng.Intn(256))
		img.Pix[i+1] = uint8(rng.Intn(256))
		img.Pix[i+2] = uint8(rng.Intn(256))
		img.Pix[i+3] = 255
		if alpha && i%8 == 0 {
			img.Pix[i+3] = 128
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodeDims(t *testing.T, data []byte) (int, int, string) {
	t.Helper()
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	return cfg.Width, cfg.Height, format
}

func TestRenderThumbnailBoundsLongestEdge(t *testing.T) {
	src := encodeJPEG(t, noisyImage(2000, 1000, false))
	out, ct, err := renderThumbnail("image/jpeg", src)
	if err != nil {
		t.Fatalf("renderThumbnail: %v", err)
	}
	if ct != "image/jpeg" {
		t.Fatalf("content type = %q, want image/jpeg", ct)
	}
	w, h, format := decodeDims(t, out)
	if w != thumbMaxEdge || h != thumbMaxEdge/2 || format != "jpeg" {
		t.Fatalf("thumbnail = %dx%d %s, want %dx%d jpeg", w, h, format, thumbMaxEdge, thumbMaxEdge/2)
	}
	if len(out) >= len(src) {
		t.Fatalf("thumbnail (%d bytes) is not smaller than the original (%d)", len(out), len(src))
	}
}

func TestRenderThumbnailKeepsAlphaAsPNG(t *testing.T) {
	src := encodePNG(t, noisyImage(600, 1500, true))
	out, ct, err := renderThumbnail("image/png", src)
	if err != nil {
		t.Fatalf("renderThumbnail: %v", err)
	}
	w, h, format := decodeDims(t, out)
	if ct != "image/png" || format != "png" {
		t.Fatalf("content type %q / format %q, want png for a translucent source", ct, format)
	}
	if h != thumbMaxEdge || w != 205 {
		t.Fatalf("thumbnail = %dx%d, want 205x%d", w, h, thumbMaxEdge)
	}
}

// pngWithDims re-labels a real PNG's IHDR with other dimensions (and a valid
// CRC), the shape of a decompression bomb: tiny file, enormous canvas.
func pngWithDims(t *testing.T, w, h uint32) []byte {
	t.Helper()
	data := encodePNG(t, image.NewGray(image.Rect(0, 0, 8, 8)))
	// signature(8) + length(4) + "IHDR"(4) -> width at 16, height at 20.
	binary.BigEndian.PutUint32(data[16:], w)
	binary.BigEndian.PutUint32(data[20:], h)
	crc := crc32.ChecksumIEEE(data[12:29])
	binary.BigEndian.PutUint32(data[29:], crc)
	return data
}

func TestRenderThumbnailRefusesDecompressionBomb(t *testing.T) {
	bomb := pngWithDims(t, 30000, 30000) // 900 MP: a 3.6 GB RGBA canvas
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(bomb)); err != nil || cfg.Width != 30000 {
		t.Fatalf("crafted header not readable as 30000 wide: %v %v", cfg, err)
	}
	start := time.Now()
	_, _, err := renderThumbnail("image/png", bomb)
	if !errors.Is(err, errThumbPassthrough) {
		t.Fatalf("err = %v, want passthrough for an over-budget image", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("refusal took %v; the header check must come before any decode", d)
	}
}

func TestRenderThumbnailPassesThroughWhatItShouldNotTouch(t *testing.T) {
	var anim bytes.Buffer
	pal := color.Palette{color.Black, color.White}
	frame := image.NewPaletted(image.Rect(0, 0, 1200, 1200), pal)
	if err := gif.EncodeAll(&anim, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, contentType string
		data              []byte
	}{
		{"already small", "image/png", encodePNG(t, noisyImage(400, 300, false))},
		{"small file, modest size", "image/jpeg", encodeJPEG(t, image.NewGray(image.Rect(0, 0, 1000, 800)))},
		{"animated gif", "image/gif", anim.Bytes()},
		{"svg", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="4000" height="4000"/>`)},
		{"undecodable", "image/png", []byte("not a png at all")},
		{"heic", "image/heic", bytes.Repeat([]byte{1}, 1<<20)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := renderThumbnail(tc.contentType, tc.data); !errors.Is(err, errThumbPassthrough) {
				t.Fatalf("err = %v, want passthrough", err)
			}
		})
	}
}

func TestApplyEXIFOrientation(t *testing.T) {
	// 2x1: red then blue.
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	red, blue := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}
	src.Set(0, 0, red)
	src.Set(1, 0, blue)

	cw := applyEXIFOrientation(src, 6) // rotate 90 CW: red on top
	if b := cw.Bounds(); b.Dx() != 1 || b.Dy() != 2 {
		t.Fatalf("orientation 6 bounds = %v", b)
	}
	if cw.RGBAAt(0, 0) != red || cw.RGBAAt(0, 1) != blue {
		t.Fatalf("orientation 6 = %v,%v; want red over blue", cw.RGBAAt(0, 0), cw.RGBAAt(0, 1))
	}
	ccw := applyEXIFOrientation(src, 8) // rotate 90 CCW: blue on top
	if ccw.RGBAAt(0, 0) != blue || ccw.RGBAAt(0, 1) != red {
		t.Fatalf("orientation 8 = %v,%v; want blue over red", ccw.RGBAAt(0, 0), ccw.RGBAAt(0, 1))
	}
	flip := applyEXIFOrientation(src, 2)
	if flip.RGBAAt(0, 0) != blue || flip.RGBAAt(1, 0) != red {
		t.Fatal("orientation 2 should mirror horizontally")
	}
}

func insertImageClip(t *testing.T, app *App, data []byte, contentType string) (int64, string) {
	t.Helper()
	hash := computeContentHash(data)
	res, err := app.db.Exec("INSERT INTO clips (content_type, data, filename, content_hash) VALUES (?, ?, 'pic', ?)", contentType, data, hash)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id, hash
}

func newTestThumbCache(t *testing.T, app *App) *ThumbnailCache {
	t.Helper()
	tc, err := NewThumbnailCache(app.db, nil, filepath.Join(t.TempDir(), "thumbs"))
	if err != nil {
		t.Fatal(err)
	}
	app.thumbCache = tc
	return tc
}

func serveThumb(t *testing.T, tc *ThumbnailCache, id int64, hash string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/thumb", nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	if !tc.Serve(rec, req, id, hash) {
		rec.Code = http.StatusNotFound
	}
	return rec
}

func TestThumbnailCacheKeyedByContentHash(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	tc := newTestThumbCache(t, app)

	id, hash1 := insertImageClip(t, app, encodeJPEG(t, noisyImage(1600, 1200, false)), "image/jpeg")

	rec := serveThumb(t, tc, id, hash1, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if w, _, _ := decodeDims(t, rec.Body.Bytes()); w != thumbMaxEdge {
		t.Fatalf("width = %d, want %d", w, thumbMaxEdge)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") || !strings.Contains(cc, "private") {
		t.Fatalf("Cache-Control = %q for a URL naming the current hash", cc)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff")
	}
	etag1 := rec.Header().Get("ETag")
	if !strings.Contains(etag1, hash1) {
		t.Fatalf("ETag %q does not name the content hash", etag1)
	}
	file1 := filepath.Join(tc.dir, hash1+"-"+strconv.Itoa(thumbMaxEdge)+".jpg")
	if _, err := os.Stat(file1); err != nil {
		t.Fatalf("cache file for hash1: %v", err)
	}

	// Revalidation answers 304 without a body.
	if rec := serveThumb(t, tc, id, hash1, http.Header{"If-None-Match": {etag1}}); rec.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match: status %d, want 304", rec.Code)
	}

	// Edit the clip: a new hash is a new cache entry, nothing to invalidate.
	edited := encodeJPEG(t, noisyImage(900, 1800, false))
	hash2 := computeContentHash(edited)
	if _, err := app.db.Exec("UPDATE clips SET data = ?, content_hash = ? WHERE id = ?", edited, hash2, id); err != nil {
		t.Fatal(err)
	}
	rec = serveThumb(t, tc, id, hash2, nil)
	if w, h, _ := decodeDims(t, rec.Body.Bytes()); w != 256 || h != thumbMaxEdge {
		t.Fatalf("edited thumbnail = %dx%d, want 256x%d", w, h, thumbMaxEdge)
	}
	if etag := rec.Header().Get("ETag"); etag == etag1 || !strings.Contains(etag, hash2) {
		t.Fatalf("edited ETag %q", etag)
	}

	// The old URL still answers, with the current image and no-cache.
	rec = serveThumb(t, tc, id, hash1, nil)
	if !strings.Contains(rec.Header().Get("ETag"), hash2) || !strings.Contains(rec.Header().Get("Cache-Control"), "no-cache") {
		t.Fatalf("stale URL: ETag %q Cache-Control %q", rec.Header().Get("ETag"), rec.Header().Get("Cache-Control"))
	}

	// Prune drops the entry no clip holds any more, keeps the live one.
	if err := tc.Prune(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file1); !os.IsNotExist(err) {
		t.Fatalf("stale thumbnail survived prune: %v", err)
	}
	if _, ok := tc.lookup(hash2); !ok {
		t.Fatal("prune removed the live thumbnail")
	}
}

func TestThumbnailServesSmallOriginalUnchanged(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	tc := newTestThumbCache(t, app)

	small := encodePNG(t, noisyImage(64, 48, false))
	id, hash := insertImageClip(t, app, small, "image/png")
	rec := serveThumb(t, tc, id, hash, nil)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), small) {
		t.Fatalf("status %d; body is not the original bytes", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	if _, err := os.Stat(filepath.Join(tc.dir, hash+"-"+strconv.Itoa(thumbMaxEdge)+".orig")); err != nil {
		t.Fatalf("passthrough decision not cached: %v", err)
	}
}

func TestThumbnailRefusesNonImageClips(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	tc := newTestThumbCache(t, app)

	id, hash := insertImageClip(t, app, []byte("secret text"), "text/plain")
	if rec := serveThumb(t, tc, id, hash, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("text clip: status %d, want 404", rec.Code)
	}
	if rec := serveThumb(t, tc, 99999, "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing clip: status %d, want 404", rec.Code)
	}
}

func TestThumbnailPruneCapsCacheSize(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	tc := newTestThumbCache(t, app)

	var hashes []string
	for i := 0; i < 3; i++ {
		id, hash := insertImageClip(t, app, encodeJPEG(t, noisyImage(1200+i, 900, false)), "image/jpeg")
		if rec := serveThumb(t, tc, id, hash, nil); rec.Code != http.StatusOK {
			t.Fatalf("serve %d: %d", i, rec.Code)
		}
		hashes = append(hashes, hash)
	}
	var total int64
	for _, h := range hashes {
		e, _ := tc.lookup(h)
		info, _ := os.Stat(e.path)
		total += info.Size()
	}
	tc.maxBytes = total - 1 // just over: one eviction is enough
	// Age the first entry so it is the least recently used.
	e, _ := tc.lookup(hashes[0])
	old := time.Now().Add(-12 * time.Hour)
	if err := os.Chtimes(e.path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := tc.Prune(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(e.path); !os.IsNotExist(err) {
		t.Fatal("least recently used thumbnail was not evicted")
	}
	if _, ok := tc.lookup(hashes[2]); !ok {
		t.Fatal("recent thumbnail was evicted")
	}
}

func TestHandleGetClipThumbEnforcesTagScope(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	newTestThumbCache(t, app)
	manager := NewAPIManager(app)

	if _, err := app.db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'work', '#111111'), (2, 'work/a', '#222222'), (3, 'personal', '#333333')`); err != nil {
		t.Fatal(err)
	}
	inID, inHash := insertImageClip(t, app, encodeJPEG(t, noisyImage(1300, 700, false)), "image/jpeg")
	outID, outHash := insertImageClip(t, app, encodeJPEG(t, noisyImage(1301, 700, false)), "image/jpeg")
	if _, err := app.db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, 2), (?, 3)`, inID, outID); err != nil {
		t.Fatal(err)
	}

	get := func(id int64, hash string, key *apiKeyContext) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/clips/"+strconv.FormatInt(id, 10)+"/thumb?h="+hash, nil)
		req.SetPathValue("id", strconv.FormatInt(id, 10))
		rec := httptest.NewRecorder()
		manager.handleGetClipThumb(rec, withKey(req, key))
		return rec
	}
	scoped := &apiKeyContext{KeyID: 1, Role: "viewer", ScopedTagID: 1}

	if rec := get(inID, inHash, scoped); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("in-scope clip: status %d type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	rec := get(outID, outHash, scoped)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("out-of-scope clip: status %d, want 403", rec.Code)
	}
	if strings.HasPrefix(rec.Header().Get("Content-Type"), "image/") {
		t.Fatal("out-of-scope request received image bytes")
	}
	if rec := get(outID, outHash, &apiKeyContext{KeyID: 2, Role: "viewer"}); rec.Code != http.StatusOK {
		t.Fatalf("unscoped key: status %d", rec.Code)
	}
}

func TestDesktopThumbRouteRequiresKey(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	newTestThumbCache(t, app)
	h := NewTransferFileHandler(app)
	app.SetTransferHandler(h)

	id, hash := insertImageClip(t, app, encodeJPEG(t, noisyImage(1100, 1100, false)), "image/jpeg")
	base, err := app.ThumbnailURLBase()
	if err != nil {
		t.Fatal(err)
	}
	again, _ := app.ThumbnailURLBase()
	if base != again {
		t.Fatalf("thumbnail base changed between calls: %q vs %q", base, again)
	}

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	ok := get(base + strconv.FormatInt(id, 10) + "/" + hash)
	if ok.Code != http.StatusOK || ok.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("keyed request: status %d type %q", ok.Code, ok.Header().Get("Content-Type"))
	}
	for _, path := range []string{
		"/thumb/" + strings.Repeat("0", 32) + "/" + strconv.FormatInt(id, 10) + "/" + hash,
		"/thumb/" + strconv.FormatInt(id, 10) + "/" + hash,
		base + "notanid/" + hash,
	} {
		if rec := get(path); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status %d, want 404", path, rec.Code)
		}
	}
}

func TestClipListingCarriesContentHash(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	id, hash := insertImageClip(t, app, encodePNG(t, noisyImage(10, 10, false)), "image/png")

	page, err := app.ListClipsPage(ClipListRequest{Mode: "all", SortField: "created_at", SortDir: "desc", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Clips) != 1 || page.Clips[0].ID != id || page.Clips[0].ContentHash != hash {
		t.Fatalf("listing = %+v, want clip %d with hash %s", page.Clips, id, hash)
	}
	preview, err := app.getClipPreview(id)
	if err != nil || preview.ContentHash != hash {
		t.Fatalf("getClipPreview hash = %q (%v), want %s", preview.ContentHash, err, hash)
	}
}
