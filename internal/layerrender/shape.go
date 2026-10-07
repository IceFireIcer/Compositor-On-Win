package layerrender

// Shape rasterization (EditorSession.shapeImage + PSDVector.raster): the
// shape-tool geometry and imported vector paths both draw here into
// premultiplied bitmaps, anti-aliased through x/image vector.

import (
	"image"
	"math"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"

	"golang.org/x/image/vector"
)

// ShapeImage rasterizes a shape-tool style into a size×size bitmap
// (EditorSession.shapeImage). Lines are stroked corner to corner (or
// between their stored fractions), rectangles/ellipses are filled.
func ShapeImage(style domain.ShapeStyle, width, height int) (*render.Bitmap, bool) {
	if width < 1 || height < 1 || width > 30000 || height > 30000 || width*height > 200_000_000 {
		return nil, false
	}
	mask := image.NewAlpha(image.Rect(0, 0, width, height))
	z := &vector.Rasterizer{}
	z.Reset(width, height)
	switch style.Kind {
	case domain.ShapeLine:
		thickness := 1.0
		if style.LineWidth != nil && *style.LineWidth > 0 {
			thickness = *style.LineWidth
		}
		inset := thickness / 2
		if inset > float64(width)/2 {
			inset = float64(width) / 2
		}
		insetY := thickness / 2
		if insetY > float64(height)/2 {
			insetY = float64(height) / 2
		}
		from := [2]float64{inset, insetY}
		to := [2]float64{float64(width) - inset, float64(height) - insetY}
		if style.Start != nil {
			from = [2]float64{style.Start[0] * float64(width), style.Start[1] * float64(height)}
		}
		if style.End != nil {
			to = [2]float64{style.End[0] * float64(width), style.End[1] * float64(height)}
		}
		strokePolyline(z, []point{{from[0], from[1]}, {to[0], to[1]}}, thickness, false)
	case domain.ShapeEllipse:
		addEllipse(z, float64(width)/2, float64(height)/2, float64(width)/2, float64(height)/2)
	case domain.ShapeRectangle:
		addRoundedRect(z, float64(width), float64(height), style.CornerRadius)
	default:
		return nil, false
	}
	z.Draw(mask, mask.Bounds(), image.White, image.Point{})
	return colorMask(mask, style.Red, style.Green, style.Blue), true
}

// VectorImage rasterizes an imported vector path with fill and/or stroke
// (PSDVector.raster): the fill draws first, the stroke over it, each in its
// own colour. Segments are absolute document-pixel coordinates;
// originX/originY shift them into the bitmap.
func VectorImage(segments []PathSeg, originX, originY, width, height int,
	fill *[3]float64, stroke *Stroke) (*render.Bitmap, bool) {
	if width < 1 || height < 1 || width > 30000 || height > 30000 || width*height > 200_000_000 || len(segments) == 0 {
		return nil, false
	}
	if fill == nil && stroke == nil {
		return nil, false
	}
	dx, dy := -float64(originX), -float64(originY)
	out := render.NewBitmap(width, height)
	bounds := image.Rect(0, 0, width, height)
	if fill != nil {
		z := &vector.Rasterizer{}
		z.Reset(width, height)
		addPath(z, segments, dx, dy)
		mask := image.NewAlpha(bounds)
		z.Draw(mask, bounds, image.White, image.Point{})
		blendMask(out, mask, *fill)
	}
	if stroke != nil {
		width := stroke.Width
		if !(width > 0) {
			width = 1
		}
		mask := image.NewAlpha(bounds)
		for _, pair := range strokeSegments(segments, dx, dy) {
			strokePolylineMask(mask, []point{pair[0], pair[1]}, width)
		}
		blendMask(out, mask, [3]float64{stroke.Red, stroke.Green, stroke.Blue})
	}
	return out, true
}

// blendMask composites a coverage mask in one colour over the bitmap
// (source-over with straight-alpha source).
func blendMask(dst *render.Bitmap, mask *image.Alpha, color [3]float64) {
	cr, cg, cb := clamp01(color[0]), clamp01(color[1]), clamp01(color[2])
	for i, a8 := range mask.Pix {
		if a8 == 0 {
			continue
		}
		sa := uint32(a8)
		da := uint32(dst.Pix[i*4+3])
		inv := 255 - sa
		// out = src + dst·(1−a), premultiplied.
		for c, sc := range [3]float64{cr, cg, cb} {
			src := uint32(sc * float64(sa))
			v := src + uint32(dst.Pix[i*4+c])*inv/255
			if v > 255 {
				v = 255
			}
			dst.Pix[i*4+c] = uint8(v)
		}
		v := sa + da*inv/255
		if v > 255 {
			v = 255
		}
		dst.Pix[i*4+3] = uint8(v)
	}
}

