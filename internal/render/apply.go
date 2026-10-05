package render

import "math"

// Application kernels, ported from the C files: LevelsPixels.c
// (levels_apply, cube_apply) and AdjustPixels.c (adjust_gradient_map).
// All three run on the premultiplied working format: each pixel's straight
// color is recovered for the lookup and multiplied back by its alpha
// afterwards, so soft edges pass through unchanged.

// ApplyLUT ports levels_apply: each channel's straight byte, as a 0–255
// value, interpolates between the two nearest table entries and multiplies
// back by alpha. Works for any LUT size (the CPU truth builds 256 entries,
// the GPU track 1024). The intermediate arithmetic is float32 throughout —
// the C kernel computes in float, and bit-parity needs the same precision.
func ApplyLUT(b *Bitmap, lut *LUT) {
	parallelFor(b.H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			row := b.Pix[y*b.W*4 : (y+1)*b.W*4]
			for i := 0; i < len(row); i += 4 {
				alpha := row[i+3]
				if alpha == 0 {
					continue
				}
				a := float32(alpha)
				for c := 0; c < 3; c++ {
					table := lut.Tables[c]
					x := float32(row[i+c]) * 255 / float32(alpha)
					if x > 255 {
						x = 255
					}
					pos := x
					if lut.Size != 256 { // C levels_apply indexes a 256 table directly
						pos = x * float32(lut.Size-1) / 255
					}
					lo := int(pos)
					if lo > lut.Size-2 {
						lo = lut.Size - 2
					}
					frac := pos - float32(lo)
					result := table[lo] + (table[lo+1]-table[lo])*frac
					row[i+c] = uint8(math.Min(float64(a), math.Max(0, math.Round(float64(result*a)))))
				}
			}
		}
	})
}

// ApplyCube ports cube_apply: trilinear interpolation between the eight
// nearest entries of the cube (straight colors; alpha is kept). At the top
// edge the lower index clamps to Dimension-2 with fraction 1, so byte 255
// resolves exactly to the last entry without reading past it. All
// intermediate arithmetic is float32 — the C kernel computes in float, and
// bit-parity needs the same precision.
func ApplyCube(b *Bitmap, cube *Cube) {
	n := cube.Dimension
	scale := float32(n-1) / 255
	dy := n
	dz := n * n
	parallelFor(b.H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			row := b.Pix[y*b.W*4 : (y+1)*b.W*4]
			for i := 0; i < len(row); i += 4 {
				alpha := row[i+3]
				if alpha == 0 {
					continue
				}
				a := float32(alpha)
				var lo [3]int
				var frac [3]float32
				for c := 0; c < 3; c++ {
					position := float32(row[i+c]) * 255 / float32(alpha)
					if position > 255 {
						position = 255
					}
					position *= scale
					lo[c] = int(position)
					if lo[c] > n-2 {
						lo[c] = n - 2
					}
					frac[c] = position - float32(lo[c])
				}
				base := ((lo[0] + lo[1]*dy + lo[2]*dz) * 4)
				sx, sy, sz := 4, dy*4, dz*4
				for c := 0; c < 3; c++ {
					at := func(offset int) float32 { return cube.Data[base+offset+c] }
					x00 := at(0) + (at(sx)-at(0))*frac[0]
					x10 := at(sy) + (at(sy+sx)-at(sy))*frac[0]
					x01 := at(sz) + (at(sz+sx)-at(sz))*frac[0]
					x11 := at(sz+sy) + (at(sz+sy+sx)-at(sz+sy))*frac[0]
					y0 := x00 + (x10-x00)*frac[1]
					y1 := x01 + (x11-x01)*frac[1]
					result := y0 + (y1-y0)*frac[2]
					row[i+c] = uint8(math.Min(float64(a), math.Max(0, math.Round(float64(result*a)))))
				}
			}
		}
	})
}

// ApplyGradientMap ports adjust_gradient_map: each pixel's Rec. 709
// luminance (integer arithmetic, as the C kernel computes it) picks a color
// from the 256×3 table, which is multiplied back by the pixel's alpha.
func ApplyGradientMap(b *Bitmap, table []uint8) {
	parallelFor(b.H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			row := b.Pix[y*b.W*4 : (y+1)*b.W*4]
			for i := 0; i < len(row); i += 4 {
				a := uint32(row[i+3])
				if a == 0 {
					continue
				}
				r, g, bl := uint32(row[i]), uint32(row[i+1]), uint32(row[i+2])
				if a < 255 {
					r = (r*255 + a/2) / a
					g = (g*255 + a/2) / a
					bl = (bl*255 + a/2) / a
					if r > 255 {
						r = 255
					}
					if g > 255 {
						g = 255
					}
					if bl > 255 {
						bl = 255
					}
				}
				level := (2126*r + 7152*g + 722*bl + 5000) / 10000
				if level > 255 {
					level = 255
				}
				color := table[level*3 : level*3+3]
				row[i] = uint8((uint32(color[0])*a + 127) / 255)
				row[i+1] = uint8((uint32(color[1])*a + 127) / 255)
				row[i+2] = uint8((uint32(color[2])*a + 127) / 255)
			}
		}
	})
}
