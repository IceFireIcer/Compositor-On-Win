package render

// Sharp reductions for Export As and Image Size (ticket V05). A single box
// average — and Core Graphics' one-step resample — look at only a few
// neighbouring pixels, so shrinking an image 4x or 8x comes out soft.
//
// DownsampleCache answers that with a chain of Lanczos halvings. This does the
// same filtering in one pass instead: the kernel widens with the reduction
// factor, which is the correct low-pass, and staying in float to the end avoids
// the byte rounding a chain would quantise at every step. The result depends
// only on the pixels, so previews and exports agree.

import "math"

// lanczosSupport is the Lanczos-3 kernel's half-width, in source pixels.
const lanczosSupport = 3.0

func lanczosKernel(x float64) float64 {
	if x < 0 {
		x = -x
	}
	if x < 1e-9 {
		return 1
	}
	if x >= lanczosSupport {
		return 0
	}
	px := math.Pi * x
	return (lanczosSupport * math.Sin(px) * math.Sin(px/lanczosSupport)) / (px * px)
}

// lanczosTaps returns the source window and normalised weights for one output
// position: `center` in source pixels, `scale` the source pixels per output
// pixel (1 or more when shrinking, so the kernel widens and cuts aliasing).
func lanczosTaps(center, scale, limit float64) (int, int, []float64) {
	support := lanczosSupport * scale
	lo := int(math.Ceil(center - support))
	hi := int(math.Floor(center + support))
	weights := make([]float64, hi-lo+1)
	total := 0.0
	for i := range weights {
		sx := float64(lo + i)
		if sx < 0 || sx >= limit {
			continue
		}
		w := lanczosKernel((sx - center) / scale)
		weights[i] = w
		total += w
	}
	if total != 0 {
		for i := range weights {
			weights[i] /= total
		}
	}
	return lo, hi, weights
}

// LanczosResample resizes src to w×h. The filter runs on premultiplied sRGB
// like every other pixel path here, so a translucent edge neither darkens nor
// haloes; weights are normalised, so a constant image stays constant.
func LanczosResample(src *Bitmap, w, h int) *Bitmap {
	if src == nil || w < 1 || h < 1 || src.W < 1 || src.H < 1 {
		return NewBitmap(max(w, 1), max(h, 1))
	}
	if w == src.W && h == src.H {
		return src.Clone()
	}
	in := make([]float64, src.W*src.H*4)
	for i, v := range src.Pix {
		in[i] = float64(v)
	}
	// Horizontal pass, then vertical over its output.
	mid := make([]float64, w*src.H*4)
	xScale := 1.0
	if w < src.W {
		xScale = float64(src.W) / float64(w)
	}
	for y := 0; y < src.H; y++ {
		for x := 0; x < w; x++ {
			lo, hi, weights := lanczosTaps((float64(x)+0.5)*float64(src.W)/float64(w)-0.5, xScale, float64(src.W))
			var acc [4]float64
			for sx := lo; sx <= hi; sx++ {
				weight := weights[sx-lo]
				if weight == 0 || sx < 0 || sx >= src.W {
					continue
				}
				for c := 0; c < 4; c++ {
					acc[c] += in[(y*src.W+sx)*4+c] * weight
				}
			}
			i := (y*w + x) * 4
			for c := 0; c < 4; c++ {
				mid[i+c] = acc[c]
			}
		}
	}
	out := NewBitmap(w, h)
	yScale := 1.0
	if h < src.H {
		yScale = float64(src.H) / float64(h)
	}
	for y := 0; y < h; y++ {
		lo, hi, weights := lanczosTaps((float64(y)+0.5)*float64(src.H)/float64(h)-0.5, yScale, float64(src.H))
		for x := 0; x < w; x++ {
			var acc [4]float64
			for sy := lo; sy <= hi; sy++ {
				weight := weights[sy-lo]
				if weight == 0 || sy < 0 || sy >= src.H {
					continue
				}
				for c := 0; c < 4; c++ {
					acc[c] += mid[(sy*w+x)*4+c] * weight
				}
			}
			i := (y*w + x) * 4
			for c := 0; c < 4; c++ {
				out.Pix[i+c] = uint8(math.Round(max(0, min(255, acc[c]))))
			}
		}
	}
	return out
}
