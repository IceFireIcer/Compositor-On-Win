package psd

// Text layer parsing (PSDText, ticket 39): a Photoshop 6 type layer
// (`TySh`) becomes the editor's text model — uniform scale/rotation/flip
// from the 2×3 transform, engine-dictionary style (font, size, color,
// tracking, leading, alignment) and the anchor. Anything the model cannot
// represent (vertical text, shear, uneven scale) returns nil and the layer
// stays the raster Photoshop stores for it.

import (
	"math"
	"strings"

	"compositor-win/internal/domain"
)

const textPadding = 12 // LayerTextStyle.padding: the gap text keeps from its box

// ParsedText is a successfully parsed type layer (PSDText.Source).
type ParsedText struct {
	Style         domain.TextStyle
	Notes         []string
	Anchor        [2]float64 // document point the image anchor lands on
	Rotation      float64
	FlipY         bool
	AnchorIsFrame bool // anchor is the paragraph frame's top-left, else the point-text baseline
}

// parseText reads the TySh additional info; nil keeps the layer raster.
func parseText(extra map[string][]byte) *ParsedText {
	data := extra["TySh"]
	if data == nil {
		data = extra["tySh"]
	}
	if data == nil || len(data) > 8_000_000 {
		return nil
	}
	r := &tdReader{data: data}
	if r.u16() != 1 {
		return nil
	}
	var m [6]float64
	for i := 0; i < 6; i++ {
		m[i] = r.f64()
	}
	finite := true
	for _, v := range m {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			finite = false
		}
	}
	if !finite {
		return nil
	}
	if r.u16() != 50 {
		return nil
	}
	text := r.descriptor()
	if text == nil {
		return nil
	}
	if text.enum("Ornt") == "Vrtc" {
		return nil // vertical text stays raster, exactly as the original
	}
	placed := placement(m)
	if placed == nil {
		return nil
	}

	var notes []string
	if r.remaining() >= 2 && r.u16() == 1 {
		if warp := r.descriptor(); warp != nil {
			if style := warp.enum("warpStyle"); style != "" && style != "warpNone" && style != "none" {
				notes = append(notes, "The Photoshop text warp was omitted.")
			}
		}
	}

	engine := engineValueData(text.data("EngineData"))
	content := cleanedText(firstString(text, "Txt ", "Txt"))
	if content == nil && engine != nil {
		if raw := engineString(walk(engine, "EngineDict", "Editor", "Text")); raw != "" {
			content = cleanedText(&raw)
		}
	}
	if content == nil || *content == "" || len([]rune(*content)) > 100_000 {
		return nil
	}

	style := domain.TextStyle{
		Content:  *content,
		FontName: "Helvetica",
		FontSize: 72,
	}
	if engine != nil {
		applyTextStyle(&style, engine, placed.pixelScale, &notes)
	} else {
		style.FontSize = math.Min(2000, math.Max(1, 12*placed.pixelScale))
	}
	if math.IsNaN(style.FontSize) || math.IsInf(style.FontSize, 0) || style.FontSize <= 0 {
		return nil
	}

	anchor := [2]float64{placed.tx, placed.ty}
	anchorIsFrame := false
	if bounds := text.rect("bounds"); bounds != nil {
		glyphs := text.rect("boundingBox")
		if glyphs != nil &&
			bounds.width > glyphs.width+4 && bounds.height > glyphs.height+4 &&
			bounds.width > 1 && bounds.height > 1 {
			frameW := bounds.width * placed.pixelScale
			frameH := bounds.height * placed.pixelScale
			boxed := style
			boxW, boxH := frameW+textPadding*2, frameH+textPadding*2
			boxed.BoxSize = &[2]float64{boxW, boxH}
			if !textStyleValid(boxed) {
				// A paragraph frame the model cannot store is dropped entirely:
				// importing as point text would lose the wrap without saying so.
				return nil
			}
			style = boxed
			anchor = placed.mapPoint(bounds.x, bounds.y)
			anchorIsFrame = true
		}
	}
	if !textStyleValid(style) {
		return nil
	}
	return &ParsedText{
		Style:         style,
		Notes:         notes,
		Anchor:        anchor,
		Rotation:      placed.rotation,
		FlipY:         placed.flipY,
		AnchorIsFrame: anchorIsFrame,
	}
}

