package plugin

import (
	"bytes"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"math/rand"
	"strings"

	"github.com/rwcarlsen/goexif/exif"
	lua "github.com/yuin/gopher-lua"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	_ "golang.org/x/image/webp"
)

//go:embed fonts/IBMPlexMono-Regular.ttf
var fontData []byte

var parsedFont *opentype.Font

func init() {
	var err error
	parsedFont, err = opentype.Parse(fontData)
	if err != nil {
		panic("failed to parse embedded font: " + err.Error())
	}
}

// ImageAPI provides image processing functions to plugins
type ImageAPI struct {
	db *sql.DB
}

// NewImageAPI creates a new image API instance
func NewImageAPI(db *sql.DB) *ImageAPI {
	return &ImageAPI{db: db}
}

// Register adds the image module to the Lua state
func (img *ImageAPI) Register(L *lua.LState) {
	imageMod := L.NewTable()

	imageMod.RawSetString("info", L.NewFunction(img.info))
	imageMod.RawSetString("resize", L.NewFunction(img.resize))
	imageMod.RawSetString("overlay_text", L.NewFunction(img.overlayText))
	imageMod.RawSetString("composite", L.NewFunction(img.composite))
	imageMod.RawSetString("dominant_colors", L.NewFunction(img.dominantColors))
	imageMod.RawSetString("grayscale_pixels", L.NewFunction(img.grayscalePixels))
	imageMod.RawSetString("metadata", L.NewFunction(img.metadata))
	imageMod.RawSetString("diff", L.NewFunction(img.diff))
	imageMod.RawSetString("convert", L.NewFunction(img.convert))

	L.SetGlobal("image", imageMod)
}

// loadClipImage loads and decodes an image from the database
func (img *ImageAPI) loadClipImage(clipID int64) (image.Image, string, error) {
	var data []byte
	var contentType string
	err := img.db.QueryRow("SELECT data, content_type FROM clips WHERE id = ?", clipID).Scan(&data, &contentType)
	if err == sql.ErrNoRows {
		return nil, "", fmt.Errorf("clip not found: %d", clipID)
	}
	if err != nil {
		return nil, "", err
	}

	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", fmt.Errorf("clip %d is not an image (type: %s)", clipID, contentType)
	}

	// Check dimensions from headers BEFORE full decode to guard against
	// decompression bombs (a small compressed file can expand to huge dimensions).
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read image config: %w", err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > 100_000_000 {
		return nil, "", fmt.Errorf("image too large (%dx%d, max 100 megapixels)", cfg.Width, cfg.Height)
	}

	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode image: %w", err)
	}

	return decoded, format, nil
}

// loadClipImageRaw loads raw bytes from the database.
// Note: the primary size protection is the 50MB MaxClipDataSize insert limit in api_clips.go.
// The 100MB check below is a defense-in-depth guard against direct DB edits or future changes.
func (img *ImageAPI) loadClipImageRaw(clipID int64) ([]byte, string, error) {
	var data []byte
	var contentType string
	err := img.db.QueryRow("SELECT data, content_type FROM clips WHERE id = ?", clipID).Scan(&data, &contentType)
	if err == sql.ErrNoRows {
		return nil, "", fmt.Errorf("clip not found: %d", clipID)
	}
	if err != nil {
		return nil, "", err
	}

	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", fmt.Errorf("clip %d is not an image (type: %s)", clipID, contentType)
	}

	if len(data) > 100*1024*1024 {
		return nil, "", fmt.Errorf("clip data too large: %d bytes", len(data))
	}

	return data, contentType, nil
}

// maxEncodedOutputBytes is the maximum size of a base64-encoded PNG output (50MB).
// This prevents a single plugin call from producing a Lua string that consumes
// excessive memory in the VM.
const maxEncodedOutputBytes = 50 * 1024 * 1024

// EncodeImagePNG encodes an image as PNG and returns base64 string
func EncodeImagePNG(im image.Image) (string, string, error) {
	return EncodeImage(im, "png", 0)
}

