package render

import "math"

// Per-pixel adjustment kernels, ported line-for-line from the macOS C core:
// AdjustPixels.c (adjust_grain :64-96 with its mix32/lattice/grain_field
// helpers, adjust_black_white :110-152, adjust_color_balance :156-196 with
// tonal_weights) and NoisePixels.c (noise_add). Invert ports the vImage pass
// of Document/PixelInvert.swift. Verbatim C copies live in tests/golden/c/
// (provenance in c/README.md). All arithmetic mirrors the C kernel's
// precision — float32 where C computes in float — so the golden comparison
// stays bit-faithful; a case's epsilon covers only the C-library
// transcendentals (logf/cosf in the Gaussian branch).
//
// Every kernel runs on premultiplied RGBA and leaves alpha alone: each
// pixel's straight color is recovered for the math and multiplied back by
// its alpha afterwards.

// mix32 ports AdjustPixels.c mix32.
func mix32(x uint32) uint32 {
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return x
}

// lattice ports AdjustPixels.c lattice: A value in −1…1 for an integer lattice point, fixed by the point and the seed. Two uniform halves
// summed give a triangular spread, closer to film grain than flat noise.
func lattice(ix, iy int64, seed uint32) float32 {
	h := mix32((uint32(ix) * 0x9E3779B1) ^ mix32((uint32(iy) * 0x85EBCA77) ^ seed))
	return float32(h&0xFFFF)/65535.0 + float32(h>>16)/65535.0 - 1.0
}

// grainField ports AdjustPixels.c grain_field: Smooth seeded noise whose features follow `scale` document pixels. Keeping both the broad and
// detailed patterns relative to the requested grain size makes Size remain visible at any Roughness.
func grainField(u, v, scale float64, seed uint32) float32 {
	cellX := math.Floor(u / scale)
	cellY := math.Floor(v / scale)
	tx := float32(u/scale - cellX)
	ty := float32(v/scale - cellY)
	tx = tx * tx * (3.0 - 2.0*tx)
	ty = ty * ty * (3.0 - 2.0*ty)
	ix, iy := int64(cellX), int64(cellY)
	n00 := lattice(ix, iy, seed)
	n10 := lattice(ix+1, iy, seed)
	n01 := lattice(ix, iy+1, seed)
	n11 := lattice(ix+1, iy+1, seed)
	top := n00 + (n10-n00)*tx
	bottom := n01 + (n11-n01)*tx
	// Blending neighboring lattice values narrows the spread; restore approximately its original range.
	return (top + (bottom-top)*ty) * 1.6
}

func clamp255f(value float32) float32 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return value
}

// roundPremultiplied ports the write-back adjust_black_white and
// adjust_color_balance share: (uint8_t)fminf(alpha, fmaxf(0, roundf(color*alpha))).
func roundPremultiplied(color, alpha float32) uint8 {
	return uint8(min(alpha, max(0.0, float32(math.Round(float64(color*alpha))))))
}

// ApplyGrain ports adjust_grain: a seeded noise field anchored in document
// space — `originX`/`originY`/`unitsPerPixel` place the bitmap's pixels
// there (a whole layer at 1:1 is origin zero, one unit per pixel), so the
// pattern stays put however the canvas redraws parts of it.
func ApplyGrain(b *Bitmap, amount, size, roughness float64, seed uint32, originX, originY, unitsPerPixel float64) {
	if !(amount > 0) || !(unitsPerPixel > 0) {
		return
	}
	if !(size > 0) {
		size = 1
	}
	strength := float32(math.Min(1.0, amount/100.0)) * 0.35 * 255.0
	rough := float32(math.Max(0.0, math.Min(roughness/100.0, 1.0)))
	fineSeed := mix32(seed ^ 0xA511E9B3)
	// Roughness adds smaller, less regular particles, as in Photoshop, but their size remains
	// proportional to the Size control instead of collapsing to fixed one-pixel noise.
	detailSize := math.Max(0.5, size*0.35)
	parallelFor(b.H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			v := originY + (float64(y)+0.5)*unitsPerPixel
			row := b.Pix[y*b.W*4 : (y+1)*b.W*4]
			for x, i := 0, 0; i < len(row); x, i = x+1, i+4 {
				a := row[i+3]
				if a == 0 {
					continue
				}
				u := originX + (float64(x)+0.5)*unitsPerPixel
				smooth := grainField(u, v, size, seed)
				fine := grainField(u, v, detailSize, fineSeed)
				noise := smooth + (fine-smooth)*rough
				unpremultiply := float32(1)
				if a != 255 {
					unpremultiply = 255.0 / float32(a)
				}
				r := float32(row[i]) * unpremultiply
				g := float32(row[i+1]) * unpremultiply
				bl := float32(row[i+2]) * unpremultiply
				level := (0.2126*r + 0.7152*g + 0.0722*bl) / 255.0
				if level > 1 {
					level = 1
				}
				// Film grain shows most in the midtones.
				delta := noise * strength * (0.4 + 2.4*level*(1.0-level))
				coverage := float32(a) / 255.0
				row[i] = uint8(clamp255f(r+delta) * coverage + 0.5)
				row[i+1] = uint8(clamp255f(g+delta) * coverage + 0.5)
				row[i+2] = uint8(clamp255f(bl+delta) * coverage + 0.5)
			}
		}
	})
}

