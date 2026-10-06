package render

// Dither — the Go port of DitherPixels.c (dither_apply / dither_dots /
// dither_glow), the 11-style dithering filter. The verbatim C in
// tests/golden/c is the truth. The C parallelizes over disjoint row bands
// (dispatch_apply); the Go port runs the same code serially — the bands
// never share writes, so the output is byte-identical. Diffusion/ordered
// styles are pure arithmetic (ε=0 goldens); scanlines crosses sinf/cosf
// (ε=1).

import "math"

// Dither style codes (DitherPixels.h, the Filter panel's order). The
// DitherStyle prefix avoids colliding with the DitherDots/DitherGlow
// helper functions.
const (
	DitherStyleAtkinson = iota
	DitherStyleFloydSteinberg
	DitherStyleBayer2
	DitherStyleBayer4
	DitherStyleBayer8
	DitherStyleDots
	DitherStyleLines
	DitherStyleDiamonds
	DitherStylePatterns
	DitherStyleGlyphs
	DitherStyleScanlines
)

// DitherParams mirrors the C struct. Glyphs are `glyphCount` coverage maps
// of glyphWidth×glyphHeight bytes (255 = fully inked), least inked first.
type DitherParams struct {
	Style          int
	Levels         int
	Diffusion      float64
	Density        float64
	Contrast       float64
	Cell           int
	Angle          float64
	LightOnDark    bool
	OriginalColors bool
	Dark           [3]uint8
	Light          [3]uint8
	GlyphWidth     int
	GlyphHeight    int
	Glyphs         []uint8
	GlyphCoverage  []float32
	GlyphCount     int
	Dots           float64
	Wobble         float64
}

func ditherClamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ditherAdjustTone: density darkens (positive) or lightens as a gamma; the
// contrast pivots on mid gray.
func ditherAdjustTone(v, gamma, contrast float32) float32 {
	v = float32(math.Pow(float64(ditherClamp01(v)), float64(gamma)))
	return ditherClamp01((v-0.5)*contrast + 0.5)
}

type ditherTap struct{ dx, dy, weight int }

var ditherAtkinsonTaps = []ditherTap{{1, 0, 1}, {2, 0, 1}, {-1, 1, 1}, {0, 1, 1}, {1, 1, 1}, {0, 2, 1}}
var ditherFloydTaps = []ditherTap{{1, 0, 7}, {-1, 1, 3}, {0, 1, 5}, {1, 1, 1}}

// Atkinson passes on only six eighths of the error — the Mac's crisp look.
func ditherKernelFor(style int) ([]ditherTap, float64) {
	if style == DitherStyleAtkinson {
		return ditherAtkinsonTaps, 8
	}
	return ditherFloydTaps, 16
}

func ditherQuantize(v float32, levels int) float32 {
	steps := float32(levels - 1)
	return float32(math.Round(float64(ditherClamp01(v)*steps))) / steps
}

// ditherDiffuse diffuses each plane in serpentine order so the error's
// drift doesn't streak to one side.
func ditherDiffuse(plane []float32, alpha []uint8, width, height int, p *DitherParams) {
	taps, divisor := ditherKernelFor(p.Style)
	for y := 0; y < height; y++ {
		reverse := y&1 == 1
		for i := 0; i < width; i++ {
			x := i
			if reverse {
				x = width - 1 - i
			}
			at := y*width + x
			if alpha[at] == 0 {
				continue
			}
			old := plane[at]
			q := ditherQuantize(old, p.Levels)
			plane[at] = q
			err := (old - q) * float32(p.Diffusion/divisor)
			for _, t := range taps {
				nx := x + t.dx
				if reverse {
					nx = x - t.dx
				}
				ny := y + t.dy
				if nx < 0 || nx >= width || ny >= height {
					continue
				}
				plane[ny*width+nx] += err * float32(t.weight)
			}
		}
	}
}