// EncodeImage encodes an image in the given format and returns base64 string.
// Supported formats: "png", "jpeg", "jpg". Quality (1-100) applies to JPEG only.
func EncodeImage(im image.Image, format string, quality int) (string, string, error) {
	var buf bytes.Buffer
	var mimeType string
	switch format {
	case "png":
		if err := png.Encode(&buf, im); err != nil {
			return "", "", fmt.Errorf("failed to encode PNG: %w", err)
		}
		mimeType = "image/png"
	case "jpeg", "jpg":
		if err := jpeg.Encode(&buf, im, &jpeg.Options{Quality: quality}); err != nil {
			return "", "", fmt.Errorf("failed to encode JPEG: %w", err)
		}
		mimeType = "image/jpeg"
	default:
		return "", "", fmt.Errorf("unsupported format %q (valid: png, jpeg, jpg)", format)
	}
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	if len(encoded) > maxEncodedOutputBytes {
		return "", "", fmt.Errorf("encoded image too large (%d bytes, max %d)", len(encoded), maxEncodedOutputBytes)
	}
	return encoded, mimeType, nil
}

// parseHexColor parses "#RGB", "#RRGGBB", or "#RRGGBBAA" to color.RGBA.
// Returns an error for invalid input.
func parseHexColor(hex string) (color.RGBA, error) {
	hex = strings.TrimPrefix(hex, "#")
	c := color.RGBA{A: 255}
	// Expand 3-char shorthand (#F00 -> FF0000)
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 && len(hex) != 8 {
		return color.RGBA{}, fmt.Errorf("invalid hex color length (%d) for #%s, must be 3, 6, or 8", len(hex), hex)
	}
	if len(hex) >= 6 {
		if _, err := fmt.Sscanf(hex[0:2], "%02x", &c.R); err != nil {
			return color.RGBA{}, fmt.Errorf("invalid red channel in #%s", hex)
		}
		if _, err := fmt.Sscanf(hex[2:4], "%02x", &c.G); err != nil {
			return color.RGBA{}, fmt.Errorf("invalid green channel in #%s", hex)
		}
		if _, err := fmt.Sscanf(hex[4:6], "%02x", &c.B); err != nil {
			return color.RGBA{}, fmt.Errorf("invalid blue channel in #%s", hex)
		}
	}
	if len(hex) == 8 {
		if _, err := fmt.Sscanf(hex[6:8], "%02x", &c.A); err != nil {
			return color.RGBA{}, fmt.Errorf("invalid alpha channel in #%s", hex)
		}
	}
	return c, nil
}

// getFont creates a font face from the embedded TTF at the given size.
// The caller MUST call face.Close() when done to release resources.
func getFont(size float64) (font.Face, error) {
	face, err := opentype.NewFace(parsedFont, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create font face: %w", err)
	}
	return face, nil
}

// convertToRGBA converts an image to *image.RGBA without resizing
func convertToRGBA(src image.Image) *image.RGBA {
	if rgba, ok := src.(*image.RGBA); ok {
		return rgba
	}
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)
	return dst
}

// resizeImage resizes an image to the target dimensions using CatmullRom interpolation
func resizeImage(src image.Image, width, height int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

// info returns image dimensions, format, size, and EXIF presence
func (img *ImageAPI) info(L *lua.LState) int {
	clipID := L.CheckInt64(1)

	rawData, contentType, err := img.loadClipImageRaw(clipID)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(rawData))
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("failed to decode image config: " + err.Error()))
		return 2
	}

	// Normalize format name
	if format == "" {
		// Extract from content type
		parts := strings.SplitN(contentType, "/", 2)
		if len(parts) == 2 {
			format = parts[1]
		}
	}

	// Check for EXIF data
	hasExif := false
	_, exifErr := exif.Decode(bytes.NewReader(rawData))
	if exifErr == nil {
		hasExif = true
	}

	result := L.NewTable()
	result.RawSetString("width", lua.LNumber(cfg.Width))
	result.RawSetString("height", lua.LNumber(cfg.Height))
	result.RawSetString("format", lua.LString(format))
	result.RawSetString("size", lua.LNumber(len(rawData)))
	result.RawSetString("has_exif", lua.LBool(hasExif))

	L.Push(result)
	return 1
}

