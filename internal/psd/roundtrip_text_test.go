package psd

// Ticket 46 round-trip tests: a domain.TextStyle encoded into a TySh record
// and read back is semantically equal, runs included; plus the multi-run
// import rule, the font-mapping note, and the text-family assertions of
// PSDRoundTripTests the ticket 39 suite did not cover yet.

import (
	"bytes"
	"math"
	"reflect"
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/layerrender"
)

// textTrackingTolerance is the round-trip error bound for Tracking. The
// engine stores thousandths of an em and the reader converts back with
// tracking = engine · size / 1000 (text.go applyTextStyle), so a value the
// engine cannot spell exactly comes back a few ULPs off — size 7 with
// tracking 500 reads back as 500.00000000000006, a relative error of about
// 1.5e-16 (7e-14 absolute), and size 11 with -100 as -99.99999999999999. The
// bound below is loose on purpose; TestTextRoundTripTrackingErrorBound
// measures both cases.
const textTrackingTolerance = 1e-9

// swiftFixtureEngineData reproduces the engine dictionary PSDFixture.tySh
// writes by default: black, auto leading, no tracking, one run covering the
// text.
func swiftFixtureEngineData(text, font string, size float64) []byte {
	return fixtureMultiRunEngineData(text, []string{font},
		[]fixtureRun{{length: len([]rune(text)), size: size}}, 0)
}

// ---------------------------------------------------------------------------
// a Photoshop-shaped multi-run EngineData fixture

// fixtureRun is one StyleSheet of fixtureMultiRunEngineData. Zero values are
// written literally, so a test that wants several runs to compare equal must
// give them the same numbers.
type fixtureRun struct {
	length           int
	font             int
	size             float64
	tracking         float64
	leading          float64
	horizontalScale  float64
	verticalScale    float64
	fauxBold         bool
	fauxItalic       bool
	red, green, blue float64
}

// fixtureMultiRunEngineData writes the engine dictionary Photoshop stores:
// one StyleSheet per run with a RunLengthArray of character counts beside
// the RunArray, a paragraph run, and a top-level font set. Text goes through
// the production escaper, so a CJK or emoji string uses Photoshop's UTF-16BE
// hex form exactly as a Photoshop-written file does.
func fixtureMultiRunEngineData(text string, fonts []string, runs []fixtureRun, justify int) []byte {
	number := engineNumberText
	boolText := engineBoolText
	var sb strings.Builder
	sb.WriteString("<<\n/EngineDict <<\n/Editor << /Text ")
	sb.WriteString(engineDataString(text))
	sb.WriteString(" >>\n/StyleRun << /RunArray [\n")
	for _, run := range runs {
		hscale, vscale := run.horizontalScale, run.verticalScale
		if hscale == 0 {
			hscale = 1
		}
		if vscale == 0 {
			vscale = 1
		}
		sb.WriteString("<< /StyleSheet << /StyleSheetData <<\n")
		sb.WriteString("/Font " + itoa(run.font) + " /FontSize " + number(run.size) + "\n")
		sb.WriteString("/FauxBold " + boolText(run.fauxBold) + " /FauxItalic " + boolText(run.fauxItalic) + "\n")
		sb.WriteString("/AutoLeading false /Leading " + number(run.leading) + "\n")
		sb.WriteString("/Tracking " + number(run.tracking) + "\n")
		sb.WriteString("/HorizontalScale " + number(hscale) + " /VerticalScale " + number(vscale) + "\n")
		sb.WriteString("/FillColor << /Type 1 /Values [ 1 " +
			number(run.red) + " " + number(run.green) + " " + number(run.blue) + " ] >>\n")
		sb.WriteString(">> >> >>\n")
	}
	sb.WriteString("] /RunLengthArray [ ")
	for i, run := range runs {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(itoa(run.length))
	}
	sb.WriteString(" ] >>\n")
	sb.WriteString("/ParagraphRun << /RunArray [ << /ParagraphSheet << /Properties << /Justification ")
	sb.WriteString(itoa(justify))
	sb.WriteString(" >> >> >> ] >>\n>>\n/ResourceDict << /FontSet [\n")
	for _, name := range fonts {
		sb.WriteString("<< /Name " + engineDataString(name) + " >>\n")
	}
	sb.WriteString("] >>\n>>\n")
	return []byte(sb.String())
}

// buildTextLayer assembles a one-layer PSD whose text layer carries tysh.
func buildTextLayer(t *testing.T, width, height int, tysh []byte) Record {
	t.Helper()
	canvas := solidBitmap(width, height, 0, 0, 0, 0)
	layer := fixtureRecord{id: "T1", name: "Type", visible: true, opacity: 1, blendKey: "norm"}
	data := buildWithExtras(t, fixtureDoc{width: width, height: height, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"T1": {"TySh": tysh}})
	doc := mustRead(t, data, 1<<30)
	if len(doc.Layers) != 1 {
		t.Fatalf("layers = %d", len(doc.Layers))
	}
	return doc.Layers[0]
}

// sameTextStyle compares the fields the round trip promises: the encoder
// writes the shortest exact decimal, so content, size, base color, alignment,
// leading, boxSize and the runs all come back exactly. Two deliberate
// transformations are allowed — the face spelling, since a PSD's font set
// carries PostScript names, so a Windows family comes back in its PostScript
// spelling (layerrender.MapToPostScript), and Tracking, whose engine spelling
// tracking = size · t / 1000 is a float round trip and so compares with
// textTrackingTolerance rather than bit-for-bit (size 7, tracking 500 comes
// back as 500.00000000000006). No other field moves.
func sameTextStyle(t *testing.T, got, want domain.TextStyle) {
	t.Helper()
	if got.Content != want.Content {
		t.Errorf("content = %q, want %q", got.Content, want.Content)
	}
	if postScript := layerrender.MapToPostScript(want.FontName); got.FontName != postScript {
		t.Errorf("fontName = %q, want the PostScript spelling %q of %q", got.FontName, postScript, want.FontName)
	}
	if windows, _ := layerrender.MapToWindows(got.FontName); windows != want.FontName {
		t.Errorf("fontName = %q names %q on Windows, want %q", got.FontName, windows, want.FontName)
	}
	if got.FontSize != want.FontSize {
		t.Errorf("fontSize = %v, want %v", got.FontSize, want.FontSize)
	}
	if got.Red != want.Red || got.Green != want.Green || got.Blue != want.Blue {
		t.Errorf("color = %v,%v,%v, want %v,%v,%v", got.Red, got.Green, got.Blue, want.Red, want.Green, want.Blue)
	}
	if got.Alignment != want.Alignment {
		t.Errorf("alignment = %v, want %v", got.Alignment, want.Alignment)
	}
	if math.Abs(got.Tracking-want.Tracking) > textTrackingTolerance {
		t.Errorf("tracking = %v, want %v within %v", got.Tracking, want.Tracking, textTrackingTolerance)
	}
	if got.Leading != want.Leading {
		t.Errorf("leading = %v, want %v", got.Leading, want.Leading)
	}
	switch {
	case got.BoxSize == nil && want.BoxSize == nil:
	case got.BoxSize == nil || want.BoxSize == nil:
		t.Errorf("boxSize = %v, want %v", got.BoxSize, want.BoxSize)
	case *got.BoxSize != *want.BoxSize:
		t.Errorf("boxSize = %v, want %v", *got.BoxSize, *want.BoxSize)
	}
	if !reflect.DeepEqual(got.ColorRuns, want.ColorRuns) {
		t.Errorf("colorRuns = %+v, want %+v", got.ColorRuns, want.ColorRuns)
	}
	var wantFonts []domain.TextFontRun
	for _, run := range want.FontRuns {
		run.FontName = layerrender.MapToPostScript(run.FontName)
		wantFonts = append(wantFonts, run)
	}
	if !reflect.DeepEqual(got.FontRuns, wantFonts) {
		t.Errorf("fontRuns = %+v, want %+v", got.FontRuns, wantFonts)
	}
}

// ---------------------------------------------------------------------------
// encode → read round trip

func TestTextRoundTripPointText(t *testing.T) {
	want := domain.TextStyle{
		Content:   "Hello",
		FontName:  "Segoe UI",
		FontSize:  24,
		Red:       0,
		Green:     0,
		Blue:      0,
		Alignment: domain.TextAlignmentLeft,
		Tracking:  0,
		Leading:   0,
	}
	tysh, ok := encodeTySh(want, [2]float64{40, 50})
	if !ok {
		t.Fatal("point text should encode")
	}
	rec := buildTextLayer(t, 200, 200, tysh)
	if rec.Kind != KindText || rec.Text == nil {
		t.Fatalf("kind=%v text=%v", rec.Kind, rec.Text)
	}
	if !rec.Text.AnchorIsFrame {
		if rec.Text.Anchor != [2]float64{40, 50} {
			t.Errorf("anchor = %v, want [40 50]", rec.Text.Anchor)
		}
	} else {
		t.Error("point text must not import as a paragraph frame")
	}
	if rec.Text.Rotation != 0 || rec.Text.FlipY {
		t.Errorf("rotation=%v flipY=%v", rec.Text.Rotation, rec.Text.FlipY)
	}
	if len(rec.Text.Notes) != 0 {
		t.Errorf("notes = %v, want none", rec.Text.Notes)
	}
	sameTextStyle(t, rec.Text.Style, want)
}