var ditherBayer8 = [64]uint8{
	0, 32, 8, 40, 2, 34, 10, 42, 48, 16, 56, 24, 50, 18, 58, 26,
	12, 44, 4, 36, 14, 46, 6, 38, 60, 28, 52, 20, 62, 30, 54, 22,
	3, 35, 11, 43, 1, 33, 9, 41, 51, 19, 59, 27, 49, 17, 57, 25,
	15, 47, 7, 39, 13, 45, 5, 37, 63, 31, 55, 23, 61, 29, 53, 21,
}

// ditherOrderedThreshold: smaller Bayer matrices are the top-left corners
// of the 8×8 one, rescaled — the recursive construction nests them.
func ditherOrderedThreshold(style int, x, y int) float32 {
	switch style {
	case DitherStyleBayer2:
		m := [4]uint8{0, 2, 3, 1}
		return (float32(m[(y&1)*2+(x&1)]) + 0.5) / 4
	case DitherStyleBayer4:
		m := [16]uint8{0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5}
		return (float32(m[(y&3)*4+(x&3)]) + 0.5) / 16
	default:
		return (float32(ditherBayer8[(y&7)*8+(x&7)]) + 0.5) / 64
	}
}

func ditherOrdered(v, threshold float32, levels int) float32 {
	steps := float32(levels - 1)
	q := float32(math.Floor(float64(ditherClamp01(v)*steps + threshold)))
	if q > steps {
		q = steps
	}
	return q / steps
}

// ditherSpot: how much of a halftone cell a point must cover before it's
// marked, per screen shape (u, v run −0.5…0.5 across the cell).
func ditherSpot(style int, u, v float32) float32 {
	au := float32(math.Abs(float64(u)))
	av := float32(math.Abs(float64(v)))
	switch style {
	case DitherStyleDots:
		return 3.14159265 * (u*u + v*v)
	case DitherStyleLines:
		return av * 2
	default:
		return au + av
	}
}

