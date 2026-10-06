package app

// Gallery image thumbnails.
//
// A gallery card is about 200 CSS px wide, yet it used to receive the whole
// original as base64 over the Wails bridge and decode it at full resolution
// (about 48 MB of bitmap for a 12 MP photo). ThumbnailCache makes a bounded
// copy once per content revision and serves it as plain HTTP, so the WebView
// fetches, caches and decodes it like any other image.
//
// Entries are keyed by content_hash, never by clip id: an edit changes the
// hash, so a stale thumbnail is simply never looked up again, and no writer of
// clips.data has to know this cache exists. Prune removes entries whose hash no
// clip holds any more and caps the directory's size.
//
// Anything a re-encode could make worse is passed through as the original
// bytes: small originals, animations (GIF, APNG, animated WebP), SVG, formats
// Go cannot decode, and images over the decode budget. For those the card
// behaves exactly as before.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/sync/singleflight"

	"go-clipboard/internal/imagemeta"
)

const (
	// thumbMaxEdge is the longest side of a generated thumbnail, in pixels:
	// a card at 2x device pixels with some headroom.
	thumbMaxEdge = 512
	// An original at most this many bytes and thumbPassthroughEdge pixels on
	// its longest side is served as it is: re-encoding it saves little.
	thumbPassthroughBytes = 256 << 10
	thumbPassthroughEdge  = 2 * thumbMaxEdge
	// Decode budget. DecodeConfig reads only the header, so a decompression
	// bomb (a small file declaring huge dimensions) is refused before any
	// pixel buffer is allocated. Refused images pass through unchanged.
	thumbMaxSourcePixels = 64_000_000
	thumbMaxSourceBytes  = 64 << 20
	// thumbMaxDecodeBytes caps the decoded pixel buffer, estimated from the
	// header's color model (decodeBytesPerPixel): a 16-bit PNG decodes to 8
	// bytes a pixel, so the pixel cap alone would let one through at 512 MB.
	thumbMaxDecodeBytes = 256 << 20
	thumbJPEGQuality    = 82
	// thumbGenerateConcurrency bounds how many decodes run at once, and with
	// it peak memory (each decode is at most thumbMaxDecodeBytes plus a
	// downscaled copy a quarter that size or less).
	thumbGenerateConcurrency = 2
	// Cache size cap and prune throttle.
	thumbCacheMaxBytes   = 256 << 20
	thumbPruneInterval   = 10 * time.Minute
	thumbTouchAfter      = 24 * time.Hour
	thumbStaleTempMaxAge = time.Hour
	// thumbDropOrphansDelay coalesces the orphan sweeps that deletes, edits
	// and restores ask for (DropOrphansSoon) into one.
	thumbDropOrphansDelay = 500 * time.Millisecond
)

// errThumbPassthrough means "serve the original bytes"; it is not a failure.
var errThumbPassthrough = errors.New("thumbnail: serve original")

// ThumbnailCache generates, stores and serves gallery thumbnails.
type ThumbnailCache struct {
	db       *sql.DB
	store    *TempClipStore // serves passthrough originals; may be nil
	dir      string
	maxBytes int64

	flights singleflight.Group
	sem     chan struct{}

	pruneMu   sync.Mutex
	lastPrune time.Time

	// dropMu guards dropTimer, the one pending DropOrphansSoon sweep.
	dropMu    sync.Mutex
	dropTimer *time.Timer
	dropDelay time.Duration
}

// thumbEntry is one cached decision for a content hash: a generated file, or
// a marker saying the original is served.
type thumbEntry struct {
	path        string
	contentType string
	original    bool
}

var thumbVariants = []struct {
	ext         string
	contentType string
}{
	{".jpg", "image/jpeg"},
	{".png", "image/png"},
	{".orig", ""}, // empty marker: serve the original bytes
}

// NewThumbnailCache creates the cache directory (private: thumbnails are
// copies of library content).
func NewThumbnailCache(db *sql.DB, store *TempClipStore, dir string) (*ThumbnailCache, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create thumbnail dir %q: %w", dir, err)
	}
	return &ThumbnailCache{
		db:        db,
		store:     store,
		dir:       dir,
		maxBytes:  thumbCacheMaxBytes,
		sem:       make(chan struct{}, thumbGenerateConcurrency),
		dropDelay: thumbDropOrphansDelay,
	}, nil
}

func isContentHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}

func (c *ThumbnailCache) base(hash string) string {
	return filepath.Join(c.dir, hash+"-"+strconv.Itoa(thumbMaxEdge))
}

