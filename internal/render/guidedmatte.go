package render

// GuidedMatte — the Go port of GuidedMatte.swift: guided filtering
// (He, Sun & Tang) that pulls a segmentation mask onto the edges of the
// image it came from, plus the refine chain SubjectRemoval applies on top
// (edge shift by blur+threshold, contrast about mid gray). All arithmetic
// is float32 like the Swift [Float] planes. There is no C kernel — the
// numbers are pinned by unit tests instead.

// GuidedBox is the mean over a (2r+1)² square as two running-sum passes —
// the cost doesn't grow with the radius.
func GuidedBox(source []float32, width, height, radius int) []float32 {
	span := float32(radius*2 + 1)
	pass := make([]float32, width*height)
	for y := 0; y < height; y++ {
		row := y * width
		sum := float32(0)
		for x := -radius; x <= radius; x++ {
			sum += source[row+clampInt(x, width)]
		}
		for x := 0; x < width; x++ {
			pass[row+x] = sum / span
			sum -= source[row+clampInt(x-radius, width)]
			sum += source[row+clampInt(x+radius+1, width)]
		}
	}
	result := make([]float32, width*height)
	for x := 0; x < width; x++ {
		sum := float32(0)
		for y := -radius; y <= radius; y++ {
			sum += pass[clampInt(y, height)*width+x]
		}
		for y := 0; y < height; y++ {
			result[y*width+x] = sum / span
			sum -= pass[clampInt(y-radius, height)*width+x]
			sum += pass[clampInt(y+radius+1, height)*width+x]
		}
	}
	return result
}

func clampInt(v, limit int) int {
	if v < 0 {
		return 0
	}
	if v > limit-1 {
		return limit - 1
	}
	return v
}

// GuidedFilter refines `mask` by `guide` (both 0–1, the same size). A
// bigger radius reaches further for detail; epsilon decides how much of an
// edge in the guide counts, so a small one follows fine strands.
func GuidedFilter(mask, guide []float32, width, height, radius int, epsilon float32) []float32 {
	count := width * height
	meanGuide := GuidedBox(guide, width, height, radius)
	meanMask := GuidedBox(mask, width, height, radius)
	squares := make([]float32, count)
	products := make([]float32, count)
	for i := 0; i < count; i++ {
		squares[i] = guide[i] * guide[i]
		products[i] = guide[i] * mask[i]
	}
	meanSquares := GuidedBox(squares, width, height, radius)
	meanProducts := GuidedBox(products, width, height, radius)
	slope := make([]float32, count)
	offset := make([]float32, count)
	for i := 0; i < count; i++ {
		variance := meanSquares[i] - meanGuide[i]*meanGuide[i]
		covariance := meanProducts[i] - meanGuide[i]*meanMask[i]
		slope[i] = covariance / (variance + epsilon)
		offset[i] = meanMask[i] - slope[i]*meanGuide[i]
	}
	meanSlope := GuidedBox(slope, width, height, radius)
	meanOffset := GuidedBox(offset, width, height, radius)
	result := make([]float32, count)
	for i := 0; i < count; i++ {
		result[i] = min(1, max(0, meanSlope[i]*guide[i]+meanOffset[i]))
	}
	return result
}

// BitmapLevels downsamples the bitmap to width×height and returns its
// Rec. 709 gray levels as 0–1 floats (GuidedMatte.levels).
func BitmapLevels(b *Bitmap, width, height int) []float32 {
	small := DownscaleBitmap(b, width, height)
	out := make([]float32, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := (y*width + x) * 4
			a := float32(small.Pix[i+3])
			var gray float32
			if a == 0 {
				gray = 0
			} else {
				r := float32(small.Pix[i]) / a
				g := float32(small.Pix[i+1]) / a
				bl := float32(small.Pix[i+2]) / a
				gray = 0.2126*r + 0.7152*g + 0.0722*bl
			}
			out[y*width+x] = min(1, gray)
		}
	}
	return out
}

// GuidedUpscale draws the small refined levels back up (bilinear), the way
// GuidedMatte.refine redrawing the small image does.
func GuidedUpscale(levels []float32, sw, sh, w, h int) []float32 {
	out := make([]float32, w*h)
	for y := 0; y < h; y++ {
		sy := (float64(y) + 0.5) * float64(sh) / float64(h)
		fy0 := math_floor(sy)
		fy := sy - float64(fy0)
		y0 := int(fy0)
		y1 := min(sh-1, y0+1)
		for x := 0; x < w; x++ {
			sx := (float64(x) + 0.5) * float64(sw) / float64(w)
			fx0 := math_floor(sx)
			fx := sx - float64(fx0)
			x0 := int(fx0)
			x1 := min(sw-1, x0+1)
			v00 := levels[y0*sw+x0]
			v10 := levels[y0*sw+x1]
			v01 := levels[y1*sw+x0]
			v11 := levels[y1*sw+x1]
			out[y*w+x] = float32(
				float64(v00)*(1-fx)*(1-fy) +
					float64(v10)*fx*(1-fy) +
					float64(v01)*(1-fx)*fy +
					float64(v11)*fx*fy)
		}
	}
	return out
}

