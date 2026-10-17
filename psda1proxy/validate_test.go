package psda1proxy

import (
	"bytes"
	"strings"
	"testing"
)

// Helper: create a valid PNG file (minimal 1×1 transparent PNG).
func makePNG() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41,
		0x54, 0x78, 0xDA, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
		0x00, 0x02, 0x00, 0x01, 0xE2, 0x21, 0xBC, 0x43,
		0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44,
		0xAE, 0x42, 0x60, 0x82,
	}
}

// Helper: create a valid JPEG file (minimal 1×1 black JPEG).
func makeJPEG() []byte {
	return []byte{
		0xFF, 0xD8, 0xFF, 0xE0,
		0x00, 0x10,
		'J', 'F', 'I', 'F', 0x00,
		0x01, 0x01,
		0x00,
		0x00, 0x01, 0x00, 0x01,
		0x00, 0x00,
		0xFF, 0xDB,
		0x00, 0x43,
		0x00,
		0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10,
		0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10,
	}
}

// Helper: create a valid WebP file (RIFF + WEBP).
func makeWebP() []byte {
	return []byte{
		'R', 'I', 'F', 'F',
		0x24, 0x00, 0x00, 0x00,
		'W', 'E', 'B', 'P',
		'V', 'P', '8', 0x00,
		0x1A, 0x00, 0x00, 0x00,
		0x2F,
		0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
}

// Helper: create an AVIF file (RIFF + ftyp + avif).
func makeAVIF() []byte {
	return []byte{
		'R', 'I', 'F', 'F',
		0x20, 0x00, 0x00, 0x00,
		'f', 't', 'y', 'p',
		'a', 'v', 'i', 'f',
		0x00, 0x00, 0x00, 0x00,
		'a', 'v', 'i', 'f', 'm', 'i', 'f', 0x31,
	}
}

// Helper: create a HEIC file (RIFF + ftyp + heic).
func makeHEIC() []byte {
	return []byte{
		'R', 'I', 'F', 'F',
		0x20, 0x00, 0x00, 0x00,
		'f', 't', 'y', 'p',
		'h', 'e', 'i', 'c',
		0x00, 0x00, 0x00, 0x00,
		'h', 'e', 'i', 'c', 'm', 'i', 'f', 0x31,
	}
}

// Helper: create a GIF file.
func makeGIF() []byte {
	return []byte{
		'G', 'I', 'F', 0x38, 0x39, 0x61,
		0x01, 0x00, 0x01, 0x00,
		0x80,
		0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
}

// Helper: create a BMP file header.
func makeBMP() []byte {
	return []byte{
		'B', 'M',
		0x36, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x36, 0x00, 0x00, 0x00,
		0x28, 0x00, 0x00, 0x00,
	}
}

func TestValidateImage_PNG(t *testing.T) {
	data := makePNG()
	format, ok := ValidateImageBuffer(data)
	if !ok {
		t.Fatal("expected PNG to be valid, got invalid")
	}
	if format != "png" {
		t.Errorf("format = %q, want \"png\"", format)
	}
}

func TestValidateImage_JPEG(t *testing.T) {
	data := makeJPEG()
	format, ok := ValidateImageBuffer(data)
	if !ok {
		t.Fatal("expected JPEG to be valid, got invalid")
	}
	if format != "jpg" {
		t.Errorf("format = %q, want \"jpg\"", format)
	}
}

func TestValidateImage_WebP(t *testing.T) {
	data := makeWebP()
	format, ok := ValidateImageBuffer(data)
	if !ok {
		t.Fatal("expected WebP to be valid, got invalid")
	}
	if format != "webp" {
		t.Errorf("format = %q, want \"webp\"", format)
	}
}

func TestValidateImage_AVIF_Rejected(t *testing.T) {
	data := makeAVIF()
	_, ok := ValidateImageBuffer(data)
	if ok {
		t.Error("expected AVIF to be rejected, got accepted")
	}
}

func TestValidateImage_HEIC_Rejected(t *testing.T) {
	data := makeHEIC()
	_, ok := ValidateImageBuffer(data)
	if ok {
		t.Error("expected HEIC to be rejected, got accepted")
	}
}

func TestValidateImage_GIF_Accepted(t *testing.T) {
	data := makeGIF()
	format, ok := ValidateImageBuffer(data)
	if !ok {
		t.Fatal("expected GIF to be valid, got invalid")
	}
	if format != "gif" {
		t.Errorf("format = %q, want \"gif\"", format)
	}
}

func TestValidateImage_GIF87a_Accepted(t *testing.T) {
	data := []byte{'G', 'I', 'F', '8', '7', 'a', 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00}
	format, ok := ValidateImageBuffer(data)
	if !ok {
		t.Fatal("expected GIF87a to be valid, got invalid")
	}
	if format != "gif" {
		t.Errorf("format = %q, want \"gif\"", format)
	}
}

func TestValidateImage_BMP_Accepted(t *testing.T) {
	data := makeBMP()
	format, ok := ValidateImageBuffer(data)
	if !ok {
		t.Fatal("expected BMP to be valid, got invalid")
	}
	if format != "bmp" {
		t.Errorf("format = %q, want \"bmp\"", format)
	}
}

func TestValidateImage_EmptyFile_Rejected(t *testing.T) {
	_, ok := ValidateImageBuffer([]byte{})
	if ok {
		t.Error("expected empty file to be rejected")
	}
}

func TestValidateImage_TooShort_Rejected(t *testing.T) {
	_, ok := ValidateImageBuffer([]byte{0x89, 0x50, 0x4E, 0x47})
	if ok {
		t.Error("expected truncated PNG to be rejected")
	}
}

func TestValidateImage_RIFF_NotWebP_Rejected(t *testing.T) {
	data := []byte{
		'R', 'I', 'F', 'F',
		0x20, 0x00, 0x00, 0x00,
		'W', 'A', 'V', 'E',
		0x00, 0x00, 0x00, 0x00,
	}
	_, ok := ValidateImageBuffer(data)
	if ok {
		t.Error("expected RIFF/WAVE to be rejected")
	}
}

func TestValidateImage_BufferReader(t *testing.T) {
	data := makePNG()
	format, ok := ValidateImage(bytes.NewReader(data))
	if !ok {
		t.Fatal("expected PNG to be valid")
	}
	if format != "png" {
		t.Errorf("format = %q, want \"png\"", format)
	}
}

func TestValidateImage_ErrInvalidFormat(t *testing.T) {
	if ErrInvalidFormat == nil {
		t.Fatal("ErrInvalidFormat must not be nil")
	}
	if !strings.Contains(ErrInvalidFormat.Error(), "webp") ||
		!strings.Contains(ErrInvalidFormat.Error(), "jpg") ||
		!strings.Contains(ErrInvalidFormat.Error(), "png") ||
		!strings.Contains(ErrInvalidFormat.Error(), "gif") ||
		!strings.Contains(ErrInvalidFormat.Error(), "bmp") {
		t.Error("ErrInvalidFormat message should mention webp, jpg, png, gif, bmp")
	}
	if !strings.Contains(ErrInvalidFormat.Error(), "avif") {
		t.Error("ErrInvalidFormat message should mention avif rejection")
	}
}