// lookup returns the cached entry for hash, if any. A hit older than
// thumbTouchAfter has its mtime refreshed, which is what Prune's size cap
// evicts by.
func (c *ThumbnailCache) lookup(hash string) (thumbEntry, bool) {
	base := c.base(hash)
	for _, v := range thumbVariants {
		p := base + v.ext
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if time.Since(info.ModTime()) > thumbTouchAfter {
			now := time.Now()
			_ = os.Chtimes(p, now, now)
		}
		return thumbEntry{path: p, contentType: v.contentType, original: v.ext == ".orig"}, true
	}
	return thumbEntry{}, false
}

type thumbResult struct {
	entry thumbEntry
	hash  string
}

// ensure returns the entry for clipID's current bytes, generating it on a
// miss. One generation runs per clip and hash however many requests ask. The hash
// returned is the one the entry belongs to: it differs from hash when the clip
// was edited after the caller read it.
func (c *ThumbnailCache) ensure(ctx context.Context, clipID int64, hash string) (thumbEntry, string, error) {
	if e, ok := c.lookup(hash); ok {
		return e, hash, nil
	}
	// The flight is shared by every waiter, so it must not die with the
	// first caller's request; each caller still stops waiting on its own.
	// Keyed by clip too: a flight reads its own clip, and if that clip was
	// edited meanwhile its result belongs to the new bytes, which a duplicate
	// clip still holding the old ones must not be handed.
	ch := c.flights.DoChan(hash+":"+strconv.FormatInt(clipID, 10), func() (any, error) {
		c.sem <- struct{}{}
		defer func() { <-c.sem }()
		if e, ok := c.lookup(hash); ok {
			return thumbResult{e, hash}, nil
		}
		return c.generate(context.Background(), clipID)
	})
	var v any
	var err error
	select {
	case res := <-ch:
		v, err = res.Val, res.Err
	case <-ctx.Done():
		return thumbEntry{}, "", ctx.Err()
	}
	if err != nil {
		return thumbEntry{}, "", err
	}
	res := v.(thumbResult)
	return res.entry, res.hash, nil
}

// generate reads the clip, renders its thumbnail (or decides on passthrough)
// and records the result under the hash of the bytes it actually read.
func (c *ThumbnailCache) generate(ctx context.Context, clipID int64) (thumbResult, error) {
	var contentType, hash string
	var size int64
	err := c.db.QueryRowContext(ctx,
		"SELECT content_type, octet_length(data), COALESCE(content_hash, '') FROM clips WHERE id = ?", clipID,
	).Scan(&contentType, &size, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return thumbResult{}, ErrClipNotFound
	}
	if err != nil {
		return thumbResult{}, err
	}

	var out []byte
	var outType string
	if size > thumbMaxSourceBytes || !strings.HasPrefix(contentType, "image/") {
		err = errThumbPassthrough
	} else {
		var data []byte
		if err := c.db.QueryRowContext(ctx,
			"SELECT content_type, data, COALESCE(content_hash, '') FROM clips WHERE id = ?", clipID,
		).Scan(&contentType, &data, &hash); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return thumbResult{}, ErrClipNotFound
			}
			return thumbResult{}, err
		}
		if hash == "" {
			hash = computeContentHash(data)
		}
		out, outType, err = renderThumbnail(contentType, data)
	}
	if !isContentHash(hash) {
		return thumbResult{}, fmt.Errorf("clip %d has no content hash", clipID)
	}

	ext := ".orig"
	switch {
	case errors.Is(err, errThumbPassthrough):
		out = nil
	case err != nil:
		return thumbResult{}, err
	case outType == "image/png":
		ext = ".png"
	default:
		ext = ".jpg"
	}
	path := c.base(hash) + ext
	if err := writeFileAtomic(c.dir, path, out); err != nil {
		return thumbResult{}, err
	}
	return thumbResult{thumbEntry{path: path, contentType: outType, original: ext == ".orig"}, hash}, nil
}