// Old Mac fill patterns, 8×8, one byte per row with the leftmost pixel in
// the top bit, sparsest to fullest.
var ditherPatterns = [17][8]uint8{
	{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	{0x80, 0x00, 0x00, 0x00, 0x08, 0x00, 0x00, 0x00},
	{0x88, 0x00, 0x22, 0x00, 0x88, 0x00, 0x22, 0x00},
	{0x80, 0x40, 0x20, 0x10, 0x08, 0x04, 0x02, 0x01},
	{0x88, 0x22, 0x88, 0x22, 0x88, 0x22, 0x88, 0x22},
	{0x00, 0xFF, 0x00, 0x00, 0x00, 0xFF, 0x00, 0x00},
	{0x11, 0x22, 0x44, 0x88, 0x11, 0x22, 0x44, 0x88},
	{0xAA, 0x00, 0xAA, 0x00, 0xAA, 0x00, 0xAA, 0x00},
	{0x88, 0x55, 0x22, 0x55, 0x88, 0x55, 0x22, 0x55},
	{0xFF, 0x80, 0x80, 0x80, 0xFF, 0x08, 0x08, 0x08},
	{0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55},
	{0x81, 0x42, 0x24, 0x18, 0x18, 0x24, 0x42, 0x81},
	{0x77, 0xAA, 0xDD, 0xAA, 0x77, 0xAA, 0xDD, 0xAA},
	{0xEE, 0xDD, 0xBB, 0x77, 0xEE, 0xDD, 0xBB, 0x77},
	{0x77, 0xFF, 0xDD, 0xFF, 0x77, 0xFF, 0xDD, 0xFF},
	{0x7F, 0xFF, 0xFF, 0xFF, 0xF7, 0xFF, 0xFF, 0xFF},
	{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
}

func ditherWritePixel(px []uint8, r, g, b float32) {
	a := float32(px[3]) / 255.0
	px[0] = uint8(math.Round(float64(ditherClamp01(r) * a * 255.0)))
	px[1] = uint8(math.Round(float64(ditherClamp01(g) * a * 255.0)))
	px[2] = uint8(math.Round(float64(ditherClamp01(b) * a * 255.0)))
}

// DitherApply ports dither_apply: dithers premultiplied RGBA in place,
// keeping alpha and leaving fully transparent pixels alone.
func DitherApply(b *Bitmap, p *DitherParams) bool {
	width, height := b.W, b.H
	count := width * height
	if count == 0 {
		return true
	}
	planes := 1
	if p.OriginalColors {
		planes = 3
	}
	tone := make([]float32, count*planes)
	alpha := make([]uint8, count)
	var source []float32
	if p.OriginalColors {
		source = make([]float32, count*3)
	}
	gamma := float32(math.Exp2(p.Density * 1.5))
	contrast := 1.0 + float32(p.Contrast)
	if p.Contrast >= 0 {
		contrast = 1.0 / (1.0 - 0.95*float32(p.Contrast))
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			px := b.Pix[y*width*4+x*4:]
			at := y*width + x
			alpha[at] = px[3]
			var r, g, bl float32
			if px[3] != 0 {
				scale := 1.0 / float32(px[3])
				r = float32(px[0]) * scale
				g = float32(px[1]) * scale
				bl = float32(px[2]) * scale
			}
			if p.OriginalColors {
				tone[at] = ditherAdjustTone(r, gamma, contrast)
				tone[count+at] = ditherAdjustTone(g, gamma, contrast)
				tone[2*count+at] = ditherAdjustTone(bl, gamma, contrast)
				source[at*3] = r
				source[at*3+1] = g
				source[at*3+2] = bl
			} else {
				tone[at] = ditherAdjustTone(0.2126*r+0.7152*g+0.0722*bl, gamma, contrast)
			}
		}
	}
	dark := [3]float32{float32(p.Dark[0]) / 255.0, float32(p.Dark[1]) / 255.0, float32(p.Dark[2]) / 255.0}
	light := [3]float32{float32(p.Light[0]) / 255.0, float32(p.Light[1]) / 255.0, float32(p.Light[2]) / 255.0}
	style := p.Style
	levels := p.Levels
	if levels < 2 {
		levels = 2
	}
	if levels > 16 {
		levels = 16
	}
	if style <= DitherStyleBayer8 {
		// Diffusion and ordered dithering: each plane quantized to `levels`
		// tones, then mapped to colors.
		if style <= DitherStyleFloydSteinberg {
			local := *p
			local.Levels = levels
			for c := 0; c < planes; c++ {
				ditherDiffuse(tone[c*count:(c+1)*count], alpha, width, height, &local)
			}
		} else {
			for c := 0; c < planes; c++ {
				plane := tone[c*count : (c+1)*count]
				for y := 0; y < height; y++ {
					for x := 0; x < width; x++ {
						at := y*width + x
						if alpha[at] != 0 {
							plane[at] = ditherOrdered(plane[at], ditherOrderedThreshold(style, x, y), levels)
						}
					}
				}
			}
		}
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				at := y*width + x
				if alpha[at] == 0 {
					continue
				}
				px := b.Pix[y*width*4+x*4:]
				if p.OriginalColors {
					ditherWritePixel(px, tone[at], tone[count+at], tone[2*count+at])
				} else {
					t := tone[at]
					ditherWritePixel(px,
						dark[0]+(light[0]-dark[0])*t,
						dark[1]+(light[1]-dark[1])*t,
						dark[2]+(light[2]-dark[2])*t)
				}
			}
		}
	} else if style == DitherStyleScanlines {
		// A CRT: each line scans the image, its tone along the line the
		// average of the rows it covers. The beam glows brighter and blooms
		// thicker where the picture is light; the screen between lines stays
		// dark.
		spacing := p.Cell
		if spacing < 2 {
			spacing = 2
		}
		middle := float32(spacing) / 2
		dots := float32(math.Min(1, math.Max(0, p.Dots)))
		scan := make([]float32, width*planes)
		for line := 0; line*spacing < height; line++ {
			top := line * spacing
			bottom := top + spacing
			if bottom > height {
				bottom = height
			}
			// Wobble: each line is pushed sideways — a slow wave down the
			// screen with a quicker one over it.
			wave := float32(math.Sin(float64(line)*0.45))*0.7 + float32(math.Sin(float64(line)*1.7+1.3))*0.3
			shift := int64(math.Round(float64(float32(p.Wobble) * wave)))
			for x := 0; x < width; x++ {
				var sum [3]float32
				n := 0
				sx := int64(x) - shift
				if sx >= 0 && sx < int64(width) {
					for y := top; y < bottom; y++ {
						at := y*width + int(sx)
						if alpha[at] == 0 {
							continue
						}
						for c := 0; c < planes; c++ {
							sum[c] += tone[c*count+at]
						}
						n++
					}
				}
				for c := 0; c < planes; c++ {
					if n > 0 {
						scan[c*width+x] = sum[c] / float32(n)
					} else {
						scan[c*width+x] = 0
					}
				}
			}
			for y := top; y < bottom; y++ {
				offset := float32(math.Abs(float64(y-top) + 0.5 - float64(middle)))
				for x := 0; x < width; x++ {
					if alpha[y*width+x] == 0 {
						continue
					}
					// Dots: the line breaks into beads, one every line
					// spacing, each lit in the color at its middle.
					along := float32(math.Mod(float64(x)+0.5, float64(spacing))) - middle
					centered := int64(math.Round(float64(float32(x) - along*dots)))
					at := 0
					if centered < 0 {
						at = 0
					} else if int64(centered) >= int64(width) {
						at = width - 1
					} else {
						at = int(centered)
					}
					var r, g, bl, t float32
					if p.OriginalColors {
						r = scan[at]
						g = scan[width+at]
						bl = scan[2*width+at]
						t = 0.2126*r + 0.7152*g + 0.0722*bl
					} else {
						t = scan[at]
						r = dark[0] + (light[0]-dark[0])*t
						g = dark[1] + (light[1]-dark[1])*t
						bl = dark[2] + (light[2]-dark[2])*t
					}
					// The beam is driven brighter than the picture, making up
					// for the dark screen between lines.
					r *= 1.35
					g *= 1.35
					bl *= 1.35
					// Half the beam's height: thin in the shadows, most of
					// the way across in the highlights.
					beam := middle * (0.2 + 0.5*float32(math.Sqrt(float64(ditherClamp01(t)))))
					across := along * dots
					distance := float32(math.Sqrt(float64(offset*offset + across*across)))
					cover := ditherClamp01(beam - distance + 0.5)
					var br, bg, bb float32
					if p.OriginalColors {
						br, bg, bb = 0, 0, 0
					} else {
						br, bg, bb = dark[0], dark[1], dark[2]
					}
					ditherWritePixel(b.Pix[y*width*4+x*4:], br+(r-br)*cover, bg+(g-bg)*cover, bb+(bl-bb)*cover)
				}
			}
		}
	} else {
		// Marks (halftone shapes, patterns, glyphs) cover as much of each
		// spot as the tone calls for. On light they stand for darkness and
		// are drawn in the dark color; light on dark, the reverse.
		marks := tone
		if p.OriginalColors {
			marks = make([]float32, count)
			for i := 0; i < count; i++ {
				marks[i] = 0.2126*tone[i] + 0.7152*tone[count+i] + 0.0722*tone[2*count+i]
			}
		}
		cell := p.Cell
		if cell < 2 {
			cell = 2
		}
		cosA := float32(math.Cos(p.Angle))
		sinA := float32(math.Sin(p.Angle))
		var ink, paper [3]float32
		if p.LightOnDark {
			ink, paper = light, dark
		} else {
			ink, paper = dark, light
		}
		// Glyphs: each cell shares one, picked from the cell's average tone.
		gw, gh := p.GlyphWidth, p.GlyphHeight
		if gw < 1 {
			gw = 1
		}
		if gh < 1 {
			gh = 1
		}
		columns := (width + gw - 1) / gw
		cellRows := (height + gh - 1) / gh
		var picked []int
		if style == DitherStyleGlyphs && p.GlyphCount > 0 {
			picked = make([]int, columns*cellRows)
			for row := 0; row < cellRows; row++ {
				for column := 0; column < columns; column++ {
					sum := float32(0)
					n := 0
					for yy := row * gh; yy < (row+1)*gh && yy < height; yy++ {
						for xx := column * gw; xx < (column+1)*gw && xx < width; xx++ {
							i := yy*width + xx
							if alpha[i] != 0 {
								sum += marks[i]
								n++
							}
						}
					}
					t := float32(1)
					if n > 0 {
						t = sum / float32(n)
					}
					wanted := t
					if !p.LightOnDark {
						wanted = 1 - t
					}
					wanted *= p.GlyphCoverage[p.GlyphCount-1]
					best := 0
					bestDistance := float32(2)
					for g := 0; g < p.GlyphCount; g++ {
						d := float32(math.Abs(float64(p.GlyphCoverage[g] - wanted)))
						if d < bestDistance {
							bestDistance = d
							best = g
						}
					}
					picked[row*columns+column] = best
				}
			}
		}
		paperOriginal := float32(1.0)
		if p.LightOnDark {
			paperOriginal = 0.0
		}
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				at := y*width + x
				if alpha[at] == 0 {
					continue
				}
				var amount float32
				if picked != nil {
					glyph := picked[(y/gh)*columns+x/gw]
					amount = float32(p.Glyphs[glyph*gw*gh+(y%gh)*gw+x%gw]) / 255.0
				} else if style == DitherStylePatterns {
					t := marks[at]
					coverage := 1 - t
					if p.LightOnDark {
						coverage = t
					}
					index := int(math.Round(float64(coverage * float32(len(ditherPatterns)-1))))
					amount = float32((ditherPatterns[index][y&7] >> (7 - (x & 7))) & 1)
				} else {
					fx := float32(x) + 0.5
					fy := float32(y) + 0.5
					u := (fx*cosA + fy*sinA) / float32(cell)
					v := (-fx*sinA + fy*cosA) / float32(cell)
					u -= float32(math.Floor(float64(u))) + 0.5
					v -= float32(math.Floor(float64(v))) + 0.5
					t := marks[at]
					mark := 1 - t
					if p.LightOnDark {
						mark = t
					}
					if mark > ditherSpot(style, u, v) {
						amount = 1
					} else {
						amount = 0
					}
				}
				px := b.Pix[y*width*4+x*4:]
				if p.OriginalColors {
					s := source[at*3:]
					ditherWritePixel(px,
						paperOriginal+(s[0]-paperOriginal)*amount,
						paperOriginal+(s[1]-paperOriginal)*amount,
						paperOriginal+(s[2]-paperOriginal)*amount)
				} else {
					ditherWritePixel(px,
						paper[0]+(ink[0]-paper[0])*amount,
						paper[1]+(ink[1]-paper[1])*amount,
						paper[2]+(ink[2]-paper[2])*amount)
				}
			}
		}
	}
	return true
}

