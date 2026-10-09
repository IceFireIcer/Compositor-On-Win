package psd

// The record-level codec of the text round trip (ticket 46) and its
// equivalence validator: encodeTySh turns a domain.TextStyle into the
// Photoshop 6 type-layer block (`TySh`) the parser in text.go reads back,
// in the shape Adobe's file format documents (Type Tool Object Setting:
// version, 2×3 transform, text descriptor, warp descriptor) and the original
// writes in PSDFixture.tySh.
//
// This is not an export path. The parity matrix records X04 — this application
// does not write PSD files — and encodeTySh has no production caller:
// roundtrip_text_test.go uses it to show the import is stable under a
// re-encode (import → export → import), which is ticket 46's acceptance at the
// record level. Nothing should start calling it to write files.
//
// The EngineData dictionary is rendered deterministically — fixed key order,
// one value per line, the shortest exact number spelling — so an encode is
// byte-stable, and a decode of it is semantically equal to the style except
// for two documented transformations: the face spelling (engineStyleRuns
// below) and tracking, which the reader derives as tracking = size · t / 1000
// (text.go applyTextStyle) and which is therefore a float round trip, not a
// bit-exact one. A value the engine cannot spell exactly returns a few ULPs
// off — size 7 with tracking 500 reads back as 500.00000000000006, a relative
// error around 1.5e-16 (7e-14 absolute) — and a second round trip does not
// drift further (TestTextRoundTripTrackingErrorBound).

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"

	"compositor-win/internal/domain"
	"compositor-win/internal/layerrender"
)

// ---------------------------------------------------------------------------
// byte writers (PSDBuffer.swift)

type tyShBuffer struct{ data []byte }

func (b *tyShBuffer) u16(v uint16) { b.data = binary.BigEndian.AppendUint16(b.data, v) }
func (b *tyShBuffer) u32(v uint32) { b.data = binary.BigEndian.AppendUint32(b.data, v) }
func (b *tyShBuffer) f64(v float64) {
	b.data = binary.BigEndian.AppendUint64(b.data, math.Float64bits(v))
}
func (b *tyShBuffer) raw(v []byte) { b.data = append(b.data, v...) }
func (b *tyShBuffer) str(v string) { b.data = append(b.data, v...) }

// ident writes a descriptor identifier: a four-byte name is stored as a
// zero length plus the four bytes (PSDBuffer.id).
func (b *tyShBuffer) ident(name string) {
	if len(name) == 4 {
		b.u32(0)
		b.str(name)
		return
	}
	b.u32(uint32(len(name)))
	b.str(name)
}

// unicode writes a UTF-16BE string with its unit count (PSDBuffer.utf16).
func (b *tyShBuffer) unicode(s string) {
	units := utf16.Encode([]rune(s))
	b.u32(uint32(len(units)))
	for _, unit := range units {
		b.u16(unit)
	}
}

// tyShItem is one descriptor entry: the key and its encoded value.
type tyShItem struct {
	key   string
	value []byte
}

func tyShTextItem(text string) []byte {
	b := &tyShBuffer{}
	b.str("TEXT")
	b.unicode(text)
	return b.data
}

func tyShEnumItem(class, name string) []byte {
	b := &tyShBuffer{}
	b.str("enum")
	b.ident(class)
	b.ident(name)
	return b.data
}

// tyShRectItem is the four-sided rectangle Photoshop stores in the text
// descriptor: an Objc value of class Rctn whose sides are UntF points
// (PSDFixture.rectItem). The reader's rect() reads the same keys.
func tyShRectItem(left, top, right, bottom float64) []byte {
	b := &tyShBuffer{}
	b.str("Objc")
	b.u32(0)
	b.ident("Rctn")
	b.u32(4)
	for _, side := range []struct {
		key   string
		value float64
	}{{"Left", left}, {"Top ", top}, {"Rght", right}, {"Btom", bottom}} {
		b.ident(side.key)
		b.str("UntF")
		b.str("#Pnt")
		b.f64(side.value)
	}
	return b.data
}

func tyShDataItem(payload []byte) []byte {
	b := &tyShBuffer{}
	b.str("tdta")
	b.u32(uint32(len(payload)))
	b.raw(payload)
	return b.data
}

// tyShDescriptor writes a versioned Object descriptor: the version, an
// empty Unicode class name, the four-byte class and the entries
// (PSDBuffer.descriptor; the reader's descriptor() expects exactly this).
func tyShDescriptor(classID string, items []tyShItem) []byte {
	b := &tyShBuffer{}
	b.u32(16)
	b.u32(0)
	b.ident(classID)
	b.u32(uint32(len(items)))
	for _, item := range items {
		b.ident(item.key)
		b.raw(item.value)
	}
	return b.data
}