// TestTextRoundTripBoxedRuns is the ticket's acceptance: a boxed style with a
// colour run and a face run, content spanning CJK and an emoji so the UTF-16
// spans matter, reads back field-for-field with the runs intact.
//
// The overrides start after the first character on purpose: Photoshop has no
// base style beside the first run, so a run covering location 0 becomes the
// base style on import (TestTextRoundTripLeadingRunBecomesTheBase documents
// that, with the per-character colours still identical).
func TestTextRoundTripBoxedRuns(t *testing.T) {
	// 汉 字 = 1 unit each; 🌏 = 2 units; a b c = 1 unit each → 7 units.
	const content = "汉字🌏abc"
	want := domain.TextStyle{
		Content:   content,
		FontName:  "Segoe UI",
		FontSize:  48,
		Red:       0.25,
		Green:     0.5,
		Blue:      0.75,
		Alignment: domain.TextAlignmentCenter,
		Tracking:  4.8, // engine 100 at 48 px
		Leading:   60,
		BoxSize:   &[2]float64{224, 104},
		ColorRuns: []domain.TextColorRun{{Location: 4, Length: 3, Red: 1, Green: 0, Blue: 0}},
		FontRuns:  []domain.TextFontRun{{Location: 2, Length: 2, FontName: "Tahoma"}},
	}
	boxOrigin := [2]float64{10, 30}
	tysh, ok := encodeTySh(want, boxOrigin)
	if !ok {
		t.Fatal("boxed text should encode")
	}
	rec := buildTextLayer(t, 400, 300, tysh)
	if rec.Kind != KindText || rec.Text == nil {
		t.Fatalf("kind=%v text=%v", rec.Kind, rec.Text)
	}
	got := rec.Text
	if !got.AnchorIsFrame {
		t.Error("boxed text must import with AnchorIsFrame")
	}
	if got.Anchor != boxOrigin {
		t.Errorf("anchor = %v, want %v", got.Anchor, boxOrigin)
	}
	if len(got.Notes) != 0 {
		t.Errorf("notes = %v, want none (runs are representable)", got.Notes)
	}
	sameTextStyle(t, got.Style, want)
}

// TestTextRoundTripEncodingIsDeterministic pins the encoder's stability: the
// same style encodes to the same bytes, key order and formatting included.
func TestTextRoundTripEncodingIsDeterministic(t *testing.T) {
	style := domain.TextStyle{
		Content:   "Stable\ntext",
		FontName:  "Segoe UI",
		FontSize:  33.5,
		Red:       0.1,
		Green:     0.2,
		Blue:      0.3,
		Alignment: domain.TextAlignmentRight,
		Tracking:  -10.05,
		Leading:   40.2,
		BoxSize:   &[2]float64{300, 120},
		ColorRuns: []domain.TextColorRun{{Location: 7, Length: 4, Red: 0, Green: 1, Blue: 0}},
		FontRuns:  []domain.TextFontRun{{Location: 0, Length: 6, FontName: "Courier New"}},
	}
	first, ok := encodeTySh(style, [2]float64{4, 8})
	if !ok {
		t.Fatal("style should encode")
	}
	second, ok := encodeTySh(style, [2]float64{4, 8})
	if !ok {
		t.Fatal("style should encode again")
	}
	if !bytes.Equal(first, second) {
		t.Error("encoding is not deterministic")
	}
	if !bytes.Equal(engineDataOf(t, style), engineDataOf(t, style)) {
		t.Error("engine data is not deterministic")
	}
}

func mustRuns(t *testing.T, style domain.TextStyle) []engineStyleRun {
	t.Helper()
	runs, _, ok := engineStyleRuns(style)
	if !ok {
		t.Fatal("style should split into runs")
	}
	return runs
}

func engineDataOf(t *testing.T, style domain.TextStyle) []byte {
	t.Helper()
	runs, fonts, ok := engineStyleRuns(style)
	if !ok {
		t.Fatal("style should split into runs")
	}
	return engineDataFor(style, runs, fonts)
}

// TestTextRoundTripLeadingRunBecomesTheBase pins the one deliberate
// transformation of the round trip: a colour run covering location 0 cannot
// also leave a base colour behind, because a PSD's base style *is* its first
// run. The read-back style carries that colour as the base and the rest of
// the text as a colour run — every character keeps the colour it was
// exported with.
func TestTextRoundTripLeadingRunBecomesTheBase(t *testing.T) {
	want := domain.TextStyle{
		Content:   "Red rest",
		FontName:  "Segoe UI",
		FontSize:  24,
		Red:       0,
		Green:     0,
		Blue:      1,
		Alignment: domain.TextAlignmentLeft,
		ColorRuns: []domain.TextColorRun{{Location: 0, Length: 3, Red: 1, Green: 0, Blue: 0}},
	}
	tysh, ok := encodeTySh(want, [2]float64{0, 0})
	if !ok {
		t.Fatal("style should encode")
	}
	rec := buildTextLayer(t, 200, 120, tysh)
	if rec.Text == nil {
		t.Fatal("text should import")
	}
	got := rec.Text.Style
	if got.Red != 1 || got.Green != 0 || got.Blue != 0 {
		t.Errorf("base colour = %v,%v,%v, want the leading run's red", got.Red, got.Green, got.Blue)
	}
	wantRun := domain.TextColorRun{Location: 3, Length: 5, Red: 0, Green: 0, Blue: 1}
	if len(got.ColorRuns) != 1 || got.ColorRuns[0] != wantRun {
		t.Errorf("colorRuns = %+v, want [%+v]", got.ColorRuns, wantRun)
	}
	// The per-character colours are exactly the exported ones.
	unit := func(style domain.TextStyle, index int) [3]float64 {
		color := [3]float64{style.Red, style.Green, style.Blue}
		for _, run := range style.ColorRuns {
			if run.Location <= index && index < run.Location+run.Length {
				color = [3]float64{run.Red, run.Green, run.Blue}
			}
		}
		return color
	}
	for i := 0; i < 8; i++ {
		if unit(got, i) != unit(want, i) {
			t.Errorf("unit %d colour = %v, want %v", i, unit(got, i), unit(want, i))
		}
	}
}

// TestEncodeTyShRefusesWhatTheReaderWouldDrop covers the gates: a style the
// model would not accept, and a box so small that the reader's frame test
// fails and the layer would silently import as point text.
func TestEncodeTyShRefusesWhatTheReaderWouldDrop(t *testing.T) {
	base := domain.TextStyle{Content: "Hi", FontName: "Segoe UI", FontSize: 24}
	if _, ok := encodeTySh(base, [2]float64{0, 0}); !ok {
		t.Error("a plain style should encode")
	}
	empty := base
	empty.Content = ""
	if _, ok := encodeTySh(empty, [2]float64{0, 0}); ok {
		t.Error("empty content should be refused")
	}
	noFont := base
	noFont.FontName = ""
	if _, ok := encodeTySh(noFont, [2]float64{0, 0}); ok {
		t.Error("an empty font name should be refused")
	}
	badSize := base
	badSize.FontSize = 0
	if _, ok := encodeTySh(badSize, [2]float64{0, 0}); ok {
		t.Error("an out-of-range size should be refused")
	}
	tinyBox := base
	tinyBox.BoxSize = &[2]float64{16, 16} // frame 0 (a reader would see 12×12)
	if _, ok := encodeTySh(tinyBox, [2]float64{0, 0}); ok {
		t.Error("a box the reader would import as point text should be refused")
	}
	thinnish := base
	thinnish.BoxSize = &[2]float64{28, 104} // frame 4: not > 4
	if _, ok := encodeTySh(thinnish, [2]float64{0, 0}); ok {
		t.Error("a frame of 4 should be refused")
	}
	badRuns := base
	badRuns.ColorRuns = []domain.TextColorRun{{Location: 1, Length: 0, Red: 0, Green: 0, Blue: 0}}
	if _, ok := encodeTySh(badRuns, [2]float64{0, 0}); ok {
		t.Error("an invalid run should be refused")
	}
}

// TestEngineDataEscaping pins the two string forms against explicit bytes:
// printable ASCII as a PostScript literal with the three specials escaped
// (PSDFixture.parenthesized), anything else as Photoshop's UTF-16BE hex
// string, whose bytes the reader decodes through the byte-order mark.
func TestEngineDataEscaping(t *testing.T) {
	style := domain.TextStyle{Content: `a(b)c\d`, FontName: "Segoe UI", FontSize: 24}
	data := string(engineDataOf(t, style))
	if !strings.Contains(data, `(a\(b\)c\\d)`) {
		t.Errorf("literal form missing or mis-escaped:\n%s", data)
	}
	style.Content = "Hi\nIo"
	data = string(engineDataOf(t, style))
	if !strings.Contains(data, "<FEFF00480069000A0049006F>") {
		t.Errorf("hex form missing or mis-encoded:\n%s", data)
	}
	emoji := domain.TextStyle{Content: "🌏", FontName: "Segoe UI", FontSize: 24}
	data = string(engineDataOf(t, emoji))
	if !strings.Contains(data, "<FEFFD83CDF0F>") {
		t.Errorf("surrogate pair not encoded:\n%s", data)
	}
	// The reader reads both forms back.
	for _, content := range []string{`a(b)c\d`, "Hi\nIo", "🌏", "汉字"} {
		s := domain.TextStyle{Content: content, FontName: "Segoe UI", FontSize: 24}
		engine := engineValueData(engineDataOf(t, s))
		if got := engineString(walk(engine, "EngineDict", "Editor", "Text")); got != content {
			t.Errorf("engine round trip of %q = %q", content, got)
		}
	}
}

// ---------------------------------------------------------------------------
// multi-run import rule

