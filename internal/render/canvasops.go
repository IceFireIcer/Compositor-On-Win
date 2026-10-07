// Package render, file canvasops.go: document-level geometry — canvas size
// (CanvasResizer.swift: anchor offset, layer/guide translation, optional
// Canvas Extension fill layer), image size (ImageResizer.swift: per-layer
// re-rasterization at the new scale) and trim (ImageTrim.swift: alpha or
// pixel-colour bounding box). Ticket 43.

package render

import (
	"math"

	"compositor-win/internal/domain"
)

// Anchor is the nine-cell anchor (row-major, top-left through bottom-right;
// 4 is the centre) — CanvasSizeOptions.anchor.
type Anchor int

// AnchorOffset is CanvasSizeOptions.offset(fromWidth:height:): floor puts
// the extra pixel on the right/bottom when expanding and removes it from
// the left/top when shrinking around the centre.
func AnchorOffset(newWidth, newHeight, oldWidth, oldHeight int, anchor Anchor) (float64, float64) {
	n := int(anchor)
	if n < 0 || n > 8 {
		n = 4
	}
	return math.Floor(float64(newWidth-oldWidth) * float64(n%3) / 2),
		math.Floor(float64(newHeight-oldHeight) * float64(n/3) / 2)
}

// CanvasExtension rasterizes the coloured backdrop for an expanded canvas:
// the whole new canvas filled with the colour, with the old canvas
// intersection punched transparent (CanvasResizer's fill step).
func CanvasExtension(newWidth, newHeight, oldWidth, oldHeight int, offsetX, offsetY float64, r, g, b uint8) *Bitmap {
	out := NewBitmap(newWidth, newHeight)
	cr, cg, cb := uint32(r), uint32(g), uint32(b)
	clearL := int(offsetX)
	clearT := int(offsetY)
	clearR := clearL + oldWidth
	clearB := clearT + oldHeight
	for y := 0; y < newHeight; y++ {
		inside := y >= clearT && y < clearB
		for x := 0; x < newWidth; x++ {
			i := (y*newWidth + x) * 4
			if inside && x >= clearL && x < clearR {
				continue // the old canvas intersection stays transparent
			}
			out.Pix[i] = uint8(cr)
			out.Pix[i+1] = uint8(cg)
			out.Pix[i+2] = uint8(cb)
			out.Pix[i+3] = 255
		}
	}
	return out
}

// TrimBasedOn is TrimBasedOn: what counts as trimmable margin.
type TrimBasedOn int

const (
	TrimTransparentPixels TrimBasedOn = iota
	TrimTopLeftPixelColor
	TrimBottomRightPixelColor
)

// TrimOptions mirrors the struct: which edges may trim plus the colour
// tolerance (0–255).
type TrimOptions struct {
	BasedOn   TrimBasedOn
	Top       bool
	Bottom    bool
	Left      bool
	Right     bool
	Tolerance uint8
}

// TrimsAny reports whether any edge may trim.
func (o TrimOptions) TrimsAny() bool { return o.Top || o.Bottom || o.Left || o.Right }

// TrimRect computes the trim rectangle in image coordinates, or ok=false
// when no content would remain (ImageTrim.calculateTrimRect).
func TrimRect(b *Bitmap, options TrimOptions) (x, y, width, height int, ok bool) {
	if !options.TrimsAny() || b.W <= 0 || b.H <= 0 {
		return 0, 0, 0, 0, false
	}
	var left, right, top, bottom int
	switch options.BasedOn {
	case TrimTransparentPixels:
		edges := AlphaBounds(b)
		if edges[2] <= 0 { // fully transparent: nothing remains
			return 0, 0, 0, 0, false
		}
		left, top, right, bottom = edges[0], edges[1], edges[2], edges[3]
	case TrimTopLeftPixelColor:
		l, t, r, bo, found := colorTrimBounds(b, 0, 0, options.Tolerance)
		if !found {
			return 0, 0, 0, 0, false
		}
		left, top, right, bottom = l, t, r, bo
	case TrimBottomRightPixelColor:
		l, t, r, bo, found := colorTrimBounds(b, b.W-1, b.H-1, options.Tolerance)
		if !found {
			return 0, 0, 0, 0, false
		}
		left, top, right, bottom = l, t, r, bo
	default:
		return 0, 0, 0, 0, false
	}
	minX := 0
	if options.Left {
		minX = left
	}
	minY := 0
	if options.Top {
		minY = top
	}
	maxX := b.W
	if options.Right {
		maxX = right
	}
	maxY := b.H
	if options.Bottom {
		maxY = bottom
	}
	if maxX <= minX || maxY <= minY {
		return 0, 0, 0, 0, false
	}
	return minX, minY, maxX - minX, maxY - minY, true
}

