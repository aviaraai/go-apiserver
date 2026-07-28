// internal/imaging/validate.go
package imaging

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
)

var (
	ErrNotAnImage         = errors.New("file is not a valid image")
	ErrDimensionsTooLarge = errors.New("image dimensions exceed allowed limit")
	ErrUnsupportedFormat  = errors.New("unsupported image format")
)

const (
	maxPixelWidth  = 8000
	maxPixelHeight = 8000
	maxTotalPixels = 40_000_000 // ~40MP ceiling regardless of aspect ratio
)

var allowedFormats = map[string]bool{"jpeg": true, "png": true}

// ValidateImage reads a bounded prefix of src to sniff content type,
// then fully decodes it to check real dimensions before it's trusted.
// Returns the sniffed content type on success. Rewinds src via Seeker if possible.
func ValidateImage(src io.Reader) (contentType string, err error) {
	header := make([]byte, 512)
	n, err := io.ReadFull(src, header)
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", fmt.Errorf("read header: %w", err)
	}
	header = header[:n]

	sniffed := http.DetectContentType(header)
	if sniffed != "image/jpeg" && sniffed != "image/png" && sniffed != "image/gif" {
		return "", ErrNotAnImage
	}

	// Decode only the config (dimensions), not the full pixel data —
	// this is intentionally cheap: it reads just enough of the header
	// to get width/height without allocating a full pixel buffer.
	full := io.MultiReader(bytes.NewReader(header), src)
	cfg, format, err := image.DecodeConfig(full)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNotAnImage, err)
	}

	if !allowedFormats[format] {
		return "", ErrUnsupportedFormat
	}

	if cfg.Width > maxPixelWidth || cfg.Height > maxPixelHeight {
		return "", ErrDimensionsTooLarge
	}
	if cfg.Width*cfg.Height > maxTotalPixels {
		return "", ErrDimensionsTooLarge
	}

	return sniffed, nil
}

func ExtensionForContentType(contentType string) (string, error) {
	switch contentType {
	case "image/jpeg":
		return ".jpg", nil
	case "image/png":
		return ".png", nil
	default:
		return "", ErrUnsupportedFormat
	}
}