// TestImportExtractsColorAndFaceRuns: runs that differ only in colour or
// face are representable and become ColorRuns/FontRuns with UTF-16 spans,
// with no first-style note. The second run here changes face only and the
// third colour only; the content starts with a CJK string so the spans
// cannot pass by accident.
func TestImportExtractsColorAndFaceRuns(t *testing.T) {
	const content = "汉字🌏abc"
	// Run lengths are character counts: 2 + 1 + 3 = 6 characters.
	runs := []fixtureRun{
		{length: 2, size: 24, leading: 30, red: 0, green: 0, blue: 0},
		{length: 1, font: 1, size: 24, leading: 30, red: 0, green: 0, blue: 0},
		{length: 3, size: 24, leading: 30, red: 0, green: 1, blue: 0},
	}
	engine := fixtureMultiRunEngineData(content, []string{"Segoe UI", "Tahoma"}, runs, 0)
	tysh := fixtureTySh([6]float64{1, 0, 0, 1, 20, 40}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: content}},
		{key: "EngineData", value: descValue{typ: "tdta", data: engine}},
	}, "")
	rec := buildTextLayer(t, 200, 120, tysh)
	if rec.Text == nil {
		t.Fatal("text should import")
	}
	style := rec.Text.Style
	if style.FontName != "Segoe UI" || style.FontSize != 24 {
		t.Errorf("base style = %q %v", style.FontName, style.FontSize)
	}
	if style.Leading != 30 {
		t.Errorf("leading = %v", style.Leading)
	}
	wantFonts := []domain.TextFontRun{{Location: 2, Length: 2, FontName: "Tahoma"}}
	if !reflect.DeepEqual(style.FontRuns, wantFonts) {
		t.Errorf("fontRuns = %+v, want the emoji run %+v", style.FontRuns, wantFonts)
	}
	wantColors := []domain.TextColorRun{{Location: 4, Length: 3, Red: 0, Green: 1, Blue: 0}}
	if !reflect.DeepEqual(style.ColorRuns, wantColors) {
		t.Errorf("colorRuns = %+v, want %+v", style.ColorRuns, wantColors)
	}
	for _, note := range rec.Text.Notes {
		if strings.Contains(note, "first text style") {
			t.Errorf("representable differences must not be reported: %v", rec.Text.Notes)
		}
	}
}

// TestImportKeepsTheFirstStyleForSizeButExtractsRuns: a run that differs in
// size (or leading, tracking, scales, faux) cannot be stored — the
// first-style note stays — while its colour and face differences are still
// extracted.
func TestImportKeepsTheFirstStyleForSizeButExtractsRuns(t *testing.T) {
	const content = "RedBig"
	runs := []fixtureRun{
		{length: 3, size: 24, leading: 30, red: 0, green: 0, blue: 0},
		{length: 3, font: 1, size: 48, leading: 30, red: 1, green: 0, blue: 0},
	}
	engine := fixtureMultiRunEngineData(content, []string{"Segoe UI", "Tahoma"}, runs, 0)
	tysh := fixtureTySh([6]float64{1, 0, 0, 1, 20, 40}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: content}},
		{key: "EngineData", value: descValue{typ: "tdta", data: engine}},
	}, "")
	rec := buildTextLayer(t, 200, 120, tysh)
	if rec.Text == nil {
		t.Fatal("text should import")
	}
	if rec.Text.Style.FontSize != 24 {
		t.Errorf("size = %v, want the first run's 24", rec.Text.Style.FontSize)
	}
	found := false
	for _, note := range rec.Text.Notes {
		if strings.Contains(note, "Only the first text style") {
			found = true
		}
	}
	if !found {
		t.Errorf("a size difference must keep the first-style note: %v", rec.Text.Notes)
	}
	if len(rec.Text.Style.ColorRuns) != 1 || rec.Text.Style.ColorRuns[0].Location != 3 {
		t.Errorf("colorRuns = %+v, want the second run's colour", rec.Text.Style.ColorRuns)
	}
	if len(rec.Text.Style.FontRuns) != 1 || rec.Text.Style.FontRuns[0].FontName != "Tahoma" {
		t.Errorf("fontRuns = %+v, want the second run's face", rec.Text.Style.FontRuns)
	}
	if len(rec.Text.Style.ColorRuns) == 1 && rec.Text.Style.ColorRuns[0].Length != 3 {
		t.Errorf("colorRuns length = %d, want 3", rec.Text.Style.ColorRuns[0].Length)
	}
}

// TestImportDropsRunsOutsideTheContent: a RunLengthArray longer than the
// text, or a zero length, must not produce a run the domain would reject.
func TestImportDropsRunsOutsideTheContent(t *testing.T) {
	const content = "abc"
	runs := []fixtureRun{
		{length: 3, size: 24, leading: 30},
		{length: 9, size: 24, leading: 30, red: 1, green: 0, blue: 0},
	}
	engine := fixtureMultiRunEngineData(content, []string{"Segoe UI"}, runs, 0)
	tysh := fixtureTySh([6]float64{1, 0, 0, 1, 0, 0}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: content}},
		{key: "EngineData", value: descValue{typ: "tdta", data: engine}},
	}, "")
	rec := buildTextLayer(t, 100, 60, tysh)
	if rec.Text == nil {
		t.Fatal("text should import")
	}
	if len(rec.Text.Style.ColorRuns) != 0 {
		t.Errorf("colorRuns = %+v, want none (out of range)", rec.Text.Style.ColorRuns)
	}
}

// ---------------------------------------------------------------------------
// PSDRoundTripTests text family

// TestPhotoshopPointTextImportsAsEditableText ports
// photoshopPointTextImportsAsEditableText: the extras become an editable
// text layer through Read and Build, with no rasterized note.
func TestPhotoshopPointTextImportsAsEditableText(t *testing.T) {
	parsed := parseExtra(t, map[string][]byte{"TySh": fixtureTySh([6]float64{1, 0, 0, 1, 40, 50}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
		{key: "EngineData", value: descValue{typ: "tdta", data: swiftFixtureEngineData("Hello", "Helvetica", 24)}},
	}, "")})
	if parsed == nil {
		t.Fatal("plain Photoshop text should parse")
	}
	if parsed.Style.Content != "Hello" || parsed.Style.FontName != "Helvetica" {
		t.Errorf("style = %q %q", parsed.Style.Content, parsed.Style.FontName)
	}
	if parsed.Style.FontSize != 24 {
		t.Errorf("fontSize = %v, want the fixture default 24", parsed.Style.FontSize)
	}
	if parsed.Style.Red != 0 || parsed.Style.Green != 0 || parsed.Style.Blue != 0 {
		t.Errorf("color = %v,%v,%v", parsed.Style.Red, parsed.Style.Green, parsed.Style.Blue)
	}
	if parsed.Style.Alignment != domain.TextAlignmentLeft {
		t.Errorf("alignment = %v", parsed.Style.Alignment)
	}
	if len(parsed.Notes) != 0 {
		t.Errorf("notes = %v, want none", parsed.Notes)
	}

	canvas := solidBitmap(32, 32, 255, 255, 255, 255)
	layer := fixtureRecord{id: "L1", name: "Greeting", visible: true, opacity: 1, blendKey: "norm",
		left: 1, top: 2, width: 8, height: 8, image: solidBitmap(8, 8, 0, 0, 0, 255)}
	data := buildWithExtras(t, fixtureDoc{width: 32, height: 32, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"L1": {
		"TySh": fixtureTySh([6]float64{1, 0, 0, 1, 40, 50}, []descItem{
			{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
			{key: "EngineData", value: descValue{typ: "tdta", data: swiftFixtureEngineData("Hello", "Helvetica", 24)}},
		}, ""),
	}})
	doc := mustRead(t, data, 1<<30)
	read := doc.Layers[0]
	if read.Kind != KindText || read.Text == nil || read.Text.Style.Content != "Hello" {
		t.Fatalf("read = kind %v text %+v", read.Kind, read.Text)
	}
	built, err := Build(doc)
	if err != nil {
		t.Fatal(err)
	}
	imported := built.Document.Layers[0]
	if imported.Text == nil || imported.Text.Content != "Hello" ||
		imported.Text.FontName != "Helvetica" || imported.Text.FontSize != 24 {
		t.Fatalf("live text = %+v", imported.Text)
	}
	for _, conv := range built.Conversions {
		if strings.Contains(conv.Message, "像素") || strings.Contains(conv.Message, "无法再重新输入") {
			t.Errorf("editable text must not report rasterization: %+v", built.Conversions)
		}
	}
	if imported.ImageFile == nil {
		t.Fatal("the layer should have rendered pixels")
	}
	if asset := built.Assets[*imported.ImageFile]; asset == nil {
		t.Fatal("asset missing")
	} else if imported.Text != nil && asset != nil && imported.Transform.Size[0] != float64(asset.W) {
		t.Errorf("transform size = %v, asset %d×%d", imported.Transform.Size, asset.W, asset.H)
	}
	// The anchor lands the image near (40, 50): the offset is the image's own
	// anchor, so only the region is bounded here — the exact placement rides
	// on the rendered size, as in the original's ±80 window.
	origin := imported.Transform.Origin
	if origin[0] < 40-80 || origin[0] > 40+80 || origin[1] < 50-80 || origin[1] > 50+80 {
		t.Errorf("origin = %v, want near (40, 50)", origin)
	}
}

func parseExtra(t *testing.T, extra map[string][]byte) *ParsedText {
	t.Helper()
	return parseText(extra)
}