func writeFileAtomic(dir, path string, data []byte) error {
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// renderThumbnail returns an encoded thumbnail of data and its content type,
// or errThumbPassthrough when the original should be served instead.
func renderThumbnail(contentType string, data []byte) ([]byte, string, error) {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	switch mediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		// SVG, HEIC, TIFF, ...: not decodable here, or vector already.
		return nil, "", errThumbPassthrough
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, "", errThumbPassthrough
	}
	if !withinDecodeBudget(cfg) {
		return nil, "", errThumbPassthrough
	}
	longest := max(cfg.Width, cfg.Height)
	if longest <= thumbMaxEdge || (len(data) <= thumbPassthroughBytes && longest <= thumbPassthroughEdge) {
		return nil, "", errThumbPassthrough
	}
	switch format {
	case "gif":
		frames, err := countGIFFrames(data, 2)
		if err != nil || frames != 1 {
			return nil, "", errThumbPassthrough
		}
	case "png":
		if isAnimatedPNG(data) {
			return nil, "", errThumbPassthrough
		}
	case "webp":
		if isAnimatedWebP(data) {
			return nil, "", errThumbPassthrough
		}
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", errThumbPassthrough
	}
	thumb := scaleToFit(src, thumbMaxEdge)
	if format == "jpeg" {
		thumb = applyEXIFOrientation(thumb, imagemeta.Orientation(data))
	}

	var buf bytes.Buffer
	outType := "image/jpeg"
	if thumb.Opaque() {
		err = jpeg.Encode(&buf, thumb, &jpeg.Options{Quality: thumbJPEGQuality})
	} else {
		outType = "image/png"
		err = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, thumb)
	}
	if err != nil {
		return nil, "", fmt.Errorf("encode thumbnail: %w", err)
	}
	if buf.Len() >= len(data) {
		return nil, "", errThumbPassthrough
	}
	return buf.Bytes(), outType, nil
}

// withinDecodeBudget reports whether decoding an image with this header stays
// under both the pixel cap and the decoded-buffer byte cap.
func withinDecodeBudget(cfg image.Config) bool {
	pixels := int64(cfg.Width) * int64(cfg.Height)
	return pixels <= thumbMaxSourcePixels && pixels*decodeBytesPerPixel(cfg.ColorModel) <= thumbMaxDecodeBytes
}

// decodeBytesPerPixel estimates the decoded buffer's bytes per pixel from a
// header's color model. Unknown models count as 8, the widest standard one.
func decodeBytesPerPixel(m color.Model) int64 {
	switch m {
	case color.GrayModel, color.AlphaModel:
		return 1
	case color.Gray16Model, color.Alpha16Model:
		return 2
	case color.RGBAModel, color.NRGBAModel, color.YCbCrModel, color.NYCbCrAModel, color.CMYKModel:
		return 4
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	}
	if _, ok := m.(color.Palette); ok {
		return 1
	}
	return 8
}

// scaleToFit returns src scaled so its longest side is edge pixels. A large
// source is first box-averaged by an integer factor (cheap, alias-free) to
// within 2x of the target, then finished with Catmull-Rom.
func scaleToFit(src image.Image, edge int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	longest := max(w, h)
	dw := max(1, (w*edge+longest/2)/longest)
	dh := max(1, (h*edge+longest/2)/longest)
	if factor := longest / (2 * edge); factor >= 2 {
		src = boxShrink(src, factor)
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return dst
}

// boxShrink averages blocks of about factor x factor pixels into one, reading
// the source a row at a time through fast paths for the decoders' own types.
func boxShrink(src image.Image, factor int) *image.RGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dw := max(1, (sw+factor-1)/factor)
	dh := max(1, (sh+factor-1)/factor)
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))

	colOf := make([]int, sw)
	colCount := make([]uint32, dw)
	for x := 0; x < sw; x++ {
		dx := x * dw / sw
		colOf[x] = dx
		colCount[dx]++
	}
	row := make([]uint8, sw*4)
	sums := make([]uint32, dw*4)
	for dy := 0; dy < dh; dy++ {
		y0, y1 := dy*sh/dh, (dy+1)*sh/dh
		clear(sums)
		for y := y0; y < y1; y++ {
			readRowRGBA(src, b.Min.Y+y, row)
			for x := 0; x < sw; x++ {
				o, s := x*4, colOf[x]*4
				sums[s] += uint32(row[o])
				sums[s+1] += uint32(row[o+1])
				sums[s+2] += uint32(row[o+2])
				sums[s+3] += uint32(row[o+3])
			}
		}
		rows := uint32(y1 - y0)
		off := dst.PixOffset(0, dy)
		for dx := 0; dx < dw; dx++ {
			n := colCount[dx] * rows
			if n == 0 {
				continue
			}
			for k := 0; k < 4; k++ {
				dst.Pix[off+dx*4+k] = uint8((sums[dx*4+k] + n/2) / n)
			}
		}
	}
	return dst
}

