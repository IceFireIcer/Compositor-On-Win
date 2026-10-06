package render

import (
	"math"
	"sort"
)

// Gradient tool rasterization: the CPU port of the macOS gradient fill
// (reference/Swift/Compositor/Document/BrushStroke.swift:688-708, the
// semantic authority; the tool settings live in Gradient.swift:8-19).
//
// The original paints the gradient through Core Graphics onto the layer's
// tiles — a linear gradient projected along the start→end axis, or a radial
// gradient centered on the start with the end on its rim (Gradient.swift:8).
// Both draw with drawsBeforeStartLocation | drawsAfterEndLocation, so the
// first stop's color extends before the axis start and the last stop's color
// beyond it. The drag commits into the layer's ordinary pixels (commit at
// Gradient.swift:111-119), so the .comp manifest has no gradient field —
// a committed gradient layer renders through the normal image path and
// takes masks, clipping, blend modes and group fade from there
// (project-format.md stores no gradient record; hard invariant 1 forbids
// inventing one).
//
// The pending drag ("re-openable" gradient, GradientTests semantics) is the
// session's concern: it re-renders from the current endpoints on every
// refresh without accumulating earlier previews (Gradient.swift:79-86,
// BrushStroke.swift:690-694). This primitive is stateless — the caller
// starts from the un-touched raster each time, exactly as paintCanvas
// restarts from each tile's base content.
//
// Pixel invariants (AGENTS.md): premultiplied 8-bit RGBA, straight sRGB
// stop colors; masks stay 8-bit grayscale.

// GradientShape is the gradient geometry, the spellings of GradientShape
// (Gradient.swift:9-12): linear runs from start to end; radial is centered
// on the start with the end on its rim.
type GradientShape string

const (
	GradientLinear GradientShape = "Linear"
	GradientRadial GradientShape = "Radial"
)

// GradientStop is one color stop: a position along the gradient axis (0 at
// start, 1 at the end's rim) and a straight sRGB color in [0,1] with alpha.
// The original fills exactly two stops at locations [0,1] built from the
// palette — foreground→background (both alpha 1) or foreground→transparent
// (second stop alpha 0), reversed swapping them (Gradient.swift:88-98).
// The stop list is generalized so stop editing has somewhere to land; the
// two-stop case is the documented default.
type GradientStop struct {
	Pos float64
	R   float64
	G   float64
	B   float64
	A   float64
}

// GradientFill composites a gradient over dst in source-over order, the Go
// counterpart of BrushStroke.fillGradient (BrushStroke.swift:688-708):
//
//	linear: drawLinearGradient(start→end)
//	radial: drawRadialGradient(startCenter start, startRadius 0,
//	                          endCenter start, endRadius hypot(end-start))
//
// start/end are in dst's own pixel grid (the Go stroke grid already IS the
// canvas — pixelToDocument is not ported, see brush.go's header). opacity
// replaces the context alpha (BrushStroke.swift:692:
// setAlpha(min(1,max(0,opacity)))); mask (the selection-clip coverage
// currency, 255 = paintable, the same raster SelectionEdits hands
// FillColor) multiplies the gradient's alpha per pixel, standing in for
// selectionClip.apply(to:) in paintCanvas (BrushStroke.swift:743). nil or
// a short mask paints everywhere. A degenerate line (start == end) paints
// nothing — the session guards drags with hasLine ≥ 0.5px first
// (Gradient.swift:32, 81-85); the guard here only keeps the math defined.
func GradientFill(dst *Bitmap, mask []uint8, shape GradientShape, start, end Point, stops []GradientStop, opacity float64) {
	if len(stops) == 0 || dst == nil || len(dst.Pix) != dst.W*dst.H*4 {
		return
	}
	op := math.Min(1, math.Max(0, opacity)) // BrushStroke.swift:692
	ordered := make([]GradientStop, len(stops))
	copy(ordered, stops)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Pos < ordered[j].Pos })

	dx := end.X - start.X
	dy := end.Y - start.Y
	var axisLen2, radius float64
	switch shape {
	case GradientRadial:
		radius = math.Hypot(dx, dy) // BrushStroke.swift:701-703
		if radius <= 0 {
			return
		}
	default: // GradientLinear and unknown spellings fall back to linear
		axisLen2 = dx*dx + dy*dy
		if axisLen2 <= 0 {
			return
		}
	}

	parallelFor(dst.H, func(y0, y1 int) {
		for py := y0; py < y1; py++ {
			for px := 0; px < dst.W; px++ {
				cov := 1.0
				if mask != nil && len(mask) >= dst.W*dst.H {
					cov = float64(mask[py*dst.W+px]) / 255
				}
				if cov <= 0 {
					continue
				}
				// Projection onto the axis (linear) or distance from the
				// start center over the rim radius (radial). Extension past
				// the ends is the stop clamping below — the CG
				// drawsBefore/AfterStartLocation options.
				var t float64
				switch shape {
				case GradientRadial:
					t = math.Hypot(float64(px)+0.5-start.X, float64(py)+0.5-start.Y) / radius
				default:
					t = ((float64(px)+0.5-start.X)*dx + (float64(py)+0.5-start.Y)*dy) / axisLen2
				}
				sr, sg, sb, sa := gradientColorAt(ordered, t)
				a := sa * op * cov
				if a <= 0 {
					continue
				}
				// Source-over onto the premultiplied destination: the
				// gradient contributes straight-color × alpha, the backdrop
				// fades by (1−a) — the default .normal blend the CG context
				// draws with.
				i := (py*dst.W + px) * 4
				dr := float64(dst.Pix[i]) / 255
				dg := float64(dst.Pix[i+1]) / 255
				db := float64(dst.Pix[i+2]) / 255
				da := float64(dst.Pix[i+3]) / 255
				inv := 1 - a
				dst.Pix[i] = uint8(clamp01(sr*a+dr*inv)*255 + 0.5)
				dst.Pix[i+1] = uint8(clamp01(sg*a+dg*inv)*255 + 0.5)
				dst.Pix[i+2] = uint8(clamp01(sb*a+db*inv)*255 + 0.5)
				dst.Pix[i+3] = uint8(clamp01(a+da*inv)*255 + 0.5)
			}
		}
	})
}

// gradientColorAt interpolates the stop ramp linearly per channel in straight
// sRGB — what CGGradient does with colors in the sRGB space
// (BrushStroke.swift:689). Colors before the first / after the last stop are
// the end stops themselves (the drawsBefore/AfterEndLocation options,
// BrushStroke.swift:698, 703). The GradientTests ramp confirms plain lerp:
// the canvas midpoint of black→white lands ≈128 with CG's own quantization
// noted there (GradientTests.swift:33-34).
func gradientColorAt(stops []GradientStop, t float64) (float64, float64, float64, float64) {
	first := stops[0]
	if t <= first.Pos {
		return first.R, first.G, first.B, first.A
	}
	last := stops[len(stops)-1]
	if t >= last.Pos {
		return last.R, last.G, last.B, last.A
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].Pos {
			p0, p1 := stops[i-1], stops[i]
			span := p1.Pos - p0.Pos
			u := 0.0
			if span > 0 {
				u = (t - p0.Pos) / span
			}
			return p0.R + (p1.R-p0.R)*u,
				p0.G + (p1.G-p0.G)*u,
				p0.B + (p1.B-p0.B)*u,
				p0.A + (p1.A-p0.A)*u
		}
	}
	return last.R, last.G, last.B, last.A
}