func math_floor(v float64) float64 {
	if v < 0 {
		return float64(int(v) - 1)
	}
	return float64(int(v))
}

// RefineSubjectMask runs SubjectRemoval.refined: the guided filter pulls
// the mask onto the guide's edges, the edge shift grows/shrinks it by
// blur+threshold, and the contrast pushes its grays apart. limit caps the
// working size (the preview path passes a smaller one). All inputs are the
// full-size mask and guide bitmaps; the result is a full-size gray bitmap.
func RefineSubjectMask(mask, guide *Bitmap, refineEdges, shiftEdge, matteContrast float64, limit float64) *Bitmap {
	fullW, fullH := mask.W, mask.H
	factor := min(1.0, limit/float64(max(1, max(fullW, fullH))))
	width := max(1, int(float64(fullW)*factor+0.5))
	height := max(1, int(float64(fullH)*factor+0.5))
	maskLevels := BitmapLevels(mask, width, height)
	guideLevels := BitmapLevels(guide, width, height)
	steps := max(1, int(refineEdges*factor+0.5))
	refined := GuidedFilter(maskLevels, guideLevels, width, height, steps, 1e-4)

	// Edge shift: a blur then a hard threshold at the matching level moves
	// the edge by the blur's reach.
	if shiftEdge != 0 {
		reach := absF(shiftEdge)
		blurred := GuidedBox(refined, width, height, max(1, int(reach/2)))
		level := 0.25
		if shiftEdge < 0 {
			level = 0.75
		}
		for i, v := range blurred {
			x := (float64(v) - level) / 0.001
			if x < 0 {
				x = 0
			}
			if x > 1 {
				x = 1
			}
			refined[i] = float32(x)
		}
	}
	// Contrast: 0 leaves the mask; 100 is a hard cut at the middle.
	if matteContrast > 0 {
		strength := matteContrast / 100
		slope := 1 / max(0.02, 1-strength*0.98)
		bias := (1 - slope) / 2
		for i, v := range refined {
			refined[i] = float32(min(1, max(0, slope*float64(v)+bias)))
		}
	}
	if width != fullW || height != fullH {
		refined = GuidedUpscale(refined, width, height, fullW, fullH)
	}
	out := NewBitmap(fullW, fullH)
	for i, v := range refined {
		g := uint8(min(255, max(0, float64(v)*255+0.5)))
		out.Pix[i*4] = g
		out.Pix[i*4+1] = g
		out.Pix[i*4+2] = g
		out.Pix[i*4+3] = 255
	}
	return out
}

// ApplySubjectMask multiplies the layer's pixels by the gray mask toward
// clear (CIBlendWithMask against a clear background): rgb and alpha scale
// by mask/255 in the premultiplied domain.
func ApplySubjectMask(b, mask *Bitmap) {
	for i := 0; i+3 < len(b.Pix); i += 4 {
		m := float32(mask.Pix[i]) / 255 // the mask is opaque gray; its R holds the level
		if m >= 1 {
			continue
		}
		b.Pix[i] = uint8(float32(b.Pix[i]) * m)
		b.Pix[i+1] = uint8(float32(b.Pix[i+1]) * m)
		b.Pix[i+2] = uint8(float32(b.Pix[i+2]) * m)
		b.Pix[i+3] = uint8(float32(b.Pix[i+3]) * m)
	}
}

// MultiplyGrayMasks combines two masks (both opaque gray bitmaps): what
// either hides stays hidden (SubjectRemoval.subjectMask's multiply).
func MultiplyGrayMasks(a, b *Bitmap) *Bitmap {
	out := a.Clone()
	for i := 0; i+3 < len(out.Pix); i += 4 {
		for c := 0; c < 3; c++ {
			out.Pix[i+c] = uint8(uint32(out.Pix[i+c]) * uint32(b.Pix[i+c]) / 255)
		}
	}
	return out
}

// LargestSubjectAt keeps the connected component of the subject mask that
// contains (px, py) — the object-selection trace — and clears the rest
// (8-connected flood over mask > 0). Returns false when the point falls on
// an empty mask pixel.
func LargestSubjectAt(mask *Bitmap, px, py int) bool {
	w, h := mask.W, mask.H
	if px < 0 || py < 0 || px >= w || py >= h || mask.Pix[(py*w+px)*4] == 0 {
		return false
	}
	keep := make([]bool, w*h)
	queue := make([]int, 0, w*h)
	start := py*w + px
	keep[start] = true
	queue = append(queue, start)
	for len(queue) > 0 {
		p := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		x, y := p%w, p/w
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				nx, ny := x+dx, y+dy
				if nx < 0 || ny < 0 || nx >= w || ny >= h {
					continue
				}
				q := ny*w + nx
				if keep[q] || mask.Pix[q*4] == 0 {
					continue
				}
				keep[q] = true
				queue = append(queue, q)
			}
		}
	}
	for i := range keep {
		if !keep[i] {
			mask.Pix[i*4] = 0
			mask.Pix[i*4+1] = 0
			mask.Pix[i*4+2] = 0
		}
	}
	return true
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
