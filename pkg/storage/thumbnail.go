package storage

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // register PNG decoder
	"io"
	"path"
	"strings"

	_ "golang.org/x/image/webp" // register WebP decoder
	"golang.org/x/image/draw"
)

const (
	// DefaultThumbWidth is the default maximum thumbnail width in pixels.
	DefaultThumbWidth = 300
	// DefaultThumbHeight is the default maximum thumbnail height in pixels.
	DefaultThumbHeight = 300
)

// GenerateThumbnail decodes the image from reader, resizes it to fit within
// maxWidth x maxHeight preserving aspect ratio, and returns the result as a
// JPEG-encoded reader. Returns an error if the image cannot be decoded.
func GenerateThumbnail(reader io.Reader, maxWidth, maxHeight int) (io.Reader, int64, error) {
	src, _, err := image.Decode(reader)
	if err != nil {
		return nil, 0, fmt.Errorf("storage/thumbnail: decode: %w", err)
	}

	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	// If already within bounds, encode as-is.
	if srcW <= maxWidth && srcH <= maxHeight {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 85}); err != nil {
			return nil, 0, fmt.Errorf("storage/thumbnail: encode: %w", err)
		}
		return &buf, int64(buf.Len()), nil
	}

	// Calculate target dimensions preserving aspect ratio.
	dstW, dstH := fitDimensions(srcW, srcH, maxWidth, maxHeight)

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, 0, fmt.Errorf("storage/thumbnail: encode: %w", err)
	}
	return &buf, int64(buf.Len()), nil
}

// IsImageContentType returns true if the content type is a rasterizable image format.
func IsImageContentType(ct string) bool {
	switch ct {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	}
	return false
}

// ThumbnailKey returns the storage key for a thumbnail derived from originalKey.
// For example, "site/private/uuid/photo.jpg" becomes "site/private/uuid/photo_thumb.jpg".
func ThumbnailKey(originalKey string) string {
	ext := path.Ext(originalKey)
	base := strings.TrimSuffix(originalKey, ext)
	// Thumbnails are always JPEG.
	return base + "_thumb.jpg"
}

// fitDimensions calculates the largest dimensions that fit within maxW x maxH
// while preserving the aspect ratio of srcW x srcH.
func fitDimensions(srcW, srcH, maxW, maxH int) (int, int) {
	ratioW := float64(maxW) / float64(srcW)
	ratioH := float64(maxH) / float64(srcH)
	ratio := ratioW
	if ratioH < ratioW {
		ratio = ratioH
	}
	w := int(float64(srcW) * ratio)
	h := int(float64(srcH) * ratio)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}
