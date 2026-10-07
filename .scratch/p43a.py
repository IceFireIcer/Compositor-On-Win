p = 'internal/render/canvasops.go'
s = open(p, encoding='utf-8').read()

# Replace ResampleLayer + Sampling block with the compositor-consistent
# transformed draw primitives ImageResizer needs.
start = s.index('// ResampleLayer draws one layer bitmap')
end = s.index('// Sampling selects the resample interpolation')
end2 = len(s)
new = '''// LayerBox is the scaled bounding box of a transformed layer: the four
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
// compositor\'s Place uses, at a local grid instead of the whole canvas
// (ImageResizer's per-layer re-rasterization; rotation and flips bake into
// the pixels, so the caller stores the plain origin/size).
func DrawTransformed(src *Bitmap, t domain.Transform, dst *Bitmap, offX, offY float64, sampling domain.Sampling) {
	if src == nil || dst == nil || t.Size[0] <= 0 || t.Size[1] <= 0 {
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
			dx := float64(px) + offX + 0.5 - cx
			dy := float64(py) + offY + 0.5 - cy
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
func DrawMaskTransformed(gray []uint8, srcW, srcH int, t domain.Transform, w, h int, offX, offY float64, sampling domain.Sampling) []uint8 {
	out := make([]uint8, w*h)
	if srcW <= 0 || srcH <= 0 || len(gray) < srcW*srcH || t.Size[0] <= 0 || t.Size[1] <= 0 {
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
			dx := float64(px) + offX + 0.5 - cx
			dy := float64(py) + offY + 0.5 - cy
			vx := c*dx + s*dy
			vy := -s*dx + c*dy
			ux := 1 - (vx/t.Size[0]+0.5)
			uy := vy/t.Size[1] + 0.5
			// Masks use the layer\'s own unit mapping but never flip: the
			// stored grid already matches the layer orientation.
			if t.FlipX {
				ux = 1 - ux
			}
			if t.FlipY {
				uy = 1 - uy
			}
			if ux < 0 || ux >= 1 || uy < 0 || uy >= 1 {
				continue
			}
			v := sample(ux*float64(srcW), uy*float64(srcH))
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
'''
s = s[:start] + new
open(p, 'w', encoding='utf-8', newline='\n').write(s)
print('canvasops rewritten; tail removed:', s.count('ResampleLayer'))