// ---------------------------------------------------------------------------
// the TySh block

// encodeTySh builds the PSD type-layer block for a style. `anchor` is the
// document point the text is placed at — the point-text baseline, or the
// paragraph frame's top-left when the style carries a BoxSize. The
// transform keeps Photoshop's shape with unit scale and no rotation: a
// paragraph frame's bounds sit at the text-space origin, so the reader's
// `translation + bounds origin` anchor rule lands exactly on `anchor`, and
// the frame size comes back as boxSize = frame + 2·padding. ok is false
// when the style cannot be stored (an invalid style, an empty content or
// font name, or a box so small that the reader's frame test would fail and
// silently import it as point text).
func encodeTySh(style domain.TextStyle, anchor [2]float64) ([]byte, bool) {
	if style.Content == "" || style.FontName == "" || !textStyleValid(style) {
		return nil, false
	}
	frame, glyphs, ok := textFrameRects(style.BoxSize)
	if !ok {
		return nil, false
	}
	runs, fonts, ok := engineStyleRuns(style)
	if !ok {
		return nil, false
	}
	items := []tyShItem{
		{key: "Txt ", value: tyShTextItem(style.Content)},
		{key: "Ornt", value: tyShEnumItem("Ornt", "Hrzn")},
	}
	if style.BoxSize != nil {
		items = append(items,
			tyShItem{key: "bounds", value: tyShRectItem(frame[0], frame[1], frame[2], frame[3])},
			tyShItem{key: "boundingBox", value: tyShRectItem(glyphs[0], glyphs[1], glyphs[2], glyphs[3])},
		)
	}
	items = append(items, tyShItem{key: "EngineData", value: tyShDataItem(engineDataFor(style, runs, fonts))})

	block := &tyShBuffer{}
	block.u16(1)
	for _, v := range []float64{1, 0, 0, 1, anchor[0], anchor[1]} {
		block.f64(v)
	}
	block.u16(50)
	block.raw(tyShDescriptor("TxLr", items))
	// Photoshop always stores the warp descriptor; warpNone means "not
	// warped", which is the only state the model has.
	block.u16(1)
	block.raw(tyShDescriptor("warp", []tyShItem{{key: "warpStyle", value: tyShEnumItem("warpStyle", "warpNone")}}))
	return block.data, true
}

// textFrameRects returns the paragraph frame rect and the glyph rect the
// reader compares it against. The frame is boxSize − 2·padding (the reader
// adds the padding back), and the glyph rect sits 8 units inside it so that
// "bounds exceed the glyphs by more than 4 per side" holds. A frame of 4 or
// less cannot satisfy that gate — the reader would import such a layer as
// point text — so such a box is refused rather than stored wrong.
func textFrameRects(box *[2]float64) (frame, glyphs [4]float64, ok bool) {
	if box == nil {
		return frame, glyphs, true
	}
	width, height := box[0]-2*textPadding, box[1]-2*textPadding
	if !(width > 4 && height > 4) {
		return frame, glyphs, false
	}
	glyphWidth, glyphHeight := math.Max(0, width-8), math.Max(0, height-8)
	if !(width > glyphWidth+4 && height > glyphHeight+4 && width > 1 && height > 1) {
		return frame, glyphs, false
	}
	return [4]float64{0, 0, width, height}, [4]float64{0, 0, glyphWidth, glyphHeight}, true
}

// ---------------------------------------------------------------------------
// style runs

// engineStyleRun is one StyleSheet entry's worth of differences from the
// base style: the span in characters (RunLengthArray counts characters; the
// reader converts them to UTF-16 units) and the face and colour it paints.
// Everything else rides with the base style, which every run repeats.
type engineStyleRun struct {
	length           int
	font             int
	red, green, blue float64
}

