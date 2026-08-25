package imagemeta

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

// EXIF tag numbers used by the fixture builder.
const (
	tagMake             = 0x010F
	tagModel            = 0x0110
	tagExifIFDPointer   = 0x8769
	tagFNumber          = 0x829D
	tagISOSpeedRatings  = 0x8827
	tagDateTimeOriginal = 0x9003
)

// TIFF field types.
const (
	typeASCII    = 2
	typeShort    = 3
	typeLong     = 4
	typeRational = 5
)

// buildEXIFJPEG hand-assembles a JPEG carrying an APP1/EXIF block. A real photo
// would be a simpler fixture, but committing a binary just to assert four tags
// makes the expected values invisible to the reader — here every value under
// test is written a few lines above the assertion that checks it.
func buildEXIFJPEG() []byte {
	be := binary.BigEndian

	entry := func(tag, typ uint16, count uint32, value []byte) []byte {
		b := make([]byte, 12)
		be.PutUint16(b[0:2], tag)
		be.PutUint16(b[2:4], typ)
		be.PutUint32(b[4:8], count)
		copy(b[8:12], value) // inline value, or a 4-byte offset into the TIFF block
		return b
	}
	offsetBytes := func(v uint32) []byte {
		b := make([]byte, 4)
		be.PutUint32(b, v)
		return b
	}
	shortInline := func(v uint16) []byte {
		// A SHORT occupies the first two bytes of the value field, left-aligned.
		b := make([]byte, 4)
		be.PutUint16(b[0:2], v)
		return b
	}

	makeVal := []byte("TestCam\x00")
	modelVal := []byte("MX-1\x00")
	dateVal := []byte("2024:06:01 12:30:45\x00") // 20 bytes, the EXIF-mandated width

	// Offsets are relative to the start of the TIFF header. IFD0 sits right
	// after the 8-byte header; each IFD is 2 + 12*n + 4 bytes.
	const ifd0Off = 8
	ifd0Size := uint32(2 + 12*3 + 4)
	makeOff := ifd0Off + ifd0Size
	modelOff := makeOff + uint32(len(makeVal))
	exifIFDOff := modelOff + uint32(len(modelVal))
	exifIFDSize := uint32(2 + 12*3 + 4)
	fNumberOff := exifIFDOff + exifIFDSize
	dateOff := fNumberOff + 8 // one RATIONAL == two LONGs

	var tiff bytes.Buffer
	tiff.WriteString("MM")           // big-endian
	tiff.Write([]byte{0x00, 0x2A})   // TIFF magic
	tiff.Write(offsetBytes(ifd0Off)) // offset of IFD0

	// --- IFD0 ---
	binary.Write(&tiff, be, uint16(3))
	tiff.Write(entry(tagMake, typeASCII, uint32(len(makeVal)), offsetBytes(makeOff)))
	tiff.Write(entry(tagModel, typeASCII, uint32(len(modelVal)), offsetBytes(modelOff)))
	tiff.Write(entry(tagExifIFDPointer, typeLong, 1, offsetBytes(exifIFDOff)))
	tiff.Write(offsetBytes(0)) // no IFD1

	tiff.Write(makeVal)
	tiff.Write(modelVal)

	// --- Exif sub-IFD ---
	binary.Write(&tiff, be, uint16(3))
	tiff.Write(entry(tagFNumber, typeRational, 1, offsetBytes(fNumberOff)))
	tiff.Write(entry(tagISOSpeedRatings, typeShort, 1, shortInline(400)))
	tiff.Write(entry(tagDateTimeOriginal, typeASCII, uint32(len(dateVal)), offsetBytes(dateOff)))
	tiff.Write(offsetBytes(0))

	binary.Write(&tiff, be, uint32(28)) // FNumber numerator
	binary.Write(&tiff, be, uint32(10)) // denominator -> f/2.8
	tiff.Write(dateVal)

	var jpeg bytes.Buffer
	jpeg.Write([]byte{0xFF, 0xD8}) // SOI
	jpeg.Write([]byte{0xFF, 0xE1}) // APP1
	// Segment length covers itself, the "Exif\0\0" preamble, and the TIFF block.
	binary.Write(&jpeg, be, uint16(2+6+tiff.Len()))
	jpeg.WriteString("Exif\x00\x00")
	jpeg.Write(tiff.Bytes())
	jpeg.Write([]byte{0xFF, 0xD9}) // EOI
	return jpeg.Bytes()
}