// noiseHash ports NoisePixels.c noise_hash: A well-mixed 32-bit hash, so neighbouring pixels get unrelated values.
func noiseHash(x uint32) uint32 {
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return x
}

// noiseUnit ports NoisePixels.c noise_unit: Uniform in [0, 1).
func noiseUnit(key uint32) float32 {
	return float32(noiseHash(key)>>8) * (1.0 / 16777216.0)
}

// ApplyAddNoise ports NoisePixels.c noise_add: position+seed deterministic
// noise at the image origin — the same pixel always gets the same value for
// a given seed, across runs and across partial redraws. (The C file's
// noise_add_at generalizes the origin for tiled application; the whole-
// raster truth path pins it to zero, as the golden driver does.)
func ApplyAddNoise(b *Bitmap, amount float32, gaussian, monochromatic bool, seed uint32) {
	spread := amount / 100.0 * 127.5
	parallelFor(b.H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			row := b.Pix[y*b.W*4 : (y+1)*b.W*4]
			for x, i := 0, 0; i < len(row); x, i = x+1, i+4 {
				alpha := row[i+3]
				if alpha == 0 {
					continue
				}
				px := uint32(x)
				py := uint32(y)
				base := noiseHash(seed ^ noiseHash((px * 0x9e3779b9) ^ noiseHash(py*0x85ebca6b)))
				for c := 0; c < 3; c++ {
					key := base
					if !monochromatic {
						key = base + uint32(c)*0x9e3779b9
					}
					var n float32
					if gaussian {
						// Box–Muller: two uniform values make one normally distributed one.
						u1 := noiseUnit(key)
						u2 := noiseUnit(key ^ 0x68e31da4)
						n = float32(math.Sqrt(float64(-2.0 * float32(math.Log(float64(1.0-u1)))))) *
							float32(math.Cos(float64(6.2831853*u2))) * spread * (2.0 / 3.0)
					} else {
						n = (noiseUnit(key)*2.0 - 1.0) * spread
					}
					value := float32(row[i+c])*255.0/float32(alpha) + n
					value = min(max(value, 0), 255)
					row[i+c] = uint8(math.Round(float64(value * float32(alpha) / 255.0)))
				}
			}
		}
	})
}

// ApplyBlackWhite ports adjust_black_white: Which of the six ranges a color's primary and secondary fall in, and how much of each it holds.
// A color is min(r,g,b) of gray, plus (mid-min) of the secondary between its two brightest channels,
// plus (max-mid) of the primary of its brightest — so the weights below are exactly Photoshop's.
// The weights arrive at kernel scale (the Swift caller divides the sliders by 100).
func ApplyBlackWhite(b *Bitmap, weights [6]float32, tint bool, tintHue, tintSaturation float64) {
	parallelFor(b.H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			row := b.Pix[y*b.W*4 : (y+1)*b.W*4]
			for i := 0; i < len(row); i += 4 {
				alpha := float32(row[i+3])
				if alpha == 0 {
					continue
				}
				r := float32(row[i]) * 255.0 / alpha
				g := float32(row[i+1]) * 255.0 / alpha
				bl := float32(row[i+2]) * 255.0 / alpha
				r = min(255.0, r) / 255.0
				g = min(255.0, g) / 255.0
				bl = min(255.0, bl) / 255.0
				mx := max(r, max(g, bl))
				mn := min(r, min(g, bl))
				md := r + g + bl - mx - mn
				// weights: 0 red, 1 yellow, 2 green, 3 cyan, 4 blue, 5 magenta
				var primary, secondary int
				if mx == r {
					primary = 0
					if g >= bl {
						secondary = 1
					} else {
						secondary = 5
					}
				} else if mx == g {
					primary = 2
					if r >= bl {
						secondary = 1
					} else {
						secondary = 3
					}
				} else {
					primary = 4
					if g >= r {
						secondary = 3
					} else {
						secondary = 5
					}
				}
				gray := mn + (md-mn)*weights[secondary] + (mx-md)*weights[primary]
				gray = min(1.0, max(0.0, gray))
				outR, outG, outB := gray, gray, gray
				if tint && tintSaturation > 0 {
					// The gray becomes the lightness of a color at the chosen hue.
					c := (1.0 - math.Abs(2.0*float64(gray)-1.0)) * tintSaturation
					hp := math.Mod(tintHue, 360.0) / 60.0
					xx := c * (1.0 - math.Abs(math.Mod(hp, 2.0)-1.0))
					var r1, g1, b1 float64
					if hp < 1 {
						r1, g1 = c, xx
					} else if hp < 2 {
						r1, g1 = xx, c
					} else if hp < 3 {
						g1, b1 = c, xx
					} else if hp < 4 {
						g1, b1 = xx, c
					} else if hp < 5 {
						r1, b1 = xx, c
					} else {
						r1, b1 = c, xx
					}
					m := float64(gray) - c/2.0
					outR = float32(min(1.0, max(0.0, r1+m)))
					outG = float32(min(1.0, max(0.0, g1+m)))
					outB = float32(min(1.0, max(0.0, b1+m)))
				}
				row[i] = roundPremultiplied(outR, alpha)
				row[i+1] = roundPremultiplied(outG, alpha)
				row[i+2] = roundPremultiplied(outB, alpha)
			}
		}
	})
}

