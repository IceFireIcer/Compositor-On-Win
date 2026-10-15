package render

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// DeepRaster is the pipeline's second pixel format (ADR-0007). It holds
// premultiplied RGBA at 16 bits a channel in sRGB, or — when HDR is set —
// half floats in extended sRGB, where white is 1 and light brighter than
// white is kept. The layout mirrors the macOS DeepLayer: native-endian
// uint16 samples, rows tightly packed (width × 8 bytes), top-left origin.
// Masks stay 8-bit grayscale and never use this type.
//
// A deep raster is the layer's authoritative pixels; the 8-bit Bitmap that
// every other operation, the canvas and the thumbnails use is a derived copy
// (ToBitmap), made again on load rather than stored, and never written back
// into these samples.
type DeepRaster struct {
	W   int
	H   int
	Pix []uint16 // len == W*H*4, premultiplied
	HDR bool
}

// NewDeep allocates a fully transparent deep raster.
func NewDeep(w, h int, hdr bool) *DeepRaster {
	return &DeepRaster{W: w, H: h, Pix: make([]uint16, w*h*4), HDR: hdr}
}

// Clone returns a deep copy.
func (d *DeepRaster) Clone() *DeepRaster {
	pix := make([]uint16, len(d.Pix))
	copy(pix, d.Pix)
	return &DeepRaster{W: d.W, H: d.H, Pix: pix, HDR: d.HDR}
}

// DeepFromBitmap widens an 8-bit premultiplied bitmap to 16 bits. Each byte
// v becomes v*257, the exact widening of the 0–255 range onto 0–65535, so a
// converted layer keeps the pixels it had and nothing is invented.
func DeepFromBitmap(b *Bitmap) *DeepRaster {
	out := NewDeep(b.W, b.H, false)
	for i, v := range b.Pix {
		out.Pix[i] = uint16(v) * 257
	}
	return out
}

// DeepFromImage converts a decoded image into the 16-bit premultiplied
// working format. color.Color.RGBA already returns premultiplied 16-bit
// samples, which is exactly this layout — so a 16-bit PNG keeps all of its
// precision, and an 8-bit one widens to v*257 as DeepFromBitmap does.
func DeepFromImage(img image.Image) *DeepRaster {
	b := img.Bounds()
	out := NewDeep(b.Dx(), b.Dy(), false)
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := (y*out.W + x) * 4
			out.Pix[i] = uint16(r)
			out.Pix[i+1] = uint16(g)
			out.Pix[i+2] = uint16(bl)
			out.Pix[i+3] = uint16(a)
		}
	}
	return out
}

// WorkingBitmap is the bitmap a decoded image becomes in a document: its
// 8-bit premultiplied working copy, with the full-precision raster attached
// when the image carries 16 bits a channel (ADR-0007 §3). A wide image's
// 8-bit copy is the dithered one, as upstream DeepLayer.dithered makes it,
// not a plain truncation — so an imported 16-bit gradient does not arrive
// already stepped.
func WorkingBitmap(img image.Image) *Bitmap {
	if !WideImage(img) {
		return BitmapFromImage(img)
	}
	deep := DeepFromImage(img)
	bmp := deep.ToBitmap()
	bmp.Deep = deep
	return bmp
}

// WideImage reports whether a decoded image carries more than 8 bits a
// channel. Go's decoders hand back the 16-bit concrete types for such files,
// which is the same signal upstream reads from kCGImagePropertyDepth.
func WideImage(img image.Image) bool {
	switch img.(type) {
	case *image.NRGBA64, *image.RGBA64, *image.Gray16:
		return true
	}
	return false
}

// DeepFromPNG decodes a 16-bit layer PNG into the working format.
func DeepFromPNG(data []byte) (*DeepRaster, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return DeepFromImage(img), nil
}