// textStyleValid is LayerTextStyle.isValid for the fields we import.
func textStyleValid(s domain.TextStyle) bool {
	if len([]rune(s.Content)) > 100_000 {
		return false
	}
	if s.BoxSize != nil {
		w, h := s.BoxSize[0], s.BoxSize[1]
		if math.IsNaN(w) || math.IsNaN(h) || math.IsInf(w, 0) || math.IsInf(h, 0) {
			return false
		}
		if !(16 <= w && w <= 30000 && 16 <= h && h <= 30000) {
			return false
		}
		if w*h > 200_000_000 {
			return false
		}
	}
	if math.IsNaN(s.FontSize) || math.IsInf(s.FontSize, 0) || !(1 <= s.FontSize && s.FontSize <= 2000) {
		return false
	}
	for _, c := range [3]float64{s.Red, s.Green, s.Blue} {
		if math.IsNaN(c) || math.IsInf(c, 0) || !(0 <= c && c <= 1) {
			return false
		}
	}
	if math.IsNaN(s.Tracking) || math.IsInf(s.Tracking, 0) || !(-100 <= s.Tracking && s.Tracking <= 1000) {
		return false
	}
	if math.IsNaN(s.Leading) || math.IsInf(s.Leading, 0) || !(0 <= s.Leading && s.Leading <= 5000) {
		return false
	}
	return true
}

type textPlacement struct {
	pixelScale float64
	rotation   float64
	flipY      bool
	tx, ty     float64
	// exx…eyy map local text-space points into document pixels.
	exx, eyx, exy, eyy float64
}

func (p *textPlacement) mapPoint(x, y float64) [2]float64 {
	return [2]float64{p.exx*x + p.exy*y + p.tx, p.eyx*x + p.eyy*y + p.ty}
}

// placement extracts uniform scale, rotation and an optional vertical flip
// from the TySh 2×3 matrix. Shear and uneven scale return nil.
func placement(m [6]float64) *textPlacement {
	xx, xy, yx, yy, tx, ty := m[0], m[1], m[2], m[3], m[4], m[5]
	scaleX := math.Hypot(xx, yx)
	if scaleX <= 1e-6 {
		return nil
	}
	cosR := xx / scaleX
	sinR := yx / scaleX
	localX := cosR*xy + sinR*yy
	localY := -sinR*xy + cosR*yy
	scaleY := math.Abs(localY)
	if scaleY <= 1e-6 {
		return nil
	}
	largest := math.Max(scaleX, scaleY)
	if math.Abs(localX) > 0.02*largest || math.Abs(scaleX-scaleY) > 0.02*largest {
		return nil
	}
	pixelScale := scaleX
	if math.IsNaN(pixelScale) || math.IsInf(pixelScale, 0) || pixelScale <= 0 {
		return nil
	}
	ySign := 1.0
	if localY < 0 {
		ySign = -1
	}
	return &textPlacement{
		pixelScale: pixelScale,
		rotation:   math.Atan2(sinR, cosR) * 180 / math.Pi,
		flipY:      localY < 0,
		tx:         tx,
		ty:         ty,
		exx:        cosR * pixelScale,
		eyx:        sinR * pixelScale,
		exy:        -sinR * pixelScale * ySign,
		eyy:        cosR * pixelScale * ySign,
	}
}

func applyTextStyle(style *domain.TextStyle, engine *engineDict, pixelScale float64, notes *[]string) {
	runs := engineArray(walk(engine, "EngineDict", "StyleRun", "RunArray"))
	var first *engineDict
	if len(runs) > 0 {
		if d, ok := runs[0].(*engineDict); ok {
			first = d
		}
	}
	var data *engineDict
	if first != nil {
		data = first
	}
	if sd, ok := walk(first, "StyleSheet", "StyleSheetData").(*engineDict); ok {
		data = sd
	}
	points := 12.0
	if n := engineNumber(walk(data, "FontSize")); n != nil && *n > 0 && !math.IsInf(*n, 0) {
		points = *n
	}
	style.FontSize = math.Min(2000, math.Max(1, points*pixelScale))

	fonts := engineArray(walk(engine, "ResourceDict", "FontSet"))
	fontIndex := 0
	if n := engineNumber(walk(data, "Font")); n != nil {
		fontIndex = int(math.Round(*n))
	}
	if fontIndex >= 0 && fontIndex < len(fonts) {
		if fd, ok := fonts[fontIndex].(*engineDict); ok {
			if name := engineString(walk(fd, "Name")); name != "" {
				style.FontName = name
			}
		}
	}
	if values := engineArray(walk(data, "FillColor", "Values")); len(values) > 0 {
		var channels []float64
		for _, v := range values {
			if n := engineNumber(v); n != nil {
				channels = append(channels, *n)
			}
		}
		r, g, b := engineColor(channels)
		style.Red, style.Green, style.Blue = r, g, b
	}
	if tracking := engineNumber(walk(data, "Tracking")); tracking != nil && !math.IsInf(*tracking, 0) {
		style.Tracking = math.Min(1000, math.Max(-100, *tracking*style.FontSize/1000))
	}
	auto := true
	if b := engineBool(walk(data, "AutoLeading")); b != nil {
		auto = *b
	}
	if !auto {
		if leading := engineNumber(walk(data, "Leading")); leading != nil && !math.IsInf(*leading, 0) && *leading > 0 {
			style.Leading = math.Min(5000, math.Max(0, *leading*pixelScale))
		}
	}
	if b := engineBool(walk(data, "FauxBold")); b != nil && *b {
		*notes = append(*notes, "Faux bold or faux italic was omitted.")
	} else if b := engineBool(walk(data, "FauxItalic")); b != nil && *b {
		*notes = append(*notes, "Faux bold or faux italic was omitted.")
	}
	if len(runs) > 1 {
		firstSig := runSignature(runs[0])
		for _, other := range runs[1:] {
			if runSignature(other) != firstSig {
				*notes = append(*notes, "Only the first text style was kept.")
				break
			}
		}
	}
	paragraphs := engineArray(walk(engine, "EngineDict", "ParagraphRun", "RunArray"))
	paragraph := paragraphs
	if len(paragraphs) > 0 {
		paragraph = paragraphs[:1]
	}
	justification := 0.0
	if len(paragraph) == 1 {
		if pd, ok := paragraph[0].(*engineDict); ok {
			if n := engineNumber(walk(pd, "ParagraphSheet", "Properties", "Justification")); n != nil {
				justification = *n
			}
		}
	}
	switch int(math.Round(justification)) {
	case 1:
		style.Alignment = domain.TextAlignmentRight
	case 2:
		style.Alignment = domain.TextAlignmentCenter
	case 0:
		style.Alignment = domain.TextAlignmentLeft
	default:
		style.Alignment = domain.TextAlignmentLeft
		*notes = append(*notes, "Full justification was imported as left alignment.")
	}
}

