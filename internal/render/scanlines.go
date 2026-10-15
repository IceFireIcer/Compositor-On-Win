package render

// Scanlines — the Go port of Scanlines.swift's ScanlinesSettings.apply and
// DitherPixels.c's scanlines_apply / draw_lines: the independent Filter ›
// Scanlines kernel (ticket 60), split out of the Dither styles. The verbatim
// C in tests/golden/c is the truth (provenance in tests/golden/c/README.md).
// The C parallelizes over disjoint row/column bands (dispatch_apply); the Go
// port runs the same code serially — the bands never share writes, so the
// output is byte-identical. The kernel crosses sinf/fmodf/sqrtf/powf/exp2f
// (ε=1 goldens, recorded in the case notes).

import "math"

// ScanlinesParams mirrors the C struct (DitherPixels.h).
type ScanlinesParams struct {
	// How far apart the lines are, in pixels.
	Spacing int
	// How much of the gap a full-bright line fills, 0-1; dimmer parts draw it thinner.
	Thickness float32
	// 0-1: tones darker than this break the lines into round dots, through dashes
	// into the solid line above it.
	Dots float32
	// Pixels the lines waver sideways, in a wave down the screen.
	Wobble float32
	// Pixels a line rises where the picture under it is bright (or, negative,
	// falls), so the lines swell into the picture's shapes.
	Displace float32
	// 0-1: tones darker than this draw no line, leaving the screen dark.
	Threshold float32
	// Pixels the red and blue are moved apart, for colored fringes.
	Split float32
	// -1...1: darker or lighter, and flatter or punchier, before the lines are drawn.
	Density  float32
	Contrast float32
	// 0-1: how bright the lines are where the picture is black, as a CRT's
	// brightness knob lifts its black.
	BlackLevel float32
	// 0-1: how far the brightness is smoothed along and across the lines before
	// it displaces them.
	Smoothness float32
	// 0: light lines on dark (straight sRGB). 1: the lines take the picture's own colors.
	OriginalColors bool
	Dark           [3]uint8
	Light          [3]uint8
}

// scanlinesIndex clamps an index into [0, width) — the C code's
// `x < 0 ? 0 : x >= width ? width - 1 : x` edge clamp.
func scanlinesIndex(v int64, width int) int {
	if v < 0 {
		return 0
	}
	if v >= int64(width) {
		return width - 1
	}
	return int(v)
}