// Wide16 returns the 16-bit premultiplied samples the 8-bit copy is rounded
// from. For a 16-bit raster that is the pixels themselves; an HDR raster's
// highlights are clipped to white first, since anything brighter shows as
// white (upstream: a dithered copy of the clipped pixels).
func (d *DeepRaster) Wide16() []uint16 {
	if !d.HDR {
		return d.Pix
	}
	out := make([]uint16, len(d.Pix))
	for i := 0; i < len(d.Pix); i += 4 {
		alpha := clamp01f(HalfToFloat(d.Pix[i+3]))
		for c := 0; c < 3; c++ {
			v := clamp01f(HalfToFloat(d.Pix[i+c]))
			// Premultiplied: a clipped color never passes its own alpha.
			if v > alpha {
				v = alpha
			}
			out[i+c] = uint16(lroundf(v * 65535))
		}
		out[i+3] = uint16(lroundf(alpha * 65535))
	}
	return out
}

// ToBitmap is the layer's 8-bit copy: the 16-bit samples rounded with the
// fixed fine noise of Quantize16, so a smooth gradient does not come out in
// steps (upstream DeepLayer.dithered). Every 8-bit consumer — the compositor,
// the canvas, the thumbnails, the export path — reads this, and nothing ever
// writes it back into the deep samples.
//
// It is a pure derivation and deliberately does not attach Deep: converting a
// layer *to* 8 bits is exactly `raster.ToBitmap()`, and a function that
// smuggled the raster back in would make dropping depth impossible to express.
// WorkingBitmap attaches it where the copy really is a wide layer's display
// copy.
func (d *DeepRaster) ToBitmap() *Bitmap {
	return &Bitmap{W: d.W, H: d.H, Pix: Quantize16(d.Wide16(), d.W, d.H)}
}

// HalfToSixteen turns an HDR raster whose samples are all within white into
// a 16-bit one, keeping the precision the halves actually hold. Quantizing
// to 8 bits and widening back would throw away exactly what made the file
// worth importing as 16-bit (upstream stores the quantized 16-bit CGImage;
// the values match within a rounding step).
func HalfToSixteen(d *DeepRaster) *DeepRaster {
	out := NewDeep(d.W, d.H, false)
	for i, h := range d.Pix {
		v := float64(HalfToFloat(h))
		if v < 0 {
			v = 0
		} else if v > 1 {
			v = 1
		}
		out.Pix[i] = uint16(math.Round(v * 65535))
	}
	return out
}

// Peak is the brightest unpremultiplied color, 0–1 for a 16-bit raster and
// above 1 for an HDR one that still holds light brighter than white. It is
// what decides whether a wide import is HDR at all (upstream deep_half_peak
// and DeepLayer.wideAsset's 1.001 threshold).
func (d *DeepRaster) Peak() float32 {
	if d.HDR {
		return HalfPeak(d.Pix, d.W, d.H)
	}
	var peak float32
	for i := 0; i < len(d.Pix); i += 4 {
		alpha := float32(d.Pix[i+3])
		if alpha <= 0 {
			continue
		}
		for c := 0; c < 3; c++ {
			if v := float32(d.Pix[i+c]) / alpha; v > peak {
				peak = v
			}
		}
	}
	return peak
}

// EncodePNG16 writes the raster as a 16-bit RGBA PNG with straight alpha.
// PNG carries no premultiplication, so each color is divided back out by its
// alpha — the same round trip CoreGraphics performs on the macOS side, with
// the same at-most-one-step rounding (ADR-0007 §6).
func EncodePNG16(d *DeepRaster) ([]byte, error) {
	img := image.NewNRGBA64(image.Rect(0, 0, d.W, d.H))
	for y := 0; y < d.H; y++ {
		for x := 0; x < d.W; x++ {
			i := (y*d.W + x) * 4
			a := uint32(d.Pix[i+3])
			var c [3]uint16
			if a != 0 {
				for k := 0; k < 3; k++ {
					v := uint32(d.Pix[i+k]) * 65535 / a
					if v > 65535 {
						v = 65535
					}
					c[k] = uint16(v)
				}
			}
			img.SetNRGBA64(x, y, color.NRGBA64{R: c[0], G: c[1], B: c[2], A: uint16(a)})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// clamp01f clamps to [0, 1]; NaN becomes 0, as a comparison-based clamp does
// in C.
func clamp01f(v float32) float32 {
	if !(v > 0) {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// lroundf rounds half away from zero, as C's lroundf does.
func lroundf(v float32) int64 {
	return int64(math.Round(float64(v)))
}
