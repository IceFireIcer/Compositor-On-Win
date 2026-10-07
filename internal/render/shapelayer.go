package render

import (
	"math"

	"compositor-win/internal/domain"
)

// Shape tool rasterization: the CPU port of the macOS shape drawing
// (reference/Swift/Compositor/Document/ShapeTool.swift — shapeImage at
// 197-223 and ShapeKind.path at 10-15, the semantic authority).
//
// A shape layer keeps its style in the manifest (`shape` record,
// project-format.md:44) alongside an ordinary raster imageFile, "so it
// redraws cleanly when scaled". The compositor therefore draws the shape
// from the style at the layer box's current size — redrawShape's semantic
// (ShapeTool.swift:161-174, where a resize re-runs shapeImage instead of
// stretching) — rather than sampling the stored PNG. Shapes with dropped
// style metadata are plain pixels and take the ordinary image path.
//
// Anti-aliasing: the original fills CGPaths with Core Graphics' analytic
// coverage; here every pixel is 2×2 supersampled with the same rotated-grid
// offsets Place uses (transform.go:87-91), the CPU compositor's established
// AA stand-in. Fill is the style color at full alpha, coverage-carried —
// the context fill of shapeImage (ShapeTool.swift:218-220); lines are
// stroked with round caps, never filled (ShapeTool.swift:8-9, 204-214).

// RenderShape draws the shape filling its w×h layer box (layer pixel
// space), premultiplied output like every raster in this package. The box
// for a line is the endpoints' bounding box plus the stroke's own thickness
// around them (ShapeTool.swift:120-127) — the caller sizes it, this only
// draws. Ellipses ignore cornerRadius; a rectangle's radius clamps to half
// its shorter side, so a large radius makes a pill (ShapeTool.swift:12-14).
func RenderShape(style domain.ShapeStyle, w, h int) *Bitmap {
	if w < 1 {
		w = 1 // redrawShape's max(1, …) box clamp, ShapeTool.swift:163
	}
	if h < 1 {
		h = 1
	}
	out := NewBitmap(w, h)

	var inside func(x, y float64) bool
	switch style.Kind {
	case domain.ShapeLine:
		// max(1, lineWidth), ShapeTool.swift:203; lines always store a
		// width (nil on other shapes, ShapeTool.swift:28) — nil degrades
		// to the 1px floor.
		thickness := 1.0
		if style.LineWidth != nil {
			thickness = math.Max(1, *style.LineWidth)
		}
		from, to := lineEnds(style, w, h, thickness)
		half := thickness / 2
		inside = func(x, y float64) bool {
			// Round caps: the stroke covers everything within half the
			// thickness of the segment — distance-to-segment, not an
			// infinite line (setLineCap(.round), ShapeTool.swift:206).
			return segmentDistance(x, y, from[0], from[1], to[0], to[1]) <= half
		}
	case domain.ShapeEllipse:
		inside = ellipseInside(w, h)
	default: // Rectangle — the radius clamp of ShapeKind.path, ShapeTool.swift:12
		radius := style.CornerRadius
		radius = math.Max(0, math.Min(radius, math.Min(float64(w)/2, float64(h)/2)))
		if radius <= 0 {
			inside = func(x, y float64) bool {
				return x >= 0 && x <= float64(w) && y >= 0 && y <= float64(h)
			}
		} else {
			inside = roundedRectInside(w, h, radius)
		}
	}

	// 2×2 rotated-grid supersampling, the offsets of Place's High quality
	// path (transform.go:87-91): sub-samples at ±0.25 around the pixel
	// center. Coverage quantizes to fifths of the pixel {0,.25,.5,.75,1}.
	parallelFor(h, func(y0, y1 int) {
		for py := y0; py < y1; py++ {
			for px := 0; px < w; px++ {
				hits := 0
				for sy := 0; sy < 2; sy++ {
					for sx := 0; sx < 2; sx++ {
						sampleX := float64(px) + 0.5 + (0.25 + 0.5*float64(sx) - 0.5)
						sampleY := float64(py) + 0.5 + (0.25 + 0.5*float64(sy) - 0.5)
						if inside(sampleX, sampleY) {
							hits++
						}
					}
				}
				if hits == 0 {
					continue
				}
				cov := float64(hits) / 4
				i := (py*w + px) * 4
				out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] =
					premul(style.Red, style.Green, style.Blue, cov)
			}
		}
	})
	return out
}

// lineEnds maps a line's stored endpoints — fractions of the layer box
// (0–1), so the line lands exactly where it was dragged and redraws
// correctly at another size (ShapeTool.swift:26-28, 135-140) — into box
// pixels (start.x·size.width, ShapeTool.swift:210-211). Lines from before
// ends were stored (nil) ran corner to corner, inset by half their
// thickness (ShapeTool.swift:207-211).
func lineEnds(style domain.ShapeStyle, w, h int, thickness float64) ([2]float64, [2]float64) {
	fw, fh := float64(w), float64(h)
	if style.Start != nil && style.End != nil {
		return [2]float64{style.Start[0] * fw, style.Start[1] * fh},
			[2]float64{style.End[0] * fw, style.End[1] * fh}
	}
	insetX := math.Min(thickness, fw) / 2
	insetY := math.Min(thickness, fh) / 2
	return [2]float64{insetX, insetY}, [2]float64{fw - insetX, fh - insetY}
}

// ellipseInside tests the ellipse inscribed in the box — CGPath(ellipseIn:),
// ShapeTool.swift:11. The boundary uses ≤ so the extremes of the axes stay
// painted, as a CG fill of the path does.
func ellipseInside(w, h int) func(x, y float64) bool {
	rx, ry := float64(w)/2, float64(h)/2
	return func(x, y float64) bool {
		nx := (x - rx) / rx
		ny := (y - ry) / ry
		return nx*nx+ny*ny <= 1
	}
}

// roundedRectInside tests the rounded rectangle of ShapeKind.path
// (ShapeTool.swift:12-14): straight edges with quarter-circle corners of
// the clamped radius. Clamping the sample to the inner rect [r, w−r]×
// [r, h−r] reduces every region to one corner-circle test — the middle
// (dx = dy = 0) is always inside, edge regions lose the offsets, and only
// true corners measure a distance. The pill (radius = half the shorter
// side) falls out of the same clamp.
func roundedRectInside(w, h int, radius float64) func(x, y float64) bool {
	loX, hiX := radius, float64(w)-radius
	loY, hiY := radius, float64(h)-radius
	r2 := radius * radius
	return func(x, y float64) bool {
		dx := x - math.Min(hiX, math.Max(loX, x))
		dy := y - math.Min(hiY, math.Max(loY, y))
		return dx*dx+dy*dy <= r2
	}
}

// segmentDistance is the Euclidean distance from p to the segment a→b,
// clamped to the endpoints — the round-cap stroke's coverage test.
func segmentDistance(px, py, ax, ay, bx, by float64) float64 {
	abx, aby := bx-ax, by-ay
	l2 := abx*abx + aby*aby
	if l2 == 0 {
		return math.Hypot(px-ax, py-ay)
	}
	t := clamp01(((px-ax)*abx + (py-ay)*aby) / l2)
	cx, cy := ax+t*abx, ay+t*aby
	return math.Hypot(px-cx, py-cy)
}
