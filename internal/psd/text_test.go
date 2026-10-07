package psd

// Ticket 39 tests: the TySh type layer (PSDTextTests semantics), the
// vector origination gates (PSDVector), and the builder's editable
// metadata + conversion report.

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// ---------------------------------------------------------------------------
// fixture byte writers

func u32be(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

func f64be(v float64) []byte { return binary.BigEndian.AppendUint64(nil, math.Float64bits(v)) }

func i32be(v int32) []byte { return binary.BigEndian.AppendUint32(nil, uint32(v)) }

func ident(name string) []byte {
	if name == "" {
		return append(u32be(0), "null"...)
	}
	return append(u32be(uint32(len(name))), name...)
}

func unicodeStr(s string) []byte {
	out := u32be(uint32(len([]rune(s))))
	for _, r := range s {
		if r > 0xFFFF {
			r1 := 0xD800 + ((r - 0x10000) >> 10)
			r2 := 0xDC00 + ((r - 0x10000) & 0x3FF)
			out = append(out, byte(r1>>8), byte(r1), byte(r2>>8), byte(r2))
		} else {
			out = append(out, byte(r>>8), byte(r))
		}
	}
	return out
}

// descValue is one descriptor item value; exactly one field is used.
type descValue struct {
	typ    string
	text   string // TEXT
	data   []byte // tdta
	enumCl string // enum class
	enumNm string // enum name
	num    float64
	obj    []descItem // Objc
}

type descItem struct {
	key   string
	value descValue
}

func descriptor(versioned bool, items []descItem) []byte {
	var out []byte
	if versioned {
		out = append(out, u32be(16)...)
	}
	out = append(out, unicodeStr("")...) // classID
	out = append(out, ident("")...)      // classID name ("null")
	out = append(out, u32be(uint32(len(items)))...)
	for _, item := range items {
		out = append(out, ident(item.key)...)
		out = append(out, item.value.encode()...)
	}
	return out
}

func (v descValue) encode() []byte {
	out := []byte(v.typ)
	switch v.typ {
	case "TEXT":
		out = append(out, unicodeStr(v.text)...)
	case "tdta":
		out = append(out, u32be(uint32(len(v.data)))...)
		out = append(out, v.data...)
	case "enum":
		out = append(out, ident(v.enumCl)...)
		out = append(out, ident(v.enumNm)...)
	case "doub":
		out = append(out, f64be(v.num)...)
	case "long":
		out = append(out, i32be(int32(v.num))...)
	case "bool":
		out = append(out, 1)
	case "Objc":
		out = append(out, descriptor(false, v.obj)...)
	}
	return out
}

// fixtureTySh builds a Photoshop 6 type layer: version, the 2×3 transform,
// 50, the text descriptor and (optionally) the warp descriptor.
func fixtureTySh(transform [6]float64, items []descItem, warpStyle string) []byte {
	out := binary.BigEndian.AppendUint16(nil, 1)
	for _, v := range transform {
		out = append(out, f64be(v)...)
	}
	out = append(out, binary.BigEndian.AppendUint16(nil, 50)...)
	out = append(out, descriptor(true, items)...)
	if warpStyle != "" {
		out = append(out, binary.BigEndian.AppendUint16(nil, 1)...)
		out = append(out, descriptor(true, []descItem{{
			key: "warpStyle", value: descValue{typ: "enum", enumCl: "warpStyle", enumNm: warpStyle},
		}})...)
	}
	return out
}

// fixtureEngineData builds the PostScript-ish text-engine dictionary.
func fixtureEngineData(text string, fontName string, fontSize float64, justify int) []byte {
	var sb strings.Builder
	sb.WriteString("<<\n/EngineDict <<\n/Editor << /Text (")
	sb.WriteString(text)
	sb.WriteString(") >>\n")
	sb.WriteString("/StyleRun << /RunArray [ <<\n/StyleSheet << /StyleSheetData <<\n")
	sb.WriteString("/FontSize ")
	sb.WriteString(ftoa(fontSize))
	sb.WriteString(" /Font 0 /Tracking 100 /AutoLeading false /Leading 60\n")
	sb.WriteString("/FillColor << /Values [ 1.0 1.0 0.0 0.0 ] >>\n")
	sb.WriteString("/FauxBold false /FauxItalic false\n")
	sb.WriteString(">> >>\n>> ] >>\n")
	sb.WriteString("/ParagraphRun << /RunArray [ << /ParagraphSheet << /Properties << /Justification ")
	sb.WriteString(itoa(justify))
	sb.WriteString(" >> >> >> ] >>\n")
	sb.WriteString(">>\n/ResourceDict << /FontSet [ << /Name (")
	sb.WriteString(fontName)
	sb.WriteString(") >> ] >>\n>>\n")
	return []byte(sb.String())
}

func ftoa(v float64) string {
	// compact float formatting without importing strconv in every call site
	return strings.TrimRight(strings.TrimRight(formatFloat(v), "0"), ".")
}

func formatFloat(v float64) string {
	if v == math.Trunc(v) {
		return itoa(int(v)) + ".0"
	}
	return fmtFloat(v)
}

func fmtFloat(v float64) string {
	intPart := int(v)
	frac := v - float64(intPart)
	return itoa(intPart) + "." + itoa(int(frac*1000+0.5))
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

// textFixtureLayer is a layer whose only pixels are empty channels (a type
// or shape layer Photoshop stores without own pixels in our fixture).
func textFixtureLayer(id, name string, bounds fixtureRecord, extras map[string][]byte) fixtureRecord {
	bounds.id = id
	bounds.name = name
	return bounds
}

func buildWithExtras(t *testing.T, doc fixtureDoc, composite *render.Bitmap, extras map[string]map[string][]byte) []byte {
	t.Helper()
	return fixtureData(doc, composite, false, "", nil, extras)
}

// ---------------------------------------------------------------------------
// PSDTextTests

func TestTextLayerParsesEngineStyle(t *testing.T) {
	canvas := solidBitmap(80, 40, 0, 0, 0, 0)
	// 单位矩阵：无缩放、无旋转。
	tysh := fixtureTySh([6]float64{1, 0, 0, 1, 4, 30}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Hello"}},
		{key: "EngineData", value: descValue{typ: "tdta",
			data: fixtureEngineData("Hello", "Segoe UI", 48, 2)}},
	}, "")
	layer := fixtureRecord{id: "T1", name: "Type", visible: true, opacity: 1, blendKey: "norm"}
	data := buildWithExtras(t, fixtureDoc{width: 80, height: 40, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"T1": {"TySh": tysh}})
	doc := mustRead(t, data, 1<<30)
	if len(doc.Layers) != 1 {
		t.Fatalf("layers = %d", len(doc.Layers))
	}
	rec := doc.Layers[0]
	if rec.Kind != KindText || rec.Text == nil {
		t.Fatalf("kind=%v text=%v", rec.Kind, rec.Text)
	}
	style := rec.Text.Style
	if style.Content != "Hello" {
		t.Errorf("content = %q", style.Content)
	}
	if style.FontName != "Segoe UI" {
		t.Errorf("font = %q", style.FontName)
	}
	if math.Abs(style.FontSize-48) > 0.01 {
		t.Errorf("size = %v", style.FontSize)
	}
	if style.Red != 1 || style.Green != 0 || style.Blue != 0 {
		t.Errorf("color = %v,%v,%v", style.Red, style.Green, style.Blue)
	}
	if style.Alignment != domain.TextAlignmentCenter {
		t.Errorf("alignment = %v", style.Alignment)
	}
	// tracking 100 → 100·48/1000 = 4.8
	if math.Abs(style.Tracking-4.8) > 0.01 {
		t.Errorf("tracking = %v", style.Tracking)
	}
	if math.Abs(style.Leading-60) > 0.01 {
		t.Errorf("leading = %v", style.Leading)
	}
	if style.BoxSize != nil {
		t.Errorf("point text 不应有框: %v", style.BoxSize)
	}
}

func TestVerticalTextStaysRaster(t *testing.T) {
	canvas := solidBitmap(40, 40, 0, 0, 0, 0)
	tysh := fixtureTySh([6]float64{1, 0, 0, 1, 0, 0}, []descItem{
		{key: "Ornt", value: descValue{typ: "enum", enumCl: "Orientation", enumNm: "Vrtc"}},
		{key: "Txt ", value: descValue{typ: "TEXT", text: "縦書き"}},
	}, "")
	layer := fixtureRecord{id: "T1", name: "V", visible: true, opacity: 1, blendKey: "norm"}
	data := buildWithExtras(t, fixtureDoc{width: 40, height: 40, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"T1": {"TySh": tysh}})
	doc := mustRead(t, data, 1<<30)
	if doc.Layers[0].Text != nil {
		t.Fatal("竖排文本应保持栅格")
	}
}

func TestShearedTransformStaysRaster(t *testing.T) {
	canvas := solidBitmap(40, 40, 0, 0, 0, 0)
	// 剪切矩阵：xy 分量过大。
	tysh := fixtureTySh([6]float64{1, 0.5, 0, 1, 0, 0}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Hi"}},
	}, "")
	layer := fixtureRecord{id: "T1", name: "S", visible: true, opacity: 1, blendKey: "norm"}
	data := buildWithExtras(t, fixtureDoc{width: 40, height: 40, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"T1": {"TySh": tysh}})
	doc := mustRead(t, data, 1<<30)
	if doc.Layers[0].Text != nil {
		t.Fatal("剪切应保持栅格")
	}
}

