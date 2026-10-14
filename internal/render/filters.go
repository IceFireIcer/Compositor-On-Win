package render

// Filter-menu kernels (Filters.swift PixelFilter.run): Gaussian/motion blur
// and bloom are Core Image-side in the original, so the Go ports reproduce
// their documented semantics (padded spread, radius definitions) rather than
// bit-exact CI output; colored vignette and tonal contrast are verbatim
// ports of the AdjustPixels.c kernels and golden-benchmarked. All kernels
// work on premultiplied RGBA8 and keep alpha.

import "math"

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// gaussianWeights builds the normalized 1-D Gaussian of σ with the given
// integer radius (weights [0]=center).
func gaussianWeights(sigma float64, radius int) []float64 {
	weights := make([]float64, radius+1)
	for i := 0; i <= radius; i++ {
		weights[i] = math.Exp(-0.5 * float64(i*i) / (sigma * sigma))
	}
	// Mirror symmetry: total = center + 2×Σ(rest).
	total := weights[0]
	for i := 1; i <= radius; i++ {
		total += 2 * weights[i]
	}
	for i := range weights {
		weights[i] /= total
	}
	return weights
}

// EdgeSides names the bitmap sides that sit at the canvas's edge, where a
// blur carries the edge pixels on past the frame (clamp-to-edge) instead of
// fading into the clear pixels beyond — Photoshop's behavior for a picture
// filling the canvas (Filters.swift PixelFilter.extending / LayerSides). The
// bits match LayerSides: left 1, right 2, top 4, bottom 8, and top is the
// image's row 0.
type EdgeSides int

const (
	EdgeLeft EdgeSides = 1 << iota
	EdgeRight
	EdgeTop
	EdgeBottom
)

// ApplyGaussianBlur blurs premultiplied RGBA in place with a true separable
// Gaussian, σ = sigma. Samples outside the frame are transparent, so a
// caller that pads the layer first (Filters.blurMargin ≈ 3σ + 2) gets the
// original's "spreads into the room made for it" behavior instead of a
// clamped border smear.
func ApplyGaussianBlur(b *Bitmap, sigma float64) {
	ApplyGaussianBlurSides(b, sigma, 0)
}

// ApplyGaussianBlurSides is ApplyGaussianBlur with clamp-to-edge sampling on
// the sides named in clamp: out-of-frame samples there repeat the edge pixel,
// as Filters.swift's extending(past:of:) does at the canvas's edge. Sides not
// named still sample transparent, so a stroke inside the canvas keeps
// softening into the clear pixels around it.
func ApplyGaussianBlurSides(b *Bitmap, sigma float64, clamp EdgeSides) {
	if sigma <= 0 || b.W == 0 || b.H == 0 {
		return
	}
	radius := int(math.Ceil(sigma * 3))
	if radius < 1 {
		radius = 1
	}
	weights := gaussianWeights(sigma, radius)
	src := b.Clone()
	tmp := NewBitmap(b.W, b.H)
	blurPassAxis(src, tmp, weights, radius, b.W, b.H, true, clamp)
	blurPassAxis(tmp, b, weights, radius, b.W, b.H, false, clamp)
}

// blurPreserveEps guards the unpremultiply of a blurred pixel whose alpha
// rounded to zero.
const blurPreserveEps = 1e-6

// ApplyGaussianBlurPreservingAlpha blurs only the colors, from the image's
// own pixels, and keeps every pixel's alpha — the Go port of Filters.swift's
// blurred.unpremultiplyingAlpha().settingAlphaOne(in:)
// .applyingFilter("CIBlendWithAlphaMask", background: empty, mask: edges).
//
// Dividing the blurred premultiplied color by the blurred alpha takes out the
// pull of the clear pixels beside it, which would otherwise darken and thin
// the colors near the outline; multiplying back by the original alpha puts
// the outline back exactly where it was. A pixel that was clear stays clear.
func ApplyGaussianBlurPreservingAlpha(b *Bitmap, sigma float64) {
	if sigma <= 0 || b.W == 0 || b.H == 0 {
		return
	}
	orig := b.Clone()
	ApplyGaussianBlur(b, sigma)
	for i := 0; i < len(b.Pix); i += 4 {
		alpha := float64(orig.Pix[i+3])
		if alpha == 0 {
			b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = 0, 0, 0, 0
			continue
		}
		blurred := math.Max(float64(b.Pix[i+3]), blurPreserveEps)
		for c := 0; c < 3; c++ {
			straight := math.Min(1, float64(b.Pix[i+c])/blurred)
			b.Pix[i+c] = uint8(math.Round(straight * alpha))
		}
		b.Pix[i+3] = orig.Pix[i+3]
	}
}

