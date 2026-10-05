package render

import (
	"math"
	"runtime"
	"sync"

	"compositor-win/internal/domain"
)

// parallelFor splits [0,n) into bands across CPUs. fn must be pure per-row
// work with no cross-row dependence — determinism guarantee of the CPU
// compositor (ticket 09: parallel output is byte-identical to serial).
func parallelFor(n int, fn func(y0, y1 int)) {
	workers := runtime.GOMAXPROCS(0)
	if workers > n {
		workers = n
	}
	if workers <= 1 {
		fn(0, n)
		return
	}
	band := (n + workers - 1) / workers
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		y0 := w * band
		y1 := y0 + band
		if y1 > n {
			y1 = n
		}
		if y0 >= y1 {
			break
		}
		wg.Add(1)
		go func(y0, y1 int) {
			defer wg.Done()
			fn(y0, y1)
		}(y0, y1)
	}
	wg.Wait()
}

// Place maps src into document space per the layer transform: the image is
// stretched to size, rotated clockwise about its center, flipped, and
// positioned at origin. Sampling: Nearest = nearest neighbor, Smooth =
// bilinear, High quality = 2×2 rotated-grid supersampled bilinear.
// Out-of-bounds document pixels stay transparent.
func Place(src *Bitmap, t domain.Transform, docW, docH int) *Bitmap {
	out := NewBitmap(docW, docH)
	rad := t.Rotation * math.Pi / 180
	c, s := math.Cos(rad), math.Sin(rad)
	cx := t.Origin[0] + t.Size[0]/2
	cy := t.Origin[1] + t.Size[1]/2
	sw := src.W
	sh := src.H
	sub := 1
	if t.Sampling == domain.SamplingHighQuality {
		sub = 2
	}
	parallelFor(docH, func(y0, y1 int) {
		for py := y0; py < y1; py++ {
			for px := 0; px < docW; px++ {
				// Inverse map: doc → rotate(−θ) about center → unit square.
				dx := float64(px) + 0.5 - cx
				dy := float64(py) + 0.5 - cy
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
				switch t.Sampling {
				case domain.SamplingNearest:
					r, g, b, a = sampleNearest(src, ux*float64(sw), uy*float64(sh))
				case domain.SamplingSmooth:
					r, g, b, a = sampleBilinear(src, ux*float64(sw), uy*float64(sh))
				default: // High quality: rotated-grid supersample
					for sy := 0; sy < sub; sy++ {
						for sx := 0; sx < sub; sx++ {
							off := 0.25 + 0.5*float64(sy)
							ox := (0.25 + 0.5*float64(sx)) - 0.5
							oy := off - 0.5
							r2, g2, b2, a2 := sampleBilinear(src, (ux+ox/float64(sw))*float64(sw), (uy+oy/float64(sh))*float64(sh))
							r, g, b, a = r+r2, g+g2, b+b2, a+a2
						}
					}
					n := float64(sub * sub)
					r, g, b, a = r/n, g/n, b/n, a/n
				}
				i := (py*docW + px) * 4
				out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = premul(r, g, b, a)
			}
		}
	})
	return out
}

// sampleNearest is nearest-neighbor lookup; out-of-range returns transparent
// (clamping happens at the unit-square level).
func sampleNearest(src *Bitmap, x, y float64) (float64, float64, float64, float64) {
	sx := int(math.Floor(x))
	sy := int(math.Floor(y))
	i := (sy*src.W + sx) * 4
	a := float64(src.Pix[i+3]) / 255
	if a == 0 {
		return 0, 0, 0, 0
	}
	return float64(src.Pix[i]) / 255 / a, float64(src.Pix[i+1]) / 255 / a, float64(src.Pix[i+2]) / 255 / a, a
}

func sampleBilinear(src *Bitmap, x, y float64) (float64, float64, float64, float64) {
	x -= 0.5
	y -= 0.5
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-math.Floor(x), y-math.Floor(y)
	var r, g, b, a float64
	for dy := 0; dy <= 1; dy++ {
		for dx := 0; dx <= 1; dx++ {
			sx, sy := x0+dx, y0+dy
			if sx < 0 || sy < 0 || sx >= src.W || sy >= src.H {
				continue
			}
			w := (1 - math.Abs(fx-float64(dx))) * (1 - math.Abs(fy-float64(dy)))
			if w == 0 {
				continue
			}
			i := (sy*src.W + sx) * 4
			pa := float64(src.Pix[i+3]) / 255
			a += w * pa
			if pa > 0 {
				r += w * float64(src.Pix[i]) / 255 / pa * pa
				g += w * float64(src.Pix[i+1]) / 255 / pa * pa
				b += w * float64(src.Pix[i+2]) / 255 / pa * pa
			}
		}
	}
	if a > 0 {
		// Colors were accumulated premultiplied (w·pa·straight); normalize.
		return r / a, g / a, b / a, a
	}
	return 0, 0, 0, 0
}
