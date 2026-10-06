package render

// DitherSettings — the filter-form model (Dither.swift) and the Swift-side
// wrapper around the C port: chunky-pixel downscale → dither → nearest
// upscale → dot shape → scanline glow. The glyph (ASCII) style rasterizes
// its characters from golang.org/x/image's embedded 7×13 bitmap font —
// deterministic everywhere, unlike the macOS system font the original
// draws with; the ink-sorted cell semantics match.

import (
	"image"
	"image/draw"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// DitherColors mode (DitherColors.swift).
const (
	DitherColorsBlackWhite = 0
	DitherColorsTwo        = 1
	DitherColorsOriginal   = 2
)

type DitherSettings struct {
	Style       int
	PixelSize   float64
	PixelShape  int // 0 square, 1 dot
	CellSize    float64
	TextSize    float64
	LineSpacing float64
	Glow        float64
	Dots        float64
	Wobble      float64
	Angle       float64
	Levels      float64
	Diffusion   float64
	Density     float64
	Contrast    float64
	Colors      int
	Dark        [3]float64 // 0–1 straight sRGB
	Light       [3]float64
	LightOnDark bool
	Characters  string
}

func ditherClampRange(v, lo, hi, fb float64) float64 {
	if v != v || v > 1e308 || v < -1e308 {
		return fb
	}
	return min(hi, max(lo, v))
}

// Normalized clamps with the Dither.swift ranges and defaults.
func (s DitherSettings) Normalized() DitherSettings {
	r := s
	r.PixelSize = float64(int(ditherClampRange(s.PixelSize, 1, 32, 2)))
	r.CellSize = float64(int(ditherClampRange(s.CellSize, 4, 64, 8)))
	r.TextSize = float64(int(ditherClampRange(s.TextSize, 6, 64, 14)))
	r.LineSpacing = float64(int(ditherClampRange(s.LineSpacing, 2, 32, 4)))
	r.Glow = ditherClampRange(s.Glow, 0, 100, 35)
	r.Dots = ditherClampRange(s.Dots, 0, 100, 0)
	r.Wobble = ditherClampRange(s.Wobble, 0, 64, 0)
	r.Angle = ditherClampRange(s.Angle, -90, 90, 45)
	r.Levels = float64(int(ditherClampRange(s.Levels, 2, 8, 2)))
	r.Diffusion = ditherClampRange(s.Diffusion, 0, 100, 100)
	r.Density = ditherClampRange(s.Density, -100, 100, 0)
	r.Contrast = ditherClampRange(s.Contrast, -100, 100, 0)
	for i := 0; i < 3; i++ {
		r.Dark[i] = min(1, max(0, s.Dark[i]))
		r.Light[i] = min(1, max(0, s.Light[i]))
	}
	if len(r.Characters) > 64 {
		r.Characters = r.Characters[:64]
	}
	return r
}

// DownscaleBitmap box-averages src into a w×h grid (the preview source;
// deterministic, like BrushRaster.draw's interpolated downscale).
func DownscaleBitmap(src *Bitmap, w, h int) *Bitmap {
	if w >= src.W && h >= src.H {
		return src.Clone()
	}
	out := NewBitmap(w, h)
	for y := 0; y < h; y++ {
		y0, y1 := y*src.H/h, (y+1)*src.H/h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < w; x++ {
			x0, x1 := x*src.W/w, (x+1)*src.W/w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var acc [4]float64
			n := 0.0
			for sy := y0; sy < y1 && sy < src.H; sy++ {
				for sx := x0; sx < x1 && sx < src.W; sx++ {
					i := (sy*src.W + sx) * 4
					for c := 0; c < 4; c++ {
						acc[c] += float64(src.Pix[i+c])
					}
					n++
				}
			}
			o := (y*w + x) * 4
			for c := 0; c < 4; c++ {
				out.Pix[o+c] = uint8(math.Round(acc[c] / n))
			}
		}
	}
	return out
}

// UpscaleNearest replicates each pixel block×block — the chunky-pixel blow
// back up (CoreGraphics draw with .copy semantics at nearest).
func UpscaleNearest(src *Bitmap, block int) *Bitmap {
	if block <= 1 {
		return src.Clone()
	}
	out := NewBitmap(src.W*block, src.H*block)
	for y := 0; y < out.H; y++ {
		for x := 0; x < out.W; x++ {
			si := ((y / block) * src.W * 4) + ((x / block) * 4)
			oi := (y*out.W + x) * 4
			copy(out.Pix[oi:oi+4], src.Pix[si:si+4])
		}
	}
	return out
}

// buildDitherStyleGlyphs rasterizes the distinct characters into coverage maps
// (255 = fully inked), least inked first — DitherSettings.glyphs.
func buildDitherStyleGlyphs(characters string, lineHeight int) (glyphs []uint8, coverage []float32, gw, gh int) {
	face := basicfont.Face7x13
	gw, gh = 8, 13
	// Integer scale so bigger cells keep crisp glyphs.
	scale := max(1, lineHeight/13)
	gw *= scale
	gh *= scale
	seen := map[rune]bool{}
	type entry struct {
		mapData []uint8
		cov     float32
	}
	var drawn []entry
	for _, ch := range characters {
		if ch == '\n' || ch == '\r' || seen[ch] {
			continue
		}
		seen[ch] = true
		if len(drawn) >= 64 {
			break
		}
		mapData := make([]uint8, gw*gh)
		dst := image.NewRGBA(image.Rect(0, 0, gw, gh))
		draw.Draw(dst, dst.Bounds(), image.Black, image.Point{}, draw.Src)
		d := font.Drawer{
			Dst:  dst,
			Src:  image.White,
			Face: face,
			Dot:  fixed.P(0, 12),
		}
		d.DrawString(string(ch))
		for y := 0; y < gh; y++ {
			for x := 0; x < gw; x++ {
				// Sample the scaled source cell; the 7×13 glyph lives in the
				// top-left with the face's own bearing.
				sx := min(6, x/scale)
				sy := min(12, y/scale)
				r, g, b, _ := dst.At(sx, sy).RGBA()
				v := uint8(0)
				if r+g+b > 0 {
					v = 255
				}
				mapData[y*gw+x] = v
			}
		}
		sum := 0
		for _, v := range mapData {
			sum += int(v)
		}
		drawn = append(drawn, entry{mapData, float32(sum) / float32(255*gw*gh)})
	}
	// Least ink first (insertion sort keeps ties in character order).
	for i := 1; i < len(drawn); i++ {
		for j := i; j > 0 && drawn[j].cov < drawn[j-1].cov; j-- {
			drawn[j], drawn[j-1] = drawn[j-1], drawn[j]
		}
	}
	for _, e := range drawn {
		glyphs = append(glyphs, e.mapData...)
		coverage = append(coverage, e.cov)
	}
	return glyphs, coverage, gw, gh
}

// ApplyDitherFilter is DitherSettings.apply: chunky pixels downscale →
// dither → nearest upscale (with the dot shape's gaps), plus the scanlines'
// phosphor glow (the CI gaussian bloom approximated with the in-house
// Gaussian). Scanlines and ASCII draw at full resolution.
func ApplyDitherFilter(b *Bitmap, settings DitherSettings) {
	s := settings.Normalized()
	usesPixelSize := s.Style != DitherStyleGlyphs && s.Style != DitherStyleScanlines
	block := 1
	if usesPixelSize {
		block = int(s.PixelSize)
	}
	working := b
	if block > 1 {
		working = DownscaleBitmap(b, (b.W+block-1)/block, (b.H+block-1)/block)
	}
	cell := int(s.CellSize)
	if s.Style == DitherStyleScanlines {
		cell = int(s.LineSpacing)
	}
	var glyphs []uint8
	var coverage []float32
	gw, gh := 1, 1
	if s.Style == DitherStyleGlyphs {
		chars := s.Characters
		if chars == "" {
			chars = " .:-=+*#%@"
		}
		glyphs, coverage, gw, gh = buildDitherStyleGlyphs(chars, int(s.TextSize))
	}
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
	params := DitherParams{
		Style:          s.Style,
		Levels:         int(s.Levels),
		Diffusion:      s.Diffusion / 100,
		Density:        s.Density / 100,
		Contrast:       s.Contrast / 100,
		Cell:           cell,
		Angle:          s.Angle * mathPiOver180,
		LightOnDark:    s.LightOnDark,
		OriginalColors: s.Colors == DitherColorsOriginal,
		Dark:           dark,
		Light:          light,
		GlyphWidth:     gw,
		GlyphHeight:    gh,
		Glyphs:         glyphs,
		GlyphCoverage:  coverage,
		GlyphCount:     len(coverage),
		Dots:           s.Dots / 100,
		Wobble:         s.Wobble,
	}
	if !DitherApply(working, &params) {
		return
	}
	result := working
	if s.Style == DitherStyleScanlines && s.Glow > 0 {
		// The lines' light, blurred across a few spacings and added back —
		// the CRT phosphor bloom.
		sigma := s.LineSpacing*3 + 3
		bloom := working.Clone()
		ApplyGaussianBlur(bloom, sigma)
		DitherGlow(working, bloom, float32(s.Glow/100*2.5))
	}
	if block > 1 {
		result = UpscaleNearest(working, block)
		if s.PixelShape == 1 {
			gap := [3]uint8{0, 0, 0}
			if s.Colors == DitherColorsTwo {
				gap = dark
			}
			DitherDots(result, block, gap)
		}
	}
	if result != b {
		*b = *result
	}
}

const mathPiOver180 = 0.017453292519943295