// ScanlinesApply ports scanlines_apply: draws premultiplied RGBA pixels as
// scanlines in place, keeping alpha and leaving fully transparent pixels
// alone. Returns false if working memory couldn't be had (Go allocations
// either succeed or panic, so it mirrors the C return contract only).
func ScanlinesApply(b *Bitmap, p *ScanlinesParams) bool {
	width, height := b.W, b.H
	count := width * height
	if count == 0 {
		return true
	}
	spacing := p.Spacing
	if spacing < 2 {
		spacing = 2
	}
	lines := (height + spacing - 1) / spacing
	planes := 1
	if p.OriginalColors {
		planes = 3
	}
	tone := make([]float32, count*planes)
	alpha := make([]uint8, count)
	// One tone per line along its direction: the average of the rows it covers,
	// sampled where the row's wobble puts it.
	scan := make([]float32, lines*width*planes)

	// Density darkens (positive) or lightens through a gamma, holding both ends;
	// contrast pivots around mid gray.
	gamma := float32(math.Exp2(float64(p.Density) * 1.5))
	contrast := float32(1)
	if p.Contrast >= 0 {
		contrast = 1 / (1 - 0.95*p.Contrast)
	} else {
		contrast = 1 + p.Contrast
	}
	original := p.OriginalColors
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			px := b.Pix[(y*width+x)*4:]
			at := y*width + x
			alpha[at] = px[3]
			var r, g, bl float32
			if px[3] != 0 {
				scale := 1 / float32(px[3])
				r = float32(px[0]) * scale
				g = float32(px[1]) * scale
				bl = float32(px[2]) * scale
			}
			if original {
				tone[at] = ditherAdjustTone(r, gamma, contrast)
				tone[count+at] = ditherAdjustTone(g, gamma, contrast)
				tone[2*count+at] = ditherAdjustTone(bl, gamma, contrast)
			} else {
				tone[at] = ditherAdjustTone(0.2126*r+0.7152*g+0.0722*bl, gamma, contrast)
			}
		}
	}
	wobble := p.Wobble
	for line := 0; line < lines; line++ {
		top := line * spacing
		bottom := top + spacing
		if bottom > height {
			bottom = height
		}
		// A slow wave over a fast one, as a CRT's picture sways when sync drifts.
		wave := float32(math.Sin(float64(line)*0.45))*0.7 + float32(math.Sin(float64(line)*1.7+1.3))*0.3
		shift := int64(math.Round(float64(wobble * wave)))
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
					scan[(c*lines+line)*width+x] = sum[c] / float32(n)
				} else {
					scan[(c*lines+line)*width+x] = 0
				}
			}
		}
	}

	if !scanlinesDrawLines(b, p, scan, alpha, lines, spacing) {
		return false
	}

	// Split: red one way and blue the other, so the lines' edges take a colored fringe.
	split := int64(math.Round(float64(p.Split)))
	if split > 0 {
		copyRow := make([]uint8, width*4)
		for y := 0; y < height; y++ {
			row := b.Pix[y*width*4:]
			copy(copyRow, row)
			for x := 0; x < width; x++ {
				from := int64(x) - split
				to := int64(x) + split
				a := row[x*4+3]
				var r, bl uint8
				if from >= 0 {
					r = copyRow[from*4]
				}
				if to < int64(width) {
					bl = copyRow[to*4+2]
				}
				if r > a {
					r = a
				}
				if bl > a {
					bl = a
				}
				row[x*4] = r
				row[x*4+2] = bl
			}
		}
	}
	return true
}