// TestPhotoshopTextSizeUsesMatrixScaleNotDocumentResolution ports the
// original: the engine size is in text-space points and the matrix scale
// multiplies it; document PPI never does — including through a full
// encode/read round trip at a non-72 resolution.
func TestPhotoshopTextSizeUsesMatrixScaleNotDocumentResolution(t *testing.T) {
	hello := func(size float64) []descItem {
		return []descItem{
			{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
			{key: "EngineData", value: descValue{typ: "tdta", data: swiftFixtureEngineData("Hello", "Helvetica", size)}},
		}
	}
	if size := parseExtra(t, map[string][]byte{"TySh": fixtureTySh([6]float64{1, 0, 0, 1, 1, 1}, hello(24), "")}).Style.FontSize; size != 24 {
		t.Errorf("scale 1: fontSize = %v, want 24", size)
	}
	if size := parseExtra(t, map[string][]byte{"TySh": fixtureTySh([6]float64{2, 0, 0, 2, 1, 1}, hello(24), "")}).Style.FontSize; size != 48 {
		t.Errorf("scale 2: fontSize = %v, want 48", size)
	}
	explicit := hello(25)
	if size := parseExtra(t, map[string][]byte{"TySh": fixtureTySh([6]float64{2, 0, 0, 2, 1, 1}, explicit, "")}).Style.FontSize; size != 50 {
		t.Errorf("explicit 25 at scale 2: fontSize = %v, want 50", size)
	}
	// The document's 300 PPI is print metadata only.
	canvas := solidBitmap(40, 40, 0, 0, 0, 0)
	layer := fixtureRecord{id: "T1", name: "T", visible: true, opacity: 1, blendKey: "norm"}
	data := buildWithExtras(t, fixtureDoc{width: 40, height: 40, resolution: 300,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"T1": {
		"TySh": fixtureTySh([6]float64{2, 0, 0, 2, 1, 1}, explicit, ""),
	}})
	doc := mustRead(t, data, 1<<30)
	if doc.Resolution != 300 {
		t.Fatalf("resolution = %v", doc.Resolution)
	}
	if size := doc.Layers[0].Text.Style.FontSize; size != 50 {
		t.Errorf("300 PPI: fontSize = %v, want 50", size)
	}
}

// TestPhotoshopTextKeepsTheFirstStyleAndReportsTheRest ports the original's
// assertions with the ticket 46 rule applied: a second run that differs in
// size keeps the note, and the base fields come from the first run.
func TestPhotoshopTextKeepsTheFirstStyleAndReportsTheRest(t *testing.T) {
	runs := []fixtureRun{
		{length: 2, size: 24, leading: 30, tracking: 1000, red: 1, green: 0, blue: 0},
		{length: 3, size: 48, leading: 30, tracking: 1000, red: 1, green: 0, blue: 0},
	}
	tysh := fixtureTySh([6]float64{1, 0, 0, 1, 40, 50}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
		{key: "EngineData", value: descValue{typ: "tdta",
			data: fixtureMultiRunEngineData("Hello", []string{"Segoe UI"}, runs, 2)}},
	}, "")
	parsed := parseExtra(t, map[string][]byte{"TySh": tysh})
	if parsed == nil {
		t.Fatal("text should parse")
	}
	if parsed.Style.Content != "Hello" {
		t.Errorf("content = %q", parsed.Style.Content)
	}
	if parsed.Style.Alignment != domain.TextAlignmentCenter {
		t.Errorf("alignment = %v, want center", parsed.Style.Alignment)
	}
	if parsed.Style.Tracking != 24 { // engine 1000 · 24 / 1000
		t.Errorf("tracking = %v, want 24", parsed.Style.Tracking)
	}
	if parsed.Style.Leading != 30 {
		t.Errorf("leading = %v, want 30", parsed.Style.Leading)
	}
	if parsed.Style.Red != 1 || parsed.Style.Green != 0 || parsed.Style.Blue != 0 {
		t.Errorf("color = %v,%v,%v", parsed.Style.Red, parsed.Style.Green, parsed.Style.Blue)
	}
	if parsed.Style.FontSize != 24 {
		t.Errorf("fontSize = %v, want the first run's 24", parsed.Style.FontSize)
	}
	if !hasNote(parsed.Notes, "Only the first text style was kept.") {
		t.Errorf("notes = %v, want the first-style note", parsed.Notes)
	}
}

// TestPhotoshopTextReportsLeadingOnlyStyleDifferences ports
// photoshopTextReportsLeadingOnlyStyleDifferences.
func TestPhotoshopTextReportsLeadingOnlyStyleDifferences(t *testing.T) {
	byLeading := parseExtra(t, map[string][]byte{"TySh": fixtureTySh([6]float64{1, 0, 0, 1, 40, 50}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
		{key: "EngineData", value: descValue{typ: "tdta", data: fixtureMultiRunEngineData("Hello",
			[]string{"Segoe UI"},
			[]fixtureRun{
				{length: 2, size: 24, leading: 30},
				{length: 3, size: 24, leading: 48},
			}, 0)}},
	}, "")})
	if byLeading == nil || byLeading.Style.Leading != 30 {
		t.Fatalf("leading style = %+v", byLeading)
	}
	if !hasNote(byLeading.Notes, "Only the first text style was kept.") {
		t.Errorf("a leading difference must keep the note: %v", byLeading.Notes)
	}
	byScale := parseExtra(t, map[string][]byte{"TySh": fixtureTySh([6]float64{1, 0, 0, 1, 40, 50}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
		{key: "EngineData", value: descValue{typ: "tdta", data: fixtureMultiRunEngineData("Hello",
			[]string{"Segoe UI"},
			[]fixtureRun{
				{length: 2, size: 24, leading: 30, horizontalScale: 1},
				{length: 3, size: 24, leading: 30, horizontalScale: 1.2},
			}, 0)}},
	}, "")})
	if byScale == nil || !hasNote(byScale.Notes, "Only the first text style was kept.") {
		t.Errorf("a scale difference must keep the note: %+v", byScale)
	}
}

func hasNote(notes []string, want string) bool {
	for _, note := range notes {
		if note == want {
			return true
		}
	}
	return false
}

// TestPhotoshopParagraphTextKeepsItsBox ports photoshopParagraphTextKeepsItsBox.
func TestPhotoshopParagraphTextKeepsItsBox(t *testing.T) {
	rect := func(l, tp, r, b float64) descValue {
		return descValue{typ: "Objc", obj: []descItem{
			{key: "Left", value: descValue{typ: "doub", num: l}},
			{key: "Top ", value: descValue{typ: "doub", num: tp}},
			{key: "Rght", value: descValue{typ: "doub", num: r}},
			{key: "Btom", value: descValue{typ: "doub", num: b}},
		}}
	}
	parsed := parseExtra(t, map[string][]byte{"TySh": fixtureTySh([6]float64{1, 0, 0, 1, 10, 30}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
		{key: "bounds", value: rect(0, 0, 200, 80)},
		{key: "boundingBox", value: rect(0, -10, 40, 10)},
	}, "")})
	if parsed == nil {
		t.Fatal("paragraph text should parse")
	}
	if !parsed.AnchorIsFrame {
		t.Error("anchorIsFrame = false, want true")
	}
	if parsed.Style.BoxSize == nil || *parsed.Style.BoxSize != [2]float64{224, 104} {
		t.Errorf("boxSize = %v, want 224×104", parsed.Style.BoxSize)
	}
	if parsed.Anchor != [2]float64{10, 30} {
		t.Errorf("anchor = %v, want (10, 30)", parsed.Anchor)
	}
	// A frame far beyond the limits drops the layer instead of importing it
	// as point text (oversizedPhotoshopParagraphFrameStaysPixels).
	oversized := parseExtra(t, map[string][]byte{"TySh": fixtureTySh([6]float64{1, 0, 0, 1, 10, 30}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
		{key: "bounds", value: rect(0, 0, 40000, 100)},
		{key: "boundingBox", value: rect(0, 0, 40, 10)},
	}, "")})
	if oversized != nil {
		t.Errorf("oversized frame = %+v, want nil", oversized)
	}
	// Through Read and Build the layer keeps the pixels Photoshop stored and
	// reports that the editable text became pixels.
	canvas := solidBitmap(16, 16, 255, 255, 255, 255)
	layer := fixtureRecord{id: "L1", name: "Billboard", visible: true, opacity: 1, blendKey: "norm",
		left: 0, top: 0, width: 4, height: 4, image: solidBitmap(4, 4, 0, 255, 0, 255)}
	data := buildWithExtras(t, fixtureDoc{width: 16, height: 16, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"L1": {
		"TySh": fixtureTySh([6]float64{1, 0, 0, 1, 10, 30}, []descItem{
			{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
			{key: "bounds", value: rect(0, 0, 40000, 100)},
			{key: "boundingBox", value: rect(0, 0, 40, 10)},
		}, ""),
	}})
	built, err := Build(mustRead(t, data, 1<<30))
	if err != nil {
		t.Fatal(err)
	}
	imported := built.Document.Layers[0]
	if imported.Text != nil {
		t.Error("an oversized frame must not stay editable")
	}
	if imported.ImageFile == nil || built.Assets[*imported.ImageFile].W != 4 {
		t.Error("the stored 4×4 pixels should be imported")
	}
	found := false
	for _, conv := range built.Conversions {
		if strings.Contains(conv.Message, "无法再重新输入") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing rasterized note: %+v", built.Conversions)
	}
}

// TestPhotoshopRotatedTextKeepsItsAngle ports rotatedPhotoshopTextKeepsItsAngle
// and adds the flipped variant: the transform's rotation and vertical flip
// survive an import.
func TestPhotoshopRotatedTextKeepsItsAngle(t *testing.T) {
	rotated := parseExtra(t, map[string][]byte{"TySh": fixtureTySh([6]float64{0, -1, 1, 0, 40, 50}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
	}, "")})
	if rotated == nil {
		t.Fatal("rotated text should import")
	}
	if math.Abs(rotated.Rotation-90) > 0.01 {
		t.Errorf("rotation = %v, want 90", rotated.Rotation)
	}
	if rotated.FlipY {
		t.Error("flipY = true, want false")
	}
	flipped := parseExtra(t, map[string][]byte{"TySh": fixtureTySh([6]float64{1, 0, 0, -1, 40, 50}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
	}, "")})
	if flipped == nil {
		t.Fatal("flipped text should import")
	}
	if !flipped.FlipY {
		t.Error("flipY = false, want true")
	}
	if math.Abs(flipped.Rotation) > 0.01 {
		t.Errorf("rotation = %v, want 0", flipped.Rotation)
	}
}