// blurPassAxis runs one separable-Gaussian direction: horizontal reads
// neighbors along x, vertical along y; out-of-frame contributes nothing
// unless clamp names that side, where the edge pixel is carried on instead.
func blurPassAxis(src, dst *Bitmap, weights []float64, radius int, w, h int, horizontal bool, clamp EdgeSides) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var acc [4]float64
			for k := -radius; k <= radius; k++ {
				wt := weights[maxInt(-k, k)]
				if wt == 0 {
					continue
				}
				var sx, sy int
				if horizontal {
					sx, sy = x+k, y
				} else {
					sx, sy = x, y+k
				}
				if sx < 0 {
					if clamp&EdgeLeft == 0 {
						continue // transparent outside
					}
					sx = 0
				} else if sx >= w {
					if clamp&EdgeRight == 0 {
						continue
					}
					sx = w - 1
				}
				if sy < 0 {
					if clamp&EdgeTop == 0 {
						continue
					}
					sy = 0
				} else if sy >= h {
					if clamp&EdgeBottom == 0 {
						continue
					}
					sy = h - 1
				}
				i := (sy*w + sx) * 4
				acc[0] += wt * float64(src.Pix[i])
				acc[1] += wt * float64(src.Pix[i+1])
				acc[2] += wt * float64(src.Pix[i+2])
				acc[3] += wt * float64(src.Pix[i+3])
			}
			o := (y*w + x) * 4
			dst.Pix[o] = uint8(math.Round(acc[0]))
			dst.Pix[o+1] = uint8(math.Round(acc[1]))
			dst.Pix[o+2] = uint8(math.Round(acc[2]))
			dst.Pix[o+3] = uint8(math.Round(acc[3]))
		}
	}
}

// MotionRadiusPerPixel is CIMotionBlur's radius per pixel of streak length
// (Filters.swift): an even streak of length d spreads d/√12, so this radius
// matches its spread.
var MotionRadiusPerPixel = 1 / math.Sqrt(12.0)

// ApplyMotionBlur smears premultiplied RGBA along the given angle
// (counterclockwise degrees from horizontal, as in Photoshop; the bitmap's
// y axis points down, so the sample direction flips y). The streak is a
// linear (box) average of half-length radiusPx — the caller scales the
// slider distance into it and pads the layer by distance/2 + 2.
func ApplyMotionBlur(b *Bitmap, angleDeg, radiusPx float64) {
	ApplyMotionBlurSides(b, angleDeg, radiusPx, 0)
}

// ApplyMotionBlurSides is ApplyMotionBlur with clamp-to-edge sampling on the
// sides named in clamp, so a streak at the canvas's edge carries the layer's
// edge pixels on rather than fading (Filters.swift extending(past:of:)).
func ApplyMotionBlurSides(b *Bitmap, angleDeg, radiusPx float64, clamp EdgeSides) {
	if radiusPx <= 0 || b.W == 0 || b.H == 0 {
		return
	}
	radians := angleDeg * math.Pi / 180
	dx := math.Cos(radians)
	dy := -math.Sin(radians) // bitmap y points down
	half := int(math.Ceil(radiusPx))
	if half < 1 {
		half = 1
	}
	src := b.Clone()
	tmp := NewBitmap(b.W, b.H)
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			var acc [4]float64
			weightSum := 0.0
			for k := -half; k <= half; k++ {
				// Sub-pixel sample positions across the streak; the two ends
				// get partial weight so the streak length is exact.
				t := float64(k)
				wt := 1.0
				if k == -half && radiusPx < float64(half) {
					wt = radiusPx - float64(half-1)
				} else if k == half && radiusPx < float64(half) {
					wt = radiusPx - float64(half-1)
				}
				if wt <= 0 {
					continue
				}
				sx := float64(x) + 0.5 + dx*t
				sy := float64(y) + 0.5 + dy*t
				ix := int(math.Floor(sx))
				iy := int(math.Floor(sy))
				fx, fy := sx-float64(ix), sy-float64(iy)
				// Past a clamped side the edge pixel repeats, so the sample
				// lands on that pixel with no fraction across the boundary.
				if ix < 0 {
					if clamp&EdgeLeft == 0 {
						continue // transparent outside
					}
					ix, fx = 0, 0
				} else if ix >= b.W {
					if clamp&EdgeRight == 0 {
						continue
					}
					ix, fx = b.W-1, 0
				}
				if iy < 0 {
					if clamp&EdgeTop == 0 {
						continue
					}
					iy, fy = 0, 0
				} else if iy >= b.H {
					if clamp&EdgeBottom == 0 {
						continue
					}
					iy, fy = b.H-1, 0
				}
				i := (iy*b.W + ix) * 4
				for c := 0; c < 4; c++ {
					var sample float64
					if fx == 0 && fy == 0 {
						sample = float64(src.Pix[i+c])
					} else {
						sample = bilinearSample(src.Pix, b.W, b.H, i, ix, iy, fx, fy, c, clamp)
					}
					acc[c] += wt * sample
				}
				weightSum += wt
			}
			o := (y*b.W + x) * 4
			if weightSum > 0 {
				for c := 0; c < 4; c++ {
					tmp.Pix[o+c] = uint8(math.Round(acc[c] / weightSum))
				}
			}
		}
	}
	copy(b.Pix, tmp.Pix)
}