// Stroke is a vector stroke colour and width (PSDVector stroke).
type Stroke struct {
	Red, Green, Blue float64
	Width            float64
}

// PathSeg is one absolute path segment (0 move, 1 line, 2 close, 3 cubic);
// the psd import decodes them, this package draws them.
type PathSeg struct {
	Kind     byte
	X, Y     float64
	C1X, C1Y float64
	C2X, C2Y float64
}

func colorMask(mask *image.Alpha, r, g, b float64) *render.Bitmap {
	out := render.NewBitmap(mask.Bounds().Dx(), mask.Bounds().Dy())
	cr, cg, cb := clamp01(r), clamp01(g), clamp01(b)
	for i, a8 := range mask.Pix {
		if a8 == 0 {
			continue
		}
		a := uint32(a8)
		out.Pix[i*4] = uint8(cr * float64(a))
		out.Pix[i*4+1] = uint8(cg * float64(a))
		out.Pix[i*4+2] = uint8(cb * float64(a))
		out.Pix[i*4+3] = uint8(a)
	}
	return out
}

type point struct{ x, y float64 }

// addRoundedRect ports ShapeKind.path(in:cornerRadius:): a plain rectangle
// when the radius is 0, a pill when it exceeds half the smaller side.
func addRoundedRect(z *vector.Rasterizer, width, height, cornerRadius float64) {
	radius := cornerRadius
	if !(radius > 0) {
		z.MoveTo(0, 0)
		z.LineTo(float32(width), 0)
		z.LineTo(float32(width), float32(height))
		z.LineTo(0, float32(height))
		z.ClosePath()
		return
	}
	limit := math.Min(width, height) / 2
	if radius > limit {
		radius = limit
	}
	const k = 0.5523 // circle-to-bezier
	addEllipseCorner := func(cx, cy, fromA, toA float64) {
		// Quarter arc from angle fromA to toA about (cx, cy).
		startX := cx + radius*math.Cos(fromA)
		startY := cy + radius*math.Sin(fromA)
		endX := cx + radius*math.Cos(toA)
		endY := cy + radius*math.Sin(toA)
		c1x := startX - k*radius*math.Sin(fromA)
		c1y := startY + k*radius*math.Cos(fromA)
		c2x := endX + k*radius*math.Sin(toA)
		c2y := endY - k*radius*math.Cos(toA)
		z.CubeTo(float32(c1x), float32(c1y), float32(c2x), float32(c2y), float32(endX), float32(endY))
	}
	z.MoveTo(float32(radius), 0)
	z.LineTo(float32(width-radius), 0)
	addEllipseCorner(width-radius, radius, -math.Pi/2, 0)
	z.LineTo(float32(width), float32(height-radius))
	addEllipseCorner(width-radius, height-radius, 0, math.Pi/2)
	z.LineTo(float32(radius), float32(height))
	addEllipseCorner(radius, height-radius, math.Pi/2, math.Pi)
	z.LineTo(0, float32(radius))
	addEllipseCorner(radius, radius, math.Pi, 3*math.Pi/2)
	z.ClosePath()
}

func addEllipse(z *vector.Rasterizer, cx, cy, rx, ry float64) {
	const k = 0.5523
	z.MoveTo(float32(cx+rx), float32(cy))
	z.CubeTo(float32(cx+rx), float32(cy+k*ry), float32(cx+k*rx), float32(cy+ry), float32(cx), float32(cy+ry))
	z.CubeTo(float32(cx-k*rx), float32(cy+ry), float32(cx-rx), float32(cy+k*ry), float32(cx-rx), float32(cy))
	z.CubeTo(float32(cx-rx), float32(cy-k*ry), float32(cx-k*rx), float32(cy-ry), float32(cx), float32(cy-ry))
	z.CubeTo(float32(cx+k*rx), float32(cy-ry), float32(cx+rx), float32(cy-k*ry), float32(cx+rx), float32(cy))
	z.ClosePath()
}