// scanlinesDrawLines ports draw_lines: it draws the lines column by column over
// the tones it scanned. Once displaced it is a landscape seen head-on (the
// Rutt-Etra video synthesizer's trick): each line is lifted by the hills the
// picture's smoothed brightness makes (or pushed the other way) and hides what
// is behind it, so the near lines wrap over the shapes instead of crossing the
// lines behind them. The nearest one is drawn first: the bottom line when the
// lines rise, the top one when they fall.
func scanlinesDrawLines(b *Bitmap, p *ScanlinesParams, scan []float32, alpha []uint8, lines, spacing int) bool {
	width, height := b.W, b.H
	original := p.OriginalColors
	plane := lines * width
	tone := make([]float32, plane)
	lift := make([]float32, plane)
	for at := 0; at < plane; at++ {
		if original {
			tone[at] = 0.2126*scan[at] + 0.7152*scan[plane+at] + 0.0722*scan[2*plane+at]
		} else {
			tone[at] = scan[at]
		}
	}
	// The height: the brightness is blurred along each line (three box passes,
	// near enough to a Gaussian) and smoothed across neighbouring rows, so a face
	// lifts as one rounded hill rather than as the staircase of the pixels under it.
	smoothness := ditherClamp01(p.Smoothness)
	radius := int64(math.Round(float64(smoothness * float32(spacing) * 2)))
	copy(lift, tone)
	if radius > 0 && p.Displace != 0 {
		source := make([]float32, width)
		for line := 0; line < lines; line++ {
			row := lift[line*width : (line+1)*width]
			for pass := 0; pass < 3; pass++ {
				copy(source, row)
				sum := float32(0)
				for x := -radius; x <= radius; x++ {
					sum += source[scanlinesIndex(x, width)]
				}
				for x := 0; x < width; x++ {
					row[x] = sum / float32(2*radius+1)
					out := int64(x) - radius
					in := int64(x) + radius + 1
					sum += source[scanlinesIndex(in, width)] - source[scanlinesIndex(out, width)]
				}
			}
		}
		across := int64(math.Round(float64(smoothness * 2)))
		spread := make([]float32, plane)
		for step := int64(0); step < across; step++ {
			for line := 0; line < lines; line++ {
				above, below := line, line
				if line > 0 {
					above = line - 1
				}
				if line+1 < lines {
					below = line + 1
				}
				a := lift[above*width:]
				h := lift[line*width:]
				bl := lift[below*width:]
				for x := 0; x < width; x++ {
					spread[line*width+x] = (a[x] + 2*h[x] + bl[x]) / 4
				}
			}
			copy(lift, spread)
		}
	}

	middle := float32(spacing) / 2
	dots := ditherClamp01(p.Dots)
	threshold := ditherClamp01(p.Threshold)
	displace := p.Displace
	thickness := ditherClamp01(p.Thickness)
	blackLevel := ditherClamp01(p.BlackLevel)
	rising := displace >= 0
	screen := [3]float32{float32(p.Dark[0]) / 255, float32(p.Dark[1]) / 255, float32(p.Dark[2]) / 255}
	phosphor := [3]float32{float32(p.Light[0]) / 255, float32(p.Light[1]) / 255, float32(p.Light[2]) / 255}
	if original {
		screen[0], screen[1], screen[2] = 0, 0, 0
	}
	cover := make([]float32, height)
	color := make([]float32, height*3)
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			cover[y] = 0
		}
		along := float32(math.Mod(float64(x)+0.5, float64(spacing))) - middle
		// The edge of what this column has already drawn; lines further back only
		// show beyond it.
		horizon := float32(math.Inf(1))
		if !rising {
			horizon = float32(math.Inf(-1))
		}
		for step := 0; step < lines; step++ {
			line := step
			if rising {
				line = lines - 1 - step
			}
			// Beading: a line darker than the Dots level breaks into beads, one per
			// spacing, each taking the tone at its middle; just above the level it
			// breaks into dashes, and brighter still it closes into a solid line.
			beading := float32(0)
			if dots > 0 {
				beading = ditherClamp01((dots - tone[line*width+x]) / 0.2)
			}
			across := along * beading
			centered := int64(math.Round(float64(float32(x) - across)))
			at := scanlinesIndex(centered, width)
			i := line*width + at
			t := tone[i]
			lit := float32(1)
			if threshold > 0 {
				lit = ditherClamp01((t - threshold) / 0.04)
			}
			if lit <= 0 {
				continue
			}
			// Drawn between this height and the previous column's, so a steep slope
			// does not break the line.
			base := float32(line)*float32(spacing) + middle
			here := base - displace*lift[i]
			before := here
			if at > 0 {
				before = base - displace*lift[i-1]
			}
			lo := float32(math.Min(float64(here), float64(before)))
			hi := float32(math.Max(float64(here), float64(before)))
			// Black level: the line's floor brightness, still visible where the
			// picture is fully black.
			level := blackLevel + (1-blackLevel)*t
			var c [3]float32
			for k := 0; k < 3; k++ {
				if original {
					c[k] = blackLevel + (1-blackLevel)*scan[k*plane+i]
				} else {
					c[k] = screen[k] + (phosphor[k]-screen[k])*level
				}
			}
			// Half the line's height: thinner where the picture is dark.
			beam := middle * thickness * (0.29 + 0.71*float32(math.Sqrt(float64(ditherClamp01(level)))))
			var top, bottom int64
			if rising {
				top = int64(math.Floor(float64(lo - beam - 1)))
				bottom = int64(math.Ceil(float64(math.Min(float64(hi+beam+1), float64(horizon+1)))))
			} else {
				top = int64(math.Floor(float64(math.Max(float64(lo-beam-1), float64(horizon-1)))))
				bottom = int64(math.Ceil(float64(hi + beam + 1)))
			}
			if top < 0 {
				top = 0
			}
			if bottom > int64(height) {
				bottom = int64(height)
			}
			for y := top; y < bottom; y++ {
				yy := float32(y) + 0.5
				off := float32(0)
				if yy < lo {
					off = lo - yy
				} else if yy > hi {
					off = yy - hi
				}
				distance := float32(math.Sqrt(float64(off*off + across*across)))
				hidden := float32(0)
				if rising {
					hidden = ditherClamp01(horizon - yy + 0.5)
				} else {
					hidden = ditherClamp01(yy - horizon + 0.5)
				}
				shown := ditherClamp01(beam-distance+0.5) * lit * hidden
				if shown > cover[y] {
					cover[y] = shown
					color[y*3] = c[0]
					color[y*3+1] = c[1]
					color[y*3+2] = c[2]
				}
			}
			if lit > 0.5 {
				if rising {
					horizon = float32(math.Min(float64(horizon), float64(lo-beam)))
				} else {
					horizon = float32(math.Max(float64(horizon), float64(hi+beam)))
				}
			}
		}
		for y := 0; y < height; y++ {
			if alpha[y*width+x] == 0 {
				continue
			}
			// The beam is brighter than the picture itself, making up for what the
			// dark screen swallows.
			shown := cover[y]
			ditherWritePixel(b.Pix[(y*width+x)*4:],
				screen[0]+(color[y*3]*1.35-screen[0])*shown,
				screen[1]+(color[y*3+1]*1.35-screen[1])*shown,
				screen[2]+(color[y*3+2]*1.35-screen[2])*shown)
		}
	}
	return true
}