// TestWarpedPhotoshopTextStaysEditableAndSaysSo ports
// warpedPhotoshopTextStaysEditableAndSaysSo.
func TestWarpedPhotoshopTextStaysEditableAndSaysSo(t *testing.T) {
	parsed := parseExtra(t, map[string][]byte{"TySh": fixtureTySh([6]float64{1, 0, 0, 1, 40, 50}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
		{key: "EngineData", value: descValue{typ: "tdta", data: fixtureEngineData("Hello", "Segoe UI", 24, 0)}},
	}, "warpArc")})
	if parsed == nil {
		t.Fatal("warped text should stay editable")
	}
	if parsed.Style.Content != "Hello" {
		t.Errorf("content = %q", parsed.Style.Content)
	}
	if !hasNote(parsed.Notes, "The Photoshop text warp was omitted.") {
		t.Errorf("missing warp note: %v", parsed.Notes)
	}
	// Faux bold: the note, still editable.
	faux := parseExtra(t, map[string][]byte{"TySh": fixtureTySh([6]float64{1, 0, 0, 1, 40, 50}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
		{key: "EngineData", value: descValue{typ: "tdta",
			data: fixtureMultiRunEngineData("Hello", []string{"Segoe UI"}, []fixtureRun{
				{length: 5, size: 24, leading: 30, fauxBold: true},
			}, 0)}},
	}, "")})
	if faux == nil {
		t.Fatal("faux bold text should stay editable")
	}
	if !hasNote(faux.Notes, "Faux bold or faux italic was omitted.") {
		t.Errorf("missing faux note: %v", faux.Notes)
	}
}

// TestVerticalOrBrokenPhotoshopTextStaysPixels ports
// verticalOrBrokenPhotoshopTextStaysPixels.
func TestVerticalOrBrokenPhotoshopTextStaysPixels(t *testing.T) {
	if parsed := parseExtra(t, map[string][]byte{"TySh": fixtureTySh([6]float64{1, 0, 0, 1, 40, 50}, []descItem{
		{key: "Ornt", value: descValue{typ: "enum", enumCl: "Orientation", enumNm: "Vrtc"}},
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
	}, "")}); parsed != nil {
		t.Errorf("vertical text = %+v, want nil", parsed)
	}
	if parsed := parseExtra(t, map[string][]byte{"TySh": {0, 1}}); parsed != nil {
		t.Errorf("broken block = %+v, want nil", parsed)
	}
	if parsed := parseExtra(t, map[string][]byte{"TySh": fixtureTySh([6]float64{2, 0, 0, 1, 40, 50}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
	}, "")}); parsed != nil {
		t.Errorf("uneven scale = %+v, want nil", parsed)
	}
	// Through Read and Build the layer keeps its stored pixels and says so.
	canvas := solidBitmap(16, 16, 255, 255, 255, 255)
	layer := fixtureRecord{id: "L1", name: "Sideways", visible: true, opacity: 1, blendKey: "norm",
		left: 0, top: 0, width: 4, height: 4, image: solidBitmap(4, 4, 0, 0, 255, 255)}
	data := buildWithExtras(t, fixtureDoc{width: 16, height: 16, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"L1": {
		"TySh": fixtureTySh([6]float64{1, 0, 0, 1, 40, 50}, []descItem{
			{key: "Ornt", value: descValue{typ: "enum", enumCl: "Orientation", enumNm: "Vrtc"}},
			{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
		}, ""),
	}})
	doc := mustRead(t, data, 1<<30)
	read := doc.Layers[0]
	if read.Text != nil {
		t.Fatalf("text = %+v, want nil", read.Text)
	}
	built, err := Build(doc)
	if err != nil {
		t.Fatal(err)
	}
	imported := built.Document.Layers[0]
	if imported.Text != nil {
		t.Error("a vertical layer must not stay editable")
	}
	if imported.ImageFile == nil || built.Assets[*imported.ImageFile].W != 4 {
		t.Error("the stored pixels should be imported")
	}
	found := false
	for _, conv := range built.Conversions {
		if strings.Contains(conv.Message, "无法再重新输入") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing rasterized note: %+v", built.Conversions)
	}
}

// TestMissingPhotoshopFontNoteNamesTheSubstitute covers ticket 46's mapping
// note: a macOS/Adobe face this machine lacks is drawn with the Windows family
// standing in for it and the note names that family, while the model keeps
// Photoshop's own spelling. The pair is picked from the machine's own fonts
// (Helvetica → Arial, Helvetica Neue → Segoe UI, …) instead of demanding that
// a particular machine lack Helvetica, so the branch runs wherever the table
// has any useful pair; the unmapped wording is covered separately by
// TestMissingPhotoshopUnmappedFontStaysEditable, which never skips.
func TestMissingPhotoshopFontNoteNamesTheSubstitute(t *testing.T) {
	face, substitute, ok := missingMappedFace()
	if !ok {
		t.Skip("every mapped face is installed, or no substitute family is")
	}
	built := buildMissingFontDocument(t, face)
	if got := built.Document.Layers[0].Text; got == nil || got.FontName != face {
		t.Fatalf("the model must keep the Photoshop name: %+v", got)
	}
	want := "字体“" + face + "”未安装，已按“" + substitute + "”替代绘制。"
	if !hasConversion(built.Conversions, want) {
		t.Errorf("conversions = %+v, want the substitution note %q", built.Conversions, want)
	}
}

// TestMissingPhotoshopUnmappedFontStaysEditable covers the other half of the
// note: a face the mapping table does not know has no substitute to name, so
// the report keeps the original's system-font wording — and the layer stays
// editable with a rendered image all the same. Nothing here depends on the
// machine's fonts: the name is invented.
func TestMissingPhotoshopUnmappedFontStaysEditable(t *testing.T) {
	const face = "NoSuchFont-XYZ"
	built := buildMissingFontDocument(t, face)
	imported := built.Document.Layers[0]
	if imported.Text == nil {
		t.Fatal("a missing face must not cost the layer its editable text")
	}
	if imported.Text.FontName != face {
		t.Errorf("fontName = %q, want the PSD's own spelling %q", imported.Text.FontName, face)
	}
	if imported.ImageFile == nil {
		t.Error("the layer must still render with the system font")
	}
	want := "字体“" + face + "”未安装，文字以系统字体绘制。"
	if !hasConversion(built.Conversions, want) {
		t.Errorf("conversions = %+v, want %q", built.Conversions, want)
	}
	for _, conv := range built.Conversions {
		if strings.Contains(conv.Message, "已按") {
			t.Errorf("an unmapped face has no substitute to name: %+v", conv)
		}
	}
}

// missingMappedFace picks a (face, substitute) pair the forward table
// documents where this machine lacks the original and has the substitute —
// what PSDText.missingFontNote names. ok is false when no such pair exists.
func missingMappedFace() (face, substitute string, ok bool) {
	pairs := [][2]string{
		{"Helvetica", "Arial"},
		{"Helvetica Neue", "Segoe UI"},
		{"Myriad Pro", "Segoe UI"},
		{"Times-Roman", "Times New Roman"},
		{"Courier", "Courier New"},
		{"Helvetica-Light", "Arial"},
		{"HelveticaNeue-Bold", "Segoe UI"},
		{"MyriadPro-Regular", "Segoe UI"},
		{"Times-Bold", "Times New Roman"},
		{"Courier-Bold", "Courier New"},
	}
	for _, pair := range pairs {
		if layerrender.FontInstalled(pair[0]) || !layerrender.FontInstalled(pair[1]) {
			continue
		}
		return pair[0], pair[1], true
	}
	return "", "", false
}

// buildMissingFontDocument imports a one-layer document whose type layer
// paints "Sub" with face through the whole Read/Build path.
func buildMissingFontDocument(t *testing.T, face string) *Import {
	t.Helper()
	canvas := solidBitmap(80, 40, 0, 0, 0, 0)
	tysh := fixtureTySh([6]float64{1, 0, 0, 1, 5, 30}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Sub"}},
		{key: "EngineData", value: descValue{typ: "tdta", data: fixtureEngineData("Sub", face, 20, 0)}},
	}, "")
	layer := fixtureRecord{id: "T1", name: "H", visible: true, opacity: 1, blendKey: "norm"}
	data := buildWithExtras(t, fixtureDoc{width: 80, height: 40, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"T1": {"TySh": tysh}})
	built, err := Build(mustRead(t, data, 1<<30))
	if err != nil {
		t.Fatal(err)
	}
	return built
}

// hasConversion reports whether the report carries exactly this message.
func hasConversion(conversions []Conversion, message string) bool {
	for _, conv := range conversions {
		if conv.Message == message {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// import → export → import (ticket 46's acceptance)

// textEngineTySh wraps engine data in the minimal TySh the reader needs.
func textEngineTySh(content string, engine []byte) []byte {
	return fixtureTySh([6]float64{1, 0, 0, 1, 40, 50}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: content}},
		{key: "EngineData", value: descValue{typ: "tdta", data: engine}},
	}, "")
}

// fixtureRect is the four-sided rectangle a text descriptor carries.
func fixtureRect(left, top, right, bottom float64) descValue {
	return descValue{typ: "Objc", obj: []descItem{
		{key: "Left", value: descValue{typ: "doub", num: left}},
		{key: "Top ", value: descValue{typ: "doub", num: top}},
		{key: "Rght", value: descValue{typ: "doub", num: right}},
		{key: "Btom", value: descValue{typ: "doub", num: bottom}},
	}}
}

// roundTripFixtureTySh is the Photoshop-shaped input of
// TestTextRoundTripImportExportImport: a paragraph box, three style runs and
// content spanning CJK, a surrogate-pair emoji and ASCII. Run 0 is the base
// style; runs 1 and 2 both paint with the second face in a colour of their
// own, the way Photoshop splits a stretch that was styled twice — the reader
// keeps them as two runs, the writer folds them back into one.
func roundTripFixtureTySh() []byte {
	const content = "汉字🌏abc" // UTF-16 units: 汉, 字, 🌏 (2), a, b, c = 7
	runs := []fixtureRun{
		{length: 2, size: 48, tracking: 100, leading: 60},
		{length: 1, font: 1, size: 48, tracking: 100, leading: 60, red: 1},
		{length: 3, font: 1, size: 48, tracking: 100, leading: 60, red: 1},
	}
	engine := fixtureMultiRunEngineData(content, []string{"Segoe UI", "Tahoma"}, runs, 0)
	return fixtureTySh([6]float64{1, 0, 0, 1, 10, 30}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: content}},
		{key: "bounds", value: fixtureRect(0, 0, 200, 80)},
		{key: "boundingBox", value: fixtureRect(0, -10, 40, 10)},
		{key: "EngineData", value: descValue{typ: "tdta", data: engine}},
	}, "")
}

