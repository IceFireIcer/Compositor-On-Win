// Package rasterio decodes raster image files into premultiplied sRGB
// bitmaps — the Go counterpart of the macOS ImageImporter (ticket 40).
// JPEG/PNG/TIFF/WebP decode here; a 16-bit source keeps its precision in
// the deep raster (ADR-0007). EXIF orientation applies exactly as
// CIImage.oriented(forExifOrientation:) did, to the deep raster as well as
// the 8-bit copy. OpenEXR and Radiance HDR decode through hdrio. SVG
// rasterizes in the frontend, RAW goes through the develop sheet and
// HEIC/HEIF through libheif (ticket 41).
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
	"compositor-win/internal/hdrio"
	"compositor-win/internal/render"

	_ "golang.org/x/image/tiff" // registered for image.Decode
	_ "golang.org/x/image/webp" // registered for image.Decode
)

// User-facing import failures, ported from ImageImportError (WebP and HEIF
// are offered by the original's importableImages list).
var (
	ErrUnreadable  = errors.New("无法读取该图像：文件可能已损坏或不可用")
	ErrUnsupported = errors.New("请选择 JPEG、PNG、HEIC、HEIF、TIFF、WebP、OpenEXR、Radiance HDR 或 Photoshop（PSD）文件")
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
	// SourceProfile is the CMYK ICC the file carried, when the import converted
	// from CMYK. The caller adopts it so proofing can go back to the inks the
	// file was made for (ticket 86). Nil for every other format.
	SourceProfile []byte
}

// Decode reads one image file into a premultiplied bitmap, refusing files
// beyond the side limit or the remaining pixel budget (the budget counts
// every pixel already stored in the document's layers, as the original's
// remainingPixels did). The dimensions checked are the stored ones, before
// the EXIF orientation turns them.
func Decode(path string, remainingPixels int) (*Result, error) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch ext {
	case "exr", "hdr":
		return decodeHDRFile(path, remainingPixels)
	case "jpg", "jpeg", "png", "tif", "tiff", "webp":
	default:
		return nil, ErrUnsupported
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	// 票 86：CMYK TIFF 走自己的读取器 —— image.DecodeConfig 对它直接 UnsupportedError。
	if (ext == "tif" || ext == "tiff") && isCMYKTIFF(data) {
		return decodeCMYKImport(path, data, remainingPixels)
	}
	if (ext == "jpg" || ext == "jpeg") && isCMYKJPEG(data) {
		return decodeCMYKJPEGImport(path, data, remainingPixels)
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
	// A 16-bit source keeps its precision in the deep raster; the bitmap is
	// then its derived 8-bit copy (ADR-0007 §3).
	bmp := render.WorkingBitmap(img)
	bmp = ApplyOrientation(bmp, exifOrientation(format, data))
	return &Result{
		Name:   strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Bitmap: bmp,
		Width:  bmp.W,
		Height: bmp.H,
	}, nil
}

// exifOrientation reads the orientation where the container carries one:
// JPEG from its APP1 EXIF segment, TIFF from IFD0, PNG from an eXIf chunk,
// WebP from its EXIF RIFF chunk. Anything unreadable stays 1 (upright).
func exifOrientation(format string, data []byte) int {
	switch format {
	case "jpeg":
		return jpegEXIFOrientation(data)
	case "tiff":
		return tiffOrientation(data)
	case "png":
		return pngEXIFOrientation(data)
	case "webp":
		return webpEXIFOrientation(data)
	}
	return 1
}

// decodeHDRFile imports an OpenEXR or Radiance HDR file as a wide layer.
// hdrio returns half floats in extended sRGB; a file no brighter than white
// becomes a 16-bit layer, anything brighter stays HDR (upstream wideAsset).
func decodeHDRFile(path string, remainingPixels int) (*Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	deep, err := hdrio.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	if deep.W < 1 || deep.H < 1 {
		return nil, ErrUnreadable
	}
	if deep.W > domain.MaxSide || deep.H > domain.MaxSide || deep.W*deep.H > remainingPixels {
		return nil, tooLarge(deep.W, deep.H, remainingPixels)
	}
	if deep.HDR && deep.Peak() <= 1.001 {
		deep = render.HalfToSixteen(deep)
	}
	bmp := deep.ToBitmap()
	bmp.Deep = deep
	return &Result{
		Name:   strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Bitmap: bmp,
		Width:  bmp.W,
		Height: bmp.H,
	}, nil
}