// ScanlinesSettings is Scanlines.swift's filter form: the Filter panel's
// sliders, with its ranges and defaults.
type ScanlinesSettings struct {
	// How far apart the lines are, in layer pixels.
	LineSpacing float64 `json:"lineSpacing"`
	// How much of the gap a full-bright line fills, 5-100%.
	Thickness float64 `json:"thickness"`
	// 0-100%: light blooming around the lines.
	Glow float64 `json:"glow"`
	// 0-100%: tones darker than this break the lines into round dots.
	Dots float64 `json:"dots"`
	// Pixels the lines waver sideways.
	Wobble float64 `json:"wobble"`
	// Pixels a line rises where the picture is bright (or falls, negative).
	Displace float64 `json:"displace"`
	// 0-100%: how far the brightness is smoothed before it displaces the lines.
	Smoothness float64 `json:"smoothness"`
	// 0-100%: tones darker than this draw no line.
	Threshold float64 `json:"threshold"`
	// Pixels the red and blue are moved apart.
	Split float64 `json:"split"`
	// -100...100: darker or lighter, flatter or punchier, before the lines are drawn.
	Density  float64 `json:"density"`
	Contrast float64 `json:"contrast"`
	// 0-100%: how bright the lines stay where the picture is fully black.
	BlackLevel float64    `json:"blackLevel"`
	Colors     int        `json:"colors"`
	Dark       [3]float64 `json:"dark"`
	Light      [3]float64 `json:"light"`
}

// DefaultScanlinesSettings is Scanlines.swift's ScanlinesSettings(): the
// Filter panel's starting values. The zero value is NOT the default — every
// range's fallback only applies to NaN/Inf, so a partial payload clamps from
// zero (the same shape DitherSettings has).
func DefaultScanlinesSettings() ScanlinesSettings {
	return ScanlinesSettings{
		LineSpacing: 4, Thickness: 70, Glow: 0, Dots: 0, Wobble: 0, Displace: 0,
		Smoothness: 50, Threshold: 0, Split: 0, Density: 0, Contrast: 0,
		BlackLevel: 0, Colors: DitherColorsBlackWhite,
		Dark: [3]float64{0, 0, 0}, Light: [3]float64{1, 1, 1},
	}
}

// Normalized clamps with the Scanlines.swift ranges and defaults.
func (s ScanlinesSettings) Normalized() ScanlinesSettings {
	r := s
	r.LineSpacing = math.Round(ditherClampRange(s.LineSpacing, 2, 32, 4))
	r.Thickness = ditherClampRange(s.Thickness, 5, 100, 70)
	r.Glow = ditherClampRange(s.Glow, 0, 100, 0)
	r.Dots = ditherClampRange(s.Dots, 0, 100, 0)
	r.Wobble = ditherClampRange(s.Wobble, 0, 64, 0)
	r.Displace = ditherClampRange(s.Displace, -100, 100, 0)
	r.Smoothness = ditherClampRange(s.Smoothness, 0, 100, 50)
	r.Threshold = ditherClampRange(s.Threshold, 0, 100, 0)
	r.Split = math.Round(ditherClampRange(s.Split, 0, 16, 0))
	r.Density = ditherClampRange(s.Density, -100, 100, 0)
	r.Contrast = ditherClampRange(s.Contrast, -100, 100, 0)
	r.BlackLevel = ditherClampRange(s.BlackLevel, 0, 100, 0)
	for i := 0; i < 3; i++ {
		r.Dark[i] = min(1, max(0, s.Dark[i]))
		r.Light[i] = min(1, max(0, s.Light[i]))
	}
	return r
}

