package render

// Blur boundary semantics (Filters.swift PixelFilter.extending / gaussianBlur):
// a blur at the canvas's edge carries the layer's edge pixels on instead of
// fading into the clear ones beyond, and Preserve Transparency blurs only the
// colors while every pixel keeps its alpha.

import "testing"

// blurOpaque builds a fully opaque white raster.
func blurOpaque(w, h int) *Bitmap {
	b := NewBitmap(w, h)
	for i := 0; i < w*h; i++ {
		b.Pix[i*4], b.Pix[i*4+1], b.Pix[i*4+2], b.Pix[i*4+3] = 255, 255, 255, 255
	}
	return b
}

// blurHalves builds red / green / clear thirds of a row, opaque where painted
// (premultiplied, so an opaque color is its own value).
func blurHalves(w, h, redTo, greenTo int) *Bitmap {
	b := NewBitmap(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			switch {
			case x < redTo:
				b.Pix[i] = 255
			case x < greenTo:
				b.Pix[i+1] = 255
			}
			if x < greenTo {
				b.Pix[i+3] = 255
			}
		}
	}
	return b
}

func blurAlphaAt(b *Bitmap, x, y int) uint8 { return b.Pix[(y*b.W+x)*4+3] }

// A clamped side carries the edge pixel on, so a constant opaque image stays
// solid to every edge it names; its other sides still fade into the clear.
func TestGaussianBlurSidesCarryTheEdgePixelOn(t *testing.T) {
	all := blurOpaque(16, 16)
	ApplyGaussianBlurSides(all, 2, EdgeLeft|EdgeRight|EdgeTop|EdgeBottom)
	for i := 0; i < len(all.Pix); i++ {
		if all.Pix[i] != 255 {
			t.Fatalf("clamped blur faded byte %d to %d, want 255", i, all.Pix[i])
		}
	}
	// The left side alone: the left edge stays solid, the top still fades.
	left := blurOpaque(16, 16)
	ApplyGaussianBlurSides(left, 2, EdgeLeft)
	if a := blurAlphaAt(left, 0, 8); a != 255 {
		t.Fatalf("left edge alpha = %d, want 255 (the edge pixel is carried on)", a)
	}
	if a := blurAlphaAt(left, 8, 0); a >= 255 {
		t.Fatalf("top edge alpha = %d, want < 255 (that side is not clamped)", a)
	}
	// Unclamped, the same edge fades as it always did.
	plain := blurOpaque(16, 16)
	ApplyGaussianBlur(plain, 2)
	if a := blurAlphaAt(plain, 0, 8); a >= 200 {
		t.Fatalf("unclamped left edge alpha = %d, want well under 255", a)
	}
}

// Past a clamped side an out-of-frame sample repeats the edge pixel, so it
// keeps that pixel's weight instead of being dropped.
func TestMotionBlurSidesCarryTheEdgePixelOn(t *testing.T) {
	// One opaque column against the left edge, clear to its right.
	edgeColumn := func() *Bitmap {
		b := NewBitmap(16, 16)
		for y := 0; y < 16; y++ {
			i := (y*16 + 0) * 4
			b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = 255, 255, 255, 255
		}
		return b
	}
	clamped := edgeColumn()
	ApplyMotionBlurSides(clamped, 0, 5, EdgeLeft)
	plain := edgeColumn()
	ApplyMotionBlur(plain, 0, 5)
	got, want := blurAlphaAt(clamped, 0, 8), blurAlphaAt(plain, 0, 8)
	if got <= want {
		t.Fatalf("left-clamped streak alpha = %d, want more than the unclamped %d (the edge column repeats)", got, want)
	}
	if want == 0 {
		t.Fatal("the unclamped streak dropped the only opaque sample entirely")
	}
	// A picture filling the canvas: clamped on every side it stays solid to
	// the edge; unclamped the same edges thin out (the frame's last row and
	// column have no neighbor to interpolate with).
	solid := blurOpaque(32, 20)
	ApplyMotionBlurSides(solid, 0, 30*MotionRadiusPerPixel, EdgeLeft|EdgeRight|EdgeTop|EdgeBottom)
	for p := 0; p < 32*20; p++ {
		if a := solid.Pix[p*4+3]; a != 255 {
			t.Fatalf("clamped streak faded pixel %d to %d, want 255", p, a)
		}
	}
	thin := blurOpaque(32, 20)
	ApplyMotionBlur(thin, 0, 30*MotionRadiusPerPixel)
	if a := blurAlphaAt(thin, 23, 19); a >= 255 {
		t.Fatalf("unclamped streak alpha at the frame edge = %d, want < 255", a)
	}
}

// Preserve Transparency blurs the colors only: every pixel keeps its alpha,
// the outline doesn't move, and the colors beside the clear part stay the
// layer's own instead of being darkened by it.
func TestGaussianBlurPreservingAlphaKeepsAlphaAndClearsTheDarkRim(t *testing.T) {
	// Red then green across the left half; the right half clear.
	b := blurHalves(20, 8, 5, 10)
	orig := b.Clone()
	ApplyGaussianBlurPreservingAlpha(b, 2)
	for y := 0; y < 8; y++ {
		for x := 0; x < 20; x++ {
			want := uint8(0)
			if x < 10 {
				want = 255
			}
			if got := blurAlphaAt(b, x, y); got != want {
				t.Fatalf("alpha at (%d,%d) = %d, want %d (preserved)", x, y, got, want)
			}
		}
	}
	// The leftmost red keeps its full color: unpremultiplying takes out the
	// pull of the clear pixels beside it. (8-bit rounding at each separable
	// pass leaves a little slack.)
	if got := b.Pix[(4*20+0)*4]; got < 250 {
		t.Fatalf("left edge red = %d, want ~255 (no dark rim)", got)
	}
	// The colors really do blur into each other at the red/green seam.
	if g := b.Pix[(4*20+4)*4+1]; g == 0 {
		t.Fatal("red beside the seam picked up no green: the colors were not blurred")
	}
	if r := b.Pix[(4*20+5)*4]; r == 0 {
		t.Fatal("green beside the seam picked up no red: the colors were not blurred")
	}
	// A clear pixel stays fully clear.
	for c := 0; c < 4; c++ {
		if got := b.Pix[(4*20+15)*4+c]; got != 0 {
			t.Fatalf("clear pixel channel %d = %d, want 0", c, got)
		}
	}
	// Without Preserve Transparency the same edge fades and darkens.
	plain := orig.Clone()
	ApplyGaussianBlur(plain, 2)
	if got := plain.Pix[(4*20+0)*4]; got >= 200 {
		t.Fatalf("plain blur left edge red = %d, want a faded, darker rim", got)
	}
	if got := blurAlphaAt(plain, 0, 4); got >= 200 {
		t.Fatalf("plain blur left edge alpha = %d, want a faded rim", got)
	}
}