// DitherDots ports dither_dots: each block×block square becomes a round dot
// in its own color on gap (straight sRGB), alpha kept.
func DitherDots(b *Bitmap, block int, gap [3]uint8) {
	if block < 2 {
		return
	}
	radius := float32(block) * 0.42
	middle := float32(block) / 2
	for y := 0; y < b.H; y++ {
		dy := float32(y%block) + 0.5 - middle
		for x := 0; x < b.W; x++ {
			px := b.Pix[(y*b.W+x)*4:]
			if px[3] == 0 {
				continue
			}
			dx := float32(x%block) + 0.5 - middle
			cover := ditherClamp01(radius - float32(math.Sqrt(float64(dx*dx+dy*dy))) + 0.5)
			if cover >= 1 {
				continue
			}
			for c := 0; c < 3; c++ {
				px[c] = uint8(math.Round(float64(float32(px[c])*cover + float32(gap[c])*float32(px[3])/255.0*(1-cover))))
			}
		}
	}
}

// DitherGlow ports dither_glow: adds glow's light at amount, never past the
// pixel's own alpha.
func DitherGlow(b *Bitmap, glow *Bitmap, amount float32) {
	for y := 0; y < b.H; y++ {
		row := b.Pix[y*b.W*4:]
		light := glow.Pix[y*glow.W*4:]
		for x := 0; x < b.W*4; x += 4 {
			a := float32(row[x+3])
			for c := 0; c < 3; c++ {
				v := float32(row[x+c]) + float32(light[x+c])*amount*a/255.0
				if v > a {
					v = a
				}
				row[x+c] = uint8(math.Round(float64(v)))
			}
		}
	}
}