// engineStyleRuns splits the content into EngineData style runs — one per
// span with the same face and colour, in text order, so RunLengthArray adds
// up to the whole text. The style's FontRuns and ColorRuns give UTF-16
// offsets; a rune takes the run covering its first unit (a run that splits
// a surrogate pair cannot move the pair's second unit, which matches how
// the reader measures runs in whole units). Faces are collected into the
// font set in first-use order, spelled the way Photoshop does — the
// PostScript name (layerrender.MapToPostScript; identity when the table does
// not know the face). ok is false for runs the domain would reject anyway
// (unsorted, overlapping, out of range).
//
// The spelling is a deliberate cross-platform mapping, not a rename: a PSD's
// font set carries PostScript names, so a Windows family goes into the file as
// the name Photoshop resolves it by ("Segoe UI" → "SegoeUI"), and the reader
// hands that spelling back to the model. No family is lost — MapToWindows
// turns the PostScript name into the Windows family again — and mapping twice
// is a no-op (layerrender.MapToPostScript is idempotent), so a PSD that was
// imported and re-exported does not drift.
func engineStyleRuns(style domain.TextStyle) ([]engineStyleRun, []string, bool) {
	if !textRunsValid(style) {
		return nil, nil, false
	}
	fonts := []string{layerrender.MapToPostScript(style.FontName)}
	fontIndex := func(name string) int {
		name = layerrender.MapToPostScript(name)
		for i, existing := range fonts {
			if existing == name {
				return i
			}
		}
		fonts = append(fonts, name)
		return len(fonts) - 1
	}
	// The domain guarantees the runs are sorted and non-overlapping, so a
	// single forward walk picks the run covering each character.
	fontRun, colorRun := 0, 0
	var out []engineStyleRun
	offset := 0
	for _, r := range style.Content {
		for fontRun < len(style.FontRuns) &&
			style.FontRuns[fontRun].Location+style.FontRuns[fontRun].Length <= offset {
			fontRun++
		}
		font := style.FontName
		if fontRun < len(style.FontRuns) && style.FontRuns[fontRun].Location <= offset {
			font = style.FontRuns[fontRun].FontName
		}
		for colorRun < len(style.ColorRuns) &&
			style.ColorRuns[colorRun].Location+style.ColorRuns[colorRun].Length <= offset {
			colorRun++
		}
		red, green, blue := style.Red, style.Green, style.Blue
		if colorRun < len(style.ColorRuns) && style.ColorRuns[colorRun].Location <= offset {
			run := style.ColorRuns[colorRun]
			red, green, blue = run.Red, run.Green, run.Blue
		}
		index := fontIndex(font)
		if n := len(out); n > 0 {
			last := &out[n-1]
			if last.font == index && last.red == red && last.green == green && last.blue == blue {
				last.length++
				offset += utf16Width(r)
				continue
			}
		}
		out = append(out, engineStyleRun{length: 1, font: index, red: red, green: green, blue: blue})
		offset += utf16Width(r)
	}
	return out, fonts, len(out) > 0
}

// textRunsValid mirrors the domain's run rules (project.validate.go):
// sorted, non-overlapping, positive lengths that stay inside the content.
func textRunsValid(style domain.TextStyle) bool {
	limit := utf16Len(style.Content)
	end := 0
	for _, run := range style.ColorRuns {
		if run.Length < 1 || run.Location < end || run.Location+run.Length > limit ||
			!unitColor(run.Red) || !unitColor(run.Green) || !unitColor(run.Blue) {
			return false
		}
		end = run.Location + run.Length
	}
	end = 0
	for _, run := range style.FontRuns {
		if run.Length < 1 || run.Location < end || run.Location+run.Length > limit || run.FontName == "" {
			return false
		}
		end = run.Location + run.Length
	}
	return true
}

func unitColor(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1 }

// utf16Len counts UTF-16 code units — the unit the text model measures runs
// in (domain.utf16Len).
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16Width(r)
	}
	return n
}

func utf16Width(r rune) int {
	if r > 0xFFFF {
		return 2
	}
	return 1
}

// ---------------------------------------------------------------------------
// EngineData

