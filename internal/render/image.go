// Package render is the Go CPU compositor: the export ground truth and the
// CPU fallback for the WebGPU pipeline (architecture: GPU fast path +
// CPU truth, mirroring the macOS dual-track renderer). Everything here is
// deterministic — parallel execution must be byte-identical to serial.
//
// Pixel invariants (AGENTS.md, ADR-0007): the compositing pipeline reads
// 8-bit RGBA premultiplied sRGB; a 16-bit or HDR layer keeps its authoritative
// pixels in Bitmap.Deep and the 8-bit bitmap is that raster's derived copy.
// Masks stay 8-bit grayscale.
// Blend-mode math operates on straight (unpremultiplied) sRGB values and
// intentionally follows the original's sRGB-space behavior — NOT a linear-
// light "correction" (see ADR-0004).
package render

// Bitmap is a premultiplied RGBA8 raster, row-major, 4 bytes per pixel.
//
// Deep is the layer's full-precision pixels when this bitmap is the derived
// 8-bit copy of a 16-bit or HDR layer (ADR-0007); it is nil for an ordinary
// 8-bit layer. Holding it here rather than in a parallel table keeps the pair
// together — upstream's ImportedImage carries `image` and `deep` the same way
// — and it gives the upstream rule for free: an operation that builds a new
// bitmap produces one without Deep, so the layer is simply 8-bit from then on
// (like text, rasterized). Nothing writes Deep's samples back into Pix, and
// nothing derives Pix except DeepRaster.ToBitmap.
type Bitmap struct {
	W    int
	H    int
	Pix  []uint8 // len == W*H*4, premultiplied
	Deep *DeepRaster
}

// NewBitmap allocates a fully transparent bitmap.
func NewBitmap(w, h int) *Bitmap {
	return &Bitmap{W: w, H: h, Pix: make([]uint8, w*h*4)}
}

// DropDeep forgets the full-precision raster. An 8-bit edit — a filter, a
// fill, a cut — changes Pix without touching Deep, so the raster it was
// derived from is no longer the layer's pixels; keeping it would make Save
// write the pre-edit samples (upstream drops deep the same way: any
// operation that is not itself 16-bit-aware produces an image without it).
func (b *Bitmap) DropDeep() {
	if b != nil {
		b.Deep = nil
	}
}

// Clone returns a deep copy, the full-precision pixels included.
func (b *Bitmap) Clone() *Bitmap {
	pix := make([]uint8, len(b.Pix))
	copy(pix, b.Pix)
	out := &Bitmap{W: b.W, H: b.H, Pix: pix}
	if b.Deep != nil {
		out.Deep = b.Deep.Clone()
	}
	return out
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