// runSignature is PSDText.Signature: the style fields that decide whether
// runs differ enough to warrant the first-style note.
func runSignature(run engineValue) engineRunSignature {
	var sig engineRunSignature
	d, ok := run.(*engineDict)
	if !ok {
		return sig
	}
	if sd, ok := walk(d, "StyleSheet", "StyleSheetData").(*engineDict); ok {
		d = sd
	}
	if n := engineNumber(walk(d, "Font")); n != nil {
		sig.font = *n
	}
	if n := engineNumber(walk(d, "FontSize")); n != nil {
		sig.size = *n
	}
	if n := engineNumber(walk(d, "Tracking")); n != nil {
		sig.tracking = *n
	}
	if b := engineBool(walk(d, "AutoLeading")); b != nil {
		sig.autoLeading = *b
	}
	if n := engineNumber(walk(d, "Leading")); n != nil {
		sig.leading = *n
	}
	if n := engineNumber(walk(d, "HorizontalScale")); n != nil {
		sig.horizontalScale = *n
	}
	if n := engineNumber(walk(d, "VerticalScale")); n != nil {
		sig.verticalScale = *n
	}
	if b := engineBool(walk(d, "FauxBold")); b != nil {
		sig.bold = *b
	}
	if b := engineBool(walk(d, "FauxItalic")); b != nil {
		sig.italic = *b
	}
	var channels []float64
	for _, v := range engineArray(walk(d, "FillColor", "Values")) {
		if n := engineNumber(v); n != nil {
			channels = append(channels, *n)
		}
	}
	r, g, b := engineColor(channels)
	sig.red, sig.green, sig.blue = r, g, b
	return sig
}

type engineRunSignature struct {
	font, size, tracking, leading  float64
	autoLeading                    bool
	horizontalScale, verticalScale float64
	bold, italic                   bool
	red, green, blue               float64
}

func engineColor(values []float64) (r, g, b float64) {
	unit := func(v float64) float64 {
		if v > 1 {
			return math.Min(255, math.Max(0, v)) / 255
		}
		return math.Min(1, math.Max(0, v))
	}
	switch {
	case len(values) >= 4:
		return unit(values[1]), unit(values[2]), unit(values[3])
	case len(values) == 3:
		return unit(values[0]), unit(values[1]), unit(values[2])
	case len(values) == 1:
		return unit(values[0]), unit(values[0]), unit(values[0])
	}
	return 0, 0, 0
}

func cleanedText(s *string) *string {
	if s == nil {
		return nil
	}
	text := *s
	for len(text) > 0 {
		r := []rune(text)[0]
		if r == 0xFEFF || r == 0 {
			text = string([]rune(text)[1:])
		} else {
			break
		}
	}
	for strings.HasSuffix(text, "\x00") {
		text = strings.TrimSuffix(text, "\x00")
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return &text
}

// firstString returns the first key present as a string value.
func firstString(d textDescriptor, keys ...string) *string {
	for _, key := range keys {
		if s := d.str(key); s != nil {
			return s
		}
	}
	return nil
}