// addPath emits the flattened path; document coordinates shifted by dx/dy.
func addPath(z *vector.Rasterizer, segments []PathSeg, dx, dy float64) {
	started := false
	for _, seg := range segments {
		switch seg.Kind {
		case 0:
			z.MoveTo(float32(seg.X+dx), float32(seg.Y+dy))
			started = true
		case 1:
			if started {
				z.LineTo(float32(seg.X+dx), float32(seg.Y+dy))
			}
		case 2:
			if started {
				z.ClosePath()
			}
		case 3:
			if started {
				z.CubeTo(float32(seg.C1X+dx), float32(seg.C1Y+dy),
					float32(seg.C2X+dx), float32(seg.C2Y+dy),
					float32(seg.X+dx), float32(seg.Y+dy))
			}
		}
	}
	z.ClosePath()
}

// strokeSegments flattens the path into line pairs for the stroke pass.
func strokeSegments(segments []PathSeg, dx, dy float64) [][2]point {
	var out [][2]point
	var current, subpathStart point
	started := false
	flatten := func(from, c1, c2, to point, cubic bool) {
		steps := 24
		prev := from
		for i := 1; i <= steps; i++ {
			t := float64(i) / float64(steps)
			var p point
			if cubic {
				mt := 1 - t
				p = point{
					x: mt*mt*mt*from.x + 3*mt*mt*t*c1.x + 3*mt*t*t*c2.x + t*t*t*to.x,
					y: mt*mt*mt*from.y + 3*mt*mt*t*c1.y + 3*mt*t*t*c2.y + t*t*t*to.y,
				}
			} else {
				p = point{from.x + (to.x-from.x)*t, from.y + (to.y-from.y)*t}
			}
			out = append(out, [2]point{prev, p})
			prev = p
		}
	}
	for _, seg := range segments {
		switch seg.Kind {
		case 0:
			current = point{seg.X + dx, seg.Y + dy}
			subpathStart = current
			started = true
		case 1:
			if started {
				to := point{seg.X + dx, seg.Y + dy}
				out = append(out, [2]point{current, to})
				current = to
			}
		case 2:
			if started {
				out = append(out, [2]point{current, subpathStart})
				current = subpathStart
			}
		case 3:
			if started {
				to := point{seg.X + dx, seg.Y + dy}
				flatten(current, point{seg.C1X + dx, seg.C1Y + dy}, point{seg.C2X + dx, seg.C2Y + dy}, to, true)
				current = to
			}
		}
	}
	return out
}

// strokePolyline strokes a polyline through z (closed when close is true),
// used by the line shape: points are stroked as a quad band.
func strokePolyline(z *vector.Rasterizer, pts []point, thickness float64, close bool) {
	halfT := thickness / 2
	for i := 0; i+1 < len(pts); i++ {
		bandQuad(z, pts[i], pts[i+1], halfT)
	}
	if close && len(pts) > 2 {
		bandQuad(z, pts[len(pts)-1], pts[0], halfT)
	}
}

// bandQuad emits one stroke band as a quad (butt caps).
func bandQuad(z *vector.Rasterizer, a, b point, halfT float64) {
	dx, dy := b.x-a.x, b.y-a.y
	length := math.Hypot(dx, dy)
	if length < 1e-9 {
		return
	}
	nx, ny := -dy/length*halfT, dx/length*halfT
	z.MoveTo(float32(a.x+nx), float32(a.y+ny))
	z.LineTo(float32(b.x+nx), float32(b.y+ny))
	z.LineTo(float32(b.x-nx), float32(b.y-ny))
	z.LineTo(float32(a.x-nx), float32(a.y-ny))
	z.ClosePath()
}

// strokePolylineMask draws a stroke directly into the coverage mask with
// round-ish joints (a small disc at each joint).
func strokePolylineMask(mask *image.Alpha, pts []point, thickness float64) {
	z := &vector.Rasterizer{}
	z.Reset(mask.Bounds().Dx(), mask.Bounds().Dy())
	halfT := thickness / 2
	for i := 0; i+1 < len(pts); i++ {
		bandQuad(z, pts[i], pts[i+1], halfT)
		// Round caps/joints.
		z.MoveTo(float32(pts[i].x+halfT), float32(pts[i].y))
		addEllipse(z, pts[i].x, pts[i].y, halfT, halfT)
	}
	z.Draw(mask, mask.Bounds(), image.White, image.Point{})
}