// resize resizes an image to the given dimensions
func (img *ImageAPI) resize(L *lua.LState) int {
	clipID := L.CheckInt64(1)
	targetWidth := L.CheckInt(2)
	targetHeight := L.CheckInt(3)

	if targetWidth <= 0 || targetHeight <= 0 {
		L.Push(lua.LNil)
		L.Push(lua.LString("width and height must be positive"))
		return 2
	}
	if targetWidth > 10000 || targetHeight > 10000 {
		L.Push(lua.LNil)
		L.Push(lua.LString("dimensions too large (max 10000x10000)"))
		return 2
	}

	src, _, err := img.loadClipImage(clipID)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	fit := "fill"
	if L.GetTop() >= 4 {
		if opts, ok := L.Get(4).(*lua.LTable); ok {
			if f := opts.RawGetString("fit"); f != lua.LNil {
				fit = f.String()
			}
		}
	}

	var dst *image.RGBA
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	switch fit {
	case "contain":
		// Scale to fit within target, maintaining aspect ratio
		scaleX := float64(targetWidth) / float64(srcW)
		scaleY := float64(targetHeight) / float64(srcH)
		scale := math.Min(scaleX, scaleY)
		newW := int(math.Round(float64(srcW) * scale))
		newH := int(math.Round(float64(srcH) * scale))
		if newW < 1 {
			newW = 1
		}
		if newH < 1 {
			newH = 1
		}
		dst = resizeImage(src, newW, newH)

	case "cover":
		// Scale to cover target, maintaining aspect ratio, then crop center
		scaleX := float64(targetWidth) / float64(srcW)
		scaleY := float64(targetHeight) / float64(srcH)
		scale := math.Max(scaleX, scaleY)
		newW := int(math.Round(float64(srcW) * scale))
		newH := int(math.Round(float64(srcH) * scale))
		if newW < 1 {
			newW = 1
		}
		if newH < 1 {
			newH = 1
		}
		scaled := resizeImage(src, newW, newH)
		// Crop to target from center
		offsetX := (newW - targetWidth) / 2
		offsetY := (newH - targetHeight) / 2
		dst = image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
		draw.Draw(dst, dst.Bounds(), scaled, image.Pt(offsetX, offsetY), draw.Src)

	case "fill":
		dst = resizeImage(src, targetWidth, targetHeight)

	default:
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("unknown fit mode %q (valid: fill, contain, cover)", fit)))
		return 2
	}

	data, mime, err := EncodeImagePNG(dst)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	result := L.NewTable()
	result.RawSetString("data", lua.LString(data))
	result.RawSetString("mime_type", lua.LString(mime))
	L.Push(result)
	return 1
}

// overlayText draws text on an image
func (img *ImageAPI) overlayText(L *lua.LState) int {
	clipID := L.CheckInt64(1)
	opts := L.CheckTable(2)

	textVal := opts.RawGetString("text")
	if textVal == lua.LNil {
		L.Push(lua.LNil)
		L.Push(lua.LString("text is required"))
		return 2
	}
	text := textVal.String()
	if len(text) > 10000 {
		L.Push(lua.LNil)
		L.Push(lua.LString("text too long (max 10000 characters)"))
		return 2
	}

	// Parse and validate font size before loading the image
	fontSize := 24.0
	if s := opts.RawGetString("size"); s != lua.LNil {
		if num, ok := s.(lua.LNumber); ok {
			fontSize = float64(num)
		}
	}

	if fontSize <= 0 {
		L.Push(lua.LNil)
		L.Push(lua.LString("font size must be positive"))
		return 2
	}
	if fontSize > 1000 {
		L.Push(lua.LNil)
		L.Push(lua.LString("font size too large (max 1000)"))
		return 2
	}

	src, _, err := img.loadClipImage(clipID)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)

	textColor := color.RGBA{R: 255, G: 255, B: 255, A: 255} // white default
	if c := opts.RawGetString("color"); c != lua.LNil {
		var colorErr error
		textColor, colorErr = parseHexColor(c.String())
		if colorErr != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid color: " + colorErr.Error()))
			return 2
		}
	}

	opacity := 1.0
	if o := opts.RawGetString("opacity"); o != lua.LNil {
		if num, ok := o.(lua.LNumber); ok {
			opacity = float64(num)
			if opacity < 0 {
				opacity = 0
			}
			if opacity > 1 {
				opacity = 1
			}
		}
	}
	textColor.A = uint8(float64(textColor.A) * opacity)

	face, err := getFont(fontSize)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	defer face.Close()

	// Calculate text position
	x := 0
	y := 0

	// Measure text for position keywords
	textAdvance := font.MeasureString(face, text)
	textWidth := textAdvance.Ceil()
	metrics := face.Metrics()
	textHeight := metrics.Ascent.Ceil()

	imgW := bounds.Dx()
	imgH := bounds.Dy()
	padding := 10

	// Check for position keyword
	if pos := opts.RawGetString("position"); pos != lua.LNil {
		position := pos.String()
		switch position {
		case "center":
			x = (imgW - textWidth) / 2
			y = (imgH + textHeight) / 2
		case "top":
			x = (imgW - textWidth) / 2
			y = textHeight + padding
		case "bottom":
			x = (imgW - textWidth) / 2
			y = imgH - padding
		case "top-left":
			x = padding
			y = textHeight + padding
		case "top-right":
			x = imgW - textWidth - padding
			y = textHeight + padding
		case "bottom-left":
			x = padding
			y = imgH - padding
		case "bottom-right":
			x = imgW - textWidth - padding
			y = imgH - padding
		default:
			L.Push(lua.LNil)
			L.Push(lua.LString(fmt.Sprintf("unknown position %q (valid: center, top, bottom, top-left, top-right, bottom-left, bottom-right)", position)))
			return 2
		}
	} else {
		// Use explicit x, y
		if xVal := opts.RawGetString("x"); xVal != lua.LNil {
			if num, ok := xVal.(lua.LNumber); ok {
				x = int(num)
			}
		}
		if yVal := opts.RawGetString("y"); yVal != lua.LNil {
			if num, ok := yVal.(lua.LNumber); ok {
				y = int(num)
			}
		}
	}

	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(textColor),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)

	data, mime, err := EncodeImagePNG(dst)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	result := L.NewTable()
	result.RawSetString("data", lua.LString(data))
	result.RawSetString("mime_type", lua.LString(mime))
	L.Push(result)
	return 1
}