func TestRotatedTextKeepsRotation(t *testing.T) {
	canvas := solidBitmap(60, 60, 0, 0, 0, 0)
	// 旋转 90°：xx=0, yx=1, xy=-1, yy=0（顺时针）。
	tysh := fixtureTySh([6]float64{0, -1, 1, 0, 30, 10}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "R"}},
		{key: "EngineData", value: descValue{typ: "tdta",
			data: fixtureEngineData("R", "Segoe UI", 24, 0)}},
	}, "")
	layer := fixtureRecord{id: "T1", name: "R", visible: true, opacity: 1, blendKey: "norm"}
	data := buildWithExtras(t, fixtureDoc{width: 60, height: 60, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"T1": {"TySh": tysh}})
	doc := mustRead(t, data, 1<<30)
	rec := doc.Layers[0]
	if rec.Text == nil {
		t.Fatal("旋转文本应可导入")
	}
	if math.Abs(rec.Text.Rotation-90) > 0.5 {
		t.Errorf("rotation = %v，想要 90", rec.Text.Rotation)
	}
}

func TestTextNotesForWarpFauxAndJustify(t *testing.T) {
	canvas := solidBitmap(50, 50, 0, 0, 0, 0)
	tysh := fixtureTySh([6]float64{1, 0, 0, 1, 5, 5}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Note"}},
		{key: "EngineData", value: descValue{typ: "tdta",
			data: fixtureEngineData("Note", "Segoe UI", 20, 3)}},
	}, "warpRise")
	layer := fixtureRecord{id: "T1", name: "N", visible: true, opacity: 1, blendKey: "norm"}
	data := buildWithExtras(t, fixtureDoc{width: 50, height: 50, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"T1": {"TySh": tysh}})
	doc := mustRead(t, data, 1<<30)
	rec := doc.Layers[0]
	if rec.Text == nil {
		t.Fatal("文本应可导入")
	}
	all := strings.Join(rec.Text.Notes, "|")
	if !strings.Contains(all, "warp") {
		t.Errorf("缺少 warp 提示: %v", rec.Text.Notes)
	}
	if !strings.Contains(all, "Full justification") {
		t.Errorf("缺少两端对齐提示: %v", rec.Text.Notes)
	}
	if rec.Text.Style.Alignment != domain.TextAlignmentLeft {
		t.Errorf("两端对齐应回落左对齐: %v", rec.Text.Style.Alignment)
	}
}