func TestExtractEXIFBytes(t *testing.T) {
	meta, err := ExtractEXIFBytes(buildEXIFJPEG())
	if err != nil {
		t.Fatalf("ExtractEXIFBytes: %v", err)
	}
	if meta == nil {
		t.Fatal("expected metadata, got nil")
	}

	if meta.CameraMake != "TestCam" {
		t.Errorf("CameraMake = %q, want %q", meta.CameraMake, "TestCam")
	}
	if meta.CameraModel != "MX-1" {
		t.Errorf("CameraModel = %q, want %q", meta.CameraModel, "MX-1")
	}
	if meta.Date != "2024-06-01T12:30:45" {
		t.Errorf("Date = %q, want %q", meta.Date, "2024-06-01T12:30:45")
	}
	if meta.Aperture == nil || *meta.Aperture != 2.8 {
		t.Errorf("Aperture = %v, want 2.8", meta.Aperture)
	}
	if meta.ISO == nil || *meta.ISO != 400 {
		t.Errorf("ISO = %v, want 400", meta.ISO)
	}

	// Absent tags must stay nil/empty rather than becoming zeros — the Lua
	// binding turns exactly these into omitted keys.
	if meta.Lens != "" {
		t.Errorf("Lens = %q, want empty", meta.Lens)
	}
	if meta.FocalLength != nil {
		t.Errorf("FocalLength = %v, want nil", meta.FocalLength)
	}
	if meta.GPS != nil {
		t.Errorf("GPS = %v, want nil", meta.GPS)
	}
	if meta.IsEmpty() {
		t.Error("IsEmpty() = true for a populated struct")
	}
}

// A missing EXIF block is the common case (PNGs, screenshots) and must read as
// "no metadata", never as an error — the Lua binding returns an empty table on
// this path and plugins branch on that.
func TestExtractEXIFNoBlock(t *testing.T) {
	cases := map[string][]byte{
		"empty":     nil,
		"not image": []byte("hello world, definitely not a jpeg"),
		"bare jpeg": {0xFF, 0xD8, 0xFF, 0xD9},
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			meta, err := ExtractEXIFBytes(data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if meta != nil {
				t.Fatalf("expected nil metadata, got %+v", meta)
			}
			if !meta.IsEmpty() {
				t.Error("IsEmpty() on nil receiver should be true")
			}
		})
	}
}

// EXIF lives in the file header, so callers bound the read with an
// io.LimitReader. A prefix large enough to hold the APP1 segment must still
// decode; a prefix that truncates it must degrade to "no metadata", not error.
func TestExtractEXIFLimitReader(t *testing.T) {
	full := buildEXIFJPEG()

	meta, err := ExtractEXIF(io.LimitReader(bytes.NewReader(full), int64(len(full))))
	if err != nil {
		t.Fatalf("full-length limit: %v", err)
	}
	if meta == nil || meta.CameraMake != "TestCam" {
		t.Fatalf("full-length limit lost the tags: %+v", meta)
	}

	truncated, err := ExtractEXIF(io.LimitReader(bytes.NewReader(full), 12))
	if err != nil {
		t.Fatalf("truncated prefix should not error, got %v", err)
	}
	if truncated != nil && !truncated.IsEmpty() {
		t.Fatalf("truncated prefix produced tags: %+v", truncated)
	}
}

func TestExtractEXIFNilReader(t *testing.T) {
	meta, err := ExtractEXIF(nil)
	if err != nil || meta != nil {
		t.Fatalf("ExtractEXIF(nil) = (%+v, %v), want (nil, nil)", meta, err)
	}
}

// A reader that always fails must not panic or hang.
func TestExtractEXIFBrokenReader(t *testing.T) {
	meta, err := ExtractEXIF(strings.NewReader(""))
	if err != nil || meta != nil {
		t.Fatalf("empty reader = (%+v, %v), want (nil, nil)", meta, err)
	}
}