// TestTextRoundTripImportExportImport is ticket 46's acceptance, read
// literally: a Photoshop-shaped TySh imports, the imported style exports back
// to a TySh, and importing that record yields a semantically equal style —
// content, size, base colour, alignment, tracking, leading, boxSize and the
// colour and face runs included.
//
// The comparison allows exactly the three normalizations
// normalizeRoundTripStyle spells out — and nothing else. The assertions before
// it show the face spelling and the run fold are load-bearing here rather than
// a blanket escape hatch; TestRoundTripStyleNormalizationSteps pins all three
// step by step, including the unit-0 fold, which a Photoshop-shaped import has
// already applied.
func TestTextRoundTripImportExportImport(t *testing.T) {
	first := parseExtra(t, map[string][]byte{"TySh": roundTripFixtureTySh()})
	if first == nil {
		t.Fatal("the Photoshop fixture should import")
	}
	if len(first.Notes) != 0 {
		t.Errorf("notes = %v, want none (every difference is representable)", first.Notes)
	}
	if !first.AnchorIsFrame || first.Anchor != [2]float64{10, 30} {
		t.Fatalf("anchor = %v frame=%v, want the paragraph frame at (10, 30)", first.Anchor, first.AnchorIsFrame)
	}
	if first.Style.FontName != "Segoe UI" || first.Style.FontSize != 48 {
		t.Errorf("base style = %q %v, want the PSD's own Segoe UI 48", first.Style.FontName, first.Style.FontSize)
	}
	if first.Style.BoxSize == nil || *first.Style.BoxSize != [2]float64{224, 104} {
		t.Errorf("boxSize = %v, want 224×104", first.Style.BoxSize)
	}
	if first.Style.Tracking != 4.8 || first.Style.Leading != 60 ||
		first.Style.Alignment != domain.TextAlignmentLeft {
		t.Errorf("tracking/leading/alignment = %v / %v / %v, want 4.8 / 60 / left",
			first.Style.Tracking, first.Style.Leading, first.Style.Alignment)
	}
	wantColors := []domain.TextColorRun{
		{Location: 2, Length: 2, Red: 1}, // the emoji is two UTF-16 units
		{Location: 4, Length: 3, Red: 1},
	}
	if !reflect.DeepEqual(first.Style.ColorRuns, wantColors) {
		t.Errorf("first colorRuns = %+v, want the file's two runs %+v", first.Style.ColorRuns, wantColors)
	}
	wantFonts := []domain.TextFontRun{
		{Location: 2, Length: 2, FontName: "Tahoma"},
		{Location: 4, Length: 3, FontName: "Tahoma"},
	}
	if !reflect.DeepEqual(first.Style.FontRuns, wantFonts) {
		t.Errorf("first fontRuns = %+v, want the file's two runs %+v", first.Style.FontRuns, wantFonts)
	}

	tysh, ok := encodeTySh(first.Style, first.Anchor)
	if !ok {
		t.Fatal("the imported style should export")
	}
	second := parseExtra(t, map[string][]byte{"TySh": tysh})
	if second == nil {
		t.Fatal("the exported record should import")
	}
	if len(second.Notes) != 0 {
		t.Errorf("notes = %v, want none", second.Notes)
	}
	// Normalization 1, visible: the exported font set carries Photoshop's
	// PostScript spelling of the Windows family.
	if second.Style.FontName != "SegoeUI" {
		t.Errorf("second fontName = %q, want the PostScript spelling SegoeUI", second.Style.FontName)
	}
	// Normalization 3, visible: the two adjacent Tahoma/red runs became one.
	wantSecondFonts := []domain.TextFontRun{{Location: 2, Length: 5, FontName: "Tahoma"}}
	if !reflect.DeepEqual(second.Style.FontRuns, wantSecondFonts) {
		t.Errorf("second fontRuns = %+v, want the merged run %+v", second.Style.FontRuns, wantSecondFonts)
	}
	wantSecondColors := []domain.TextColorRun{{Location: 2, Length: 5, Red: 1}}
	if !reflect.DeepEqual(second.Style.ColorRuns, wantSecondColors) {
		t.Errorf("second colorRuns = %+v, want the merged run %+v", second.Style.ColorRuns, wantSecondColors)
	}

	got, want := normalizeRoundTripStyle(second.Style), normalizeRoundTripStyle(first.Style)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("second import =\n%+v\nwant the first import's style\n%+v", got, want)
	}
	if second.Anchor != first.Anchor || second.AnchorIsFrame != first.AnchorIsFrame {
		t.Errorf("second anchor = %v frame=%v, want %v frame=%v",
			second.Anchor, second.AnchorIsFrame, first.Anchor, first.AnchorIsFrame)
	}
	// The family survives the spelling change: the mapping table turns the
	// PostScript name back into the Windows family, so nothing was lost.
	if windows, ok := layerrender.MapToWindows(second.Style.FontName); !ok || windows != "Segoe UI" {
		t.Errorf("MapToWindows(%q) = (%q, %v), want the Segoe UI family", second.Style.FontName, windows, ok)
	}
}

// normalizeRoundTripStyle rewrites a style into the shape an
// import → export → import pair is allowed to produce, so two imports can be
// compared with reflect.DeepEqual. Exactly three normalizations are allowed —
// nothing else is excused:
//
//  1. Face spelling. A PSD font set carries PostScript names, so the writer
//     spells a Windows family that way (writetext.go engineStyleRuns calls
//     layerrender.MapToPostScript: "Segoe UI" → "SegoeUI", identity for a name
//     the table does not know) and the reader hands that spelling back.
//     Mapping again is a no-op (TestMapToPostScriptIsIdempotent), and
//     MapToWindows recovers the family.
//  2. A run covering UTF-16 unit 0 folds into the base style. Photoshop has no
//     base style beside its first style run, so unit 0's colour and face are
//     the base's (TestTextRoundTripLeadingRunBecomesTheBase); the old base
//     covers the rest. A Photoshop-shaped import has already folded it, so the
//     step matters for a style coming from the editor.
//  3. Adjacent runs with equal colour and face merge. The writer emits one run
//     per stretch of equal colour and face (writetext.go:257-266); the reader
//     keeps the runs the file carries (text.go extractRunOverrides), so a file
//     that spells one override as two runs gains a run the export drops.
func normalizeRoundTripStyle(s domain.TextStyle) domain.TextStyle {
	s.FontName = layerrender.MapToPostScript(s.FontName)
	for i := range s.FontRuns {
		s.FontRuns[i].FontName = layerrender.MapToPostScript(s.FontRuns[i].FontName)
	}
	s = foldLeadingRun(s)
	s.ColorRuns = mergeEqualColorRuns(s.ColorRuns)
	s.FontRuns = mergeEqualFontRuns(s.FontRuns)
	return s
}

// foldLeadingRun is normalization 2: a colour or face run starting at unit 0
// becomes the base style, and the old base comes back over the rest.
func foldLeadingRun(s domain.TextStyle) domain.TextStyle {
	limit := domain.TextUTF16Len(s.Content)
	if len(s.ColorRuns) > 0 && s.ColorRuns[0].Location == 0 {
		first, rest := s.ColorRuns[0], s.ColorRuns[1:]
		s.ColorRuns = rest
		same := first.Red == s.Red && first.Green == s.Green && first.Blue == s.Blue
		if length := limit - first.Length; !same && length > 0 {
			s.ColorRuns = append([]domain.TextColorRun{{Location: first.Length, Length: length,
				Red: s.Red, Green: s.Green, Blue: s.Blue}}, rest...)
		}
		s.Red, s.Green, s.Blue = first.Red, first.Green, first.Blue
	}
	if len(s.FontRuns) > 0 && s.FontRuns[0].Location == 0 {
		first, rest := s.FontRuns[0], s.FontRuns[1:]
		s.FontRuns = rest
		if length := limit - first.Length; first.FontName != s.FontName && length > 0 {
			s.FontRuns = append([]domain.TextFontRun{{Location: first.Length, Length: length,
				FontName: s.FontName}}, rest...)
		}
		s.FontName = first.FontName
	}
	// A folded run that covered the whole text leaves an empty slice behind;
	// the canonical form of "no runs" is nil (what the reader produces).
	if len(s.ColorRuns) == 0 {
		s.ColorRuns = nil
	}
	if len(s.FontRuns) == 0 {
		s.FontRuns = nil
	}
	return s
}

// mergeEqualColorRuns is normalization 3 for colour runs; nil stays nil.
func mergeEqualColorRuns(runs []domain.TextColorRun) []domain.TextColorRun {
	if len(runs) < 2 {
		return runs
	}
	out := make([]domain.TextColorRun, 0, len(runs))
	for _, run := range runs {
		if n := len(out); n > 0 {
			last := &out[n-1]
			if last.Location+last.Length == run.Location &&
				last.Red == run.Red && last.Green == run.Green && last.Blue == run.Blue {
				last.Length += run.Length
				continue
			}
		}
		out = append(out, run)
	}
	return out
}

// mergeEqualFontRuns is normalization 3 for face runs; nil stays nil.
func mergeEqualFontRuns(runs []domain.TextFontRun) []domain.TextFontRun {
	if len(runs) < 2 {
		return runs
	}
	out := make([]domain.TextFontRun, 0, len(runs))
	for _, run := range runs {
		if n := len(out); n > 0 {
			last := &out[n-1]
			if last.Location+last.Length == run.Location && last.FontName == run.FontName {
				last.Length += run.Length
				continue
			}
		}
		out = append(out, run)
	}
	return out
}