func TestParagraphFrameBecomesBoxSize(t *testing.T) {
	canvas := solidBitmap(200, 100, 0, 0, 0, 0)
	rect := func(l, tp, r, b float64) descValue {
		return descValue{typ: "Objc", obj: []descItem{
			{key: "Left", value: descValue{typ: "doub", num: l}},
			{key: "Top ", value: descValue{typ: "doub", num: tp}},
			{key: "Rght", value: descValue{typ: "doub", num: r}},
			{key: "Btom", value: descValue{typ: "doub", num: b}},
		}}
	}
	tysh := fixtureTySh([6]float64{1, 0, 0, 1, 10, 10}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Boxed"}},
		{key: "bounds", value: rect(0, 0, 120, 60)},
		{key: "boundingBox", value: rect(0, 0, 60, 20)},
		{key: "EngineData", value: descValue{typ: "tdta",
			data: fixtureEngineData("Boxed", "Segoe UI", 24, 0)}},
	}, "")
	layer := fixtureRecord{id: "T1", name: "B", visible: true, opacity: 1, blendKey: "norm"}
	data := buildWithExtras(t, fixtureDoc{width: 200, height: 100, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"T1": {"TySh": tysh}})
	doc := mustRead(t, data, 1<<30)
	rec := doc.Layers[0]
	if rec.Text == nil || rec.Text.Style.BoxSize == nil {
		t.Fatal("段落框应转成 BoxSize")
	}
	if !rec.Text.AnchorIsFrame {
		t.Error("段落框锚点应在框左上")
	}
	if rec.Text.Style.BoxSize[0] != 144 || rec.Text.Style.BoxSize[1] != 84 {
		t.Errorf("boxSize = %v（应为 120+24 × 60+24）", rec.Text.Style.BoxSize)
	}
}

