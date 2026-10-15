package render

// Progressive blur (ticket 73), Filters.swift's progressivelyBlurred. The blur
// amount varies across the image: linearly from From at the line's start to To
// at its end, or radially outward from the start to the circle through the end.
//
// Upstream does it with CIMaskedVariableBlur over a gradient mask. There is no
// Core Image here, so a ladder of fixed-radius Gaussian blurs is interpolated
// by the same mask value instead: the same semantics (one blur amount per
// pixel), at the cost of a handful of whole-image Gaussian passes.

import "math"

// progressiveRungs is how many fixed-radius blurs the ladder holds. More rungs
// smooth the ramp between them; each one costs a full Gaussian pass.
const progressiveRungs = 8

// ProgressiveBlurSettings is one Progressive Blur: the line's two endpoints in
// the raster's own pixels, the blur amount at each end, and which shape and
// direction the ramp takes. Bundled rather than passed as eight arguments so a
// call site cannot silently swap From for To or start for end.
type ProgressiveBlurSettings struct {
	Start Point
	End   Point
	// From is the blur at Start, To the blur at End (pixels). Inverted swaps
	// them, as upstream swaps the two before taking the strongest.
	From     float64
	To       float64
	Radial   bool
	Inverted bool
	// Clamp names the canvas edges where the layer's edge pixels carry on, as
	// the other blurs do.
	Clamp EdgeSides
}

// ApplyProgressiveBlur blurs premultiplied RGBA in place with an amount that
// ramps from settings.From at Start to settings.To at End.
func ApplyProgressiveBlur(b *Bitmap, s ProgressiveBlurSettings) {
	if b == nil || b.W == 0 || b.H == 0 {
		return
	}
	from, to := s.From, s.To
	if s.Inverted {
		from, to = to, from
	}
	strongest := math.Max(from, to)
	if !(strongest > 0) {
		return
	}
	dx, dy := s.End.X-s.Start.X, s.End.Y-s.Start.Y
	lengthSq := dx*dx + dy*dy
	// A line shorter than a pixel has no direction; upstream nudges one so the
	// gradient still has somewhere to go (Filters.swift:396).
	if math.Sqrt(lengthSq) < 1 {
		dx, dy = 0, -1
		lengthSq = 1
	}
	length := math.Sqrt(lengthSq)

	// The ladder: rung i is the whole image blurred by strongest·i/rungs.
	ladder := make([]*Bitmap, progressiveRungs+1)
	ladder[0] = b.Clone()
	for i := 1; i <= progressiveRungs; i++ {
		ladder[i] = b.Clone()
		ApplyGaussianBlurSides(ladder[i], strongest*float64(i)/float64(progressiveRungs), s.Clamp)
	}

	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			share := progressiveShare(float64(x)+0.5, float64(y)+0.5, s.Start, dx, dy, lengthSq, length, s.Radial)
			radius := from + (to-from)*share
			pos := radius / strongest * float64(progressiveRungs)
			lo := int(math.Floor(pos))
			if lo < 0 {
				lo = 0
			}
			if lo >= progressiveRungs {
				lo = progressiveRungs - 1
			}
			frac := pos - float64(lo)
			i := (y*b.W + x) * 4
			for c := 0; c < 4; c++ {
				near := float64(ladder[lo].Pix[i+c])
				far := float64(ladder[lo+1].Pix[i+c])
				b.Pix[i+c] = uint8(math.Round(near + (far-near)*frac))
			}
		}
	}
}

// progressiveShare is a pixel's place on the ramp, 0 at the start and 1 at the
// end (and past it, as Core Image's gradients extend their end colors). Linear
// projects onto the line; radial uses the distance from the start, so the 1
// contour is the circle through the end.
func progressiveShare(px, py float64, start Point, dx, dy, lengthSq, length float64, radial bool) float64 {
	vx, vy := px-start.X, py-start.Y
	var share float64
	if radial {
		share = math.Hypot(vx, vy) / length
	} else {
		share = (vx*dx + vy*dy) / lengthSq
	}
	if share < 0 {
		return 0
	}
	if share > 1 {
		return 1
	}
	return share
}