// TestRoundTripStyleNormalizationSteps pins each normalization
// normalizeRoundTripStyle applies, so the round-trip comparison cannot quietly
// widen into "ignore this field": every step is exercised here on a style of
// its own.
func TestRoundTripStyleNormalizationSteps(t *testing.T) {
	t.Run("face spelling", func(t *testing.T) {
		got := normalizeRoundTripStyle(domain.TextStyle{
			Content: "abc", FontName: "Segoe UI", FontSize: 12,
			FontRuns: []domain.TextFontRun{{Location: 1, Length: 2, FontName: "Courier New"}},
		})
		if got.FontName != "SegoeUI" {
			t.Errorf("fontName = %q, want SegoeUI", got.FontName)
		}
		want := []domain.TextFontRun{{Location: 1, Length: 2, FontName: "CourierNewPSMT"}}
		if !reflect.DeepEqual(got.FontRuns, want) {
			t.Errorf("fontRuns = %+v, want %+v", got.FontRuns, want)
		}
	})
	t.Run("a leading run folds into the base", func(t *testing.T) {
		got := normalizeRoundTripStyle(domain.TextStyle{
			Content: "abcd", FontName: "Segoe UI", FontSize: 12, Blue: 1,
			ColorRuns: []domain.TextColorRun{{Location: 0, Length: 2, Red: 1}},
			FontRuns:  []domain.TextFontRun{{Location: 0, Length: 2, FontName: "Tahoma"}},
		})
		if got.Red != 1 || got.Green != 0 || got.Blue != 0 {
			t.Errorf("base colour = %v,%v,%v, want the leading run's red", got.Red, got.Green, got.Blue)
		}
		wantColors := []domain.TextColorRun{{Location: 2, Length: 2, Red: 0, Green: 0, Blue: 1}}
		if !reflect.DeepEqual(got.ColorRuns, wantColors) {
			t.Errorf("colorRuns = %+v, want the old base back over the rest %+v", got.ColorRuns, wantColors)
		}
		if got.FontName != "Tahoma" {
			t.Errorf("fontName = %q, want the leading run's face", got.FontName)
		}
		wantFonts := []domain.TextFontRun{{Location: 2, Length: 2, FontName: "SegoeUI"}}
		if !reflect.DeepEqual(got.FontRuns, wantFonts) {
			t.Errorf("fontRuns = %+v, want the old base face over the rest %+v", got.FontRuns, wantFonts)
		}
	})
	t.Run("a leading run over the whole text leaves no run", func(t *testing.T) {
		got := normalizeRoundTripStyle(domain.TextStyle{
			Content: "ab", FontName: "Segoe UI", FontSize: 12,
			ColorRuns: []domain.TextColorRun{{Location: 0, Length: 2, Red: 1}},
		})
		if got.Red != 1 || got.ColorRuns != nil {
			t.Errorf("style = %+v, want the red base and no runs left", got)
		}
	})
	t.Run("adjacent equal runs merge", func(t *testing.T) {
		got := normalizeRoundTripStyle(domain.TextStyle{
			Content: "abcd", FontName: "Segoe UI", FontSize: 12,
			ColorRuns: []domain.TextColorRun{
				{Location: 1, Length: 1, Red: 1},
				{Location: 2, Length: 1, Red: 1},
				{Location: 3, Length: 1, Red: 0, Green: 1},
			},
			FontRuns: []domain.TextFontRun{
				{Location: 1, Length: 1, FontName: "Tahoma"},
				{Location: 2, Length: 1, FontName: "Tahoma"},
			},
		})
		wantColors := []domain.TextColorRun{{Location: 1, Length: 2, Red: 1}, {Location: 3, Length: 1, Green: 1}}
		if !reflect.DeepEqual(got.ColorRuns, wantColors) {
			t.Errorf("colorRuns = %+v, want %+v", got.ColorRuns, wantColors)
		}
		wantFonts := []domain.TextFontRun{{Location: 1, Length: 2, FontName: "Tahoma"}}
		if !reflect.DeepEqual(got.FontRuns, wantFonts) {
			t.Errorf("fontRuns = %+v, want %+v", got.FontRuns, wantFonts)
		}
	})
	t.Run("a gap keeps equal runs apart", func(t *testing.T) {
		got := normalizeRoundTripStyle(domain.TextStyle{
			Content: "abcd", FontName: "Segoe UI", FontSize: 12,
			ColorRuns: []domain.TextColorRun{
				{Location: 0, Length: 1, Red: 1},
				{Location: 2, Length: 1, Red: 1},
			},
		})
		if len(got.ColorRuns) != 2 {
			t.Errorf("colorRuns = %+v, want the two runs kept apart by the gap", got.ColorRuns)
		}
	})
}

// TestEngineStyleRunsMergeAdjacentEqualRuns pins the writer's folding rule
// (writetext.go:257-266): two adjacent stretches of the same colour and face
// are one run on export, whether the style spelled them as one run or two.
func TestEngineStyleRunsMergeAdjacentEqualRuns(t *testing.T) {
	base := domain.TextStyle{Content: "abcdef", FontName: "Segoe UI", FontSize: 24}
	split := base
	split.ColorRuns = []domain.TextColorRun{
		{Location: 1, Length: 2, Red: 1},
		{Location: 3, Length: 2, Red: 1},
	}
	joined := base
	joined.ColorRuns = []domain.TextColorRun{{Location: 1, Length: 4, Red: 1}}
	want := []engineStyleRun{
		{length: 1},
		{length: 4, red: 1},
		{length: 1},
	}
	if got := mustRuns(t, split); !reflect.DeepEqual(got, want) {
		t.Errorf("split runs = %+v, want the merged form %+v", got, want)
	}
	if got := mustRuns(t, joined); !reflect.DeepEqual(got, want) {
		t.Errorf("joined runs = %+v, want %+v", got, want)
	}
}

// TestEncodeTyShDropsRedundantOverrides: a run that only repeats the base
// style is not an override at all — the writer folds it into the base run, so
// the exported record carries one run and the re-import no runs.
func TestEncodeTyShDropsRedundantOverrides(t *testing.T) {
	style := domain.TextStyle{
		Content: "abcd", FontName: "Segoe UI", FontSize: 24,
		Red: 0.25, Green: 0.5, Blue: 0.75,
		ColorRuns: []domain.TextColorRun{{Location: 1, Length: 2, Red: 0.25, Green: 0.5, Blue: 0.75}},
		FontRuns:  []domain.TextFontRun{{Location: 2, Length: 2, FontName: "Segoe UI"}},
	}
	runs := mustRuns(t, style)
	if len(runs) != 1 || runs[0].length != 4 {
		t.Errorf("engine runs = %+v, want one run over the whole text", runs)
	}
	tysh, ok := encodeTySh(style, [2]float64{0, 0})
	if !ok {
		t.Fatal("the style should export")
	}
	parsed := parseExtra(t, map[string][]byte{"TySh": tysh})
	if parsed == nil {
		t.Fatal("the exported record should import")
	}
	if len(parsed.Style.ColorRuns) != 0 || len(parsed.Style.FontRuns) != 0 {
		t.Errorf("runs = %+v / %+v, want none: they repeated the base style",
			parsed.Style.ColorRuns, parsed.Style.FontRuns)
	}
	if parsed.Style.Red != 0.25 || parsed.Style.FontName != "SegoeUI" {
		t.Errorf("base style = %+v, want the base colour and the PostScript face", parsed.Style)
	}
}

// TestEngineStyleRunsSnapToWholeRunes: a face run that starts on a surrogate
// pair's second unit cannot split the pair — a rune takes the run covering its
// first unit (writetext.go engineStyleRuns), so the pair keeps the base face
// and the override starts after it. The writer neither panics nor emits a run
// the domain would reject.
func TestEngineStyleRunsSnapToWholeRunes(t *testing.T) {
	const content = "a🌏b" // UTF-16: a = 0, 🌏 = 1–2, b = 3
	style := domain.TextStyle{Content: content, FontName: "Segoe UI", FontSize: 24,
		FontRuns: []domain.TextFontRun{{Location: 2, Length: 2, FontName: "Tahoma"}}}
	if !textRunsValid(style) {
		t.Fatal("the input style's runs are inside the content")
	}
	want := []engineStyleRun{{length: 2}, {length: 1, font: 1}}
	if got := mustRuns(t, style); !reflect.DeepEqual(got, want) {
		t.Errorf("runs = %+v, want %+v (the pair keeps the base face)", got, want)
	}
	tysh, ok := encodeTySh(style, [2]float64{0, 0})
	if !ok {
		t.Fatal("the style should export")
	}
	parsed := parseExtra(t, map[string][]byte{"TySh": tysh})
	if parsed == nil {
		t.Fatal("the exported record should import")
	}
	wantRuns := []domain.TextFontRun{{Location: 3, Length: 1, FontName: "Tahoma"}}
	if !reflect.DeepEqual(parsed.Style.FontRuns, wantRuns) {
		t.Errorf("fontRuns = %+v, want the override on the rune after the pair %+v", parsed.Style.FontRuns, wantRuns)
	}
	if err := parsed.Style.ValidateRuns(); err != nil {
		t.Errorf("the imported runs must satisfy the domain: %v", err)
	}
}

// engineDataWithRunLengths writes the StyleRun block of a file whose
// RunLengthArray says less than its RunArray does: the sheets are given
// verbatim (each may or may not carry its own /RunLength) and the array's
// entries are written as passed.
func engineDataWithRunLengths(sheets []string, lengths string) []byte {
	var sb strings.Builder
	sb.WriteString("<<\n/EngineDict <<\n/Editor << /Text (Hello) >>\n/StyleRun << /RunArray [\n")
	for _, sheet := range sheets {
		sb.WriteString(sheet)
	}
	sb.WriteString("] /RunLengthArray [ " + lengths + " ] >>\n")
	sb.WriteString("/ParagraphRun << /RunArray [ << /ParagraphSheet << /Properties << /Justification 0 >> >> >> ] >>\n")
	sb.WriteString(">>\n/ResourceDict << /FontSet [ << /Name (Segoe UI) >> << /Name (Tahoma) >> ] >>\n>>\n")
	return []byte(sb.String())
}