// ---------------------------------------------------------------------------
// PSDVectorTests

// vogk builds the vector-origination payload: version, then descriptor-ish
// entries the original scans by key name.
func fixtureVogk(kind int32, left, top, right, bottom float64, radius *float64) []byte {
	untf := func(v float64) []byte {
		out := append([]byte("UntF"), "Pnt "...)
		return append(out, f64be(v)...)
	}
	entry := func(key string, value []byte) []byte {
		return append(ident(key), value...)
	}
	out := u32be(1)
	out = append(out, entry("keyOriginType", append([]byte("long"), i32be(kind)...))...)
	var bbox []byte
	bbox = append(bbox, entry("Left", untf(left))...)
	bbox = append(bbox, entry("Top ", untf(top))...)
	bbox = append(bbox, entry("Rght", untf(right))...)
	bbox = append(bbox, entry("Btom", untf(bottom))...)
	out = append(out, entry("keyOriginShapeBBox", append([]byte("Objc"), bbox...))...)
	if radius != nil {
		var radii []byte
		for _, key := range []string{"topLeft", "topRight", "bottomRight", "bottomLeft"} {
			radii = append(radii, entry(key, untf(*radius))...)
		}
		out = append(out, entry("keyOriginRRectRadii", append([]byte("Objc"), radii...))...)
	}
	return out
}

func fixtureSoCo(r, g, b float64) []byte {
	out := []byte{}
	for _, kv := range []struct {
		key string
		val float64
	}{{"Rd  ", r}, {"Grn ", g}, {"Bl  ", b}} {
		out = append(out, ident(kv.key)...)
		out = append(out, []byte("doub")...)
		out = append(out, f64be(kv.val)...)
	}
	return out
}

func fixtureVstk(fillEnabled, strokeEnabled bool, width float64) []byte {
	out := []byte{}
	out = append(out, ident("fillEnabled")...)
	out = append(out, []byte("bool")...)
	if fillEnabled {
		out = append(out, 1)
	} else {
		out = append(out, 0)
	}
	out = append(out, ident("strokeEnabled")...)
	out = append(out, []byte("bool")...)
	if strokeEnabled {
		out = append(out, 1)
	} else {
		out = append(out, 0)
	}
	if strokeEnabled {
		out = append(out, fixtureSoCo(0, 0, 1)...)
		out = append(out, ident("strokeStyleLineWidth")...)
		out = append(out, []byte("UntF")...)
		out = append(out, []byte("Pnt ")...)
		out = append(out, f64be(width)...)
	}
	return out
}

// fixtureVmsk builds a path record list: version + records of 26 bytes.
type pathPoint struct {
	x, y float64
}

func fixtureVmsk(points []pathPoint, canvasW, canvasH int, closed bool) []byte {
	out := u32be(3)
	out = append(out, u32be(0)...) // record area length (unused by the reader)
	typ := uint16(3)
	if !closed {
		typ = 1
	}
	rec := binary.BigEndian.AppendUint16(nil, typ)
	rec = append(rec, binary.BigEndian.AppendUint16(nil, uint16(len(points)))...)
	rec = append(rec, make([]byte, 22)...)
	out = append(out, rec...)
	for _, p := range points {
		write24_8 := func(v float64, size int) []byte {
			return i32be(int32(v / float64(size) * 0x1000000))
		}
		one := write24_8(p.y, canvasH)
		one = append(one, write24_8(p.x, canvasW)...) // 8 bytes: y then x
		body := append([]byte{}, one...)              // in
		body = append(body, one...)                   // anchor
		body = append(body, one...)                   // out: sharp
		rec := binary.BigEndian.AppendUint16(nil, 1)
		rec = append(rec, body...)
		out = append(out, rec...)
	}
	return out
}