// colorTrimBounds finds the content bounds treating pixels within tolerance
// of the sample as trimmable (ImageTrim.calculateColorTrimRect).
func colorTrimBounds(b *Bitmap, sampleX, sampleY int, tolerance uint8) (left, top, right, bottom int, ok bool) {
	i := (sampleY*b.W + sampleX) * 4
	tr, tg, tb, ta := int(b.Pix[i]), int(b.Pix[i+1]), int(b.Pix[i+2]), int(b.Pix[i+3])
	tol := int(tolerance)
	matches := func(x, y int) bool {
		o := (y*b.W + x) * 4
		return absInt(int(b.Pix[o])-tr) <= tol &&
			absInt(int(b.Pix[o+1])-tg) <= tol &&
			absInt(int(b.Pix[o+2])-tb) <= tol &&
			absInt(int(b.Pix[o+3])-ta) <= tol
	}
	left, right, top, bottom = b.W, 0, b.H, 0
	for y := 0; y < b.H; y++ {
		first := 0
		for first < b.W && matches(first, y) {
			first++
		}
		if first == b.W {
			continue
		}
		last := b.W
		for last > first && matches(last-1, y) {
			last--
		}
		if first < left {
			left = first
		}
		if last > right {
			right = last
		}
		if y < top {
			top = y
		}
		bottom = y + 1
	}
	if right <= 0 { // every pixel matched the sample colour
		return 0, 0, 0, 0, false
	}
	return left, top, right, bottom, true
}

// LayerBox is the scaled bounding box of a transformed layer: the four
// unit corners are mapped through the transform, scaled by (sx, sy) and
// floored/ceiled to a pixel grid (ImageResizer's corner math).
func LayerBox(t domain.Transform, sx, sy float64) (left, top float64, width, height int) {
	rad := t.Rotation * math.Pi / 180
	c, sn := math.Cos(rad), math.Sin(rad)
	cx := t.Origin[0] + t.Size[0]/2
	cy := t.Origin[1] + t.Size[1]/2
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, uv := range [4][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
		lx := (uv[0] - 0.5) * t.Size[0]
		ly := (uv[1] - 0.5) * t.Size[1]
		if t.FlipX {
			lx = -lx
		}
		if t.FlipY {
			ly = -ly
		}
		x := (c*lx - sn*ly + cx) * sx
		y := (sn*lx + c*ly + cy) * sy
		minX, minY = math.Min(minX, x), math.Min(minY, y)
		maxX, maxY = math.Max(maxX, x), math.Max(maxY, y)
	}
	left = math.Floor(minX)
	top = math.Floor(minY)
	width = int(math.Ceil(maxX) - left)
	height = int(math.Ceil(maxY) - top)
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	return left, top, width, height
}

// DrawTransformed draws src through t into dst, whose top-left sits at
// document (offX, offY) — the same inverse mapping and sampling the
// compositor's Place uses, at a local grid instead of the whole canvas
// (ImageResizer's per-layer re-rasterization; rotation and flips bake into
// the pixels, so the caller stores the plain origin/size).
func DrawTransformed(src *Bitmap, t domain.Transform, sx, sy float64, dst *Bitmap, offX, offY float64, sampling domain.Sampling) {
	if src == nil || dst == nil || t.Size[0] <= 0 || t.Size[1] <= 0 || sx <= 0 || sy <= 0 {
		return
	}
	rad := t.Rotation * math.Pi / 180
	c, s := math.Cos(rad), math.Sin(rad)
	cx := t.Origin[0] + t.Size[0]/2
	cy := t.Origin[1] + t.Size[1]/2
	sub := 1
	if sampling == domain.SamplingHighQuality {
		sub = 2
	}
	for py := 0; py < dst.H; py++ {
		for px := 0; px < dst.W; px++ {
			dx := (float64(px)+0.5+offX)/sx - cx
			dy := (float64(py)+0.5+offY)/sy - cy
			vx := c*dx + s*dy
			vy := -s*dx + c*dy
			ux := vx/t.Size[0] + 0.5
			uy := vy/t.Size[1] + 0.5
			if t.FlipX {
				ux = 1 - ux
			}
			if t.FlipY {
				uy = 1 - uy
			}
			if ux < 0 || ux >= 1 || uy < 0 || uy >= 1 {
				continue
			}
			var r, g, b, a float64
			switch sampling {
			case domain.SamplingNearest:
				r, g, b, a = sampleNearest(src, ux*float64(src.W), uy*float64(src.H))
			case domain.SamplingSmooth:
				r, g, b, a = sampleBilinear(src, ux*float64(src.W), uy*float64(src.H))
			default:
				for sy := 0; sy < sub; sy++ {
					for sx := 0; sx < sub; sx++ {
						ox := (0.25 + 0.5*float64(sx)) - 0.5
						oy := (0.25 + 0.5*float64(sy)) - 0.5
						r2, g2, b2, a2 := sampleBilinear(src, (ux+ox/float64(src.W))*float64(src.W), (uy+oy/float64(src.H))*float64(src.H))
						r, g, b, a = r+r2, g+g2, b+b2, a+a2
					}
				}
				n := float64(sub * sub)
				r, g, b, a = r/n, g/n, b/n, a/n
			}
			i := (py*dst.W + px) * 4
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = premul(r, g, b, a)
		}
	}
}

