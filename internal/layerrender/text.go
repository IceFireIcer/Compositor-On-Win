package layerrender

// Text rasterization (EditorSession.textImage): the content is laid out with
// the style's font, size, tracking, leading and alignment, wrapped to the
// paragraph box when there is one, and drawn into a premultiplied bitmap with
// the style's color. AppKit's NSLayoutManager is approximated by an explicit
// line layout (word wrap, fixed line height, baseline at lineHeight − descent,
// the original's own click formula); the numbers are documented where they
// differ from AppKit.
//
// colorRuns/fontRuns (manifest v10/v11) override single letters: each rune is
// measured and rasterized with its own face and color, and text whose runs
// change nothing keeps the original single-mask rasterization
// (TypeTool.color(at:)/fontName(at:) semantics).

import (
	"image"
	"math"
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

// styledRune is one content rune with the face and color that apply to it: the
// first run containing the rune's UTF-16 offset wins, and runes outside every
// run keep the base values (TypeTool.color(at:)/fontName(at:); runs hold only
// the letters that differ from the base style).
type styledRune struct {
	r     rune
	face  *sfnt.Font
	color [3]float64
}

// TextImage rasterizes the style, returning ok=false when nothing drawable
// exists (invalid style, no font at all, or beyond the document limits).
func TextImage(style domain.TextStyle) (*render.Bitmap, bool) {
	// The content cap counts UTF-16 units, the unit the manifest validator
	// (project/validate.go) and the text runs use — a rune count would let a
	// 100k-emoji string through here and then refuse to save it.
	if domain.TextUTF16Len(style.Content) > 100_000 {
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
	lines := layoutLines(contentLines(style, f), ppem, style.Tracking, maxWidth)

	maxLineWidth := 0.0
	for _, line := range lines {
		if w := measureLine(&buf, line, ppem, style.Tracking); w > maxLineWidth {
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

	color := [3]float64{clamp01(style.Red), clamp01(style.Green), clamp01(style.Blue)}
	if !uniformContent(lines, f, color) {
		// Per-run colors: every glyph goes through its own small mask, since a
		// full-size mask per distinct color would not scale to many runs.
		out := render.NewBitmap(width, height)
		z := &vector.Rasterizer{}
		for i, line := range lines {
			if len(line) == 0 {
				continue
			}
			lineWidth := measureLine(&buf, line, ppem, style.Tracking)
			penX := linePenX(style.Alignment, width, lineWidth)
			baselineY := TextPadding + float64(i+1)*lineHeight - descent
			rasterizeColoredLine(out, z, line, &buf, ppem, style.Tracking, penX, baselineY)
		}
		return out, true
	}

	mask := image.NewAlpha(image.Rect(0, 0, width, height))
	z := &vector.Rasterizer{}
	for i, line := range lines {
		if len(line) == 0 {
			continue
		}
		lineWidth := measureLine(&buf, line, ppem, style.Tracking)
		penX := linePenX(style.Alignment, width, lineWidth)
		baselineY := TextPadding + float64(i+1)*lineHeight - descent
		rasterizeLine(z, mask, line, &buf, ppem, style.Tracking, penX, baselineY)
	}

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

// linePenX is the alignment's starting pen position (PSDText.render's
// alignment branches): the padding corner, centered, or right-aligned.
func linePenX(alignment domain.TextAlignment, width int, lineWidth float64) float64 {
	switch alignment {
	case domain.TextAlignmentCenter:
		return float64(width)/2 - lineWidth/2
	case domain.TextAlignmentRight:
		return float64(width) - TextPadding - lineWidth
	default:
		return TextPadding
	}
}

// uniformContent reports whether every rune still uses the base face and color,
// so the runs change nothing and the original single-mask rasterization
// applies.
func uniformContent(lines [][]styledRune, baseFace *sfnt.Font, baseColor [3]float64) bool {
	for _, line := range lines {
		for _, c := range line {
			if c.face != baseFace || c.color != baseColor {
				return false
			}
		}
	}
	return true
}

// contentLines resolves the content into hard lines of styled runes: each "\n"
// starts a new line and is not itself drawn. Every different run face name is
// resolved once; a name with no installed match keeps Resolve's system-font
// fallback, so a bad run never fails the render.
func contentLines(style domain.TextStyle, baseFace *sfnt.Font) [][]styledRune {
	baseColor := [3]float64{clamp01(style.Red), clamp01(style.Green), clamp01(style.Blue)}
	faces := map[string]*sfnt.Font{style.FontName: baseFace}
	lines := [][]styledRune{{}}
	offset := 0 // UTF-16 offset into the content, the runs' unit
	for _, r := range style.Content {
		if r == '\n' {
			lines = append(lines, nil)
			offset++
			continue
		}
		name := fontNameAt(style.FontRuns, offset, style.FontName)
		lines[len(lines)-1] = append(lines[len(lines)-1], styledRune{
			r:     r,
			face:  resolveFace(name, baseFace, faces),
			color: colorAt(style.ColorRuns, offset, baseColor),
		})
		offset += utf16Len(r)
	}
	return lines
}

// utf16Len is how many UTF-16 units one rune occupies in the content offsets
// the runs use (surrogate pairs count two).
func utf16Len(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}

// runSpans reports whether a run covers the UTF-16 offset. Non-positive
// lengths and spans overflowing int cover nothing; a negative location still
// covers from offset 0 on, as the original's unitColors clamp did.
func runSpans(location, length, offset int) bool {
	if length <= 0 || location > math.MaxInt-length {
		return false
	}
	return offset >= location && offset < location+length
}

// colorAt is the color of the UTF-16 unit at offset (TypeTool.color(at:)).
func colorAt(runs []domain.TextColorRun, offset int, base [3]float64) [3]float64 {
	for _, run := range runs {
		if runSpans(run.Location, run.Length, offset) {
			return [3]float64{clamp01(run.Red), clamp01(run.Green), clamp01(run.Blue)}
		}
	}
	return base
}

// fontNameAt is the face name of the UTF-16 unit at offset
// (TypeTool.fontName(at:)).
func fontNameAt(runs []domain.TextFontRun, offset int, base string) string {
	for _, run := range runs {
		if runSpans(run.Location, run.Length, offset) {
			return run.FontName
		}
	}
	return base
}

// resolveFace looks a run's face up once per name (cache). A name that does not
// resolve keeps Resolve's fallback; the base face resolved before, so a nil
// font is unreachable but still handled.
func resolveFace(name string, baseFace *sfnt.Font, cache map[string]*sfnt.Font) *sfnt.Font {
	if face, ok := cache[name]; ok {
		return face
	}
	face := baseFace
	if fontObj, _ := Resolve(name); fontObj != nil {
		face = fontObj.Sfnt
	}
	cache[name] = face
	return face
}

// layoutLines splits on hard newlines, then wraps each hard line greedily to
// maxWidth (AppKit byWordWrapping). Each rune's advance comes from its own
// face.
func layoutLines(hardLines [][]styledRune, ppem, tracking, maxWidth float64) [][]styledRune {
	if math.IsInf(maxWidth, 1) {
		return hardLines
	}
	var buf sfnt.Buffer
	var out [][]styledRune
	for _, hard := range hardLines {
		out = append(out, wrapHardLine(&buf, hard, ppem, tracking, maxWidth)...)
	}
	return out
}

// wrapHardLine greedily fills lines up to maxWidth, breaking at AppKit
// byWordWrapping's opportunities: after a space, and between a wide
// (CJK/kana/hangul/fullwidth) rune and its neighbor — TextKit breaks CJK text
// per character, which UAX #14 also allows between ideographs. A chunk with no
// opportunity that is wider than the box falls back to a character boundary,
// as byWordWrapping does with a word that does not fit on a line of its own.
//
// The space a line breaks at is not part of either line (the previous
// word-splitting behavior), so alignment measures ink, not trailing blanks.
func wrapHardLine(buf *sfnt.Buffer, line []styledRune, ppem, tracking, maxWidth float64) [][]styledRune {
	if len(line) == 0 {
		return [][]styledRune{{}}
	}
	adv := make([]float64, len(line))
	for i, c := range line {
		adv[i] = glyphAdvance(c.face, buf, c.r, ppem) + tracking
	}
	var out [][]styledRune
	start := 0
	for start < len(line) {
		// How far the next line reaches, and the last break opportunity inside it.
		width := 0.0
		end := start
		cut, next := -1, -1
		for i := start; i < len(line); i++ {
			if i > start && width+adv[i] > maxWidth {
				break
			}
			width += adv[i]
			end = i + 1
			if line[i].r == ' ' {
				cut, next = i, i+1 // break after the space, dropping it
			} else if i+1 < len(line) && (isWideRune(line[i].r) || isWideRune(line[i+1].r)) &&
				!cannotStartLine(line[i+1].r) && !cannotEndLine(line[i].r) {
				cut, next = i+1, i+1
			}
		}
		if end == len(line) {
			out = append(out, line[start:])
			break
		}
		if cut <= start { // no opportunity fit: character fallback
			cut, next = end, end
		}
		out = append(out, line[start:cut])
		start = next
	}
	return out
}

// isWideRune reports whether a rune belongs to a script whose characters each
// form their own break opportunity (CJK ideographs, kana, hangul, fullwidth
// forms) — the ranges UAX #14 gives the ID/HL classes, minus the punctuation
// the two functions below keep off the edges.
func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo initial consonants
		r >= 0x2E80 && r <= 0x303E, // CJK radicals, Kangxi, CJK symbols
		r >= 0x3041 && r <= 0x33FF, // kana, Bopomofo, compatibility
		r >= 0x3400 && r <= 0x4DBF, // CJK extension A
		r >= 0x4E00 && r <= 0x9FFF, // CJK unified ideographs
		r >= 0xA000 && r <= 0xA4CF, // Yi
		r >= 0xAC00 && r <= 0xD7A3, // Hangul syllables
		r >= 0xF900 && r <= 0xFAFF, // CJK compatibility ideographs
		r >= 0xFE30 && r <= 0xFE6F, // CJK compatibility forms
		r >= 0xFF00 && r <= 0xFF60, // fullwidth forms
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x20000 && r <= 0x3FFFD: // CJK extensions B and beyond
		return true
	}
	return false
}

// cannotStartLine reports the CJK closing punctuation and small kana that must
// not begin a line (UAX #14's CL/NS classes: 、。，．：；？！）］｝〉》」』】〙〛 and
// the small kana ぁぃ…ゎヽヾー々). AppKit's byWordWrapping honors the same
// prohibition, so a break opportunity is refused in front of them.
func cannotStartLine(r rune) bool {
	switch r {
	case '、', '。', '，', '．', '：', '；', '？', '！', '’', '”',
		'）', '］', '｝', '〉', '》', '」', '』', '】', '〕', '〗', '〙', '〛',
		'ヽ', 'ヾ', 'ー', '々', '〜', '…', '‥', '・', 'ゝ', 'ゞ', 'ぁ', 'ぃ', 'ぅ', 'ぇ', 'ぉ',
		'っ', 'ゃ', 'ゅ', 'ょ', 'ゎ', 'ァ', 'ィ', 'ゥ', 'ェ', 'ォ', 'ッ', 'ャ', 'ュ', 'ョ', 'ヮ':
		return true
	}
	return false
}

// cannotEndLine reports the CJK opening punctuation that must not end a line
// (UAX #14's OP class). The space rule already keeps ASCII spaces off the
// line's tail.
func cannotEndLine(r rune) bool {
	switch r {
	case '（', '［', '｛', '〈', '《', '「', '『', '【', '〔', '〖', '〘', '〚', '‘', '“':
		return true
	}
	return false
}

// measureLine sums glyph advances plus tracking, each rune's advance coming
// from its own face.
func measureLine(buf *sfnt.Buffer, line []styledRune, ppem, tracking float64) float64 {
	total := 0.0
	for _, c := range line {
		total += glyphAdvance(c.face, buf, c.r, ppem) + tracking
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

// rasterizeLine draws every glyph of one line into the full-canvas coverage
// mask (the uniform face and color path). Baseline placement mirrors the
// original's click formula: the baseline sits lineHeight − descent below the
// top of the line.
func rasterizeLine(z *vector.Rasterizer, mask *image.Alpha, line []styledRune, buf *sfnt.Buffer,
	ppem, tracking, penX, baselineY float64) {
	bounds := mask.Bounds()
	for _, c := range line {
		if c.r == utf8.RuneError {
			continue
		}
		gi, err := c.face.GlyphIndex(buf, c.r)
		adv := glyphAdvance(c.face, buf, c.r, ppem)
		if err == nil && gi != 0 {
			if segs, err := c.face.LoadGlyph(buf, gi, fixed.Int26_6(ppem*64), nil); err == nil && len(segs) > 0 {
				z.Reset(bounds.Dx(), bounds.Dy())
				addGlyphPath(z, segs, penX, baselineY)
				z.ClosePath()
				z.Draw(mask, bounds, image.White, image.Point{})
			}
		}
		penX += adv + tracking
	}
}

// rasterizeColoredLine draws one line glyph by glyph, each glyph through its
// own small mask so its run color can be premultiplied in; a full-size mask per
// distinct color would not scale to the run count.
func rasterizeColoredLine(dst *render.Bitmap, z *vector.Rasterizer, line []styledRune, buf *sfnt.Buffer,
	ppem, tracking, penX, baselineY float64) {
	for _, c := range line {
		if c.r == utf8.RuneError {
			continue
		}
		gi, err := c.face.GlyphIndex(buf, c.r)
		adv := glyphAdvance(c.face, buf, c.r, ppem)
		if err == nil && gi != 0 {
			if segs, err := c.face.LoadGlyph(buf, gi, fixed.Int26_6(ppem*64), nil); err == nil && len(segs) > 0 {
				drawGlyph(dst, z, segs, c.color, penX, baselineY)
			}
		}
		penX += adv + tracking
	}
}

// drawGlyph rasterizes one glyph outline into a mask sized from the glyph's own
// bounding box (plus 2 pixels of slack for antialiasing, clamped to the
// bitmap) and blends it into dst with the glyph's color.
func drawGlyph(dst *render.Bitmap, z *vector.Rasterizer, segs sfnt.Segments, color [3]float64,
	penX, baselineY float64) {
	bounds := segs.Bounds()
	x0 := math.Max(0, math.Floor(float64(bounds.Min.X)/64+penX)-2)
	y0 := math.Max(0, math.Floor(float64(bounds.Min.Y)/64+baselineY)-2)
	x1 := math.Min(float64(dst.W), math.Ceil(float64(bounds.Max.X)/64+penX)+2)
	y1 := math.Min(float64(dst.H), math.Ceil(float64(bounds.Max.Y)/64+baselineY)+2)
	if !(x0 < x1 && y0 < y1) { // empty, entirely outside, or non-finite
		return
	}
	w, h := int(x1-x0), int(y1-y0)
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	z.Reset(w, h)
	addGlyphPath(z, segs, penX-x0, baselineY-y0)
	z.ClosePath()
	z.Draw(mask, mask.Bounds(), image.White, image.Point{})
	for y := 0; y < h; y++ {
		row := y * mask.Stride
		base := ((int(y0)+y)*dst.W + int(x0)) * 4
		for x := 0; x < w; x++ {
			a := uint32(mask.Pix[row+x])
			if a == 0 {
				continue
			}
			premulBlit(dst.Pix[base+x*4:base+x*4+4], color, a)
		}
	}
}

// premulBlit composites one glyph pixel (color premultiplied by its 8-bit
// coverage) over the destination, both premultiplied RGBA8. Overlapping glyphs
// keep the same over composition the single-mask path gets from the rasterizer.
func premulBlit(pix []uint8, color [3]float64, a uint32) {
	inv := 255 - a
	pix[0] = uint8(uint32(uint8(color[0]*float64(a))) + uint32(pix[0])*inv/255)
	pix[1] = uint8(uint32(uint8(color[1]*float64(a))) + uint32(pix[1])*inv/255)
	pix[2] = uint8(uint32(uint8(color[2]*float64(a))) + uint32(pix[2])*inv/255)
	pix[3] = uint8(uint32(a) + uint32(pix[3])*inv/255)
}

// addGlyphPath appends one glyph's segments to the rasterizer, translating the
// 26.6 pixel coordinates (Y down, matching the bitmap, relative to the glyph
// origin on the baseline) by the given pen offsets.
func addGlyphPath(z *vector.Rasterizer, segs sfnt.Segments, offsetX, offsetY float64) {
	for _, seg := range segs {
		px := func(p fixed.Point26_6) (float32, float32) {
			return float32(offsetX + float64(p.X)/64), float32(offsetY + float64(p.Y)/64)
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

// TextBaselineInset is where the first baseline sits inside a text image:
// TextPadding + lineHeight − descent (EditorSession.beginText's formula,
// which places a click's baseline on the pointer). Negative descent values
// (signed metrics) clamp to the same fallback the rasterizer uses.
func TextBaselineInset(style domain.TextStyle) float64 {
	lineHeight := style.Leading
	if !(lineHeight > 0) {
		lineHeight = style.FontSize * 1.2
	}
	descent := style.FontSize * 0.25
	if fontObj, _ := Resolve(style.FontName); fontObj != nil {
		var buf sfnt.Buffer
		if metrics, err := fontObj.Sfnt.Metrics(&buf, fixed.Int26_6(style.FontSize*64), font.HintingNone); err == nil {
			if d := float64(metrics.Descent) / 64; d > 0 && !math.IsNaN(d) {
				descent = d
			}
		}
	}
	return TextPadding + lineHeight - descent
}