// engineDataFor renders the EngineData dictionary: EngineDict carrying the
// editor text, the style runs and the paragraph run, plus ResourceDict with
// the font set beside it, at the top level — the shape Photoshop writes and
// both readers look for (`walk(engine, "ResourceDict", "FontSet")`).
func engineDataFor(style domain.TextStyle, runs []engineStyleRun, fonts []string) []byte {
	size := style.FontSize
	autoLeading := style.Leading <= 0
	leading := style.Leading
	if autoLeading {
		leading = size * 1.2 // Photoshop's auto leading default
	}
	tracking := engineTracking(style)
	total := 0
	for _, run := range runs {
		total += run.length
	}

	var sb strings.Builder
	sb.WriteString("<<\n/EngineDict\n<<\n")
	sb.WriteString("/Editor\n<<\n/Text ")
	sb.WriteString(engineDataString(style.Content))
	sb.WriteString("\n>>\n")

	sb.WriteString("/StyleRun\n<<\n/RunArray\n[\n")
	for _, run := range runs {
		sb.WriteString("<<\n/StyleSheet\n<<\n/StyleSheetData\n<<\n")
		sb.WriteString("/Font " + strconv.Itoa(run.font) + "\n")
		sb.WriteString("/FontSize " + engineNumberText(size) + "\n")
		sb.WriteString("/FauxBold false\n/FauxItalic false\n")
		sb.WriteString("/AutoLeading " + engineBoolText(autoLeading) + "\n")
		sb.WriteString("/Leading " + engineNumberText(leading) + "\n")
		sb.WriteString("/Tracking " + engineNumberText(tracking) + "\n")
		sb.WriteString("/HorizontalScale 1\n/VerticalScale 1\n")
		sb.WriteString("/FillColor\n<<\n/Type 1\n/Values [ 1 ")
		sb.WriteString(engineNumberText(run.red) + " " + engineNumberText(run.green) + " " + engineNumberText(run.blue))
		sb.WriteString(" ]\n>>\n>>\n>>\n>>\n")
	}
	sb.WriteString("]\n/RunLengthArray\n[ ")
	lengths := make([]string, 0, len(runs))
	for _, run := range runs {
		lengths = append(lengths, strconv.Itoa(run.length))
	}
	sb.WriteString(strings.Join(lengths, " "))
	sb.WriteString(" ]\n>>\n")

	sb.WriteString("/ParagraphRun\n<<\n/RunArray\n[\n")
	sb.WriteString("<<\n/ParagraphSheet\n<<\n/DefaultStyleSheet 0\n/Properties\n<<\n/Justification ")
	sb.WriteString(strconv.Itoa(engineJustification(style.Alignment)))
	sb.WriteString("\n>>\n>>\n>>\n")
	sb.WriteString("]\n/RunLengthArray\n[ " + strconv.Itoa(total) + " ]\n>>\n")
	sb.WriteString(">>\n")

	sb.WriteString("/ResourceDict\n<<\n/FontSet\n[\n")
	for _, name := range fonts {
		sb.WriteString("<<\n/Name " + engineDataString(name) + "\n>>\n")
	}
	sb.WriteString("]\n>>\n>>\n")
	return []byte(sb.String())
}

// engineTracking inverts the reader's tracking conversion
// (style.Tracking = engine · size / 1000), so the engine value is the
// thousandths-of-an-em Photoshop stores.
func engineTracking(style domain.TextStyle) float64 {
	if !(style.FontSize > 0) {
		return 0
	}
	return style.Tracking * 1000 / style.FontSize
}

// engineJustification is the paragraph sheet's Justification: left 0,
// right 1, center 2 (the reader's switch).
func engineJustification(alignment domain.TextAlignment) int {
	switch alignment {
	case domain.TextAlignmentRight:
		return 1
	case domain.TextAlignmentCenter:
		return 2
	default:
		return 0
	}
}

// engineNumberText renders a number as the shortest decimal the engine
// parser reads back exactly (ParseFloat of the same text). Swift's Double
// description prints the same digits, plus a ".0" on integral values, which
// the engine grammar does not care about.
func engineNumberText(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func engineBoolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// engineDataString renders a string value for the EngineData grammar. Plain
// printable ASCII uses the parenthesized literal the original's fixture
// writes, escaping the three PostScript specials; anything else — CJK,
// emoji, newlines — uses the UTF-16BE hex form Photoshop itself stores for
// /Text, whose delimiters need no escaping and whose bytes the reader
// decodes through the FEFF byte-order mark.
func engineDataString(s string) string {
	if literal, ok := engineDataLiteral(s); ok {
		return literal
	}
	var sb strings.Builder
	sb.WriteString("<FEFF")
	for _, unit := range utf16.Encode([]rune(s)) {
		sb.WriteString(hexByte(byte(unit >> 8)))
		sb.WriteString(hexByte(byte(unit)))
	}
	sb.WriteString(">")
	return sb.String()
}

// engineDataLiteral quotes plain printable ASCII (PSDFixture.parenthesized):
// backslash and parentheses get the PostScript escape. ok is false when the
// text needs the hex form instead.
func engineDataLiteral(s string) (string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7E {
			return "", false
		}
	}
	var sb strings.Builder
	sb.WriteByte('(')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\', '(', ')':
			sb.WriteByte('\\')
		}
		sb.WriteByte(s[i])
	}
	sb.WriteByte(')')
	return sb.String(), true
}

func hexByte(b byte) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[b>>4], digits[b&0x0F]})
}