// readRowRGBA writes row y of src into buf as premultiplied 8-bit RGBA.
func readRowRGBA(src image.Image, y int, buf []uint8) {
	b := src.Bounds()
	w := b.Dx()
	switch m := src.(type) {
	case *image.RGBA:
		off := m.PixOffset(b.Min.X, y)
		copy(buf, m.Pix[off:off+w*4])
	case *image.NRGBA:
		off := m.PixOffset(b.Min.X, y)
		for x := 0; x < w; x++ {
			p := m.Pix[off+x*4 : off+x*4+4]
			a := uint32(p[3])
			buf[x*4] = uint8(uint32(p[0]) * a / 255)
			buf[x*4+1] = uint8(uint32(p[1]) * a / 255)
			buf[x*4+2] = uint8(uint32(p[2]) * a / 255)
			buf[x*4+3] = p[3]
		}
	case *image.YCbCr:
		for x := 0; x < w; x++ {
			yi := m.YOffset(b.Min.X+x, y)
			ci := m.COffset(b.Min.X+x, y)
			r, g, bl := color.YCbCrToRGB(m.Y[yi], m.Cb[ci], m.Cr[ci])
			buf[x*4], buf[x*4+1], buf[x*4+2], buf[x*4+3] = r, g, bl, 255
		}
	case *image.Gray:
		off := m.PixOffset(b.Min.X, y)
		for x := 0; x < w; x++ {
			v := m.Pix[off+x]
			buf[x*4], buf[x*4+1], buf[x*4+2], buf[x*4+3] = v, v, v, 255
		}
	default:
		for x := 0; x < w; x++ {
			r, g, bl, a := src.At(b.Min.X+x, y).RGBA()
			buf[x*4], buf[x*4+1], buf[x*4+2], buf[x*4+3] = uint8(r>>8), uint8(g>>8), uint8(bl>>8), uint8(a>>8)
		}
	}
}

// applyEXIFOrientation returns img transformed so it displays upright for
// EXIF orientation o (1–8), the way a browser renders the original.
func applyEXIFOrientation(img *image.RGBA, o int) *image.RGBA {
	if o <= 1 || o > 8 {
		return img
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var sx, sy int
			switch o {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			so, do := img.PixOffset(sx, sy), dst.PixOffset(x, y)
			copy(dst.Pix[do:do+4], img.Pix[so:so+4])
		}
	}
	return dst
}

// Serve writes the thumbnail of clipID. wantHash is the content hash the
// caller's URL names, or "". When it is the clip's current hash the response
// is immutable (the URL can never mean other bytes); otherwise it is served
// for the current bytes with no-cache. It returns false, having written
// nothing, when there is no such image clip. It performs no authorization.
func (c *ThumbnailCache) Serve(w http.ResponseWriter, r *http.Request, clipID int64, wantHash string) bool {
	ctx := r.Context()
	var contentType, hash string
	matched := false
	if isContentHash(wantHash) {
		// Answered from idx_clips_content_hash (hash, rowid) plus the row's
		// first page: content_hash sits past the blob in the table.
		err := c.db.QueryRowContext(ctx,
			"SELECT content_type FROM clips INDEXED BY idx_clips_content_hash WHERE content_hash = ? AND id = ?",
			wantHash, clipID).Scan(&contentType)
		if err == nil {
			hash, matched = wantHash, true
		}
	}
	if !matched {
		meta, h, err := loadClipBodyMetadata(ctx, c.db, clipID)
		if err != nil {
			return false
		}
		contentType, hash = meta.contentType, h
	}
	if !strings.HasPrefix(contentType, "image/") {
		return false
	}
	if !isContentHash(hash) {
		return c.serveOriginal(w, r, clipID, "", false)
	}

	entry, gotHash, err := c.ensure(ctx, clipID, hash)
	if err != nil {
		if !errors.Is(err, ErrClipNotFound) && !errors.Is(err, context.Canceled) {
			log.Printf("thumbnail: clip %d: %v", clipID, err)
		}
		if errors.Is(err, ErrClipNotFound) {
			return false
		}
		// Any other failure: the original still renders the card.
		return c.serveOriginal(w, r, clipID, "", false)
	}
	if gotHash != hash {
		matched = false
	}
	if entry.original {
		return c.serveOriginal(w, r, clipID, gotHash, matched)
	}

	f, err := os.Open(entry.path)
	if err != nil {
		// Pruned between lookup and open: the original is always right.
		return c.serveOriginal(w, r, clipID, "", false)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return c.serveOriginal(w, r, clipID, "", false)
	}
	etag := fmt.Sprintf(`"t%d-%s"`, thumbMaxEdge, gotHash)
	writeThumbBody(w, r, f, info.Size(), entry.contentType, etag, matched)
	return true
}

