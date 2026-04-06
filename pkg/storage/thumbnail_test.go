package storage

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// newTestJPEG creates a JPEG image in memory with the given dimensions.
func newTestJPEG(t *testing.T, w, h int) *bytes.Buffer {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode test JPEG: %v", err)
	}
	return &buf
}

// newTestPNG creates a PNG image in memory with the given dimensions.
func newTestPNG(t *testing.T, w, h int) *bytes.Buffer {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 100, G: 200, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	return &buf
}

func TestGenerateThumbnail_ResizesLargeImage(t *testing.T) {
	src := newTestJPEG(t, 800, 600)

	reader, size, err := GenerateThumbnail(src, 300, 300)
	if err != nil {
		t.Fatalf("GenerateThumbnail: %v", err)
	}
	if size <= 0 {
		t.Fatal("expected positive thumbnail size")
	}

	// Decode the thumbnail and verify dimensions.
	img, _, err := image.Decode(reader)
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() > 300 || bounds.Dy() > 300 {
		t.Errorf("thumbnail too large: %dx%d", bounds.Dx(), bounds.Dy())
	}
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		t.Error("thumbnail has zero dimension")
	}
}

func TestGenerateThumbnail_SmallImageNoResize(t *testing.T) {
	src := newTestJPEG(t, 100, 80)

	reader, size, err := GenerateThumbnail(src, 300, 300)
	if err != nil {
		t.Fatalf("GenerateThumbnail: %v", err)
	}
	if size <= 0 {
		t.Fatal("expected positive size")
	}

	// Image should be re-encoded at original dimensions.
	img, _, err := image.Decode(reader)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 100 || bounds.Dy() != 80 {
		t.Errorf("expected 100x80, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestGenerateThumbnail_PNG(t *testing.T) {
	src := newTestPNG(t, 500, 400)

	reader, size, err := GenerateThumbnail(src, 200, 200)
	if err != nil {
		t.Fatalf("GenerateThumbnail: %v", err)
	}
	if size <= 0 {
		t.Fatal("expected positive size")
	}

	img, format, err := image.Decode(reader)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Thumbnails are always JPEG.
	if format != "jpeg" {
		t.Errorf("expected jpeg format, got %q", format)
	}
	bounds := img.Bounds()
	if bounds.Dx() > 200 || bounds.Dy() > 200 {
		t.Errorf("thumbnail too large: %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestGenerateThumbnail_InvalidInput(t *testing.T) {
	_, _, err := GenerateThumbnail(bytes.NewReader([]byte("not an image")), 300, 300)
	if err == nil {
		t.Fatal("expected error for invalid image data")
	}
}

func TestIsImageContentType(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{"image/jpeg", true},
		{"image/png", true},
		{"image/gif", true},
		{"image/webp", true},
		{"image/svg+xml", false},
		{"application/pdf", false},
		{"text/plain", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsImageContentType(tt.ct); got != tt.want {
			t.Errorf("IsImageContentType(%q) = %v, want %v", tt.ct, got, tt.want)
		}
	}
}

func TestThumbnailKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"site/private/uuid/photo.jpg", "site/private/uuid/photo_thumb.jpg"},
		{"site/public/abc/doc.png", "site/public/abc/doc_thumb.jpg"},
		{"noext", "noext_thumb.jpg"},
	}
	for _, tt := range tests {
		if got := ThumbnailKey(tt.input); got != tt.want {
			t.Errorf("ThumbnailKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFitDimensions(t *testing.T) {
	tests := []struct {
		srcW, srcH, maxW, maxH int
		wantW, wantH           int
	}{
		{800, 600, 300, 300, 300, 225},
		{600, 800, 300, 300, 225, 300},
		{100, 100, 300, 300, 300, 300}, // fitDimensions scales up; GenerateThumbnail guards against this
	}
	for _, tt := range tests {
		w, h := fitDimensions(tt.srcW, tt.srcH, tt.maxW, tt.maxH)
		if w != tt.wantW || h != tt.wantH {
			t.Errorf("fitDimensions(%d, %d, %d, %d) = (%d, %d), want (%d, %d)",
				tt.srcW, tt.srcH, tt.maxW, tt.maxH, w, h, tt.wantW, tt.wantH)
		}
	}
}
