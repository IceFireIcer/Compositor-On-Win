// Package rasterio decodes raster image files into premultiplied 8-bit
// sRGB bitmaps — the Go counterpart of the macOS ImageImporter (ticket 40).
// JPEG/PNG/TIFF decode here; EXIF orientation applies exactly as
// CIImage.oriented(forExifOrientation:) did. SVG rasterizes in the
// frontend and RAW/HEIC arrive with ticket 41, so neither is handled here.
package rasterio

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registered for image.Decode
	_ "image/png"  // registered for image.Decode
	"os"
	"path/filepath"
	"strings"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"

	_ "golang.org/x/image/tiff" // registered for image.Decode
)

// User-facing import failures, ported from ImageImportError.
var (
	ErrUnreadable  = errors.New("无法读取该图像：文件可能已损坏或不可用")
	ErrUnsupported = errors.New("请选择 JPEG、PNG、HEIC、TIFF 或 Photoshop（PSD）文件")
)

// TooLargeError reports the same refusal the original did: over the side
// limit or the remaining pixel budget.
type TooLargeError struct{ Msg string }

func (e *TooLargeError) Error() string { return e.Msg }

// NewTooLarge builds the refusal for one image (the message quotes the
// document budget, as the original's did).
func NewTooLarge(width, height, budget int) *TooLargeError {
	return &TooLargeError{Msg: fmt.Sprintf("导入超过当前 %.0f 百万像素文档预算或 %d 像素边长限制（图像 %d × %d）",
		float64(budget)/1e6, domain.MaxSide, width, height)}
}

func tooLarge(width, height, remaining int) error {
	return NewTooLarge(width, height, remaining)
}

// Result is one decoded image: the layer name (file base name), the
// premultiplied bitmap and the oriented dimensions.
type Result struct {
	Name   string
	Bitmap *render.Bitmap
	Width  int
	Height int
}

// Decode reads one image file into a premultiplied bitmap, refusing files
// beyond the side limit or the remaining pixel budget (the budget counts
// every pixel already stored in the document's layers, as the original's
// remainingPixels did). The dimensions checked are the stored ones, before
// the EXIF orientation turns them.
func Decode(path string, remainingPixels int) (*Result, error) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch ext {
	case "jpg", "jpeg", "png", "tif", "tiff":
	default:
		return nil, ErrUnsupported
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	if cfg.Width < 1 || cfg.Height < 1 {
		return nil, ErrUnreadable
	}
	if cfg.Width > domain.MaxSide || cfg.Height > domain.MaxSide || cfg.Width*cfg.Height > remainingPixels {
		return nil, tooLarge(cfg.Width, cfg.Height, remainingPixels)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	bmp := render.BitmapFromImage(img)
	bmp = ApplyOrientation(bmp, exifOrientation(format, data))
	return &Result{
		Name:   strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Bitmap: bmp,
		Width:  bmp.W,
		Height: bmp.H,
	}, nil
}

// exifOrientation reads the orientation where the container carries one:
// JPEG from its APP1 EXIF segment, TIFF from IFD0, PNG from an eXIf chunk.
// Anything unreadable stays 1 (upright).
func exifOrientation(format string, data []byte) int {
	switch format {
	case "jpeg":
		return jpegEXIFOrientation(data)
	case "tiff":
		return tiffOrientation(data)
	case "png":
		return pngEXIFOrientation(data)
	}
	return 1
}
