// Package render is the Go CPU compositor: the export ground truth and the
// CPU fallback for the WebGPU pipeline (architecture: GPU fast path +
// CPU truth, mirroring the macOS dual-track renderer). Everything here is
// deterministic — parallel execution must be byte-identical to serial.
//
// Pixel invariants (AGENTS.md): 8-bit RGBA premultiplied throughout the
// compositing pipeline, sRGB color space, masks are 8-bit grayscale.
// Blend-mode math operates on straight (unpremultiplied) sRGB values and
// intentionally follows the original's sRGB-space behavior — NOT a linear-
// light "correction" (see ADR-0004).
package render

// Bitmap is a premultiplied RGBA8 raster, row-major, 4 bytes per pixel.
type Bitmap struct {
	W   int
	H   int
	Pix []uint8 // len == W*H*4, premultiplied
}

// NewBitmap allocates a fully transparent bitmap.
func NewBitmap(w, h int) *Bitmap {
	return &Bitmap{W: w, H: h, Pix: make([]uint8, w*h*4)}
}

// Clone returns a deep copy.
func (b *Bitmap) Clone() *Bitmap {
	pix := make([]uint8, len(b.Pix))
	copy(pix, b.Pix)
	return &Bitmap{W: b.W, H: b.H, Pix: pix}
}

// coverage extracts the alpha channel as a grayscale coverage raster (the
// currency of clipping masks).
func (b *Bitmap) coverage() []uint8 {
	out := make([]uint8, b.W*b.H)
	for i := range out {
		out[i] = b.Pix[i*4+3]
	}
	return out
}