func TestLiveShapeRectangleBecomesShapeLayer(t *testing.T) {
	canvas := solidBitmap(100, 80, 0, 0, 0, 0)
	radius := 0.0
	layer := fixtureRecord{id: "V1", name: "Shape", visible: true, opacity: 1, blendKey: "norm",
		left: 20, top: 10, width: 40, height: 30, image: solidBitmap(40, 30, 9, 9, 9, 255)}
	data := buildWithExtras(t, fixtureDoc{width: 100, height: 80, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"V1": {
		"vogk": fixtureVogk(2, 20, 10, 60, 40, &radius),
		"SoCo": fixtureSoCo(0.2, 0.4, 0.6),
	}})
	doc := mustRead(t, data, 1<<30)
	rec := doc.Layers[0]
	if rec.Kind != KindVector || rec.Shape == nil {
		t.Fatalf("kind=%v shape=%v", rec.Kind, rec.Shape)
	}
	if rec.Shape.Kind != "Rectangle" {
		t.Errorf("kind = %q", rec.Shape.Kind)
	}
	if rec.Shape.X != 20 || rec.Shape.Y != 10 || rec.Shape.Width != 40 || rec.Shape.Height != 30 {
		t.Errorf("bounds = %v,%v %v×%v", rec.Shape.X, rec.Shape.Y, rec.Shape.Width, rec.Shape.Height)
	}
	if math.Abs(rec.Shape.Red-0.2) > 0.01 || math.Abs(rec.Shape.Green-0.4) > 0.01 || math.Abs(rec.Shape.Blue-0.6) > 0.01 {
		t.Errorf("color = %v,%v,%v", rec.Shape.Red, rec.Shape.Green, rec.Shape.Blue)
	}
	// 活形状的像素由重绘产生（不再是存储的半灰）。
	if rec.Image == nil {
		t.Fatal("活形状应有重绘像素")
	}
	mid := rec.Image.Pix[(15*40+20)*4 : (15*40+20)*4+4]
	if mid[3] == 0 {
		t.Fatal("矩形中心应有填充")
	}
	if mid[0] < 40 || mid[2] < 140 {
		t.Errorf("填充色应为 SoCo 色: %v", mid)
	}
	// 圆角矩形。
	layer2 := fixtureRecord{id: "V2", name: "Rounded", visible: true, opacity: 1, blendKey: "norm",
		left: 0, top: 0, width: 60, height: 40, image: solidBitmap(60, 40, 9, 9, 9, 255)}
	r := 8.0
	data2 := buildWithExtras(t, fixtureDoc{width: 100, height: 80, resolution: 72,
		layers: []fixtureRecord{layer2}}, canvas, map[string]map[string][]byte{"V2": {
		"vogk": fixtureVogk(2, 0, 0, 60, 40, &r),
		"SoCo": fixtureSoCo(1, 0, 0),
	}})
	doc2 := mustRead(t, data2, 1<<30)
	if doc2.Layers[0].Shape == nil || doc2.Layers[0].Shape.CornerRadius != 8 {
		t.Fatalf("圆角 = %v", doc2.Layers[0].Shape)
	}
}

func TestVectorRasterFallbackDrawsPath(t *testing.T) {
	canvas := solidBitmap(60, 60, 0, 0, 0, 0)
	// 空层（无存储像素）+ vmsk 三角 + SoCo。
	empty := fixtureRecord{id: "P1", name: "Path", visible: true, opacity: 1, blendKey: "norm"}
	data := buildWithExtras(t, fixtureDoc{width: 60, height: 60, resolution: 72,
		layers: []fixtureRecord{empty}}, canvas, map[string]map[string][]byte{"P1": {
		"vmsk": fixtureVmsk([]pathPoint{{10, 10}, {50, 10}, {30, 50}}, 60, 60, true),
		"SoCo": fixtureSoCo(0, 1, 0),
	}})
	doc := mustRead(t, data, 1<<30)
	rec := doc.Layers[0]
	if rec.Kind != KindVector || rec.Image == nil {
		t.Fatalf("空层 vmsk 应重绘: kind=%v image=%v", rec.Kind, rec.Image)
	}
	// 三角形内部（30,20 在上边线附近内侧）应有绿填充。
	inside := false
	for y := 12; y < 30 && !inside; y++ {
		for x := 25; x < 35; x++ {
			i := (y*rec.Image.W + x) * 4
			if rec.Image.Pix[i+3] > 0 && rec.Image.Pix[i+1] > rec.Image.Pix[i] {
				inside = true
				break
			}
		}
	}
	if !inside {
		t.Fatal("三角形内部应有填充")
	}
}