// composite creates a new image by layering multiple clip images
func (img *ImageAPI) composite(L *lua.LState) int {
	opts := L.CheckTable(1)

	widthVal := opts.RawGetString("width")
	heightVal := opts.RawGetString("height")
	if widthVal == lua.LNil || heightVal == lua.LNil {
		L.Push(lua.LNil)
		L.Push(lua.LString("width and height are required"))
		return 2
	}

	widthNum, ok := widthVal.(lua.LNumber)
	if !ok {
		L.Push(lua.LNil)
		L.Push(lua.LString("width must be a number"))
		return 2
	}
	heightNum, ok := heightVal.(lua.LNumber)
	if !ok {
		L.Push(lua.LNil)
		L.Push(lua.LString("height must be a number"))
		return 2
	}
	canvasWidth := int(widthNum)
	canvasHeight := int(heightNum)
	if canvasWidth <= 0 || canvasHeight <= 0 || canvasWidth > 10000 || canvasHeight > 10000 {
		L.Push(lua.LNil)
		L.Push(lua.LString("invalid canvas dimensions"))
		return 2
	}

	// Create canvas with background color
	canvas := image.NewRGBA(image.Rect(0, 0, canvasWidth, canvasHeight))
	bgColor := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	if bg := opts.RawGetString("background"); bg != lua.LNil {
		var bgErr error
		bgColor, bgErr = parseHexColor(bg.String())
		if bgErr != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid background color: " + bgErr.Error()))
			return 2
		}
	}
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(bgColor), image.Point{}, draw.Src)

	// Draw layers
	layersVal := opts.RawGetString("layers")
	if layersVal == lua.LNil {
		L.Push(lua.LNil)
		L.Push(lua.LString("layers are required"))
		return 2
	}
	layers, ok := layersVal.(*lua.LTable)
	if !ok {
		L.Push(lua.LNil)
		L.Push(lua.LString("layers must be an array"))
		return 2
	}

	layerCount := layers.Len()
	if layerCount > 50 {
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("too many layers (%d, max 50)", layerCount)))
		return 2
	}

	// layerErr captures the first error from the ForEach closure since
	// gopher-lua's ForEach doesn't support error returns. Subsequent
	// iterations short-circuit via the early-return check.
	var layerErr error
	layers.ForEach(func(_, v lua.LValue) {
		if layerErr != nil {
			return
		}
		layer, ok := v.(*lua.LTable)
		if !ok {
			layerErr = fmt.Errorf("each layer must be a table")
			return
		}

		clipIDVal := layer.RawGetString("clip_id")
		if clipIDVal == lua.LNil {
			layerErr = fmt.Errorf("clip_id is required for each layer")
			return
		}
		clipIDNum, ok := clipIDVal.(lua.LNumber)
		if !ok {
			layerErr = fmt.Errorf("clip_id must be a number")
			return
		}
		clipID := int64(clipIDNum)

		layerImg, _, err := img.loadClipImage(clipID)
		if err != nil {
			layerErr = err
			return
		}

		lx, ly := 0, 0
		if xVal := layer.RawGetString("x"); xVal != lua.LNil {
			if num, ok := xVal.(lua.LNumber); ok {
				lx = int(num)
			}
		}
		if yVal := layer.RawGetString("y"); yVal != lua.LNil {
			if num, ok := yVal.(lua.LNumber); ok {
				ly = int(num)
			}
		}

		// Resize layer if dimensions specified
		lw := layerImg.Bounds().Dx()
		lh := layerImg.Bounds().Dy()
		if wVal := layer.RawGetString("width"); wVal != lua.LNil {
			if num, ok := wVal.(lua.LNumber); ok {
				lw = int(num)
			}
		}
		if hVal := layer.RawGetString("height"); hVal != lua.LNil {
			if num, ok := hVal.(lua.LNumber); ok {
				lh = int(num)
			}
		}

		if lw <= 0 || lh <= 0 || lw > 10000 || lh > 10000 {
			layerErr = fmt.Errorf("invalid layer dimensions: %dx%d (must be 1-10000)", lw, lh)
			return
		}

		if lw != layerImg.Bounds().Dx() || lh != layerImg.Bounds().Dy() {
			layerImg = resizeImage(layerImg, lw, lh)
		}

		draw.Draw(canvas, image.Rect(lx, ly, lx+lw, ly+lh), layerImg, layerImg.Bounds().Min, draw.Over)
	})

	if layerErr != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(layerErr.Error()))
		return 2
	}

	data, mime, err := EncodeImagePNG(canvas)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	result := L.NewTable()
	result.RawSetString("data", lua.LString(data))
	result.RawSetString("mime_type", lua.LString(mime))
	L.Push(result)
	return 1
}