// glowSigmaAndShrink is Scanlines.glowing's arithmetic (Scanlines.swift:84):
// the glow is wide and soft, so it is blurred at a fraction of the size and
// scaled back up — "the same light for a small part of the work".
func glowSigmaAndShrink(lineSpacing float64) (sigma, shrink float64) {
	sigma = lineSpacing*3 + 3
	shrink = math.Max(1, math.Floor(sigma/4))
	return sigma, shrink
}

// glowBloom is the phosphor bloom the glow adds back: the raster is shrunk by
// `shrink`, blurred there with sigma/shrink, and grown again
// (Scanlines.swift:85-90) — "the same light for a small part of the work".
//
// One deliberate difference: the original wraps the image in clampedToExtent()
// first, so the blur sees edge-replicated pixels; here the blur samples
// transparent outside the frame, as every other blur in this package does. A
// clamped wide blur on a small fixture averages to the picture's mean and fills
// the dark gaps, which fails the original's own assertion that lines and gaps
// survive a full glow (ScanlinesTests.linesShowOnSolidWhiteAndBlack) — so the
// clamp is left off until the Core Image numerics can be matched exactly.
func glowBloom(b *Bitmap, lineSpacing float64) *Bitmap {
	sigma, shrink := glowSigmaAndShrink(lineSpacing)
	if shrink <= 1 {
		bloom := b.Clone()
		ApplyGaussianBlur(bloom, sigma)
		return bloom
	}
	smallW := int(math.Max(1, math.Round(float64(b.W)/shrink)))
	smallH := int(math.Max(1, math.Round(float64(b.H)/shrink)))
	small := ResampleBitmap(b, smallW, smallH)
	ApplyGaussianBlur(small, sigma/shrink)
	return ResampleBitmap(small, b.W, b.H)
}

// ApplyScanlinesFilter is ScanlinesSettings.apply: the kernel draws the
// lines, then the phosphor glow is blurred across a few line spacings and
// added back over them.
func ApplyScanlinesFilter(b *Bitmap, settings ScanlinesSettings) {
	s := settings.Normalized()
	var dark, light [3]uint8
	if s.Colors == DitherColorsTwo {
		for i := 0; i < 3; i++ {
			dark[i] = uint8(min(255, max(0, s.Dark[i]*255+0.5)))
			light[i] = uint8(min(255, max(0, s.Light[i]*255+0.5)))
		}
	} else {
		dark = [3]uint8{0, 0, 0}
		light = [3]uint8{255, 255, 255}
	}
	params := ScanlinesParams{
		Spacing:        int(s.LineSpacing),
		Thickness:      float32(s.Thickness / 100),
		Dots:           float32(s.Dots / 100),
		Wobble:         float32(s.Wobble),
		Displace:       float32(s.Displace),
		Threshold:      float32(s.Threshold / 100),
		Split:          float32(s.Split),
		Density:        float32(s.Density / 100),
		Contrast:       float32(s.Contrast / 100),
		BlackLevel:     float32(s.BlackLevel / 100),
		Smoothness:     float32(s.Smoothness / 100),
		OriginalColors: s.Colors == DitherColorsOriginal,
		Dark:           dark,
		Light:          light,
	}
	if !ScanlinesApply(b, &params) {
		return
	}
	if s.Glow > 0 {
		DitherGlow(b, glowBloom(b, s.LineSpacing), float32(s.Glow/100*2.5))
	}
}
