package layerrender

// Text rasterization (EditorSession.textImage): the content is laid out with
// the style's font, size, tracking, leading and alignment, wrapped to the
// paragraph box when there is one, and drawn into a premultiplied bitmap
// with the style's colour. AppKit's NSLayoutManager is approximated by an
// explicit line layout (word wrap, fixed line height, baseline at
// lineHeight − descent, the original's own click formula); the numbers are
// documented where they differ from AppKit.

import (
	"image"
	"image/draw"
	"math"
	"strings"
	"unicode/utf8"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// TextPadding is LayerTextStyle.padding: the gap between text and its box.
const TextPadding = 12

// FontInstalled reports whether name resolves to a face on this machine
// (PSDText.missingFontNote); the caller reports the system-font fallback.
func FontInstalled(name string) bool {
	_, exact := Resolve(name)
	return exact
}

// TextImage rasterizes the style, returning ok=false when nothing drawable
// exists (invalid style, no font at all, or beyond the document limits).
func TextImage(style domain.TextStyle) (*render.Bitmap, bool) {
	if len([]rune(style.Content)) > 100_000 {
		return nil, false
	}
	fontObj, _ := Resolve(style.FontName)
	if fontObj == nil {
		return nil, false
	}
	f := fontObj.Sfnt
	var buf sfnt.Buffer
	ppem := style.FontSize
	if !(ppem >= 1 && ppem <= 2000) || math.IsNaN(ppem) || math.IsInf(ppem, 0) {
		return nil, false
	}
	lineHeight := style.Leading
	if !(lineHeight > 0) {
		lineHeight = ppem * 1.2 // auto leading
	}
	if math.IsNaN(lineHeight) || math.IsInf(lineHeight, 0) {
		return nil, false
	}
	descent := ppem * 0.25
	if metrics, err := f.Metrics(&buf, fixed.Int26_6(ppem*64), font.HintingNone); err == nil {
		if d := float64(metrics.Descent) / 64; d > 0 && !math.IsNaN(d) {
			descent = d
		}
	}

	// Wrap: hard newlines first, then greedy word wrap inside the box.
	maxWidth := math.Inf(1)
	if style.BoxSize != nil {
		maxWidth = style.BoxSize[0] - 2*TextPadding
		if maxWidth < 1 {
			maxWidth = 1
		}
	}
	lines := layoutLines(f, &buf, style, ppem, maxWidth)

	maxLineWidth := 0.0
	for _, line := range lines {
		if w := measureLine(f, &buf, line, ppem, style.Tracking); w > maxLineWidth {
			maxLineWidth = w
		}
	}
	measuredHeight := float64(len(lines)) * lineHeight

	var width, height int
	if style.BoxSize != nil {
		width = int(math.Ceil(style.BoxSize[0]))
		height = int(math.Ceil(style.BoxSize[1]))
	} else {
		w := math.Max(16, math.Ceil(maxLineWidth+2*TextPadding+ppem*0.1))
		h := math.Max(16, math.Ceil(math.Max(measuredHeight, math.Ceil(lineHeight))+2*TextPadding))
		width, height = int(w), int(h)
	}
	if width < 1 || height < 1 || width > 30000 || height > 30000 || width*height > 200_000_000 {
		return nil, false
	}

	mask := image.NewAlpha(image.Rect(0, 0, width, height))
	z := &vector.Rasterizer{}
	for i, line := range lines {
		if line == "" {
			continue
		}
		lineWidth := measureLine(f, &buf, line, ppem, style.Tracking)
		var penX float64
		switch style.Alignment {
		case domain.TextAlignmentCenter:
			penX = float64(width)/2 - lineWidth/2
		case domain.TextAlignmentRight:
			penX = float64(width) - TextPadding - lineWidth
		default:
			penX = TextPadding
		}
		baselineY := TextPadding + float64(i+1)*lineHeight - descent
		rasterizeLine(z, mask, f, &buf, line, ppem, style.Tracking, penX, baselineY)
	}

	color := [3]float64{clamp01(style.Red), clamp01(style.Green), clamp01(style.Blue)}
	out := render.NewBitmap(width, height)
	for i := 0; i < width*height; i++ {
		a := uint32(mask.Pix[i])
		if a == 0 {
			continue
		}
		out.Pix[i*4] = uint8(color[0] * float64(a))
		out.Pix[i*4+1] = uint8(color[1] * float64(a))
		out.Pix[i*4+2] = uint8(color[2] * float64(a))
		out.Pix[i*4+3] = uint8(a)
	}
	return out, true
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(0, math.Min(1, v))
}

// layoutLines splits on hard newlines, then wraps each hard line greedily
// at spaces (AppKit byWordWrapping).
func layoutLines(f *sfnt.Font, buf *sfnt.Buffer, style domain.TextStyle, ppem, maxWidth float64) []string {
	var out []string
	for _, hard := range strings.Split(style.Content, "\n") {
		if math.IsInf(maxWidth, 1) {
			out = append(out, hard)
			continue
		}
		words := strings.Split(hard, " ")
		current := ""
		for _, word := range words {
			candidate := word
			if current != "" {
				candidate = current + " " + word
			}
			if current != "" && measureLine(f, buf, candidate, ppem, style.Tracking) > maxWidth {
				out = append(out, current)
				current = word
			} else {
				current = candidate
			}
		}
		out = append(out, current)
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
}

// measureLine sums glyph advances plus tracking.
func measureLine(f *sfnt.Font, buf *sfnt.Buffer, text string, ppem, tracking float64) float64 {
	total := 0.0
	for _, r := range text {
		total += glyphAdvance(f, buf, r, ppem) + tracking
	}
	return total
}

func glyphAdvance(f *sfnt.Font, buf *sfnt.Buffer, r rune, ppem float64) float64 {
	gi, err := f.GlyphIndex(buf, r)
	if err != nil {
		return 0
	}
	adv, err := f.GlyphAdvance(buf, gi, fixed.Int26_6(ppem*64), font.HintingNone)
	if err != nil {
		return 0
	}
	return float64(adv) / 64
}

// rasterizeLine draws every glyph of one line into the coverage mask.
// Baseline placement mirrors the original's click formula: the baseline
// sits lineHeight − descent below the top of the line.
func rasterizeLine(z *vector.Rasterizer, mask *image.Alpha, f *sfnt.Font, buf *sfnt.Buffer,
	text string, ppem, tracking, penX, baselineY float64) {
	bounds := mask.Bounds()
	for _, r := range text {
		if r == utf8.RuneError {
			continue
		}
		gi, err := f.GlyphIndex(buf, r)
		adv := glyphAdvance(f, buf, r, ppem)
		if err == nil && gi != 0 {
			if segs, err := f.LoadGlyph(buf, gi, fixed.Int26_6(ppem*64), nil); err == nil && len(segs) > 0 {
				z.Reset(bounds.Dx(), bounds.Dy())
				for _, seg := range segs {
					// sfnt segment coordinates are 26.6 pixels with Y down
					// (matching the bitmap), relative to the glyph origin on
					// the baseline.
					px := func(p fixed.Point26_6) (float32, float32) {
						return float32(penX + float64(p.X)/64), float32(baselineY + float64(p.Y)/64)
					}
					switch seg.Op {
					case sfnt.SegmentOpMoveTo:
						x, y := px(seg.Args[0])
						z.MoveTo(x, y)
					case sfnt.SegmentOpLineTo:
						x, y := px(seg.Args[0])
						z.LineTo(x, y)
					case sfnt.SegmentOpQuadTo:
						x1, y1 := px(seg.Args[0])
						x2, y2 := px(seg.Args[1])
						z.QuadTo(x1, y1, x2, y2)
					case sfnt.SegmentOpCubeTo:
						x1, y1 := px(seg.Args[0])
						x2, y2 := px(seg.Args[1])
						x3, y3 := px(seg.Args[2])
						z.CubeTo(x1, y1, x2, y2, x3, y3)
					}
				}
				z.ClosePath()
				z.Draw(mask, bounds, image.White, image.Point{})
			}
		}
		penX += adv + tracking
	}
	_ = draw.Over
}

// TextAnchor is the point inside the text image that the document anchor
// lands on (PSDText.render's imageAnchor): the padding corner for a fixed
// box, and otherwise the horizontal anchor for the alignment plus the first
// baseline — TextKit's glyph location reads as the ascent, the original's
// baseline() role.
func TextAnchor(style domain.TextStyle, imageWidth float64) (float64, float64) {
	ascent := style.FontSize * 0.8
	if fontObj, _ := Resolve(style.FontName); fontObj != nil {
		var buf sfnt.Buffer
		if metrics, err := fontObj.Sfnt.Metrics(&buf, fixed.Int26_6(style.FontSize*64), font.HintingNone); err == nil {
			if a := float64(metrics.Ascent) / 64; a > 0 && !math.IsNaN(a) {
				ascent = a
			}
		}
	}
	x := float64(TextPadding)
	switch style.Alignment {
	case domain.TextAlignmentCenter:
		x = imageWidth / 2
	case domain.TextAlignmentRight:
		x = imageWidth - TextPadding
	}
	return x, TextPadding + ascent
}