// DrawMaskTransformed draws a grayscale coverage mask (stored as the
// brightness in the red channel, as the .comp store keeps it) through the
// same mapping into a w×h gray grid (ImageResizer's drawCoverage path).
// Uniform 1×1 masks and unlinked placements are resolution independent —
// the caller keeps those instead of calling here.
func DrawMaskTransformed(gray []uint8, srcW, srcH int, t domain.Transform, sx, sy float64, w, h int, offX, offY float64, sampling domain.Sampling) []uint8 {
	out := make([]uint8, w*h)
	if srcW <= 0 || srcH <= 0 || len(gray) < srcW*srcH || t.Size[0] <= 0 || t.Size[1] <= 0 || sx <= 0 || sy <= 0 {
		return out
	}
	rad := t.Rotation * math.Pi / 180
	c, s := math.Cos(rad), math.Sin(rad)
	cx := t.Origin[0] + t.Size[0]/2
	cy := t.Origin[1] + t.Size[1]/2
	sample := func(x, y float64) float64 {
		if x < 0 {
			x = 0
		} else if x > float64(srcW-1) {
			x = float64(srcW - 1)
		}
		if y < 0 {
			y = 0
		} else if y > float64(srcH-1) {
			y = float64(srcH - 1)
		}
		x0, y0 := int(x), int(y)
		x1, y1 := x0+1, y0+1
		if x1 >= srcW {
			x1 = srcW - 1
		}
		if y1 >= srcH {
			y1 = srcH - 1
		}
		tx, ty := x-float64(x0), y-float64(y0)
		v00 := float64(gray[y0*srcW+x0])
		v10 := float64(gray[y0*srcW+x1])
		v01 := float64(gray[y1*srcW+x0])
		v11 := float64(gray[y1*srcW+x1])
		top := v00 + (v10-v00)*tx
		bottom := v01 + (v11-v01)*tx
		return top + (bottom-top)*ty
	}
	for py := 0; py < h; py++ {
		for px := 0; px < w; px++ {
			dx := (float64(px)+0.5+offX)/sx - cx
			dy := (float64(py)+0.5+offY)/sy - cy
			vx := c*dx + s*dy
			vy := -s*dx + c*dy
			ux := vx/t.Size[0] + 0.5
			uy := vy/t.Size[1] + 0.5
			if t.FlipX {
				ux = 1 - ux
			}
			if t.FlipY {
				uy = 1 - uy
			}
			if ux < 0 || ux >= 1 || uy < 0 || uy >= 1 {
				continue
			}
			var v float64
			if sampling == domain.SamplingNearest {
				sx := int(ux * float64(srcW))
				sy := int(uy * float64(srcH))
				if sx >= srcW {
					sx = srcW - 1
				}
				if sy >= srcH {
					sy = srcH - 1
				}
				v = float64(gray[sy*srcW+sx])
			} else {
				v = sample(ux*float64(srcW), uy*float64(srcH))
			}
			if v < 0 {
				v = 0
			} else if v > 255 {
				v = 255
			}
			out[py*w+px] = uint8(v + 0.5)
		}
	}
	return out
}