// bilinearSample interpolates one channel at (ix+fx, iy+fy) inside a
// premultiplied RGBA byte grid whose base index of (ix,iy) is i. The
// neighbor past the frame is transparent, unless clamp names that side, in
// which case the edge pixel repeats there (so a streak at the canvas's edge
// does not thin out against a pixel that isn't there).
func bilinearSample(pix []uint8, w, h, i, ix, iy int, fx, fy float64, c int, clamp EdgeSides) float64 {
	x1, y1 := ix+1, iy+1
	inX := x1 < w
	inY := y1 < h
	v00 := float64(pix[i+c])
	var v10, v01, v11 float64
	switch {
	case inX:
		v10 = float64(pix[i+4+c])
	case clamp&EdgeRight != 0:
		v10 = v00
	}
	switch {
	case inY:
		v01 = float64(pix[i+w*4+c])
	case clamp&EdgeBottom != 0:
		v01 = v00
	}
	switch {
	case inX && inY:
		v11 = float64(pix[i+w*4+4+c])
	case inX && clamp&EdgeBottom != 0:
		v11 = v10
	case inY && clamp&EdgeRight != 0:
		v11 = v01
	case clamp&EdgeRight != 0 && clamp&EdgeBottom != 0:
		v11 = v00
	}
	top := v00*(1-fx) + v10*fx
	bottom := v01*(1-fx) + v11*fx
	return top*(1-fy) + bottom*fy
}

// ApplyBloom adds a glow the way the original's CIBloom does at its
// semantics level: the image is blurred at radius and the light that
// spreads beyond the source (blur − source, never darkening) is added back
// scaled by intensity. intensity 0 or radius 0 leaves the image unchanged.
func ApplyBloom(b *Bitmap, radius, intensity float64) {
	if radius <= 0 || intensity == 0 || b.W == 0 || b.H == 0 {
		return
	}
	blurred := b.Clone()
	ApplyGaussianBlur(blurred, radius)
	for i := 0; i < len(b.Pix); i += 4 {
		for c := 0; c < 4; c++ {
			spread := float64(blurred.Pix[i+c]) - float64(b.Pix[i+c])
			if spread <= 0 {
				continue
			}
			v := float64(b.Pix[i+c]) + intensity*spread
			if v > 255 {
				v = 255
			}
			b.Pix[i+c] = uint8(math.Round(v))
		}
	}
}

// ---------------------------------------------------------------------------
// adjust_colored_vignette — verbatim C port (AdjustPixels.c)
// ---------------------------------------------------------------------------