// dominantColors extracts dominant colors using k-means clustering
func (img *ImageAPI) dominantColors(L *lua.LState) int {
	clipID := L.CheckInt64(1)
	count := L.OptInt(2, 5)
	if count < 1 {
		count = 1
	}
	if count > 20 {
		count = 20
	}

	src, _, err := img.loadClipImage(clipID)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	// Downsample to max 100x100 for performance
	bounds := src.Bounds()
	sampleW := bounds.Dx()
	sampleH := bounds.Dy()
	if sampleW > 100 || sampleH > 100 {
		scale := math.Min(100.0/float64(sampleW), 100.0/float64(sampleH))
		sampleW = int(math.Max(1, math.Round(float64(sampleW)*scale)))
		sampleH = int(math.Max(1, math.Round(float64(sampleH)*scale)))
		src = resizeImage(src, sampleW, sampleH)
		bounds = src.Bounds()
	}

	// Collect pixels
	type pixel struct {
		r, g, b float64
	}
	var pixels []pixel
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			if a < 128*256 {
				continue // Skip mostly transparent
			}
			pixels = append(pixels, pixel{float64(r >> 8), float64(g >> 8), float64(b >> 8)})
		}
	}

	if len(pixels) == 0 {
		L.Push(L.NewTable())
		return 1
	}

	if count > len(pixels) {
		count = len(pixels)
	}

	// K-means clustering
	rng := rand.New(rand.NewSource(42))
	centroids := make([]pixel, count)
	for i := range centroids {
		centroids[i] = pixels[rng.Intn(len(pixels))]
	}

	assignments := make([]int, len(pixels))

	for iter := 0; iter < 10; iter++ {
		// Assign pixels to nearest centroid
		for i, p := range pixels {
			minDist := math.MaxFloat64
			for j, c := range centroids {
				dr := p.r - c.r
				dg := p.g - c.g
				db := p.b - c.b
				dist := dr*dr + dg*dg + db*db
				if dist < minDist {
					minDist = dist
					assignments[i] = j
				}
			}
		}

		// Recompute centroids
		sums := make([]pixel, count)
		counts := make([]int, count)
		for i, p := range pixels {
			c := assignments[i]
			sums[c].r += p.r
			sums[c].g += p.g
			sums[c].b += p.b
			counts[c]++
		}
		for j := range centroids {
			if counts[j] > 0 {
				centroids[j] = pixel{
					r: sums[j].r / float64(counts[j]),
					g: sums[j].g / float64(counts[j]),
					b: sums[j].b / float64(counts[j]),
				}
			}
		}
	}

	// Sort by cluster size (descending)
	clusterCounts := make([]int, count)
	for _, a := range assignments {
		clusterCounts[a]++
	}

	type colorEntry struct {
		c     pixel
		count int
	}
	entries := make([]colorEntry, count)
	for i, c := range centroids {
		entries[i] = colorEntry{c, clusterCounts[i]}
	}
	// Simple insertion sort
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].count > entries[j-1].count; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}

	result := L.NewTable()
	for _, e := range entries {
		if e.count == 0 {
			continue
		}
		hex := fmt.Sprintf("#%02x%02x%02x", int(math.Round(e.c.r)), int(math.Round(e.c.g)), int(math.Round(e.c.b)))
		result.Append(lua.LString(hex))
	}

	L.Push(result)
	return 1
}

