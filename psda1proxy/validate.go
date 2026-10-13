package psda1proxy

import (
	"bytes"
	"fmt"
	"io"
)

// ValidateImage inspects the magic bytes at the start of the stream and
// returns the detected format ("png", "jpg", "webp", "gif") and true when
// the file is one of the four accepted types. Any other format – including
// AVIF, HEIC, BMP, TIFF – returns ("", false).
//
// This is a byte-level check; file extensions and Content-Type headers
// are intentionally ignored because they are trivially spoofable.
//
// Format signatures (RFC 2083 / JFIF / libwebp / GIF spec):
//
//	PNG    89 50 4E 47 0D 0A 1A 0A          (137,80,78,71,13,10,26,10)
//	JPEG   FF D8 FF                          (255,216,255)
//	WebP   52 49 46 4F [size:4] 57 45 42 50  ("RIFF" … "WEBP")
//	GIF87a 47 49 46 38 37 61                 ("GIF87a")
//	GIF89a 47 49 46 38 39 61                 ("GIF89a")
//	AVIF   52 49 46 4F [size:4] 66 74 79 70  ("RIFF" … "ftyp" … "avif")
//
// We read up to 16 bytes, which is enough to identify every supported
// format and to distinguish WebP from other RIFF-based codecs.
func ValidateImage(r io.Reader) (format string, ok bool) {
	buf := make([]byte, 16)
	n, _ := io.ReadFull(r, buf)

	switch {
	// PNG: 8-byte signature
	case n >= 8 &&
		buf[0] == 0x89 && buf[1] == 0x50 &&
		buf[2] == 0x4E && buf[3] == 0x47 &&
		buf[4] == 0x0D && buf[5] == 0x0A &&
		buf[6] == 0x1A && buf[7] == 0x0A:
		return "png", true

	// JPEG: 3-byte SOI marker
	case n >= 3 && buf[0] == 0xFF && buf[1] == 0xD8 && buf[2] == 0xFF:
		return "jpg", true

	// RIFF family: WebP vs. AVIF/HEIC/etc.
	case n >= 12 &&
		buf[0] == 'R' && buf[1] == 'I' &&
		buf[2] == 'F' && buf[3] == 'F':
		if buf[8] == 'W' && buf[9] == 'E' &&
			buf[10] == 'B' && buf[11] == 'P' {
			return "webp", true
		}
		// RIFF container but not WebP → AVIF, HEIC, WebM, etc.
		return "", false

	// GIF87a / GIF89a: 6-byte signature "GIF8?a"
	case n >= 6 &&
		buf[0] == 'G' && buf[1] == 'I' && buf[2] == 'F' && buf[3] == '8' &&
		(buf[4] == '7' || buf[4] == '9') && buf[5] == 'a':
		return "gif", true
	}

	return "", false
}

// ValidateImageBuffer is a convenience wrapper that validates an
// in-memory byte slice (useful in tests and in the Driver.Put method
// where the body has already been read into memory).
func ValidateImageBuffer(data []byte) (format string, ok bool) {
	return ValidateImage(bytes.NewReader(data))
}

// ErrInvalidFormat is returned when the uploaded file is not one of
// webp / jpg / png / gif.
var ErrInvalidFormat = fmt.Errorf("psda1proxy: invalid image format: only webp, jpg, png, gif are accepted (avif and other formats rejected)")