// ApplyColoredVignette ports adjust_colored_vignette: blends straight sRGB
// toward the edge color with the Camera Raw falloff shape. The frame is the
// rect the vignette shapes to, in this bitmap's pixels (top-down rows);
// with fillsClear it paints transparent pixels too, without it recolors
// only the pixels that are there.
func ApplyColoredVignette(b *Bitmap, frameX, frameY, frameWidth, frameHeight float64, fillsClear bool,
	amount, midpoint, roundness, feather, highlights float64, red, green, blue float64) {
	if amount <= 0 || b.W == 0 || b.H == 0 || frameWidth <= 0 || frameHeight <= 0 {
		return
	}
	strength := cameraClamp(amount / 100.0)
	red = cameraClamp(red)
	green = cameraClamp(green)
	blue = cameraClamp(blue)
	fill := 0
	if fillsClear {
		fill = 1
	}
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			i := (y*b.W + x) * 4
			p := b.Pix[i : i+4 : i+4]
			if p[3] == 0 && fill == 0 {
				continue
			}
			mask := camVignetteMaskAt(float64(x)+0.5-frameX, float64(y)+0.5-frameY,
				frameWidth, frameHeight, midpoint, roundness, feather)
			if mask <= 0 {
				continue
			}
			alpha := float64(p[3]) / 255.0
			var r, g, bl, bright float64
			if p[3] != 0 {
				r = math.Min(1.0, float64(p[0])/float64(p[3]))
				g = math.Min(1.0, float64(p[1])/float64(p[3]))
				bl = math.Min(1.0, float64(p[2])/float64(p[3]))
				bright = cameraClamp((camRec709(r, g, bl) - 0.45) / 0.55)
			}
			effect := strength * mask * (1.0 - (highlights/100.0)*bright)
			if fill == 0 {
				// Only the pixels that are there change color; their coverage
				// stays as it was.
				writePremulD(p, r+(red-r)*effect, g+(green-g)*effect, bl+(blue-bl)*effect, float64(p[3]))
				continue
			}
			// An opaque pixel moves toward the color, a clear one takes it on.
			out := alpha + effect*(1.0-alpha)
			if out <= 0 {
				continue
			}
			r = (red*effect + r*alpha*(1.0-effect)) / out
			g = (green*effect + g*alpha*(1.0-effect)) / out
			bl = (blue*effect + bl*alpha*(1.0-effect)) / out
			na := out * 255.0
			if na > 255 {
				na = 255
			}
			p[3] = uint8(math.Round(na))
			writePremulD(p, r, g, bl, float64(p[3]))
		}
	}
}

// ---------------------------------------------------------------------------
// adjust_tonal_contrast — verbatim C port (AdjustPixels.c)
// ---------------------------------------------------------------------------

// ApplyTonalContrast ports adjust_tonal_contrast: local luminance contrast
// against `blurred` (the same image Gaussian-blurred at the detail radius,
// as Filters.swift's CIGaussianBlur base), with independent shadow,
// midtone, and highlight gains.
func ApplyTonalContrast(b, blurred *Bitmap, amount, shadows, midtones, highlights float64) {
	if amount <= 0 || (shadows == 0 && midtones == 0 && highlights == 0) {
		return
	}
	if b.W != blurred.W || b.H != blurred.H {
		return
	}
	strength := amount / 50.0
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			i := (y*b.W + x) * 4
			p := b.Pix[i : i+4 : i+4]
			base := blurred.Pix[i : i+4 : i+4]
			alpha := float64(p[3])
			if alpha == 0 || base[3] == 0 {
				continue
			}
			r := math.Min(1.0, float64(p[0])/alpha)
			g := math.Min(1.0, float64(p[1])/alpha)
			bl := math.Min(1.0, float64(p[2])/alpha)
			lum := camRec709(r, g, bl)
			baseLum := camRec709(
				math.Min(1.0, float64(base[0])/float64(base[3])),
				math.Min(1.0, float64(base[1])/float64(base[3])),
				math.Min(1.0, float64(base[2])/float64(base[3])))
			shadowWeight := 1.0 - tonalSmooth(0.15, 0.5, baseLum)
			highlightWeight := tonalSmooth(0.5, 0.85, baseLum)
			midtoneWeight := 1.0 - shadowWeight - highlightWeight
			weight := (shadows*shadowWeight + midtones*midtoneWeight +
				highlights*highlightWeight) / 100.0
			detail := lum - baseLum
			delta := 0.18 * math.Tanh(detail*6.0) * weight * strength * (4.0 * lum * (1.0 - lum))
			writePremulD(p, cameraClamp(r+delta), cameraClamp(g+delta), cameraClamp(bl+delta), alpha)
		}
	}
}

func tonalSmooth(low, high, value float64) float64 {
	t := cameraClamp((value - low) / (high - low))
	return t * t * (3.0 - 2.0*t)
}