// textRunSheet is one StyleSheet of engineDataWithRunLengths.
func textRunSheet(font int, runLength, fill string) string {
	return " << /StyleSheet << /StyleSheetData << /Font " + itoa(font) +
		" /FontSize 24 /AutoLeading false /Leading 30" + runLength + fill + " >> >> >>\n"
}

// TestRunLengthArrayShorterThanRunArray covers the fallback chain behind the
// run lengths (text.go runLength / runLengthFromSheet): a RunLengthArray with
// fewer entries than the RunArray — or with a non-positive entry — falls back
// to the StyleSheet's own /RunLength, and a run with neither collapses to an
// empty span and is dropped instead of importing an invalid run.
func TestRunLengthArrayShorterThanRunArray(t *testing.T) {
	sheets := []string{
		textRunSheet(0, " /RunLength 2", ""),
		textRunSheet(1, " /RunLength 1", ""),
		textRunSheet(0, " /RunLength 2", " /FillColor << /Values [ 1 1 0 0 ] >>"),
	}
	wantFonts := []domain.TextFontRun{{Location: 2, Length: 1, FontName: "Tahoma"}}
	wantColors := []domain.TextColorRun{{Location: 3, Length: 2, Red: 1}}
	for _, lengths := range []string{"2", "2 0"} {
		rec := readTextRecord(t, textEngineTySh("Hello", engineDataWithRunLengths(sheets, lengths)))
		if rec.Text == nil {
			t.Fatalf("RunLengthArray %q: text should import", lengths)
		}
		if !reflect.DeepEqual(rec.Text.Style.FontRuns, wantFonts) {
			t.Errorf("RunLengthArray %q: fontRuns = %+v, want %+v", lengths, rec.Text.Style.FontRuns, wantFonts)
		}
		if !reflect.DeepEqual(rec.Text.Style.ColorRuns, wantColors) {
			t.Errorf("RunLengthArray %q: colorRuns = %+v, want %+v", lengths, rec.Text.Style.ColorRuns, wantColors)
		}
	}
	// The last run has no count anywhere: its span is empty, so it is dropped
	// rather than stretched over the text or imported as an invalid run.
	noLength := []string{
		textRunSheet(0, " /RunLength 2", ""),
		textRunSheet(1, " /RunLength 1", ""),
		textRunSheet(0, "", " /FillColor << /Values [ 1 1 0 0 ] >>"),
	}
	rec := readTextRecord(t, textEngineTySh("Hello", engineDataWithRunLengths(noLength, "2")))
	if rec.Text == nil {
		t.Fatal("text should import")
	}
	if len(rec.Text.Style.ColorRuns) != 0 {
		t.Errorf("colorRuns = %+v, want none (the run has no length)", rec.Text.Style.ColorRuns)
	}
	if !reflect.DeepEqual(rec.Text.Style.FontRuns, wantFonts) {
		t.Errorf("fontRuns = %+v, want %+v", rec.Text.Style.FontRuns, wantFonts)
	}
}

// TestImportKeepsAdjacentEqualRunsApart pins the asymmetry with the writer:
// the reader keeps one run per StyleSheet the file carries
// (text.go extractRunOverrides), so a Photoshop file that spells a single
// override as two adjacent runs imports as two runs. Ticket 39 accepted that
// behaviour; the writer folds them back into one on export, which is the
// normalization the round-trip comparison applies.
func TestImportKeepsAdjacentEqualRunsApart(t *testing.T) {
	const content = "abcd"
	runs := []fixtureRun{
		{length: 2, size: 24, leading: 30},
		{length: 1, font: 1, size: 24, leading: 30, red: 1},
		{length: 1, font: 1, size: 24, leading: 30, red: 1},
	}
	engine := fixtureMultiRunEngineData(content, []string{"Segoe UI", "Tahoma"}, runs, 0)
	rec := readTextRecord(t, textEngineTySh(content, engine))
	if rec.Text == nil {
		t.Fatal("text should import")
	}
	wantFonts := []domain.TextFontRun{
		{Location: 2, Length: 1, FontName: "Tahoma"},
		{Location: 3, Length: 1, FontName: "Tahoma"},
	}
	if !reflect.DeepEqual(rec.Text.Style.FontRuns, wantFonts) {
		t.Errorf("fontRuns = %+v, want the file's two runs %+v", rec.Text.Style.FontRuns, wantFonts)
	}
	wantColors := []domain.TextColorRun{
		{Location: 2, Length: 1, Red: 1},
		{Location: 3, Length: 1, Red: 1},
	}
	if !reflect.DeepEqual(rec.Text.Style.ColorRuns, wantColors) {
		t.Errorf("colorRuns = %+v, want the file's two runs %+v", rec.Text.Style.ColorRuns, wantColors)
	}
	// The writer folds them instead — and the fold leaves every letter's style
	// alone, which is why the round-trip comparison may normalize it.
	folded := mustRuns(t, rec.Text.Style)
	if len(folded) != 2 || folded[1].length != 2 || folded[1].font != 1 {
		t.Errorf("engine runs = %+v, want the base run then one two-character override", folded)
	}
}

// TestTextRoundTripTrackingErrorBound measures the one inexact field of the
// round trip: the engine stores thousandths of an em and the reader converts
// back with tracking = engine · size / 1000 (text.go applyTextStyle), so a
// value the engine cannot spell exactly returns a few ULPs off. The bound is
// textTrackingTolerance, and a second round trip must not drift further —
// which is why sameTextStyle compares Tracking with the same tolerance.
func TestTextRoundTripTrackingErrorBound(t *testing.T) {
	cases := []struct {
		size, tracking float64
		comment        string
	}{
		{7, 500, "engine 71428.57142857143, reads back 500.00000000000006"},
		{11, -100, "engine -9090.90909090909, reads back -99.99999999999999"},
		{48, 4.8, "engine 100 exactly; the tolerance must not hide a real move"},
	}
	for _, c := range cases {
		style := domain.TextStyle{Content: "Round", FontName: "Segoe UI", FontSize: c.size,
			Alignment: domain.TextAlignmentLeft, Tracking: c.tracking}
		tysh, ok := encodeTySh(style, [2]float64{0, 0})
		if !ok {
			t.Fatalf("size %v tracking %v: the style should export", c.size, c.tracking)
		}
		parsed := parseExtra(t, map[string][]byte{"TySh": tysh})
		if parsed == nil {
			t.Fatalf("size %v tracking %v: the record should import", c.size, c.tracking)
		}
		got := parsed.Style.Tracking
		if math.Abs(got-c.tracking) > textTrackingTolerance {
			t.Errorf("size %v tracking %v (%s): read back %v, off by %v (bound %v)",
				c.size, c.tracking, c.comment, got, math.Abs(got-c.tracking), textTrackingTolerance)
		}
		again, ok := encodeTySh(parsed.Style, [2]float64{0, 0})
		if !ok {
			t.Fatalf("size %v tracking %v: the imported style should export again", c.size, c.tracking)
		}
		second := parseExtra(t, map[string][]byte{"TySh": again})
		if second == nil {
			t.Fatalf("size %v tracking %v: the second record should import", c.size, c.tracking)
		}
		if math.Abs(second.Style.Tracking-got) > textTrackingTolerance {
			t.Errorf("size %v tracking %v: the second round trip moved %v → %v",
				c.size, c.tracking, got, second.Style.Tracking)
		}
	}
}

// ---------------------------------------------------------------------------
// malformed styles stay raster

// TestMalformedTextStyleStaysRaster covers the import gate that mirrors
// LayerTextStyle.isValid (TypeTool.swift:35-62): a file whose style the model
// could not store must leave the layer raster instead of importing a document
// that cannot be saved. The content limit counts UTF-16 units, the unit
// project/validate.go:254 uses, so an emoji string can exceed it while staying
// under 100_000 runes.
func TestMalformedTextStyleStaysRaster(t *testing.T) {
	cases := []struct {
		name    string
		fonts   []string
		content string
	}{
		{"run face over 200 characters", []string{"Segoe UI", strings.Repeat("F", 201)}, "Hello"},
		{"run face with a newline", []string{"Segoe UI", "Ari\nal"}, "Hello"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runs := []fixtureRun{
				{length: 3, size: 24, leading: 30},
				{length: 2, font: 1, size: 24, leading: 30},
			}
			engine := fixtureMultiRunEngineData(c.content, c.fonts, runs, 0)
			rec := readTextRecord(t, textEngineTySh(c.content, engine))
			if rec.Text != nil {
				t.Errorf("text = %+v, want the layer to stay raster: the run face cannot be stored", rec.Text.Style)
			}
		})
	}
	t.Run("content over 100000 UTF-16 units", func(t *testing.T) {
		// 50_001 emoji are 100_002 UTF-16 units but only 50_001 runes.
		content := strings.Repeat("🌏", 50_001)
		if got := domain.TextUTF16Len(content); got != 100_002 {
			t.Fatalf("fixture content = %d UTF-16 units, want 100002", got)
		}
		engine := fixtureMultiRunEngineData(content, []string{"Segoe UI"},
			[]fixtureRun{{length: len([]rune(content)), size: 24, leading: 30}}, 0)
		rec := readTextRecord(t, textEngineTySh(content, engine))
		if rec.Text != nil {
			t.Errorf("text = %d UTF-16 units, want the layer to stay raster (over the 100000 limit)",
				domain.TextUTF16Len(rec.Text.Style.Content))
		}
	})
}