// tonalWeights ports AdjustPixels.c tonal_weights: How much a tone belongs to the shadows, midtones and highlights: three overlapping curves that sum
// to about one across the range, so a shift fades in and out rather than banding at a threshold.
func tonalWeights(v float32) (shadow, mid, highlight float32) {
	const (
		a     = 0.25
		b     = 0.333
		scale = 0.7
	)
	s := (v-b)/-a + 0.5
	h := (v+b-1.0)/a + 0.5
	s = min(1.0, max(0.0, s))
	h = min(1.0, max(0.0, h))
	m1 := min(1.0, max(0.0, (v-b)/a+0.5))
	m2 := min(1.0, max(0.0, (v+b-1.0)/-a+0.5))
	return s * scale, m1 * m2 * scale, h * scale
}

// ApplyColorBalance ports adjust_color_balance: the three tonal-range shifts
// (at kernel scale — the Swift caller divides the −100…100 sliders by 100)
// with Rec. 709-style 0.299/0.587/0.114 luminosity preservation.
func ApplyColorBalance(b *Bitmap, shadows, midtones, highlights [3]float32, preserveLuminosity bool) {
	parallelFor(b.H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			row := b.Pix[y*b.W*4 : (y+1)*b.W*4]
			for i := 0; i < len(row); i += 4 {
				alpha := float32(row[i+3])
				if alpha == 0 {
					continue
				}
				var c [3]float32
				for j := 0; j < 3; j++ {
					c[j] = min(255.0, float32(row[i+j])*255.0/alpha) / 255.0
				}
				before := 0.299*c[0] + 0.587*c[1] + 0.114*c[2]
				for j := 0; j < 3; j++ {
					s, m, h := tonalWeights(c[j])
					c[j] += shadows[j]*s + midtones[j]*m + highlights[j]*h
					c[j] = min(1.0, max(0.0, c[j]))
				}
				if preserveLuminosity {
					after := 0.299*c[0] + 0.587*c[1] + 0.114*c[2]
					if after > 0.0001 {
						ratio := before / after
						for j := 0; j < 3; j++ {
							c[j] = min(1.0, max(0.0, c[j]*ratio))
						}
					}
				}
				for j := 0; j < 3; j++ {
					row[i+j] = roundPremultiplied(c[j], alpha)
				}
			}
		}
	})
}

// ApplyInvert ports PixelInvert.swift's vImage pass: on premultiplied RGBA
// each color becomes alpha − color (the 4×4 matrix −256/256 with divisor
// 256, applied as pixel × matrix), so transparency is kept and alpha itself
// is unchanged. For straight colors at full coverage this is the familiar
// 255 − v. The low clamp mirrors vImage's output clipping; valid
// premultiplied bytes keep every result in range.
func ApplyInvert(b *Bitmap) {
	parallelFor(b.H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			row := b.Pix[y*b.W*4 : (y+1)*b.W*4]
			for i := 0; i < len(row); i += 4 {
				a := int(row[i+3])
				for c := 0; c < 3; c++ {
					v := a - int(row[i+c])
					if v < 0 {
						v = 0
					}
					row[i+c] = uint8(v)
				}
			}
		}
	})
}