// grayscalePixels returns a flat array of luminance values.
// NOTE: Creates one Lua table entry per pixel (up to 1M entries at the max of
// 1000x1000). Each entry is a lua.LNumber (~24 bytes), so the max theoretical
// memory usage is ~24MB for the Lua table alone. Callers should use the
// smallest dimensions that meet their needs.
func (img *ImageAPI) grayscalePixels(L *lua.LState) int {
	clipID := L.CheckInt64(1)
	targetWidth := L.CheckInt(2)
	targetHeight := L.CheckInt(3)

	if targetWidth <= 0 || targetHeight <= 0 {
		L.Push(lua.LNil)
		L.Push(lua.LString("width and height must be positive"))
		return 2
	}
	if targetWidth*targetHeight > 1000000 {
		L.Push(lua.LNil)
		L.Push(lua.LString("total pixels too large (max 1000000)"))
		return 2
	}

	src, _, err := img.loadClipImage(clipID)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	resized := resizeImage(src, targetWidth, targetHeight)

	result := L.NewTable()
	for y := 0; y < targetHeight; y++ {
		for x := 0; x < targetWidth; x++ {
			r, g, b, _ := resized.At(x, y).RGBA()
			// ITU-R BT.601 luminance
			lum := 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(b>>8)
			result.Append(lua.LNumber(int(math.Round(lum))))
		}
	}

	L.Push(result)
	return 1
}

// metadata extracts EXIF metadata from an image
func (img *ImageAPI) metadata(L *lua.LState) int {
	clipID := L.CheckInt64(1)

	rawData, _, err := img.loadClipImageRaw(clipID)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	x, err := exif.Decode(bytes.NewReader(rawData))
	if err != nil {
		// No EXIF data (common for PNGs) - return empty table instead of error
		L.Push(L.NewTable())
		return 1
	}

	result := L.NewTable()

	// Helper to get string tag
	getString := func(field exif.FieldName) string {
		tag, err := x.Get(field)
		if err != nil {
			return ""
		}
		s, err := tag.StringVal()
		if err != nil {
			return tag.String()
		}
		return s
	}

	// Helper to get rational tag as float
	getRational := func(field exif.FieldName) (float64, bool) {
		tag, err := x.Get(field)
		if err != nil {
			return 0, false
		}
		num, den, err := tag.Rat2(0)
		if err != nil {
			return 0, false
		}
		if den == 0 {
			return 0, false
		}
		return float64(num) / float64(den), true
	}

	if v := getString(exif.Make); v != "" {
		result.RawSetString("camera_make", lua.LString(v))
	}
	if v := getString(exif.Model); v != "" {
		result.RawSetString("camera_model", lua.LString(v))
	}
	if v := getString(exif.LensModel); v != "" {
		result.RawSetString("lens", lua.LString(v))
	}

	if v, ok := getRational(exif.ISOSpeedRatings); ok {
		result.RawSetString("iso", lua.LNumber(v))
	}
	if v, ok := getRational(exif.FNumber); ok {
		result.RawSetString("aperture", lua.LNumber(v))
	}
	if v, ok := getRational(exif.ExposureTime); ok {
		result.RawSetString("shutter_speed", lua.LNumber(v))
	}
	if v, ok := getRational(exif.FocalLength); ok {
		result.RawSetString("focal_length", lua.LNumber(v))
	}

	if tm, err := x.DateTime(); err == nil {
		result.RawSetString("date", lua.LString(tm.Format("2006-01-02T15:04:05")))
	}

	// GPS
	lat, lon, err := x.LatLong()
	if err == nil {
		gps := L.NewTable()
		gps.RawSetString("latitude", lua.LNumber(lat))
		gps.RawSetString("longitude", lua.LNumber(lon))
		result.RawSetString("gps", gps)
	}

	L.Push(result)
	return 1
}