func TestVectorStrokeNote(t *testing.T) {
	canvas := solidBitmap(60, 60, 0, 0, 0, 0)
	radius := 0.0
	layer := fixtureRecord{id: "V1", name: "Stroked", visible: true, opacity: 1, blendKey: "norm",
		left: 5, top: 5, width: 30, height: 30, image: solidBitmap(30, 30, 0, 0, 0, 0)}
	data := buildWithExtras(t, fixtureDoc{width: 60, height: 60, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"V1": {
		"vogk": fixtureVogk(1, 5, 5, 35, 35, &radius),
		"SoCo": fixtureSoCo(1, 1, 0),
		"vstk": fixtureVstk(true, true, 4),
	}})
	doc := mustRead(t, data, 1<<30)
	rec := doc.Layers[0]
	if rec.Shape == nil {
		t.Fatal("应仍是活形状")
	}
	found := false
	for _, note := range rec.Shape.Notes {
		if strings.Contains(note, "stroke") {
			found = true
		}
	}
	if !found {
		t.Errorf("描边应产生省略提示: %v", rec.Shape.Notes)
	}
}

func TestEllipseOrigination(t *testing.T) {
	canvas := solidBitmap(80, 80, 0, 0, 0, 0)
	layer := fixtureRecord{id: "E1", name: "Ellipse", visible: true, opacity: 1, blendKey: "norm",
		left: 10, top: 10, width: 60, height: 40, image: solidBitmap(60, 40, 0, 0, 0, 0)}
	data := buildWithExtras(t, fixtureDoc{width: 80, height: 80, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"E1": {
		"vogk": fixtureVogk(5, 10, 10, 70, 50, nil),
		"SoCo": fixtureSoCo(1, 0, 1),
	}})
	doc := mustRead(t, data, 1<<30)
	rec := doc.Layers[0]
	if rec.Shape == nil || rec.Shape.Kind != "Ellipse" {
		t.Fatalf("shape = %v", rec.Shape)
	}
	if rec.Image == nil {
		t.Fatal("椭圆应重绘")
	}
	// 四角空、中心实。
	if rec.Image.Pix[(0*60+0)*4+3] != 0 {
		t.Fatal("椭圆角落应为空")
	}
	if rec.Image.Pix[(20*60+30)*4+3] == 0 {
		t.Fatal("椭圆中心应有填充")
	}
}

// ---------------------------------------------------------------------------
// builder: editable metadata + conversion report

func TestBuilderKeepsTextEditableAndRendersPixels(t *testing.T) {
	canvas := solidBitmap(120, 60, 0, 0, 0, 0)
	tysh := fixtureTySh([6]float64{1, 0, 0, 1, 10, 10}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "Title"}},
		{key: "EngineData", value: descValue{typ: "tdta",
			data: fixtureEngineData("Title", "Segoe UI", 32, 0)}},
	}, "")
	layer := fixtureRecord{id: "T1", name: "Title", visible: true, opacity: 1, blendKey: "norm"}
	data := buildWithExtras(t, fixtureDoc{width: 120, height: 60, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"T1": {"TySh": tysh}})
	doc := mustRead(t, data, 1<<30)
	built, err := Build(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Document.Layers) != 1 {
		t.Fatalf("layers = %d", len(built.Document.Layers))
	}
	l := built.Document.Layers[0]
	if l.Text == nil {
		t.Fatal("文字图层应保留可编辑元数据")
	}
	if l.Text.Content != "Title" || l.Text.FontSize != 32 {
		t.Errorf("text = %+v", l.Text)
	}
	if l.ImageFile == nil {
		t.Fatal("文字图层应有渲染像素资产")
	}
	asset := built.Assets[*l.ImageFile]
	if asset == nil {
		t.Fatal("资产缺失")
	}
	inked := 0
	for i := 0; i < asset.W*asset.H; i++ {
		if asset.Pix[i*4+3] > 0 {
			inked++
		}
	}
	if inked < 50 {
		t.Errorf("渲染文本应有墨迹: %d", inked)
	}
	// 位图尺寸 = 图像尺寸（点位文本）。
	if l.Transform.Size[0] != float64(asset.W) || l.Transform.Size[1] != float64(asset.H) {
		t.Errorf("transform = %v", l.Transform)
	}
}

