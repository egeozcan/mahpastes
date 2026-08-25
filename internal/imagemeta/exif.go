// Package imagemeta extracts EXIF metadata from raw image bytes.
//
// It exists as a leaf package because two callers need the same extraction from
// two different starting points: the Lua `image.metadata` binding in
// go-clipboard/plugin, which reads bytes back out of the clips table, and the
// folder-import wizard in internal/app, which reads a file that is not a clip
// yet. Neither package can import the other, so the shared logic lives here.
package imagemeta

import (
	"bytes"
	"io"

	"github.com/rwcarlsen/goexif/exif"
)

// GPS is a decoded coordinate pair, in signed decimal degrees.
type GPS struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// EXIF is the tag subset the app surfaces. Every field is optional. The
// numerics are pointers so that a genuine zero (ISO 0, focal length 0) stays
// distinguishable from an absent tag — callers render only what is present,
// and the Lua binding's contract is that missing tags are omitted keys rather
// than zero values.
type EXIF struct {
	CameraMake   string   `json:"camera_make,omitempty"`
	CameraModel  string   `json:"camera_model,omitempty"`
	Lens         string   `json:"lens,omitempty"`
	ISO          *float64 `json:"iso,omitempty"`
	Aperture     *float64 `json:"aperture,omitempty"`
	ShutterSpeed *float64 `json:"shutter_speed,omitempty"`
	FocalLength  *float64 `json:"focal_length,omitempty"`
	Date         string   `json:"date,omitempty"`
	GPS          *GPS     `json:"gps,omitempty"`
}

// DateLayout is the format Date is rendered in. Pinned as a constant because
// the Lua `image.metadata` API has always emitted exactly this shape and
// plugins parse it.
const DateLayout = "2006-01-02T15:04:05"

// IsEmpty reports whether no tag at all was recovered. A JPEG can carry an EXIF
// block with nothing the app cares about in it, which is worth rendering as
// "no metadata" rather than an empty table.
func (e *EXIF) IsEmpty() bool {
	if e == nil {
		return true
	}
	return e.CameraMake == "" && e.CameraModel == "" && e.Lens == "" &&
		e.ISO == nil && e.Aperture == nil && e.ShutterSpeed == nil &&
		e.FocalLength == nil && e.Date == "" && e.GPS == nil
}

// ExtractEXIF decodes EXIF tags from an image stream.
//
// A missing or unparseable EXIF block is not an error — it is the common case
// for PNGs and screenshots — so that returns (nil, nil). A non-nil error means
// the reader itself failed. Callers may pass an io.LimitReader: EXIF lives in
// the file header, so a bounded prefix is enough and keeps a very large TIFF
// from being read in full just to answer "what camera was this?".
func ExtractEXIF(r io.Reader) (*EXIF, error) {
	if r == nil {
		return nil, nil
	}

	x, err := exif.Decode(r)
	if err != nil {
		// Includes io.EOF from a truncated LimitReader prefix. Absent metadata,
		// not a failure.
		return nil, nil
	}

	// getString mirrors the original binding: fall back to the tag's own
	// String() when it is not a string-typed tag, so numeric-encoded makes and
	// models still render something.
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

	// getNumber reads a numeric tag as a float, accepting either encoding.
	//
	// Rat2 alone is not enough: it errors on anything that is not a RATIONAL,
	// and ISOSpeedRatings is a SHORT in practically every camera file. The
	// original binding used a rational-only helper for all four numerics, so
	// `iso` silently never appeared. The integer fallback is what makes it
	// work; the other three tags are genuine rationals and take the first path.
	getNumber := func(field exif.FieldName) *float64 {
		tag, err := x.Get(field)
		if err != nil {
			return nil
		}
		if num, den, err := tag.Rat2(0); err == nil {
			if den == 0 {
				return nil
			}
			v := float64(num) / float64(den)
			return &v
		}
		if i, err := tag.Int64(0); err == nil {
			v := float64(i)
			return &v
		}
		return nil
	}

	out := &EXIF{
		CameraMake:   getString(exif.Make),
		CameraModel:  getString(exif.Model),
		Lens:         getString(exif.LensModel),
		ISO:          getNumber(exif.ISOSpeedRatings),
		Aperture:     getNumber(exif.FNumber),
		ShutterSpeed: getNumber(exif.ExposureTime),
		FocalLength:  getNumber(exif.FocalLength),
	}

	if tm, err := x.DateTime(); err == nil {
		out.Date = tm.Format(DateLayout)
	}

	if lat, lon, err := x.LatLong(); err == nil {
		out.GPS = &GPS{Latitude: lat, Longitude: lon}
	}

	return out, nil
}

// ExtractEXIFBytes is ExtractEXIF over an in-memory buffer.
func ExtractEXIFBytes(b []byte) (*EXIF, error) {
	if len(b) == 0 {
		return nil, nil
	}
	return ExtractEXIF(bytes.NewReader(b))
}