// serveOriginal serves the clip's own bytes as its thumbnail. immutable is
// honoured only when the bytes read are the revision expected.
func (c *ThumbnailCache) serveOriginal(w http.ResponseWriter, r *http.Request, clipID int64, expectHash string, immutable bool) bool {
	body, err := openClipBody(r.Context(), c.db, c.store, clipID, r.Method == http.MethodHead)
	if err != nil {
		return false
	}
	defer body.Close()
	if !strings.HasPrefix(body.contentType, "image/") {
		return false
	}
	if body.hash == "" || body.hash != expectHash {
		immutable = false
	}
	etag := ""
	if body.hash != "" {
		etag = `"o-` + body.hash + `"`
	}
	var src io.Reader
	if body.data != nil {
		src = io.NewSectionReader(body.data, 0, body.size)
	}
	writeThumbBody(w, r, src, body.size, body.contentType, etag, immutable)
	return true
}

// writeThumbBody writes a thumbnail response. The headers are the same
// stored-XSS defenses as writeClipBytes (an SVG original passes through), plus
// private caching: immutable when the URL names this exact revision.
func writeThumbBody(w http.ResponseWriter, r *http.Request, src io.Reader, size int64, contentType, etag string, immutable bool) {
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if immutable {
		h.Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "private, no-cache")
	}
	if etag != "" {
		h.Set("ETag", etag)
		if etagListMatches(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	if r.Method == http.MethodHead || src == nil || size == 0 {
		return
	}
	_ = copyClipBody(w, src)
}

func etagListMatches(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || strings.TrimPrefix(part, "W/") == etag {
			return true
		}
	}
	return false
}

// DropOrphansSoon schedules a forced Prune shortly after a clip delete, an
// edit or a restore, so a thumbnail — a downscaled copy of library content —
// does not outlive the clip by a whole prune interval. Calls within
// thumbDropOrphansDelay share one sweep. Safe on a nil cache.
func (c *ThumbnailCache) DropOrphansSoon() {
	if c == nil {
		return
	}
	c.dropMu.Lock()
	defer c.dropMu.Unlock()
	if c.dropTimer != nil {
		return
	}
	c.dropTimer = time.AfterFunc(c.dropDelay, func() {
		c.dropMu.Lock()
		c.dropTimer = nil
		c.dropMu.Unlock()
		if err := c.Prune(true); err != nil && !strings.Contains(err.Error(), "database is closed") {
			log.Printf("thumbnail: drop orphans: %v", err)
		}
	})
}

// Prune removes cache entries whose hash no clip holds any more, stale
// temporaries, and then the least recently used entries over the size cap.
// Unless force is set it runs at most once per thumbPruneInterval.
func (c *ThumbnailCache) Prune(force bool) error {
	c.pruneMu.Lock()
	defer c.pruneMu.Unlock()
	now := time.Now()
	if !force && now.Sub(c.lastPrune) < thumbPruneInterval {
		return nil
	}
	c.lastPrune = now

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}
	type file struct {
		path    string
		size    int64
		modTime time.Time
	}
	byHash := map[string][]file{}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		path := filepath.Join(c.dir, name)
		if strings.HasPrefix(name, ".tmp-") {
			if now.Sub(info.ModTime()) > thumbStaleTempMaxAge {
				_ = os.Remove(path)
			}
			continue
		}
		if len(name) < 65 || name[64] != '-' || !isContentHash(name[:64]) {
			continue
		}
		byHash[name[:64]] = append(byHash[name[:64]], file{path, info.Size(), info.ModTime()})
	}

	var live []file
	var total int64
	for hash, files := range byHash {
		var one int
		err := c.db.QueryRow(
			"SELECT 1 FROM clips INDEXED BY idx_clips_content_hash WHERE content_hash = ? LIMIT 1", hash,
		).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			for _, f := range files {
				_ = os.Remove(f.path)
			}
			continue
		}
		if err != nil {
			return err
		}
		for _, f := range files {
			live = append(live, f)
			total += f.size
		}
	}
	if total <= c.maxBytes {
		return nil
	}
	sort.Slice(live, func(i, j int) bool { return live[i].modTime.Before(live[j].modTime) })
	target := c.maxBytes * 9 / 10
	for _, f := range live {
		if total <= target {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
	return nil
}