func TestBuilderLiveShapeKeepsShapeStyle(t *testing.T) {
	canvas := solidBitmap(60, 60, 0, 0, 0, 0)
	layer := fixtureRecord{id: "V1", name: "Rect", visible: true, opacity: 1, blendKey: "norm",
		left: 5, top: 5, width: 40, height: 30, image: solidBitmap(40, 30, 9, 9, 9, 255)}
	data := buildWithExtras(t, fixtureDoc{width: 60, height: 60, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"V1": {
		"vogk": fixtureVogk(2, 5, 5, 45, 35, nil),
		"SoCo": fixtureSoCo(1, 0.5, 0),
	}})
	doc := mustRead(t, data, 1<<30)
	built, err := Build(doc)
	if err != nil {
		t.Fatal(err)
	}
	l := built.Document.Layers[0]
	if l.Shape == nil {
		t.Fatal("矢量图层应保留形状样式")
	}
	if l.Shape.Kind != domain.ShapeRectangle || l.Shape.Red != 1 || math.Abs(l.Shape.Green-0.5) > 0.01 {
		t.Errorf("shape = %+v", l.Shape)
	}
	if l.Transform.Origin != [2]float64{5, 5} || l.Transform.Size != [2]float64{40, 30} {
		t.Errorf("transform = %v", l.Transform)
	}
}

func TestBuilderMissingFontNote(t *testing.T) {
	canvas := solidBitmap(80, 40, 0, 0, 0, 0)
	tysh := fixtureTySh([6]float64{1, 0, 0, 1, 5, 30}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: "X"}},
		{key: "EngineData", value: descValue{typ: "tdta",
			data: fixtureEngineData("X", "NoSuchFont-XYZ", 20, 0)}},
	}, "")
	layer := fixtureRecord{id: "T1", name: "M", visible: true, opacity: 1, blendKey: "norm"}
	data := buildWithExtras(t, fixtureDoc{width: 80, height: 40, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"T1": {"TySh": tysh}})
	doc := mustRead(t, data, 1<<30)
	built, err := Build(doc)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, conv := range built.Conversions {
		if strings.Contains(conv.Message, "NoSuchFont-XYZ") {
			found = true
		}
	}
	if !found {
		t.Errorf("应报告字体缺失: %+v", built.Conversions)
	}
}

func TestBuilderUnrenderableTextFallsBackToPixels(t *testing.T) {
	canvas := solidBitmap(40, 40, 0, 0, 0, 0)
	// TySh 里只有内容、没有 EngineData 也没有 Txt：解析成功但内容来自
	// Editor/Text——这里故意放一个会解析失败的超长字体尺寸之外的值：
	// 用空内容使 parseText 返回 nil → 图层保持存储像素。
	tysh := fixtureTySh([6]float64{1, 0, 0, 1, 0, 0}, []descItem{
		{key: "Txt ", value: descValue{typ: "TEXT", text: ""}},
	}, "")
	layer := fixtureRecord{id: "T1", name: "Empty", visible: true, opacity: 1, blendKey: "norm",
		image: solidBitmap(10, 10, 1, 2, 3, 255)}
	data := buildWithExtras(t, fixtureDoc{width: 40, height: 40, resolution: 72,
		layers: []fixtureRecord{layer}}, canvas, map[string]map[string][]byte{"T1": {"TySh": tysh}})
	doc := mustRead(t, data, 1<<30)
	rec := doc.Layers[0]
	if rec.Text != nil {
		t.Fatal("空内容应保持栅格")
	}
	if rec.Image == nil {
		t.Fatal("存储像素应保留")
	}
}
