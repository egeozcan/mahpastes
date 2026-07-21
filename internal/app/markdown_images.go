package app

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"mime"
	"net/http"
)

const (
	maxMarkdownImageBytes  = 15 * 1024 * 1024
	maxMarkdownImagePixels = 25_000_000
	maxMarkdownImageSide   = 8192
	maxMarkdownGIFFrames   = 200
)

var supportedMarkdownImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// MarkdownImageData is validated raster data suitable for inline display.
type MarkdownImageData struct {
	ClipID      int64  `json:"clip_id"`
	ContentType string `json:"content_type"`
	Data        string `json:"data"`
	Size        int64  `json:"size"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	DecodedSize int64  `json:"decoded_size"`
}

// GetMarkdownImage returns a validated local clip image without trusting its
// extension or declared MIME type alone.
func (a *App) GetMarkdownImage(clipID int64) (MarkdownImageData, error) {
	var data []byte
	var contentType string
	if err := a.db.QueryRow(`
		SELECT data, content_type FROM clips
		WHERE id = ? AND (expires_at IS NULL OR datetime(expires_at) > CURRENT_TIMESTAMP)`, clipID).Scan(&data, &contentType); err != nil {
		return MarkdownImageData{}, fmt.Errorf("get Markdown image: %w", err)
	}

	validatedType, width, height, decodedSize, err := validateMarkdownImage(data, contentType)
	if err != nil {
		return MarkdownImageData{}, err
	}
	return MarkdownImageData{
		ClipID:      clipID,
		ContentType: validatedType,
		Data:        base64.StdEncoding.EncodeToString(data),
		Size:        int64(len(data)),
		Width:       width,
		Height:      height,
		DecodedSize: decodedSize,
	}, nil
}

func validateMarkdownImage(data []byte, declaredType string) (string, int, int, int64, error) {
	if len(data) > maxMarkdownImageBytes {
		return "", 0, 0, 0, fmt.Errorf("image exceeds %d byte limit", maxMarkdownImageBytes)
	}
	mediaType, _, err := mime.ParseMediaType(declaredType)
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("invalid image content type")
	}
	if !supportedMarkdownImageTypes[mediaType] {
		return "", 0, 0, 0, fmt.Errorf("unsupported Markdown image type %q", mediaType)
	}

	detectedType := http.DetectContentType(data)
	if detectedType != mediaType {
		return "", 0, 0, 0, fmt.Errorf("image bytes are %q, not declared %q", detectedType, mediaType)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("decode image header: %w", err)
	}
	formatType := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp"}[format]
	if formatType != mediaType {
		return "", 0, 0, 0, fmt.Errorf("decoded image format %q does not match %q", format, mediaType)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxMarkdownImageSide || config.Height > maxMarkdownImageSide {
		return "", 0, 0, 0, fmt.Errorf("image dimensions %dx%d exceed limit", config.Width, config.Height)
	}
	if int64(config.Width)*int64(config.Height) > maxMarkdownImagePixels {
		return "", 0, 0, 0, fmt.Errorf("image exceeds %d pixel limit", maxMarkdownImagePixels)
	}
	decodedSize := int64(config.Width) * int64(config.Height) * 4
	if mediaType == "image/png" && isAnimatedPNG(data) {
		return "", 0, 0, 0, fmt.Errorf("animated PNG is not supported in Markdown previews")
	}
	if mediaType == "image/webp" && isAnimatedWebP(data) {
		return "", 0, 0, 0, fmt.Errorf("animated WebP is not supported in Markdown previews")
	}
	if mediaType == "image/gif" {
		frames, err := countGIFFrames(data, maxMarkdownGIFFrames+1)
		if err != nil {
			return "", 0, 0, 0, fmt.Errorf("inspect GIF frames: %w", err)
		}
		if frames > maxMarkdownGIFFrames {
			return "", 0, 0, 0, fmt.Errorf("GIF exceeds %d frame limit", maxMarkdownGIFFrames)
		}
		if int64(config.Width)*int64(config.Height)*int64(frames) > maxMarkdownImagePixels {
			return "", 0, 0, 0, fmt.Errorf("GIF exceeds cumulative decoded-pixel limit")
		}
		decodedSize *= int64(frames)
	}
	return mediaType, config.Width, config.Height, decodedSize, nil
}

func isAnimatedPNG(data []byte) bool {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		return false
	}
	for pos := 8; pos+12 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		if length < 0 || pos+12+length > len(data) {
			return false
		}
		if string(data[pos+4:pos+8]) == "acTL" {
			return true
		}
		pos += 12 + length
	}
	return false
}

func isAnimatedWebP(data []byte) bool {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return false
	}
	for pos := 12; pos+8 <= len(data); {
		chunkType := string(data[pos : pos+4])
		length := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		if length < 0 || pos+8+length > len(data) {
			return false
		}
		if chunkType == "ANIM" || chunkType == "ANMF" || (chunkType == "VP8X" && length > 0 && data[pos+8]&0x02 != 0) {
			return true
		}
		pos += 8 + length
		if length%2 != 0 {
			pos++
		}
	}
	return false
}

// countGIFFrames walks GIF blocks without decoding pixels, so a frame bomb is
// rejected before allocating every frame.
func countGIFFrames(data []byte, stopAfter int) (int, error) {
	if len(data) < 13 || (string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a") {
		return 0, fmt.Errorf("invalid GIF header")
	}
	pos := 13
	packed := data[10]
	if packed&0x80 != 0 {
		pos += 3 * (1 << ((packed & 0x07) + 1))
	}
	frames := 0
	for pos < len(data) {
		switch data[pos] {
		case 0x3b:
			return frames, nil
		case 0x2c:
			if pos+10 > len(data) {
				return 0, fmt.Errorf("truncated image descriptor")
			}
			localPacked := data[pos+9]
			pos += 10
			if localPacked&0x80 != 0 {
				pos += 3 * (1 << ((localPacked & 0x07) + 1))
			}
			if pos >= len(data) {
				return 0, fmt.Errorf("truncated image data")
			}
			pos++ // LZW minimum code size
			var err error
			pos, err = skipGIFSubBlocks(data, pos)
			if err != nil {
				return 0, err
			}
			frames++
			if frames >= stopAfter {
				return frames, nil
			}
		case 0x21:
			if pos+2 > len(data) {
				return 0, fmt.Errorf("truncated extension")
			}
			pos += 2 // extension introducer and label
			var err error
			pos, err = skipGIFSubBlocks(data, pos)
			if err != nil {
				return 0, err
			}
		default:
			return 0, fmt.Errorf("unknown GIF block 0x%x", data[pos])
		}
	}
	return 0, fmt.Errorf("missing GIF trailer")
}

func skipGIFSubBlocks(data []byte, pos int) (int, error) {
	for {
		if pos >= len(data) {
			return 0, fmt.Errorf("truncated sub-block")
		}
		size := int(data[pos])
		pos++
		if size == 0 {
			return pos, nil
		}
		if pos+size > len(data) {
			return 0, fmt.Errorf("truncated sub-block data")
		}
		pos += size
	}
}