// convert re-encodes a clip image in a different format (png, jpeg, jpg)
func (img *ImageAPI) convert(L *lua.LState) int {
	clipID := L.CheckInt64(1)
	format := L.CheckString(2)

	quality := 85
	if L.GetTop() >= 3 {
		if opts, ok := L.Get(3).(*lua.LTable); ok {
			if q := opts.RawGetString("quality"); q != lua.LNil {
				if num, ok := q.(lua.LNumber); ok {
					quality = int(num)
					if quality < 1 {
						quality = 1
					}
					if quality > 100 {
						quality = 100
					}
				}
			}
		}
	}

	src, _, err := img.loadClipImage(clipID)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	data, mimeType, err := EncodeImage(src, format, quality)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	result := L.NewTable()
	result.RawSetString("data", lua.LString(data))
	result.RawSetString("mime_type", lua.LString(mimeType))
	L.Push(result)
	return 1
}

// DiffImages compares two images pixel-by-pixel and returns a diff image + similarity score.
// threshold controls sensitivity: lower = more differences shown (typical range 10-50, default 30).
func DiffImages(imgA, imgB image.Image, threshold float64) (diffImg *image.RGBA, similarity float64) {
	boundsA := imgA.Bounds()
	w := boundsA.Dx()
	h := boundsA.Dy()

	if w > 2000 || h > 2000 {
		scale := math.Min(2000.0/float64(w), 2000.0/float64(h))
		w = int(math.Max(1, math.Round(float64(w)*scale)))
		h = int(math.Max(1, math.Round(float64(h)*scale)))
	}

	var a, b *image.RGBA
	if imgA.Bounds().Dx() != w || imgA.Bounds().Dy() != h {
		a = resizeImage(imgA, w, h)
	} else {
		a = convertToRGBA(imgA)
	}
	if imgB.Bounds().Dx() != w || imgB.Bounds().Dy() != h {
		b = resizeImage(imgB, w, h)
	} else {
		b = convertToRGBA(imgB)
	}

	diffImg = image.NewRGBA(image.Rect(0, 0, w, h))
	totalDiff := 0.0
	totalPixels := float64(w * h)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ra, ga, ba, _ := a.At(x, y).RGBA()
			rb, gb, bb, _ := b.At(x, y).RGBA()

			dr := math.Abs(float64(ra>>8) - float64(rb>>8))
			dg := math.Abs(float64(ga>>8) - float64(gb>>8))
			db := math.Abs(float64(ba>>8) - float64(bb>>8))

			avgDiff := (dr + dg + db) / 3.0
			totalDiff += avgDiff / 255.0

			if avgDiff > threshold {
				intensity := uint8(math.Min(255, avgDiff*3))
				diffImg.SetRGBA(x, y, color.RGBA{R: intensity, G: 0, B: 0, A: 255})
			} else {
				diffImg.SetRGBA(x, y, color.RGBA{
					R: uint8(ra >> 9),
					G: uint8(ga >> 9),
					B: uint8(ba >> 9),
					A: 255,
				})
			}
		}
	}

	similarity = 1.0 - (totalDiff / totalPixels)
	if similarity < 0 {
		similarity = 0
	}
	return diffImg, similarity
}

// diff compares two images and returns similarity score and diff image
func (img *ImageAPI) diff(L *lua.LState) int {
	clipIDA := L.CheckInt64(1)
	clipIDB := L.CheckInt64(2)

	imgA, _, err := img.loadClipImage(clipIDA)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("image A: " + err.Error()))
		return 2
	}

	imgB, _, err := img.loadClipImage(clipIDB)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("image B: " + err.Error()))
		return 2
	}

	diffImg, similarity := DiffImages(imgA, imgB, 30.0)

	diffData, diffMime, err := EncodeImagePNG(diffImg)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	result := L.NewTable()
	result.RawSetString("similarity", lua.LNumber(similarity))
	result.RawSetString("diff_data", lua.LString(diffData))
	result.RawSetString("diff_mime_type", lua.LString(diffMime))
	L.Push(result)
	return 1
}
